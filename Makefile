.PHONY: install dev dev-backend build pam-helper check test

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

# Build on the target Linux host with gcc and pam-devel/libpam0g-dev installed.
pam-helper:
	@test "$$(uname -s)" = Linux || (echo 'PAM helper requires Linux'; exit 1)
	cd backend && CGO_ENABLED=1 go build -o slurm-console-pam-helper ./cmd/pam-helper

check:
	npm --prefix frontend run typecheck
	npm --prefix frontend run lint
	cd backend && go vet ./...

test:
	npm --prefix frontend test
	cd backend && go test ./...
