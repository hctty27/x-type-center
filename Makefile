.PHONY: run build build-linux cli test fmt import migrate docker-up docker-down

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

run:
	go run ./cmd/x-type-center server

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/x-type-center ./cmd/x-type-center

build-linux:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/x-type-center-linux-amd64 ./cmd/x-type-center

cli:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/type-registry ./cmd/type-registry

test:
	go test ./...

fmt:
	find ./cmd ./internal ./web -name '*.go' -print0 | xargs -0 gofmt -w

import:
	@test -n "$(FILE)" || (echo "usage: make import FILE=/path/to/types.xlsx" && exit 1)
	go run ./cmd/x-type-center import --file "$(FILE)"

migrate:
	go run ./cmd/x-type-center migrate

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
