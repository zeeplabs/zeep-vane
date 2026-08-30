## What

<!-- Short description of the change -->

## Why

<!-- Motivation or issue reference (e.g. closes #123) -->

## Checklist

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes (integration tests noted/skipped with reason if not run)
- [ ] `go vet ./...` clean, `gofmt -l .` empty
- [ ] Frontend: `npm run test` and `npx tsc -b --noEmit` pass (if `web/` touched)
- [ ] No new secrets or credentials in code
