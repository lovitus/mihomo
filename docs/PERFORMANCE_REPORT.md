# Performance Audit Report (2026-03-04)

This report focuses on runtime hot paths for large deployments (tens of groups, hundreds of nodes).

## Scope

- Group selection and URL probing
- Provider health checks
- Rule matching path
- DNS query path
- Logging path

## Executive Summary

Top opportunities with highest ROI:

1. Limit concurrent group URL tests in `adapter/outboundgroup/groupbase.go`.
2. Precompile and cache health-check filter regex in `adapter/provider/healthcheck.go`.
3. Add log-level fast path in `log/log.go` to avoid formatting and channel handoff when logs are disabled.
4. Build a rule pre-index for `tunnel.match` to reduce full-scan rule matching.

Expected impact for large configs:

- Lower CPU spikes during probe rounds.
- Lower goroutine count and GC pressure.
- More stable latency under heavy rule sets.
- Better throughput during peak connection creation.

## Detailed Findings

### 1) Unbounded URLTest fan-out (High impact, Low risk)

File: `adapter/outboundgroup/groupbase.go:217`

Current behavior:

- One goroutine is created for each proxy in a group (`for` + `go func`).
- With many groups and many nodes, this can create bursty goroutine storms.

Optimization:

- Add bounded concurrency (worker pool or semaphore).
- Suggested default: `min(32, len(proxies))`, with optional config override.

Expected gain:

- Significant drop in CPU burst and scheduler pressure.
- More predictable probe completion time under load.

Risk:

- Minimal; only changes probe parallelism, not probe logic.

### 2) Regex recompile in health check loop (High impact, Low risk)

File: `adapter/provider/healthcheck.go:157`

Current behavior:

- `regexp2.MustCompile(strings.Join(filters, "|"), ...)` runs in `execute`, i.e. every check cycle.

Optimization:

- Precompile regex when registering health-check tasks.
- Store compiled regex in `extraOption`.

Expected gain:

- Reduces repeated allocations and compile CPU.
- Noticeable when many groups/tasks and short intervals are used.

Risk:

- Low; behavior remains identical if compile happens at registration time.

### 3) Logging always formats and emits events (High impact, Medium risk)

File: `log/log.go:37-59`, `log/log.go:99`

Current behavior:

- All log calls create formatted strings (`fmt.Sprintf`) and send to `logCh`, even if current level suppresses output.

Optimization:

- Add early check before formatting and channel send for levels below current threshold.
- Optional: make event stream behavior configurable if any UI relies on all-level events.

Expected gain:

- Large CPU reduction in debug-heavy paths (DNS/routing/probing loops).
- Less channel contention.

Risk:

- Medium because current event-stream semantics may rely on suppressed logs still being emitted.

### 4) Rule matching is full linear scan (High impact, High risk)

File: `tunnel/tunnel.go:633`

Current behavior:

- Each connection iterates all rules in order until match.
- Complexity is O(number of rules) per connection.

Optimization:

- Build pre-indexed candidate buckets by rule type/domain suffix/port/network.
- Keep strict rule-order semantics inside each bucket.

Expected gain:

- Major throughput gains for large rule sets.

Risk:

- High; rule-order semantics are sensitive and must be preserved exactly.

### 5) Group proxy filtering recomputation strategy (Medium impact, Medium risk)

File: `adapter/outboundgroup/groupbase.go:102`

Current behavior:

- Any provider version change invalidates whole cached proxy set, then recomputes full merged/filter pipeline.

Optimization:

- Keep per-provider filtered caches and only rebuild affected provider slices.
- Merge cached slices at the end.

Expected gain:

- Lower CPU in frequent provider refresh scenarios.

Risk:

- Medium; cache invalidation logic becomes more complex.

### 6) FakeIP skipper performs linear matcher scans (Medium impact, Low risk)

File: `component/fakeip/skipper.go:19`

Current behavior:

- Domain skip check iterates all rules/matchers for each query.

Optimization:

- Add trie/set pre-index for common exact/suffix host patterns.
- Keep fallback to current matcher for complex rules.

Expected gain:

- Better DNS throughput when `fake-ip-filter` is large.

Risk:

- Low if fallback path is retained.

### 7) DNS upstream strategy always races all main clients (Medium impact, Medium risk)

File: `dns/util.go:347`

Current behavior:

- `batchExchange` sends queries to all configured clients in parallel.

Optimization:

- Use staged hedging: first preferred server, then hedge after a short delay.

Expected gain:

- Lower upstream query volume and lower CPU/network usage.

Risk:

- Medium; may increase tail latency if hedge timing is misconfigured.

## Recommended Roadmap

Phase 1 (safe and immediate):

1. Limit group URLTest concurrency.
2. Precompile health-check regex.
3. Add optional log fast-path behind a compatibility switch.

Phase 2 (higher gain, needs thorough regression testing):

1. Rule matching pre-index.
2. Incremental group proxy cache.
3. DNS hedged strategy.

## Validation Plan

- Functional:
  - `go test ./...`
  - `go vet ./...`
  - group selection regression tests for URLTest/Fallback behavior
- Performance:
  - benchmark route match throughput with 1k/5k rules
  - benchmark health-check cycles with 300+ proxies
  - track goroutine peak and GC pause during probe bursts
