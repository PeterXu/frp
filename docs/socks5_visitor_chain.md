# Socks5 Visitor Chain

> Summary of the `socks5` visitor feature added in commits `765146d4` and `6d3f9fb0`
> (design: `doc/superpowers/specs/2026-06-13-socks5-visitor-chain-design.md`,
> plan: `doc/superpowers/plans/2026-06-13-socks5-visitor-chain.md`).

## Overview

frp's SOCKS5 entry point previously lived only on **frps** (`server/socks5proxy`).
The socks5 visitor moves that entry point onto **frpc1** (inside a trusted
network), with frps acting purely as an internal relay. It is the visitor-style
twin of the existing frps SOCKS5 listener, and reuses the same routing model
(`selectFrpcFn`) so the downstream relay is unaware of where a request
originated.

Target chain:

```
SOCKS5 client → frpc1 (socks5 visitor) → frps → frpc2 (socks5_relay) → outbound target
```

This mirrors frp's generic visitor pattern (`client → frpc1 → frps → frpc2`),
extended to carry SOCKS5 semantics with a per-connection dynamic target.

## Architecture

```
┌─────────┐ SOCKS5    ┌──────────┐ NewSocks5VisitorConn ┌──────┐ StartWorkConn ┌──────────┐ dial
│ SOCKS5  ├──────────►│ frpc1    ├─────────────────────►│ frps ├──────────────►│ frpc2    ├─────►
│ client  │           │ visitor  │ ◄────────────────────┤      │ ◄─────────────┤ socks5_  │
│         │           │ (listen) │  VisitorConnResp     │      │   work conn   │ relay    │
└─────────┘           └──────────┘                      └──────┘               └──────────┘
        (frpc1 bridges client ↔ yamux stream)                    (frpc2 bridges workconn ↔ target)
```

**Roles**

- **frps socks5-proxy** — built-in server feature (a *service* frps provides). Behavior unchanged (refactored to use the shared package, no behavior change).
- **frpc1 socks5 visitor** — new visitor type; a *consumer* of `socks5_relay`.
- **frpc2 socks5_relay** — existing proxy type, **unchanged**. Receives a normal `StartWorkConn` and dials the target.

**Why a visitor type, not a proxy type?** In frp's model, `proxies[]` expose services through frps (listener sits on frps); `visitors[]` consume services via frps (listener sits on frpc1). The new listener sits on frpc1, so it belongs under `visitors[]`, keeping it symmetric with STCP/XTCP/SUDP.

**Why reuse `selectFrpcFn`?** The frps-side SOCKS5 server already implements group/userID/targetUser routing through `Service.makeSelectFrpcFn` (`server/service.go`). The visitor path forwards an equivalent request to frps, which uses the same routing. One routing model serves both entry points; frpc2 sees an identical `StartWorkConn` regardless of origin. This is the central correctness property of the feature.

## Components

| Area | File | Change |
|---|---|---|
| Shared protocol | `pkg/util/socks5/protocol.go` | **New.** Single source of truth for the SOCKS5 wire protocol: `ParseGroupUserID`, `Handshake`, `Authenticate`, `ReadConnectRequest`, `SendReply`. |
| Shared protocol tests | `pkg/util/socks5/protocol_test.go` | **New.** Round-trip tests via `net.Pipe`. |
| New message | `pkg/msg/socks5_proxy.go` | **New.** `NewSocks5VisitorConn` + `NewSocks5VisitorConnResp`. |
| Message registry | `pkg/msg/msg.go` | Register types `'V'` and `'Y'` in `msgTypeMap`. |
| Visitor config | `pkg/config/v1/visitor.go` | **New.** `VisitorTypeSocks5 = "socks5"`, `Socks5VisitorConfig`, registered in `visitorConfigTypeMap`. |
| Config validation | `pkg/config/v1/validation/visitor.go` | `validateSocks5VisitorConfig` — requires `authPassword`. |
| Config validation | `pkg/config/v1/validation/proxy.go` | `socks5_relay` no longer requires frps to expose `socks5ProxyPort`/`httpConnectProxyPort` (enabling change — see below). |
| Visitor impl | `client/visitor/socks5.go` | **New.** `Socks5Visitor`. |
| Visitor factory | `client/visitor/visitor.go` | `*v1.Socks5VisitorConfig` case in `NewVisitor`. |
| Server handler | `server/socks5proxy/dial_handler.go` | **New.** `HandleNewSocks5VisitorConn`. |
| Server dispatch | `server/service.go` | `case *msg.NewSocks5VisitorConn` in `handleConnection`. |
| frps listener refactor | `server/socks5proxy/handler.go` | Inline protocol code replaced with `pkg/util/socks5` calls. No behavior change. |
| http_connect refactor | `server/socks5proxy/http_connect.go` | Uses shared `socks5.ParseGroupUserID`. |
| E2E | `test/e2e/v1/features/socks5_visitor.go` | **New.** 5 scenarios. |

**Unchanged:** `client/proxy/socks5_relay.go` (frpc2 dialer), `Service.makeSelectFrpcFn`.

### Enabling change: `socks5_relay` validation loosened

`validateSocks5RelayProxyConfigForServer` previously rejected a `socks5_relay`
proxy unless frps had `socks5ProxyPort` or `httpConnectProxyPort` set. That guard
is removed. Rationale: the visitor path is now an alternative entry point that
reaches `socks5_relay` without frps ever exposing a public SOCKS/HTTP-CONNECT
port. This lets `socks5_relay` be used purely as a visitor-targeted relay.

## Routing model

Routing is derived from the **SOCKS5 username** exactly as on the frps listener
(`ParseGroupUserID`):

- `group` → round-robin across frpc2 members of the group
- `group@userID` → session affinity (sticky to one frpc2)
- `group=targetUser` → direct targeting (pin to a specific frpc2 by user)
- both `@` and `=` → error
- empty/short value after a delimiter → error

When the SOCKS5 client supplies **no username**, the visitor falls back to static
config: `serverName` (group) and optional `serverUser` (targetUser). If neither
a username nor `serverName` is present, the visitor sends SOCKS5 reply `0x01`
and closes.

| Mode | Trigger | group | userID / targetUser |
|---|---|---|---|
| Passthrough (default) | SOCKS5 username non-empty | parsed from username | parsed from username |
| Static fallback | username empty **and** `serverName` set | `serverName` | `serverUser` (optional) |
| Reject | username empty **and** no `serverName` | — | reply `0x01`, close |

> **Note:** `Authenticate` intentionally allows an empty username (returns empty
> group/userID/targetUser, no error) so the caller can apply the static fallback.
> The actual username parsing still rejects malformed forms (`@`+`=` together,
> empty value after a delimiter, empty group).

## Per-connection message flow

1. SOCKS5 client connects to `frpc1:bindPort`.
2. frpc1: `Handshake` → `Authenticate` (verifies password locally, constant-time) → `ReadConnectRequest`, yielding `(group, userID, targetUser, dstAddr, dstPort)` (or static fallback).
3. frpc1 opens a yamux stream on the existing control session to frps via `helper.ConnectServer()` (no new TCP per request).
4. frpc1 writes `NewSocks5VisitorConn{RunID, Group, UserID, TargetUser, DstAddr, DstPort, AuthPassword}`.
5. frps `handleConnection` reads the first message and dispatches to the `*msg.NewSocks5VisitorConn` case.
6. frps calls `makeSelectFrpcFn()(group, userID, targetUser, dstAddr, dstPort)`: looks up frpc2 in `groupRegistry`, `ReqWorkConn` → work conn, sends `StartWorkConn{DstAddr, DstPort, ProxyName}` to frpc2, returns `(workConn, proxyName, runID)`.
7. frps writes `NewSocks5VisitorConnResp{Error: ""}` to frpc1, then `libio.Join(visitorStream, frpc2WorkConn)`.
8. frpc1 reads the resp; on success sends SOCKS5 reply `0x00`, then `libio.Join(socks5ClientConn, yamuxStream)`.
9. frpc2 `Socks5RelayProxy.InWorkConn` receives `StartWorkConn`, acquires its `maxConcurrent` token, dials the target, `libio.Join(workConn, targetConn)`.

Teardown: standard `libio.Join` EOF cascade — any side closing propagates to all others.

## Configuration

frpc1 visitor:

```toml
[[visitors]]
name         = "my-socks5-entry"
type         = "socks5"
bindAddr     = "127.0.0.1"
bindPort     = 1080
authPassword = "shared-secret"      # required; SOCKS5 client↔frpc1 auth
serverName   = "relay-pool-alpha"   # static fallback group
# serverUser  = "host-b"            # optional: direct targeting fallback
# maxConcurrent = 50                # optional: local concurrency cap (0 = unlimited)
```

frpc2 relay (unchanged):

```toml
group = "relay-pool-alpha"          # top-level ClientCommonConfig field

[[proxies]]
name = "relay-pool-alpha"
type = "socks5_relay"
```

`authPassword` is **required** by config validation. `serverName` lives on
`VisitorBaseConfig`.

## Concurrency & limits

The chain is subject to the wooden-barrel effect documented in
`doc/socks5_concurrence_limits.md`. Limits, narrowest wins:

- frpc1 visitor `maxConcurrent` (optional, default unlimited)
- frpc2 `Socks5RelayProxyConfig.MaxConcurrent` (existing)
- yamux `sendCh` (512) and `maxStreamWindowSize` (6 MB)
- frps `maxProxyConnections` — gates the **public** SOCKS5/HTTP-CONNECT ports
  only; **visitor-originated traffic bypasses it** (visitor is already
  authenticated via the control connection). See Open Questions in the design.

## Error handling & known limitations

| Failure | What the SOCKS5 client sees |
|---|---|
| Handshake / auth fail on frpc1 | Connection reset (no reply — matches SOCKS5 spec) |
| Bad username format | SOCKS5 failure reply `0x01` |
| frpc1 can't open stream to frps | reply `0x01`; control reconnect logic engages |
| No frpc2 in group (`selectFrpcFn` error) | frps → `Resp{Error}` → frpc1 → reply `0x01` |
| frpc2 `GetWorkConn` timeout (default 10s) | reply `0x01` after up to ~10s+ |
| frpc1 `maxConcurrent` reached | immediate reject, no reply (connection reset) |

**Known limitation — late failure after SOCKS5 success reply.** When frpc1 sends
reply `0x00`, it has only confirmed frps accepted the stream; frpc2 has not yet
dialed the target. If frpc2's dial (or its `maxConcurrent` acquire) then fails,
the client sees a successful SOCKS5 handshake followed by an immediate drop.
This matches the existing frps-side socks5-proxy behavior. Two-phase reply /
eager dial are explicitly out of scope for v1.

**`AuthPassword` on the wire is not verified by frpc2.** It is carried in
`NewSocks5VisitorConn` for forward compatibility only; frpc1 validates the SOCKS5
client auth locally and frpc2 trusts it (consistent with the frps listener, where
auth lives on the listener side). The field travels only over the authenticated,
TLS-protected control connection.

## Testing

Unit tests (all passing):

- `pkg/util/socks5/protocol_test.go` — `ParseGroupUserID` matrix; `Handshake` / `Authenticate` / `ReadConnectRequest` / `SendReply` round-trips via `net.Pipe`.
- `server/socks5proxy/dial_handler_test.go` — `HandleNewSocks5VisitorConn` success (bridging + forwarded args) and select-error (writes `Resp{Error}`).
- `pkg/msg/socks5_proxy_test.go` — JSON round-trip of the new messages.
- `pkg/msg/msg_test.go` — `msgTypeMap` size assertion bumped 24 → 26.

E2E (`test/e2e/v1/features/socks5_visitor.go`, 5 scenarios):

1. Basic chain `client → frpc1 → frps → frpc2 → echo`.
2. Round-robin across two frpc2 in a group (verifies the chain works under multi-member routing; does not assert strict distribution).
3. Direct targeting via `group=targetUser`.
4. Wrong auth password → request fails at handshake.
5. No frpc2 in group → SOCKS5 failure path.

Regression: the frps-side listener is untouched behaviorally (pure refactor to
the shared package); the existing `socks5_relay` e2e covers it.

## Design notes

- **No new `client/control.go` method.** The design proposed
  `OpenSocks5VisitorStream`; the implementation reuses the existing
  `Helper.ConnectServer()` + `Helper.RunID()` abstraction (same one
  `dialRawVisitorConn` uses). Cleaner — zero new control-layer code.
- **No `secretKey`.** STCP/XTCP use it for end-to-end visitor↔proxy auth; socks5
  routing relies on frps's `selectFrpcFn` (which already validates group
  membership and frpc2 login auth), so it is unnecessary.
- **No P2P / NAT-traversal variant (cf. XTCP).** Visitor traffic always relays
  through frps. UDP/BIND commands are unsupported — CONNECT over IPv4/IPv6/domain
  only, matching the frps listener.
