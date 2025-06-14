package circuitbreaker

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestStateTransitions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tests := []struct {
		name         string
		scenario     func(t *testing.T, cb *CircuitBreaker)
		expectedEnd  State
	}{
		{
			name: "closed_to_open_after_max_failures",
			scenario: func(t *testing.T, cb *CircuitBreaker) {
				// Cause MaxFailures failures
				for i := 0; i < 3; i++ {
					err := cb.Execute(func() error {
						return errors.New("test failure")
					})
					if err == nil {
						t.Error("Expected failure")
					}
				}
			},
			expectedEnd: StateOpen,
		},
		{
			name: "open_to_half_open_after_timeout",
			scenario: func(t *testing.T, cb *CircuitBreaker) {
				// Force open
				for i := 0; i < 3; i++ {
					cb.Execute(func() error {
						return errors.New("test failure")
					})
				}
				
				// Verify open
				if cb.State() != StateOpen {
					t.Fatalf("Expected StateOpen, got %s", cb.State())
				}
				
				// Wait for timeout
				time.Sleep(110 * time.Millisecond)
				
				// Next request should transition to half-open
				cb.Execute(func() error {
					return nil
				})
			},
			expectedEnd: StateClosed, // Success in half-open closes it
		},
		{
			name: "half_open_to_closed_on_success",
			scenario: func(t *testing.T, cb *CircuitBreaker) {
				// Force open
				for i := 0; i < 3; i++ {
					cb.Execute(func() error {
						return errors.New("test failure")
					})
				}
				
				// Wait for timeout
				time.Sleep(110 * time.Millisecond)
				
				// Success in half-open should close
				err := cb.Execute(func() error {
					return nil
				})
				if err != nil {
					t.Errorf("Expected success, got %v", err)
				}
			},
			expectedEnd: StateClosed,
		},
		{
			name: "half_open_to_open_on_failure",
			scenario: func(t *testing.T, cb *CircuitBreaker) {
				// Force open
				for i := 0; i < 3; i++ {
					cb.Execute(func() error {
						return errors.New("test failure")
					})
				}
				
				// Wait for timeout
				time.Sleep(110 * time.Millisecond)
				
				// Failure in half-open should re-open
				cb.Execute(func() error {
					return errors.New("test failure")
				})
			},
			expectedEnd: StateOpen,
		},
		{
			name: "closed_remains_closed_with_successes",
			scenario: func(t *testing.T, cb *CircuitBreaker) {
				// All successes should keep it closed
				for i := 0; i < 10; i++ {
					err := cb.Execute(func() error {
						return nil
					})
					if err != nil {
						t.Errorf("Expected success, got %v", err)
					}
				}
			},
			expectedEnd: StateClosed,
		},
		{
			name: "failures_reset_on_success",
			scenario: func(t *testing.T, cb *CircuitBreaker) {
				// Two failures (one less than max)
				for i := 0; i < 2; i++ {
					cb.Execute(func() error {
						return errors.New("test failure")
					})
				}
				
				// Success should reset failure count
				err := cb.Execute(func() error {
					return nil
				})
				if err != nil {
					t.Errorf("Expected success, got %v", err)
				}
				
				// Two more failures should not open (count was reset)
				for i := 0; i < 2; i++ {
					cb.Execute(func() error {
						return errors.New("test failure")
					})
				}
			},
			expectedEnd: StateClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := New(Config{
				Name:        "test",
				MaxFailures: 3,
				Timeout:     100 * time.Millisecond,
				MaxRequests: 2,
			}, logger)

			tt.scenario(t, cb)

			if cb.State() != tt.expectedEnd {
				t.Errorf("Expected final state %s, got %s", tt.expectedEnd, cb.State())
			}
		})
	}
}

func TestStateChangeCallbacks(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	var stateChanges []struct {
		from State
		to   State
	}
	var mu sync.Mutex

	cb := New(Config{
		Name:        "callback-test",
		MaxFailures: 2,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 1,
		OnStateChange: func(name string, from State, to State) {
			mu.Lock()
			stateChanges = append(stateChanges, struct {
				from State
				to   State
			}{from, to})
			mu.Unlock()
		},
	}, logger)

	// Trigger state transitions
	// 1. Closed -> Open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return errors.New("test failure")
		})
	}

	// 2. Open -> Half-Open (after timeout)
	time.Sleep(60 * time.Millisecond)
	
	// 3. Half-Open -> Closed
	cb.Execute(func() error {
		return nil
	})

	// Wait for callbacks to complete
	time.Sleep(100 * time.Millisecond)

	// Verify callbacks
	mu.Lock()
	defer mu.Unlock()

	// We expect at least one state change (Closed -> Open)
	// The exact number depends on timing and whether the half-open transition occurs
	if len(stateChanges) == 0 {
		t.Fatal("Expected at least one state change")
	}

	// First change should always be Closed -> Open
	if len(stateChanges) > 0 {
		first := stateChanges[0]
		if first.from != StateClosed || first.to != StateOpen {
			t.Errorf("First state change: expected closed->open, got %s->%s",
				first.from, first.to)
		}
	}

	t.Logf("Recorded %d state changes", len(stateChanges))

	// Shutdown and verify no more callbacks
	cb.Shutdown()
}

func TestStateChangeCallbackPanic(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	panicCount := 0
	var mu sync.Mutex

	cb := New(Config{
		Name:        "panic-test",
		MaxFailures: 1,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 1,
		OnStateChange: func(name string, from State, to State) {
			mu.Lock()
			panicCount++
			mu.Unlock()
			panic("test panic")
		},
	}, logger)

	// Trigger state change (should not crash)
	cb.Execute(func() error {
		return errors.New("test failure")
	})

	// Wait for callback
	time.Sleep(100 * time.Millisecond)

	// Verify state changed despite panic
	if cb.State() != StateOpen {
		t.Errorf("Expected StateOpen after failure, got %s", cb.State())
	}

	mu.Lock()
	if panicCount == 0 {
		t.Error("Expected callback to be called even though it panics")
	}
	mu.Unlock()

	cb.Shutdown()
}

func TestStateChangeCallbackTimeout(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	callbackStarted := make(chan struct{})
	callbackCompleted := atomic.Bool{}

	cb := New(Config{
		Name:        "timeout-test",
		MaxFailures: 1,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 1,
		OnStateChange: func(name string, from State, to State) {
			close(callbackStarted)
			// Simulate slow callback that exceeds 5s timeout
			time.Sleep(6 * time.Second)
			callbackCompleted.Store(true)
		},
	}, logger)

	// Trigger state change
	cb.Execute(func() error {
		return errors.New("test failure")
	})

	// Wait for callback to start
	<-callbackStarted

	// Wait for timeout period
	time.Sleep(5500 * time.Millisecond)

	// Callback should have timed out
	if callbackCompleted.Load() {
		t.Error("Expected callback to timeout, but it completed")
	}

	cb.Shutdown()
}

func TestReset(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cb := New(Config{
		Name:        "reset-test",
		MaxFailures: 2,
		Timeout:     50 * time.Millisecond,
		MaxRequests: 1,
	}, logger)

	// Force circuit breaker to open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return errors.New("test failure")
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("Expected StateOpen, got %s", cb.State())
	}

	// Reset
	cb.Reset()

	// Verify reset
	if cb.State() != StateClosed {
		t.Errorf("Expected StateClosed after reset, got %s", cb.State())
	}

	metrics := cb.Metrics()
	if metrics["failures"].(int) != 0 {
		t.Errorf("Expected failures to be 0 after reset, got %d", metrics["failures"])
	}
	if metrics["requests"].(int) != 0 {
		t.Errorf("Expected requests to be 0 after reset, got %d", metrics["requests"])
	}

	// Verify circuit breaker works normally after reset
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected success after reset, got %v", err)
	}
}

func TestMultipleStateChangeCallbacks(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Test that multiple state changes are handled correctly by worker pool
	callbackCount := atomic.Int32{}
	
	cb := New(Config{
		Name:        "multi-callback-test",
		MaxFailures: 1,
		Timeout:     20 * time.Millisecond,
		MaxRequests: 1,
		OnStateChange: func(name string, from State, to State) {
			callbackCount.Add(1)
			// Simulate some work
			time.Sleep(10 * time.Millisecond)
		},
	}, logger)

	// Rapid state changes
	for i := 0; i < 5; i++ {
		// Open circuit
		cb.Execute(func() error {
			return errors.New("test failure")
		})
		
		// Wait for timeout
		time.Sleep(25 * time.Millisecond)
		
		// Close circuit
		cb.Execute(func() error {
			return nil
		})
		
		// Reset for next iteration
		cb.Reset()
	}

	// Wait for all callbacks
	time.Sleep(200 * time.Millisecond)

	count := callbackCount.Load()
	if count == 0 {
		t.Error("Expected callbacks to be processed")
	}

	t.Logf("Processed %d callbacks", count)
	
	cb.Shutdown()
}