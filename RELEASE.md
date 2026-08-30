# Release Process

Step-by-step to create a new zeep-vane release.

Day-to-day work happens on `develop` (adopted 2026-08-30, mirroring `zeep-orbit`'s model — see `AGENTS.md` §2). Releases are cut from `develop` into `main` via a release branch + PR, never by pushing straight to `main`.

## 0. Open a release branch and PR into `main`

```bash
git checkout develop
git pull origin develop
git checkout -b release-v0.2.0
git push origin release-v0.2.0
```

Open a PR `release-v0.2.0` → `main`. Do steps 1-3 below as commits on this branch (locally before pushing, or as follow-up commits on the open PR). Once CI is green and the PR is reviewed, merge it using **"Squash and merge"** — pre-adopted from `zeep-orbit` before vane hits the same failure: if `develop` ever picks up a `merge --no-ff` reconciliation commit from `main` after a release, GitHub's "Rebase and merge" can't replay it via cherry-pick and fails with "This branch can't be rebased" on every release branch cut afterward. Squashing sidesteps that entirely — the whole release branch becomes one commit on `main`, full history stays on `develop`.

After the PR merges, `main` has everything needed for the release. Continue from step 4 (tag).

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

## 4. Commit, push, and merge the release PR

```bash
git add -A
git commit -m "release: bump to v0.2.0"
git push origin release-v0.2.0
```

Merge the PR into `main` once CI is green (squash and merge — see step 0).

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

## 7. Reconcile `develop` with `main`

After the tag is out, check whether `develop` and `main` have diverged (the release branch's version-bump commit landed on `main` via squash, so `develop` doesn't have it yet):

```bash
git checkout develop
git merge origin/main --no-ff
git push origin develop
```

This is the reconciliation commit called out in step 0/`AGENTS.md` §2 — it's exactly what makes a *future* release branch's "Rebase and merge" fail once it's in that branch's diff against `main`, which is why step 0 already commits to squash instead.

## Helm chart repository caveat

`docs.yml` regenerates the GitHub Pages site from scratch on every run — it only packages whatever is in `charts/zeep-vane` at that commit and indexes just that one `.tgz`. It does **not** fetch previously published chart versions before re-indexing, so **only the most recently published chart version is guaranteed to be listed** in `https://zeeplabs.github.io/zeep-vane/helm/index.yaml` at any given time. If you need older chart versions to remain installable via `helm repo add`/`helm install --version`, download the `.tgz` from that release's GitHub Release assets instead of relying on the Pages index. Fixing this properly means fetching and merging the existing `index.yaml`/`.tgz` files before re-packaging — not yet implemented here (same gap exists in `zeep-orbit`'s `docs.yml`).

## Verify

- [ ] Docker image: `docker pull ghcr.io/zeeplabs/zeep-vane:0.2.0` (no leading `v` on the image tag, unlike the git tag/GitHub Release name)
- [ ] GitHub Release: https://github.com/zeeplabs/zeep-vane/releases
- [ ] Helm chart: `helm repo add zeeplabs https://zeeplabs.github.io/zeep-vane/helm && helm repo update && helm search repo zeeplabs/zeep-vane`
- [ ] `helm template`/`helm install --dry-run` against the new chart version with real `secrets.*` values
- [ ] `develop` reconciled with `main` (no divergence: `git merge-base --is-ancestor main develop`)

## Checklist

- [ ] Release branch `release-vX.Y.Z` created from `develop`
- [ ] `charts/zeep-vane/Chart.yaml` version bumped (`version` + `appVersion`)
- [ ] `web/package.json` version bumped
- [ ] `CHANGELOG.md` updated, new `[Unreleased]` heading left at top
- [ ] `.github/release-notes-vX.Y.Z.md` written
- [ ] PR opened, CI green, reviewed
- [ ] PR merged into `main` via **squash and merge**
- [ ] Tag pushed (`git push origin vX.Y.Z`)
- [ ] `docker-publish.yml` passed (test → build-push → release)
- [ ] Docker pull works
- [ ] Helm install works against the new chart version
- [ ] `develop` reconciled with `main` (`git merge origin/main --no-ff`)
