# SOCKS5/HTTP CONNECT Relay Proxy

*added in v0.53.0*

frps supports SOCKS5 and HTTP CONNECT proxy protocols, allowing frpc instances to act as outbound proxies for external clients. Multiple frpcs can be grouped for load balancing with session affinity.

## Architecture

```
external client -> frps:10800 (SOCKS5) -> [route by group/user] -> frpc (exit node) -> target
external client -> frps:10801 (HTTP CONNECT) -> [route by group/user] -> frpc (exit node) -> target
```

frps runs the proxy protocol and routes each connection to an frpc exit node based on group membership. Multiple frpcs in the same group are load-balanced with session affinity.

## Configuration

### frps

```toml
# frps.toml

# SOCKS5 proxy listener port (0 = disabled)
socks5ProxyPort = 10800
socks5ProxyAuthPassword = "your_password"

# HTTP CONNECT proxy listener port (0 = disabled)
httpConnectProxyPort = 10801
httpConnectProxyAuthPassword = "your_password"

# Maximum concurrent connections on the SOCKS5/HTTP CONNECT proxy ports.
# New connections are blocked until a slot is freed. 0 = unlimited.
# maxProxyConnections = 10000

# Retention time for closed connections in dashboard (default: 600 = 10 minutes)
# connRetentionDuration = 600
```

When running `./frps -c frps.toml`, frps will listen on port 10800 for SOCKS5 and port 10801 for HTTP CONNECT proxy requests.

### frpc

```toml
# frpc.toml
serverAddr = "x.x.x.x"
serverPort = 7000

# Group assignment (overridden by TLS cert OU when cert is configured)
group = "groupA"

[[proxies]]
name = "relay-groupA"
type = "socks5_relay"
# Token pool size: requests wait (block) when limit reached
# 0 = unlimited, recommended: 3-10 for typical usage
maxConcurrent = 3
```

Group membership can be set either via the global `group` field or through the OU (Organization Unit) field of a TLS client certificate. When both are present, the TLS cert OU takes precedence.

## Usage

External clients connect through frps using standard proxy protocols:

```bash
# SOCKS5 (username = group name)
curl -x socks5://groupA:your_password@frps-host:10800 https://example.com

# HTTP CONNECT (username = group name)
curl -x http://groupA:your_password@frps-host:10801 https://example.com
```

The proxy username is the group name. frps authenticates the password, then selects an frpc exit node from that group.

## Dashboard

The frps dashboard includes real-time relay connection monitoring:

- **Connections view** (`/connections`) — shows active and recently closed relay connections
- **Retention period** — configurable via API (`PUT /api/socks5relay/retention`) or `connRetentionDuration` in frps config
- **SSE event stream** (`/api/socks5relay/events`) — real-time connection events for custom monitoring

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/socks5relay/groups` | List relay groups and member status |
| GET | `/api/socks5relay/sessions` | List active user sessions |
| GET | `/api/socks5relay/connections` | List relay connections (use `?active=true` for active only) |
| GET | `/api/socks5relay/stats` | Aggregate connection statistics |
| GET | `/api/socks5relay/events` | SSE stream of connection events |
| GET | `/api/socks5relay/retention` | Get retention duration |
| PUT | `/api/socks5relay/retention` | Set retention duration (0-3600 seconds) |

## See Also

- [SOCKS5 Proxy Extension Design](/doc/socks5_proxy_extension/design.md)
- [Testing Guide](/test/socks5_relay_testing.md)
- [Proxy Types Overview](proxy-types.md)
