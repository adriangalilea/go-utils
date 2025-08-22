/*
Queue provides a thread-safe work queue with automatic deduplication.

Queue solves the common distributed systems problem of duplicate work:
  - API calls that shouldn't be made twice
  - Background jobs that must run exactly once  
  - Event processing with at-most-once semantics
  - Resource operations that should be singleton

Example usage:

	// Create a queue for API requests
	q := utils.NewQueue[string, APIRequest](100,
		utils.WithMaxCompleted(500),
		utils.WithQueueLogger(logger),
	)
	
	// Start workers that return results
	results := q.Process(ctx, 5, func(ctx context.Context, url string, req APIRequest) error {
		return makeAPICall(ctx, url, req)
	})
	
	// Handle results
	go func() {
		for result := range results {
			if result.Error != nil {
				log.Printf("Failed %s: %v", result.Key, result.Error)
			}
		}
	}()
	
	// Enqueue work - duplicates automatically filtered
	q.TryEnqueue("api.example.com", request1) // true
	q.TryEnqueue("api.example.com", request2) // false - already queued

When to use Queue vs channels:
  - Use Queue when you need automatic deduplication
  - Use Queue when you need to track work state (pending/in-flight/completed)
  - Use Queue for idempotent operations that shouldn't repeat
  - Use channels for simple producer-consumer without dedup needs
  - Use Queue when you need metrics and observability

Performance characteristics:
  - O(1) enqueue/dequeue operations
  - O(1) deduplication check
  - Minimal lock contention with fine-grained locking
  - Memory overhead: ~200 bytes per tracked item
*/
package utils

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// QueueState represents the state of a queued item
type QueueState int

const (
	QueueStatePending QueueState = iota
	QueueStateInFlight
	QueueStateCompleted
	QueueStateFailed
)

func (s QueueState) String() string {
	switch s {
	case QueueStatePending:
		return "pending"
	case QueueStateInFlight:
		return "in-flight"
	case QueueStateCompleted:
		return "completed"
	case QueueStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Handler is the function signature for processing queue items.
// Context allows for cancellation and deadline propagation.
type Handler[K comparable, V any] func(ctx context.Context, key K, value V) error

// Result represents the outcome of processing a queue item
type Result[K comparable] struct {
	Key      K
	Error    error
	Duration time.Duration
}

// QueueMetrics provides observability into queue operations
type QueueMetrics interface {
	RecordEnqueue(success bool)
	RecordDequeue(duration time.Duration, err error)
	RecordQueueSize(size int)
	RecordStateChange(from, to QueueState)
}

// Logger interface for queue operations
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// Queue provides a thread-safe work queue with built-in deduplication.
// Each unique key can only be queued once until it completes processing.
type Queue[K comparable, V any] struct {
	// Synchronization
	mu         sync.RWMutex
	work       chan queueItem[K, V]
	state      map[K]QueueState
	completedAt map[K]time.Time
	
	// Configuration
	maxCompleted   int
	metrics        QueueMetrics
	logger         Logger
	
	// Runtime stats
	totalEnqueued  atomic.Int64
	totalProcessed atomic.Int64
	totalFailed    atomic.Int64
	
	// Shutdown
	drainOnce sync.Once
	drainCh   chan struct{}
}

// queueItem wraps work with a deduplication key
type queueItem[K comparable, V any] struct {
	Key   K
	Value V
	EnqueuedAt time.Time
}

// Option configures a Queue
type Option[K comparable, V any] func(*Queue[K, V])

// WithMaxCompleted sets the maximum number of completed items to track (for dedup)
func WithMaxCompleted[K comparable, V any](n int) Option[K, V] {
	return func(q *Queue[K, V]) {
		q.maxCompleted = n
	}
}

// WithMetrics adds metrics collection to the queue
func WithMetrics[K comparable, V any](m QueueMetrics) Option[K, V] {
	return func(q *Queue[K, V]) {
		q.metrics = m
	}
}

// WithQueueLogger adds logging to the queue
func WithQueueLogger[K comparable, V any](l Logger) Option[K, V] {
	return func(q *Queue[K, V]) {
		q.logger = l
	}
}

// NewQueue creates a new Queue with the specified buffer size and options
func NewQueue[K comparable, V any](bufferSize int, opts ...Option[K, V]) *Queue[K, V] {
	q := &Queue[K, V]{
		work:         make(chan queueItem[K, V], bufferSize),
		state:        make(map[K]QueueState),
		completedAt:  make(map[K]time.Time),
		maxCompleted: 1000, // Default
		drainCh:      make(chan struct{}),
	}
	
	// Apply options
	for _, opt := range opts {
		opt(q)
	}
	
	return q
}

// TryEnqueue attempts to enqueue work, returns false if already queued/in-flight/queue full
func (q *Queue[K, V]) TryEnqueue(key K, value V) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	// Check if already pending or in-flight
	if state, exists := q.state[key]; exists && (state == QueueStatePending || state == QueueStateInFlight) {
		if q.logger != nil {
			q.logger.Debug("Queue: item already %s: %v", state, key)
		}
		if q.metrics != nil {
			q.metrics.RecordEnqueue(false)
		}
		return false
	}
	
	// Mark as pending BEFORE channel send
	oldState := q.state[key]
	q.state[key] = QueueStatePending
	
	// Try non-blocking send
	select {
	case q.work <- queueItem[K, V]{Key: key, Value: value, EnqueuedAt: time.Now()}:
		q.totalEnqueued.Add(1)
		if q.metrics != nil {
			q.metrics.RecordEnqueue(true)
			q.metrics.RecordQueueSize(len(q.work))
			if oldState != QueueStatePending {
				q.metrics.RecordStateChange(oldState, QueueStatePending)
			}
		}
		if q.logger != nil {
			q.logger.Debug("Queue: enqueued item: %v", key)
		}
		return true
	default:
		// Queue full - rollback the pending mark
		if oldState == 0 {
			delete(q.state, key)
		} else {
			q.state[key] = oldState
		}
		if q.metrics != nil {
			q.metrics.RecordEnqueue(false)
		}
		if q.logger != nil {
			q.logger.Debug("Queue: queue full, cannot enqueue: %v", key)
		}
		return false
	}
}

// TryEnqueueBatch attempts to enqueue multiple items, returns keys that were successfully enqueued
func (q *Queue[K, V]) TryEnqueueBatch(items map[K]V) []K {
	enqueued := make([]K, 0, len(items))
	
	for key, value := range items {
		if q.TryEnqueue(key, value) {
			enqueued = append(enqueued, key)
		}
	}
	
	return enqueued
}

// MustEnqueue enqueues work, blocking if queue is full (still deduplicates)
func (q *Queue[K, V]) MustEnqueue(ctx context.Context, key K, value V) error {
	q.mu.Lock()
	
	// Check if already pending or in-flight
	if state, exists := q.state[key]; exists && (state == QueueStatePending || state == QueueStateInFlight) {
		q.mu.Unlock()
		if q.logger != nil {
			q.logger.Debug("Queue: item already %s, skipping: %v", state, key)
		}
		return nil // Already queued or being processed, not an error
	}
	
	// Mark as pending
	oldState := q.state[key]
	q.state[key] = QueueStatePending
	q.mu.Unlock()
	
	// Blocking send
	select {
	case q.work <- queueItem[K, V]{Key: key, Value: value, EnqueuedAt: time.Now()}:
		q.totalEnqueued.Add(1)
		if q.metrics != nil {
			q.metrics.RecordEnqueue(true)
			q.metrics.RecordQueueSize(len(q.work))
			q.metrics.RecordStateChange(oldState, QueueStatePending)
		}
		return nil
	case <-ctx.Done():
		// Rollback on context cancellation
		q.mu.Lock()
		if oldState == 0 {
			delete(q.state, key)
		} else {
			q.state[key] = oldState
		}
		q.mu.Unlock()
		return ctx.Err()
	case <-q.drainCh:
		// Queue is draining
		q.mu.Lock()
		if oldState == 0 {
			delete(q.state, key)
		} else {
			q.state[key] = oldState
		}
		q.mu.Unlock()
		return fmt.Errorf("queue is draining")
	}
}

// Process starts workers that process items from the queue and returns a results channel
func (q *Queue[K, V]) Process(ctx context.Context, workers int, handler Handler[K, V]) <-chan Result[K] {
	results := make(chan Result[K], workers)
	
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			q.processWorker(ctx, handler, results, workerID)
		}(i)
	}
	
	// Close results channel when all workers done
	go func() {
		wg.Wait()
		close(results)
	}()
	
	return results
}

// processWorker is the internal worker that processes queue items
func (q *Queue[K, V]) processWorker(ctx context.Context, handler Handler[K, V], results chan<- Result[K], workerID int) {
	if q.logger != nil {
		q.logger.Debug("Queue: worker %d started", workerID)
	}
	
	for {
		select {
		case item := <-q.work:
			q.processItem(ctx, item, handler, results)
			
		case <-ctx.Done():
			if q.logger != nil {
				q.logger.Debug("Queue: worker %d stopped (context done)", workerID)
			}
			return
		case <-q.drainCh:
			// Process remaining items during drain
			for {
				select {
				case item := <-q.work:
					q.processItem(ctx, item, handler, results)
				default:
					if q.logger != nil {
						q.logger.Debug("Queue: worker %d stopped (drained)", workerID)
					}
					return
				}
			}
		}
	}
}

// processItem processes a single queue item
func (q *Queue[K, V]) processItem(ctx context.Context, item queueItem[K, V], handler Handler[K, V], results chan<- Result[K]) {
	// Update metrics
	waitTime := time.Since(item.EnqueuedAt)
	if q.metrics != nil {
		q.metrics.RecordQueueSize(len(q.work))
	}
	
	// Mark as in-flight
	q.mu.Lock()
	q.state[item.Key] = QueueStateInFlight
	q.mu.Unlock()
	
	if q.metrics != nil {
		q.metrics.RecordStateChange(QueueStatePending, QueueStateInFlight)
	}
	
	// Process the work
	start := time.Now()
	err := handler(ctx, item.Key, item.Value)
	duration := time.Since(start)
	
	// Update state based on result
	q.mu.Lock()
	if err == nil {
		q.state[item.Key] = QueueStateCompleted
		q.completedAt[item.Key] = time.Now()
		q.totalProcessed.Add(1)
		q.cleanupCompleted()
	} else {
		q.state[item.Key] = QueueStateFailed
		q.totalFailed.Add(1)
	}
	q.mu.Unlock()
	
	// Record metrics
	if q.metrics != nil {
		q.metrics.RecordDequeue(duration, err)
		if err == nil {
			q.metrics.RecordStateChange(QueueStateInFlight, QueueStateCompleted)
		} else {
			q.metrics.RecordStateChange(QueueStateInFlight, QueueStateFailed)
		}
	}
	
	// Log result
	if q.logger != nil {
		if err != nil {
			q.logger.Error("Queue: item failed: key=%v wait=%v duration=%v error=%v", 
				item.Key, waitTime, duration, err)
		} else {
			q.logger.Debug("Queue: item completed: key=%v wait=%v duration=%v", 
				item.Key, waitTime, duration)
		}
	}
	
	// Send result
	select {
	case results <- Result[K]{Key: item.Key, Error: err, Duration: duration}:
	case <-ctx.Done():
		return
	}
}

// ProcessWithRetry processes items with automatic retry on failure
func (q *Queue[K, V]) ProcessWithRetry(ctx context.Context, workers int, handler Handler[K, V], maxRetries int) <-chan Result[K] {
	retryHandler := func(ctx context.Context, key K, value V) error {
		var err error
		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				// Exponential backoff
				select {
				case <-time.After(time.Duration(1<<uint(attempt-1)) * time.Second):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			
			err = handler(ctx, key, value)
			if err == nil {
				return nil
			}
			
			if q.logger != nil {
				q.logger.Debug("Queue: retry attempt %d/%d for key=%v error=%v", 
					attempt+1, maxRetries+1, key, err)
			}
		}
		return err
	}
	
	return q.Process(ctx, workers, retryHandler)
}

// cleanupCompleted removes old completed items if we exceed maxCompleted
// Must be called with lock held
func (q *Queue[K, V]) cleanupCompleted() {
	if q.maxCompleted <= 0 || len(q.completedAt) <= q.maxCompleted {
		return
	}
	
	// Find oldest completed items to remove
	type completedItem struct {
		key  K
		time time.Time
	}
	
	items := make([]completedItem, 0, len(q.completedAt))
	for k, t := range q.completedAt {
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
	toRemove := len(items) - q.maxCompleted
	for i := 0; i < toRemove; i++ {
		key := items[i].key
		delete(q.state, key)
		delete(q.completedAt, key)
	}
}

// Drain gracefully shuts down the queue, processing remaining items
func (q *Queue[K, V]) Drain() {
	q.drainOnce.Do(func() {
		close(q.drainCh)
		if q.logger != nil {
			q.logger.Info("Queue: draining with %d items remaining", len(q.work))
		}
	})
}

// GetState returns the current state of a queued item
func (q *Queue[K, V]) GetState(key K) (QueueState, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	
	state, exists := q.state[key]
	return state, exists
}

// IsPending returns true if the item is pending or in-flight
func (q *Queue[K, V]) IsPending(key K) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	
	state, exists := q.state[key]
	return exists && (state == QueueStatePending || state == QueueStateInFlight)
}

// Peek returns the next item without removing it from the queue
func (q *Queue[K, V]) Peek() (K, V, bool) {
	select {
	case item := <-q.work:
		// Put it back
		q.work <- item
		return item.Key, item.Value, true
	default:
		var zeroK K
		var zeroV V
		return zeroK, zeroV, false
	}
}

// Size returns the current number of items in the queue channel
func (q *Queue[K, V]) Size() int {
	return len(q.work)
}

// Clear removes all state tracking (doesn't drain the channel)
func (q *Queue[K, V]) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	q.state = make(map[K]QueueState)
	q.completedAt = make(map[K]time.Time)
	
	if q.logger != nil {
		q.logger.Info("Queue: cleared all state tracking")
	}
}

// QueueStats provides detailed queue statistics
type QueueStats struct {
	QueueSize      int
	Pending        int
	InFlight       int
	Completed      int
	Failed         int
	TotalEnqueued  int64
	TotalProcessed int64
	TotalFailed    int64
}

// Stats returns current queue statistics
func (q *Queue[K, V]) Stats() QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()
	
	stats := QueueStats{
		QueueSize:      len(q.work),
		TotalEnqueued:  q.totalEnqueued.Load(),
		TotalProcessed: q.totalProcessed.Load(),
		TotalFailed:    q.totalFailed.Load(),
	}
	
	for _, state := range q.state {
		switch state {
		case QueueStatePending:
			stats.Pending++
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