.PHONY: install dev dev-backend build check test

BACKEND_HTTP ?= 127.0.0.1:8090

install:
	npm --prefix frontend ci
	cd backend && go mod download

dev:
	npm --prefix frontend run dev

dev-backend:
	cd backend && go run . serve --http=$(BACKEND_HTTP)

build:
	npm --prefix frontend run build
	cd backend && go build -o slurm-console .

check:
	npm --prefix frontend run typecheck
	npm --prefix frontend run lint
	cd backend && go vet ./...

test:
	npm --prefix frontend test
	cd backend && go test ./...
