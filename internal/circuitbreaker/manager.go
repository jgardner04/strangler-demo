package circuitbreaker

import (
	"sync"

	"github.com/sirupsen/logrus"
)

type Manager struct {
	breakers map[string]*CircuitBreaker
	mutex    sync.RWMutex
	logger   *logrus.Logger
}

func NewManager(logger *logrus.Logger) *Manager {
	return &Manager{
		breakers: make(map[string]*CircuitBreaker),
		logger:   logger,
	}
}

func (m *Manager) GetOrCreate(name string, config Config) *CircuitBreaker {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	if breaker, exists := m.breakers[name]; exists {
		return breaker
	}
	
	config.Name = name
	breaker := New(config, m.logger)
	m.breakers[name] = breaker
	
	m.logger.WithFields(logrus.Fields{
		"circuit_breaker": name,
		"max_failures": config.MaxFailures,
		"timeout": config.Timeout.String(),
		"max_requests": config.MaxRequests,
	}).Info("Circuit breaker created")
	
	return breaker
}

func (m *Manager) Get(name string) *CircuitBreaker {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	return m.breakers[name]
}

func (m *Manager) GetAllMetrics() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	metrics := make(map[string]interface{})
	for name, breaker := range m.breakers {
		metrics[name] = breaker.Metrics()
	}
	
	return metrics
}

func (m *Manager) ResetAll() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	for _, breaker := range m.breakers {
		breaker.Reset()
	}
	
	m.logger.Info("All circuit breakers reset")
}

func (m *Manager) Reset(name string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	if breaker, exists := m.breakers[name]; exists {
		breaker.Reset()
		m.logger.WithField("circuit_breaker", name).Info("Circuit breaker reset")
		return true
	}
	
	return false
}