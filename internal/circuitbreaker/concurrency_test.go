package circuitbreaker

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestConcurrentStateTransitions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "concurrent-state-test",
		MaxFailures: 5,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 3,
	}, logger)

	// Test concurrent failures leading to open state
	const numGoroutines = 20
	var wg sync.WaitGroup
	failureCount := atomic.Int32{}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := cb.Execute(func() error {
				failureCount.Add(1)
				return errors.New("concurrent failure")
			})
			if err == nil {
				t.Error("Expected error")
			}
		}()
	}

	wg.Wait()

	// Circuit should be open
	if cb.State() != StateOpen {
		t.Errorf("Expected StateOpen after concurrent failures, got %s", cb.State())
	}

	// Verify metrics consistency
	metrics := cb.Metrics()
	totalFailures := metrics["total_failures"].(int64)
	
	// Not all goroutines may have executed due to circuit opening
	if totalFailures < int64(cb.maxFailures) {
		t.Errorf("Expected at least %d failures, got %d", cb.maxFailures, totalFailures)
	}
}

func TestConcurrentHalfOpenRequests(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "half-open-concurrent-test",
		MaxFailures: 1,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 3, // Allow 3 requests in half-open
	}, logger)

	// Force open
	cb.Execute(func() error {
		return errors.New("force open")
	})

	if cb.State() != StateOpen {
		t.Fatalf("Expected StateOpen, got %s", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Launch concurrent requests
	const numRequests = 10
	var wg sync.WaitGroup
	successCount := atomic.Int32{}
	rejectedCount := atomic.Int32{}
	
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := cb.Execute(func() error {
				// Simulate some work
				time.Sleep(20 * time.Millisecond)
				return nil
			})
			results <- err
		}(i)
	}

	wg.Wait()
	close(results)

	// Count results
	for err := range results {
		if err == nil {
			successCount.Add(1)
		} else if err == ErrCircuitBreakerOpen {
			rejectedCount.Add(1)
		} else {
			t.Errorf("Unexpected error: %v", err)
		}
	}

	success := successCount.Load()
	rejected := rejectedCount.Load()

	t.Logf("Half-open concurrent test: %d successes, %d rejected", success, rejected)

	// Should have at most MaxRequests successes
	if success > 3 {
		t.Errorf("Expected at most 3 successes in half-open, got %d", success)
	}

	// Total should be 10
	if success+rejected != 10 {
		t.Errorf("Expected 10 total results, got %d", success+rejected)
	}

	// If we had successes, circuit should be closed
	if success > 0 && cb.State() != StateClosed {
		t.Errorf("Expected StateClosed after successful half-open requests, got %s", cb.State())
	}
}

func TestRaceConditionProtection(t *testing.T) {
	// Run with -race flag to detect race conditions
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "race-test",
		MaxFailures: 10,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 5,
	}, logger)

	const numGoroutines = 100
	done := make(chan struct{})

	// Concurrent executions
	go func() {
		var wg sync.WaitGroup
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					cb.Execute(func() error {
						if id%3 == 0 {
							return errors.New("error")
						}
						return nil
					})
				}
			}(i)
		}
		wg.Wait()
		close(done)
	}()

	// Concurrent state checks
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_ = cb.State()
			}
		}
	}()

	// Concurrent metrics reads
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_ = cb.Metrics()
			}
		}
	}()

	// Concurrent resets
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				cb.Reset()
			}
		}
	}()

	<-done
	
	// Final state check
	metrics := cb.Metrics()
	t.Logf("Final metrics after race test: %+v", metrics)
}

func TestConcurrentStateChangeCallbacks(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	callbackCount := atomic.Int32{}
	maxConcurrent := atomic.Int32{}
	currentConcurrent := atomic.Int32{}

	cb := New(Config{
		Name:        "callback-concurrent-test",
		MaxFailures: 2,
		Timeout:     20 * time.Millisecond,
		MaxRequests: 1,
		OnStateChange: func(name string, from State, to State) {
			current := currentConcurrent.Add(1)
			defer currentConcurrent.Add(-1)
			
			// Track max concurrent callbacks
			for {
				max := maxConcurrent.Load()
				if current <= max || maxConcurrent.CompareAndSwap(max, current) {
					break
				}
			}
			
			callbackCount.Add(1)
			// Simulate work
			time.Sleep(50 * time.Millisecond)
		},
	}, logger)

	// Trigger rapid state changes
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Cause failures to trigger state changes
			for j := 0; j < 2; j++ {
				cb.Execute(func() error {
					return errors.New("test")
				})
			}
			time.Sleep(25 * time.Millisecond)
			// Reset to allow more state changes
			cb.Reset()
		}()
	}

	wg.Wait()
	
	// Wait for callbacks to complete
	time.Sleep(200 * time.Millisecond)

	callbacks := callbackCount.Load()
	maxConcur := maxConcurrent.Load()

	t.Logf("Total callbacks: %d, Max concurrent: %d", callbacks, maxConcur)

	// Verify callbacks were processed
	if callbacks == 0 {
		t.Error("Expected callbacks to be processed")
	}

	// Verify concurrency was limited (worker pool size is 2)
	if maxConcur > 2 {
		t.Errorf("Expected max concurrent callbacks to be <= 2, got %d", maxConcur)
	}

	cb.Shutdown()
}

func TestCallbackQueueOverflow(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	processedCallbacks := atomic.Int32{}
	blockedCallback := make(chan struct{})
	releaseCallback := make(chan struct{})

	cb := New(Config{
		Name:        "queue-overflow-test",
		MaxFailures: 1,
		Timeout:     10 * time.Millisecond,
		MaxRequests: 1,
		OnStateChange: func(name string, from State, to State) {
			if processedCallbacks.Add(1) == 1 {
				// First callback blocks until released
				close(blockedCallback)
				<-releaseCallback
			}
			// Other callbacks process normally
			time.Sleep(5 * time.Millisecond)
		},
	}, logger)

	// Trigger first state change
	cb.Execute(func() error {
		return errors.New("test")
	})

	// Wait for first callback to block
	<-blockedCallback

	// Trigger many more state changes to overflow queue
	for i := 0; i < 10; i++ {
		cb.Reset()
		cb.Execute(func() error {
			return errors.New("test")
		})
	}

	// Release blocked callback
	close(releaseCallback)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	processed := processedCallbacks.Load()
	t.Logf("Processed callbacks: %d", processed)

	// Some callbacks may have been dropped due to queue overflow
	// This is expected behavior to prevent unbounded memory growth
	if processed == 0 {
		t.Error("Expected at least some callbacks to be processed")
	}

	cb.Shutdown()
}

func TestConcurrentMetricsAccuracy(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "metrics-accuracy-test",
		MaxFailures: 100, // High to avoid opening
		Timeout:     50 * time.Millisecond,
		MaxRequests: 10,
	}, logger)

	const numGoroutines = 50
	const opsPerGoroutine = 20
	
	expectedSuccesses := atomic.Int64{}
	expectedFailures := atomic.Int64{}

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				shouldFail := (id+j)%3 == 0
				err := cb.Execute(func() error {
					if shouldFail {
						return errors.New("test failure")
					}
					return nil
				})
				
				if err != nil && err != ErrCircuitBreakerOpen {
					expectedFailures.Add(1)
				} else if err == nil {
					expectedSuccesses.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify metrics
	metrics := cb.Metrics()
	totalRequests := metrics["total_requests"].(int64)
	totalSuccesses := metrics["total_successes"].(int64)
	totalFailures := metrics["total_failures"].(int64)

	t.Logf("Metrics - Requests: %d, Successes: %d, Failures: %d", 
		totalRequests, totalSuccesses, totalFailures)
	t.Logf("Expected - Successes: %d, Failures: %d",
		expectedSuccesses.Load(), expectedFailures.Load())

	// Verify consistency
	if totalRequests != totalSuccesses+totalFailures {
		t.Errorf("Inconsistent metrics: requests=%d != successes=%d + failures=%d",
			totalRequests, totalSuccesses, totalFailures)
	}

	// Verify accuracy (within reasonable bounds due to circuit breaker behavior)
	if totalSuccesses != expectedSuccesses.Load() {
		t.Errorf("Success count mismatch: got %d, expected %d",
			totalSuccesses, expectedSuccesses.Load())
	}

	if totalFailures != expectedFailures.Load() {
		t.Errorf("Failure count mismatch: got %d, expected %d",
			totalFailures, expectedFailures.Load())
	}
}

func TestShutdownConcurrency(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	callbackStarted := atomic.Bool{}
	callbackCompleted := atomic.Bool{}

	cb := New(Config{
		Name:        "shutdown-test",
		MaxFailures: 1,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 1,
		OnStateChange: func(name string, from State, to State) {
			callbackStarted.Store(true)
			time.Sleep(100 * time.Millisecond)
			callbackCompleted.Store(true)
		},
	}, logger)

	// Trigger state change
	cb.Execute(func() error {
		return errors.New("test")
	})

	// Wait for callback to start
	for !callbackStarted.Load() {
		time.Sleep(5 * time.Millisecond)
	}

	// Shutdown with timeout
	done := make(chan struct{})
	go func() {
		cb.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		// Shutdown completed
	case <-time.After(6 * time.Second):
		t.Error("Shutdown took too long")
	}

	// Verify shutdown behavior
	if !callbackStarted.Load() {
		t.Error("Callback should have started")
	}
}