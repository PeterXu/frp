# SOCKS5 Relay Session Binding

## Overview

When multiple frpc instances belong to the same group, frps uses round-robin to distribute connections. Optionally, a `userID` can be provided to enable session affinity — all connections from the same `userID` are routed to the same frpc.

## Username Format

The SOCKS5 / HTTP CONNECT auth username determines the routing behavior:

| Username        | Behavior                                       |
|-----------------|------------------------------------------------|
| `group1`        | Pure round-robin, no session binding           |
| `group1@userA`  | Session affinity: sticky to one frpc per user  |

The separator is `@`. Everything before `@` is the group name; everything after is the userID.

## Session Affinity Rules

- **With userID**: first connection round-robin selects an frpc; subsequent connections with the same `group@userID` always route to that frpc (until it disconnects).
- **Without userID**: every connection round-robin independently, no binding.
- **Frpc disconnect**: all session bindings to that frpc are cleared; affected users rebind to a new frpc on next connection.

## Client Configuration

### SOCKS5 Proxy (curl, proxychains4, etc.)

```bash
# No userID — pure round-robin
curl -x socks5h://group1:mypassword@172.19.78.38:1081 http://example.com

# With userID — session affinity
curl -x socks5h://group1@userA:mypassword@172.19.78.38:1081 http://example.com
```

URL parsing (RFC 3986) splits on the **last** `@`, so `group1@userA:mypassword@host` is parsed as:
- userinfo: `group1@userA:mypassword`
- username: `group1@userA`
- password: `mypassword`

### HTTP CONNECT Proxy

```bash
# With userID
curl -x http://group1@userA:mypassword@172.19.78.38:8080 http://example.com
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
