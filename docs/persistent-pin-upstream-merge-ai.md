# Persistent Pin Upstream Merge Playbook (AI)

## Objective

You are upgrading this fork to a newer upstream `MetaCubeX/mihomo` base.

Your task is not to "merge a branch".
Your task is to preserve 100% of the fork-specific persistent-pin behavior and its related release workflow behavior on top of the latest upstream code.

You must work from code diff and test evidence only.

Do not infer behavior from:

- commit messages
- PR descriptions
- branch names
- comments alone
- memory

## Required source-of-truth inputs

Before doing anything else, determine:

- old fork branch, normally `codex/persistent-pin-option`
- old fork branch commit
- old fork release commit if relevant
- common ancestor between old fork branch and target upstream
- target upstream commit

Use commands like:

```bash
git rev-parse codex/persistent-pin-option
git rev-parse <target-upstream-commit>
git merge-base codex/persistent-pin-option <target-upstream-commit>
git diff --name-status <old-common-base>..codex/persistent-pin-option
```

Also record the actual touched file set on the new merge branch relative to the target upstream base:

```bash
git diff --name-status <target-upstream-commit> --
```

Then lock the exact old fork diff for the fork-owned files:

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

If the diff shows additional fork-owned files, expand the scope before proceeding.

If the current merge branch changes files outside the intended migration scope, classify each extra changed file as:

1. required structural adaptation
2. required new test/support file
3. accidental or unrelated change

Class 3 is a blocker.

## Mandatory migration scope

Treat these files as fork-owned migration scope unless the real diff proves otherwise:

- `.github/workflows/build.yml`
- `adapter/outboundgroup/fallback.go`
- `adapter/outboundgroup/groupbase.go`
- `adapter/outboundgroup/parser.go`
- `adapter/outboundgroup/urltest.go`
- `adapter/outboundgroup/util.go`
- `docs/config.yaml`
- `hub/route/groups.go`

Also treat these test files as mandatory evidence targets:

- `adapter/outboundgroup/persistent_pin_test.go`
- `hub/route/groups_test.go`

Do not report completion until each fork-owned file and each evidence target has been reviewed:

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

## Required workflow

### 1. Start from the new upstream base

Create a fresh branch from the target upstream commit.

Do not:

- rebase the old fork branch and assume success
- bulk-copy old files over current upstream files
- stop after cherry-pick succeeds

### 2. Replay behavior onto the new structure

Use the latest upstream structure as the host.

Allowed:

- adapting to field moves
- adapting to renamed methods or interfaces
- adapting to upstream refactors
- keeping unrelated upstream improvements

Not allowed:

- removing old fork behavior because the new structure looks different
- replacing diff review with "this should be equivalent"
- reporting success without proving each required semantic

### 3. Audit the post-port diff against the old branch

Run:

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

Classify every remaining diff into exactly one class:

1. required upstream structural adaptation
2. upstream-only addition unrelated to fork behavior
3. missing fork behavior

Class 3 is a hard blocker.

For every remaining diff, you must record:

- file path
- inspection command used
- whether comparison was against old fork branch or target upstream
- chosen class
- explanation tied to code, not inference

If you cannot produce that evidence, the classification is invalid.

Also inspect the changed-file set relative to target upstream:

```bash
git diff --name-status <target-upstream-commit> --
```

Every file changed relative to target upstream must appear in the final report. If a file is changed but omitted from the report, the report is incomplete.

### 4. Verify with tests

Run:

```bash
go test ./adapter/outboundgroup ./hub/route
go test ./...
```

If a required behavior is not covered by tests, add tests before reporting success.

Do not claim that GitHub workflow behavior was "tested" by `go test`.
Workflow and release-path preservation must be verified by file diff and manual review.

## Verification modes

Every required conclusion must use one of these evidence modes:

### Mode A. Automated verification

Allowed only when repository commands really exercise the target behavior.

Examples:

- `go test ./adapter/outboundgroup ./hub/route`
- `go test ./...`

### Mode B. Manual diff verification

Required for:

- `.github/workflows/build.yml`
- `docs/config.yaml`
- remaining runtime diffs explained as structural adaptation
- JSON field exposure when not directly covered by tests

For every manual verification result, include:

- command used
- file reviewed
- exact conclusion
- why that conclusion is sufficient

## Required semantics to preserve

### A. `fallback.go`

Must preserve:

- ordered selection of the first alive proxy
- no timeout-node selection when any alive proxy exists
- persistent pin survives automatic checks
- missing pinned proxy clears the pin and warns
- auto-unfix only after threshold
- counter increments only when pinned proxy is unhealthy and an alternative alive proxy exists
- counter resets when:
  - pinned proxy recovers
  - successful test records exist after the last processed checkpoint
  - no alternative alive proxy exists
  - selection changes manually
- JSON output exposes:
  - `fixed`
  - `persistentPin`
  - `pinUnhealthyLogInterval`
  - `persistentPinAutoUnfixThreshold`

### B. `urltest.go`

Must preserve:

- no timeout-node selection when any alive proxy exists
- persistent pin survives automatic URL-test refresh
- manual `URLTest()` refreshes cached routing state
- missing pinned proxy clears the pin and warns
- same auto-unfix and counter-reset rules as `fallback`
- JSON output exposes:
  - `fixed`
  - `persistentPin`
  - `pinUnhealthyLogInterval`
  - `persistentPinAutoUnfixThreshold`

### C. `groupbase.go`

Must preserve:

- delay map records only valid successful results that are also still healthy for the same test URL

### D. `parser.go`

Must preserve the public options:

```yaml
persistent-pin: false
pin-unhealthy-log-interval: 10
persistent-pin-auto-unfix-threshold: 10
```

Must preserve validation semantics:

- `persistent-pin` default is `false`
- `pin-unhealthy-log-interval < 0` invalid
- `pin-unhealthy-log-interval: 0` not parser-invalid
- `pin-unhealthy-log-interval: 0` still leads to constructor default `10s`
- `persistent-pin-auto-unfix-threshold` unset uses default
- explicit `persistent-pin-auto-unfix-threshold <= 0` invalid

### E. `hub/route/groups.go`

Must preserve:

- `/groups/{name}/delay` does not clear pin when the group is persistent-pin-aware and `persistent-pin=true`

### F. Logging

Must preserve:

- unhealthy pin warning
- counter visible in warning
- reset log with reason
- missing pinned member warning
- auto-unfix log with reached count

### G. Workflow

Must preserve in `.github/workflows/build.yml`:

- Docker build path disabled
- tag-triggered GitHub Release publishing
- workflow-driven asset publication

## Required file-by-file review checklist

Use this checklist explicitly and record the result in your final report.

### `.github/workflows/build.yml`

Verify:

- Docker path disabled
- tag release path present
- release artifacts published by workflow

### `adapter/outboundgroup/fallback.go`

Verify:

- ordered alive selection still exists
- persistent pin state still exists
- missing member path exists
- auto-unfix path exists
- reset paths exist
- JSON fields exist

### `adapter/outboundgroup/urltest.go`

Verify:

- alive-only selection preference exists
- persistent pin path exists
- manual `URLTest()` refresh path exists
- missing member path exists
- auto-unfix path exists
- reset paths exist
- JSON fields exist

### `adapter/outboundgroup/groupbase.go`

Verify:

- healthy-result filtering exists

### `adapter/outboundgroup/parser.go`

Verify:

- three public options exist
- `0`, negative, and unset semantics are unchanged

### `adapter/outboundgroup/util.go`

Verify:

- `PersistentPinAware` remains available if route code depends on it
- delay-history helper still supports runtime logic

### `hub/route/groups.go`

Verify:

- delay route preserves persistent pin

### `docs/config.yaml`

Verify:

- config examples still mention the three options
- comments still match runtime semantics

## Required regression evidence

Before reporting completion, ensure tests exist and pass for:

1. `fallback` refuses timeout choice when alive alternative exists
2. `fallback` keeps pin until threshold
3. `fallback` resets counter when no alternative alive node exists
4. `fallback` resets counter when pinned proxy recovers
5. `fallback` resets counter when newer successful records exist
6. `fallback` clears missing pinned member
7. `url-test` refuses timeout choice when alive alternative exists
8. `url-test` refreshes cached state after manual `URLTest()`
9. `url-test` keeps pin until threshold
10. `url-test` resets counter when pinned proxy recovers
11. `url-test` resets counter when newer successful records exist
12. `url-test` clears missing pinned member
13. `GroupBase.URLTest` filters unhealthy results
14. `/groups/{name}/delay` preserves persistent pin
15. parser preserves `0`, negative, and unset semantics
16. JSON output exposes persistent pin fields
17. workflow still disables Docker and supports tag-triggered release by manual diff verification

## Explicit anti-patterns

Do not say any of the following without code proof:

- "This should be equivalent"
- "Looks unchanged"
- "Probably covered by that commit"
- "Only comments changed"
- "Cherry-pick was clean so behavior is preserved"
- "The old branch likely intended..."

Every such statement is invalid unless tied to diff or test evidence.

## Required final report structure

When reporting completion, include all of the following:

1. target upstream commit
2. old common base used for diff
3. fork-owned files reviewed
4. files changed relative to target upstream and why each changed
5. remaining old-vs-new diffs and why each is acceptable
6. automated checks run
7. any unresolved risk

If any of those sections is missing, the report is incomplete.

## Required per-file report format

For each fork-owned file and evidence target, report in this format:

```text
file: <path>
status: preserved | adapted | extended | not-applicable
evidence_type: automated | manual
evidence_command: <command>
result: <precise conclusion>
```

If a file still differs from the old branch, add:

```text
diff_class: structural adaptation | upstream-only addition | missing behavior
why_acceptable: <code-based explanation>
```

Do not compress multiple files into one vague summary.

## Required semantic report format

For each required semantic, report:

```text
semantic: <name>
evidence_type: automated | manual
evidence_command: <command>
result: preserved | missing | partially preserved
notes: <short code-based justification>
```

Do not mark a semantic as preserved without either a test result or an explicit code-path review.

## Completion gate

You may say the migration is complete only if all are true:

1. old fork-only file set identified from real diff
2. each fork-owned file reviewed against old branch code
3. all fork semantics preserved
4. remaining diffs explained as upstream structural adaptation or upstream-only additions
5. required tests pass

If any of the above is not true, the migration is not complete.
