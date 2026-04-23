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

- Mesh TCP/UDP listener retry is on-use, not timer-driven.
- Retry delay uses exponential backoff from `10s`.
- This avoids mobile or low-power devices waking periodically just to retry listeners.

## Files And Packages

New package:

- `component/tsnet`

New shared listener helper package:

- `listener/sockscommon`

Main modified areas:

- config parsing and docs
- hub apply lifecycle
- route handler reuse
- SOCKS5 outbound dial path
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

- Push a `v*` tag to trigger `Build`.
- `Upload-Tag-Release` downloads all build artifacts.
- Checksums are generated.
- Assets are uploaded to the GitHub Release for that tag.

Recommended release tag for this feature branch:

```text
v2026.04.22-persistent-pin.6-tsnet
```
