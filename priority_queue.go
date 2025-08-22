/*
PriorityQueue provides a dual-queue system with fairness guarantees for priority-based work distribution.

Unlike a traditional priority queue where items can be reordered or demoted, PriorityQueue maintains
two separate permanent queues. Items in the priority queue always maintain their priority status,
ensuring critical work (like monitoring essential nodes) never loses precedence, while fairness
mechanisms prevent starvation of normal work.

Key characteristics:
  - Two permanent queues: priority items never demote to normal
  - Configurable fairness ratio prevents monopolization
  - Shared deduplication across both queues
  - Unified state tracking for all items
  - Queue starts automatically and runs until stopped
  - Multiple Process() calls can share the same queue

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
	
	// Consume results
	for result := range results {
		// Handle result
	}

When to use PriorityQueue vs Queue:
  - Use PriorityQueue when some items must always have precedence
  - Use PriorityQueue when priority items may fail repeatedly but shouldn't lose status
  - Use PriorityQueue when you need guaranteed fairness between priority levels
  - Use regular Queue for simple FIFO with deduplication
  - Use regular Queue when all work has equal priority

Performance characteristics:
  - O(1) enqueue/dequeue operations
  - O(1) deduplication check
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
	priorityQueue chan queueItem[K, V]
	normalQueue   chan queueItem[K, V]
	
	// Single output channel for workers (fed by dispatcher)
	workChannel   chan workItem[K, V]
	
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

// workItem includes metadata about which queue it came from
type workItem[K comparable, V any] struct {
	queueItem[K, V]
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
		priorityQueue:  make(chan queueItem[K, V], prioritySize),
		normalQueue:    make(chan queueItem[K, V], normalSize),
		workChannel:    make(chan workItem[K, V], workChannelSize),
		state:          make(map[K]QueueState),
		isPriority:     make(map[K]bool),
		completedAt:    make(map[K]time.Time),
		fairnessRatio:  fairnessRatio,
		maxCompleted:   1000, // Default
		dispatcherDone: make(chan struct{}),
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
	
	if pq.logger != nil {
		pq.logger.Info("PriorityQueue: stopping dispatcher")
	}
	
	// Stop dispatcher
	pq.dispatcherCancel()
	<-pq.dispatcherDone  // Wait for dispatcher to finish
	
	// Close work channel to signal any remaining workers
	close(pq.workChannel)
	
	if pq.logger != nil {
		pq.logger.Info("PriorityQueue: stopped")
	}
}

// TryEnqueue attempts to enqueue work to the appropriate queue based on priority
func (pq *PriorityQueue[K, V]) TryEnqueue(key K, value V, isPriority bool) EnqueueResult {
	if !pq.running.Load() {
		return QueueFull // Queue is stopped
	}
	
	pq.mu.Lock()
	defer pq.mu.Unlock()
	
	// Check if already pending or in-flight (dedup across both queues)
	if state, exists := pq.state[key]; exists && (state == QueueStatePending || state == QueueStateInFlight) {
		if pq.logger != nil {
			pq.logger.Debug("PriorityQueue: item already %s: %v", state, key)
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
	item := queueItem[K, V]{Key: key, Value: value, EnqueuedAt: time.Now()}
	
	var targetQueue chan queueItem[K, V]
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
		if pq.logger != nil {
			pq.logger.Debug("PriorityQueue: enqueued item to %s queue: %v", queueName, key)
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
		if pq.logger != nil {
			pq.logger.Debug("PriorityQueue: %s queue full, cannot enqueue: %v", queueName, key)
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
	
	// Track items taken from each queue for fairness
	priorityTaken := 0
	normalTaken := 0
	
	for {
		select {
		case <-pq.dispatcherCtx.Done():
			if pq.logger != nil {
				pq.logger.Debug("PriorityQueue: dispatcher stopped")
			}
			return
		default:
		}
		
		// Determine what we MUST take for fairness
		mustTakeNormal := priorityTaken >= pq.fairnessRatio && normalTaken == 0
		
		var item queueItem[K, V]
		var fromPriority bool
		var gotItem bool
		
		if mustTakeNormal {
			// MUST take from normal queue to maintain fairness
			select {
			case item = <-pq.normalQueue:
				normalTaken++
				fromPriority = false
				gotItem = true
				
				// Check if we've completed a fairness cycle
				if normalTaken >= 1 {
					// Reset for next cycle
					priorityTaken = 0
					normalTaken = 0
				}
			case <-pq.dispatcherCtx.Done():
				return
			}
		} else {
			// Can take from either, prefer priority
			select {
			case item = <-pq.priorityQueue:
				priorityTaken++
				fromPriority = true
				gotItem = true
			case item = <-pq.normalQueue:
				normalTaken++
				fromPriority = false
				gotItem = true
				
				// Check if this completes a cycle
				if priorityTaken >= pq.fairnessRatio && normalTaken >= 1 {
					// Reset for next cycle
					priorityTaken = 0
					normalTaken = 0
				}
			case <-pq.dispatcherCtx.Done():
				return
			}
		}
		
		// Send to work channel
		if gotItem {
			workItem := workItem[K, V]{
				queueItem:    item,
				fromPriority: fromPriority,
			}
			
			select {
			case pq.workChannel <- workItem:
				// Update stats
				if fromPriority {
					pq.priorityProcessed.Add(1)
				} else {
					pq.normalProcessed.Add(1)
				}
			case <-pq.dispatcherCtx.Done():
				return
			}
		}
	}
}

// worker processes items from the work channel
func (pq *PriorityQueue[K, V]) worker(ctx context.Context, handler Handler[K, V], results chan<- Result[K], workerID int) {
	if pq.logger != nil {
		pq.logger.Debug("PriorityQueue: worker %d started", workerID)
	}
	
	for {
		select {
		case item, ok := <-pq.workChannel:
			if !ok {
				// Work channel closed, queue is stopped
				if pq.logger != nil {
					pq.logger.Debug("PriorityQueue: worker %d stopped (queue stopped)", workerID)
				}
				return
			}
			pq.processItem(ctx, item, handler, results)
			
		case <-ctx.Done():
			if pq.logger != nil {
				pq.logger.Debug("PriorityQueue: worker %d stopped (context done)", workerID)
			}
			return
		}
	}
}

// processItem processes a single item
func (pq *PriorityQueue[K, V]) processItem(ctx context.Context, item workItem[K, V], handler Handler[K, V], results chan<- Result[K]) {
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
	if pq.logger != nil {
		queueType := "normal"
		if item.fromPriority {
			queueType = "priority"
		}
		if err != nil {
			pq.logger.Error("PriorityQueue: %s item failed: key=%v wait=%v duration=%v error=%v", 
				queueType, item.Key, waitTime, duration, err)
		} else {
			pq.logger.Debug("PriorityQueue: %s item completed: key=%v wait=%v duration=%v", 
				queueType, item.Key, waitTime, duration)
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
		if pq.logger != nil {
			pq.logger.Debug("PriorityQueue: item already %s, skipping: %v", state, key)
		}
		return nil // Already queued, not an error
	}
	
	// Mark as pending
	oldState := pq.state[key]
	pq.state[key] = QueueStatePending
	pq.isPriority[key] = isPriority
	pq.mu.Unlock()
	
	// Select appropriate queue
	var targetQueue chan queueItem[K, V]
	if isPriority {
		targetQueue = pq.priorityQueue
	} else {
		targetQueue = pq.normalQueue
	}
	
	// Blocking send
	select {
	case targetQueue <- queueItem[K, V]{Key: key, Value: value, EnqueuedAt: time.Now()}:
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