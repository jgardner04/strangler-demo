package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/jogardn/strangler-demo/internal/circuitbreaker"
	"github.com/jogardn/strangler-demo/internal/orders"
	"github.com/jogardn/strangler-demo/internal/sap"
	"github.com/jogardn/strangler-demo/internal/websocket"
	"github.com/sirupsen/logrus"
)

func main() {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)

	port := getEnv("PROXY_PORT", "8080")
	sapURL := getEnv("SAP_URL", "http://localhost:8082")
	orderServiceURL := getEnv("ORDER_SERVICE_URL", "")

	// Create circuit breaker manager
	cbManager := circuitbreaker.NewManager(logger)

	// Configure circuit breaker settings for SAP
	sapCBConfig := getSAPCircuitBreakerConfig(logger)
	sapClient := sapClientWithCircuitBreaker(sapURL, logger, cbManager, sapCBConfig)

	var orderServiceClient *orders.OrderServiceClient
	if orderServiceURL != "" {
		// Configure circuit breaker settings for Order Service
		orderServiceCBConfig := getOrderServiceCircuitBreakerConfig(logger)
		orderServiceClient = orderServiceClientWithCircuitBreaker(orderServiceURL, logger, cbManager, orderServiceCBConfig)
		logger.WithField("url", orderServiceURL).Info("Order service client configured")
	} else {
		logger.Info("Order service URL not configured - running in Phase 1 mode")
	}

	// Create WebSocket hub
	wsHub := websocket.NewHub(logger)
	go wsHub.Run()

	orderHandler := orders.NewHandler(sapClient, orderServiceClient, logger)
	orderHandler.SetWebSocketHub(wsHub)

	router := mux.NewRouter()
	router.HandleFunc("/health", orderHandler.HealthCheck).Methods("GET", "OPTIONS")
	router.HandleFunc("/api/health/all", allServicesHealthCheck(sapClient, orderServiceClient, cbManager, logger)).Methods("GET", "OPTIONS")
	router.HandleFunc("/orders", orderHandler.CreateOrder).Methods("POST", "OPTIONS")
	router.HandleFunc("/orders", orderHandler.GetOrders).Methods("GET", "OPTIONS")
	router.HandleFunc("/compare/orders", orderHandler.CompareOrders).Methods("GET", "OPTIONS")
	router.HandleFunc("/compare/orders/{id}", orderHandler.CompareOrder).Methods("GET", "OPTIONS")
	router.HandleFunc("/metrics/circuit-breakers", circuitBreakerMetrics(cbManager)).Methods("GET", "OPTIONS")
	router.HandleFunc("/circuit-breakers/reset", resetCircuitBreakers(cbManager, logger)).Methods("POST", "OPTIONS")
	router.HandleFunc("/circuit-breakers/reset/{name}", resetCircuitBreaker(cbManager, logger)).Methods("POST", "OPTIONS")
	router.HandleFunc("/ws", wsHub.HandleWebSocket)

	router.Use(corsMiddleware())
	router.Use(loggingMiddleware(logger))

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.WithField("port", port).Info("Starting proxy server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("Failed to start server")
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Server forced to shutdown")
	}

	logger.Info("Server gracefully stopped")
}

func loggingMiddleware(logger *logrus.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			logger.WithFields(logrus.Fields{
				"method": r.Method,
				"path":   r.URL.Path,
				"remote": r.RemoteAddr,
			}).Info("Request received")

			next.ServeHTTP(w, r)

			logger.WithFields(logrus.Fields{
				"method":   r.Method,
				"path":     r.URL.Path,
				"duration": time.Since(start).Milliseconds(),
			}).Info("Request completed")
		})
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func corsMiddleware() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow all origins for development
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func allServicesHealthCheck(sapClient *sap.Client, orderServiceClient *orders.OrderServiceClient, cbManager *circuitbreaker.Manager, logger *logrus.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		healthStatus := make(map[string]interface{})

		// Check proxy health (always healthy if running)
		healthStatus["proxy"] = map[string]interface{}{
			"status": "healthy",
			"service": "proxy",
			"response_time": 0,
			"last_check": time.Now().Format(time.RFC3339),
		}

		// Check order service health
		if orderServiceClient != nil {
			start := time.Now()
			orderServiceCB := cbManager.Get("order-service")

			// Try to get orders to check if service is healthy
			_, err := orderServiceClient.GetOrders()
			responseTime := time.Since(start).Milliseconds()

			cbState := "unknown"
			cbMetrics := map[string]interface{}{}
			if orderServiceCB != nil {
				cbState = orderServiceCB.State().String()
				cbMetrics = orderServiceCB.Metrics()
			}

			if err == nil {
				healthStatus["order_service"] = map[string]interface{}{
					"status": "healthy",
					"service": "order_service",
					"response_time": responseTime,
					"last_check": time.Now().Format(time.RFC3339),
					"circuit_breaker": map[string]interface{}{
						"state": cbState,
						"metrics": cbMetrics,
					},
				}
			} else {
				healthStatus["order_service"] = map[string]interface{}{
					"status": "unhealthy",
					"service": "order_service",
					"error": err.Error(),
					"response_time": responseTime,
					"last_check": time.Now().Format(time.RFC3339),
					"circuit_breaker": map[string]interface{}{
						"state": cbState,
						"metrics": cbMetrics,
					},
				}
			}
		} else {
			healthStatus["order_service"] = map[string]interface{}{
				"status": "unavailable",
				"service": "order_service",
				"error": "Service not configured",
				"last_check": time.Now().Format(time.RFC3339),
				"circuit_breaker": map[string]interface{}{
					"state": "not_configured",
					"metrics": map[string]interface{}{},
				},
			}
		}

		// Check SAP health
		start := time.Now()
		sapCB := cbManager.Get("sap")
		_, err := sapClient.GetOrders()
		responseTime := time.Since(start).Milliseconds()

		cbState := "unknown"
		cbMetrics := map[string]interface{}{}
		if sapCB != nil {
			cbState = sapCB.State().String()
			cbMetrics = sapCB.Metrics()
		}

		if err == nil {
			healthStatus["sap_mock"] = map[string]interface{}{
				"status": "healthy",
				"service": "sap_mock",
				"response_time": responseTime,
				"last_check": time.Now().Format(time.RFC3339),
				"circuit_breaker": map[string]interface{}{
					"state": cbState,
					"metrics": cbMetrics,
				},
			}
		} else {
			healthStatus["sap_mock"] = map[string]interface{}{
				"status": "unhealthy",
				"service": "sap_mock",
				"error": err.Error(),
				"response_time": responseTime,
				"last_check": time.Now().Format(time.RFC3339),
				"circuit_breaker": map[string]interface{}{
					"state": cbState,
					"metrics": cbMetrics,
				},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(healthStatus)
	}
}

func circuitBreakerMetrics(cbManager *circuitbreaker.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		metrics := cbManager.GetAllMetrics()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"circuit_breakers": metrics,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

func resetCircuitBreakers(cbManager *circuitbreaker.Manager, logger *logrus.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cbManager.ResetAll()
		logger.Info("All circuit breakers reset via API")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "All circuit breakers reset",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

func resetCircuitBreaker(cbManager *circuitbreaker.Manager, logger *logrus.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		name := vars["name"]

		if name == "" {
			http.Error(w, "Circuit breaker name is required", http.StatusBadRequest)
			return
		}

		success := cbManager.Reset(name)
		if !success {
			http.Error(w, "Circuit breaker not found", http.StatusNotFound)
			return
		}

		logger.WithField("circuit_breaker", name).Info("Circuit breaker reset via API")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Circuit breaker reset",
			"name": name,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

func parseIntWithDefault(envVar, defaultValue string, logger *logrus.Logger) int {
	value := getEnv(envVar, defaultValue)
	parsed, err := strconv.Atoi(value)
	if err != nil {
		logger.WithFields(logrus.Fields{
			"env_var": envVar,
			"value": value,
			"default": defaultValue,
			"error": err.Error(),
		}).Warn("Failed to parse environment variable as integer, using default")

		// Parse the default value - this should always work
		defaultParsed, defaultErr := strconv.Atoi(defaultValue)
		if defaultErr != nil {
			logger.WithFields(logrus.Fields{
				"env_var": envVar,
				"default": defaultValue,
				"error": defaultErr.Error(),
			}).Error("Failed to parse default value as integer")
			return 1 // Fallback to safe default
		}
		return defaultParsed
	}
	return parsed
}

func getSAPCircuitBreakerConfig(logger *logrus.Logger) circuitbreaker.Config {
	maxFailures := parseIntWithDefault("SAP_CB_MAX_FAILURES", "3", logger)
	if maxFailures < 1 {
		logger.Warn("SAP_CB_MAX_FAILURES must be > 0; using 1")
		maxFailures = 1
	}
	timeout := parseIntWithDefault("SAP_CB_TIMEOUT_SECONDS", "10", logger)
	if timeout < 1 {
		logger.Warn("SAP_CB_TIMEOUT_SECONDS must be > 0; using 1")
		timeout = 1
	}
	maxRequests := parseIntWithDefault("SAP_CB_MAX_REQUESTS", "2", logger)
	if maxRequests < 1 {
		logger.Warn("SAP_CB_MAX_REQUESTS must be > 0; using 1")
		maxRequests = 1
	}
	return circuitbreaker.Config{
		MaxFailures: maxFailures,
		Timeout:     time.Duration(timeout) * time.Second,
		MaxRequests: maxRequests,
	}
}

func getOrderServiceCircuitBreakerConfig(logger *logrus.Logger) circuitbreaker.Config {
	maxFailures := parseIntWithDefault("ORDER_SERVICE_CB_MAX_FAILURES", "5", logger)
	if maxFailures < 1 {
		logger.Warn("ORDER_SERVICE_CB_MAX_FAILURES must be > 0; using 1")
		maxFailures = 1
	}
	timeout := parseIntWithDefault("ORDER_SERVICE_CB_TIMEOUT_SECONDS", "15", logger)
	if timeout < 1 {
		logger.Warn("ORDER_SERVICE_CB_TIMEOUT_SECONDS must be > 0; using 1")
		timeout = 1
	}
	maxRequests := parseIntWithDefault("ORDER_SERVICE_CB_MAX_REQUESTS", "3", logger)
	if maxRequests < 1 {
		logger.Warn("ORDER_SERVICE_CB_MAX_REQUESTS must be > 0; using 1")
		maxRequests = 1
	}
	return circuitbreaker.Config{
		MaxFailures: maxFailures,
		Timeout:     time.Duration(timeout) * time.Second,
		MaxRequests: maxRequests,
	}
}

func sapClientWithCircuitBreaker(baseURL string, logger *logrus.Logger, cbManager *circuitbreaker.Manager, config circuitbreaker.Config) *sap.Client {
	cbManager.GetOrCreate("sap", config)
	return sap.NewClient(baseURL, logger, cbManager)
}

func orderServiceClientWithCircuitBreaker(baseURL string, logger *logrus.Logger, cbManager *circuitbreaker.Manager, config circuitbreaker.Config) *orders.OrderServiceClient {
	cbManager.GetOrCreate("order-service", config)
	return orders.NewOrderServiceClient(baseURL, logger, cbManager)
}
