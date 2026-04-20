# Persistent Pin Upstream Merge Checklist

Use this checklist during every upstream merge for this fork.

This file is intentionally short.
It does not replace the detailed playbooks:

- `docs/persistent-pin-upstream-merge-human.md`
- `docs/persistent-pin-upstream-merge-ai.md`
- `docs/persistent-pin-upstream-merge-report-template.md`

Its only purpose is to prevent missed steps during execution.

## 1. Establish Scope

- [ ] Record old fork branch commit with `git rev-parse codex/persistent-pin-option`
- [ ] Record target upstream commit
- [ ] Compute old common base with `git merge-base codex/persistent-pin-option <target-upstream-commit>`
- [ ] Record old fork changed-file set with `git diff --name-status <old-common-base>..codex/persistent-pin-option`
- [ ] Record current branch changed-file set with `git diff --name-status <target-upstream-commit> --`

## 2. Review Old Fork-Owned Diff

- [ ] Review `.github/workflows/build.yml`
- [ ] Review `adapter/outboundgroup/fallback.go`
- [ ] Review `adapter/outboundgroup/groupbase.go`
- [ ] Review `adapter/outboundgroup/parser.go`
- [ ] Review `adapter/outboundgroup/urltest.go`
- [ ] Review `adapter/outboundgroup/util.go`
- [ ] Review `adapter/outboundgroup/persistent_pin_test.go`
- [ ] Review `docs/config.yaml`
- [ ] Review `hub/route/groups.go`
- [ ] Review `hub/route/groups_test.go`

Command to use:

```bash
git diff <old-common-base>..codex/persistent-pin-option -- \
  .github/workflows/build.yml \
  adapter/outboundgroup/fallback.go \
  adapter/outboundgroup/groupbase.go \
  adapter/outboundgroup/parser.go \
  adapter/outboundgroup/urltest.go \
  adapter/outboundgroup/util.go \
  adapter/outboundgroup/persistent_pin_test.go \
  docs/config.yaml \
  hub/route/groups.go \
  hub/route/groups_test.go
```

## 3. Create Merge Branch

- [ ] Checkout target upstream commit
- [ ] Create new merge branch from target upstream
- [ ] Confirm work starts from target upstream, not the old fork branch

## 4. Port Behavior

- [ ] Replay behavior, not file snapshots
- [ ] Preserve `fallback` alive-selection semantics
- [ ] Preserve `url-test` alive-selection semantics
- [ ] Preserve persistent pin behavior
- [ ] Preserve parser semantics for unset / `0` / invalid values
- [ ] Preserve `/groups/{name}/delay` pin protection
- [ ] Preserve logging behavior
- [ ] Preserve release workflow behavior
- [ ] Update tests if runtime behavior changed

## 5. Audit New Branch vs Old Fork

- [ ] Run old-vs-new diff on fork-owned files
- [ ] Classify every remaining diff
- [ ] Record evidence command for every classification
- [ ] Confirm no remaining diff is `missing behavior`

Command to use:

```bash
git diff codex/persistent-pin-option -- \
  .github/workflows/build.yml \
  adapter/outboundgroup/fallback.go \
  adapter/outboundgroup/groupbase.go \
  adapter/outboundgroup/parser.go \
  adapter/outboundgroup/urltest.go \
  adapter/outboundgroup/util.go \
  adapter/outboundgroup/persistent_pin_test.go \
  docs/config.yaml \
  hub/route/groups.go \
  hub/route/groups_test.go
```

## 6. Audit New Branch vs Target Upstream

- [ ] Run `git diff --name-status <target-upstream-commit> --`
- [ ] Review every changed file relative to target upstream
- [ ] Classify every extra changed file outside fork-owned scope
- [ ] Remove or explain every accidental/unrelated change

## 7. Validate Required Semantics

- [ ] `fallback` picks first alive proxy in original order
- [ ] `fallback` never picks timeout node when alive alternative exists
- [ ] `fallback` preserves pin when `persistent-pin=true`
- [ ] `fallback` auto-unfix only after threshold
- [ ] `fallback` resets counter on recovery
- [ ] `fallback` resets counter when no alternative alive proxy exists
- [ ] `fallback` resets counter when newer successful records exist
- [ ] `fallback` clears missing pinned proxy
- [ ] `url-test` never picks timeout node when alive alternative exists
- [ ] `url-test` preserves pin during automatic refresh
- [ ] `url-test` refreshes cached state after manual `URLTest()`
- [ ] `url-test` auto-unfix/reset semantics match `fallback`
- [ ] `url-test` clears missing pinned proxy
- [ ] `GroupBase.URLTest` filters unhealthy results
- [ ] parser preserves `persistent-pin`
- [ ] parser preserves `pin-unhealthy-log-interval` semantics
- [ ] parser preserves `persistent-pin-auto-unfix-threshold` semantics
- [ ] `/groups/{name}/delay` preserves persistent pin
- [ ] JSON output still exposes persistent pin fields
- [ ] logging still exposes unhealthy pin / reset / missing-pin / auto-unfix information
- [ ] workflow still disables Docker
- [ ] workflow still supports tag-triggered release publishing

## 8. Run Automated Checks

- [ ] Run `go test ./adapter/outboundgroup ./hub/route`
- [ ] Run `go test ./...`
- [ ] Record actual command results

## 9. Complete Report

- [ ] Fill `docs/persistent-pin-upstream-merge-report-template.md`
- [ ] Include all fork-owned files
- [ ] Include all files changed relative to target upstream
- [ ] Include all required semantics
- [ ] Include automated checks actually run
- [ ] Include manual diff verification for workflow/docs/structural diffs
- [ ] State unresolved risk explicitly, or write `none`
- [ ] Set `ready_to_release: yes` only if every required item is complete

## 10. Final Gate

- [ ] No fork-owned file was skipped
- [ ] No changed file relative to target upstream was skipped
- [ ] Every file from `git diff --name-status <target-upstream-commit> --` appears in the final report exactly once
- [ ] No required semantic was left unverified
- [ ] No unresolved `missing behavior` diff remains
- [ ] No accidental/unrelated changed file remains unexplained
- [ ] Report is complete
- [ ] Merge is ready
