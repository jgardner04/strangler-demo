# Circuit Breaker Pattern Implementation

This document describes the circuit breaker implementation in the strangler pattern demo, providing resilience against service failures and preventing cascading outages.

## Overview

Circuit breakers are implemented to protect external service calls to both SAP and Order Service. When a service fails repeatedly, the circuit breaker "opens" and returns errors immediately instead of waiting for timeouts, providing fast failure and preventing resource exhaustion.

## Architecture

```
┌─────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  E-commerce │───▶│     Proxy       │───▶│  Order Service  │
│   System    │    │    Service      │    │  (Circuit       │
└─────────────┘    │                 │    │   Protected)    │
                   │  ┌─────────────┐ │    └─────────────────┘
                   │  │   Circuit   │ │
                   │  │   Breaker   │ │    ┌─────────────────┐
                   │  │   Manager   │ │───▶│   SAP Service   │
                   │  └─────────────┘ │    │  (Circuit       │
                   └─────────────────┘    │   Protected)    │
                                          └─────────────────┘
```

## Circuit Breaker States

### 1. Closed (Normal Operation)

- All requests pass through to the target service
- Failure count is tracked
- Circuit opens when failure threshold is exceeded

### 2. Open (Service Failing)

- All requests fail immediately with `ErrCircuitBreakerOpen`
- No requests are sent to the failing service
- Prevents resource exhaustion and cascading failures
- After timeout period, circuit transitions to Half-Open

### 3. Half-Open (Testing Recovery)

- Limited number of requests are allowed through
- If requests succeed, circuit closes
- If requests fail, circuit opens again
- Allows automatic recovery detection

## Configuration

Circuit breakers are configured via environment variables:

### SAP Circuit Breaker

```bash
SAP_CB_MAX_FAILURES=3          # failures before opening circuit
SAP_CB_TIMEOUT_SECONDS=10      # seconds to wait before testing recovery
SAP_CB_MAX_REQUESTS=2          # max requests allowed in half-open state
SAP_HTTP_TIMEOUT_SECONDS=10    # HTTP client timeout (default: 10s)
```

### Order Service Circuit Breaker

```bash
ORDER_SERVICE_CB_MAX_FAILURES=5      # more tolerant of transient failures
ORDER_SERVICE_CB_TIMEOUT_SECONDS=15  # longer recovery test period
ORDER_SERVICE_CB_MAX_REQUESTS=3      # allow more test requests
ORDER_SERVICE_HTTP_TIMEOUT_SECONDS=15  # HTTP client timeout (default: 15s)
```

### Timeout Strategy

**Different timeout values reflect service characteristics:**

- **SAP (Legacy System)**: 10-second timeouts

  - Legacy systems typically have predictable but slower response patterns
  - Shorter circuit breaker timeout for faster failure detection
  - Lower failure threshold due to expected stability

- **Order Service (Microservice)**: 15-second timeouts
  - Modern microservice with variable load patterns
  - Higher failure threshold to account for transient issues
  - Longer recovery period to allow for proper health stabilization

### Default Values

If environment variables are not set, the following defaults are used:

| Service       | Max Failures | Timeout | Max Requests |
| ------------- | ------------ | ------- | ------------ |
| SAP           | 3            | 10s     | 2            |
| Order Service | 5            | 15s     | 3            |

## Implementation Details

### Core Components

**`internal/circuitbreaker/circuit_breaker.go`**

- Main circuit breaker implementation
- State management and transition logic
- Metrics collection and failure tracking

**`internal/circuitbreaker/manager.go`**

- Manages multiple circuit breakers
- Central registry for all circuit breakers
- Provides factory methods and bulk operations

### Protected Services

**SAP Client (`internal/sap/client.go`)**

- All HTTP calls wrapped with circuit breaker
- Methods: `CreateOrder()`, `GetOrders()`, `GetOrder()`

**Order Service Client (`internal/orders/client.go`)**

- All HTTP calls wrapped with circuit breaker
- Methods: `CreateOrder()`, `CreateOrderHistorical()`, `GetOrders()`, `GetOrder()`

### Circuit Breaker Execution

```go
err := circuitBreaker.Execute(func() error {
    // Make HTTP request to external service
    resp, err := httpClient.Do(req)
    if err != nil {
        return err  // Will increment failure count
    }
    // Process response
    return nil  // Will reset failure count
})
```

## Monitoring and Observability

### Metrics Endpoint

**GET** `/metrics/circuit-breakers`

Returns comprehensive metrics for all circuit breakers:

```json
{
  "circuit_breakers": {
    "sap": {
      "name": "sap",
      "state": "closed",
      "failures": 0,
      "requests": 0,
      "total_requests": 45,
      "total_failures": 2,
      "total_successes": 43,
      "state_changes": 1,
      "max_failures": 3,
      "timeout_seconds": 10,
      "max_requests": 2,
      "last_failure": "2025-06-14T10:15:30Z",
      "last_state_change": "2025-06-14T10:16:00Z"
    },
    "order-service": {
      // Similar structure
    }
  },
  "timestamp": "2025-06-14T10:25:00Z"
}
```

### Key Metrics

- **state**: Current circuit breaker state (closed/open/half-open)
- **failures**: Current consecutive failure count
- **total_requests**: Total requests processed
- **total_failures**: Total failed requests
- **total_successes**: Total successful requests
- **state_changes**: Number of state transitions
- **last_failure**: Timestamp of most recent failure
- **last_state_change**: Timestamp of most recent state change

### Management Endpoints

**Reset All Circuit Breakers**

```bash
curl -X POST http://localhost:8080/circuit-breakers/reset
```

**Reset Specific Circuit Breaker**

```bash
curl -X POST http://localhost:8080/circuit-breakers/reset/sap
curl -X POST http://localhost:8080/circuit-breakers/reset/order-service
```

## Testing

### Test Script

Use the provided test script to demonstrate circuit breaker functionality:

```bash
./scripts/test-circuit-breaker.sh
```

This script:

- Shows initial circuit breaker states
- Creates test orders to demonstrate normal operation
- Provides instructions for testing failure scenarios
- Shows circuit breaker metrics and recovery

### Manual Testing

**1. Normal Operation**

```bash
# View circuit breaker metrics
curl http://localhost:8080/metrics/circuit-breakers | jq

# Create order (should succeed)
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "test", "items": [{"product_id": "widget", "quantity": 1, "unit_price": 10}], "total_amount": 10}'
```

**2. Simulate Service Failure**

```bash
# Stop SAP service to trigger circuit breaker
docker-compose stop sap-mock

# Try to create order (will fail after threshold)
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "test", "items": [{"product_id": "widget", "quantity": 1, "unit_price": 10}], "total_amount": 10}'

# Check circuit breaker state (should show "open")
curl http://localhost:8080/metrics/circuit-breakers | jq
```

**3. Test Recovery**

```bash
# Restart SAP service
docker-compose start sap-mock

# Wait for timeout period, then test
sleep 15

# Try order creation (circuit should test recovery)
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "test", "items": [{"product_id": "widget", "quantity": 1, "unit_price": 10}], "total_amount": 10}'

# Check if circuit closed
curl http://localhost:8080/metrics/circuit-breakers | jq
```

## Error Handling

### Circuit Breaker Open Error

When a circuit breaker is open, requests fail immediately with:

```json
{
  "success": false,
  "message": "Failed to create order in SAP",
  "error": "circuit breaker is open"
}
```

### Logging

Circuit breaker events are logged with structured logging:

```json
{
  "level": "info",
  "msg": "Circuit breaker state changed",
  "circuit_breaker": "sap",
  "from_state": "closed",
  "to_state": "open",
  "timestamp": "2025-06-14T10:15:30Z"
}
```

```json
{
  "level": "error",
  "msg": "Failed to create order in SAP",
  "order_id": "order-123",
  "error": "circuit breaker is open",
  "circuit_breaker_state": "open",
  "timestamp": "2025-06-14T10:15:31Z"
}
```

## Benefits

1. **Fast Failure**: No waiting for timeouts when services are down
2. **Resource Protection**: Prevents thread/connection pool exhaustion
3. **Cascading Failure Prevention**: Stops failures from propagating
4. **Automatic Recovery**: Services resume automatically when healthy
5. **Observability**: Real-time visibility into service health
6. **Configurability**: Tune behavior per service requirements

## Best Practices

1. **Set Appropriate Thresholds**: Balance between early detection and false positives
2. **Monitor Circuit States**: Use metrics to understand service behavior
3. **Test Recovery Scenarios**: Ensure circuits close when services recover
4. **Log State Changes**: Track circuit breaker events for debugging
5. **Graceful Degradation**: Provide fallback responses when circuits are open

## Integration with Strangler Pattern

Circuit breakers enhance the strangler pattern by:

- **Protecting the proxy layer** during migration phases
- **Preventing legacy system failures** from affecting new services
- **Enabling safe rollback** if new services fail
- **Providing visibility** into service health during migration
- **Supporting gradual cutover** with failure isolation

This implementation ensures that the strangler pattern migration is resilient and can handle service failures gracefully while maintaining system stability.
