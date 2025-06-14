# Strangler Pattern Demo

A comprehensive demonstration of the strangler pattern for gradually migrating from a monolithic SAP system to event-driven microservices architecture.

## Overview

This project demonstrates how to implement the strangler pattern by building a proxy service that sits between an e-commerce system and an SAP backend. The proxy gradually takes over functionality while maintaining backward compatibility.

## Current Implementation Status

✅ **Phase 1 Complete**: Basic proxy that passes requests to SAP with logging  
✅ **Phase 2 Complete**: Dual-write pattern with new order service, PostgreSQL, and Kafka events  
✅ **Phase 3 Complete**: Event-driven architecture with SAP consuming from Kafka  
✅ **Dashboard**: Real-time monitoring with WebSocket updates  
✅ **Data Tools**: Migration and validation CLI tools  
✅ **Circuit Breaker**: Resilience pattern protecting against service failures

## Architecture

**Phase 3: Event-Driven Architecture (Current)**

```
┌─────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  E-commerce │───▶│     Proxy       │───▶│  Order Service  │
│   System    │    │    Service      │    │   (Port 8081)   │
└─────────────┘    └─────────────────┘    └────────┬────────┘
                                                    │
                                                    │ Publishes
                                                    ▼
                   ┌─────────────────┐    ┌─────────────────┐
                   │   PostgreSQL    │    │     Kafka       │
                   │    Database     │    │ order.created   │
                   └─────────────────┘    └────────┬────────┘
                                                   │
                                                   │ Consumes
                                                   ▼
                                         ┌─────────────────┐
                                         │   SAP Mock      │
                                         │  (Consumer)     │
                                         └─────────────────┘
```

**Key Changes in Phase 3:**
- ✅ Proxy **no longer calls SAP directly**
- ✅ SAP Mock **consumes events from Kafka**
- ✅ Complete **event-driven decoupling**
- ✅ Orders flow: Client → Proxy → Order Service → Kafka → SAP

## Tech Stack

**Backend:**
- **Go 1.21+** - Main programming language
- **PostgreSQL** - Order service database
- **Kafka** - Event streaming platform
- **Gorilla Mux** - HTTP routing and WebSocket
- **Logrus** - Structured logging
- **Sarama** - Kafka client library

**Frontend:**
- **Next.js** - Dashboard framework
- **TypeScript** - Type-safe frontend development
- **Tailwind CSS** - Styling
- **WebSocket** - Real-time updates

**Infrastructure:**
- **Docker & Docker Compose** - Containerization
- **Nginx** - Load balancing (planned)

## Project Structure

```
strangler-demo/
├── cmd/
│   ├── proxy/          # Main proxy service with WebSocket
│   ├── order-service/  # New order microservice  
│   ├── sap-mock/       # Mock SAP service
│   └── data-tools/     # CLI for data migration & validation
├── dashboard/          # Next.js real-time monitoring dashboard
├── internal/
│   ├── orders/         # Order handling logic
│   ├── events/         # Kafka event publishing
│   ├── sap/            # SAP client integration
│   ├── websocket/      # WebSocket hub for real-time updates
│   ├── migration/      # Data migration utilities
│   ├── comparison/     # Data validation and comparison
│   └── circuitbreaker/ # Circuit breaker resilience pattern
├── pkg/
│   └── models/         # Shared data models
├── scripts/            # Test and demo scripts
└── docker-compose.yml  # Service orchestration
```

## Quick Start

### Prerequisites

- Docker and Docker Compose
- Go 1.21+ (for local development)
- jq (for JSON formatting in scripts)

### 1. Start All Services

```bash
# Start all services with Phase 3 event-driven architecture
docker-compose up --build
```

### 2. Access the Dashboard

The real-time monitoring dashboard is available at:

- **Dashboard**: http://localhost:3000
- **API Proxy**: http://localhost:8080  
- **Order Service**: http://localhost:8081

The dashboard provides:
- ✅ Real-time order monitoring with WebSocket updates
- ✅ Service health status across all components
- ✅ Performance metrics and charts
- ✅ Load testing interface
- ✅ Data synchronization comparison tools

### 3. Test the Implementation

```bash
# Basic functionality test
./scripts/test-order.sh

# Phase 3 event-driven demo
./scripts/demo-phase3.sh

# Data comparison verification
./scripts/compare-data.sh

# Circuit breaker testing
./scripts/test-circuit-breaker.sh

# Load testing
./scripts/load-test.sh
```

### 4. Load Testing

```bash
# Light load test (development)
./scripts/advanced-load-test.sh -s light

# Medium load test (integration)  
./scripts/advanced-load-test.sh -s medium

# Performance benchmark
./scripts/performance-benchmark.sh
```

## Service Endpoints

| Service | Port | Description | Health Check |
|---------|------|-------------|--------------|
| Proxy | 8080 | Main API endpoint | `GET /health` |
| Order Service | 8081 | New microservice | `GET /health` |
| SAP Mock | 8082 | Legacy system simulation | `GET /health` |
| Kafka UI | 8090 | Event monitoring | Web interface |
| PostgreSQL | 5432 | Database | Internal |

## Key Features

### ✅ Event-Driven Architecture (Phase 3)
- Proxy forwards orders to Order Service **only**
- SAP Mock **consumes events from Kafka**
- Complete decoupling of legacy system
- No direct HTTP calls to SAP

### ✅ Event Streaming
- Kafka events published for all order operations
- `order.created` events with full order details
- Event monitoring via Kafka UI
- Reliable event consumption with retry logic

### ✅ Circuit Breaker Resilience
- Protection against cascading failures
- Fast failure when services are down (no timeouts)
- Automatic recovery testing and circuit closing
- Independent circuit breakers for SAP and Order Service
- Real-time monitoring via `/metrics/circuit-breakers`
- Configurable thresholds and timeout settings

### ✅ Data Consistency
- Real-time consistency verification between systems
- Individual order comparison endpoints
- Event-driven synchronization
- Automated data consistency testing

### ✅ Performance & Resilience
- Improved response times (no SAP blocking)
- Asynchronous order processing
- Graceful degradation capabilities
- Comprehensive load testing suite

## API Documentation

### Core Order API

**Create Order**: `POST /orders`
```json
{
  "customer_id": "CUST-12345",
  "items": [{
    "product_id": "WIDGET-001",
    "quantity": 10,
    "unit_price": 25.99,
    "specifications": {
      "color": "blue",
      "finish": "matte"
    }
  }],
  "total_amount": 259.90,
  "delivery_date": "2025-06-20T00:00:00Z"
}
```

### Data Comparison API (Phase 2)

**Compare All Orders**: `GET /compare/orders`
- Returns comprehensive comparison between Order Service and SAP Mock
- Includes sync status, missing orders, and detailed analysis

**Compare Specific Order**: `GET /compare/orders/{id}`
- Field-by-field comparison of individual orders
- Perfect match verification

### Service-Specific APIs

**Order Service**: `GET /orders` - List all orders from PostgreSQL  
**SAP Mock**: `GET /orders` - List all orders from in-memory storage

For complete API documentation, see [API.md](API.md).

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PROXY_PORT` | 8080 | Proxy service port |
| `ORDER_SERVICE_PORT` | 8081 | Order service port |
| `SAP_URL` | http://sap-mock:8082 | SAP service endpoint |
| `ORDER_SERVICE_URL` | http://order-service:8081 | Order service endpoint |
| `DB_HOST` | postgres | Database host |
| `KAFKA_BROKERS` | kafka:29092 | Kafka broker list |

## Testing & Verification

### Automated Testing
```bash
# Complete verification suite
./scripts/compare-data.sh --create-test-data

# Performance testing
./scripts/load-test.sh 100 20  # 100 orders, 20 concurrent

# Advanced scenarios
./scripts/advanced-load-test.sh -s heavy -d -r
```

### Manual Verification
```bash
# Create an order
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{"customer_id": "TEST", "items": [{"product_id": "TEST-1", "quantity": 1, "unit_price": 10.00}], "total_amount": 10.00, "delivery_date": "2025-07-01T00:00:00Z"}'

# Verify in both systems
curl http://localhost:8081/orders | jq .  # Order Service
curl http://localhost:8082/orders | jq .  # SAP Mock

# Check consistency
curl http://localhost:8080/compare/orders | jq .analysis.sync_status
```

## Monitoring & Observability

### Logging
- Structured JSON logging with Logrus
- Request/response timing
- Error tracking and categorization
- Performance metrics

### Metrics
- Order creation response times
- System-specific performance measurements
- Data consistency verification results
- Load testing analytics

### Event Monitoring
- Kafka UI: http://localhost:8090
- Real-time event stream visualization
- Topic and partition monitoring

## Development

### Local Development
```bash
# Install dependencies
go mod download

# Run tests
go test ./...

# Build all services
go build ./cmd/proxy
go build ./cmd/order-service  
go build ./cmd/sap-mock
```

### Docker Development
```bash
# Rebuild specific service
docker-compose build proxy
docker-compose up -d proxy

# View logs
docker-compose logs -f proxy
docker-compose logs -f order-service
```

## Troubleshooting

### Common Issues

**Services not starting:**
```bash
# Check service status
docker-compose ps

# Check logs
docker-compose logs [service-name]

# Restart all services
docker-compose restart
```

**Performance issues:**
```bash
# Check resource usage
docker stats

# Check service health
curl http://localhost:8080/health
curl http://localhost:8081/health
curl http://localhost:8082/health
```

**Data inconsistency:**
```bash
# Run comparison check
./scripts/compare-data.sh

# Check individual order
curl http://localhost:8080/compare/orders/{order-id}
```

For detailed troubleshooting, see [LOAD-TESTING.md](LOAD-TESTING.md).

## Documentation

- **[API.md](API.md)** - Complete API documentation
- **[DOCKER.md](DOCKER.md)** - Docker configuration and deployment
- **[LOAD-TESTING.md](LOAD-TESTING.md)** - Comprehensive load testing guide
- **[CLAUDE.md](CLAUDE.md)** - Development methodology and guidelines

## Implementation Phases

### ✅ Phase 1: Basic Proxy (Complete)
- HTTP proxy between e-commerce and SAP
- Request/response logging
- Basic health checks

### ✅ Phase 2: Dual Write (Complete)
- Order Service with PostgreSQL database
- Kafka event publishing
- Dual-write to both systems
- Data consistency verification
- Comprehensive load testing

### ✅ Phase 3: Event-Driven (Complete)
- SAP consumes events from Kafka
- Removed direct SAP calls from proxy
- Complete strangler pattern implementation
- Full event-driven architecture achieved

## Success Metrics

### Performance
- **Phase 3 Proxy → Order Service**: ~50-100ms response time
- **Event Processing**: Asynchronous (no blocking)
- **Throughput**: 30+ orders/second under load
- **SAP Processing**: 1-3 seconds via Kafka (async)

### Reliability
- **Success Rate**: >95% under normal load
- **Data Consistency**: 100% synchronization between systems
- **Zero Data Loss**: All successful orders in both systems

### Migration Success
- ✅ Complete decoupling from legacy system
- ✅ Event-driven architecture implemented
- ✅ Proven data consistency via events
- ✅ Strangler pattern successfully applied

## Contributing

This is a demonstration project showcasing strangler pattern implementation. Feel free to:

- Fork and experiment with different approaches
- Add new features or testing scenarios
- Improve performance optimizations
- Extend to additional services

## License

MIT License - See LICENSE file for details

---

**Ready to see the complete strangler pattern in action?**

```bash
docker-compose up --build
./scripts/demo-phase3.sh
```