.PHONY: build run test clean docker-up docker-down migrate seed

# Variables
APP_NAME=discovery-feed-service
DOCKER_COMPOSE=docker-compose
GO=go

# Build the binary
build:
	$(GO) build -o bin/$(APP_NAME) ./cmd/api

# Run locally (requires deps)
run:
	$(GO) run ./cmd/api

# Run tests
test:
	$(GO) test -v ./...

# Run tests with coverage
test-cover:
	$(GO) test -cover ./...

# Clean build artifacts
clean:
	rm -rf bin/
	$(GO) clean

# Docker commands
docker-up:
	$(DOCKER_COMPOSE) up -d

docker-down:
	$(DOCKER_COMPOSE) down
docker-build:
	$(DOCKER_COMPOSE) build

docker-logs:
	$(DOCKER_COMPOSE) logs -f api

# Database
migrate:
	psql -U feeduser -d discoveryfeed -f migrations/001_initial_schema.sql

seed:
	psql -U feeduser -d discoveryfeed -f migrations/001_initial_schema.sql

# Development helpers
dev-deps:
	docker-compose up -d postgres redis jaeger

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

lint: fmt vet
	golangci-lint run

# Benchmarks
bench:
	$(GO) test -bench=. ./internal/ranking/

# API testing
curl-health:
	curl http://localhost:8080/health

curl-feed:
	curl "http://localhost:8080/api/v1/feed?user_id=11111111-1111-1111-1111-111111111111&latitude=40.7128&longitude=-74.0060"

curl-optimize:
	curl -X POST http://localhost:8080/api/v1/ai/optimize \
		-H "Content-Type: application/json" \
		-d '{"goal":"increase_ctr"}'

# Full setup
setup: dev-deps migrate
	@echo "Development environment ready!"
	@echo "API: http://localhost:8080"
	@echo "Jaeger: http://localhost:16686"
	@echo "Prometheus: http://localhost:9090"
	@echo "Grafana: http://localhost:3000"
