.PHONY: build web-build run test test-integration lint vet dev-db dev-db-stop migrate dev-backend dev-frontend dev

DEV_DB_CONTAINER := vane-dev-pg
DEV_DB_PORT := 5432
DATABASE_URL := postgres://vane:vane@localhost:$(DEV_DB_PORT)/vane?sslmode=disable

web-build:
	cd web && npm install && npm run build

build: web-build
	go build -o bin/vane ./cmd/vane


# Runs the built binary (front + back in one process) on :8080. Requires
# `make build` first, plus dev-db + migrate already run.
run:
	DATABASE_URL=$(DATABASE_URL) \
	VANE_MASTER_KEY=dev-master-key-change-me-0123456789 \
	VANE_SESSION_SECRET=dev-session-secret-change-me-0123456789 \
	PORT=8080 \
	POLL_INTERVAL_SECONDS=60 \
	./bin/vane serve

test:
	go test ./...

# Integration gate against a disposable Postgres (never the dev DB).
# Mirrors AGENTS.md §3 and .github/workflows/ci.yml: -p 1 is mandatory -
# every integration package shares the one TEST_DATABASE_URL and several
# mutate the same singleton/shared tables (users, tenants, integrations,
# admins), so parallel package binaries race each other (spurious 401s,
# count mismatches, and the intermittent `dbtest: pg_advisory_lock failed:
# timeout`). -count=1 forces real runs instead of cached package results,
# and the container is created fresh per run because a few singleton tables
# are not reset between successive runs against the same database.
INTEGRATION_DB_CONTAINER := vane-test-pg
INTEGRATION_DB_PORT := 5433
INTEGRATION_DATABASE_URL := postgres://vane:vane@localhost:$(INTEGRATION_DB_PORT)/vane?sslmode=disable

test-integration:
	@docker rm -f $(INTEGRATION_DB_CONTAINER) >/dev/null 2>&1 || true
	docker run -d --rm --name $(INTEGRATION_DB_CONTAINER) \
		-e POSTGRES_USER=vane -e POSTGRES_PASSWORD=vane -e POSTGRES_DB=vane \
		-p $(INTEGRATION_DB_PORT):5432 postgres:16-alpine -c max_connections=300
	@for i in $$(seq 1 30); do \
		docker exec $(INTEGRATION_DB_CONTAINER) pg_isready -U vane >/dev/null 2>&1 && break; \
		sleep 1; \
	done
	@trap 'docker rm -f $(INTEGRATION_DB_CONTAINER) >/dev/null 2>&1 || true' EXIT; \
	TEST_DATABASE_URL="$(INTEGRATION_DATABASE_URL)" go test -tags=integration -count=1 -p 1 ./...

lint:
	gofmt -l .

vet:
	go vet ./...

# Starts a local Postgres container for dev if it isn't already running.
# Safe to re-run: reuses the existing container instead of erroring.
dev-db:
	@docker start $(DEV_DB_CONTAINER) 2>/dev/null || docker run -d --name $(DEV_DB_CONTAINER) \
		-e POSTGRES_USER=vane -e POSTGRES_PASSWORD=vane -e POSTGRES_DB=vane \
		-p $(DEV_DB_PORT):5432 postgres:16-alpine

dev-db-stop:
	docker stop $(DEV_DB_CONTAINER)

# Applies pending migrations against the dev database.
migrate:
	DATABASE_URL=$(DATABASE_URL) \
	VANE_MASTER_KEY=dev-master-key-change-me-0123456789 \
	VANE_SESSION_SECRET=dev-session-secret-change-me-0123456789 \
	PORT=8080 \
	POLL_INTERVAL_SECONDS=60 \
	go run ./cmd/vane migrate up

# Runs the admin API on :8080. Requires dev-db + migrate already run.
dev-backend:
	DATABASE_URL=$(DATABASE_URL) \
	VANE_MASTER_KEY=dev-master-key-change-me-0123456789 \
	VANE_SESSION_SECRET=dev-session-secret-change-me-0123456789 \
	PORT=8080 \
	POLL_INTERVAL_SECONDS=60 \
	go run ./cmd/vane serve

# Runs the Vite dev server on :5173.
dev-frontend:
	cd web && npm run dev

# Runs backend and frontend together; Ctrl+C stops both.
dev:
	@trap 'kill 0' EXIT; \
	$(MAKE) dev-backend & \
	$(MAKE) dev-frontend & \
	wait
