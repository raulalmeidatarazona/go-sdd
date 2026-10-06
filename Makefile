SHELL := /bin/bash
.DEFAULT_GOAL := help

BIN := $(CURDIR)/bin
BUF := $(BIN)/buf
# Pinned code generation toolchain. Bump deliberately and regenerate.
BUF_VERSION := v1.73.0
PROTOC_GEN_GO_VERSION := v1.36.12
PROTOC_GEN_GO_GRPC_VERSION := v1.6.2
GRPC_GATEWAY_VERSION := v2.31.0

LOCAL_DB_URL := postgres://oms_app:oms_app_local@localhost:5432/oms?sslmode=disable
LOCAL_AMQP_URL := amqp://oms:oms_local@localhost:5672/
LOCAL_OTEL := http://localhost:4317

.PHONY: help
help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## --- Code generation -------------------------------------------------------

.PHONY: tools
tools: ## Install pinned buf and protoc plugins into ./bin
	GOBIN=$(BIN) go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	GOBIN=$(BIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOBIN=$(BIN) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)
	GOBIN=$(BIN) go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@$(GRPC_GATEWAY_VERSION)
	GOBIN=$(BIN) go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@$(GRPC_GATEWAY_VERSION)

.PHONY: proto
proto: ## Lint protos and regenerate gRPC, gateway and OpenAPI code
	$(BUF) lint
	$(BUF) format -w
	$(BUF) generate

.PHONY: proto-breaking
proto-breaking: ## Fail if the proto contract breaks compatibility with main
	$(BUF) breaking --against '.git#branch=main'

## --- Quality gates ---------------------------------------------------------

.PHONY: fmt
fmt: ## Format Go code
	gofmt -w cmd internal

.PHONY: lint
lint: ## Run golangci-lint and buf lint
	golangci-lint run ./...
	$(BUF) lint

.PHONY: test
test: ## Unit tests with the race detector
	go test -race -count=1 ./...

.PHONY: test-integration
test-integration: ## End-to-end tests against the running stack (make up first)
	go test -race -count=1 -tags=integration ./test/...

.PHONY: check
check: lint test ## Everything CI runs before merge
	@test -z "$$(gofmt -l cmd internal)" || (echo "run make fmt" && exit 1)
	go mod verify
	@if git rev-parse --is-inside-work-tree >/dev/null 2>&1 && ! git diff --quiet -- gen; then \
		echo "warning: generated code differs from git; run make proto and commit"; fi

## --- Local platform --------------------------------------------------------

.PHONY: up
up: ## Build and start the whole platform in Docker
	docker compose up -d --build --wait

.PHONY: infra
infra: ## Start only dependencies (Postgres, RabbitMQ, observability) and migrate
	docker compose up -d --wait postgres rabbitmq otel-collector jaeger prometheus loki grafana swagger-ui
	docker compose run --rm migrate

.PHONY: run-api
run-api: ## Run the API on the host against `make infra`
	DATABASE_URL=$(LOCAL_DB_URL) OTEL_EXPORTER_OTLP_ENDPOINT=$(LOCAL_OTEL) OTEL_SERVICE_NAME=oms-api go run ./cmd/api

.PHONY: run-worker
run-worker: ## Run the worker on the host against `make infra`
	DATABASE_URL=$(LOCAL_DB_URL) RABBITMQ_URL=$(LOCAL_AMQP_URL) OTEL_EXPORTER_OTLP_ENDPOINT=$(LOCAL_OTEL) OTEL_SERVICE_NAME=oms-worker go run ./cmd/worker

.PHONY: down
down: ## Stop the platform (keeps data)
	docker compose down

.PHONY: reset
reset: ## Stop the platform and delete all data volumes
	docker compose down -v

.PHONY: logs
logs: ## Tail api and worker logs
	docker compose logs -f api worker

.PHONY: migrate-new
migrate-new: ## Create a migration pair: make migrate-new NAME=add_shipments
	@test -n "$(NAME)" || (echo "usage: make migrate-new NAME=snake_case" && exit 1)
	@n=$$(printf "%06d" $$(( $$(ls migrations/*.up.sql 2>/dev/null | wc -l) + 1 ))); \
	touch migrations/$${n}_$(NAME).up.sql migrations/$${n}_$(NAME).down.sql; \
	echo "created migrations/$${n}_$(NAME).{up,down}.sql"

.PHONY: smoke
smoke: ## Place, read, list and cancel an order through REST
	./scripts/smoke.sh
