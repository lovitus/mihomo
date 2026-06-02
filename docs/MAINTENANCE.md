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
