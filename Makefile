.PHONY: build test docker-build up up-aa up-ap migrate lint tidy

build:
	go build ./cmd/llm-proxy/...
	go build ./cmd/auth-service/...
	go build ./cmd/billing-service/...

tidy:
	go mod tidy

test:
	go test ./...

docker-build:
	docker build -f deployments/llm-proxy/Dockerfile -t llm-proxy:latest .
	docker build -f deployments/auth-service/Dockerfile -t auth-service:latest .
	docker build -f deployments/billing-service/Dockerfile -t billing-service:latest .

up:
	docker compose -f deployments/docker-compose.yml up -d

up-aa:
	LB_MODE=active-active docker compose -f deployments/docker-compose.yml up -d

up-ap:
	LB_MODE=active-passive docker compose -f deployments/docker-compose.yml up -d

down:
	docker compose -f deployments/docker-compose.yml down

logs:
	docker compose -f deployments/docker-compose.yml logs -f

migrate:
	psql $${DB_URL} -f migrations/auth/001_init.sql
	psql $${DB_URL} -f migrations/billing/001_init.sql

lint:
	golangci-lint run ./...
