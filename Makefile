.PHONY: run build cli test fmt import docker-up docker-down

run:
	go run ./cmd/server

build:
	go build ./cmd/server

cli:
	go build -o bin/type-registry ./cmd/type-registry

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal ./web

import:
	@test -n "$(FILE)" || (echo "usage: make import FILE=/path/to/types.xlsx" && exit 1)
	go run ./cmd/import-xlsx --file "$(FILE)" --mapping config/import-mapping.json

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
