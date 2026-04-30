# Built-in tsnet / Tailscale Integration

## Summary

This fork branch adds a minimal built-in `tsnet` integration to `mihomo`.

The implementation embeds a Tailscale client node inside `mihomo`, keeps its own persistent local identity, can expose the existing controller over the tailnet, and can expose a standard SOCKS5 service on the tailnet.

It intentionally does not implement a system VPN, does not manage the host `tailscaled`, does not require TUN/TAP, does not add a new proxy type, and does not add magic proxy names.

## Branch And Base

- feature branch: `pin-1.19.24merge-tailscale`
- branch source: `persistent-pin-option-1.19.24merge`
- source branch HEAD before this feature: `78defbdb docs(release): mark v2026.04.20-persistent-pin.4 superseded`
- previous valid fork release on this upstream line: `v2026.04.20-persistent-pin.5`
- previous valid fork release commit: `313230d95da4f28f9c23327709d7c91dcfc48919`
- upstream base: `MetaCubeX/mihomo v1.19.24`
- upstream base commit: `a84724665eb7f989809abe463c05f5723bd24975`

This branch keeps the `persistent-pin` fork behavior from `persistent-pin-option-1.19.24merge` and adds the new built-in `tsnet` feature on top.

## Public Configuration

Minimum config:

```yaml
tailscale:
  enable: true
  login-server: https://hs.example.com
  state-dir: tailscale
  expose-controller: true
  mesh: true
  socks5: 1666
```

Defaults:

```yaml
tailscale:
  enable: false
  login-server: ""
  state-dir: tailscale
  expose-controller: false
  mesh: false
  socks5: 1666
```

Field meanings:

- `enable`: starts or stops the built-in `tsnet` subsystem.
- `login-server`: Headscale or Tailscale control server URL. Required when `enable=true`.
- `state-dir`: persistent `tsnet` state directory. This is the node identity root.
- `expose-controller`: exposes the existing ordinary `external-controller` on the tailnet.
- `mesh`: exposes a standard SOCKS5 server on the tailnet.
- `socks5`: tailnet-side SOCKS5 port. It is not a host `127.0.0.1` or `0.0.0.0` port.

Important state-dir rules:

- Deleting `state-dir` deletes the embedded tailnet node identity.
- Copying `state-dir` copies the same embedded tailnet node identity.
- The same `state-dir` must not be shared by multiple running `mihomo` instances.
- `state-dir` is sensitive identity material and must follow mihomo safe-path rules.

## Standard Proxy Usage

The new feature does not add a new proxy type. Use standard `socks5` nodes.

Recommended direct tailnet IP usage:

```yaml
proxies:
  - name: tail-socks-home
    type: socks5
    server: 10.20.0.33
    port: 1666
    udp: true
```

Best-effort MagicDNS or Headscale DNS usage:

```yaml
proxies:
  - name: tail-socks-name
    type: socks5
    server: peer-node33.example.internal
    port: 1666
    udp: true
```

Use a tailnet SOCKS5 node as a normal `dialer-proxy`:

```yaml
proxies:
  - name: tail-socks-home
    type: socks5
    server: 10.20.0.33
    port: 1666
    udp: true

  - name: ss-through-home-tail
    type: ss
    server: example.com
    port: 443
    cipher: chacha20-ietf-poly1305
    password: "password"
    udp: true
    dialer-proxy: tail-socks-home
```

Compatibility behavior:

- Old cores that can parse the YAML still see standard `socks5` nodes.
- Old cores do not understand the `tailscale` block, depending on their parser behavior.
- There is no magic `name`, no magic `server`, and no new proxy schema.
- If the old core loads the config, the tailnet SOCKS5 nodes simply cannot connect unless the environment already routes those addresses.

## Runtime Flow

Startup:

1. Config parser reads `tailscale`.
2. If `enable=false`, the subsystem stays disabled.
3. If enabled, parser validates `login-server`, `state-dir`, `socks5`, and safe path.
4. `hub.ApplyConfig` applies routes and executor config first.
5. `component/tsnet.ApplyConfig` starts the embedded tsnet runtime.
6. The runtime resolves and locks `state-dir`.
7. The runtime creates a stable node name of the form `mihomo-<short-id>`.
8. The runtime starts `tsnet.Server`.
9. It waits up to 60 seconds for Tailscale IP assignment.
10. If connected, it optionally starts tailnet SOCKS5 and controller listeners.
11. If the node is not connected within 60 seconds, `tsnet` is disabled for this run and the main mihomo process continues.

Reload:

- `ApplyConfig` is replace-all.
- Old worker is stopped.
- Old TCP listeners are closed.
- Old UDP packet conns are closed.
- Old controller listener is closed.
- Old `state-dir` lock is released.
- New runtime starts from the new config.

Shutdown:

- `main` calls `tsnet.Stop()`.
- The runtime closes listeners, packet conns, tsnet server, and the state-dir lock.
- A reload or restart retries `tsnet` startup after a previous 60-second startup grace timeout.

## Registration And Authorization

This feature does not use YAML `preauthkey`.

Expected first-run flow:

1. Start with an empty `state-dir`.
2. `tsnet` generates local identity material.
3. `tsnet` contacts `login-server`.
4. Headscale/Tailscale returns a native authorization state or auth URL.
5. `mihomo` logs the pending state and auth URL if available.
6. User approves the node using the native Headscale/Tailscale workflow.
7. Runtime enters `connected` after the control server accepts the node.

`login-server` hostnames are resolved by Tailscale's internal control-plane paths on a best-effort basis. If a hostname cannot be resolved or the control server cannot complete authorization within the 60-second startup grace period, `tsnet` is disabled for the current run without changing the normal mihomo DNS behavior.

Important state mapping:

- `unregistered`: local state exists but login or authorization is not complete.
- `connected`: node has a tailnet IP and can use `tsnet` dial/listen.
- `needs-reauth`: local state exists but the control server requires machine/node authorization again.
- `register-failed`: startup, local client, backend, or status watch failed.
- `disabled`: config is off or runtime initialization could not safely start.

## Registration Wizard

For devices where normal logs are hard to inspect, such as OpenWrt services, mihomo provides a small one-shot registration helper:

```bash
mihomo -d /path/to/home -f /path/to/config.yaml -tailscale-wizard
```

Behavior:

- Reads only `tailscale.login-server` and `tailscale.state-dir` from the YAML config.
- Does not load rules, providers, geosite, geoip, proxies, listeners, or the full mihomo runtime.
- Resolves relative `state-dir` against `-d`; if `-d` is omitted, it uses the normal mihomo home directory.
- Defaults missing `tailscale.state-dir` to `tailscale`, matching normal config defaults.
- Uses the same state-dir lock and stable node name as normal tsnet startup.
- Prints state-file diagnostics before startup, including missing state or very small state files.
- Starts a temporary `tsnet.Server`, prints the authorization URL when the control server provides one, and waits up to `2m` for the node to connect.
- Exits `0` after the node is connected and a tail IP is visible.
- Exits `1` for config errors, state-dir lock conflicts, startup errors, status watch errors, or timeout.

Operational notes:

- Stop the normal mihomo service before running the wizard against the same `state-dir`; the lock intentionally rejects concurrent use.
- After successful authorization, start mihomo normally with the same `state-dir`.
- The wizard is diagnostic and registration-only. It does not expose mesh SOCKS, gateway SOCKS, controller routes, proxy listeners, or Web UI.
- The wizard still disables Tailscale logtail uploads and uses the same default-resolver lifecycle guard as the normal embedded tsnet runtime.

## Tailnet SOCKS5 Service

When enabled:

```yaml
tailscale:
  mesh: true
  socks5: 1666
```

Behavior:

- TCP listener is created with `tsnet.Server.Listen("tcp", ":1666")`.
- UDP listener is created per local tailnet IP with `tsnet.Server.ListenPacket("udp", "<tail-ip>:1666")`.
- The listener is visible on the tailnet, not on the host LAN or loopback.
- Host `127.0.0.1:1666` and `0.0.0.0:1666` are not occupied by this feature.
- SOCKS authentication reuses the existing mihomo SOCKS auth store.
- The normal host `allow-lan` remote-address rule is not applied to tailnet mesh SOCKS.

Safety hardening:

- Idle SOCKS handshake timeout: `10s`.
- Tailnet SOCKS active TCP/control connection cap: `1024`.
- Limit warning interval: `30s`.
- The cap covers TCP CONNECT and UDP ASSOCIATE control connections.
- If the cap is reached, new tailnet SOCKS connections are closed immediately.
- UDP data flow is not given a second session manager; it uses existing tunnel/NAT behavior.

## Gateway SOCKS5

`tailscale.gateway-socks5` is a host-facing SOCKS5 gateway for local applications that need to reach services inside the tailnet through the embedded `tsnet` node.

Example:

```yaml
tailscale:
  enable: true
  login-server: https://headscale.example.com
  state-dir: tailscale
  socks5: 1666
  gateway-socks5: 127.0.0.1:1667
```

Concept split:

- `tailscale.socks5` is tailnet-facing mesh/node SOCKS5. Other tailnet nodes connect to this mihomo node through its tailnet IP.
- `tailscale.gateway-socks5` is host-facing tailnet access SOCKS5. Local apps connect to it to reach tailnet services such as SSH, RDP, SMB, DNS, or HTTP.
- `gateway-socks5` does not enter mihomo rule/group/DNS selection. If rule-based selection is needed, configure `127.0.0.1:1667` as a normal `socks5` proxy node.

Address rules:

- Empty or missing value disables the gateway.
- A port-only value such as `1667` is normalized to `127.0.0.1:1667`.
- `127.0.0.1:1667`, `[::1]:1667`, `:1667`, and `0.0.0.0:1667` are valid.
- Port `0`, `:0`, invalid ports, and invalid addresses are rejected.
- `:1667`, `0.0.0.0:1667`, and `[::]:1667` are allowed but log a warning because they expose tailnet access to the host network.
- Listening uses Go's standard single-address semantics; mihomo does not split wildcard listeners into separate IPv4 and IPv6 sockets.

TCP behavior:

- SOCKS5 CONNECT is dialed with `tsnet.Server.Dial(ctx, "tcp", target)`.
- The gateway does not use the global mihomo tunnel, rules, groups, or DNS resolver.
- TCP connect timeout uses the existing `C.DefaultTCPTimeout`.
- Data relay uses the existing connection relay helper.
- Dial failures close the current connection and do not fall back to another path.
- No gateway-specific TCP connection cap is added; behavior matches ordinary host SOCKS listeners.

UDP behavior:

- SOCKS5 UDP ASSOCIATE is supported when the gateway UDP bind succeeds.
- If the gateway UDP bind fails, UDP ASSOCIATE fails explicitly and does not return a fake bind address.
- For wildcard gateway listeners such as `:1667`, `0.0.0.0:1667`, or `[::]:1667`, the UDP ASSOCIATE reply uses the concrete local IP of the accepted TCP control connection.
- If a wildcard gateway listener cannot derive a concrete local IP from the accepted TCP control connection, UDP ASSOCIATE fails instead of returning `0.0.0.0` or `[::]`.
- SOCKS5 UDP packet decode, encode, write-back, and buffer drop behavior reuse `listener/sockscommon`.
- UDP does not enter the global mihomo UDP NAT/rule/group/DNS pipeline.
- UDP session key is `clientAddr.String() + "|" + resolvedTargetAddrPort.String()`.
- `resolvedTargetAddrPort` is a normalized single `netip.AddrPort`; IPv4-mapped IPv6 addresses are unmapped and zone identifiers are unsupported.
- Each session owns one `tsnet.Server.ListenPacket("udp", "<local-tail-ip>:0")` PacketConn.
- Idle timeout uses the existing `C.DefaultUDPTimeout`.
- UDP sessions close on idle timeout, write/read error, write-back error, reload, or shutdown.
- A minimal hard cap of `4096` gateway UDP sessions prevents unbounded PacketConn/goroutine growth. New sessions over the cap are dropped with rate-limited warning logs; existing sessions continue.

UDP name resolution:

- TCP names are passed to tsnet directly.
- UDP names are resolved only from the tsnet identity cache built from `LocalClient().Status(ctx)`.
- DNSName/FQDN matching is lower-case and ignores a trailing dot.
- Short HostName matching is accepted only when unique in the visible self/peer set.
- Unknown names, conflicting hostnames, and refresh failures do not use the system resolver or mihomo DNS; the UDP packet is dropped with rate-limited logs.
- For peers with both IPv4 and IPv6 tail IPs, gateway UDP prefers the client UDP address family, then IPv4, then IPv6.

Self-loop protection:

- Gateway TCP/UDP blocks targets that resolve to this node's own tail identity and port `tailscale.socks5` or `tailscale.gateway-socks5`.
- Other self ports are allowed so local tailnet services remain reachable.

Usage as a mihomo proxy node:

```yaml
proxies:
  - name: tailnet-gateway
    type: socks5
    server: 127.0.0.1
    port: 1667
    udp: true

rules:
  - IP-CIDR,100.64.0.0/10,tailnet-gateway,no-resolve
```

Manual check tool:

```bash
go run ./cmd/tsnet-gateway-check -gateway 127.0.0.1:1667 -tcp 100.64.0.10:22
go run ./cmd/tsnet-gateway-check -gateway 127.0.0.1:1667 -udp 100.64.0.10:53 -udp-payload hex:0000010000010000000000000377777706676f6f676c6503636f6d0000010001
```

## SOCKS5 Outbound Reuse

The only outbound behavior change is inside the standard SOCKS5 adapter when connecting to the SOCKS5 server itself.

Rule:

- If `tsnet` is ready and the SOCKS5 node port equals `tailscale.socks5`, try `tsnet` dial first.
- If `tsnet` dial succeeds, use it.
- If `tsnet` dial fails, fall back to the original dialer.
- If `tsnet` is not ready, use the original dialer directly.

No extra matching rules:

- No CIDR check.
- No `100.64.0.0/10` assumption.
- No `.ts.net` suffix assumption.
- No peer-list scan.
- No global DNS interception.
- No provider/group refresh.
- No forced health check.

UDP outbound behavior:

- SOCKS5 UDP ASSOCIATE first uses the same TCP control dial selection.
- If the `tsnet` TCP control path or `tsnet` PacketConn path fails, the whole UDP ASSOCIATE is retried once using the original dialer.
- If the original dialer also fails, the error is returned.
- `tsnet` UDP PacketConn binds `<local-tail-ip>:0` matching the remote UDP relay address family.
- When a SOCKS5 server returns an unspecified UDP bind address, the implementation intentionally does not add tailnet identity lookup or MagicDNS relay-address resolution. Tailnet addresses are private and this path is best-effort; if the tsnet UDP attempt misses, the existing default-dialer retry handles recovery.

## Controller Exposure

When enabled:

```yaml
tailscale:
  expose-controller: true
```

Behavior:

- Only ordinary `external-controller` is supported.
- `external-controller-tls`, `external-controller-unix`, and `external-controller-pipe` are not exposed by this feature.
- It reuses the existing controller handler.
- It reuses existing secret, CORS, debug routes, and DOH handler settings.
- It does not create any new host listener.

If no ordinary `external-controller` is configured:

- The feature logs a warning.
- Tailnet controller exposure is skipped.
- The main mihomo process continues.

## Error Handling

Config errors that fail config load:

- `tailscale.enable=true` and `login-server` is empty.
- `tailscale.enable=true` and resolved `state-dir` is empty.
- `tailscale.socks5` outside `1..65535`.
- `state-dir` violates mihomo safe-path rules.

Runtime errors that disable only the subfeature:

- `state-dir` lock failure disables the `tsnet` subsystem.
- `tsnet.Server.Start` failure sets `register-failed` until the startup grace watchdog disables `tsnet`.
- startup grace timeout after 60 seconds disables `tsnet` for this run.
- pending authorization sets `unregistered`.
- machine authorization requirement sets `needs-reauth`.
- mesh SOCKS listener failure disables mesh SOCKS until retry.
- mesh UDP listener failure disables UDP relay until retry.
- controller expose failure disables only tailnet controller exposure.

Retry behavior:

- Backend `Running` status is allowed a short tail-IP assignment grace: status is retried up to 3 times with a `500ms` interval before startup is considered failed.
- Mesh TCP/UDP listener retry is on-use, not timer-driven.
- Mesh retry delay uses exponential backoff from `10s` and caps at `5m`.
- Gateway TCP listen failures are retried in the background with the same bounded exponential backoff. This handles transient host port conflicts without requiring a mihomo reload.
- This avoids mobile or low-power devices waking periodically just to retry listeners.

## Read-only Status API And Web Page

The controller exposes a read-only tsnet status surface:

- `GET /tailscale`
- `GET /tailscale/logs`
- `GET /tailscale/web`

Authentication:

- `/tailscale` and `/tailscale/logs` reuse the existing controller Bearer secret.
- If the controller secret is empty, the API behaves like other unprotected controller routes and returns data directly.
- `/tailscale/web` is public, but it is only a static HTML/CSS/JS shell. Runtime data is fetched from the protected API after the user enters the controller secret.
- The page stores the entered secret only in browser `sessionStorage`; there is no server-side session or cookie.

Status API behavior:

- The status endpoint combines the in-memory runtime snapshot with a best-effort `LocalClient.Status(ctx)` query.
- Handlers use a short request-derived timeout so a blocked tsnet local client does not hang the controller.
- If the live status query fails, the endpoint still returns HTTP `200` with the runtime snapshot and a `statusError` field.
- When tsnet is disabled or the runtime is absent, the endpoint returns the minimal disabled state: `enable=false`, `state=disabled`, `ready=false`.
- Logs are kept in memory only. Disabled/runtime-nil state returns an empty log list instead of `404` or `500`.
- Peer `tailscaleIPs` primarily comes from `PeerStatus.TailscaleIPs`. For Headscale deployments using non-standard IPv4 node ranges that Tailscale filters out of `PeerStatus.TailscaleIPs`, single-host `AllowedIPs` are added back when they are not advertised primary routes.

Web page behavior:

- The page is intentionally read-only. It does not restart tsnet, reauthorize the node, change config, or write logs to disk.
- It displays runtime state, self node details, service readiness, peers, recent in-memory logs, and diagnostic details.
- It includes the live Tailscale daemon/library version when available from `LocalClient.Status(ctx)`.
- Peer rows can be filtered by host, DNS name, OS, state, tail IP, route, and tag.
- Peer rows can be sorted by name, first tail IP, OS, state, relay path, and last activity.
- Peer display includes direct-vs-relay connection state, direct address details, tags, exit-node markers, near-expiry key hints, sorted tail IPs, and last refresh time.
- Self and peer IP lists are rendered one address per line for readability.
- The Routes column is hidden when the current peer list has no advertised primary routes.
- Auth URLs are available only after API authentication and can be copied from the page.
- Local diagnostic paths are kept in a diagnostic section and are not embedded in the static HTML shell.
- Responses use `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, and `Referrer-Policy: no-referrer`.

## Files And Packages

New package:

- `component/tsnet`

New shared listener helper package:

- `listener/sockscommon`

Manual verification command:

- `cmd/tsnet-gateway-check`

Main modified areas:

- config parsing and docs
- hub apply lifecycle
- route handler reuse
- SOCKS5 outbound dial path
- host-facing tsnet gateway SOCKS5
- SOCKS5 server handshake reply policy
- ordinary SOCKS listener wrapper
- build and test workflows

Direct dependency added:

- `tailscale.com v1.68.2`

Build requirement change:

- root module now requires `go 1.22.0`
- Go 1.20 and Go 1.21 legacy build/test entries are removed

## CI And Release Notes

CI expectations:

- `go test -p 1 ./...`
- `go test -p 1 ./... -tags "with_gvisor" -count=1`
- GitHub Actions build matrix on `v*` tag push
- GitHub Actions tag release upload through `Upload-Tag-Release`
- Docker job remains disabled in this fork
- CMFA downstream update skips cleanly when maintainer app secrets are not configured

Release workflow:

- This fork does not run branch-push release builds for `pin-1.19.24merge-tailscale`; use a `v*` tag or `workflow_dispatch`.
- Push a `v*` tag to trigger `Build`.
- `Upload-Tag-Release` downloads all build artifacts.
- Checksums are generated.
- Assets are uploaded to the GitHub Release for that tag.

Recommended release tag for this feature branch:

```text
v2026.04.30-persistent-pin.22-tsnet
```
