## FRP Socks5 Proxy Extension Requirements

### Core Architecture

```
[ Public frps ]                        [ Internal frpc ]
socks5/http proxy listen <---persistent tunnel--->  frpc(initiates connection)
    |                                        |
    v                                        v
  External target                      Dynamic outbound access
```

**Traffic path**: `client -> frps(proxy listen) -> tunnel -> frpc -> outbound access to external target`

---

### Design Status

**Design document complete**: `doc/socks5_proxy_extension/design.md`

Contains complete design:
- 6 module architecture
- Data structure definitions
- Interface definitions
- Key decision rationale
- Error handling strategy
- Testing strategy

---

### Requirement 1: Reverse Tunnel + Socks5 Chain Forwarding

| Constraint | Description |
|------------|-------------|
| frpc location | Internal network, no public IP, cannot be actively connected |
| Connection direction | frpc actively connects to frps, establishes persistent tunnel |
| Tunnel mechanism | keep-alive long connection, bidirectional traffic after establishment |

---

### Requirement 2: Optional Acceleration Channel + Dynamic Group Management

| Feature | Description |
|---------|-------------|
| Acceleration channel | frpc can optionally connect to frps via third-party socks5 proxy |
| Group management | frps side dynamically manages frpc, supports grouping |

---

### Requirement 3: Session Affinity Routing

| Scenario | Behavior |
|----------|----------|
| Request identifier | SOCKS5/HTTP proxy auth username |
| Group binding | username = group name, directly determines target group |
| First request | Round-Robin selects available frpc within group |
| Subsequent requests | Route to last used frpc (session persistence) |
| frpc offline | Re-select via Round-Robin within group |
| Group empty | Request fails |

---

## Design Decisions

### 1. Architecture Positioning

Extended based on existing FRP roles:
- **frps side**: New socks5/http proxy listen ports (server-level components, configured in ServerConfig)
- **frpc side**: Extended proxy capability, supports dynamic target outbound (not fixed localIP:localPort)
- **Tunnel**: Reuse existing control + work connection mechanism

### 2. Unified Authentication

| Protocol | URL Format | Auth Method |
|----------|------------|-------------|
| HTTP proxy | `http://groupA:pass@frps-host:8080` | Proxy-Authorization: Basic |
| SOCKS5 | `socks5://groupA:pass@frps-host:1080` | Method 2 (Username/Password) |

Auth parameters:
- `username` = group identifier (e.g. "groupA", "vip-users")
- `password` = fixed value (e.g. "pass")

### 3. Listen Port Configuration

frps dual-port separate listening (server-level config in frps.toml):
- **Port 1080**: SOCKS5 proxy (`socks5ProxyPort`)
- **Port 8080**: HTTP CONNECT proxy (`httpConnectProxyPort`)

Each port handled independently, no protocol ambiguity.

### 4. Session Persistence

Dual-layer session persistence:
1. **username -> frpc binding**: Same group name across TCP connections maintains binding to same frpc
2. **TCP connection binding**: Fixed frpc during one CONNECT tunnel

Storage: frps side in-memory (reassign after restart)

### 5. frpc Group Management

Reuse FRP existing group mechanism via proxy type:
- frpc config declares group membership in socks5_relay proxy (`group = "groupA"`)
- frpc dynamically registers with frps on startup via NewProxy message
- groupKey verifies same group members

No extra mapping table needed (username directly used as group name).

### 6. frpc Selection Strategy

Round-Robin (following FRP HTTP Group pattern):
- Maintain counter `index`
- Selection formula: `runIDs[index % len(runIDs)]`
- frpc automatically removed from list when offline

### 7. frpc Offline Detection

Based on FRP existing heartbeat mechanism:
- Control connection heartbeat detection
- frpc offline -> control disconnects -> frps auto-detects
- Session-bound frpc offline -> re-select via Round-Robin

### 8. frpc Connection to frps

Two modes:
- **Default**: frpc connects directly to frps
- **Optional config**: frpc connects via third-party socks5 proxy

Config example (frpc):
```toml
transport.proxyURL = "socks5://127.0.0.1:1080"
```

### 9. frpc Outbound Access

Two modes:
- **Default**: frpc connects directly to target address
- **Optional config**: frpc outbound via proxy

Config example (frpc):
```toml
[[proxies]]
name = "relay-groupA"
type = "socks5_relay"
group = "groupA"
outboundProxy = "socks5://local-proxy:1080"
```

---

## Overall Architecture Diagram

```
[Public frps]                               [Internal frpc]

  :1080 SOCKS5 proxy ----+
  :8080 HTTP proxy   ----+---- tunnel ---+-> frpc(group declaration) ---> external target
  (username=group auth)        |                  ^        |
                               |                  |        |
                               v                  |        v
                      username->frpc binding    (optional proxy outbound)
                      (frps in-memory)
                               ^
                      frpc optional via third-party socks5 to connect frps
```

---

## Comparison with Existing FRP Architecture

| Aspect | Existing FRP proxy | This requirement extension |
|--------|--------------------|---------------------------|
| frps listen | Receives external user requests | Receives proxy requests (SOCKS5/HTTP) |
| Configuration | frpc registers via NewProxy | **Server-level config** (frps.toml) + frpc proxy type |
| frpc processing | Forward to fixed localIP:localPort | **Dynamic outbound**, target from proxy request |
| Traffic endpoint | Internal local service | External target |
| Listener lifecycle | Bound to frpc Control lifecycle | Independent (server-level, NewService) |

**Core difference**: frpc needs "dynamic outbound proxy" capability, target address not fixed.

---

## Config Examples

### frps side

```toml
bindAddr = "0.0.0.0"
bindPort = 7000

# SOCKS5 proxy listener
socks5ProxyPort = 1080
socks5ProxyAuthPassword = "pass"

# HTTP CONNECT proxy listener
httpConnectProxyPort = 8080
httpConnectProxyAuthPassword = "pass"
```

### frpc side

```toml
serverAddr = "frps.example.com"
serverPort = 7000

# Optional: connect to frps through proxy (existing FRP feature)
# transport.proxyURL = "socks5://accelerate-proxy:1080"

[[proxies]]
name = "relay-groupA"
type = "socks5_relay"
group = "groupA"
groupKey = "secret123"

# Optional: outbound via proxy
# outboundProxy = "socks5://local-proxy:1080"
```

### Client usage

```bash
# SOCKS5 proxy
curl --socks5 groupA:pass@frps.example.com:1080 https://target.com/api

# HTTP proxy
curl --proxy http://groupA:pass@frps.example.com:8080 https://target.com/api

# SOCKS5 with hostname (shows domain in dashboard)
curl --socks5-hostname groupA:pass@frps.example.com:1080 https://target.com/api
```

---

### Dashboard & Monitoring

**Connections View** (`/connections`):
- View active and recent (closed) connections
- Real-time stats: active count, recent count, traffic totals
- Adjustable retention period (default 10 minutes, configurable in UI)
- Server-Sent Events (SSE) for live updates
- IPv6 addresses displayed as `[addr]:port`
- Domain names displayed when client uses SOCKS5 domain type

**API Endpoints**:
- `GET /api/socks5relay/connections` - List all connections
- `GET /api/socks5relay/stats` - Aggregated statistics
- `GET/PUT /api/socks5relay/retention` - View/change retention setting
- `GET /api/socks5relay/events` - SSE event stream

**Configuration** (frps.toml):
```toml
[webServer]
addr = "127.0.0.1"
port = 7500
user = "admin"
password = "admin"
assetsDir = "./web/frps/dist"

# Optional: connection retention (default 10min)
# connRetentionDuration = 600  # seconds, 0 = disabled
```
