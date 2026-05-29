# Proxy Types Overview

frp supports several proxy types, each serving a different networking need. This document explains what each type does and how they differ.

## Basic Proxy Types

These types forward traffic from a port on frps to a local service on frpc. frps opens a listener and relays connections to frpc.

### `tcp`
Forward a remote TCP port on frps to a local TCP service.

```
user -> frps:6001 -> frpc -> 127.0.0.1:22
```

### `udp`
Forward a remote UDP port on frps to a local UDP service.

```
user -> frps:6002(UDP) -> frpc -> 114.114.114.114:53
```

### `http`
Route HTTP requests by `Host` header (virtual host) to a local HTTP service. frps listens on a single HTTP port (e.g., 80) and dispatches to different frpcs based on domain name. Supports subdomain, custom domains, path-based routing, HTTP auth, and header rewriting.

```
user -> frps:80 (HTTP, Host: app1.example.com) -> frpc1 -> 127.0.0.1:8080
user -> frps:80 (HTTP, Host: app2.example.com) -> frpc2 -> 127.0.0.1:9090
```

### `https`
Route HTTPS connections by TLS SNI to a local service. Similar to `http` but for TLS traffic. frps inspects the SNI during the TLS handshake to determine routing. Supports Proxy Protocol for passing the real client IP.

### `tcpmux`
Multiplex multiple TCP connections over a single frps port using the HTTP CONNECT protocol. Connections are distinguished by `customDomains` or `routeByHTTPUser`.

```
user -> frps:1337 (HTTP CONNECT, tunnel1) -> frpc -> 127.0.0.1:8080
user -> frps:1337 (HTTP CONNECT, tunnel2) -> frpc -> 127.0.0.1:9090
```

## Peer-to-Peer Proxy Types

These types do not expose a remotePort on frps. Instead, another frpc instance (the visitor) connects through frps to reach the service. All traffic is authenticated with a shared `secretKey`.

### `stcp` (Secret TCP)
Encrypted peer-to-peer TCP tunnel relayed through frps. No remotePort is needed. A visitor frpc connects to frps, which relays the connection to the server frpc.

```
visitor:9000 -> visitor frpc -> frps (relay) -> server frpc -> 127.0.0.1:22
```

### `xtcp` (P2P TCP)
Attempts direct NAT traversal between visitor and server frpcs using STUN-based hole punching. If direct connection succeeds, traffic does not flow through frps. Falls back to frps relay if hole-punching fails.

```
visitor:9001 -> visitor frpc --[P2P direct]--> server frpc -> 127.0.0.1:22
                              (fallback: via frps relay)
```

### `sudp` (Secret UDP)
The UDP equivalent of `stcp`. Encrypted peer-to-peer UDP tunnel relayed through frps.

```
visitor:9002(UDP) -> visitor frpc -> frps (relay) -> server frpc -> 127.0.0.1:53
```

## Special Types

### `socks5_relay`
frpc registers as a SOCKS5 exit node. frps runs a SOCKS5 listener and routes incoming SOCKS5 requests to the appropriate frpc exit node based on user and group membership. Group is determined by the global `group` setting or the TLS certificate OU field.

This is a multi-tenant relay system: multiple frpc instances can serve as exit nodes, and frps selects which one handles each connection.

```
user -> frps:1081 (SOCKS5, user=A, group=G1) -> [route] -> frpc1 (exit node, group=G1) -> target
user -> frps:1081 (SOCKS5, user=B, group=G1) -> [route] -> frpc2 (exit node, group=G1) -> target
```

## Plugin Types

Plugins replace the `localIP`/`localPort` with a built-in handler. When a plugin is configured, frpc handles the incoming connection itself instead of forwarding to a local service.

| Plugin | Description |
|--------|-------------|
| `unix_domain_socket` | Forward TCP traffic to a Unix domain socket (e.g., Docker daemon) |
| `http_proxy` | Expose an HTTP forward proxy |
| `socks5` | Expose a SOCKS5 proxy with optional authentication |
| `static_file` | Serve local files over HTTP |
| `https2http` | Terminate TLS and forward as HTTP to local service |
| `https2https` | Re-encrypt and forward as HTTPS to local service |
| `http2https` | Accept HTTP and forward as HTTPS to local service |
| `http2http` | Accept TCP and forward as HTTP to local service |
| `tls2raw` | Terminate TLS and forward raw TCP to local service |
| `virtual_net` | Inject traffic into a virtual network (requires `VirtualNet` feature gate) |

## How frps Distinguishes Proxy Services

Understanding how frps identifies and routes to different services is key:

### Port-based (tcp, udp, tcpmux, plugins)

For basic types and all plugins, frps distinguishes services **solely by `remotePort`**. frps does not understand the application protocol — it simply forwards TCP/UDP connections on a given port to frpc. The protocol intelligence lives entirely in frpc's plugin.

```
frps:6004 -> frpc -> http_proxy plugin
frps:6005 -> frpc -> socks5 plugin
frps:6006 -> frpc -> static_file plugin
```

frps has no concept of "this is a SOCKS5 proxy" vs "this is an HTTP proxy". It is all plain TCP port forwarding.

### Host-based (http, https)

For `http` and `https` types, frps is protocol-aware. It inspects the HTTP `Host` header or TLS SNI to route connections to the correct frpc. Multiple services share the same listener port.

```
frps:80  -> Host: app1.com -> frpc1
         -> Host: app2.com -> frpc2
```

### User/Group-based (socks5_relay)

For `socks5_relay`, frps runs the SOCKS5 protocol itself. It authenticates users and routes connections to the appropriate frpc exit node based on group membership.

```
frps:1081 -> SOCKS5 auth -> match user/group -> select frpc exit node
```

### Secret-based (stcp, xtcp, sudp)

For peer-to-peer types, there is no public listener on frps. The visitor frpc connects to frps, which matches the `serverName` and `secretKey` to relay (or facilitate P2P with) the server frpc.

```
visitor frpc -> frps (match serverName + secretKey) -> server frpc
```

## Visitors

Visitors are the client side of peer-to-peer proxies. They bind a local port on the visitor frpc that applications can connect to. The traffic is then tunneled through frps to the matching proxy server.

```
local app -> visitor:bindPort -> frps -> proxy server -> local service
```

| Visitor Type | Proxy Type | Description |
|-------------|-----------|-------------|
| `stcp` | stcp | Connect to stcp server via frps relay |
| `xtcp` | xtcp | P2P connection with NAT traversal, optional fallback to stcp |
| `sudp` | sudp | Connect to sudp server via frps relay (UDP) |
