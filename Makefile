APP=cloudpulse

.PHONY: build run test fmt tidy compose-up compose-down

build:
	go build ./cmd/... ./internal/...

run:
	go run ./cmd/cloudpulse -config configs/config.yaml

test:
	go test ./cmd/... ./internal/... ./tests/...

fmt:
	go fmt ./...

tidy:
	go mod tidy

compose-up:
	docker compose up --build

compose-down:
	docker compose down -v
