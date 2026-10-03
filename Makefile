.PHONY: dev dev-backend dev-frontend db-only build test test-frontend test-binlog-export-integration test-session-management-integration test-table-schema-integration migrate lint

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

test-binlog-export-integration:
	bash backend/test/integration/binlog_export.sh

test-session-management-integration:
	bash backend/test/integration/session_management.sh

test-table-schema-integration:
	bash backend/test/integration/table_schema.sh

lint:
	cd backend && golangci-lint run ./...

migrate:
	cd backend && go run ./cmd/server -migrate-only

tidy:
	cd backend && go mod tidy

gen-key:
	@dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64

reset-mfa:
	@test -n "$(USERNAME)" || (echo "USERNAME is required, e.g. make reset-mfa USERNAME=admin" && exit 1)
	cd backend && go run ./cmd/server -reset-mfa-username "$(USERNAME)"
