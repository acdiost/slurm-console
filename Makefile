.PHONY: install dev dev-backend build release check test

BACKEND_HTTP ?= 127.0.0.1:6829
TARGET_ARCH ?= amd64

install:
	npm --prefix frontend ci
	cd backend && go mod download

dev:
	npm --prefix frontend run dev

dev-backend:
	cd backend && go run . serve --http=$(BACKEND_HTTP)

build:
	npm --prefix frontend run build
	bash deploy/package.sh
	cd backend && go build -o slurm-console .

release:
	npm --prefix frontend run build
	bash deploy/package.sh "$(TARGET_ARCH)"
	mkdir -p dist/slurm-console-linux-$(TARGET_ARCH)
	cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=$(TARGET_ARCH) go build -trimpath -o ../dist/slurm-console-linux-$(TARGET_ARCH)/slurm-console .
	cp deploy/slurm-console.service dist/slurm-console-linux-$(TARGET_ARCH)/slurm-console.service

check:
	npm --prefix frontend run typecheck
	npm --prefix frontend run lint
	cd backend && go vet ./...

test:
	npm --prefix frontend test
	cd backend && go test ./...
