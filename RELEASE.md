# Release Process

Step-by-step to create a new zeep-vane release.

This repo has no `develop`/release-branch flow (unlike `zeep-orbit`) — all work happens on `main` (see `AGENTS.md` §2). A release is a normal set of commits on `main` followed by a tag push.

## 0. Make sure `main` is release-ready

CI (`ci.yml`) must be green on `main` before you tag: Go build/vet/test, Go integration tests, frontend typecheck/test/build, and the full embedded-binary build. Don't tag off a red `main`.

## 1. Update version files

### charts/zeep-vane/Chart.yaml

```yaml
version: 0.2.0      # bump this
appVersion: "0.2.0" # keep in sync with the release version
```

### web/package.json

```json
{
  "version": "0.2.0"
}
```

Vane has no in-app version display or update-notification banner today (unlike `zeep-orbit`'s dashboard) — this bump is bookkeeping/consistency, not something the running app reads.

## 2. Update CHANGELOG.md

Move `[Unreleased]` entries into a new dated version section at the top:

```markdown
## [0.2.0] — 2026-09-15

### Added
- ...

### Fixed
- ...

## [Unreleased]
```

Keep an empty `[Unreleased]` heading at the top for the next round of changes to land under.

## 3. Write release notes

Create `.github/release-notes-v0.2.0.md` summarizing the release for humans (features, fixes, breaking changes, upgrade instructions). This becomes the **GitHub Release body**: `docker-publish.yml`'s release job reads `.github/release-notes-<tag>.md` verbatim and appends a Docker/Helm install snippet automatically. If the file is missing for a given tag, CI falls back to GitHub's auto-generated notes instead — so skipping this step doesn't fail the release, it just publishes generic notes.

## 4. Commit and push to `main`

```bash
git add -A
git commit -m "release: bump to v0.2.0"
git push origin main
```

Wait for CI to go green on this commit before tagging.

> **Note:** Landing on `main` with a change under `charts/**` triggers `docs.yml`, which packages the Helm chart and publishes it to `https://zeeplabs.github.io/zeep-vane/helm/`. The chart version comes from `Chart.yaml`. See the caveat under [Helm chart repository](#helm-chart-repository-caveat) below before relying on old versions still being listed there.

## 5. Create and push the tag

```bash
git tag v0.2.0
git push origin v0.2.0
```

## 6. CI does the rest

Pushing the tag triggers `docker-publish.yml`:

| Job | What it does |
|---|---|
| `test` | Reuses `ci.yml` (Go + frontend test suites) as a required gate |
| `build-push` | Multi-arch (`linux/amd64`, `linux/arm64`) Docker image, pushed to GHCR |
| `release` | Creates the GitHub Release for the tag, packages the Helm chart `.tgz` (version = tag, stripped of `v`) and attaches it |

## Helm chart repository caveat

`docs.yml` regenerates the GitHub Pages site from scratch on every run — it only packages whatever is in `charts/zeep-vane` at that commit and indexes just that one `.tgz`. It does **not** fetch previously published chart versions before re-indexing, so **only the most recently published chart version is guaranteed to be listed** in `https://zeeplabs.github.io/zeep-vane/helm/index.yaml` at any given time. If you need older chart versions to remain installable via `helm repo add`/`helm install --version`, download the `.tgz` from that release's GitHub Release assets instead of relying on the Pages index. Fixing this properly means fetching and merging the existing `index.yaml`/`.tgz` files before re-packaging — not yet implemented here (same gap exists in `zeep-orbit`'s `docs.yml`).

## Verify

- [ ] Docker image: `docker pull ghcr.io/zeeplabs/zeep-vane:0.2.0` (no leading `v` on the image tag, unlike the git tag/GitHub Release name)
- [ ] GitHub Release: https://github.com/zeeplabs/zeep-vane/releases
- [ ] Helm chart: `helm repo add zeeplabs https://zeeplabs.github.io/zeep-vane/helm && helm repo update && helm search repo zeeplabs/zeep-vane`
- [ ] `helm template`/`helm install --dry-run` against the new chart version with real `secrets.*` values

## Checklist

- [ ] `main` green on CI before tagging
- [ ] `charts/zeep-vane/Chart.yaml` version bumped (`version` + `appVersion`)
- [ ] `web/package.json` version bumped
- [ ] `CHANGELOG.md` updated, new `[Unreleased]` heading left at top
- [ ] `.github/release-notes-vX.Y.Z.md` written
- [ ] Commit pushed to `main`, CI green
- [ ] Tag pushed (`git push origin vX.Y.Z`)
- [ ] `docker-publish.yml` passed (test → build-push → release)
- [ ] Docker pull works
- [ ] Helm install works against the new chart version
