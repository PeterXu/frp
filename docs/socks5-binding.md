# SOCKS5 Relay Session Binding

## Overview

When multiple frpc instances belong to the same group, frps uses round-robin to distribute connections. Optionally, a `userID` can be provided to enable session affinity — all connections from the same `userID` are routed to the same frpc. Alternatively, a `targetUser` can be provided to directly target a specific frpc by its configured `user` field.

## Username Format

The SOCKS5 / HTTP CONNECT auth username determines the routing behavior:

| Username        | Behavior                                       |
|-----------------|------------------------------------------------|
| `group1`        | Pure round-robin, no session binding           |
| `group1@userA`  | Session affinity: sticky to one frpc per user  |
| `group1=frpc1`  | Direct targeting: route to specific frpc by its `user` field |

The separators:
- `@` for session affinity: everything before `@` is the group name; everything after is the userID.
- `=` for direct targeting: everything before `=` is the group name; everything after is the target frpc's `user` field.

Note: `userID` (session affinity) and `targetUser` (direct targeting) are mutually exclusive — you cannot use both in the same username.

## Routing Rules

- **With userID (`@`)**: first connection round-robin selects an frpc; subsequent connections with the same `group@userID` always route to that frpc (until it disconnects).
- **With targetUser (`=`)**: directly route to the frpc whose configured `user` field matches the targetUser; no round-robin, no session affinity. If that frpc is offline or not in the group, the connection fails.
- **Without userID or targetUser**: every connection round-robin independently, no binding.
- **Frpc disconnect**: all session bindings to that frpc are cleared; affected users rebind to a new frpc on next connection.

## Client Configuration

### SOCKS5 Proxy (curl, proxychains4, etc.)

```bash
# No userID — pure round-robin
curl -x socks5h://group1:mypassword@172.19.78.38:1081 http://example.com

# With userID — session affinity
curl -x socks5h://group1@userA:mypassword@172.19.78.38:1081 http://example.com

# With targetUser — direct targeting (route to frpc with user="frpc-beijing")
curl -x socks5h://group1=frpc-beijing:mypassword@172.19.78.38:1081 http://example.com

# With targetUser - ssh connect to frpc machine (no socks5h in ncat)
ssh -o "ProxyCommand ncat --proxy-type socks5 --proxy 127.0.0.1:1081 --proxy-auth group=frpc-beijing:mypassword %h %p" remote-user@localhost
ssh -o "ProxyCommand ncat --proxy-type socks5 --proxy 127.0.0.1:1081 --proxy-auth group=frpc-beijing:mypassword --proxy-dns remote %h %p" remote-user@localhost
```

URL parsing (RFC 3986) splits on the **last** `@`, so `group1@userA:mypassword@host` is parsed as:
- userinfo: `group1@userA:mypassword`
- username: `group1@userA`
- password: `mypassword`

Note: The `=` character is shell-safe (no escaping needed) and URI-safe (valid in userinfo).

### HTTP CONNECT Proxy

```bash
# With userID — session affinity
curl -x http://group1@userA:mypassword@172.19.78.38:8080 http://example.com

# With targetUser — direct targeting
curl -x http://group1=frpc-beijing:mypassword@172.19.78.38:8080 http://example.com
```

### proxychains4

```ini
[ProxyList]
socks5 172.19.78.38 1081 group1@userA mypassword
```

## Architecture

```
client → socks5-proxy → frps (SOCKS5/HTTP CONNECT listener)
                              │
                              ├─ parseGroupUserID("group1@userA")
                              │    → group="group1", userID="userA"
                              │
                              ├─ SelectFrpc("group1", "userA")
                              │    → check sessions["group1@userA"]
                              │    → hit: return bound frpc (affinity)
                              │    → miss: round-robin + store binding
                              │
                              └─ workConn → frpc → target
```

## Group Configuration

One frpc instance belongs to exactly one group. The group is a **client-level** setting, not a per-proxy setting:

```toml
# frpc.toml
group = "groupA"             # client-level, not inside [[proxies]]

# Optional: TLS certificate OU overrides the group
# [transport.tls]
# certFile = "client.crt"    # OU field → group

[[proxies]]
name = "relay"
type = "socks5_relay"
# No group field here — it comes from the client config above
```

Multiple groups require separate frpc instances.

## Performance Tuning

### How poolCount Affects socks5_relay

Each relay request consumes one **work connection** from frpc's pool. The nature of that work connection depends on the transport mode:

| Transport | Work connection = | On-demand cost | poolCount importance |
|---|---|---|---|
| `tcpMux=true` (default) | yamux stream on 1 TCP connection | Instant (in-memory) | Low — streams are cheap, unlimited |
| `tcpMux=false` | Separate TCP connection + TLS handshake | Expensive (full handshake) | **High** — each work connection needs a new TCP+TLS round-trip |
| QUIC | QUIC stream on 1 QUIC connection | Instant (built-in multiplexing) | Low — like yamux |
| KCP | Separate KCP "connection" | Moderate (KCP setup over UDP) | Medium — each needs setup |

**Key point**: With `tcpMux=true` (default) or QUIC, `poolCount` is just a warm cache. yamux/QUIC create new streams instantly in-memory — there is no concurrency limit from poolCount. With `tcpMux=false`, poolCount directly affects how many concurrent relays can start without latency.

### Recommended Settings

**tcpMux=true or QUIC (default):**

```toml
# frpc.toml
transport.poolCount = 1        # default, no need to tune
transport.tcpMux = true        # default
```

**tcpMux=false:**

```toml
# frpc.toml
transport.poolCount = 50       # match your peak concurrent users
transport.tcpMux = false

# frps.toml
transport.maxPoolCount = 50    # must be >= poolCount
```

### Server Connection Limit

Use `maxProxyConnections` to limit concurrent incoming connections on the SOCKS5/HTTP CONNECT listener:

```toml
# frps.toml
maxProxyConnections = 1000     # 0 = unlimited
```

### Client Concurrency Limit

Use `maxConcurrent` on the socks5_relay proxy to limit how many concurrent outbound connections frpc makes:

```toml
# frpc.toml
[[proxies]]
name = "relay"
type = "socks5_relay"
maxConcurrent = 50            # 0 = unlimited (default)
```

Request #51 blocks until one of the first 50 finishes (30s timeout, then rejected).

### Summary of Limits

| Setting | Where | Controls |
|---|---|---|
| `transport.poolCount` | frpc | Pre-warmed work connections (warm cache) |
| `transport.maxPoolCount` | frps | Max idle work connections per client (ceiling on poolCount) |
| `maxProxyConnections` | frps | Max concurrent incoming SOCKS5/HTTP CONNECT connections |
| `maxConcurrent` | frpc proxy | Hard cap on concurrent outbound relay dials |

## Future Plan: Direct Stream Opening (Option B)

### Current Architecture

Each relay request consumes one work connection from frpc's pool:

```
frps: pool empty → ReqWorkConn → frpc → OpenStream → NewWorkConn → frps → put in pool
frps: get from pool → StartWorkConn{target} → relay
```

This involves a message round-trip for on-demand connections. With sufficient `poolCount`, the pool covers peak traffic and latency is zero. But under burst beyond poolCount, requests wait for on-demand creation.

### Option B: frps Opens Streams Directly

frps reuses the existing yamux session (already established with each frpc) to open relay streams directly, without the ReqWorkConn/NewWorkConn round-trip:

```
frps: session.OpenStream() → instant → send StartWorkConn{target} → relay
frpc: AcceptStream() → read target → dial → bridge
```

**Benefits:**
- Zero-latency for all requests (not just pre-warmed ones)
- No poolCount/maxPoolCount tuning needed for socks5_relay
- Simpler internal flow — no ReqWorkConn message round-trip

**Why not now:**
- Current design works well with tcpMux=true (streams are instant to create)
- Would be a new pattern — no other frp proxy type uses server-initiated streams
- Requires storing yamux session per frpc and adding accept loop on frpc side

**When to consider:**
- Large-scale deployments (1000+ concurrent users) where even yamux stream pool management adds overhead
- When tcpMux=false is required and poolCount cannot cover peak traffic
