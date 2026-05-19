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

Fork-specific features are documented in this repository. For the built-in
tsnet/Tailscale node, including `tailscale.login-server-ip-fallbacks` and
`-tailscale-wizard`, see
[Built-in tsnet / Tailscale Integration](/Users/fanli/Documents/mihomo-rev/docs/tailscale-tsnet.md).

## Fork Notes

This fork tracks upstream `MetaCubeX/mihomo` while preserving the fork-specific persistent pin behavior and related release workflow behavior.

Reference documents in this repository:

- [Persistent Pin Upstream Merge Playbook (Human)](/Users/fanli/Documents/mihomo-rev/docs/persistent-pin-upstream-merge-human.md)
- [Persistent Pin Upstream Merge Playbook (AI)](/Users/fanli/Documents/mihomo-rev/docs/persistent-pin-upstream-merge-ai.md)
- [Persistent Pin Upstream Merge Checklist](/Users/fanli/Documents/mihomo-rev/docs/persistent-pin-upstream-merge-checklist.md)
- [Persistent Pin Upstream Merge Report: v1.19.24](/Users/fanli/Documents/mihomo-rev/docs/persistent-pin-upstream-merge-report-v1.19.24.md)
- [Fork Release Notes: v2026.04.20-persistent-pin.5](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.04.20-persistent-pin.5.md)
- [Built-in tsnet / Tailscale Integration](/Users/fanli/Documents/mihomo-rev/docs/tailscale-tsnet.md)
- [Fork Release Notes: v2026.04.22-persistent-pin.6-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.04.22-persistent-pin.6-tsnet.md)
- [Fork Release Notes: v2026.04.28-persistent-pin.18-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.04.28-persistent-pin.18-tsnet.md)
- [Fork Release Notes: v2026.04.28-persistent-pin.19-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.04.28-persistent-pin.19-tsnet.md)
- [Fork Release Notes: v2026.04.28-persistent-pin.20-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.04.28-persistent-pin.20-tsnet.md)
- [Fork Release Notes: v2026.04.28-persistent-pin.21-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.04.28-persistent-pin.21-tsnet.md)
- [Fork Release Notes: v2026.04.30-persistent-pin.22-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.04.30-persistent-pin.22-tsnet.md)
- [Fork Release Notes: v2026.05.15-persistent-pin.24-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.05.15-persistent-pin.24-tsnet.md)
- [Fork Release Notes: v2026.05.18-persistent-pin.25-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.05.18-persistent-pin.25-tsnet.md)
- [Fork Release Notes: v2026.05.19-persistent-pin.26-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.05.19-persistent-pin.26-tsnet.md)
- [Fork Release Notes: v2026.05.20-persistent-pin.27-tsnet](/Users/fanli/Documents/mihomo-rev/docs/releases/v2026.05.20-persistent-pin.27-tsnet.md)

## For development

Requirements:
[Go 1.22 or newer](https://go.dev/dl/)

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

Run tests:

```shell
go test ./...
make test-inbound
```

The `listener/inbound` package includes protocol matrix tests and local loopback traffic tests. The default inbound run keeps concurrent traffic small enough for normal development while still covering concurrent request handling. Heavier resource-exhaustion coverage is opt-in:

```shell
make test-inbound-stress
INBOUND_CONCURRENT_REQUESTS=64 go test ./listener/inbound -count=1 -timeout=180s
```

Use `make test-inbound-no-concurrent` or set `SKIP_CONCURRENT_TEST=1` when debugging unrelated failures on resource-constrained machines.

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
