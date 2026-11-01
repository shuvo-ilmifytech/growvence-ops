include .env
export

.PHONY: infra api web seed test sqlc e2e up down

infra: ## start Postgres, Redis, MinIO for local development
	docker compose up -d postgres redis minio

api: ## run the Go API (applies migrations on start)
	cd backend && go run ./cmd/api

web: ## run the Next.js dev server
	cd frontend && npm run dev

seed: ## create demo users (one per role) + lookups on an empty DB
	cd backend && go run ./cmd/opsctl seed-dev

sqlc: ## regenerate typed queries after editing SQL
	cd backend && go tool sqlc generate

test: ## Go unit + integration tests (needs `make infra`)
	cd backend && go test ./...

e2e: ## browser QA of the whole spec (BASE_URL, CHROME_PATH)
	cd e2e && npm install --silent && node qa.mjs

up: ## full stack in Docker
	docker compose --profile app up -d --build

down:
	docker compose --profile app down
