.PHONY: run build test tidy migrate

run:
	go run ./cmd/api

build:
	go build -o bin/api ./cmd/api

test:
	go test ./...

tidy:
	go mod tidy

migrate:
	bash scripts/migrate.sh
