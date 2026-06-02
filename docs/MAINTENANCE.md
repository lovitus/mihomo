# Branch Maintenance Guide

## Active feature branches

| Branch | Purpose | Base |
|--------|---------|------|
| persistent-pin-option-{VER}merge | Persistent pin selection feature, no tsnet | upstream vVER |
| pin-{VER}merge-tailscale | Above + tsnet mesh overlay (tailscale.com v1.98.3+) | persistent-pin-option-{VER}merge |

## What we keep vs what we exclude from upstream

### We KEEP (our features)
- adapter/outboundgroup/fallback.go -- persistentPin logic
- adapter/outboundgroup/urltest.go -- persistentPin + setFastNode sync
- adapter/outboundgroup/groupbase.go -- shared pin state
- hub/route/groups.go -- API endpoints for pin
- component/tsnet/ -- tsnet mesh overlay (tsnet branch only)
- adapter/outbound/socks5.go -- usedTsnet retry logic (tsnet branch only)

### We EXCLUDE from upstream (upstream tailscale outbound -- different feature, different deps)
- adapter/outbound/tailscale.go
- adapter/outbound/tailscale_stub.go
- constant/features/no_tailscale.go
- constant/features/no_tailscale_stub.go
- dns/tailscale.go
- go.mod: github.com/metacubex/tailscale, github.com/metacubex/tailscale-wireguard-go

### Stale reference sites (check after every upstream merge)
- constant/features/tags.go: NoTailscale block
- dns/util.go: case "tailscale"
- adapter/parser.go: case "tailscale"
- config/config.go: case "tailscale" DNS

## Key dependency constraints

| Branch type | tailscale dep | metacubex/http | Go min |
|------------|--------------|----------------|--------|
| persistent-pin-option | none | v0.1.6+ (CloseHttp2Connections ok) | 1.22+ |
| pin-*-tailscale | tailscale.com v1.98.3 | v0.1.2 (no CloseHttp2Connections) | 1.26.3 |

For pin-*-tailscale: if dns/doh.go gains CloseHttp2Connections, check metacubex/http version first.

## Our tsnet vs upstream tailscale outbound -- they are NOT the same feature

Our tsnet (component/tsnet/):
- Mesh overlay between mihomo instances via official tailscale.com
- Nodes form a tailnet; SOCKS5 mesh for transparent group failover
- No build tags required

Upstream tailscale (adapter/outbound/tailscale.go):
- Single outbound proxy type using github.com/metacubex/tailscale fork
- Traffic exits through a tailscale node
- Requires with_gvisor build tag

They can coexist but we intentionally exclude the upstream one.

## go.sum conflict resolution

When go.sum has conflict markers:
1. Take UNION of both sides (all lines from both HEAD and THEIRS)
2. Sort the block
3. Run go mod tidy to prune stale entries

## Workflow

See full steps below. The .windsurf/workflows/sync-pin-branches.md file is for Windsurf IDE only; steps are duplicated here for use with any AI tool (Claude Code, Codex, etc).

## Branch history

| Release | persistent-pin branch | tsnet branch | Notes |
|---------|----------------------|--------------|-------|
| v1.19.24 | persistent-pin-option-1.19.24merge | pin-1.19.24merge-tailscale | baseline |
| v1.19.26 | persistent-pin-option-1.19.26merge | pin-1.19.26merge-tailscale | tailscale.com v1.98.3, Go 1.26.3 |

## Sync steps (tool-agnostic)

Repeat for each new upstream release. Replace OLD_VER/NEW_VER (e.g. 1.19.26/1.19.28).

### Phase 1: persistent-pin branch

1. git fetch origin --tags
2. git checkout persistent-pin-option-OLD_VERmerge
3. git checkout -b persistent-pin-option-NEW_VERmerge
4. GIT_EDITOR=true git merge vNEW_VER
5. Delete upstream tailscale files if added: adapter/outbound/tailscale.go, adapter/outbound/tailscale_stub.go, constant/features/no_tailscale.go, constant/features/no_tailscale_stub.go, dns/tailscale.go
6. Resolve go.mod/go.sum: take upstream (git checkout --theirs), run go mod tidy
7. Resolve build.yml: take HEAD (git checkout --ours)
8. Run stale grep (see below) -- fix all hits
9. go build ./... && go vet ./... -- both must be zero
10. git add -A && git commit -m "merge: sync vNEW_VER, skip type:tailscale outbound"
11. git push fork persistent-pin-option-NEW_VERmerge

### Phase 2: tsnet branch rebase

1. git checkout pin-OLD_VERmerge-tailscale
2. git checkout -b pin-NEW_VERmerge-tailscale
3. GIT_EDITOR=true git rebase persistent-pin-option-NEW_VERmerge
4. For each conflict round:
   - adapter/outbound/socks5.go: manual merge -- usedTsnet retry block BEFORE defer, upstream explicit return INSIDE tls block
   - go.mod/go.sum: git checkout --theirs, then go mod tidy
   - build.yml: git checkout --ours
   - adapter/outboundgroup/fallback.go: git checkout --ours
   - adapter/outboundgroup/urltest.go: git checkout --ours
   - git add <files> && GIT_EDITOR=true git rebase --continue
5. go build ./... && go vet ./... -- both must be zero
6. git push fork pin-NEW_VERmerge-tailscale --force-with-lease

### Stale reference grep (run after every merge/rebase, before committing)

grep -rn "NoTailscale\|newTailscaleClient\|TailscaleOption\|NewTailscale" --include="*.go" .

Expected: zero hits. Fix sites: constant/features/tags.go, dns/util.go, adapter/parser.go, config/config.go

## Known pitfalls

1. NEVER use regex scripts (e.g. Python re.sub) to mass-resolve git conflicts.
   Regex with DOTALL greedily spans multiple conflict blocks and corrupts function bodies.
   Use: git checkout --ours <file> or git checkout --theirs <file> per file.

2. GIT_EDITOR=true is required before git rebase --continue to prevent editor hangs in AI sessions.

3. dns/doh.go CloseHttp2Connections: only exists in metacubex/http fork v0.1.6+.
   pin-*-tailscale uses v0.1.2 (constrained by tailscale.com deps) -- remove the call there.

4. transport/sudoku/obfs/httpmask/tunnel.go: go vet lostcancel fires when cancel() is not called
   on all return paths inside the poll loop. Add cancel() before each early return.

5. After go mod tidy, new deps may appear (rasky/go-lzo, metacubex/ssh, etc). This is normal.

## Known upstream bugs (do not fix in our branches, report upstream)

These were identified during review of persistent-pin-option-1.19.26merge.
They are NOT introduced by our features. Do not patch in our branches to avoid merge conflicts.

### [P1] component/process/process_linux.go:145 -- wrong socket fallback
When netlink returns multiple messages and none match src port+IP,
the loop unconditionally sets uid/inode/err=nil before filtering,
so the last unrelated socket becomes the fallback instead of ErrNotFound.
Impact: process-based routing rules may match the wrong process.
Status: upstream intentional design ("allow fallback"), debatable correctness.

### [P2] adapter/outbound/openvpn.go:91 -- UDP option ignored
BaseOption is hardcoded UDP:true regardless of option.UDP (line 59).
Users setting udp: false in OpenVPN config have no effect.
Impact: OpenVPN outbound always advertises UDP support.
Status: upstream bug introduced with OpenVPN feature.

Both issues existed before v1.19.26. Neither is in our feature files.

### [P2-lesson] socks5.go defer position on every rebase
Root cause: our tsnet retry commit placed defer AFTER the tls block.
Upstream keeps defer BEFORE the tls block.
On every rebase, when resolving socks5.go conflict, verify:
  1. defer safeConnClose must come BEFORE the if ss.tls block
  2. The usedTsnet retry path calls c.Close() explicitly before returning -- this is correct
  3. The non-tsnet TLS failure path (err != nil, !usedTsnet) must be covered by the defer
Correct structure:
  defer func(c *net.Conn) { safeConnClose(*c, err) }(&c)  // <-- BEFORE tls block
  if ss.tls {
    cc := tls.Client(c, ss.tlsConfig)
    err = cc.HandshakeContext(ctx)
    if err != nil && usedTsnet { _ = c.Close(); return retry }
    if err != nil { return nil, fmt.Errorf(...) }  // covered by defer above
    c = cc
  }

### [P3-lesson] README and doc links must use repo-relative paths
Never use absolute /Users/... paths in markdown files.
Use relative paths: docs/foo.md or [text](docs/foo.md)
Check before every commit: grep -rn "/Users/" --include="*.md" .

### [build.yml] legacy Go matrix must be removed on tsnet branch
go.mod go 1.26.3 + GOTOOLCHAIN=local blocks older toolchains even when goversion is explicitly set.
Confirmed by: GOTOOLCHAIN=local go1.25.10/bin/go list -> "go.mod requires go >= 1.26.3"
On pin-*-tailscale branches: delete ALL matrix entries with goversion 1.22/1.23/1.24/1.25.
Entries to delete: windows go1.22-go1.25, darwin go1.22/go1.24, linux go1.23.
Suspended: loong64-abi1 -- MetaCubeX/loongarch64-golang only has go1.26.0, blocked by go.mod go 1.26.3. Re-enable when fork catches up to 1.26.3+.
Note: persistent-pin-option branches have lower go requirement; this only applies to tsnet branches.

### [go.mod dep mismatch] metacubex/tls + metacubex/http must match persistent-pin branch
After rebase, tailscale.com deps may pin metacubex/tls and metacubex/http to older versions.
This breaks listener/inbound tests (TestInboundTrustTunnel_H2) with EOF + authorization failed.
Fix: after rebase, run:
  go get github.com/metacubex/tls@vX.X.X github.com/metacubex/http@vX.X.X
Use same versions as persistent-pin-option branch (check with git show persistent-pin-option-...:go.mod).
Then: go build ./... && go test ./listener/inbound/... -run TestInboundTrustTunnel_H2 locally.
