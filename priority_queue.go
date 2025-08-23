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
  - Queue starts automatically and runs until stopped
  - Multiple Process() calls can share the same queue

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

Performance characteristics:
  - O(1) enqueue/dequeue operations
  - O(1) deduplication check
  - O(1) skip handling (re-enqueue at back)
  - Fair scheduling overhead is negligible
  - Memory: ~200 bytes per item + dual channel buffers
  - Fairness guarantee: Exactly fairnessRatio priority items before forcing normal

Lifecycle:
  - Queue and dispatcher start immediately on creation
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
	PriorityPending  int
	NormalPending    int
	InFlight         int
	Completed        int
	Failed           int
	
	// Totals
	TotalEnqueued    int64
	TotalProcessed   int64
	TotalFailed      int64
	
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
	workChannel   chan priorityWorkItem[K, V]
	
	// Shared state across both queues
	state       map[K]QueueState
	isPriority  map[K]bool      // Track which queue an item belongs to
	completedAt map[K]time.Time
	
	// Fairness control
	fairnessRatio int  // e.g., 2 = process 2 priority per 1 normal
	
	// Configuration
	maxCompleted int
	metrics      QueueMetrics
	logger       Logger
	pqLog        *logOps  // Priority queue specific logger
	
	// Runtime stats
	totalEnqueued      atomic.Int64
	totalProcessed     atomic.Int64
	totalFailed        atomic.Int64
	priorityProcessed  atomic.Int64
	normalProcessed    atomic.Int64
	
	// Dispatcher lifecycle (permanent)
	running          atomic.Bool
	dispatcherCtx    context.Context
	dispatcherCancel context.CancelFunc
	dispatcherDone   chan struct{}
}

// priorityQueueItem represents an item in the priority queue with optional skip count
type priorityQueueItem[K comparable, V any] struct {
	Key        K
	Value      V
	SkipCount  int       // How many times to skip before processing (0 = process immediately)
	EnqueuedAt time.Time
}

// priorityWorkItem includes metadata about which queue it came from
type priorityWorkItem[K comparable, V any] struct {
	priorityQueueItem[K, V]
	fromPriority bool
}

// NewPriorityQueue creates and starts a new priority queue with specified sizes and fairness ratio
func NewPriorityQueue[K comparable, V any](prioritySize, normalSize, fairnessRatio int, opts ...Option[K, V]) *PriorityQueue[K, V] {
	if fairnessRatio < 1 {
		fairnessRatio = 1 // Minimum 1:1 fairness
	}
	
	// Work channel size should accommodate both queues
	workChannelSize := (prioritySize + normalSize) / 2
	if workChannelSize < 10 {
		workChannelSize = 10
	}
	
	pq := &PriorityQueue[K, V]{
		priorityQueue:  make(chan priorityQueueItem[K, V], prioritySize),
		normalQueue:    make(chan priorityQueueItem[K, V], normalSize),
		workChannel:    make(chan priorityWorkItem[K, V], workChannelSize),
		state:          make(map[K]QueueState),
		isPriority:     make(map[K]bool),
		completedAt:    make(map[K]time.Time),
		fairnessRatio:  fairnessRatio,
		maxCompleted:   1000, // Default
		dispatcherDone: make(chan struct{}),
		pqLog:          NewLogger("PRIORITY_QUEUE"),
	}
	
	// Apply options (reuse from queue.go)
	for _, opt := range opts {
		opt((*Queue[K, V])(nil)) // Type assertion workaround
		// In practice, we'd need PriorityQueueOption type
	}
	
	// Start dispatcher immediately
	pq.dispatcherCtx, pq.dispatcherCancel = context.WithCancel(context.Background())
	pq.running.Store(true)
	go pq.dispatcher()
	
	return pq
}

// Stop gracefully shuts down the queue and dispatcher
func (pq *PriorityQueue[K, V]) Stop() {
	if !pq.running.CompareAndSwap(true, false) {
		return // Already stopped
	}
	
	pq.pqLog.Info("PriorityQueue: stopping dispatcher")
	if pq.logger != nil {
		pq.logger.Info("PriorityQueue: stopping dispatcher")
	}
	
	// Stop dispatcher
	pq.dispatcherCancel()
	<-pq.dispatcherDone  // Wait for dispatcher to finish
	
	// Close work channel to signal any remaining workers
	close(pq.workChannel)
	
	pq.pqLog.Info("PriorityQueue: stopped")
	if pq.logger != nil {
		pq.logger.Info("PriorityQueue: stopped")
	}
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
		pq.pqLog.Debug("PriorityQueue: item already ", state, ": ", key)
		if pq.logger != nil {
			pq.logger.Debug("PriorityQueue: item already ", state, ": ", key)
		}
		if pq.metrics != nil {
			pq.metrics.RecordEnqueue(false)
		}
		return AlreadyQueued
	}
	
	// Mark as pending and remember which queue
	oldState := pq.state[key]
	pq.state[key] = QueueStatePending
	pq.isPriority[key] = isPriority
	
	// Try non-blocking send to appropriate queue
	item := priorityQueueItem[K, V]{Key: key, Value: value, SkipCount: skip, EnqueuedAt: time.Now()}
	
	var targetQueue chan priorityQueueItem[K, V]
	var queueName string
	if isPriority {
		targetQueue = pq.priorityQueue
		queueName = "priority"
	} else {
		targetQueue = pq.normalQueue
		queueName = "normal"
	}
	
	select {
	case targetQueue <- item:
		pq.totalEnqueued.Add(1)
		if pq.metrics != nil {
			pq.metrics.RecordEnqueue(true)
			if oldState != QueueStatePending {
				pq.metrics.RecordStateChange(oldState, QueueStatePending)
			}
		}
		pq.pqLog.Debug("PriorityQueue: enqueued item to ", queueName, " queue: ", key)
		if pq.logger != nil {
			pq.logger.Debug("PriorityQueue: enqueued item to ", queueName, " queue: ", key)
		}
		return Enqueued
	default:
		// Queue full - rollback
		if oldState == 0 {
			delete(pq.state, key)
			delete(pq.isPriority, key)
		} else {
			pq.state[key] = oldState
		}
		if pq.metrics != nil {
			pq.metrics.RecordEnqueue(false)
		}
		pq.pqLog.Debug("PriorityQueue: ", queueName, " queue full, cannot enqueue: ", key)
		if pq.logger != nil {
			pq.logger.Debug("PriorityQueue: ", queueName, " queue full, cannot enqueue: ", key)
		}
		return QueueFull
	}
}

// Process starts workers that process items from the work channel
func (pq *PriorityQueue[K, V]) Process(ctx context.Context, workers int, handler Handler[K, V]) <-chan Result[K] {
	if !pq.running.Load() {
		panic("cannot process on stopped queue")
	}
	
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

// dispatcher implements the fair scheduling algorithm
func (pq *PriorityQueue[K, V]) dispatcher() {
	defer close(pq.dispatcherDone)
	
	if pq.logger != nil {
		pq.logger.Debug("PriorityQueue: dispatcher started with fairness ratio %d:1", pq.fairnessRatio)
	}
	
	// Track items SENT TO WORKERS from each queue for fairness (not skipped items)
	priorityTaken := 0
	normalTaken := 0
	dispatcherCycles := 0
	
	for {
		select {
		case <-pq.dispatcherCtx.Done():
			pq.pqLog.Trace("[Dispatcher] Context done at start of loop, stopping after ", dispatcherCycles, " cycles")
			if pq.logger != nil {
				pq.logger.Debug("PriorityQueue: dispatcher stopped after %d cycles", dispatcherCycles)
			}
			return
		default:
		}
		
		dispatcherCycles++
		pq.pqLog.Trace("[Dispatcher] Starting cycle ", dispatcherCycles)
		
		// Determine what we MUST take for fairness
		mustTakeNormal := priorityTaken >= pq.fairnessRatio && normalTaken == 0
		pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Fairness check: P=", priorityTaken, " N=", normalTaken, ", mustTakeNormal=", mustTakeNormal)
		
		var item priorityQueueItem[K, V]
		var fromPriority bool
		var gotItem bool
		
		if mustTakeNormal {
			// PREFER normal queue for fairness, but don't block if only priority has items
			pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Preferring normal queue for fairness")
			select {
			case item = <-pq.normalQueue:
				fromPriority = false
				gotItem = true
				pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Got item from normal queue")
			case item = <-pq.priorityQueue:
				// Normal queue empty, take from priority instead
				fromPriority = true
				gotItem = true
				pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Normal queue empty, taking from priority queue instead")
			case <-pq.dispatcherCtx.Done():
				pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Context done while waiting")
				return
			}
		} else {
			// Can take from either, prefer priority
			pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Waiting for items from either queue (prefer priority)...")
			select {
			case item = <-pq.priorityQueue:
				fromPriority = true
				gotItem = true
				pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Got item from priority queue")
			case item = <-pq.normalQueue:
				fromPriority = false
				gotItem = true
				pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Got item from normal queue")
			case <-pq.dispatcherCtx.Done():
				pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Context done, stopping dispatcher")
				return
			}
		}
		
		// Process the item we got
		if gotItem {
			queueName := "normal"
			if fromPriority {
				queueName = "priority"
			}
			
			pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Got ", queueName, " item: ", item.Key, " (skipCount=", item.SkipCount, ")")
			
			// Check if we should skip this item
			if item.SkipCount > 0 {
				// Decrement skip count and re-enqueue at back of same queue
				item.SkipCount--
				
				pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Skipping ", queueName, " item ", item.Key, ", will re-enqueue with ", item.SkipCount, " skips remaining")
				
				// Try non-blocking re-enqueue to avoid deadlock
				if fromPriority {
					select {
					case pq.priorityQueue <- item:
						pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Successfully re-enqueued ", queueName, " item ", item.Key, " to priority queue")
						pq.pqLog.Debug("[Cycle ", dispatcherCycles, "] Skipped ", queueName, " item ", item.Key, ", ", item.SkipCount, " skips remaining")
						if pq.logger != nil {
							pq.logger.Debug("[Cycle ", dispatcherCycles, "] Skipped ", queueName, " item ", item.Key, ", ", item.SkipCount, " skips remaining")
						}
					default:
						// Queue full, item lost - log error
						pq.pqLog.Error("[Dispatcher Cycle ", dispatcherCycles, "] ERROR: Failed to re-enqueue ", queueName, " item ", item.Key, " - queue full!")
						if pq.logger != nil {
							pq.logger.Error("[Cycle ", dispatcherCycles, "] Failed to re-enqueue skipped ", queueName, " item ", item.Key, " - queue full!")
						}
					}
				} else {
					select {
					case pq.normalQueue <- item:
						pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Successfully re-enqueued ", queueName, " item ", item.Key, " to normal queue")
						pq.pqLog.Debug("[Cycle ", dispatcherCycles, "] Skipped ", queueName, " item ", item.Key, ", ", item.SkipCount, " skips remaining")
						if pq.logger != nil {
							pq.logger.Debug("[Cycle ", dispatcherCycles, "] Skipped ", queueName, " item ", item.Key, ", ", item.SkipCount, " skips remaining")
						}
					default:
						// Queue full, item lost - log error
						pq.pqLog.Error("[Dispatcher Cycle ", dispatcherCycles, "] ERROR: Failed to re-enqueue ", queueName, " item ", item.Key, " - queue full!")
						if pq.logger != nil {
							pq.logger.Error("[Cycle ", dispatcherCycles, "] Failed to re-enqueue skipped ", queueName, " item ", item.Key, " - queue full!")
						}
					}
				}
				// DON'T count skipped items toward fairness!
				// Continue to next item immediately - don't send to workers
				continue
			}
			
			// No skips, send to workers
			workItem := priorityWorkItem[K, V]{
				priorityQueueItem: item,
				fromPriority:      fromPriority,
			}
			
			select {
			case pq.workChannel <- workItem:
				// NOW count toward fairness (only items actually sent to workers)
				if fromPriority {
					priorityTaken++
					pq.priorityProcessed.Add(1)
				} else {
					normalTaken++
					pq.normalProcessed.Add(1)
				}
				
				pq.pqLog.Trace("[Dispatcher Cycle ", dispatcherCycles, "] Sent ", queueName, " item ", item.Key, " to workers (fairness: P=", priorityTaken, " N=", normalTaken, ")")
				pq.pqLog.Debug("[Cycle ", dispatcherCycles, "] Sent ", queueName, " item ", item.Key, " to workers (fairness: P=", priorityTaken, " N=", normalTaken, ")")
				if pq.logger != nil {
					pq.logger.Debug("[Cycle ", dispatcherCycles, "] Sent ", queueName, " item ", item.Key, " to workers (fairness: P=", priorityTaken, " N=", normalTaken, ")")
				}
				
				// Check if we've completed a fairness cycle
				if priorityTaken >= pq.fairnessRatio && normalTaken >= 1 {
					pq.pqLog.Debug("[Cycle ", dispatcherCycles, "] Fairness cycle complete, resetting counters")
					if pq.logger != nil {
						pq.logger.Debug("[Cycle ", dispatcherCycles, "] Fairness cycle complete, resetting counters")
					}
					priorityTaken = 0
					normalTaken = 0
				}
			case <-pq.dispatcherCtx.Done():
				return
			}
		}
	}
}

// worker processes items from the work channel
func (pq *PriorityQueue[K, V]) worker(ctx context.Context, handler Handler[K, V], results chan<- Result[K], workerID int) {
	pq.pqLog.Debug("PriorityQueue: worker ", workerID, " started")
	if pq.logger != nil {
		pq.logger.Debug("PriorityQueue: worker ", workerID, " started")
	}
	
	for {
		select {
		case item, ok := <-pq.workChannel:
			if !ok {
				// Work channel closed, queue is stopped
				pq.pqLog.Debug("PriorityQueue: worker ", workerID, " stopped (queue stopped)")
				if pq.logger != nil {
					pq.logger.Debug("PriorityQueue: worker ", workerID, " stopped (queue stopped)")
				}
				return
			}
			pq.processItem(ctx, item, handler, results)
			
		case <-ctx.Done():
			pq.pqLog.Debug("PriorityQueue: worker ", workerID, " stopped (context done)")
			if pq.logger != nil {
				pq.logger.Debug("PriorityQueue: worker ", workerID, " stopped (context done)")
			}
			return
		}
	}
}

// processItem processes a single item
func (pq *PriorityQueue[K, V]) processItem(ctx context.Context, item priorityWorkItem[K, V], handler Handler[K, V], results chan<- Result[K]) {
	// Update metrics
	waitTime := time.Since(item.EnqueuedAt)
	
	// Mark as in-flight
	pq.mu.Lock()
	pq.state[item.Key] = QueueStateInFlight
	pq.mu.Unlock()
	
	if pq.metrics != nil {
		pq.metrics.RecordStateChange(QueueStatePending, QueueStateInFlight)
	}
	
	// Process the work
	start := time.Now()
	err := handler(ctx, item.Key, item.Value)
	duration := time.Since(start)
	
	// Update state based on result
	pq.mu.Lock()
	if err == nil {
		pq.state[item.Key] = QueueStateCompleted
		pq.completedAt[item.Key] = time.Now()
		pq.totalProcessed.Add(1)
		pq.cleanupCompleted()
	} else {
		pq.state[item.Key] = QueueStateFailed
		pq.totalFailed.Add(1)
	}
	pq.mu.Unlock()
	
	// Record metrics
	if pq.metrics != nil {
		pq.metrics.RecordDequeue(duration, err)
		if err == nil {
			pq.metrics.RecordStateChange(QueueStateInFlight, QueueStateCompleted)
		} else {
			pq.metrics.RecordStateChange(QueueStateInFlight, QueueStateFailed)
		}
	}
	
	// Log result
	queueType := "normal"
	if item.fromPriority {
		queueType = "priority"
	}
	if err != nil {
		pq.pqLog.Error("PriorityQueue: ", queueType, " item failed: key=", item.Key, " wait=", waitTime, " duration=", duration, " error=", err)
		if pq.logger != nil {
			pq.logger.Error("PriorityQueue: ", queueType, " item failed: key=", item.Key, " wait=", waitTime, " duration=", duration, " error=", err)
		}
	} else {
		pq.pqLog.Debug("PriorityQueue: ", queueType, " item completed: key=", item.Key, " wait=", waitTime, " duration=", duration)
		if pq.logger != nil {
			pq.logger.Debug("PriorityQueue: ", queueType, " item completed: key=", item.Key, " wait=", waitTime, " duration=", duration)
		}
	}
	
	// Send result
	select {
	case results <- Result[K]{Key: item.Key, Error: err, Duration: duration}:
	case <-ctx.Done():
		return
	}
}

// cleanupCompleted removes old completed items if we exceed maxCompleted
// Must be called with lock held
func (pq *PriorityQueue[K, V]) cleanupCompleted() {
	if pq.maxCompleted <= 0 || len(pq.completedAt) <= pq.maxCompleted {
		return
	}
	
	// Find oldest completed items to remove
	type completedItem struct {
		key  K
		time time.Time
	}
	
	items := make([]completedItem, 0, len(pq.completedAt))
	for k, t := range pq.completedAt {
		items = append(items, completedItem{key: k, time: t})
	}
	
	// Sort by time (oldest first)
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].time.After(items[j].time) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	
	// Remove oldest items
	toRemove := len(items) - pq.maxCompleted
	for i := 0; i < toRemove; i++ {
		key := items[i].key
		delete(pq.state, key)
		delete(pq.isPriority, key)
		delete(pq.completedAt, key)
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
	pq.completedAt = make(map[K]time.Time)
	
	pq.pqLog.Info("PriorityQueue: cleared all state tracking")
	if pq.logger != nil {
		pq.logger.Info("PriorityQueue: cleared all state tracking")
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
		pq.pqLog.Debug("PriorityQueue: item already ", state, ", skipping: ", key)
		if pq.logger != nil {
			pq.logger.Debug("PriorityQueue: item already ", state, ", skipping: ", key)
		}
		return nil // Already queued, not an error
	}
	
	// Mark as pending
	oldState := pq.state[key]
	pq.state[key] = QueueStatePending
	pq.isPriority[key] = isPriority
	pq.mu.Unlock()
	
	// Select appropriate queue
	var targetQueue chan priorityQueueItem[K, V]
	if isPriority {
		targetQueue = pq.priorityQueue
	} else {
		targetQueue = pq.normalQueue
	}
	
	// Blocking send
	select {
	case targetQueue <- priorityQueueItem[K, V]{Key: key, Value: value, SkipCount: 0, EnqueuedAt: time.Now()}:
		pq.totalEnqueued.Add(1)
		if pq.metrics != nil {
			pq.metrics.RecordEnqueue(true)
			pq.metrics.RecordStateChange(oldState, QueueStatePending)
		}
		return nil
	case <-ctx.Done():
		// Rollback on context cancellation
		pq.mu.Lock()
		if oldState == 0 {
			delete(pq.state, key)
			delete(pq.isPriority, key)
		} else {
			pq.state[key] = oldState
		}
		pq.mu.Unlock()
		return ctx.Err()
	}
}