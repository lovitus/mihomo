# Persistent Pin Upstream Merge Report Template

Use this template at the end of every upstream merge for this fork.

Do not replace sections with a short summary.
Do not omit sections because "nothing changed".
If a section is not applicable, write `none` and explain why.

This report is not optional. If it cannot be filled precisely, the merge is not ready.

## 1. Target and Base

- target upstream commit:
- target upstream tag or version:
- old fork branch:
- old fork branch commit:
- old fork release commit:
- old common base:
- merge branch:

## 2. Commands Used to Establish Scope

- command: `git rev-parse codex/persistent-pin-option`
  result:
- command: `git rev-parse <target-upstream-commit>`
  result:
- command: `git merge-base codex/persistent-pin-option <target-upstream-commit>`
  result:
- command: `git diff --name-status <old-common-base>..codex/persistent-pin-option`
  result:
- command: `git diff --name-status <target-upstream-commit> --`
  result:

## 3. Fork-Owned Files Reviewed

Repeat this block for every fork-owned file and evidence target:

```text
file: .github/workflows/build.yml
status: preserved | adapted | extended | not-applicable
evidence_type: manual
evidence_command:
result:
diff_class: structural adaptation | upstream-only addition | missing behavior | none
why_acceptable:
```

Status meaning:

- `preserved`: old fork behavior remains intact without needing extra scope beyond preservation
- `adapted`: old fork behavior remains intact, but implementation changed because upstream structure changed
- `extended`: old fork behavior remains intact and the file also contains additional acceptable enhancement such as stronger tests or upstream-compatible workflow improvement
- `not-applicable`: the file was reviewed and explicitly determined not to require behavior-bearing changes for this merge; this must be explained

Required files:

- `.github/workflows/build.yml`
- `adapter/outboundgroup/fallback.go`
- `adapter/outboundgroup/groupbase.go`
- `adapter/outboundgroup/parser.go`
- `adapter/outboundgroup/urltest.go`
- `adapter/outboundgroup/util.go`
- `adapter/outboundgroup/persistent_pin_test.go`
- `docs/config.yaml`
- `hub/route/groups.go`
- `hub/route/groups_test.go`

## 4. Files Changed Relative to Target Upstream

Repeat this block for every file shown by:

```bash
git diff --name-status <target-upstream-commit> --
```

This section must account for every file in that changed-file set exactly once.
If a file appears in `git diff --name-status <target-upstream-commit> --` but is missing here, the report is incomplete.

Template:

```text
file:
reason_changed:
expected_or_unexpected:
evidence_command:
result:
```

If any changed file is missing from this section, the report is incomplete.

## 5. Required Semantics Review

Repeat this block for every required semantic:

```text
semantic:
evidence_type: automated | manual
evidence_command:
result: preserved | missing | partially preserved
notes:
```

Required semantics:

1. `fallback` selects the first alive proxy in original order
2. `fallback` does not select timeout/unhealthy nodes when alive alternatives exist
3. `fallback` preserves pin when `persistent-pin=true`
4. `fallback` auto-unfix triggers only after threshold
5. `fallback` resets counter when no alternative alive proxy exists
6. `fallback` resets counter when pinned proxy recovers
7. `fallback` resets counter when newer successful records exist after the last checkpoint
8. `fallback` clears and warns when pinned proxy disappears
9. `url-test` does not select timeout/unhealthy nodes when alive alternatives exist
10. `url-test` preserves pin during automatic refresh
11. `url-test` refreshes cached routing state after manual `URLTest()`
12. `url-test` auto-unfix and reset semantics match `fallback`
13. `url-test` clears and warns when pinned proxy disappears
14. `GroupBase.URLTest` only records valid healthy results
15. parser preserves `persistent-pin`
16. parser preserves `pin-unhealthy-log-interval` semantics for unset / `0` / negative
17. parser preserves `persistent-pin-auto-unfix-threshold` semantics for unset / invalid
18. `/groups/{name}/delay` preserves persistent pin
19. JSON output still exposes persistent pin fields
20. logging still includes unhealthy pin, reset, missing-pin, and auto-unfix messages
21. workflow still disables Docker builds
22. workflow still supports tag-triggered release publishing

## 6. Automated Checks

List every automated command actually run.

```text
command: go test ./adapter/outboundgroup ./hub/route
result:
```

```text
command: go test ./...
result:
```

Add more blocks if additional automated checks were run.

Do not list commands that were not actually executed.

## 7. Manual Diff Verification

List every conclusion that depended on manual review rather than repository tests.

Template:

```text
topic:
file:
command:
conclusion:
why_sufficient:
```

This section is required for at least:

- `.github/workflows/build.yml`
- `docs/config.yaml`
- remaining structural diffs versus old fork branch
- any JSON exposure check not explicitly covered by tests

## 8. Remaining Old-vs-New Diffs

Repeat this block for every remaining diff from:

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

Template:

```text
file:
diff_class: structural adaptation | upstream-only addition | missing behavior | none
evidence_command:
why_acceptable:
```

`missing behavior` is a hard blocker. Do not close the report with any unresolved `missing behavior`.

## 9. Unresolved Risk

- none

If there is any unresolved risk, describe it precisely:

```text
risk:
impact:
why_not_resolved:
required_followup:
```

## 10. Final Readiness Decision

- ready_to_release: yes | no
- rationale:

If `ready_to_release` is `yes`, that means:

- every fork-owned file was reviewed
- every changed file relative to target upstream was reviewed
- every required semantic was checked
- remaining diffs were classified with evidence
- automated checks passed
- manual verification gaps were documented

If any one of those is false, the answer must be `no`.
