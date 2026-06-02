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

Use the Windsurf workflow: /sync-pin-branches

## Branch history

| Release | persistent-pin branch | tsnet branch | Notes |
|---------|----------------------|--------------|-------|
| v1.19.24 | persistent-pin-option-1.19.24merge | pin-1.19.24merge-tailscale | baseline |
| v1.19.26 | persistent-pin-option-1.19.26merge | pin-1.19.26merge-tailscale | tailscale.com v1.98.3, Go 1.26.3 |
