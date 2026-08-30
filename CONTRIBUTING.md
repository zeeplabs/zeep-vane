# Contributing to zeep-vane

## Prerequisites

- Go 1.26+
- Node 18+
- PostgreSQL 14+ (a local Docker container is the easiest path)
- Docker + Docker Compose (optional)

## Development setup

```bash
git clone https://github.com/zeeplabs/zeep-vane
cd zeep-vane
go mod download
make build
```

See [Running in development](README.md#%EF%B8%8F-running-in-development) in the README for the full backend + frontend setup, including the `.env` you need locally.

## Running tests

Backend unit tests (no database required):

```bash
go test ./...
go vet ./...
gofmt -l .        # should print nothing
```

Backend integration tests (require `//go:build integration` and a real Postgres via `TEST_DATABASE_URL`):

```bash
make dev-db
TEST_DATABASE_URL="postgres://vane:vane@localhost:5432/vane?sslmode=disable" go test -tags=integration ./...
```

**Never point `TEST_DATABASE_URL` at a database with real data** — the integration suite runs `db.MigrateUp` and writes/deletes rows freely. Always use a disposable container.

Frontend:

```bash
cd web
npm run test         # vitest run
npx tsc -b --noEmit   # type-check
```

## Making changes

1. Fork the repository
2. Branch from `develop`: `git checkout develop && git checkout -b feat/my-change`
3. Make your changes
4. Run the relevant test/gate commands above for whatever you touched (backend, frontend, or both)
5. Commit with a clear message (see style below)
6. Open a pull request against `develop` — `main` only receives merges from a release branch (see `RELEASE.md`), never a feature PR directly

## Commit style

```
type: short description

Longer explanation if needed.
```

Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `release`

## Spec-driven changes

Non-trivial features in this repo go through `.specs/` — see `.specs/STATE.md` for the running log of architectural decisions (`AD-*`) and `.specs/features/` for per-feature spec history. If you're proposing a change that affects the domain model, auth, or public routing, skim `.specs/STATE.md` first so your PR doesn't contradict a decision already made (and documented) there.

## What to work on

Check open issues labeled `good first issue` or `help wanted`, or the [Known gaps / backlog](README.md#-known-gaps--backlog) section of the README.

## Security

Do not open public issues for security vulnerabilities — see [SECURITY.md](SECURITY.md).

## License

By contributing you agree your code will be licensed under the [Apache License 2.0](LICENSE).
