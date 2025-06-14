package circuitbreaker

import (
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestManager(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	manager := NewManager(logger)

	// Test GetOrCreate
	config1 := Config{
		MaxFailures: 3,
		Timeout:     100 * time.Millisecond,
		MaxRequests: 2,
	}

	cb1 := manager.GetOrCreate("test-cb-1", config1)
	if cb1 == nil {
		t.Fatal("Expected circuit breaker, got nil")
	}

	// Getting the same circuit breaker should return the same instance
	cb1Again := manager.GetOrCreate("test-cb-1", config1)
	if cb1 != cb1Again {
		t.Error("Expected same circuit breaker instance")
	}

	// Create another circuit breaker
	config2 := Config{
		MaxFailures: 5,
		Timeout:     200 * time.Millisecond,
		MaxRequests: 3,
	}

	cb2 := manager.GetOrCreate("test-cb-2", config2)
	if cb2 == nil {
		t.Fatal("Expected circuit breaker, got nil")
	}

	if cb1 == cb2 {
		t.Error("Expected different circuit breaker instances")
	}

	// Test Get
	cbGet := manager.Get("test-cb-1")
	if cbGet != cb1 {
		t.Error("Expected to get cb1")
	}

	cbGetNil := manager.Get("non-existent")
	if cbGetNil != nil {
		t.Error("Expected nil for non-existent circuit breaker")
	}

	// Test GetAllMetrics
	metrics := manager.GetAllMetrics()
	if len(metrics) != 2 {
		t.Errorf("Expected 2 circuit breakers in metrics, got %d", len(metrics))
	}

	// Verify metrics contain our circuit breakers
	foundCb1 := false
	foundCb2 := false
	for cbName, m := range metrics {
		metricsMap := m.(map[string]interface{})
		name := metricsMap["name"].(string)
		if cbName == "test-cb-1" && name == "test-cb-1" {
			foundCb1 = true
		} else if cbName == "test-cb-2" && name == "test-cb-2" {
			foundCb2 = true
		}
	}

	if !foundCb1 || !foundCb2 {
		t.Error("Expected to find both circuit breakers in metrics")
	}

	// Test Reset specific circuit breaker
	// First cause a failure
	cb1.Execute(func() error {
		return errors.New("test failure")
	})

	metrics1 := cb1.Metrics()
	if metrics1["failures"].(int) != 1 {
		t.Error("Expected 1 failure")
	}

	// Reset it
	manager.Reset("test-cb-1")

	metrics1After := cb1.Metrics()
	if metrics1After["failures"].(int) != 0 {
		t.Error("Expected 0 failures after reset")
	}

	// Test ResetAll
	// Cause failures in both
	cb1.Execute(func() error {
		return errors.New("test failure")
	})
	cb2.Execute(func() error {
		return errors.New("test failure")
	})

	// Reset all
	manager.ResetAll()

	// Verify both are reset
	metrics1Final := cb1.Metrics()
	metrics2Final := cb2.Metrics()

	if metrics1Final["failures"].(int) != 0 {
		t.Error("Expected cb1 failures to be 0 after ResetAll")
	}
	if metrics2Final["failures"].(int) != 0 {
		t.Error("Expected cb2 failures to be 0 after ResetAll")
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	manager := NewManager(logger)

	// Test concurrent GetOrCreate
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			config := Config{
				MaxFailures: 3,
				Timeout:     100 * time.Millisecond,
				MaxRequests: 2,
			}
			cb := manager.GetOrCreate("concurrent-test", config)
			cb.Execute(func() error {
				return nil
			})
		}
		close(done)
	}()

	// Concurrent metrics reads
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_ = manager.GetAllMetrics()
			}
		}
	}()

	// Concurrent resets
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				manager.Reset("concurrent-test")
			}
		}
	}()

	<-done
}