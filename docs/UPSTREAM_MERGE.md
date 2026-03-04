# Upstream Merge Playbook

This repository tracks upstream `MetaCubeX/mihomo` (branch `Alpha`) and keeps local behavior patches.

## Branch Strategy

- `main`: your release branch in this fork.
- `upstream/alpha-sync` (temporary): branch used to replay upstream updates.
- local patch branch examples: `codex/fix-group-selection`.

## One-Time Remote Setup

```bash
git remote -v
git remote add upstream https://github.com/MetaCubeX/mihomo.git
```

## Regular Upstream Sync Workflow

1. Fetch upstream changes and tags.

```bash
git fetch upstream Alpha --tags
```

2. Create a sync branch from your `main`.

```bash
git checkout main
git pull --ff-only origin main
git checkout -B upstream/alpha-sync
```

3. Merge upstream into the sync branch.

```bash
git merge --no-ff upstream/Alpha
```

4. Resolve conflicts.

- Keep local behavior patch for group selection logic in:
  - `adapter/outboundgroup/urltest.go`
  - `adapter/outboundgroup/fallback.go`
  - `adapter/outboundgroup/groupbase.go`
- Re-run formatting and compile checks:

```bash
gofmt -w adapter/outboundgroup/urltest.go adapter/outboundgroup/fallback.go adapter/outboundgroup/groupbase.go
go test ./adapter/outboundgroup ./hub/route
```

5. Fast smoke build.

```bash
make -j4
```

6. Merge sync branch into `main` and push.

```bash
git checkout main
git merge --no-ff upstream/alpha-sync
git push origin main --tags
```

## Release Workflow

Use GitHub Actions for full multi-platform binaries (recommended):

1. Create a version tag:

```bash
git checkout main
git pull --ff-only origin main
git tag -a vX.Y.Z-fork.N -m "fork release vX.Y.Z-fork.N"
git push origin vX.Y.Z-fork.N
```

2. Trigger/verify release workflow in GitHub Actions.

3. Confirm release artifacts are attached to the GitHub Release page.

## Conflict Resolution Rule of Thumb

When upstream changes the same selection files, prefer:

1. Keep upstream API/struct changes.
2. Re-apply local policy:
   - Prefer alive nodes over timeout nodes when at least one alive exists.
   - Refresh URLTest cached decision after group URL tests.
   - Fallback selected-node logic should re-scan all nodes after selected node becomes unavailable.
3. Re-run `go test` for changed packages.

