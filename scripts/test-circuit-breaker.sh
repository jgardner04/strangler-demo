#!/bin/bash

# Test script for circuit breaker functionality
# This script demonstrates how the circuit breaker protects against failing services

PROXY_URL="http://localhost:8080"
METRICS_URL="$PROXY_URL/metrics/circuit-breakers"
RESET_URL="$PROXY_URL/circuit-breakers/reset"

echo "=== Circuit Breaker Test Script ==="
echo "Proxy URL: $PROXY_URL"
echo ""

# Function to check circuit breaker metrics
check_metrics() {
    echo "--- Circuit Breaker Metrics ---"
    curl -s "$METRICS_URL" | jq '.circuit_breakers' 2>/dev/null || curl -s "$METRICS_URL"
    echo ""
}

# Function to create an order (will fail if SAP is down)
create_order() {
    local order_id="test-order-$(date +%s)"
    echo "Creating order: $order_id"
    
    curl -s -X POST "$PROXY_URL/orders" \
        -H "Content-Type: application/json" \
        -d "{
            \"id\": \"$order_id\",
            \"customer_id\": \"customer-123\",
            \"items\": [{
                \"product_id\": \"widget-1\",
                \"quantity\": 2,
                \"unit_price\": 10.00,
                \"specifications\": {\"color\": \"blue\"}
            }],
            \"total_amount\": 20.00,
            \"delivery_date\": \"$(date -d '+7 days' '+%Y-%m-%dT%H:%M:%SZ')\",
            \"status\": \"pending\"
        }" | jq '.' 2>/dev/null || echo "Request failed or returned non-JSON response"
    echo ""
}

# Check initial metrics
echo "1. Initial circuit breaker state:"
check_metrics

# Create a few orders to test normal operation
echo "2. Testing normal operation (creating 3 orders):"
for i in {1..3}; do
    echo "  Order $i:"
    create_order
    sleep 1
done

# Check metrics after normal operation
echo "3. Circuit breaker state after normal operation:"
check_metrics

# Stop SAP service to trigger circuit breaker (in real scenario)
echo "4. To test circuit breaker failure protection:"
echo "   - Stop the SAP service: docker-compose stop sap-mock"
echo "   - Then run this script again to see circuit breaker protection"
echo "   - Restart SAP service: docker-compose start sap-mock"
echo ""

echo "5. Other useful commands:"
echo "   - View metrics: curl $METRICS_URL | jq"
echo "   - Reset all circuit breakers: curl -X POST $RESET_URL"
echo "   - Reset specific circuit breaker: curl -X POST $RESET_URL/sap"
echo "   - Check health: curl $PROXY_URL/api/health/all | jq"
echo ""

echo "=== Test completed ==="