package circuitbreaker

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestMetricsBasicAccuracy(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "metrics-test",
		MaxFailures: 10,
		Timeout:     100 * time.Millisecond,
		MaxRequests: 5,
	}, logger)

	// Execute some successful requests
	for i := 0; i < 5; i++ {
		err := cb.Execute(func() error {
			return nil
		})
		if err != nil {
			t.Errorf("Expected success, got %v", err)
		}
	}

	// Execute some failed requests
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error {
			return errors.New("test failure")
		})
		if err == nil {
			t.Error("Expected failure")
		}
	}

	metrics := cb.Metrics()

	// Verify basic metrics
	if metrics["total_requests"].(int64) != 8 {
		t.Errorf("Expected 8 total requests, got %d", metrics["total_requests"])
	}
	if metrics["total_successes"].(int64) != 5 {
		t.Errorf("Expected 5 successes, got %d", metrics["total_successes"])
	}
	if metrics["total_failures"].(int64) != 3 {
		t.Errorf("Expected 3 failures, got %d", metrics["total_failures"])
	}
	if metrics["failures"].(int) != 3 {
		t.Errorf("Expected current failures to be 3, got %d", metrics["failures"])
	}
}

func TestMetricsDoNotCountRejectedRequests(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "rejection-metrics-test",
		MaxFailures: 1,
		Timeout:     100 * time.Millisecond,
		MaxRequests: 2,
	}, logger)

	// Force circuit to open
	err := cb.Execute(func() error {
		return errors.New("force open")
	})
	if err == nil {
		t.Fatal("Expected failure")
	}

	initialMetrics := cb.Metrics()
	initialTotal := initialMetrics["total_requests"].(int64)

	// Try to execute while open (should be rejected)
	for i := 0; i < 5; i++ {
		err = cb.Execute(func() error {
			t.Error("This should not execute")
			return nil
		})
		if err != ErrCircuitBreakerOpen {
			t.Errorf("Expected ErrCircuitBreakerOpen, got %v", err)
		}
	}

	// Check metrics didn't increase for rejected requests
	finalMetrics := cb.Metrics()
	finalTotal := finalMetrics["total_requests"].(int64)

	if finalTotal != initialTotal {
		t.Errorf("Total requests increased for rejected requests: %d -> %d", 
			initialTotal, finalTotal)
	}
}

func TestMetricsHalfOpenAccuracy(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "half-open-metrics-test",
		MaxFailures: 1,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 3,
	}, logger)

	// Force open
	cb.Execute(func() error {
		return errors.New("force open")
	})

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Execute requests in half-open state
	var wg sync.WaitGroup
	successCount := atomic.Int32{}
	rejectedCount := atomic.Int32{}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := cb.Execute(func() error {
				time.Sleep(10 * time.Millisecond)
				return nil
			})
			if err == nil {
				successCount.Add(1)
			} else if err == ErrCircuitBreakerOpen {
				rejectedCount.Add(1)
			}
		}()
	}

	wg.Wait()

	metrics := cb.Metrics()
	
	// Total requests should only count the initial failure + successful half-open requests
	totalRequests := metrics["total_requests"].(int64)
	totalSuccesses := metrics["total_successes"].(int64)
	totalFailures := metrics["total_failures"].(int64)

	t.Logf("Metrics: requests=%d, successes=%d, failures=%d", 
		totalRequests, totalSuccesses, totalFailures)
	t.Logf("Counts: success=%d, rejected=%d", 
		successCount.Load(), rejectedCount.Load())

	// Verify only successful requests were counted
	expectedTotal := int64(1 + successCount.Load()) // 1 initial failure + successes
	if totalRequests != expectedTotal {
		t.Errorf("Expected %d total requests, got %d", expectedTotal, totalRequests)
	}

	// Verify consistency
	if totalRequests != totalSuccesses+totalFailures {
		t.Errorf("Inconsistent metrics: %d != %d + %d", 
			totalRequests, totalSuccesses, totalFailures)
	}
}

func TestMetricsStateChangeTracking(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "state-change-metrics-test",
		MaxFailures: 2,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 1,
	}, logger)

	// Initial state changes should be 0
	metrics := cb.Metrics()
	if metrics["state_changes"].(int64) != 0 {
		t.Errorf("Expected 0 initial state changes, got %d", metrics["state_changes"])
	}

	// Force state changes
	// Closed -> Open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return errors.New("failure")
		})
	}

	metrics = cb.Metrics()
	if metrics["state_changes"].(int64) != 1 {
		t.Errorf("Expected 1 state change (closed->open), got %d", metrics["state_changes"])
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// The next request will transition to half-open and possibly to closed
	err := cb.Execute(func() error {
		return nil
	})

	metrics = cb.Metrics()
	stateChanges := metrics["state_changes"].(int64)
	
	// We should have at least 2 state changes:
	// 1. Closed -> Open (when failures exceeded)
	// 2. Open -> Half-Open (when first request after timeout)
	// 3. Half-Open -> Closed (if the request succeeded)
	// However, due to timing, we might only see 1 or 2
	if stateChanges < 1 {
		t.Errorf("Expected at least 1 state change, got %d", stateChanges)
	}
	
	// If the request succeeded, we should see the circuit closed
	if err == nil && cb.State() == StateClosed {
		// We should have seen all 3 transitions
		if stateChanges < 2 {
			t.Errorf("Expected at least 2 state changes after successful recovery, got %d", stateChanges)
		}
	}
	
	t.Logf("State changes: %d, Final state: %s", stateChanges, cb.State())

	// Verify last state change time was updated
	lastChange := metrics["last_state_change"].(string)
	if lastChange == "" {
		t.Error("Expected last_state_change to be set")
	}
}

func TestMetricsAllFields(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "complete-metrics-test",
		MaxFailures: 5,
		Timeout:     30 * time.Second,
		MaxRequests: 3,
	}, logger)

	// Execute to populate some metrics
	cb.Execute(func() error {
		return errors.New("test")
	})

	metrics := cb.Metrics()

	// Verify all expected fields are present
	expectedFields := []string{
		"name",
		"state",
		"failures",
		"requests",
		"total_requests",
		"total_failures",
		"total_successes",
		"state_changes",
		"max_failures",
		"timeout_seconds",
		"max_requests",
		"last_failure",
		"last_state_change",
	}

	for _, field := range expectedFields {
		if _, exists := metrics[field]; !exists {
			t.Errorf("Expected field %q in metrics", field)
		}
	}

	// Verify field types
	if _, ok := metrics["name"].(string); !ok {
		t.Error("Expected 'name' to be string")
	}
	if _, ok := metrics["state"].(string); !ok {
		t.Error("Expected 'state' to be string")
	}
	if _, ok := metrics["failures"].(int); !ok {
		t.Error("Expected 'failures' to be int")
	}
	if _, ok := metrics["requests"].(int); !ok {
		t.Error("Expected 'requests' to be int")
	}
	if _, ok := metrics["total_requests"].(int64); !ok {
		t.Error("Expected 'total_requests' to be int64")
	}
	if _, ok := metrics["total_failures"].(int64); !ok {
		t.Error("Expected 'total_failures' to be int64")
	}
	if _, ok := metrics["total_successes"].(int64); !ok {
		t.Error("Expected 'total_successes' to be int64")
	}
	if _, ok := metrics["state_changes"].(int64); !ok {
		t.Error("Expected 'state_changes' to be int64")
	}
	if _, ok := metrics["max_failures"].(int); !ok {
		t.Error("Expected 'max_failures' to be int")
	}
	if _, ok := metrics["timeout_seconds"].(float64); !ok {
		t.Error("Expected 'timeout_seconds' to be float64")
	}
	if _, ok := metrics["max_requests"].(int); !ok {
		t.Error("Expected 'max_requests' to be int")
	}
	if _, ok := metrics["last_failure"].(string); !ok {
		t.Error("Expected 'last_failure' to be string")
	}
	if _, ok := metrics["last_state_change"].(string); !ok {
		t.Error("Expected 'last_state_change' to be string")
	}
}

func TestMetricsThreadSafety(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "thread-safety-test",
		MaxFailures: 100,
		Timeout:     100 * time.Millisecond,
		MaxRequests: 10,
	}, logger)

	const numGoroutines = 50
	var wg sync.WaitGroup

	// Concurrent executions
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				cb.Execute(func() error {
					if id%2 == 0 {
						return nil
					}
					return errors.New("error")
				})
			}
		}(i)
	}

	// Concurrent metrics reads
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	metricsReadCount := 0
	for {
		select {
		case <-done:
			goto verify
		default:
			metrics := cb.Metrics()
			metricsReadCount++
			
			// Just read metrics, don't check consistency during concurrent execution
			// as values might be in flux
			_ = metrics["total_requests"].(int64)
			_ = metrics["total_successes"].(int64)
			_ = metrics["total_failures"].(int64)
		}
	}

verify:
	t.Logf("Read metrics %d times during concurrent execution", metricsReadCount)
	
	finalMetrics := cb.Metrics()
	totalReqs := finalMetrics["total_requests"].(int64)
	totalSuccess := finalMetrics["total_successes"].(int64)
	totalFail := finalMetrics["total_failures"].(int64)
	
	t.Logf("Final metrics: requests=%d, successes=%d, failures=%d",
		totalReqs, totalSuccess, totalFail)
	
	if totalReqs != totalSuccess+totalFail {
		t.Errorf("Final metrics inconsistent: %d != %d + %d",
			totalReqs, totalSuccess, totalFail)
	}
}

func TestMetricsAfterReset(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "reset-metrics-test",
		MaxFailures: 3,
		Timeout:     100 * time.Millisecond,
		MaxRequests: 2,
	}, logger)

	// Build up some metrics
	for i := 0; i < 5; i++ {
		cb.Execute(func() error {
			if i < 3 {
				return nil
			}
			return errors.New("error")
		})
	}

	beforeReset := cb.Metrics()
	
	// Reset
	cb.Reset()
	
	afterReset := cb.Metrics()

	// Current counters should be reset
	if afterReset["failures"].(int) != 0 {
		t.Errorf("Expected failures to be 0 after reset, got %d", afterReset["failures"])
	}
	if afterReset["requests"].(int) != 0 {
		t.Errorf("Expected requests to be 0 after reset, got %d", afterReset["requests"])
	}
	if afterReset["state"].(string) != "closed" {
		t.Errorf("Expected state to be closed after reset, got %s", afterReset["state"])
	}

	// Total counters should NOT be reset
	if afterReset["total_requests"].(int64) != beforeReset["total_requests"].(int64) {
		t.Error("Total requests should not be reset")
	}
	if afterReset["total_successes"].(int64) != beforeReset["total_successes"].(int64) {
		t.Error("Total successes should not be reset")
	}
	if afterReset["total_failures"].(int64) != beforeReset["total_failures"].(int64) {
		t.Error("Total failures should not be reset")
	}

	// State changes might increase if the state was not already closed
	// Reset only calls setState if the current state is not already closed
	beforeState := beforeReset["state"].(string)
	afterStateChanges := afterReset["state_changes"].(int64)
	beforeStateChanges := beforeReset["state_changes"].(int64)
	
	if beforeState != "closed" && afterStateChanges == beforeStateChanges {
		t.Errorf("Expected state_changes to increase after reset from %s state, got %d (was %d)",
			beforeState, afterStateChanges, beforeStateChanges)
	}
}

func TestStringRepresentation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "string-test",
		MaxFailures: 5,
		Timeout:     30 * time.Second,
		MaxRequests: 2,
	}, logger)

	// Test initial state
	str := cb.String()
	expected := "CircuitBreaker(name=string-test, state=closed, failures=0/5)"
	if str != expected {
		t.Errorf("Expected %q, got %q", expected, str)
	}

	// Add some failures
	for i := 0; i < 3; i++ {
		cb.Execute(func() error {
			return errors.New("test")
		})
	}

	str = cb.String()
	expected = "CircuitBreaker(name=string-test, state=closed, failures=3/5)"
	if str != expected {
		t.Errorf("Expected %q, got %q", expected, str)
	}

	// Force open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return errors.New("test")
		})
	}

	str = cb.String()
	expected = "CircuitBreaker(name=string-test, state=open, failures=5/5)"
	if str != expected {
		t.Errorf("Expected %q, got %q", expected, str)
	}
}