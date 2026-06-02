---
description: sync pin branches to new upstream release
---

## Overview

Branch naming convention:
- `persistent-pin-option-{VER}merge` — pin feature only, no tsnet, no upstream tailscale
- `pin-{VER}merge-tailscale` — pin feature + tsnet mesh, rebased onto the above

Where `{VER}` is e.g. `1.19.26`.

## Step 1: Fetch upstream tags

```
git fetch origin --tags
```

## Step 2: Create new persistent-pin branch

Replace `NEW_VER` and `OLD_VER` with actual versions, e.g. `1.19.28` and `1.19.26`.

```
git checkout persistent-pin-option-OLD_VERmerge
git checkout -b persistent-pin-option-NEW_VERmerge
GIT_EDITOR=true git merge vNEW_VER
```


## Step 3: Resolve merge conflicts

Always take HEAD for workflow files. Always take THEIRS for go.mod/go.sum.

### Files to delete after every merge (upstream tailscale artifacts):
- adapter/outbound/tailscale.go
- adapter/outbound/tailscale_stub.go
- constant/features/no_tailscale.go
- constant/features/no_tailscale_stub.go
- dns/tailscale.go

### Stale reference check (run before every commit):
```
grep -rn "NoTailscale\|newTailscaleClient\|TailscaleOption\|NewTailscale" --include="*.go" .
```
Expected: zero hits.

Files to clean if hits found:
- constant/features/tags.go: remove the NoTailscale block
- dns/util.go: remove case "tailscale"
- adapter/parser.go: remove case "tailscale"
- config/config.go: remove case "tailscale" DNS entry

### Absolute path check in markdown (run before every commit):
```
grep -rn "/Users/" --include="*.md" .
```
Expected: zero hits. Use repo-relative paths only.

### Dependency version check (run after rebase of tsnet branch):
After rebase, tailscale.com may pin metacubex/tls and metacubex/http to older versions.
Check and upgrade to match persistent-pin-option branch:
```
git show persistent-pin-option-NEW_VERmerge:go.mod | grep "metacubex/tls\|metacubex/http\|metacubex/quic-go"
go get github.com/metacubex/tls@<version> github.com/metacubex/http@<version> github.com/metacubex/quic-go@<version>
go build ./... && go test ./listener/inbound/... -run TestInboundTrustTunnel_H2
```

### socks5.go conflict reminder:
After resolving adapter/outbound/socks5.go, verify defer is BEFORE the tls block:
  defer func(c *net.Conn) { safeConnClose(*c, err) }(&c)   // must be here
  if ss.tls { ... usedTsnet retry ... }                     // tls block after defer

## Step 4: Verify

```
go build ./...
go vet ./...
```

Both must exit 0. Fix errors before committing.

## Step 5: Commit and push persistent-pin branch

```
git add -A
git commit -m "merge: sync vNEW_VER, skip type:tailscale outbound"
git push fork persistent-pin-option-NEW_VERmerge
```

## Step 6: Rebase tsnet branch onto new persistent-pin branch

```
git checkout pin-OLD_VERmerge-tailscale
git checkout -b pin-NEW_VERmerge-tailscale
GIT_EDITOR=true git rebase persistent-pin-option-NEW_VERmerge
```

## Step 7: Resolve rebase conflicts

Conflict strategy per file:
- adapter/outbound/socks5.go: MERGE both sides
  HEAD = upstream new error handling
  THEIRS = usedTsnet retry logic
  Result: keep usedTsnet retry block BEFORE defer, then upstreams explicit return
- go.mod/go.sum: run git checkout --theirs, then go mod tidy
- .github/workflows/build.yml: git checkout --ours (HEAD is more complete)
- adapter/outboundgroup/fallback.go: git checkout --ours
- adapter/outboundgroup/urltest.go: git checkout --ours

NEVER use regex scripts to resolve conflicts — use git checkout --ours/--theirs per file.

Continue after each conflict:
```
git add <resolved_files>
GIT_EDITOR=true git rebase --continue
```

## Step 8: Final verify and push

```
go build ./...
go vet ./...
git push fork pin-NEW_VERmerge-tailscale --force-with-lease
```

## Known pitfalls (from 1.19.26 experience)

1. NEVER use regex scripts to mass-resolve conflicts. Use git checkout --ours/--theirs per file.
2. After every merge, grep for all 4 stale tailscale symbols (Step 3).
3. dns/doh.go may call CloseHttp2Connections() -- only valid with metacubex/http fork.
   pin-*-tailscale uses metacubex/http v0.1.x without this method; remove the call.
4. transport/sudoku/obfs/httpmask/tunnel.go: go vet lostcancel -- call cancel() on all return paths.
5. Build tags: upstream tailscale outbound needs with_gvisor. Our tsnet needs none.
6. go.sum conflicts: take UNION of both sides (python: set(HEAD) | set(THEIRS)), then go mod tidy.
7. GIT_EDITOR=true required for all git rebase --continue to prevent editor hangs.

## Branch state after each release

| Release | persistent-pin branch | tsnet branch |
|---------|----------------------|--------------|
| v1.19.24 | persistent-pin-option-1.19.24merge | pin-1.19.24merge-tailscale |
| v1.19.26 | persistent-pin-option-1.19.26merge | pin-1.19.26merge-tailscale |
| next    | persistent-pin-option-X.XX.XXmerge | pin-X.XX.XXmerge-tailscale |
