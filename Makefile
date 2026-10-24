.PHONY: install dev build check test

install:
	npm --prefix frontend ci

dev:
	npm --prefix frontend run dev

build:
	npm --prefix frontend run build

check:
	npm --prefix frontend run typecheck
	npm --prefix frontend run lint

test:
	npm --prefix frontend test

