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
	for f in migrations/*.sql; do psql "$$DATABASE_URL" -f "$$f" || exit 1; done
