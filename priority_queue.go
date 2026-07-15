/*
PriorityQueue provides a dual-queue system with fairness guarantees and built-in skip-based backoff.

Unlike a traditional priority queue where items can be reordered or demoted, PriorityQueue maintains
two separate permanent queues. Items in the priority queue always maintain their priority status,
ensuring critical work (like monitoring essential nodes) never loses precedence, while fairness
mechanisms prevent starvation of normal work.

Key characteristics:
  - Two permanent queues: priority items never demote to normal
  - Built-in skip-based linear backoff for retry logic
  - Configurable fairness ratio prevents monopolization
  - Shared deduplication across both queues
  - Unified state tracking for all items
  - Dispatcher starts on the first Process() call and runs until stopped
  - Multiple Process() calls can share the same queue
  - Min-wait throttle: the same key is never dispatched twice within
    MinWait (default 30s) - tune with SetMinWait

Skip-based backoff:
  - Items can be enqueued with a skip count
  - When an item with skipCount > 0 reaches the front, it's decremented and re-enqueued
  - Provides linear backoff without blocking the queue
  - Perfect for connection retry logic with increasing delays

Example usage:

	// Create and start queue with 2:1 fairness (process 2 priority per 1 normal)
	pq := utils.NewPriorityQueue[string, ConnWork](
		10,   // Priority queue size
		100,  // Normal queue size
		2,    // Fairness ratio
	)
	defer pq.Stop()  // Stop queue when done

	// Start workers with fair processing
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	results := pq.Process(ctx, 5, func(ctx context.Context, nodeID string, work ConnWork) error {
		// Priority items attempted first, but fairness ensures normal items run too
		return connectToNode(ctx, nodeID, work)
	})

	// Enqueue work - priority is permanent
	if node.Critical {
		pq.TryEnqueue(node.ID, work, true)   // Always stays priority
	} else {
		pq.TryEnqueue(node.ID, work, false)  // Always stays normal
	}

	// Enqueue with backoff after failure
	if connectionFailed {
		pq.TryEnqueue(node.ID, work, isPriority, node.ConsecutiveFailures)  // Linear backoff
	}

	// Consume results
	for result := range results {
		// Handle result
	}

When to use PriorityQueue vs Queue:
  - Use PriorityQueue when some items must always have precedence
  - Use PriorityQueue when priority items may fail repeatedly but shouldn't lose status
  - Use PriorityQueue when you need guaranteed fairness between priority levels
  - Use PriorityQueue when you need built-in retry backoff logic
  - Use regular Queue for simple FIFO with deduplication
  - Use regular Queue when all work has equal priority

Logging goes through a KEV-driven logger: set PRIORITY_QUEUE_LOG_LEVEL
(or LOG_LEVEL) to debug/trace to watch the dispatcher work.

Lifecycle:
  - The dispatcher starts on the first Process() call - enqueued items
    sit untouched in their queues until there are workers
  - Multiple Process() calls share the same dispatcher
  - Stop() must be called to clean up resources
*/
package utils

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// PriorityQueueStats provides detailed statistics for both queues
type PriorityQueueStats struct {
	// Queue sizes
	PriorityQueueSize int
	NormalQueueSize   int

	// State counts
	PriorityPending int
	NormalPending   int
	InFlight        int
	Completed       int
	Failed          int

	// Totals
	TotalEnqueued  int64
	TotalProcessed int64
	TotalFailed    int64

	// Fairness metrics
	PriorityProcessed int64
	NormalProcessed   int64
	FairnessRatio     int
}

// PriorityQueue manages two permanent queues with fair processing
type PriorityQueue[K comparable, V any] struct {
	// Synchronization
	mu sync.RWMutex

	// Dual input queues - items never move between them
	priorityQueue chan priorityQueueItem[K, V]
	normalQueue   chan priorityQueueItem[K, V]

	// Single output channel for workers (fed by dispatcher)
	workChannel chan priorityWorkItem[K, V]

	// Shared state across both queues
	state      map[K]QueueState
	isPriority map[K]bool      // Track which queue an item belongs to
	doneAt     map[K]time.Time // terminal (completed/failed) timestamps, for pruning

	// Min-wait protection to prevent rapid cycling
	lastSentToWorker map[K]time.Time // Track when item was last sent to workers
	minWait          time.Duration   // Minimum wait between sending same item to workers

	// Fairness control
	fairnessRatio int // e.g., 2 = process 2 priority per 1 normal

	// Configuration
	maxCompleted int
	log          *logOps

	// Runtime stats
	totalEnqueued     atomic.Int64
	totalProcessed    atomic.Int64
	totalFailed       atomic.Int64
	priorityProcessed atomic.Int64
	normalProcessed   atomic.Int64

	// Dispatcher lifecycle - starts lazily on first Process() so
	// enqueued items sit untouched in their queues until there are workers
	running          atomic.Bool
	dispatchOnce     sync.Once
	dispatcherCtx    context.Context
	dispatcherCancel context.CancelFunc
	dispatcherDone   chan struct{}
}

// priorityQueueItem represents an item in the priority queue with optional skip count
type priorityQueueItem[K comparable, V any] struct {
	Key        K
	Value      V
	SkipCount  int // How many times to skip before processing (0 = process immediately)
	EnqueuedAt time.Time
}

// priorityWorkItem includes metadata about which queue it came from
type priorityWorkItem[K comparable, V any] struct {
	priorityQueueItem[K, V]
	fromPriority bool
}

// NewPriorityQueue creates and starts a new priority queue with specified sizes and fairness ratio
func NewPriorityQueue[K comparable, V any](prioritySize, normalSize, fairnessRatio int) *PriorityQueue[K, V] {
	Assert(prioritySize > 0 && normalSize > 0, "queue sizes must be positive:", prioritySize, normalSize)
	if fairnessRatio < 1 {
		fairnessRatio = 1 // Minimum 1:1 fairness
	}

	// Work channel size should accommodate both queues
	workChannelSize := (prioritySize + normalSize) / 2
	if workChannelSize < 10 {
		workChannelSize = 10
	}

	pq := &PriorityQueue[K, V]{
		priorityQueue:    make(chan priorityQueueItem[K, V], prioritySize),
		normalQueue:      make(chan priorityQueueItem[K, V], normalSize),
		workChannel:      make(chan priorityWorkItem[K, V], workChannelSize),
		state:            make(map[K]QueueState),
		isPriority:       make(map[K]bool),
		doneAt:           make(map[K]time.Time),
		lastSentToWorker: make(map[K]time.Time),
		minWait:          30 * time.Second, // Default 30 seconds min-wait
		fairnessRatio:    fairnessRatio,
		maxCompleted:     1000, // Default
		dispatcherDone:   make(chan struct{}),
		log:              NewLogger("PRIORITY_QUEUE"),
	}

	pq.dispatcherCtx, pq.dispatcherCancel = context.WithCancel(context.Background())
	pq.running.Store(true)

	return pq
}

// SetMinWait configures the minimum wait time between processing attempts for the same item
func (pq *PriorityQueue[K, V]) SetMinWait(duration time.Duration) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.minWait = duration
}

// Stop gracefully shuts down the queue and dispatcher
func (pq *PriorityQueue[K, V]) Stop() {
	if !pq.running.CompareAndSwap(true, false) {
		return // Already stopped
	}

	pq.log.Debug("PriorityQueue: stopping dispatcher")

	// Stop dispatcher. If it never started (no Process() call), the Once
	// closes dispatcherDone so the wait below returns immediately.
	pq.dispatcherCancel()
	pq.dispatchOnce.Do(func() { close(pq.dispatcherDone) })
	<-pq.dispatcherDone // Wait for dispatcher to finish

	// Close work channel to signal any remaining workers
	close(pq.workChannel)

	pq.log.Debug("PriorityQueue: stopped")
}

// TryEnqueue attempts to enqueue work to the appropriate queue based on priority
// Optional skipCount parameter specifies how many times to skip before processing (defaults to 0)
func (pq *PriorityQueue[K, V]) TryEnqueue(key K, value V, isPriority bool, skipCount ...int) EnqueueResult {
	skip := 0
	if len(skipCount) > 0 {
		skip = skipCount[0]
	}
	if !pq.running.Load() {
		return QueueFull // Queue is stopped
	}

	pq.mu.Lock()
	defer pq.mu.Unlock()

	// Check if already pending or in-flight (dedup across both queues)
	if state, exists := pq.state[key]; exists && (state == QueueStatePending || state == QueueStateInFlight) {
		pq.log.Debug("PriorityQueue: item already", state, ":", key)
		return AlreadyQueued
	}

	// Mark as pending and remember which queue
	oldState, hadState := pq.state[key]
	pq.state[key] = QueueStatePending
	pq.isPriority[key] = isPriority

	// Try non-blocking send to appropriate queue
	item := priorityQueueItem[K, V]{Key: key, Value: value, SkipCount: skip, EnqueuedAt: time.Now()}

	targetQueue := pq.normalQueue
	queueName := "normal"
	if isPriority {
		targetQueue = pq.priorityQueue
		queueName = "priority"
	}

	select {
	case targetQueue <- item:
		pq.totalEnqueued.Add(1)
		pq.log.Debug("PriorityQueue: enqueued item to", queueName, "queue:", key)
		return Enqueued
	default:
		// Queue full - rollback
		if hadState {
			pq.state[key] = oldState
		} else {
			delete(pq.state, key)
			delete(pq.isPriority, key)
		}
		pq.log.Debug("PriorityQueue:", queueName, "queue full, cannot enqueue:", key)
		return QueueFull
	}
}

// MustEnqueue enqueues work, blocking if the appropriate queue is full
func (pq *PriorityQueue[K, V]) MustEnqueue(ctx context.Context, key K, value V, isPriority bool) error {
	if !pq.running.Load() {
		return fmt.Errorf("queue is stopped")
	}

	pq.mu.Lock()

	// Check if already pending or in-flight
	if state, exists := pq.state[key]; exists && (state == QueueStatePending || state == QueueStateInFlight) {
		pq.mu.Unlock()
		pq.log.Debug("PriorityQueue: item already", state, ", skipping:", key)
		return nil // Already queued, not an error
	}

	// Mark as pending
	oldState, hadState := pq.state[key]
	pq.state[key] = QueueStatePending
	pq.isPriority[key] = isPriority
	pq.mu.Unlock()

	// Select appropriate queue
	targetQueue := pq.normalQueue
	if isPriority {
		targetQueue = pq.priorityQueue
	}

	// Blocking send
	select {
	case targetQueue <- priorityQueueItem[K, V]{Key: key, Value: value, SkipCount: 0, EnqueuedAt: time.Now()}:
		pq.totalEnqueued.Add(1)
		return nil
	case <-ctx.Done():
		// Rollback on context cancellation
		pq.mu.Lock()
		if hadState {
			pq.state[key] = oldState
		} else {
			delete(pq.state, key)
			delete(pq.isPriority, key)
		}
		pq.mu.Unlock()
		return ctx.Err()
	}
}

// Process starts workers that process items from the work channel.
// The first call also starts the shared dispatcher.
func (pq *PriorityQueue[K, V]) Process(ctx context.Context, workers int, handler Handler[K, V]) <-chan Result[K] {
	Assert(pq.running.Load(), "cannot process on stopped queue")

	pq.dispatchOnce.Do(func() { go pq.dispatcher() })

	results := make(chan Result[K], workers)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			pq.worker(ctx, handler, results, workerID)
		}(i)
	}

	// Cleanup goroutine - waits for workers to finish
	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

// reEnqueue puts an item back at the end of the queue it came from.
// If that queue is meanwhile full the item is dropped - state is cleared
// so the key can be enqueued again instead of staying Pending forever.
func (pq *PriorityQueue[K, V]) reEnqueue(item priorityQueueItem[K, V], fromPriority bool, reason string) {
	targetQueue := pq.normalQueue
	queueName := "normal"
	if fromPriority {
		targetQueue = pq.priorityQueue
		queueName = "priority"
	}

	select {
	case targetQueue <- item:
		pq.log.Trace("PriorityQueue: re-enqueued", queueName, "item", item.Key, "-", reason)
	default:
		pq.mu.Lock()
		delete(pq.state, item.Key)
		delete(pq.isPriority, item.Key)
		pq.mu.Unlock()
		pq.log.Error("PriorityQueue: dropped", queueName, "item", item.Key, "-", queueName, "queue full while re-enqueueing for", reason)
	}
}

// reEnqueueTick paces the dispatcher when a full lap produced only
// re-enqueues (skips / min-wait): without it the loop spins at 100% CPU
// and burns skip counts in microseconds on an otherwise idle queue.
const reEnqueueTick = 25 * time.Millisecond

// dispatcher implements the fair scheduling algorithm.
// Only items actually sent to workers count toward fairness -
// skipped and min-waiting items don't.
func (pq *PriorityQueue[K, V]) dispatcher() {
	defer close(pq.dispatcherDone)

	pq.log.Debug("PriorityQueue: dispatcher started with fairness ratio", pq.fairnessRatio, "to 1")

	priorityTaken := 0
	normalTaken := 0
	lapReEnqueues := 0

	pace := func() {
		lapReEnqueues++
		if lapReEnqueues >= len(pq.priorityQueue)+len(pq.normalQueue) {
			time.Sleep(reEnqueueTick)
			lapReEnqueues = 0
		}
	}

	for {
		// Items cycling through skip/min-wait re-enqueues keep the loop busy
		// without ever reaching the blocking select below - check for
		// cancellation explicitly so Stop() never has to wait for them
		select {
		case <-pq.dispatcherCtx.Done():
			pq.log.Debug("PriorityQueue: dispatcher stopped")
			return
		default:
		}

		// After fairnessRatio priority items with no normal item, normal goes first.
		// A blocking select picks RANDOMLY among ready cases, so real preference
		// needs a non-blocking grab from the preferred queue first.
		preferNormal := priorityTaken >= pq.fairnessRatio && normalTaken == 0

		var item priorityQueueItem[K, V]
		var fromPriority bool

		preferred, other := pq.priorityQueue, pq.normalQueue
		if preferNormal {
			preferred, other = pq.normalQueue, pq.priorityQueue
		}

		select {
		case item = <-preferred:
			fromPriority = !preferNormal
		default:
			select {
			case item = <-preferred:
				fromPriority = !preferNormal
			case item = <-other:
				fromPriority = preferNormal
			case <-pq.dispatcherCtx.Done():
				pq.log.Debug("PriorityQueue: dispatcher stopped")
				return
			}
		}

		queueName := "normal"
		if fromPriority {
			queueName = "priority"
		}
		pq.log.Trace("PriorityQueue: dispatching", queueName, "item", item.Key, "- skips:", item.SkipCount)

		// Skip-based backoff: decrement and send to the back of its queue
		if item.SkipCount > 0 {
			item.SkipCount--
			pq.reEnqueue(item, fromPriority, "skip backoff")
			pace()
			continue
		}

		// Min-wait: don't hammer the same key
		pq.mu.RLock()
		lastSent, hasLastSent := pq.lastSentToWorker[item.Key]
		minWait := pq.minWait
		pq.mu.RUnlock()

		if hasLastSent {
			remaining := minWait - time.Since(lastSent)
			if remaining > 0 {
				pq.log.Debug("PriorityQueue: min-wait active for", item.Key, "-", remaining.Round(time.Second), "remaining")
				pq.reEnqueue(item, fromPriority, "min-wait")
				pace()
				continue
			}
		}

		pq.mu.Lock()
		pq.lastSentToWorker[item.Key] = time.Now()
		pq.mu.Unlock()

		select {
		case pq.workChannel <- priorityWorkItem[K, V]{priorityQueueItem: item, fromPriority: fromPriority}:
			lapReEnqueues = 0
			if fromPriority {
				priorityTaken++
				pq.priorityProcessed.Add(1)
			} else {
				normalTaken++
				pq.normalProcessed.Add(1)
			}
			pq.log.Debug("PriorityQueue: sent", queueName, "item", item.Key, "to workers - fairness P:", priorityTaken, "N:", normalTaken)

			if priorityTaken >= pq.fairnessRatio && normalTaken >= 1 {
				priorityTaken = 0
				normalTaken = 0
			}
		case <-pq.dispatcherCtx.Done():
			return
		}
	}
}

// worker processes items from the work channel
func (pq *PriorityQueue[K, V]) worker(ctx context.Context, handler Handler[K, V], results chan<- Result[K], workerID int) {
	pq.log.Debug("PriorityQueue: worker", workerID, "started")

	for {
		select {
		case item, ok := <-pq.workChannel:
			if !ok {
				pq.log.Debug("PriorityQueue: worker", workerID, "stopped (queue stopped)")
				return
			}
			pq.processItem(ctx, item, handler, results)

		case <-ctx.Done():
			pq.log.Debug("PriorityQueue: worker", workerID, "stopped (context done)")
			return
		}
	}
}

// processItem processes a single item
func (pq *PriorityQueue[K, V]) processItem(ctx context.Context, item priorityWorkItem[K, V], handler Handler[K, V], results chan<- Result[K]) {
	waitTime := time.Since(item.EnqueuedAt)

	// Mark as in-flight
	pq.mu.Lock()
	pq.state[item.Key] = QueueStateInFlight
	pq.mu.Unlock()

	// Process the work
	start := time.Now()
	err := handler(ctx, item.Key, item.Value)
	duration := time.Since(start)

	// Update state based on result - both outcomes are terminal and
	// tracked in doneAt so pruning bounds memory for failures too
	pq.mu.Lock()
	if err == nil {
		pq.state[item.Key] = QueueStateCompleted
		pq.totalProcessed.Add(1)
	} else {
		pq.state[item.Key] = QueueStateFailed
		pq.totalFailed.Add(1)
	}
	pq.doneAt[item.Key] = time.Now()
	pruneDone(pq.doneAt, pq.maxCompleted, func(key K) {
		delete(pq.state, key)
		delete(pq.isPriority, key)
		delete(pq.doneAt, key)
		delete(pq.lastSentToWorker, key)
	})
	pq.mu.Unlock()

	queueType := "normal"
	if item.fromPriority {
		queueType = "priority"
	}
	if err != nil {
		pq.log.Error("PriorityQueue:", queueType, "item failed:", item.Key, "wait:", waitTime, "duration:", duration, "error:", err)
	} else {
		pq.log.Debug("PriorityQueue:", queueType, "item completed:", item.Key, "wait:", waitTime, "duration:", duration)
	}

	// Send result
	select {
	case results <- Result[K]{Key: item.Key, Error: err, Duration: duration}:
	case <-ctx.Done():
		return
	}
}

// GetState returns the current state of a queued item
func (pq *PriorityQueue[K, V]) GetState(key K) (QueueState, bool) {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	state, exists := pq.state[key]
	return state, exists
}

// IsPending returns true if the item is pending or in-flight in either queue
func (pq *PriorityQueue[K, V]) IsPending(key K) bool {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	state, exists := pq.state[key]
	return exists && (state == QueueStatePending || state == QueueStateInFlight)
}

// IsPriority returns whether an item is in the priority queue
func (pq *PriorityQueue[K, V]) IsPriority(key K) (bool, bool) {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	isPriority, exists := pq.isPriority[key]
	return isPriority, exists
}

// Stats returns current statistics for both queues
func (pq *PriorityQueue[K, V]) Stats() PriorityQueueStats {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	stats := PriorityQueueStats{
		PriorityQueueSize: len(pq.priorityQueue),
		NormalQueueSize:   len(pq.normalQueue),
		TotalEnqueued:     pq.totalEnqueued.Load(),
		TotalProcessed:    pq.totalProcessed.Load(),
		TotalFailed:       pq.totalFailed.Load(),
		PriorityProcessed: pq.priorityProcessed.Load(),
		NormalProcessed:   pq.normalProcessed.Load(),
		FairnessRatio:     pq.fairnessRatio,
	}

	// Count states per queue
	for key, state := range pq.state {
		isPriority := pq.isPriority[key]
		switch state {
		case QueueStatePending:
			if isPriority {
				stats.PriorityPending++
			} else {
				stats.NormalPending++
			}
		case QueueStateInFlight:
			stats.InFlight++
		case QueueStateCompleted:
			stats.Completed++
		case QueueStateFailed:
			stats.Failed++
		}
	}

	return stats
}

// Size returns the total number of items across both queues
func (pq *PriorityQueue[K, V]) Size() (priority int, normal int) {
	return len(pq.priorityQueue), len(pq.normalQueue)
}

// Clear removes all state tracking (doesn't drain the channels)
func (pq *PriorityQueue[K, V]) Clear() {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	pq.state = make(map[K]QueueState)
	pq.isPriority = make(map[K]bool)
	pq.doneAt = make(map[K]time.Time)

	pq.log.Debug("PriorityQueue: cleared all state tracking")
}
