package utils

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPriorityQueueBasicEnqueue tests basic enqueueing to both queues
func TestPriorityQueueBasicEnqueue(t *testing.T) {
	pq := NewPriorityQueue[string, int](10, 10, 2)
	defer pq.Stop()
	
	// Test priority enqueue
	result := pq.TryEnqueue("priority1", 1, true)
	if result != Enqueued {
		t.Errorf("Expected Enqueued, got %v", result)
	}
	
	// Test normal enqueue
	result = pq.TryEnqueue("normal1", 2, false)
	if result != Enqueued {
		t.Errorf("Expected Enqueued, got %v", result)
	}
	
	// Test deduplication across queues
	result = pq.TryEnqueue("priority1", 1, false) // Try to add to normal
	if result != AlreadyQueued {
		t.Errorf("Expected AlreadyQueued, got %v", result)
	}
	
	// Check sizes
	priority, normal := pq.Size()
	if priority != 1 || normal != 1 {
		t.Errorf("Expected sizes (1,1), got (%d,%d)", priority, normal)
	}
}

// TestPriorityQueueFairness tests the fairness mechanism
func TestPriorityQueueFairness(t *testing.T) {
	// Create queue with 2:1 fairness ratio
	pq := NewPriorityQueue[string, int](100, 100, 2)
	defer pq.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Add many items to both queues to ensure continuous work
	for i := 0; i < 50; i++ {
		pq.MustEnqueue(ctx, fmt.Sprintf("p%d", i), i, true)  // Priority
	}
	for i := 0; i < 50; i++ {
		pq.MustEnqueue(ctx, fmt.Sprintf("n%d", i), i, false) // Normal
	}
	
	// Track processing order
	var priorityCount, normalCount atomic.Int32
	var mu sync.Mutex
	var order []string
	
	// Process with tracking
	results := pq.Process(ctx, 2, func(ctx context.Context, key string, value int) error {
		mu.Lock()
		order = append(order, key)
		mu.Unlock()
		
		if key[0] == 'p' {
			priorityCount.Add(1)
		} else {
			normalCount.Add(1)
		}
		
		// Simulate work
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	
	// Let processing happen for a while
	time.Sleep(300 * time.Millisecond)
	cancel()
	
	// Drain results
	for range results {
		// Consume all
	}
	
	// Check fairness - with 2:1 ratio, we expect roughly 2x priority items
	pCount := priorityCount.Load()
	nCount := normalCount.Load()
	
	if pCount == 0 || nCount == 0 {
		t.Errorf("Expected both queues to be processed, got priority=%d normal=%d", pCount, nCount)
	}
	
	// Check that fairness is roughly maintained (allow some variance)
	ratio := float64(pCount) / float64(nCount)
	if ratio < 1.5 || ratio > 2.5 {
		t.Errorf("Fairness ratio off: expected ~2.0, got %.2f (p=%d, n=%d)", ratio, pCount, nCount)
	}
	
	// Verify order respects fairness pattern
	// Count runs of priority items
	var runs []int
	currentRun := 0
	for _, key := range order {
		if key[0] == 'p' {
			currentRun++
		} else {
			if currentRun > 0 {
				runs = append(runs, currentRun)
				currentRun = 0
			}
		}
	}
	if currentRun > 0 {
		runs = append(runs, currentRun)
	}
	
	// Most runs should be 2 (the fairness ratio)
	maxRun := 0
	for _, run := range runs {
		if run > maxRun {
			maxRun = run
		}
	}
	
	// With 2:1 ratio, shouldn't see runs much longer than 2
	// (allowing some variance for edge cases)
	if maxRun > 4 {
		t.Errorf("Too many consecutive priority items: %d", maxRun)
	}
}

// TestPriorityQueueNoStarvation ensures normal queue isn't starved
func TestPriorityQueueNoStarvation(t *testing.T) {
	// Create queue with 3:1 fairness
	pq := NewPriorityQueue[string, int](100, 100, 3)
	defer pq.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Continuously add priority items
	go func() {
		for i := 0; ctx.Err() == nil; i++ {
			pq.TryEnqueue(fmt.Sprintf("p%d", i), i, true)
			time.Sleep(5 * time.Millisecond)
		}
	}()
	
	// Add a few normal items
	for i := 0; i < 5; i++ {
		pq.MustEnqueue(ctx, fmt.Sprintf("n%d", i), i, false)
	}
	
	normalProcessed := atomic.Int32{}
	
	// Process
	results := pq.Process(ctx, 2, func(ctx context.Context, key string, value int) error {
		if key[0] == 'n' {
			normalProcessed.Add(1)
		}
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	
	// Wait and check that normal items get processed despite priority pressure
	time.Sleep(300 * time.Millisecond)
	cancel()
	
	for range results {
		// Drain
	}
	
	// All 5 normal items should have been processed
	if normalProcessed.Load() != 5 {
		t.Errorf("Normal queue starved: only %d/5 items processed", normalProcessed.Load())
	}
}

// TestPriorityQueueQueueFull tests queue full scenarios
func TestPriorityQueueQueueFull(t *testing.T) {
	// Small queues
	pq := NewPriorityQueue[string, int](2, 2, 1)
	defer pq.Stop()
	
	// Fill priority queue
	pq.TryEnqueue("p1", 1, true)
	pq.TryEnqueue("p2", 2, true)
	
	// Next priority should fail
	result := pq.TryEnqueue("p3", 3, true)
	if result != QueueFull {
		t.Errorf("Expected QueueFull for priority, got %v", result)
	}
	
	// But normal should work
	result = pq.TryEnqueue("n1", 1, false)
	if result != Enqueued {
		t.Errorf("Expected Enqueued for normal, got %v", result)
	}
}

// TestPriorityQueueDeduplication tests dedup across both queues
func TestPriorityQueueDeduplication(t *testing.T) {
	pq := NewPriorityQueue[string, int](10, 10, 1)
	defer pq.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Add to priority
	result := pq.TryEnqueue("key1", 1, true)
	if result != Enqueued {
		t.Errorf("Expected Enqueued, got %v", result)
	}
	
	// Try to add same key to normal - should be deduplicated
	result = pq.TryEnqueue("key1", 2, false)
	if result != AlreadyQueued {
		t.Errorf("Expected AlreadyQueued for duplicate, got %v", result)
	}
	
	// Process the item
	processed := false
	results := pq.Process(ctx, 1, func(ctx context.Context, key string, value int) error {
		if key == "key1" && value == 1 { // Should get original value
			processed = true
		}
		return nil
	})
	
	time.Sleep(50 * time.Millisecond)
	
	if !processed {
		t.Error("Item not processed")
	}
	
	// Now should be able to add again
	result = pq.TryEnqueue("key1", 3, false)
	if result != Enqueued {
		t.Errorf("Expected Enqueued after processing, got %v", result)
	}
	
	// Cancel context to stop processing
	cancel()
	
	// Drain results
	for range results {
		// Drain
	}
}

// TestPriorityQueueStats tests statistics tracking
func TestPriorityQueueStats(t *testing.T) {
	pq := NewPriorityQueue[string, int](10, 10, 2)
	defer pq.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Add items
	pq.TryEnqueue("p1", 1, true)
	pq.TryEnqueue("p2", 2, true)
	pq.TryEnqueue("n1", 3, false)
	
	// Check initial stats
	stats := pq.Stats()
	if stats.PriorityPending != 2 || stats.NormalPending != 1 {
		t.Errorf("Wrong pending counts: priority=%d, normal=%d", 
			stats.PriorityPending, stats.NormalPending)
	}
	
	// Process some items
	processed := atomic.Int32{}
	results := pq.Process(ctx, 1, func(ctx context.Context, key string, value int) error {
		processed.Add(1)
		if processed.Load() >= 2 {
			cancel() // Stop after 2 items
		}
		return nil
	})
	
	// Wait for processing
	for range results {
		// Drain
	}
	
	// Check final stats
	stats = pq.Stats()
	if stats.TotalProcessed != 2 {
		t.Errorf("Expected 2 processed, got %d", stats.TotalProcessed)
	}
	
	// Fairness ratio should be maintained in stats
	if stats.FairnessRatio != 2 {
		t.Errorf("Expected fairness ratio 2, got %d", stats.FairnessRatio)
	}
}

// TestPriorityQueueStop tests graceful stop
func TestPriorityQueueStop(t *testing.T) {
	pq := NewPriorityQueue[string, int](10, 10, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Add items to both queues
	for i := 0; i < 3; i++ {
		pq.TryEnqueue(fmt.Sprintf("p%d", i), i, true)
		pq.TryEnqueue(fmt.Sprintf("n%d", i), i, false)
	}
	
	processed := atomic.Int32{}
	
	// Start processing
	results := pq.Process(ctx, 2, func(ctx context.Context, key string, value int) error {
		processed.Add(1)
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	
	// Stop queue after short delay
	time.Sleep(20 * time.Millisecond)
	pq.Stop()
	
	// Cancel processing context
	cancel()
	
	// Wait for all results
	for range results {
		// Count
	}
	
	// Should have processed some items before stop
	if processed.Load() == 0 {
		t.Error("Expected some items to be processed before stop")
	}
}

// TestPriorityQueueConcurrency tests concurrent operations
func TestPriorityQueueConcurrency(t *testing.T) {
	pq := NewPriorityQueue[int, int](100, 100, 2)
	defer pq.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	var wg sync.WaitGroup
	
	// Multiple concurrent enqueuers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				key := id*100 + j
				pq.TryEnqueue(key, key, id%2 == 0) // Even IDs are priority
			}
		}(i)
	}
	
	// Start processing
	processed := atomic.Int32{}
	results := pq.Process(ctx, 3, func(ctx context.Context, key int, value int) error {
		processed.Add(1)
		return nil
	})
	
	// Wait for enqueuers
	wg.Wait()
	
	// Let processing happen
	time.Sleep(100 * time.Millisecond)
	cancel()
	
	// Drain results
	for range results {
		// Count
	}
	
	// Should have processed many items without issues
	if processed.Load() == 0 {
		t.Error("No items processed in concurrent test")
	}
	
	// Check stats consistency
	stats := pq.Stats()
	if stats.TotalProcessed != int64(processed.Load()) {
		t.Errorf("Stats mismatch: reported=%d, actual=%d", 
			stats.TotalProcessed, processed.Load())
	}
}

// TestPriorityQueueIsPriority tests checking if item is priority
func TestPriorityQueueIsPriority(t *testing.T) {
	pq := NewPriorityQueue[string, int](10, 10, 1)
	defer pq.Stop()
	
	// Add priority item
	pq.TryEnqueue("p1", 1, true)
	
	// Check it's marked as priority
	isPriority, exists := pq.IsPriority("p1")
	if !exists || !isPriority {
		t.Error("Priority item not marked correctly")
	}
	
	// Add normal item
	pq.TryEnqueue("n1", 2, false)
	
	// Check it's marked as normal
	isPriority, exists = pq.IsPriority("n1")
	if !exists || isPriority {
		t.Error("Normal item not marked correctly")
	}
	
	// Check non-existent
	_, exists = pq.IsPriority("unknown")
	if exists {
		t.Error("Non-existent item reported as existing")
	}
}