APP=torvix
DEV_COMPOSE=docker compose -f docker-compose.dev.yml
PROD_COMPOSE=docker compose -f docker-compose.prod.yml

.PHONY: build run test fmt tidy compose-up compose-down compose-dev-up compose-dev-down compose-prod-up compose-prod-down compose-dev-config compose-prod-config swagger

build:
	go build ./cmd/... ./internal/...

swagger:
	swag init -g cmd/torvix/main.go -o docs --parseDependency --parseInternal

run:
	go run ./cmd/torvix -config configs/config.yaml

test:
	go test ./cmd/... ./internal/... ./tests/...

fmt:
	go fmt ./...

tidy:
	go mod tidy

compose-up:
	$(DEV_COMPOSE) up --build

compose-down:
	$(DEV_COMPOSE) down -v

compose-dev-up:
	$(DEV_COMPOSE) up --build

compose-dev-down:
	$(DEV_COMPOSE) down -v

compose-prod-up:
	$(PROD_COMPOSE) up --build -d

compose-prod-down:
	$(PROD_COMPOSE) down

compose-dev-config:
	$(DEV_COMPOSE) config --quiet

compose-prod-config:
	$(PROD_COMPOSE) config --quiet
