package circuitbreaker

import (
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		expectedName   string
		expectedMax    int
		expectedTimeout time.Duration
		expectedReqs   int
		shouldPanic    bool
	}{
		{
			name: "valid_config",
			config: Config{
				Name:        "test-cb",
				MaxFailures: 5,
				Timeout:     30 * time.Second,
				MaxRequests: 3,
			},
			expectedName:    "test-cb",
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    3,
		},
		{
			name: "empty_name_gets_default",
			config: Config{
				Name:        "",
				MaxFailures: 5,
				Timeout:     30 * time.Second,
				MaxRequests: 3,
			},
			expectedName:    "unnamed",
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    3,
		},
		{
			name: "negative_max_failures_gets_default",
			config: Config{
				Name:        "test",
				MaxFailures: -1,
				Timeout:     30 * time.Second,
				MaxRequests: 3,
			},
			expectedName:    "test",
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    3,
		},
		{
			name: "zero_max_failures_gets_default",
			config: Config{
				Name:        "test",
				MaxFailures: 0,
				Timeout:     30 * time.Second,
				MaxRequests: 3,
			},
			expectedName:    "test",
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    3,
		},
		{
			name: "negative_timeout_gets_default",
			config: Config{
				Name:        "test",
				MaxFailures: 5,
				Timeout:     -1 * time.Second,
				MaxRequests: 3,
			},
			expectedName:    "test",
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    3,
		},
		{
			name: "zero_timeout_gets_default",
			config: Config{
				Name:        "test",
				MaxFailures: 5,
				Timeout:     0,
				MaxRequests: 3,
			},
			expectedName:    "test",
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    3,
		},
		{
			name: "timeout_too_small_gets_minimum",
			config: Config{
				Name:        "test",
				MaxFailures: 5,
				Timeout:     50 * time.Millisecond,
				MaxRequests: 3,
			},
			expectedName:    "test",
			expectedMax:     5,
			expectedTimeout: 100 * time.Millisecond,
			expectedReqs:    3,
		},
		{
			name: "negative_max_requests_gets_default",
			config: Config{
				Name:        "test",
				MaxFailures: 5,
				Timeout:     30 * time.Second,
				MaxRequests: -1,
			},
			expectedName:    "test",
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    1,
		},
		{
			name: "zero_max_requests_gets_default",
			config: Config{
				Name:        "test",
				MaxFailures: 5,
				Timeout:     30 * time.Second,
				MaxRequests: 0,
			},
			expectedName:    "test",
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    1,
		},
		{
			name: "max_failures_too_high_gets_capped",
			config: Config{
				Name:        "test",
				MaxFailures: 2000,
				Timeout:     30 * time.Second,
				MaxRequests: 3,
			},
			expectedName:    "test",
			expectedMax:     1000,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    3,
		},
		{
			name: "timeout_too_high_gets_capped",
			config: Config{
				Name:        "test",
				MaxFailures: 5,
				Timeout:     20 * time.Minute,
				MaxRequests: 3,
			},
			expectedName:    "test",
			expectedMax:     5,
			expectedTimeout: 10 * time.Minute,
			expectedReqs:    3,
		},
		{
			name: "max_requests_too_high_gets_capped",
			config: Config{
				Name:        "test",
				MaxFailures: 5,
				Timeout:     30 * time.Second,
				MaxRequests: 200,
			},
			expectedName:    "test",
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    100,
		},
		{
			name: "very_long_name_gets_truncated",
			config: Config{
				Name:        strings.Repeat("a", 150),
				MaxFailures: 5,
				Timeout:     30 * time.Second,
				MaxRequests: 3,
			},
			expectedName:    strings.Repeat("a", 100),
			expectedMax:     5,
			expectedTimeout: 30 * time.Second,
			expectedReqs:    3,
		},
		{
			name: "nil_logger_panics",
			config: Config{
				Name:        "test",
				MaxFailures: 5,
				Timeout:     30 * time.Second,
				MaxRequests: 3,
			},
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Error("Expected panic for nil logger")
					}
				}()
				_ = New(tt.config, nil)
				return
			}

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)
			
			cb := New(tt.config, logger)

			if cb.name != tt.expectedName {
				t.Errorf("Expected name %q, got %q", tt.expectedName, cb.name)
			}
			if cb.maxFailures != tt.expectedMax {
				t.Errorf("Expected maxFailures %d, got %d", tt.expectedMax, cb.maxFailures)
			}
			if cb.timeout != tt.expectedTimeout {
				t.Errorf("Expected timeout %v, got %v", tt.expectedTimeout, cb.timeout)
			}
			if cb.maxRequests != tt.expectedReqs {
				t.Errorf("Expected maxRequests %d, got %d", tt.expectedReqs, cb.maxRequests)
			}
		})
	}
}

func TestConfigWithStateChangeCallback(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	callbackCalled := false
	config := Config{
		Name:        "callback-test",
		MaxFailures: 3,
		Timeout:     30 * time.Second,
		MaxRequests: 2,
		OnStateChange: func(name string, from State, to State) {
			callbackCalled = true
		},
	}

	cb := New(config, logger)

	// Verify callback pool was initialized
	if cb.callbackPool == nil {
		t.Error("Expected callback pool to be initialized when OnStateChange is provided")
	}

	// Verify callback is not called during initialization
	if callbackCalled {
		t.Error("Callback should not be called during initialization")
	}

	cb.Shutdown()
}

func TestConfigWithoutStateChangeCallback(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := Config{
		Name:        "no-callback-test",
		MaxFailures: 3,
		Timeout:     30 * time.Second,
		MaxRequests: 2,
		OnStateChange: nil,
	}

	cb := New(config, logger)

	// Verify callback pool was not initialized
	if cb.callbackPool != nil {
		t.Error("Expected callback pool to be nil when OnStateChange is not provided")
	}

	// Should not panic when state changes without callback
	cb.setState(StateOpen)
	cb.setState(StateClosed)

	cb.Shutdown() // Should not panic even without callback pool
}

func TestConfigCoherenceWarning(t *testing.T) {
	// Create a logger that captures output
	var logBuffer strings.Builder
	logger := logrus.New()
	logger.SetOutput(&logBuffer)
	logger.SetLevel(logrus.WarnLevel)

	config := Config{
		Name:        "coherence-test",
		MaxFailures: 3,
		Timeout:     30 * time.Second,
		MaxRequests: 5, // Greater than MaxFailures
	}

	_ = New(config, logger)

	output := logBuffer.String()
	if !strings.Contains(output, "MaxRequests is greater than MaxFailures") {
		t.Error("Expected warning about MaxRequests > MaxFailures")
	}
}

func TestWorkerPoolConfiguration(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Test default worker pool size
	cb1 := New(Config{
		Name:        "default-workers",
		MaxFailures: 3,
		Timeout:     30 * time.Second,
		MaxRequests: 2,
		OnStateChange: func(name string, from State, to State) {},
	}, logger)

	if cb1.callbackPool == nil {
		t.Fatal("Expected callback pool to be initialized")
	}
	if cb1.callbackPool.workers != 2 {
		t.Errorf("Expected default 2 workers, got %d", cb1.callbackPool.workers)
	}

	cb1.Shutdown()

	// Test worker pool limits (this requires modifying newCallbackWorkerPool to accept workers parameter)
	// For now, we'll just verify the pool is created correctly
	cb2 := New(Config{
		Name:        "test-workers",
		MaxFailures: 3,
		Timeout:     30 * time.Second,
		MaxRequests: 2,
		OnStateChange: func(name string, from State, to State) {},
	}, logger)

	// Verify channel buffer size
	if cap(cb2.callbackPool.eventChan) != 4 { // workers * 2 = 2 * 2 = 4
		t.Errorf("Expected event channel capacity 4, got %d", cap(cb2.callbackPool.eventChan))
	}

	cb2.Shutdown()
}

func TestAllConfigurationFields(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	var logBuffer strings.Builder
	logger.SetOutput(&logBuffer)

	config := Config{
		Name:        "full-config-test",
		MaxFailures: 5,
		Timeout:     1 * time.Minute,
		MaxRequests: 3,
		OnStateChange: func(name string, from State, to State) {},
	}

	cb := New(config, logger)

	// Check all fields were set correctly
	if cb.name != config.Name {
		t.Errorf("Name mismatch: expected %s, got %s", config.Name, cb.name)
	}
	if cb.maxFailures != config.MaxFailures {
		t.Errorf("MaxFailures mismatch: expected %d, got %d", config.MaxFailures, cb.maxFailures)
	}
	if cb.timeout != config.Timeout {
		t.Errorf("Timeout mismatch: expected %v, got %v", config.Timeout, cb.timeout)
	}
	if cb.maxRequests != config.MaxRequests {
		t.Errorf("MaxRequests mismatch: expected %d, got %d", config.MaxRequests, cb.maxRequests)
	}
	if cb.onStateChange == nil {
		t.Error("OnStateChange callback was not set")
	}
	if cb.state != StateClosed {
		t.Errorf("Initial state should be StateClosed, got %s", cb.state)
	}
	if cb.logger != logger {
		t.Error("Logger was not set correctly")
	}

	// Verify debug log was written
	output := logBuffer.String()
	if !strings.Contains(output, "Circuit breaker created with configuration") {
		t.Error("Expected debug log message about configuration")
	}

	cb.Shutdown()
}