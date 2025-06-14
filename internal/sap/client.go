package sap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jogardn/strangler-demo/internal/circuitbreaker"
	"github.com/jogardn/strangler-demo/pkg/models"
	"github.com/sirupsen/logrus"
)

type Client struct {
	baseURL        string
	httpClient     *http.Client
	logger         *logrus.Logger
	circuitBreaker *circuitbreaker.CircuitBreaker
}

func NewClient(baseURL string, logger *logrus.Logger, cbManager *circuitbreaker.Manager) *Client {
	cb := cbManager.Get("sap")

	// HTTP timeout should be shorter than circuit breaker timeout for proper coordination
	// SAP typically has slower response times due to legacy system complexity
	// Default: 10 seconds (vs 15 seconds for modern Order Service)
	httpTimeout := getHTTPTimeout("SAP_HTTP_TIMEOUT_SECONDS", "10", logger)

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		logger:         logger,
		circuitBreaker: cb,
	}
}

func (c *Client) CreateOrder(order *models.Order) (*models.OrderResponse, error) {
	c.logger.WithField("order_id", order.ID).Info("Sending order to SAP")

	var orderResp *models.OrderResponse
	err := c.circuitBreaker.Execute(func() error {
		jsonData, err := json.Marshal(order)
		if err != nil {
			return fmt.Errorf("failed to marshal order: %w", err)
		}

		req, err := http.NewRequest("POST", c.baseURL+"/orders", bytes.NewBuffer(jsonData))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request to SAP: %w", err)
		}
		defer resp.Body.Close()

		var respData models.OrderResponse
		if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
			return fmt.Errorf("failed to decode SAP response: %w", err)
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("SAP returned error status: %d", resp.StatusCode)
		}

		orderResp = &respData

		c.logger.WithFields(logrus.Fields{
			"order_id": order.ID,
			"status":   resp.StatusCode,
			"success":  respData.Success,
		}).Info("Received response from SAP")

		return nil
	})

	if err != nil {
		c.logger.WithFields(logrus.Fields{
			"order_id": order.ID,
			"error": err.Error(),
			"circuit_breaker_state": c.circuitBreaker.State().String(),
		}).Error("Failed to create order in SAP")
		return nil, err
	}

	return orderResp, nil
}

func (c *Client) GetOrders() ([]models.Order, error) {
	c.logger.Info("Fetching orders from SAP")

	var orders []models.Order
	err := c.circuitBreaker.Execute(func() error {
		req, err := http.NewRequest("GET", c.baseURL+"/orders", nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request to SAP: %w", err)
		}
		defer resp.Body.Close()

		var response struct {
			Success bool            `json:"success"`
			Orders  []models.Order  `json:"orders"`
			Count   int             `json:"count"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return fmt.Errorf("failed to decode SAP response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("SAP returned error status: %d", resp.StatusCode)
		}

		orders = response.Orders
		c.logger.WithField("count", response.Count).Info("Retrieved orders from SAP")
		return nil
	})

	if err != nil {
		c.logger.WithFields(logrus.Fields{
			"error": err.Error(),
			"circuit_breaker_state": c.circuitBreaker.State().String(),
		}).Error("Failed to get orders from SAP")
		return nil, err
	}

	return orders, nil
}

func (c *Client) GetOrder(orderID string) (*models.Order, error) {
	c.logger.WithField("order_id", orderID).Info("Fetching order from SAP")

	var order *models.Order
	err := c.circuitBreaker.Execute(func() error {
		req, err := http.NewRequest("GET", c.baseURL+"/orders/"+orderID, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request to SAP: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("order not found in SAP")
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("SAP returned error status: %d", resp.StatusCode)
		}

		var orderData models.Order
		if err := json.NewDecoder(resp.Body).Decode(&orderData); err != nil {
			return fmt.Errorf("failed to decode SAP response: %w", err)
		}

		order = &orderData
		c.logger.WithField("order_id", orderID).Info("Retrieved order from SAP")
		return nil
	})

	if err != nil {
		c.logger.WithFields(logrus.Fields{
			"order_id": orderID,
			"error": err.Error(),
			"circuit_breaker_state": c.circuitBreaker.State().String(),
		}).Error("Failed to get order from SAP")
		return nil, err
	}

	return order, nil
}

func getHTTPTimeout(envVar, defaultValue string, logger *logrus.Logger) time.Duration {
	value := os.Getenv(envVar)
	if value == "" {
		value = defaultValue
	}

	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		logger.WithFields(logrus.Fields{
			"env_var": envVar,
			"value": value,
			"default": defaultValue,
			"error": err,
		}).Warn("Invalid HTTP timeout value, using default")

		defaultSeconds, defaultErr := strconv.Atoi(defaultValue)
		if defaultErr != nil || defaultSeconds <= 0 {
			logger.WithFields(logrus.Fields{
				"env_var": envVar,
				"default": defaultValue,
			}).Error("Invalid default HTTP timeout, using 5 seconds")
			return 5 * time.Second
		}
		return time.Duration(defaultSeconds) * time.Second
	}

	// Cap at reasonable maximum
	if seconds > 300 { // 5 minutes
		logger.WithFields(logrus.Fields{
			"env_var": envVar,
			"value": seconds,
			"max_allowed": 300,
		}).Warn("HTTP timeout too high, capping at 5 minutes")
		seconds = 300
	}

	return time.Duration(seconds) * time.Second
}
