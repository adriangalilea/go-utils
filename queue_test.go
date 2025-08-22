package utils

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueBasicEnqueueDequeue(t *testing.T) {
	q := NewQueue[string, int](10)
	
	// First enqueue should succeed
	if !q.TryEnqueue("item1", 100) {
		t.Fatal("Failed to enqueue first item")
	}
	
	// Duplicate enqueue should fail
	if q.TryEnqueue("item1", 200) {
		t.Fatal("Should not allow duplicate enqueue")
	}
	
	// Different key should succeed
	if !q.TryEnqueue("item2", 300) {
		t.Fatal("Failed to enqueue different item")
	}
	
	stats := q.Stats()
	if stats.Pending != 2 {
		t.Errorf("Expected 2 pending items, got %d", stats.Pending)
	}
}

func TestQueueFullBehavior(t *testing.T) {
	q := NewQueue[int, string](2) // Small queue
	
	// Fill the queue
	if !q.TryEnqueue(1, "first") {
		t.Fatal("Failed to enqueue first item")
	}
	if !q.TryEnqueue(2, "second") {
		t.Fatal("Failed to enqueue second item")
	}
	
	// Queue should be full
	if q.TryEnqueue(3, "third") {
		t.Fatal("Should not enqueue when queue is full")
	}
	
	// But duplicate should still return false (not enqueue)
	if q.TryEnqueue(1, "duplicate") {
		t.Fatal("Should not allow duplicate even when checking queue full")
	}
}

func TestQueueProcessWorker(t *testing.T) {
	q := NewQueue[string, int](10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	processed := make(map[string]int)
	var mu sync.Mutex
	
	// Start workers and get results channel
	results := q.Process(ctx, 2, func(ctx context.Context, key string, value int) error {
		mu.Lock()
		processed[key] = value
		mu.Unlock()
		return nil
	})
	
	// Enqueue items
	q.TryEnqueue("a", 1)
	q.TryEnqueue("b", 2)
	q.TryEnqueue("c", 3)
	
	// Collect results
	count := 0
	for count < 3 {
		select {
		case result := <-results:
			if result.Error != nil {
				t.Error("Unexpected error:", result.Error)
			}
			count++
		case <-time.After(200 * time.Millisecond):
			t.Fatal("Timeout waiting for results")
		}
	}
	
	mu.Lock()
	defer mu.Unlock()
	
	if len(processed) != 3 {
		t.Errorf("Expected 3 processed items, got %d", len(processed))
	}
	if processed["a"] != 1 || processed["b"] != 2 || processed["c"] != 3 {
		t.Error("Processed values don't match")
	}
}

func TestQueueRaceConditions(t *testing.T) {
	q := NewQueue[int, int](100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	var processed atomic.Int32
	
	// Start workers with results channel
	results := q.Process(ctx, 5, func(ctx context.Context, key int, value int) error {
		processed.Add(1)
		time.Sleep(time.Millisecond) // Simulate work
		return nil
	})
	
	// Drain results in background
	go func() {
		for range results {
		}
	}()
	
	// Multiple goroutines trying to enqueue the same items
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Each worker tries to enqueue the same keys
				q.TryEnqueue(j, j*worker)
			}
		}(i)
	}
	
	wg.Wait()
	time.Sleep(200 * time.Millisecond) // Wait for processing
	
	// Should have processed exactly 100 unique items (0-99)
	count := processed.Load()
	if count != 100 {
		t.Errorf("Expected 100 processed items, got %d", count)
	}
}

func TestQueueStateTransitions(t *testing.T) {
	q := NewQueue[string, int](10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Check initial state
	if q.IsPending("test") {
		t.Error("Should not be pending initially")
	}
	
	// Enqueue
	q.TryEnqueue("test", 42)
	
	state, exists := q.GetState("test")
	if !exists || state != QueueStatePending {
		t.Error("Should be in pending state after enqueue")
	}
	
	processed := make(chan bool)
	
	// Start worker with results
	results := q.Process(ctx, 1, func(ctx context.Context, key string, value int) error {
		// Check in-flight state
		state, exists := q.GetState(key)
		if !exists || state != QueueStateInFlight {
			t.Error("Should be in-flight during processing")
		}
		processed <- true
		return nil
	})
	
	// Drain results
	go func() {
		for range results {
		}
	}()
	
	<-processed
	time.Sleep(10 * time.Millisecond) // Let it complete
	
	state, exists = q.GetState("test")
	if !exists || state != QueueStateCompleted {
		t.Error("Should be in completed state after processing")
	}
	
	// Can re-enqueue after completion
	if !q.TryEnqueue("test", 43) {
		t.Error("Should be able to re-enqueue completed item")
	}
}

func TestQueueProcessWithRetry(t *testing.T) {
	q := NewQueue[string, int](10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	attempts := make(map[string]int)
	var mu sync.Mutex
	
	// Start worker with retry
	results := q.ProcessWithRetry(ctx, 1, func(ctx context.Context, key string, value int) error {
		mu.Lock()
		attempts[key]++
		count := attempts[key]
		mu.Unlock()
		
		if count < 3 {
			return errors.New("simulated failure")
		}
		return nil
	}, 5) // max 5 retries
	
	// Drain results
	go func() {
		for range results {
		}
	}()
	
	q.TryEnqueue("retry-test", 1)
	
	time.Sleep(5 * time.Second) // Wait for retries
	
	mu.Lock()
	defer mu.Unlock()
	
	if attempts["retry-test"] != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts["retry-test"])
	}
	
	// Should be completed after successful retry
	state, _ := q.GetState("retry-test")
	if state != QueueStateCompleted {
		t.Error("Should be completed after successful retry")
	}
}

func TestQueueMustEnqueue(t *testing.T) {
	q := NewQueue[string, int](2)
	ctx, cancel := context.WithCancel(context.Background())
	
	// Fill queue
	q.TryEnqueue("a", 1)
	q.TryEnqueue("b", 2)
	
	// Start a goroutine that will block on MustEnqueue
	done := make(chan error)
	go func() {
		err := q.MustEnqueue(ctx, "c", 3)
		done <- err
	}()
	
	// Should timeout since queue is full
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("MustEnqueue should block when queue is full, not error")
		}
		t.Fatal("MustEnqueue should block when queue is full")
	case <-time.After(100 * time.Millisecond):
		// Expected
	}
	
	// Cancel context
	cancel()
	
	// Now it should complete with context error
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("MustEnqueue should return error when context cancelled")
		}
		// Expected - should be context.Canceled error
	case <-time.After(100 * time.Millisecond):
		t.Fatal("MustEnqueue should return when context cancelled")
	}
}

func TestQueueCleanupCompleted(t *testing.T) {
	q := NewQueue[int, string](10, WithMaxCompleted[int, string](5)) // Track only 5 completed
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Start worker
	results := q.Process(ctx, 1, func(ctx context.Context, key int, value string) error {
		return nil
	})
	
	// Drain results
	go func() {
		for range results {
		}
	}()
	
	// Enqueue more than maxCompleted
	for i := 0; i < 10; i++ {
		q.TryEnqueue(i, "value")
		time.Sleep(10 * time.Millisecond) // Ensure processing
	}
	
	time.Sleep(100 * time.Millisecond)
	
	stats := q.Stats()
	if stats.Completed > 5 {
		t.Errorf("Should track at most 5 completed, got %d", stats.Completed)
	}
}

func TestQueueConcurrentEnqueueSameKey(t *testing.T) {
	q := NewQueue[string, int](100)
	
	success := atomic.Int32{}
	
	// 100 goroutines try to enqueue the same key
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			if q.TryEnqueue("same-key", val) {
				success.Add(1)
			}
		}(i)
	}
	
	wg.Wait()
	
	// Exactly one should succeed
	if success.Load() != 1 {
		t.Errorf("Expected exactly 1 successful enqueue, got %d", success.Load())
	}
}

func BenchmarkQueueTryEnqueue(b *testing.B) {
	q := NewQueue[int, int](1000)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.TryEnqueue(i, i)
	}
}

func BenchmarkQueueTryEnqueueParallel(b *testing.B) {
	q := NewQueue[int, int](1000)
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			q.TryEnqueue(i, i)
			i++
		}
	})
}