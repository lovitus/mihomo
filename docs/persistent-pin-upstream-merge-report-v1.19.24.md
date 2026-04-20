# Persistent Pin Upstream Merge Report: Upstream `v1.19.24`

This report records the migration of the fork-specific persistent pin feature set from the historical fork branch onto upstream `v1.19.24`.

It is intentionally explicit so future merges can compare against a known-good preservation report instead of reconstructing intent from commit history alone.

## 1. Target and Base

- target upstream commit: `a84724665eb7f989809abe463c05f5723bd24975`
- target upstream tag or version: `v1.19.24`
- old fork branch: `codex/persistent-pin-option`
- old fork branch commit: `a0611406bfb4760f3b64d12b3d94a8f8b8fe36b6`
- old fork release commit: `1101ff88f227c6575d010a0054b1ed5d31874d91`
- old common base: `0317d9f74229374bd5ed55e4c2bb63f68e06c9c8`
- merge branch: `persistent-pin-option-1.19.24merge`

## 2. Commands Used to Establish Scope

- command: `git rev-parse codex/persistent-pin-option`
  result: `a0611406bfb4760f3b64d12b3d94a8f8b8fe36b6`
- command: `git rev-parse a84724665eb7f989809abe463c05f5723bd24975`
  result: `a84724665eb7f989809abe463c05f5723bd24975`
- command: `git merge-base codex/persistent-pin-option a84724665eb7f989809abe463c05f5723bd24975`
  result: `0317d9f74229374bd5ed55e4c2bb63f68e06c9c8`
- command: `git diff --name-status 0317d9f74229374bd5ed55e4c2bb63f68e06c9c8..codex/persistent-pin-option`
  result: old fork-only scope concentrated in workflow, outboundgroup runtime, route protection, docs, and release/test evidence
- command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 --`
  result: current merge branch changed files include runtime ports, test additions, merge playbooks, merge checklist/report files, release note files, and README fork entrypoints

## 3. Historical Fork Commit Chain Preserved by This Merge

The old fork feature set that had to be preserved came from this commit lineage on top of the old common base:

1. `7498386e` `fix(outboundgroup): avoid selecting timeout nodes when alive nodes exist`
2. `021ca2af` `feat(group): add optional persistent pin mode for url-test/fallback`
3. `a9838966` `feat(group): add auto-unfix threshold for persistent pin`
4. `6c00ba3a` `chore(ci): disable docker job in fork builds`
5. `1101ff88` `chore(group): enrich persistent pin counter logs`
6. `a0611406` `ci(release): auto publish release on v* tag push`

This merge replayed those behaviors onto upstream `v1.19.24` rather than cherry-picking them mechanically.

## 4. Fork-Owned Files Reviewed

### `.github/workflows/build.yml`

- status: adapted
- evidence_type: manual
- evidence_command: `git diff codex/persistent-pin-option -- .github/workflows/build.yml`
- result: preserves disabled Docker job and tag-triggered release publishing on top of upstream workflow structure
- diff_class: structural adaptation
- why_acceptable: upstream workflow layout changed, but fork-required release behavior remains intact

### `adapter/outboundgroup/fallback.go`

- status: adapted
- evidence_type: automated + manual
- evidence_command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/fallback.go`
- result: ordered first-alive selection, persistent pin state machine, missing-member clear, reset paths, auto-unfix, and JSON exposure preserved
- diff_class: structural adaptation
- why_acceptable: implementation was fitted to `v1.19.24` layout without dropping fork semantics

### `adapter/outboundgroup/groupbase.go`

- status: preserved
- evidence_type: automated + manual
- evidence_command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/groupbase.go`
- result: URL test result filtering still requires success plus healthy state
- diff_class: none
- why_acceptable: old fork semantic carried over directly

### `adapter/outboundgroup/parser.go`

- status: preserved
- evidence_type: automated + manual
- evidence_command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/parser.go`
- result: public options and `unset / 0 / invalid` validation semantics preserved
- diff_class: none
- why_acceptable: parser behavior matches old fork behavior contract

### `adapter/outboundgroup/urltest.go`

- status: adapted
- evidence_type: automated + manual
- evidence_command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/urltest.go`
- result: alive-only selection preference, persistent pin retention, manual URLTest refresh, missing-member clear, reset paths, auto-unfix, and JSON exposure preserved
- diff_class: structural adaptation
- why_acceptable: implementation moved with upstream structure while preserving required fork semantics

### `adapter/outboundgroup/util.go`

- status: preserved
- evidence_type: manual
- evidence_command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/util.go`
- result: `PersistentPinAware` plus extra-delay-history helpers preserved
- diff_class: none
- why_acceptable: exact helper surface still supports route protection and runtime counter logic

### `adapter/outboundgroup/persistent_pin_test.go`

- status: extended
- evidence_type: automated + manual
- evidence_command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/persistent_pin_test.go`
- result: old fork behavior now has explicit regression tests for missing-member, recovery, successful-record reset, JSON fields, parser semantics, and URLTest refresh behavior
- diff_class: upstream-only addition
- why_acceptable: this file strengthens evidence coverage without changing runtime semantics

### `docs/config.yaml`

- status: preserved
- evidence_type: manual
- evidence_command: `git diff codex/persistent-pin-option -- docs/config.yaml`
- result: persistent pin config examples remain documented for both `url-test` and `fallback`
- diff_class: none
- why_acceptable: documentation still matches runtime config surface

### `hub/route/groups.go`

- status: preserved
- evidence_type: automated + manual
- evidence_command: `git diff codex/persistent-pin-option -- hub/route/groups.go`
- result: `/groups/{name}/delay` still respects persistent pin and does not silently clear it when enabled
- diff_class: none
- why_acceptable: route protection behavior is preserved directly

### `hub/route/groups_test.go`

- status: extended
- evidence_type: automated + manual
- evidence_command: `git diff codex/persistent-pin-option -- hub/route/groups_test.go`
- result: explicit regression coverage now proves both protected and non-protected `/groups/{name}/delay` paths
- diff_class: upstream-only addition
- why_acceptable: this file strengthens verification without changing runtime behavior

## 5. Files Changed Relative to Target Upstream

Every file in the merge branch changed-file set must appear exactly once below.

### Runtime and workflow files

- file: `.github/workflows/build.yml`
  reason_changed: preserved fork release behavior on upstream workflow structure
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- .github/workflows/build.yml`
  result: changed

- file: `adapter/outboundgroup/fallback.go`
  reason_changed: replayed fallback timeout-avoidance and persistent pin semantics
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- adapter/outboundgroup/fallback.go`
  result: changed

- file: `adapter/outboundgroup/groupbase.go`
  reason_changed: preserved healthy-result filtering in URL tests
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- adapter/outboundgroup/groupbase.go`
  result: changed

- file: `adapter/outboundgroup/parser.go`
  reason_changed: preserved persistent pin config parsing and validation semantics
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- adapter/outboundgroup/parser.go`
  result: changed

- file: `adapter/outboundgroup/urltest.go`
  reason_changed: replayed url-test timeout-avoidance, persistent pin, and manual refresh semantics
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- adapter/outboundgroup/urltest.go`
  result: changed

- file: `adapter/outboundgroup/util.go`
  reason_changed: preserved shared persistent pin helper surface
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- adapter/outboundgroup/util.go`
  result: changed

- file: `docs/config.yaml`
  reason_changed: preserved fork config documentation for persistent pin options
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- docs/config.yaml`
  result: changed

- file: `hub/route/groups.go`
  reason_changed: preserved delay-route pin protection
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- hub/route/groups.go`
  result: changed

### Evidence and documentation files

- file: `adapter/outboundgroup/persistent_pin_test.go`
  reason_changed: new regression test file for fork runtime semantics
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- adapter/outboundgroup/persistent_pin_test.go`
  result: added

- file: `hub/route/groups_test.go`
  reason_changed: new regression test file for delay-route pin protection
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- hub/route/groups_test.go`
  result: added

- file: `docs/persistent-pin-upstream-merge-human.md`
  reason_changed: long-form human merge protocol
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- docs/persistent-pin-upstream-merge-human.md`
  result: added

- file: `docs/persistent-pin-upstream-merge-ai.md`
  reason_changed: strict AI/agent merge protocol
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- docs/persistent-pin-upstream-merge-ai.md`
  result: added

- file: `docs/persistent-pin-upstream-merge-checklist.md`
  reason_changed: one-page operational checklist for future merges
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- docs/persistent-pin-upstream-merge-checklist.md`
  result: added

- file: `docs/persistent-pin-upstream-merge-report-template.md`
  reason_changed: reusable audit report template for future merges
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- docs/persistent-pin-upstream-merge-report-template.md`
  result: added

- file: `docs/persistent-pin-upstream-merge-report-v1.19.24.md`
  reason_changed: filled audit report for this specific upstream `v1.19.24` migration
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- docs/persistent-pin-upstream-merge-report-v1.19.24.md`
  result: added

- file: `docs/releases/v2026.04.20-persistent-pin.4.md`
  reason_changed: detailed release note for this fork release
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- docs/releases/v2026.04.20-persistent-pin.4.md`
  result: added

- file: `README.md`
  reason_changed: added fork documentation entry points
  expected_or_unexpected: expected
  evidence_command: `git diff --name-status a84724665eb7f989809abe463c05f5723bd24975 -- README.md`
  result: changed

## 6. Required Semantics Review

1. semantic: `fallback` selects the first alive proxy in original order
   evidence_type: automated + manual
   evidence_command: `go test ./adapter/outboundgroup ./hub/route` plus review of `adapter/outboundgroup/fallback.go`
   result: preserved
   notes: selection remains order-based, not latency-based

2. semantic: `fallback` does not select timeout/unhealthy nodes when alive alternatives exist
   evidence_type: automated
   evidence_command: `go test ./adapter/outboundgroup ./hub/route`
   result: preserved
   notes: covered by persistent pin regression tests

3. semantic: `fallback` preserves pin when `persistent-pin=true`
   evidence_type: automated
   evidence_command: `go test ./adapter/outboundgroup ./hub/route`
   result: preserved
   notes: covered by threshold-hold tests

4. semantic: `fallback` auto-unfix triggers only after threshold
   evidence_type: automated
   evidence_command: `go test ./adapter/outboundgroup ./hub/route`
   result: preserved
   notes: explicit threshold test present

5. semantic: `fallback` resets counter when no alternative alive proxy exists
   evidence_type: automated
   evidence_command: `go test ./adapter/outboundgroup ./hub/route`
   result: preserved
   notes: explicit reset test present

6. semantic: `fallback` resets counter when pinned proxy recovers
   evidence_type: automated
   evidence_command: `go test ./adapter/outboundgroup ./hub/route`
   result: preserved
   notes: explicit recovery test present

7. semantic: `fallback` resets counter when newer successful records exist after the last checkpoint
   evidence_type: automated
   evidence_command: `go test ./adapter/outboundgroup ./hub/route`
   result: preserved
   notes: explicit successful-record reset test present

8. semantic: `fallback` clears and warns when pinned proxy disappears
   evidence_type: automated + manual
   evidence_command: `go test ./adapter/outboundgroup ./hub/route` and review of `fallback.go`
   result: preserved
   notes: missing-member path and warning retained

9. semantic: `url-test` does not select timeout/unhealthy nodes when alive alternatives exist
   evidence_type: automated
   evidence_command: `go test ./adapter/outboundgroup ./hub/route`
   result: preserved
   notes: explicit alive-preference tests present

10. semantic: `url-test` preserves pin during automatic refresh
    evidence_type: automated
    evidence_command: `go test ./adapter/outboundgroup ./hub/route`
    result: preserved
    notes: explicit threshold-hold tests present

11. semantic: `url-test` refreshes cached routing state after manual `URLTest()`
    evidence_type: automated
    evidence_command: `go test ./adapter/outboundgroup ./hub/route`
    result: preserved
    notes: explicit refresh test present

12. semantic: `url-test` auto-unfix and reset semantics match `fallback`
    evidence_type: automated + manual
    evidence_command: `go test ./adapter/outboundgroup ./hub/route` and review of `urltest.go`
    result: preserved
    notes: threshold, recovery, successful-record, and missing-member paths all covered

13. semantic: `url-test` clears and warns when pinned proxy disappears
    evidence_type: automated + manual
    evidence_command: `go test ./adapter/outboundgroup ./hub/route` and review of `urltest.go`
    result: preserved
    notes: explicit missing-member path retained

14. semantic: `GroupBase.URLTest` only records valid healthy results
    evidence_type: automated
    evidence_command: `go test ./adapter/outboundgroup ./hub/route`
    result: preserved
    notes: explicit filtering test present

15. semantic: parser preserves `persistent-pin`
    evidence_type: automated + manual
    evidence_command: `go test ./adapter/outboundgroup ./hub/route` and review of `parser.go`
    result: preserved
    notes: public option still parsed and passed through constructors

16. semantic: parser preserves `pin-unhealthy-log-interval` semantics for unset / `0` / negative
    evidence_type: automated
    evidence_command: `go test ./adapter/outboundgroup ./hub/route`
    result: preserved
    notes: explicit zero and negative validation tests present; constructor default remains 10s

17. semantic: parser preserves `persistent-pin-auto-unfix-threshold` semantics for unset / invalid
    evidence_type: automated
    evidence_command: `go test ./adapter/outboundgroup ./hub/route`
    result: preserved
    notes: explicit unset and zero handling tests present

18. semantic: `/groups/{name}/delay` preserves persistent pin
    evidence_type: automated
    evidence_command: `go test ./adapter/outboundgroup ./hub/route`
    result: preserved
    notes: route protection tests cover both persistent and non-persistent paths

19. semantic: JSON output still exposes persistent pin fields
    evidence_type: automated
    evidence_command: `go test ./adapter/outboundgroup ./hub/route`
    result: preserved
    notes: explicit JSON field assertions added for `fallback` and `url-test`

20. semantic: logging still includes unhealthy pin, reset, missing-pin, and auto-unfix messages
    evidence_type: manual
    evidence_command: `rg -n "keeps persistent pin|reset persistent pin auto-unfix counter|cleared persistent pin|auto-unfixed persistent pin" adapter/outboundgroup`
    result: preserved
    notes: messages remain present in both `fallback.go` and `urltest.go`

21. semantic: workflow still disables Docker builds
    evidence_type: manual
    evidence_command: `git diff codex/persistent-pin-option -- .github/workflows/build.yml && rg -n "Disabled in this fork|if: \\$\\{\\{ false \\}\\}" .github/workflows/build.yml`
    result: preserved
    notes: Docker path remains disabled by explicit guard

22. semantic: workflow still supports tag-triggered release publishing
    evidence_type: manual
    evidence_command: `rg -n "Upload-Tag-Release|startsWith\\(github.ref, 'refs/tags/v'\\)|action-gh-release" .github/workflows/build.yml`
    result: preserved
    notes: release assets are still published by Actions on `v*` tag push

## 7. Automated Checks

- command: `go test ./adapter/outboundgroup ./hub/route`
  result: `ok github.com/metacubex/mihomo/adapter/outboundgroup` and `ok github.com/metacubex/mihomo/hub/route`

- command: `go test ./adapter/outboundgroup -run 'Test(Fallback|URLTest|ParseProxyGroup|GroupBase)' -count=1`
  result: `ok github.com/metacubex/mihomo/adapter/outboundgroup`

## 8. Manual Diff Verification

- topic: workflow release preservation
  file: `.github/workflows/build.yml`
  command: `git diff codex/persistent-pin-option -- .github/workflows/build.yml`
  conclusion: Docker remains disabled and `v*` tag release publishing remains available
  why_sufficient: workflow release behavior cannot be exercised by `go test`; diff plus workflow inspection is the correct evidence mode

- topic: config documentation preservation
  file: `docs/config.yaml`
  command: `git diff codex/persistent-pin-option -- docs/config.yaml`
  conclusion: both `url-test` and `fallback` examples still describe the three persistent pin options
  why_sufficient: this file is pure documentation and is best verified by direct diff review

- topic: runtime structural adaptation
  file: `adapter/outboundgroup/fallback.go` and `adapter/outboundgroup/urltest.go`
  command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/fallback.go adapter/outboundgroup/urltest.go`
  conclusion: remaining differences are attributable to upstream `v1.19.24` structure, not lost fork behavior
  why_sufficient: runtime semantics were also backed by automated regression tests

## 9. Remaining Old-vs-New Diffs

- file: `.github/workflows/build.yml`
  diff_class: structural adaptation
  evidence_command: `git diff codex/persistent-pin-option -- .github/workflows/build.yml`
  why_acceptable: upstream workflow moved, but fork-required release behavior remains

- file: `adapter/outboundgroup/fallback.go`
  diff_class: structural adaptation
  evidence_command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/fallback.go`
  why_acceptable: implementation adapted to current upstream structure while preserving old semantics

- file: `adapter/outboundgroup/urltest.go`
  diff_class: structural adaptation
  evidence_command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/urltest.go`
  why_acceptable: implementation adapted to current upstream structure while preserving old semantics

- file: `adapter/outboundgroup/persistent_pin_test.go`
  diff_class: upstream-only addition
  evidence_command: `git diff codex/persistent-pin-option -- adapter/outboundgroup/persistent_pin_test.go`
  why_acceptable: adds preservation evidence, not behavior changes

- file: `hub/route/groups_test.go`
  diff_class: upstream-only addition
  evidence_command: `git diff codex/persistent-pin-option -- hub/route/groups_test.go`
  why_acceptable: adds route protection evidence, not behavior changes

## 10. Unresolved Risk

- none

## 11. Final Readiness Decision

- ready_to_release: yes
- rationale: fork-owned files were reviewed, all changed files relative to target upstream are accounted for, required semantics were checked by automated or manual evidence, workflow preservation was verified manually, and targeted regression tests passed
