## FRP Socks5 Proxy Extension Design

### Overview

This document describes the design for extending FRP with SOCKS5/HTTP CONNECT proxy capabilities, enabling frpc (in internal network) to provide outbound proxy services through frps (public network).

### System Architecture Overview

```
+----------------------------------------------------------------------+
|                          Complete System                              |
|                                                                      |
|  +--------------------+                    +--------------------+    |
|  |   External Client  |                    |   External Target  |    |
|  | (SOCKS5/HTTP user)  |                    |   (target server)  |    |
|  +--------------------+                    +--------------------+    |
|            |                                          ^              |
|            | SOCKS5/HTTP CONNECT                      |              |
|            | (username=group, password=pass)          |              |
|            v                                          |              |
|  +--------------------------------------------------------------+    |
|  |                      frps (Server)                             |    |
|  |                                                               |    |
|  |  :1080 SOCKS5Handler -----+                                   |    |
|  |  :8080 HTTPConnectHandler -+-> SessionManager                  |    |
|  |                            |  (username->frpc binding)        |    |
|  |                            v                                   |    |
|  |                  Socks5RelayGroupRegistry                      |    |
|  |                  (group->runID mapping)                        |    |
|  |                            v                                   |    |
|  |                  ControlManager (existing)                     |    |
|  |                  (select target frpc)                          |    |
|  |                            |                                   |    |
|  |                            v GetWorkConn + StartWorkConn       |    |
|  |                  (with target address DstAddr:DstPort)         |    |
|  +--------------------------------------------------------------+    |
|                            |                                         |
|                            v tunnel messages                         |
|  +--------------------------------------------------------------+    |
|  |                      frpc (Client)                             |    |
|  |                                                               |    |
|  |  Socks5RelayProxy                                             |    |
|  |  (group=groupA config)                                        |    |
|  |       |                                                       |    |
|  |       v parse target address                                  |    |
|  |  OutboundDialer (optional)                                    |    |
|  |  (direct or via proxy outbound)                               |    |
|  |       |                                                       |    |
|  |       v                                                       |    |
|  +--------------------------------------------------------------+    |
|                            |                                         |
|                            v outbound access                         |
|                    External Target                                   |
|                                                                      |
|  Optional: UpstreamProxyDialer (frpc connects to frps via proxy,    |
|            **already exists in FRP via transport.proxyURL**)          |
|                                                                      |
+----------------------------------------------------------------------+

Data flow:
  Client -> frps(proxy port) -> auth(username=group) -> select frpc -> tunnel -> frpc -> target

Module breakdown:
  +--------------------------------------------------------------+
  |  Module 1: frps Server Components (SOCKS5Handler, HTTPConnectHandler) |
  |  Module 2: frpc Proxy Type (Socks5RelayProxy)                 |
  |  Module 3: Session Layer (SessionManager, Registry)           |
  |  Module 4: Configuration Extension                            |
  |  Module 5: Message Extension (reuse StartWorkConn)            |
  |  Module 6: Upstream Proxy (**already exists in FRP**)         |
  +--------------------------------------------------------------+
```

### High-Level Flow

**Flow**: `Client -> frps(proxy listener) -> Tunnel -> frpc -> Outbound -> External Target`

---

## Module 1: frps Server-Level Proxy Components

> **Architecture Decision**: SOCKS5 and HTTP CONNECT proxy listeners are **server-level components**,
> NOT proxy types. They are created in `NewService()` startup, following the same pattern as
> `tcpmuxHTTPConnectPort`, `vhostHTTPPort`. Only `socks5_relay` (Module 2) is a frpc-registered proxy type.

### Architecture

```
+--------------------------------------------------------------+
|                        frps (Service)                         |
|                                                               |
|  +--------------+  +------------------+  +--------------+    |
|  | ControlManager|  | ResourceController |  | ProxyManager |    |
|  | (existing,    |  | (resource mgmt)   |  | (all proxies)|    |
|  | control.go)   |  |                    |  |              |    |
|  +--------------+  +------------------+  +--------------+    |
|         |                   |                                 |
|         |     ResourceController new fields:                  |
|         |       - ControlManager (reference to existing)      |
|         |       - Socks5RelayGroupRegistry (new)              |
|         |       - SessionManager (new)                        |
|         |                   |                                 |
|         +-------------------+                                 |
|                                                               |
|  +-------------------------------------------------------+   |
|  |    SOCKS5Handler / HTTPConnectHandler                  |   |
|  |    (server-level components, NOT proxy types)          |   |
|  |                                                        |   |
|  |    Config source: ServerConfig new fields              |   |
|  |    Created in: NewService()                            |   |
|  |    Listen port: 1080(SOCKS5) / 8080(HTTP CONNECT)     |   |
|  |                                                        |   |
|  |    Processing flow:                                    |   |
|  |      1. Protocol handshake -> extract username(=group)|   |
|  |      2. SessionManager selects target frpc            |   |
|  |      3. targetCtl.GetWorkConn() gets tunnel conn      |   |
|  |      4. Send StartWorkConn(with target address)       |   |
|  |      5. Bridge client <-> tunnel                      |   |
|  +-------------------------------------------------------+   |
|                                                               |
|  Comparison with existing server-level components:            |
|  +---------------------+----------------------------------+   |
|  | tcpmuxHTTPConnectPort| shared port + proxy route reg   |   |
|  | vhostHTTPPort        | shared port + proxy route reg   |   |
|  | sshTunnelGateway     | standalone, creates virtual clnt|   |
|  | SOCKS5/HTTPConnect   | standalone, dynamic route to fpc|   |
|  +---------------------+----------------------------------+   |
|                                                               |
+--------------------------------------------------------------+
```

### Data Structures

**ServerConfig new fields** (pkg/config/v1/server.go):
```go
// SOCKS5 proxy configuration
Socks5ProxyPort         int    `json:"socks5ProxyPort,omitempty"`
Socks5ProxyAuthPassword string `json:"socks5ProxyAuthPassword,omitempty"`

// HTTP CONNECT proxy configuration
HTTPConnectProxyPort         int    `json:"httpConnectProxyPort,omitempty"`
HTTPConnectProxyAuthPassword string `json:"httpConnectProxyAuthPassword,omitempty"`
```

**SOCKS5Handler** (server/socks5proxy/handler.go):
```
Fields:
  - listener: net.Listener      // SOCKS5 listener
  - authPassword: string        // auth password
  - sessionMgr: *SessionManager // session management (from ResourceController)

Methods:
  - Run(ctx)                    // start listening, accept connections
  - Close()                     // close listener
  - handleConn(net.Conn)        // handle single SOCKS5 connection
```

**HTTPConnectHandler** (server/socks5proxy/http_connect.go):
```
Fields:
  - listener: net.Listener      // HTTP CONNECT listener
  - authPassword: string        // auth password
  - sessionMgr: *SessionManager // session management (from ResourceController)

Methods:
  - Run(ctx)                    // start listening, accept connections
  - Close()                     // close listener
  - handleConn(net.Conn)        // handle single HTTP CONNECT connection
```

### Key Decisions

| Decision | Reason |
|----------|--------|
| Server-level components, not proxy types | SOCKS5/HTTPConnect listeners are shared resources, not owned by any frpc Control lifecycle. Follows tcpmuxHTTPConnectPort pattern. |
| Config in ServerConfig | Consistent with existing vhostHTTPPort, tcpmuxHTTPConnectPort - port configured in frps.toml |
| Created in NewService() | Ensures listeners are ready at service startup, independent of any frpc connection |
| Does NOT inherit BaseProxy | BaseProxy binds to a single Control's getWorkConnFn. SOCKS5 needs dynamic selection across multiple frpcs. |
| Does NOT register in proxyConfigTypeMap | socks5/http_connect are not proxy types, no Configurer/MarshalToMsg needed |

### Relationship with Existing Code

**Reuse**:
- ControlManager (existing, server/control.go) - accessed via ResourceController reference
- Control.GetWorkConn() - get work connection from selected frpc
- msg.StartWorkConn - notify frpc of target address

**Add**:
- SOCKS5/HTTP CONNECT protocol handling (new components)
- SessionManager (session affinity)
- Socks5RelayGroupRegistry (group registration)

---

## Module 2: frpc Proxy Type

### Architecture

```
+--------------------------------------------------------------+
|                        frpc (Client)                          |
|                                                               |
|  +-------------------------------------------------------+   |
|  |              Socks5RelayProxy                          |   |
|  |                                                        |   |
|  |  Processing flow:                                      |   |
|  |    1. Receive StartWorkConn message                    |   |
|  |    2. Parse target address (DstAddr:DstPort)           |   |
|  |    3. Connect target (direct or via OutboundProxy)     |   |
|  |    4. Bridge tunnel connection <-> target connection   |   |
|  |                                                        |   |
|  +-------------------------------------------------------+   |
|                             |                                |
|  +--------------------------+----------------------------+   |
|  |              OutboundDialer (optional)                  |   |
|  |                                                         |   |
|  |  Supports two outbound modes:                           |   |
|  |    - Default: direct connection to target               |   |
|  |    - Optional config: via socks5/http proxy outbound    |   |
|  |                                                         |   |
|  +-------------------------------------------------------+   |
|                                                               |
|  +-------------------------------------------------------+   |
|  |    UpstreamProxyDialer (optional, **already in FRP**)   |   |
|  |                                                         |   |
|  |  frpc can connect to frps via third-party socks5 proxy |   |
|  |  Config: transport.proxyURL                             |   |
|  |                                                         |   |
|  +-------------------------------------------------------+   |
|                                                               |
+--------------------------------------------------------------+
```

### Data Structures

**Socks5RelayProxyConfig** (frpc side):
```
Inherits: ProxyBaseConfig

New fields:
  - OutboundProxy: string  // outbound proxy URL (optional)
  - MaxConcurrent: int     // concurrent relay connection limit (0 = unlimited)
```

**ClientCommonConfig Extension** (group source):
```
Group: string  // socks5_relay group name, sent in loginMsg.Metas["group"]

Group resolution order:
  1. ClientCommonConfig.Group (from frpc.toml "group" field)
  2. TLS certificate OU field (overrides the above if present)
```

> **Constraint**: One frpc instance belongs to exactly one group. The group is a client-level
> setting, not a per-proxy setting. All socks5_relay proxies on the same frpc share the same group.

**Socks5RelayProxy** (client/proxy/socks5_relay.go):
```
Fields:
  - BaseProxy              // inherit existing structure
  - cfg: Socks5RelayProxyConfig
  - tokenPool: chan struct{}  // semaphore for MaxConcurrent limiting
```

**OutboundDialer**:
```
Fields:
  - proxyURL: string       // socks5://host:port or http://host:port

Capability:
  - Dial(targetAddr) -> net.Conn  // connect to target via proxy or directly
```

### Interfaces

**client/proxy.Proxy interface** (reuse existing):
```
Run() -> error
InWorkConn(conn: net.Conn, m: StartWorkConn) -> void
Close() -> void
```

**OutboundDialer**:
```
Dial(addr: string) -> (net.Conn, error)
```

> OutboundDialer is stateless (holds only proxyURL config), no Close() needed.

### Key Decisions

| Decision | Reason |
|----------|--------|
| Target address from StartWorkConn.DstAddr/DstPort | Reuse existing message fields, no protocol modification needed |
| OutboundProxy as optional config | Support two scenarios: direct (default) and proxy outbound (optional) |
| OutboundDialer independent encapsulation | Proxy logic is complex, should be independent module |
| UpstreamProxy reuse existing transport.proxyURL | FRP already implements this, no duplicate development |

### Comparison with Existing TCP Proxy

| Aspect | Existing TCP Proxy | Socks5RelayProxy |
|--------|--------------------|------------------|
| Target source | Config: LocalIP:LocalPort | Message: DstAddr:DstPort |
| Target count | Fixed single address | Dynamic any address |
| Outbound method | Direct to local | Direct or through proxy |
| InWorkConn flow | HandleTCPWorkConnection -> local dial | Parse DstAddr -> OutboundDialer.Dial -> Join |

### Server-Side Proxy (Minimal)

In FRP's architecture, every proxy type needs both a client factory and a server factory.
When frpc sends `NewProxy(type="socks5_relay")`, `proxy.NewProxy()` looks up the server-side
factory by configurer type. Without a registered factory, registration fails.

**Socks5RelayServerProxy** (server/proxy/socks5_relay.go):
```
Fields:
  - BaseProxy              // inherit existing structure
  - cfg: Socks5RelayProxyConfig

Methods:
  - Run() -> (remoteAddr, err)
      // Extract group from loginMsg.Metas["group"] (set by ClientCommonConfig.Group)
      // Validate group name matches regex: ^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,62}$
      // Register in Socks5RelayGroupRegistry
      rc.Socks5RelayGroupRegistry.Register(group, loginMsg.RunID, cfg.Name)
      return "", nil  // no listener, no remoteAddr

  - Close()
      // Unregister from Socks5RelayGroupRegistry (removes all groups for this runID)
      rc.Socks5RelayGroupRegistry.Unregister(loginMsg.RunID)
      // Eager cleanup: remove sessions bound to this frpc
      rc.Socks5SessionManager.RemoveSession(loginMsg.RunID)
      BaseProxy.Close()
```

> **Constraint**: One frpc = one group. `Unregister(runID)` removes the single group
> registration for that frpc. If a deployment needs multiple groups, use separate frpc instances.

Server-side factory registration (init):
```go
func init() {
    RegisterProxyFactory(reflect.TypeFor[*v1.Socks5RelayProxyConfig](), NewSocks5RelayServerProxy)
}
```

This proxy does NOT listen on any port, does NOT handle user connections, and does NOT
use `getWorkConnFn`. Its sole purpose is lifecycle management: register the frpc's group
membership when the proxy starts, unregister when it closes.

---

## Module 3: Session Manager & Group Registry

### Architecture

```
+--------------------------------------------------------------+
|                    frps (Session Layer)                        |
|                                                               |
|  +-------------------------------------------------------+   |
|  |           Socks5RelayGroupRegistry                      |   |
|  |                                                         |   |
|  |  Responsibility: track group -> frpc(runID) mappings   |   |
|  |                                                         |   |
|  |  Data:                                                  |   |
|  |    - groups: map[group] -> []runID                     |   |
|  |    - runIDGroups: map[runID] -> []group                |   |
|  |                                                         |   |
|  |  Triggers:                                              |   |
|  |    - Register: socks5_relay proxy registration         |   |
|  |    - Unregister: frpc disconnect or proxy close        |   |
|  |                                                         |   |
|  +-------------------------------------------------------+   |
|                             |                                 |
|                             v                                 |
|  +-------------------------------------------------------+   |
|  |              SessionManager                              |   |
|  |                                                         |   |
|  |  Responsibility: manage username -> frpc session binding|   |
|  |                                                         |   |
|  |  Data:                                                  |   |
|  |    - sessions: map[username] -> runID                  |   |
|  |    - groupIndex: map[group] -> Round-Robin counter     |   |
|  |                                                         |   |
|  |  Selection flow:                                        |   |
|  |    1. Check session binding -> if bound frpc alive, ret |   |
|  |    2. If not exists or offline -> Round-Robin new frpc |   |
|  |    3. Update session binding                            |   |
|  |                                                         |   |
|  +-------------------------------------------------------+   |
|                             |                                 |
|                             v                                 |
|  +-------------------------------------------------------+   |
|  |              ControlManager (existing)                   |   |
|  |              (server/control.go)                         |   |
|  |                                                         |   |
|  |  Responsibility: manage all frpc Control objects        |   |
|  |  Already exists, no modification needed                 |   |
|  |                                                         |   |
|  |  Data:                                                  |   |
|  |    - ctlsByRunID: map[runID] -> Control                |   |
|  |                                                         |   |
|  |  Capability:                                            |   |
|  |    - GetByID(runID) -> (*Control, bool)               |   |
|  |    - Control.GetWorkConn() -> workConn                 |   |
|  |                                                         |   |
|  +-------------------------------------------------------+   |
|                                                               |
+--------------------------------------------------------------+
```

### Data Flow (Select frpc)

```
SOCKS5 request -> extract username(=group)
      |
      v
SessionManager.SelectFrpc(username)
      |
      v
Check sessions[username] -> has binding?
      | Yes                      | No
      v                          v
Verify frpc is alive           Round-Robin select
      |                              |
      v                              v
Return bound Control           Update sessions[username]
      |                              |
      +------------------------------+
      |
      v
Return Control object
      |
      v
Control.GetWorkConn() -> get tunnel connection
```

### Data Structures

**Socks5RelayGroupRegistry**:
```
Fields:
  - groups: map[string][]string      // group -> runID list
  - runIDGroups: map[string][]string // runID -> group list (reverse index)
  - mu: sync.RWMutex                 // concurrency protection
```

**SessionManager**:
```
Fields:
  - sessions: map[string]string              // "group@userID" -> runID binding
  - runIDSessions: map[string]map[string]struct{} // runID -> set of sessionKeys (reverse index)
  - groupIndex: map[string]*atomic.Uint64    // group -> RR counter
  - groupRegistry: *Socks5RelayGroupRegistry // get group members
  - ctlManager: *ControlManager              // get Control object (existing)
  - mu: sync.RWMutex                        // concurrency protection
```

**ResourceController Extension**:
```
New fields:
  - ControlManager: *ControlManager              // reference to existing Service-level manager
  - Socks5RelayGroupRegistry: *Socks5RelayGroupRegistry  // new
  - SessionManager: *SessionManager              // new
```

### Interfaces

**Socks5RelayGroupRegistry**:
```
Register(group: string, runID: string, proxyName: string) -> void
Unregister(runID: string) -> void
GetGroupMembers(group: string) -> []string  // sorted runIDs
GetProxyName(group: string, runID: string) -> string
```

**ControlManager** (existing, no changes):
```
Add(runID: string, ctl: *Control) -> *Control  // already called in Service.RegisterControl(), returns old control if replaced
Del(runID: string, ctl: *Control)               // already called on frpc disconnect, verifies pointer identity
GetByID(runID: string) -> (*Control, bool)      // lookup by runID
```

**SessionManager**:
```
SelectFrpc(group: string, userID: string) -> (*Control, string, error)
    // Returns Control, proxyName, error
    // With userID: session affinity keyed by "group@userID"
    // Without userID: always round-robin
RemoveSession(runID: string) -> void   // eager cleanup in Socks5RelayServerProxy.Close()
ClearBinding(sessionKey: string) -> void // clear binding on work conn failure
```

### Key Decisions

| Decision | Reason |
|----------|--------|
| New Socks5RelayGroupRegistry | Existing group tracking is proxy-level, we need frpc(runID)-level mapping |
| Bidirectional index (groups + runIDGroups) | Fast cleanup when frpc disconnects — Unregister(runID) removes all group entries |
| Reverse index (runIDSessions) | O(1) session cleanup on frpc disconnect, avoids linear scan |
| Round-Robin not random | Follow FRP HTTP Group mechanism, more even load distribution |
| In-memory session state | Simplified design, reassign on frps restart |
| Reference existing ControlManager | Already exists in server/control.go, managed by Service. No new ControlManager needed. |

### Trigger Timing

| Event | Trigger | Where it happens |
|-------|---------|-----------------|
| frpc connects | ControlManager.Add(runID, ctl) | **Already happens** in Service.RegisterControl() (service.go:787) |
| frpc disconnects | ControlManager.Del(runID, ctl) | **Already happens** after ctl.WaitClosed() (service.go:501) |
| socks5_relay proxy registered | Registry.Register(group, runID) | **New** in Socks5RelayServerProxy.Run() (server/proxy/socks5_relay.go) |
| socks5_relay proxy unregistered | Registry.Unregister(runID) + SessionManager.RemoveSession(runID) | **New** in Socks5RelayServerProxy.Close() (server/proxy/socks5_relay.go) |
| SOCKS5 request arrives | SessionManager.SelectFrpc(username) | **New** in SOCKS5Handler.handleConn() |

> **Note**: ControlManager is already fully populated by existing FRP flow.
> All frpc connections (including non-socks5_relay types) are registered in ControlManager at connect time.
> We only need to add a reference to it in ResourceController.

### Dependency Graph

```
SOCKS5Handler / HTTPConnectHandler
    | depends on
    v
SessionManager
    | depends on
    +------------------+
    |                  |
    v                  v
Socks5RelayGroupRegistry   ControlManager (existing)
    |                  |
    +------------------+
         |
         v
  ResourceController (centralized management)
```

**New file paths**:
```
server/socks5proxy/handler.go              - SOCKS5Handler implementation
server/socks5proxy/http_connect.go         - HTTPConnectHandler implementation
server/proxy/session_manager.go            - SessionManager implementation
server/controller/socks5_group_registry.go - Socks5RelayGroupRegistry implementation
server/proxy/socks5_relay.go               - server-side socks5_relay proxy (minimal, group registration)
client/proxy/socks5_relay.go               - frpc Socks5RelayProxy implementation
```

> **Note**: No new ControlManager file is needed - it already exists in `server/control.go`.
> We only add a reference field `ControlManager` to `ResourceController` and wire it
> in `NewService()` as `svr.rc.ControlManager = svr.ctlManager`.

---

## Module 4: Configuration Extension

### Architecture

```
+--------------------------------------------------------------+
|                   pkg/config/v1/server.go                     |
|                                                               |
|  Existing server-level config fields:                         |
|  +------------------+  +------------------+  +-------------+ |
|  | VhostHTTPPort    |  | TCPMuxHTTPConnect|  | SSHGateway  | |
|  +------------------+  +------------------+  +-------------+ |
|                                                               |
|  New server-level config fields:                              |
|  +------------------------+  +----------------------------+  |
|  | Socks5ProxyPort        |  | HTTPConnectProxyPort       |  |
|  | Socks5ProxyAuthPassword|  | HTTPConnectProxyAuthPassword|  |
|  +------------------------+  +----------------------------+  |
|                                                               |
+--------------------------------------------------------------+

+--------------------------------------------------------------+
|                   pkg/config/v1/proxy.go                      |
|                                                               |
|  New proxy config type (frpc only):                           |
|  +----------------------------------------------------+      |
|  |        Socks5RelayProxyConfig                       |      |
|  |        (frpc side, registered via NewProxy message) |      |
|  +----------------------------------------------------+      |
|                                                               |
|  Config registration mechanism:                               |
|  +----------------------------------------------------+      |
|  |        proxyConfigTypeMap                           |      |
|  |  map[ProxyType] -> reflect.Type                    |      |
|  |                                                     |      |
|  |  New entry:                                         |      |
|  |    "socks5_relay" -> Socks5RelayProxyConfig        |      |
|  +----------------------------------------------------+      |
|                                                               |
+--------------------------------------------------------------+
```

### Data Structures

**ServerConfig new fields** (pkg/config/v1/server.go):
```go
// SOCKS5 proxy listener port, 0 = disabled
Socks5ProxyPort         int    `json:"socks5ProxyPort,omitempty"`
// SOCKS5 proxy auth password
Socks5ProxyAuthPassword string `json:"socks5ProxyAuthPassword,omitempty"`

// HTTP CONNECT proxy listener port, 0 = disabled
HTTPConnectProxyPort         int    `json:"httpConnectProxyPort,omitempty"`
// HTTP CONNECT proxy auth password
HTTPConnectProxyAuthPassword string `json:"httpConnectProxyAuthPassword,omitempty"`
```

**ProxyType Constants** (pkg/config/v1/proxy.go):
```
New:
  ProxyTypeSocks5Relay  = "socks5_relay"
```

> **Note**: No ProxyTypeSocks5 or ProxyTypeHTTPConnect constants are needed.
> SOCKS5 and HTTP CONNECT are server-level components, not proxy types.

**Socks5RelayProxyConfig** (frpc side, pkg/config/v1/proxy.go):
```
Inherits: ProxyBaseConfig

New fields:
  - OutboundProxy: string   // outbound proxy URL (optional)
  - MaxConcurrent: int      // concurrent relay connection limit (0 = unlimited)
```

> Group is NOT a field on this config. It comes from `ClientCommonConfig.Group`
> and is sent via `loginMsg.Metas["group"]`. See Module 2 for details.

**MarshalToMsg/UnmarshalFromMsg**:
```go
func (c *Socks5RelayProxyConfig) MarshalToMsg(m *msg.NewProxy) {
    c.ProxyBaseConfig.MarshalToMsg(m)
    m.OutboundProxy = c.OutboundProxy
}

func (c *Socks5RelayProxyConfig) UnmarshalFromMsg(m *msg.NewProxy) {
    c.ProxyBaseConfig.UnmarshalFromMsg(m)
    c.OutboundProxy = m.OutboundProxy
}
```

> **Note**: The group is NOT carried in `Socks5RelayProxyConfig`. Instead, it
> is a **client-level** setting in `ClientCommonConfig.Group`, sent via
> `loginMsg.Metas["group"]` during the Login handshake. On the server side,
> the group is extracted from `loginMsg.Metas["group"]` when the socks5_relay
> proxy runs (`server/proxy/socks5_relay.go:Run()`). This means one frpc
> instance belongs to exactly one group.
>
> The `OutboundProxy` field uses a dedicated field in `msg.NewProxy`
> (not the metas map).

**Complete() and Clone()** (follow existing pattern):
```go
func (c *Socks5RelayProxyConfig) Complete() {
    c.ProxyBaseConfig.Complete()
}

func (c *Socks5RelayProxyConfig) Clone() ProxyConfigurer {
    out := *c
    out.ProxyBaseConfig = c.ProxyBaseConfig.Clone()
    return &out
}
```

### Interfaces

**ProxyConfigurer** (reuse existing):
```
GetBaseConfig() -> *ProxyBaseConfig
Clone() -> ProxyConfigurer
Complete() -> void
MarshalToMsg(*msg.NewProxy)
UnmarshalFromMsg(*msg.NewProxy)
```

### Key Decisions

| Decision | Reason |
|----------|--------|
| SOCKS5/HTTPConnect config in ServerConfig | Server-level components configured in frps.toml, consistent with vhostHTTPPort pattern |
| Only socks5_relay as proxy type | SOCKS5/HTTPConnect listeners don't need proxy registration mechanism |
| Group is client-level, not proxy-level | One frpc = one group. Simplifies session management — Unregister(runID) removes the single group. |
| OutboundProxy via dedicated msg field | Clean first-class field in msg.NewProxy, not overloaded in metas map |
| TLS cert OU overrides group | Allows group assignment via certificate, useful in managed deployments |

### Naming Convention

| Context | Style | Example |
|---------|-------|---------|
| Go struct field | UpperCamelCase | `Socks5ProxyPort`, `AuthPassword`, `OutboundProxy` |
| TOML field | lowerCamelCase | `socks5ProxyPort`, `authPassword`, `outboundProxy` |
| Proxy type constant | UpperCamelCase | `ProxyTypeSocks5Relay` |

### Port Conflict Handling

| Scenario | Handling |
|----------|----------|
| socks5ProxyPort conflicts with bindPort | `net.Listen()` fails in NewService(), returns error at startup |
| socks5ProxyPort conflicts with vhostHTTPPort or other server-level ports | `net.Listen()` fails in NewService(), same as existing vhostHTTPPort behavior |
| socks5ProxyPort conflicts with proxy-allocated ports | Possible if proxy uses same port via TCPPortManager; operator must avoid overlap |

### Comparison with Existing Configs

| Config Type | Location | Target Source | Listen Mode |
|-------------|----------|---------------|-------------|
| VhostHTTPPort | ServerConfig | domain/location routing | Shared HTTP server |
| TCPMuxHTTPConnectPort | ServerConfig | domain routing | Shared muxer |
| Socks5ProxyPort | ServerConfig | username(=group) routing | **Standalone handler** |
| HTTPConnectProxyPort | ServerConfig | username(=group) routing | **Standalone handler** |
| Socks5RelayProxyConfig | frpc proxy | Message: DstAddr/DstPort | Passive, no listen |

---

## Module 5: Message Extension

### Architecture

```
+--------------------------------------------------------------+
|                   pkg/msg/msg.go                              |
|                                                               |
|  Existing message types (no changes needed):                  |
|  +------------------------------------------------------+    |
|  |              StartWorkConn                            |    |
|  |                                                       |    |
|  |  Existing fields:                                     |    |
|  |    - ProxyName: string      // proxy name            |    |
|  |    - SrcAddr: string        // client source address  |    |
|  |    - SrcPort: uint16        // client source port     |    |
|  |    - DstAddr: string        // target address (ok)   |    |
|  |    - DstPort: uint16        // target port (ok)      |    |
|  |    - Error: string          // error info            |    |
|  |                                                       |    |
|  |  Conclusion: DstAddr/DstPort already exist            |    |
|  |                                                       |    |
|  +------------------------------------------------------+    |
|                                                               |
|  Existing message reuse:                                      |
|  +------------------------------------------------------+    |
|  |              NewProxy                                 |    |
|  |                                                       |    |
|  |  Existing fields:                                     |    |
|  |    - ProxyType: string      // identifies socks5_relay|   |
|  |    - OutboundProxy: string // outbound proxy URL (new)|   |
|  |    - Metas: map[string]string // carries other metadata|   |
|  |                                                       |    |
|  |  Conclusion: Reuse existing fields for group info     |    |
|  |  No new message types or fields needed                |    |
|  +------------------------------------------------------+    |
|                                                               |
+--------------------------------------------------------------+
```

### Message Flow

```
Client request -> SOCKS5 protocol handshake
                |
                v
         extract username(=group), targetAddr
                |
                v
         SessionManager.SelectFrpc(username)
                |
                v
         targetCtl.GetWorkConn() -> workConn
                |
                v
         Send StartWorkConn message:
           {
             ProxyName: "relay-groupA",
             DstAddr: "target.com",
             DstPort: 443
           }
                |
                v
         frpc receives StartWorkConn
                |
                v
         Parse DstAddr:DstPort -> connect to target
```

### Key Decisions

| Decision | Reason |
|----------|--------|
| No new message types needed | StartWorkConn already has DstAddr/DstPort fields |
| One new field in msg.NewProxy | `OutboundProxy` carries the optional outbound proxy URL from frpc |
| Group via Login metas | Group sent as `loginMsg.Metas["group"]` during Login, not via NewProxy |
| DstAddr/DstPort semantic extension | Existing proxy: ProxyProtocol info (optional); SOCKS5: target address (required) |
| DstAddr/DstPort semantic extension | Existing proxy: ProxyProtocol info (optional); SOCKS5: target address (required) |

### DstAddr/DstPort Semantic Comparison

| Proxy Type | DstAddr/DstPort Usage |
|------------|----------------------|
| TCP Proxy | ProxyProtocol info (optional) |
| HTTP Proxy | ProxyProtocol info (optional) |
| SOCKS5 Handler | **Required**: target address |
| socks5_relay | **Required**: connect target |

---

## Module 6: frpc Upstream Proxy

> **Note**: This feature already exists in FRP (`transport.proxyURL`), no new implementation needed.
> This module serves as design reference only.

### Architecture

```
+--------------------------------------------------------------+
|                        frpc (Client)                          |
|                                                               |
|  Connection modes to frps:                                    |
|                                                               |
|  +-------------------------------------------------------+   |
|  |              Default: direct connection to frps        |   |
|  |                                                         |   |
|  |  frpc --------direct TCP connection--------> frps      |   |
|  |                                                         |   |
|  +-------------------------------------------------------+   |
|                                                               |
|  +-------------------------------------------------------+   |
|  |              Optional: connect via proxy               |   |
|  |                                                         |   |
|  |  frpc --SOCKS5--> third-party proxy --> frps           |   |
|  |        (config transport.proxyURL)                      |   |
|  |                                                         |   |
|  |  Use cases:                                             |   |
|  |    - Network acceleration                               |   |
|  |    - Bypass restrictions                                |   |
|  |    - Multi-hop routing                                  |   |
|  |                                                         |   |
|  +-------------------------------------------------------+   |
|                                                               |
+--------------------------------------------------------------+
```

### Existing FRP Implementation

**Config field** (already in ClientCommonConfig):
```
transport.proxyURL: string  // supports http, https, socks5, socks5h, ntlm
```

**Config example**:
```toml
# Use existing config, no new fields needed
transport.proxyURL = "socks5://user:passwd@accelerate-proxy:1080"
```

---

## Module 7: Dashboard & Monitoring

> **Overview**: Real-time connection tracking and monitoring through the web dashboard.

### Architecture

```
+--------------------------------------------------------------+
|                        frps Dashboard                         |
|                                                               |
|  +-------------------------------------------------------+   |
|  |              RelayConnTracker                          |   |
|  |                                                       |   |
|  |  Responsibility: track active & closed connections    |   |
|  |                                                       |   |
|  |  Data:                                                |   |
|  |    - connections: map[id] -> RelayConnInfo            |   |
|  |    - closedConnections: map[id] -> RelayConnInfo      |   |
|  |    - retentionDuration: time.Duration                 |   |
|  |    - subscribers: map[chan] -> struct{}               |   |
|  |                                                       |   |
|  |  Operations:                                          |   |
|  |    - Track(info) -> id                               |   |
|  |    - UpdateBytes(id, bytesIn, bytesOut)              |   |
|  |    - Remove(id) -> moves to closedConnections        |   |
|  |    - GetAll() -> []RelayConnInfo                      |   |
|  |    - GetAllIncludingClosed() -> []RelayConnInfo       |   |
|  |    - Subscribe(ctx) -> <-chan RelayConnEvent          |   |
|  |                                                       |   |
|  |  Cleanup:                                             |   |
|  |    - Periodic cleanup every minute                    |   |
|  |    - Remove closed connections > retentionDuration    |   |
|  |    - Early cleanup if >10,000 entries (memory limit)  |   |
|  |                                                       |   |
|  +-------------------------------------------------------+   |
|                             |                                 |
|                             v                                 |
|  +-------------------------------------------------------+   |
|  |              API Endpoints                            |   |
|  |                                                       |   |
|  |  GET  /api/socks5relay/connections                   |   |
|  |  GET  /api/socks5relay/stats                         |   |
|  |  GET  /api/socks5relay/retention                     |   |
|  |  PUT  /api/socks5relay/retention                     |   |
|  |  GET  /api/socks5relay/events (SSE)                  |   |
|  |                                                       |   |
|  +-------------------------------------------------------+   |
|                             |                                 |
|                             v                                 |
|  +-------------------------------------------------------+   |
|  |              Dashboard UI                             |   |
|  |                                                       |   |
|  |  Connections View:                                    |   |
|  |    - Active connections table                         |   |
|  |    - Recent connections table (closed)                 |   |
|  |    - Stats cards (active count, traffic)              |   |
|  |    - Retention adjustment dialog                       |   |
|  |    - SSE real-time updates                            |   |
|  |                                                       |   |
|  +-------------------------------------------------------+   |
|                                                               |
+--------------------------------------------------------------+
```

### Data Structures

**RelayConnInfo** (server/socks5proxy/relay_conn_tracker.go):
```go
type RelayConnInfo struct {
    ID        string      // Unique connection ID
    SourceIP  string      // Client IP:port
    Protocol  string      // "socks5" or "http_connect"
    Group     string      // Username (= group name)
    DstAddr   string      // Target address (IPv4, IPv6, or domain)
    DstPort   uint16      // Target port
    ProxyName string      // frpc proxy name
    RunID     string      // frpc instance ID
    StartTime time.Time   // Connection start time
    EndTime   *time.Time  // nil if active, set if closed
    BytesIn   int64       // Bytes from client
    BytesOut  int64       // Bytes to client
    IsActive  bool        // true for active, false for closed
}
```

**RelayConnEvent** (SSE events):
```go
type RelayConnEvent struct {
    Type string        `json:"type"` // "created", "updated", "deleted"
    Conn RelayConnInfo `json:"conn"`
}
```

### API Endpoints

| Endpoint | Method | Response | Description |
|----------|--------|----------|-------------|
| `/api/socks5relay/connections` | GET | `[]RelayConnectionInfo` | Get all connections (active + closed) |
| `/api/socks5relay/stats` | GET | `RelayConnectionStats` | Get aggregated stats |
| `/api/socks5relay/retention` | GET | `{retentionSeconds: int}` | Get current retention setting |
| `/api/socks5relay/retention` | PUT | `{retentionSeconds: int}` | Set retention (0-3600 seconds) |
| `/api/socks5relay/events` | GET | SSE stream | Real-time connection events |

### Dashboard Features

**Connections View** (`/connections`):
```
┌─────────────────────────────────────────────────────────────┐
│  Connections                                                 │
│  Active and recent SOCKS5/HTTP CONNECT relay connections    │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Active: 12   │  │ Recent: 45   │  │ Traffic:     │      │
│  │              │  │ (10min)      │  │ ↓1.2MB ↑800KB│      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                              │
│  [Active Connections] [Recent Connections]                  │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ Source │ Protocol │ Group │ Dest │ Proxy │ Duration │   │ │
│  │────────│──────────│───────│──────│───────│──────────│───│ │
│  │ ...    │ ...      │ ...   │ ...  │ ...   │ ...      │   │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

**Features**:
- **Active Connections Tab**: Shows currently connected clients
- **Recent Connections Tab**: Shows closed connections within retention period
- **Auto-refresh**: Updates every 30 seconds via polling + SSE events
- **Retention Adjustment**: Click "Recent" card to adjust retention time
- **IPv6 Support**: Properly formats IPv6 addresses as `[addr]:port`
- **Domain vs IP**: Shows what the client sent (domain if using `--socks5-hostname`, IP otherwise)

### Retention Settings

**Behavior**:
- Default: 10 minutes (600 seconds)
- Range: 0 (disabled) to 3600 (1 hour)
- Runtime-only: Resets to 10min on frps restart
- Validation: Server-side, rejects invalid values

**Presets in UI**:
- Disabled (0s) - No closed connections stored
- 1 minute (60s)
- 5 minutes (300s)
- 10 minutes (600s) - Default
- 15 minutes (900s)
- 30 minutes (1800s)
- 1 hour (3600s)

### Destination Display

**What gets displayed** depends on what the SOCKS5 client sends:

| Client Mode | Address Type | Example Display |
|-------------|--------------|-----------------|
| `curl -x` | IPv4/IPv6 address | `93.184.216.34:80` or `[2606:2800:220:1:248:1893:25c8:1946]:80` |
| `curl --socks5-hostname` | Domain name | `example.com:80` |

**SOCKS5 Protocol Support**:
- Type 0x01: IPv4 address → Shows IP
- Type 0x03: Domain name → Shows domain
- Type 0x04: IPv6 address → Shows IP

**Note**: Most clients default to local DNS resolution. To see domain names in the dashboard, clients must use the SOCKS5 domain name feature (type 0x03).

### Connection Lifecycle

```
Client connects → Track() → conn added to connections map
                                        |
                                        v
                            SSE "created" event broadcast
                                        |
                                        v
                    UpdateBytes() called periodically (via StatsConn)
                                        |
                                        v
Client disconnect → Remove() → moved to closedConnections map
                                        |
                                        v
                            SSE "deleted" event broadcast
                                        |
                                        v
                    Cleanup after retentionDuration expires
```

### Memory Management

**Cleanup Strategy**:
1. **Periodic cleanup**: Runs every minute via `cleanupLoop()`
2. **Retention-based**: Removes connections older than `retentionDuration`
3. **Size-based**: Early cleanup if >10,000 closed connections (prevents memory pressure)
4. **Disabled mode**: If retention = 0, clears all closed connections immediately

**Thread Safety**:
- `sync.RWMutex` protects `connections` and `closedConnections` maps
- Separate `subMu` protects `subscribers` map
- Broadcast happens outside lock to prevent blocking

### Configuration

**ServerConfig extension** (pkg/config/v1/server.go):
```go
// ConnRetentionDuration specifies how long to keep closed connections
// in memory for dashboard display. Default is 10 minutes.
// Set to 0 to disable keeping closed connections.
ConnRetentionDuration int64 `json:"connRetentionDuration,omitempty"`
```

**Example frps.toml**:
```toml
[webServer]
addr = "127.0.0.1"
port = 7500
user = "admin"
password = "admin"
assetsDir = "./web/frps/dist"

# Connection retention (optional, defaults to 10min)
# connRetentionDuration = 600  # seconds, 0 = disabled
```

### Key Decisions

| Decision | Reason |
|----------|--------|
| In-memory storage only | Simplified design, no persistence needed for transient data |
| SSE for real-time updates | Efficient push-based updates vs polling |
| Retention runtime-only | Avoids config file complexity, restart resets to default |
| Separate active/closed tabs | Clear UX distinction between current and historical data |
| IPv6 bracket notation | Standard URL format for IPv6 addresses with ports |
| Show what client sent | Client controls DNS resolution (local vs proxy) |

### File Locations

```
server/socks5proxy/relay_conn_tracker.go  - Connection tracker implementation
server/api_router.go                       - API endpoints
server/service.go                          - Tracker creation & lifecycle
web/frps/src/views/Connections.vue        - Dashboard UI
web/frps/src/api/socks5relay.ts           - Frontend API functions
web/frps/src/types/socks5relay.ts         - TypeScript types
```

---

## Security Considerations

### SSRF Protection

| Risk | Mitigation |
|------|------------|
| Target address SSRF | Recommendation: frpc can configure `allowedDestinations` to limit accessible target ranges - **optional extension** |
| Internal service exposure | Recommendation: default deny private IPs (10.x, 192.168.x, 172.16-31.x, 127.x), enable via `allowPrivateIP` - **optional extension** |

> **Note**: SSRF protection is optional. Core design does not include `allowedDestinations` or `allowPrivateIP` fields.

### Authentication Security

| Risk | Mitigation |
|------|------------|
| Weak password | Recommend `authPassword` >= 8 chars, use strong password in production |
| Proxy auth exposure | `authPassword` transmitted in SOCKS5/HTTP proxy auth, recommend TLS |
| Global password | Same password for all groups — no per-group isolation beyond group name |

### Access Control

| Risk | Mitigation |
|------|------------|
| Unauthorized access | SOCKS5/HTTP auth failure rejects connection |
| Group cross-access | username bound to group, cannot access other groups' frpcs |

---

## Policy Clarifications

### Multi-Group Scenarios

| Question | Decision |
|----------|----------|
| Can one frpc register multiple groups? | **No**. Group is a client-level setting (`ClientCommonConfig.Group`), not per-proxy. One frpc = one group. Multiple groups require separate frpc instances. |
| Can one group have multiple frpcs? | **Yes**. This is the load balancing base case. |
| Can a request specify multiple groups? | **No**. username=single group name, request binds to one group. |

### OutboundProxy Failure Policy

| Scenario | Default Behavior |
|----------|----------|
| OutboundProxy connection failed | **Fail directly**, no fallback to direct. If direct is desired, don't configure OutboundProxy. |
| OutboundProxy auth failed | **Fail directly**, log error. |
| Target connection timeout | Default 10s timeout, close connection and log. This is a default constant, not configurable. |

> **Note**: Target connection timeout (10s) is a default constant at implementation time, not exposed in config.

---

## Error Handling Design

### frps Side Error Handling

| Scenario | Error Type | Handling |
|----------|------------|----------|
| SOCKS5 auth failed | username/password mismatch | Return SOCKS5 auth failure response (0x01), close connection |
| HTTP CONNECT auth failed | Proxy-Authorization invalid | Return 407 Proxy Authentication Required |
| Group empty | No available frpc | SOCKS5: return general failure (0x01); HTTP: return 502 Bad Gateway |
| Invalid target address | DstAddr empty or malformed | Reject request, return error response |
| frpc connection pool exhausted | GetWorkConn timeout | Return service unavailable error |
| Target frpc offline | Control not found | Re-select from group, if still fails return error |

### frpc Side Error Handling

| Scenario | Error Type | Handling |
|----------|------------|----------|
| Target address parse failed | DstAddr/DstPort invalid | Close workConn, log error |
| Target connection timeout | Dial timeout (10s) | Close workConn, log error |
| OutboundProxy connection failed | Proxy unreachable | Fail directly, no fallback to direct |
| OutboundProxy auth failed | Proxy auth error | Log error, close workConn |

---

## Module Dependency Overview

```
+----------------------------------------------------------------------+
|                    Complete System Architecture                        |
|                                                                      |
|  +--------------------------------------------------------------+   |
|  |                    frps (Server)                              |   |
|  |                                                               |   |
|  |  Module 1 ------------------------------------------------   |   |
|  |  SOCKS5Handler / HTTPConnectHandler (server components)       |   |
|  |      |                                                        |   |
|  |      v                                                        |   |
|  |  Module 3 ------------------------------------------------   |   |
|  |  SessionManager + Socks5RelayGroupRegistry                    |   |
|  |      |                                                        |   |
|  |      v                                                        |   |
|  |  ResourceController + ControlManager (existing)               |   |
|  |                                                               |   |
|  |  Module 4 ------------------------------------------------   |   |
|  |  Configuration (ServerConfig fields + Socks5RelayProxyConfig) |   |
|  |                                                               |   |
|  |  Module 5 ------------------------------------------------   |   |
|  |  StartWorkConn message (DstAddr/DstPort for target address)   |   |
|  |                                                               |   |
|  +--------------------------------------------------------------+   |
|                              |                                       |
|                              v message passing                       |
|  +--------------------------------------------------------------+   |
|  |                    frpc (Client)                              |   |
|  |                                                               |   |
|  |  Module 2 ------------------------------------------------   |   |
|  |  Socks5RelayProxy + OutboundDialer                            |   |
|  |                                                               |   |
|  |  Module 4 ------------------------------------------------   |   |
|  |  Configuration Extension (Socks5RelayProxyConfig)             |   |
|  |                                                               |   |
|  |  Module 6 ------------------------------------------------   |   |
|  |  UpstreamProxyDialer (optional, **already exists in FRP**)    |   |
|  |                                                               |   |
|  +--------------------------------------------------------------+   |
|                                                                      |
+----------------------------------------------------------------------+

Module dependency (implementation order):

  Module 4 (config) --------+---------------------------------------> Module 2 (frpc proxy)
                              |                                        needs Socks5RelayProxyConfig
                              v
  Module 3 (session) ---------------------------> Module 1 (server components)
                              needs Registry/Manager               depends on SessionManager

  Module 5 (message verify) -- no changes, verify DstAddr/DstPort available

  Module 1 + Module 2 + Module 3 ---------> complete functionality

  Module 6 (upstream proxy) -> **already exists in FRP, no implementation needed**

  Module 7 (dashboard) --------> optional monitoring layer
                              depends on Module 1 (for tracking integration)
```

---

## Testing Strategy

### Unit Tests

| Module | Test Scope |
|--------|------------|
| Module 1 | SOCKS5/HTTP CONNECT protocol parsing, auth validation, connection bridging |
| Module 2 | Target address parsing, OutboundDialer.Dial, InWorkConn flow |
| Module 3 | Registry.Register/Unregister, SessionManager.SelectFrpc, Round-Robin logic |
| Module 4 | Config parsing (ServerConfig fields, Socks5RelayProxyConfig), MarshalToMsg/UnmarshalFromMsg |

### E2E Tests

| Scenario | Test Content |
|----------|-------------|
| Basic functionality | Single frpc + single group, SOCKS5 request succeeds |
| Session affinity | Same username across multiple requests routes to same frpc |
| frpc offline | frpc goes offline, re-selects another frpc in group |
| Multi-frpc load balancing | Multiple frpcs in same group, Round-Robin distribution |
| Empty group | Request fails when group has no frpc |
| HTTP CONNECT | HTTP proxy request succeeds |
| OutboundProxy | frpc outbound through proxy to target |

### Performance Tests

| Scenario | Metrics |
|----------|---------|
| High concurrency | SessionManager concurrent performance, no lock contention |
| Large number of frpc registrations | Registry performance, memory usage |
| Long connection persistence | Session binding stability, memory leak detection |

---

## Configuration Examples

### frps Configuration

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

### frpc Configuration

```toml
serverAddr = "frps.example.com"
serverPort = 7000

# Group is a client-level setting, NOT per-proxy
# One frpc = one group
group = "groupA"

# Optional: TLS cert OU overrides the group field
# [transport.tls]
# certFile = "client.crt"  # OU field will override group

# Optional: connect to frps through proxy (existing FRP feature)
# transport.proxyURL = "socks5://accelerate-proxy:1080"

[[proxies]]
name = "relay-groupA"
type = "socks5_relay"

# Optional: outbound through proxy
# outboundProxy = "socks5://local-proxy:1080"

# Optional: limit concurrent outbound connections
# maxConcurrent = 50
```

### Client Usage

```bash
# SOCKS5 proxy
curl --socks5 groupA:pass@frps.example.com:1080 https://target.com/api

# HTTP CONNECT proxy
curl --proxy http://groupA:pass@frps.example.com:8080 https://target.com/api
```

---

## Implementation Sequence

Recommended order:

1. Module 4: Configuration Extension
   - Add ServerConfig fields (socks5ProxyPort, httpConnectProxyPort, etc.)
   - Add Socks5RelayProxyConfig to pkg/config/v1/proxy.go
   - Add ProxyTypeSocks5Relay constant and proxyConfigTypeMap entry
2. Module 5: Message Extension (verify DstAddr/DstPort, no code changes)
3. Module 3: Session Manager & Group Registry
   - Socks5RelayGroupRegistry
   - SessionManager
   - ResourceController extension (add reference fields)
   - Wire ControlManager reference in NewService()
4. Module 1: frps Server Components
   - SOCKS5Handler
   - HTTPConnectHandler
   - Wire in NewService() based on ServerConfig
5. Module 2: frpc Proxy Type
   - Server-side: Socks5RelayServerProxy (server/proxy/socks5_relay.go) - registers with Socks5RelayGroupRegistry
   - Client-side: Socks5RelayProxy (client/proxy/socks5_relay.go) - handles InWorkConn
   - Register both server and client proxy factories
   - OutboundDialer
6. ~~Module 6: frpc Upstream Proxy~~ (**already exists in FRP, no implementation needed**)
7. Module 7: Dashboard & Monitoring (optional)
   - RelayConnTracker implementation
   - API endpoints for connections/stats/retention
   - Dashboard UI (Connections view)
   - SSE events for real-time updates
8. Tests

---

## Comparison with Existing FRP

| Aspect | Existing FRP Proxy | This Extension |
|--------|--------------------|----------------|
| frps listens | External user requests (TCP/UDP/HTTP) | Proxy requests (SOCKS5/HTTP CONNECT) |
| Configuration method | frpc registers via NewProxy message | Server-level config in frps.toml |
| frpc processing | Forward to fixed localIP:localPort | **Dynamic outbound**, target from message |
| Traffic endpoint | Internal local service | External target |
| Group mechanism | Proxy-level grouping (TCPGroupCtl) | frpc(runID)-level grouping (Socks5RelayGroupRegistry) |
| Listener lifecycle | Bound to frpc Control lifecycle | Independent, server-level (NewService) |
