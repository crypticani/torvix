APP=cloudpulse

.PHONY: build run test fmt tidy compose-up compose-down

build:
	go build ./...

run:
	go run ./cmd/cloudpulse -config configs/config.example.yaml

test:
	go test ./...

fmt:
	go fmt ./...

tidy:
	go mod tidy

compose-up:
	docker compose up --build

compose-down:
	docker compose down -v
