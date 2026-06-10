.PHONY: dev dev-backend dev-frontend db-only build test test-frontend migrate lint

dev:
	docker compose up --build

dev-backend:
	docker compose up --build mysql app

dev-frontend:
	cd frontend && npm run dev

db-only:
	docker compose up mysql

build:
	cd backend && go build -o ../bin/maestro ./cmd/server

test:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm test

lint:
	cd backend && golangci-lint run ./...

migrate:
	cd backend && go run ./cmd/server -migrate-only

tidy:
	cd backend && go mod tidy

gen-key:
	@dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64
