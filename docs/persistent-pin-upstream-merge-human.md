# Persistent Pin Upstream Merge Playbook (Human)

## Purpose

This fork tracks upstream `MetaCubeX/mihomo`, but it is not allowed to lose the fork-specific `persistent-pin-option` behavior during future upstream upgrades.

This document is a human merge checklist. It is intentionally repetitive and operational. Its job is to prevent a future maintainer from saying "the merge looks fine" while silently dropping part of the feature surface.

This playbook is based on the migration that moved the fork from:

- old fork branch: `codex/persistent-pin-option`
- old fork reference commit: `a0611406bfb4760f3b64d12b3d94a8f8b8fe36b6`
- old fork release commit: `1101ff88f227c6575d010a0054b1ed5d31874d91`
- old common upstream base: `0317d9f74229374bd5ed55e4c2bb63f68e06c9c8`
- new upstream base used in that migration: `a84724665eb7f989809abe463c05f5723bd24975`

These values are not permanent. For every future merge, you must recompute the new common base and compare against the real old fork branch.

## Non-negotiable rules

1. Do not rely on commit titles.
2. Do not rely on memory.
3. Do not rely on branch names.
4. Do not claim completion from a successful cherry-pick.
5. Do not copy old files wholesale onto new upstream files.
6. Always extract fork behavior from actual code diff.
7. Always prove preservation with tests or explicit line-by-line review.

This fork must be upgraded by behavioral replay onto the latest upstream structure.

## Source-of-truth procedure

Before any new upstream merge, collect these exact values:

```bash
git rev-parse codex/persistent-pin-option
git rev-parse <target-upstream-commit>
git merge-base codex/persistent-pin-option <target-upstream-commit>
git diff --name-status <old-common-base>..codex/persistent-pin-option
```

Also record the actual file set changed on the new merge branch relative to the target upstream base:

```bash
git diff --name-status <target-upstream-commit> --
```

Then capture the real old fork diff for the fork-owned files:

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

If the real diff has expanded beyond these files, update this document before merging.

If the new merge branch changes files outside the intended migration scope, do not ignore them. Classify each extra touched file as one of:

1. required by upstream structural adaptation
2. required new test/support file
3. accidental or unrelated change

Class 3 is a blocker and must be removed or explained before completion.

## Fork-owned migration scope

Treat these files as mandatory review targets for every upstream merge:

- `.github/workflows/build.yml`
- `adapter/outboundgroup/fallback.go`
- `adapter/outboundgroup/groupbase.go`
- `adapter/outboundgroup/parser.go`
- `adapter/outboundgroup/urltest.go`
- `adapter/outboundgroup/util.go`
- `docs/config.yaml`
- `hub/route/groups.go`

Also treat these tests as mandatory preservation evidence:

- `adapter/outboundgroup/persistent_pin_test.go`
- `hub/route/groups_test.go`

If a future merge changes behavior in one of the runtime files, tests must be updated in the same turn.

Do not mark the merge complete after reviewing only runtime files. Every fork-owned file and every evidence target must be reviewed:

- `.github/workflows/build.yml`
- `adapter/outboundgroup/fallback.go`
- `adapter/outboundgroup/groupbase.go`
- `adapter/outboundgroup/parser.go`
- `adapter/outboundgroup/urltest.go`
- `adapter/outboundgroup/util.go`
- `docs/config.yaml`
- `hub/route/groups.go`
- `adapter/outboundgroup/persistent_pin_test.go`
- `hub/route/groups_test.go`

## Merge workflow

### Step 1. Start from the new upstream base

Create a fresh branch from the target upstream commit:

```bash
git checkout <target-upstream-commit>
git checkout -b persistent-pin-option-<upstream-version>merge
```

Do not start from the old fork branch.

### Step 2. Replay behavior, not snapshots

Port fork behavior onto the new upstream structure.

Allowed:

- adapting to upstream field moves
- adapting to renamed methods or interfaces
- keeping upstream improvements outside the fork behavior surface

Not allowed:

- dropping behavior because "the structure changed"
- bulk-replacing current upstream files with old fork files
- treating conflict-free cherry-pick as proof of correctness

### Step 3. Audit old vs new after porting

After implementation, compare the new branch against the old fork branch:

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

Every remaining diff must be classified as one of:

1. required upstream structural adaptation
2. upstream-only addition unrelated to fork behavior
3. fork behavior accidentally lost

Class 3 is a blocker.

Do not write a classification without evidence. For every remaining diff, record:

- file path
- exact command used to inspect it
- whether it was compared against old fork branch or target upstream
- why it is class 1, 2, or 3
- if class 1, what upstream structure change forced the adaptation

Also compare the current branch against the target upstream base:

```bash
git diff --name-status <target-upstream-commit> --
```

The changed-file set on the new branch must be reviewed explicitly. If a file is changed relative to target upstream but is missing from the migration report, the report is incomplete.

### Step 4. Run tests and only then declare success

At minimum run:

```bash
go test ./adapter/outboundgroup ./hub/route
go test ./...
```

If any preserved behavior lacks test coverage, add tests before declaring the merge complete.

Do not pretend GitHub workflow behavior is covered by `go test`. Workflow preservation must be checked by file diff and workflow review, not by Go test.

## Exact behavior contract

This section is the real preservation target. Future maintainers must check every item.

### A. `fallback.go`

Required behavior:

- Selection still respects original proxy order.
- If a selected proxy is unavailable and another alive proxy exists, the result must become the first alive proxy in order.
- If at least one alive proxy exists, `fallback` must not return a timeout or unhealthy node.
- `persistent-pin=true` must keep the pinned node effective across automatic health checks.
- If the pinned node disappears from current members, the selection must be cleared and a warning must be logged.
- Auto-unfix must happen only after `persistent-pin-auto-unfix-threshold` consecutive unhealthy observations.
- The counter may increase only when:
  - pinned node is unhealthy
  - there is at least one alternative alive node
  - the latest test record is newer than the last processed record
- The counter must reset when:
  - pinned node recovers
  - there are successful test records after the previous processed point
  - there is no alternative alive node
  - the pin is manually changed or cleared

### B. `urltest.go`

Required behavior:

- If any alive proxy exists, auto-selection must not choose a timeout or unhealthy node.
- `persistent-pin=true` must keep the pinned node effective during automatic refresh.
- `URLTest()` must refresh cached routing state after test results are updated.
- Missing pinned member must clear the pin and warn.
- Auto-unfix and reset semantics must match `fallback`.
- A manually pinned node must remain effective until:
  - manual unfix
  - missing member clear
  - threshold-triggered auto-unfix

### C. `groupbase.go`

Required behavior:

- Delay results are accepted only when the URL test succeeds and the proxy is still healthy for the same test URL.
- Group health logic must not be fooled by stale successful delay leftovers.

### D. `parser.go`

Required public options:

```yaml
persistent-pin: false
pin-unhealthy-log-interval: 10
persistent-pin-auto-unfix-threshold: 10
```

Required parser semantics:

- `persistent-pin` default is `false`
- `pin-unhealthy-log-interval < 0` is invalid and falls back to fork default
- `pin-unhealthy-log-interval: 0` is not parser-invalid
- `pin-unhealthy-log-interval: 0` still results in constructor-level default `10s`
- `persistent-pin-auto-unfix-threshold` uses fork default when unset
- `persistent-pin-auto-unfix-threshold <= 0` is invalid when explicitly provided
- if `persistent-pin=false`, behavior should stay as close as possible to upstream except for the intentional timeout-selection fix

### E. `hub/route/groups.go`

Required behavior:

- `/groups/{name}/delay` must not clear the selected node when the group implements persistent pin and `persistent-pin=true`
- when `persistent-pin=false`, upstream-style clearing behavior may remain

### F. Logging

Required logs:

- warning for unhealthy pinned proxy
- warning includes auto-unfix counter state
- warning for missing pinned proxy
- reset log when the counter is cleared
- auto-unfix log when threshold is reached

### G. Release workflow

Required workflow behavior in `.github/workflows/build.yml`:

- Docker job remains disabled
- tag push on `v*` can publish a GitHub Release
- release assets are published directly from GitHub Actions
- no local manual upload should be required

## File-by-file checklist

Use this checklist during every future merge.

### `.github/workflows/build.yml`

Confirm:

- Docker path is still disabled
- tag-triggered release path still exists
- artifacts are uploaded from Actions to Release

### `adapter/outboundgroup/fallback.go`

Confirm:

- ordered alive selection is preserved
- persistent pin fields/state still exist
- missing pinned member path exists
- auto-unfix counter path exists
- counter reset paths exist
- JSON output still exposes persistent pin fields

### `adapter/outboundgroup/urltest.go`

Confirm:

- alive-node-only selection preference exists
- persistent pin path exists
- manual `URLTest()` refresh path exists
- missing pinned member path exists
- auto-unfix counter path exists
- counter reset paths exist
- JSON output still exposes persistent pin fields

### `adapter/outboundgroup/groupbase.go`

Confirm:

- URLTest result filtering still requires both success and healthy state

### `adapter/outboundgroup/parser.go`

Confirm:

- three public config options still exist
- validation semantics for `0`, negative, and unset are unchanged

### `adapter/outboundgroup/util.go`

Confirm:

- `PersistentPinAware` still exists if route code depends on it
- helper used for extra delay histories still matches runtime logic

### `hub/route/groups.go`

Confirm:

- delay route still respects persistent pin and does not clear it

### `docs/config.yaml`

Confirm:

- config examples still mention the three persistent pin options
- comments still match real runtime semantics

## Verification modes

Every required item in this document must be proven by one of these two modes:

### Mode 1. Automated verification

Use this only for behavior that is actually covered by repository tests or build commands.

Allowed examples:

- `go test ./adapter/outboundgroup ./hub/route`
- `go test ./...`

### Mode 2. Manual diff verification

Use this for items that cannot be proven by repository tests alone.

Required examples:

- `.github/workflows/build.yml` release behavior
- `docs/config.yaml` semantic accuracy
- remaining runtime diffs caused by upstream structure changes
- JSON exposure checks when not covered by tests

For manual diff verification, always record:

- command used
- file reviewed
- exact conclusion
- why the conclusion is sufficient

## Required regression evidence

Before marking the merge done, verify these cases by test or explicit code review:

1. `fallback` does not select timeout nodes when alive nodes exist
2. `fallback` keeps pin until threshold
3. `fallback` resets counter when no alternative alive proxy exists
4. `fallback` resets counter when pinned proxy recovers
5. `fallback` resets counter when newer successful records exist
6. `fallback` clears missing pinned member
7. `url-test` does not select timeout nodes when alive nodes exist
8. `url-test` refreshes cached state after `URLTest()`
9. `url-test` keeps pin until threshold
10. `url-test` resets counter when pinned proxy recovers
11. `url-test` resets counter when newer successful records exist
12. `url-test` clears missing pinned member
13. `GroupBase.URLTest` filters unhealthy results
14. `/groups/{name}/delay` preserves persistent pin
15. parser preserves `0`, negative, and unset semantics
16. JSON output still exposes persistent pin fields
17. workflow still disables Docker and supports tag-triggered release by manual diff verification

## Stop-and-investigate conditions

Stop and inspect carefully if any of the following happens:

- first cherry-pick conflicts in `fallback.go` or `urltest.go`
- upstream changed `GroupBase` layout or selection interfaces
- upstream changed `/groups/{name}/delay` behavior
- upstream changed delay history storage or health-check state model
- upstream changed release workflow structure significantly
- tests pass but one of the above checklist items cannot be pointed to in code

These are not reasons to simplify the feature. They are reasons to manually replay it more carefully.

## Definition of done

The upstream merge is complete only when all are true:

1. old common base was recomputed from the real target upstream
2. fork-owned file set was re-diffed from code
3. every required behavior in this document was checked
4. remaining old-vs-new diffs are explainable as upstream structural adaptation or upstream-only additions
5. required tests pass
6. docs were updated if the process or semantics changed

If any one of these is missing, the merge is not complete.

## Required merge report template

Every future upstream merge should end with a report using this structure.

### 1. Target and base

- target upstream commit:
- old fork branch commit:
- old common base:

### 2. Fork-owned files reviewed

- file:
  status: preserved / adapted / extended / not-applicable
  evidence:

Repeat for every fork-owned file and every evidence target.

### 3. Files changed relative to target upstream

- file:
  reason changed:
  expected or unexpected:
  evidence:

Repeat for every file shown by `git diff --name-status <target-upstream-commit> --`.

### 4. Required semantics review

- semantic:
  evidence type: automated / manual
  evidence:
  result:

Repeat for every required semantic in this document.

### 5. Remaining old-vs-new diffs

- file:
  diff class:
  why acceptable:

### 6. Commands run

- command:
  result:

### 7. Unresolved risk

- none

If this template cannot be filled without hand-waving, the merge is not ready.
