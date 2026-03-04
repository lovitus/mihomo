<h1 align="center">
  <img src="Meta.png" alt="Meta Kennel" width="200">
  <br>Meta Kernel<br>
</h1>

<h3 align="center">Another Mihomo Kernel.</h3>

<p align="center">
  <a href="https://goreportcard.com/report/github.com/MetaCubeX/mihomo">
    <img src="https://goreportcard.com/badge/github.com/MetaCubeX/mihomo?style=flat-square">
  </a>
  <img src="https://img.shields.io/github/go-mod/go-version/MetaCubeX/mihomo/Alpha?style=flat-square">
  <a href="https://github.com/MetaCubeX/mihomo/releases">
    <img src="https://img.shields.io/github/release/MetaCubeX/mihomo/all.svg?style=flat-square">
  </a>
  <a href="https://github.com/MetaCubeX/mihomo">
    <img src="https://img.shields.io/badge/release-Meta-00b4f0?style=flat-square">
  </a>
</p>

## Fork Notes (lovitus)

This fork exists to improve proxy-group selection stability for large deployments (many groups and many nodes), especially when groups contain both healthy and timeout nodes.

### Why this fork exists

- Upstream behavior can temporarily route to timeout nodes even when healthy nodes are still present.
- Group delay output and group selection criteria may diverge, making troubleshooting confusing.

### What changed in this fork

- `adapter/outboundgroup/urltest.go`
  - `url-test` now prefers alive nodes only when at least one alive node exists.
  - Prevents falling back to timeout nodes while healthy choices are available.
  - After group URL tests complete, resets and refreshes the cached fast node decision immediately.
  - Added lock-protected state access for `selected`/`fastNode` to avoid concurrent read-write races under API set/force-set plus runtime auto-selection.
- `adapter/outboundgroup/fallback.go`
  - Reworked selected-node handling:
    - check the selected node first;
    - if selected is unavailable, clear it and then iterate all nodes in configured order, selecting the first alive node (serial order, not concurrent scan).
  - Reduces accidental fallback to an unhealthy first entry.
  - Added lock-protected selected-node state to avoid race conditions when manual override and automatic probing happen concurrently.
- `adapter/outboundgroup/groupbase.go`
  - Group delay map now records only nodes that are still alive for the tested URL.
  - Better aligns API delay output with actual group selection decisions.
- `transport/sudoku/obfs/httpmask/tunnel.go`
  - Fixed missing `cancel()` calls in poll pull loop early-return branches, avoiding context/timer leaks during long-running retry/failure conditions.
- `docs/UPSTREAM_MERGE.md`
  - Added a dedicated upstream sync and conflict-resolution playbook for maintaining this fork over time.

### Local validation done

- `go test ./...`
- `go vet ./...`
- `go test -race ./adapter/outboundgroup ./hub/route ./transport/sudoku/obfs/httpmask`

### Long-term maintenance

- Follow [`docs/UPSTREAM_MERGE.md`](docs/UPSTREAM_MERGE.md) for:
  - syncing `upstream/Alpha`,
  - replaying this fork policy after conflicts,
  - rebuilding and publishing releases.
- Performance tuning backlog is tracked in [`docs/PERFORMANCE_REPORT.md`](docs/PERFORMANCE_REPORT.md).

## Features

- Local HTTP/HTTPS/SOCKS server with authentication support
- VMess, VLESS, Shadowsocks, Trojan, Snell, TUIC, Hysteria protocol support
- Built-in DNS server that aims to minimize DNS pollution attack impact, supports DoH/DoT upstream and fake IP.
- Rules based off domains, GEOIP, IPCIDR or Process to forward packets to different nodes
- Remote groups allow users to implement powerful rules. Supports automatic fallback, load balancing or auto select node
  based off latency
- Remote providers, allowing users to get node lists remotely instead of hard-coding in config
- Netfilter TCP redirecting. Deploy Mihomo on your Internet gateway with `iptables`.
- Comprehensive HTTP RESTful API controller

## Dashboard

A web dashboard with first-class support for this project has been created; it can be checked out at [metacubexd](https://github.com/MetaCubeX/metacubexd).

## Configration example

Configuration example is located at [/docs/config.yaml](https://github.com/MetaCubeX/mihomo/blob/Alpha/docs/config.yaml).

## Docs

Documentation can be found in [mihomo Docs](https://wiki.metacubex.one/).

## For development

Requirements:
[Go 1.20 or newer](https://go.dev/dl/)

Build mihomo:

```shell
git clone https://github.com/MetaCubeX/mihomo.git
cd mihomo && go mod download
go build
```

Set go proxy if a connection to GitHub is not possible:

```shell
go env -w GOPROXY=https://goproxy.io,direct
```

Build with gvisor tun stack:

```shell
go build -tags with_gvisor
```

### IPTABLES configuration

Work on Linux OS which supported `iptables`

```yaml
# Enable the TPROXY listener
tproxy-port: 9898

iptables:
  enable: true # default is false
  inbound-interface: eth0 # detect the inbound interface, default is 'lo'
```

## Debugging

Check [wiki](https://wiki.metacubex.one/api/#debug) to get an instruction on using debug
API.

## Credits

- [Dreamacro/clash](https://github.com/Dreamacro/clash)
- [SagerNet/sing-box](https://github.com/SagerNet/sing-box)
- [riobard/go-shadowsocks2](https://github.com/riobard/go-shadowsocks2)
- [v2ray/v2ray-core](https://github.com/v2ray/v2ray-core)
- [WireGuard/wireguard-go](https://github.com/WireGuard/wireguard-go)
- [yaling888/clash-plus-pro](https://github.com/yaling888/clash)

## License

This software is released under the GPL-3.0 license.

**In addition, any downstream projects not affiliated with `MetaCubeX` shall not contain the word `mihomo` in their names.**
