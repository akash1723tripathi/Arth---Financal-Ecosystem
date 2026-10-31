# Arth API tasks. Run from apps/api: `make test`. Windows without make: `./scripts/dev.ps1 test` (same task names).
# Programs load ./.env themselves; nothing here reads or prints it.

SQLC_IMAGE ?= sqlc/sqlc:1.30.0
IMAGE      ?= arth-api:dev
BIN        ?= bin

.PHONY: run worker test vet build migrate-up migrate-down migrate-status generate sqlc docker-build

run:
	go run ./cmd/api

worker:
	go run ./cmd/worker

test:
	go test ./...

vet:
	go vet ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN)/api ./cmd/api
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN)/worker ./cmd/worker

migrate-up:
	go run ./cmd/api migrate up

migrate-down:
	go run ./cmd/api migrate down

migrate-status:
	go run ./cmd/api migrate status

generate:
	go generate ./...
	$(MAKE) sqlc

# MSYS_NO_PATHCONV stops Git Bash from rewriting the container path /src.
sqlc:
	MSYS_NO_PATHCONV=1 docker run --rm -v "$(CURDIR):/src" -w /src $(SQLC_IMAGE) generate

docker-build:
	docker build -t $(IMAGE) .
