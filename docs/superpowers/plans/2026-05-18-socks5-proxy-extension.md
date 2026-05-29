# SOCKS5 Proxy Extension Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add SOCKS5/HTTP CONNECT reverse proxy capability to FRP, enabling internal-network frpc instances to provide outbound proxy services through public frps.

**Architecture:** Server-level SOCKS5 and HTTP CONNECT listeners on frps accept external proxy requests, authenticate via username (=group), route to a registered frpc via session affinity, and bridge traffic through the existing tunnel. frpc registers as a `socks5_relay` proxy type, receives target addresses via `StartWorkConn.DstAddr/DstPort`, and dials outbound (directly or via optional proxy).

**Tech Stack:** Go 1.x, existing FRP framework (msg, config/v1, server/proxy, client/proxy packages), `github.com/armon/go-socks5` (already vendored)

**Design spec:** `doc/socks5_proxy_extension/design.md`

---

## File Structure

| Action | Path | Responsibility |
|--------|------|----------------|
| Modify | `pkg/config/v1/proxy.go` | Add `ProxyTypeSocks5Relay`, `Socks5RelayProxyConfig` struct + interface methods |
| Modify | `pkg/config/v1/server.go` | Add `Socks5ProxyPort`, `Socks5ProxyAuthPassword`, `HTTPConnectProxyPort`, `HTTPConnectProxyAuthPassword` fields |
| Create | `server/socks5_group_registry.go` | `Socks5RelayGroupRegistry` — tracks group→runID mappings (in `server` package to avoid import cycles) |
| Create | `server/session_manager.go` | `SessionManager` — username→frpc session binding + round-robin selection (in `server` package) |
| Modify | `server/controller/resource.go` | Add `Socks5GroupRegistry`, `Socks5SessionManager` interface fields to ResourceController |
| Create | `server/socks5proxy/handler.go` | `SOCKS5Handler` — SOCKS5 protocol listener + auth + bridge |
| Create | `server/socks5proxy/http_connect.go` | `HTTPConnectHandler` — HTTP CONNECT protocol listener + auth + bridge |
| Create | `server/proxy/socks5_relay.go` | `Socks5RelayServerProxy` — server-side proxy type (group register/unregister lifecycle) |
| Create | `client/proxy/socks5_relay.go` | `Socks5RelayProxy` — client-side proxy type (InWorkConn: parse target, dial outbound, bridge) |
| Modify | `server/service.go` | Wire new components in `NewService()`, `Run()`, `Close()` |

---

### Task 1: Configuration Extension

**Files:**
- Modify: `pkg/config/v1/proxy.go` (add `ProxyTypeSocks5Relay` constant, `Socks5RelayProxyConfig` struct)
- Modify: `pkg/config/v1/server.go` (add 4 new ServerConfig fields)

- [ ] **Step 1: Add ProxyTypeSocks5Relay constant and Socks5RelayProxyConfig to proxy.go**

In `pkg/config/v1/proxy.go`, add after the `ProxyTypeSUDP` constant (line 238):

```go
ProxyTypeSocks5Relay ProxyType = "socks5_relay"
```

Add to `proxyConfigTypeMap` (after line 249):

```go
ProxyTypeSocks5Relay: reflect.TypeFor[Socks5RelayProxyConfig](),
```

Add the new config struct after `SUDPProxyConfig` (after line 534):

```go
var _ ProxyConfigurer = &Socks5RelayProxyConfig{}

type Socks5RelayProxyConfig struct {
	ProxyBaseConfig

	Group         string `json:"group,omitempty"`
	GroupKey      string `json:"groupKey,omitempty"`
	OutboundProxy string `json:"outboundProxy,omitempty"`
}

func (c *Socks5RelayProxyConfig) MarshalToMsg(m *msg.NewProxy) {
	c.ProxyBaseConfig.MarshalToMsg(m)

	m.Group = c.Group
	m.GroupKey = c.GroupKey
	if c.OutboundProxy != "" {
		if m.Metas == nil {
			m.Metas = make(map[string]string)
		}
		m.Metas["outboundProxy"] = c.OutboundProxy
	}
}

func (c *Socks5RelayProxyConfig) UnmarshalFromMsg(m *msg.NewProxy) {
	c.ProxyBaseConfig.UnmarshalFromMsg(m)

	c.Group = m.Group
	c.GroupKey = m.GroupKey
	if m.Metas != nil {
		c.OutboundProxy = m.Metas["outboundProxy"]
	}
}

func (c *Socks5RelayProxyConfig) Clone() ProxyConfigurer {
	out := *c
	out.ProxyBaseConfig = c.ProxyBaseConfig.Clone()
	return &out
}
```

- [ ] **Step 2: Add ServerConfig fields to server.go**

In `pkg/config/v1/server.go`, find the `ServerConfig` struct. Add after the existing `TCPMuxPassthrough` field:

```go
// SOCKS5 proxy listener port, 0 means disabled
Socks5ProxyPort int `json:"socks5ProxyPort,omitempty"`
// SOCKS5 proxy authentication password
Socks5ProxyAuthPassword string `json:"socks5ProxyAuthPassword,omitempty"`

// HTTP CONNECT proxy listener port, 0 means disabled
HTTPConnectProxyPort int `json:"httpConnectProxyPort,omitempty"`
// HTTP CONNECT proxy authentication password
HTTPConnectProxyAuthPassword string `json:"httpConnectProxyAuthPassword,omitempty"`
```

- [ ] **Step 3: Run `make build` to verify compilation**

Run: `make build`
Expected: builds without errors

- [ ] **Step 4: Run existing tests to verify no regression**

Run: `make test`
Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add pkg/config/v1/proxy.go pkg/config/v1/server.go
git commit -m "feat(config): add Socks5RelayProxyConfig and server-level proxy port config fields"
```

---

### Task 2: Socks5RelayGroupRegistry

**Files:**
- Create: `server/socks5_group_registry.go`

> **Package note:** Both `Socks5RelayGroupRegistry` and `SessionManager` live in the `server` package (same package as `ControlManager` and `Control`). This avoids circular imports since they reference `ControlManager`/`Control` types.

- [ ] **Step 1: Implement Socks5RelayGroupRegistry**

Create `server/socks5_group_registry.go`:

```go
// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package server

import "sync"

// Socks5RelayGroupRegistry tracks which frpc instances (by runID) belong to which groups.
type Socks5RelayGroupRegistry struct {
	groups      map[string]map[string]struct{} // group -> set of runIDs
	runIDGroups map[string]map[string]struct{} // runID -> set of groups (reverse index)
	mu          sync.RWMutex
}

func NewSocks5RelayGroupRegistry() *Socks5RelayGroupRegistry {
	return &Socks5RelayGroupRegistry{
		groups:      make(map[string]map[string]struct{}),
		runIDGroups: make(map[string]map[string]struct{}),
	}
}

func (r *Socks5RelayGroupRegistry) Register(group, runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.groups[group]; !ok {
		r.groups[group] = make(map[string]struct{})
	}
	r.groups[group][runID] = struct{}{}

	if _, ok := r.runIDGroups[runID]; !ok {
		r.runIDGroups[runID] = make(map[string]struct{})
	}
	r.runIDGroups[runID][group] = struct{}{}
}

func (r *Socks5RelayGroupRegistry) Unregister(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	groups, ok := r.runIDGroups[runID]
	if !ok {
		return
	}
	for group := range groups {
		if members, ok := r.groups[group]; ok {
			delete(members, runID)
			if len(members) == 0 {
				delete(r.groups, group)
			}
		}
	}
	delete(r.runIDGroups, runID)
}

func (r *Socks5RelayGroupRegistry) GetGroupMembers(group string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	members, ok := r.groups[group]
	if !ok {
		return nil
	}
	result := make([]string, 0, len(members))
	for runID := range members {
		result = append(result, runID)
	}
	return result
}
```

- [ ] **Step 2: Run `make build` to verify compilation**

Run: `make build`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add server/socks5_group_registry.go
git commit -m "feat(server): add Socks5RelayGroupRegistry for group→runID tracking"
```

---

### Task 3: SessionManager

**Files:**
- Create: `server/session_manager.go`

- [ ] **Step 1: Implement SessionManager**

Create `server/session_manager.go`:

```go
// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package server

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// SessionManager manages username→frpc session binding with round-robin selection.
type SessionManager struct {
	sessions      map[string]string         // username -> runID binding
	groupIndex    map[string]*atomic.Uint64 // group -> round-robin counter
	groupRegistry *Socks5RelayGroupRegistry
	ctlManager    *ControlManager
	mu            sync.RWMutex
}

func NewSessionManager(groupRegistry *Socks5RelayGroupRegistry, ctlManager *ControlManager) *SessionManager {
	return &SessionManager{
		sessions:      make(map[string]string),
		groupIndex:    make(map[string]*atomic.Uint64),
		groupRegistry: groupRegistry,
		ctlManager:    ctlManager,
	}
}

// SelectFrpc selects a frpc Control for the given username (= group name).
// Uses session affinity: if the username was previously bound to a live frpc, return it.
// Otherwise, select a new frpc via round-robin from the group.
func (sm *SessionManager) SelectFrpc(username string) (*Control, error) {
	sm.mu.RLock()
	boundRunID, hasBinding := sm.sessions[username]
	sm.mu.RUnlock()

	if hasBinding {
		ctl, ok := sm.ctlManager.GetByID(boundRunID)
		if ok {
			return ctl, nil
		}
	}

	members := sm.groupRegistry.GetGroupMembers(username)
	if len(members) == 0 {
		return nil, fmt.Errorf("no available frpc in group [%s]", username)
	}

	sm.mu.Lock()
	counter, ok := sm.groupIndex[username]
	if !ok {
		counter = &atomic.Uint64{}
		sm.groupIndex[username] = counter
	}
	sm.mu.Unlock()

	idx := counter.Add(1) - 1
	runID := members[idx%uint64(len(members))]

	ctl, ok := sm.ctlManager.GetByID(runID)
	if !ok {
		return nil, fmt.Errorf("selected frpc [%s] is not available", runID)
	}

	sm.mu.Lock()
	sm.sessions[username] = runID
	sm.mu.Unlock()

	return ctl, nil
}

// RemoveSession removes all session bindings for a given runID (called on frpc disconnect).
func (sm *SessionManager) RemoveSession(runID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for username, boundRunID := range sm.sessions {
		if boundRunID == runID {
			delete(sm.sessions, username)
		}
	}
}
```

- [ ] **Step 2: Run `make build` to verify compilation**

Run: `make build`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add server/session_manager.go
git commit -m "feat(server): add SessionManager for socks5_relay session affinity"
```

---

### Task 4: ResourceController Extension

**Files:**
- Modify: `server/controller/resource.go` (add interface types and fields)

> **Why interfaces?** `Socks5RelayGroupRegistry` and `SessionManager` live in the `server` package (to reference `ControlManager`/`Control`). `server/proxy` imports `server/controller`, so we add interface fields to `ResourceController` that the `server` package satisfies at wiring time. No circular imports.

- [ ] **Step 1: Add interfaces and fields to ResourceController**

Modify `server/controller/resource.go`. Add two interface definitions after the `ResourceController` struct, and add two new fields to the struct:

```go
type ResourceController struct {
	// ... existing fields ...

	// Socks5 relay group registry (interface satisfied by server.Socks5RelayGroupRegistry)
	Socks5RelayGroupRegistry Socks5GroupRegistry
	// Socks5 relay session manager (interface satisfied by server.SessionManager)
	Socks5SessionManager Socks5SessionManager
}

// Socks5GroupRegistry manages socks5_relay group → frpc runID mappings.
type Socks5GroupRegistry interface {
	Register(group, runID string)
	Unregister(runID string)
	GetGroupMembers(group string) []string
}

// Socks5SessionManager manages socks5_relay session cleanup.
// SelectFrpc is NOT in this interface — only the handlers call it, via the selectFrpcFn closure
// from Service (which accesses SessionManager directly since they're in the same package).
type Socks5SessionManager interface {
	RemoveSession(runID string)
}
```

- [ ] **Step 2: Run `make build`**

Run: `make build`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add server/controller/resource.go
git commit -m "feat(server): add Socks5GroupRegistry/Socks5SessionManager interfaces to ResourceController"
```

---

### Task 5: Server-Side Socks5RelayServerProxy

**Files:**
- Create: `server/proxy/socks5_relay.go`

This proxy type's sole purpose is lifecycle management: register group membership on Run(), unregister on Close(). It does NOT listen on any port or handle user connections.

- [ ] **Step 1: Implement Socks5RelayServerProxy**

Create `server/proxy/socks5_relay.go`:

```go
// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package proxy

import (
	"reflect"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func init() {
	RegisterProxyFactory(reflect.TypeFor[*v1.Socks5RelayProxyConfig](), NewSocks5RelayServerProxy)
}

type Socks5RelayServerProxy struct {
	*BaseProxy
	cfg *v1.Socks5RelayProxyConfig
}

func NewSocks5RelayServerProxy(baseProxy *BaseProxy) Proxy {
	unwrapped, ok := baseProxy.GetConfigurer().(*v1.Socks5RelayProxyConfig)
	if !ok {
		return nil
	}
	return &Socks5RelayServerProxy{
		BaseProxy: baseProxy,
		cfg:       unwrapped,
	}
}

func (pxy *Socks5RelayServerProxy) Run() (remoteAddr string, err error) {
	rc := pxy.GetResourceController()
	runID := pxy.GetLoginMsg().RunID

	rc.Socks5RelayGroupRegistry.Register(pxy.cfg.Group, runID)
	pxy.xl.Infof("socks5_relay proxy registered group [%s] with runID [%s]", pxy.cfg.Group, runID)
	return "", nil
}

func (pxy *Socks5RelayServerProxy) Close() {
	rc := pxy.GetResourceController()
	runID := pxy.GetLoginMsg().RunID

	rc.Socks5RelayGroupRegistry.Unregister(runID)
	rc.Socks5SessionManager.RemoveSession(runID)
	pxy.xl.Infof("socks5_relay proxy unregistered runID [%s]", runID)
	pxy.BaseProxy.Close()
}
```

- [ ] **Step 2: Run `make build`**

Run: `make build`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add server/proxy/socks5_relay.go
git commit -m "feat(server): add Socks5RelayServerProxy for group lifecycle management"
```

---

### Task 6: Client-Side Socks5RelayProxy

**Files:**
- Create: `client/proxy/socks5_relay.go`

The client-side proxy receives `StartWorkConn` with `DstAddr:DstPort` as the target address, dials the target (directly or via OutboundProxy), and bridges the connection.

- [ ] **Step 1: Implement Socks5RelayProxy**

Create `client/proxy/socks5_relay.go`:

```go
// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package proxy

import (
	"fmt"
	"net"
	"reflect"
	"strconv"
	"time"

	libio "github.com/fatedier/golib/io"
	libnet "github.com/fatedier/golib/net"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
)

func init() {
	RegisterProxyFactory(reflect.TypeFor[*v1.Socks5RelayProxyConfig](), NewSocks5RelayProxy)
}

type Socks5RelayProxy struct {
	*BaseProxy
	cfg *v1.Socks5RelayProxyConfig
}

func NewSocks5RelayProxy(baseProxy *BaseProxy, cfg v1.ProxyConfigurer) Proxy {
	unwrapped, ok := cfg.(*v1.Socks5RelayProxyConfig)
	if !ok {
		return nil
	}
	return &Socks5RelayProxy{
		BaseProxy: baseProxy,
		cfg:       unwrapped,
	}
}

func (pxy *Socks5RelayProxy) Run() error {
	return nil
}

// InWorkConn handles work connections from frps.
// Note: deliberately does NOT call BaseProxy.HandleTCPWorkConnection, which applies
// per-proxy encryption/compression. The wire-level AEAD encryption (negotiated in the
// control channel) already protects tunnel traffic. socks5_relay targets are dynamic,
// so per-proxy encryption wrapping is not applicable.
func (pxy *Socks5RelayProxy) InWorkConn(conn net.Conn, m *msg.StartWorkConn) {
	xl := pxy.xl

	if m.DstAddr == "" || m.DstPort == 0 {
		xl.Errorf("missing target address in StartWorkConn message")
		conn.Close()
		return
	}

	targetAddr := net.JoinHostPort(m.DstAddr, strconv.Itoa(int(m.DstPort)))
	xl.Debugf("socks5_relay dialing target [%s]", targetAddr)

	var targetConn net.Conn
	var err error

	if pxy.cfg.OutboundProxy != "" {
		targetConn, err = pxy.dialViaProxy(targetAddr)
	} else {
		targetConn, err = libnet.Dial(targetAddr, libnet.WithTimeout(10*time.Second))
	}

	if err != nil {
		xl.Errorf("dial target [%s] error: %v", targetAddr, err)
		conn.Close()
		return
	}

	xl.Debugf("socks5_relay connected to target [%s], bridging", targetAddr)
	_, _, _ = libio.Join(conn, targetConn)
}

func (pxy *Socks5RelayProxy) Close() {
	// nothing to clean up
}

// dialViaProxy dials the target address through the configured outbound proxy.
// Uses the same libnet API that FRP uses for transport.proxyURL (see client/connector.go).
func (pxy *Socks5RelayProxy) dialViaProxy(targetAddr string) (net.Conn, error) {
	proxyType, addr, auth, err := libnet.ParseProxyURL(pxy.cfg.OutboundProxy)
	if err != nil {
		return nil, fmt.Errorf("parse outbound proxy URL error: %w", err)
	}

	return libnet.Dial(targetAddr,
		libnet.WithTimeout(10*time.Second),
		libnet.WithProxy(proxyType, addr),
		libnet.WithProxyAuth(auth),
	)
}
```

- [ ] **Step 2: Run `make build`**

Run: `make build`
Expected: builds without errors (may need adjustment based on available proxy dial utilities)

- [ ] **Step 3: Commit**

```bash
git add client/proxy/socks5_relay.go
git commit -m "feat(client): add Socks5RelayProxy with dynamic outbound dialing"
```

---

### Task 7: SOCKS5 Handler

**Files:**
- Create: `server/socks5proxy/handler.go`

- [ ] **Step 1: Implement SOCKS5Handler**

Create `server/socks5proxy/handler.go`:

```go
// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package socks5proxy

import (
	"context"
	"fmt"
	"io"
	"net"

	libio "github.com/fatedier/golib/io"

	"github.com/fatedier/frp/pkg/util/xlog"
)

// SOCKS5Handler is a server-level SOCKS5 proxy listener.
type SOCKS5Handler struct {
	listener     net.Listener
	authPassword string
	selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)
}

func NewSOCKS5Handler(listener net.Listener, authPassword string, selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)) *SOCKS5Handler {
	return &SOCKS5Handler{
		listener:     listener,
		authPassword: authPassword,
		selectFrpcFn: selectFrpcFn,
	}
}

func (h *SOCKS5Handler) Run(ctx context.Context) {
	xl := xlog.FromContextSafe(ctx)
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				xl.Warnf("socks5 listener accept error: %v", err)
				continue
			}
		}
		go h.handleConn(ctx, conn)
	}
}

func (h *SOCKS5Handler) Close() error {
	if h.listener != nil {
		return h.listener.Close()
	}
	return nil
}

func (h *SOCKS5Handler) handleConn(ctx context.Context, clientConn net.Conn) {
	xl := xlog.FromContextSafe(ctx)
	defer clientConn.Close()

	// SOCKS5 handshake
	if err := h.socks5Handshake(clientConn); err != nil {
		xl.Debugf("socks5 handshake error: %v", err)
		return
	}

	// SOCKS5 auth (username = group, password = authPassword)
	username, err := h.socks5Auth(clientConn)
	if err != nil {
		xl.Debugf("socks5 auth error: %v", err)
		return
	}

	// SOCKS5 connect request - get target address
	dstAddr, dstPort, err := h.socks5ConnectRequest(clientConn)
	if err != nil {
		xl.Debugf("socks5 connect request error: %v", err)
		return
	}

	// Select frpc and get work connection with target address
	workConn, err := h.selectFrpcFn(username, dstAddr, dstPort)
	if err != nil {
		xl.Warnf("select frpc for group [%s] error: %v", username, err)
		h.sendSOCKS5Reply(clientConn, 0x01) // general SOCKS server failure
		return
	}
	defer workConn.Close()

	// Send success reply to client
	h.sendSOCKS5Reply(clientConn, 0x00)

	// Bridge traffic
	_, _, _ = libio.Join(clientConn, workConn)
}

// socks5Handshake performs the initial SOCKS5 greeting.
func (h *SOCKS5Handler) socks5Handshake(conn net.Conn) error {
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("read version/method selection: %w", err)
	}
	if buf[0] != 0x05 {
		return fmt.Errorf("not SOCKS5 protocol")
	}
	nMethods := int(buf[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("read methods: %w", err)
	}

	// Check if username/password auth (0x02) is offered
	hasUserPassAuth := false
	for _, m := range methods {
		if m == 0x02 {
			hasUserPassAuth = true
			break
		}
	}

	if !hasUserPassAuth {
		// No acceptable methods
		conn.Write([]byte{0x05, 0xFF})
		return fmt.Errorf("client does not support username/password auth")
	}

	// Select username/password auth method
	conn.Write([]byte{0x05, 0x02})
	return nil
}

// socks5Auth performs SOCKS5 username/password authentication.
// Returns the username (which equals the group name).
func (h *SOCKS5Handler) socks5Auth(conn net.Conn) (string, error) {
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", fmt.Errorf("read auth version: %w", err)
	}
	// buf[0] = sub-negotiation version (0x01)
	ulen := int(buf[1])
	username := make([]byte, ulen)
	if _, err := io.ReadFull(conn, username); err != nil {
		return "", fmt.Errorf("read username: %w", err)
	}

	buf2 := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf2); err != nil {
		return "", fmt.Errorf("read password length: %w", err)
	}
	plen := int(buf2[0])
	password := make([]byte, plen)
	if _, err := io.ReadFull(conn, password); err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	if string(password) != h.authPassword {
		conn.Write([]byte{0x01, 0x01}) // auth failure
		return "", fmt.Errorf("auth failed for user [%s]", string(username))
	}

	conn.Write([]byte{0x01, 0x00}) // auth success
	return string(username), nil
}

// socks5ConnectRequest reads the SOCKS5 connect request and returns target address.
func (h *SOCKS5Handler) socks5ConnectRequest(conn net.Conn) (string, uint16, error) {
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", 0, fmt.Errorf("read connect request header: %w", err)
	}
	// buf[0] = version (0x05), buf[1] = cmd, buf[2] = reserved
	if buf[1] != 0x01 { // only CONNECT command supported
		return "", 0, fmt.Errorf("unsupported SOCKS5 command: %d", buf[1])
	}

	addrType := buf[3]
	var dstAddr string

	switch addrType {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", 0, fmt.Errorf("read IPv4 address: %w", err)
		}
		dstAddr = net.IP(ip).String()
	case 0x03: // Domain name
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", 0, fmt.Errorf("read domain length: %w", err)
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", 0, fmt.Errorf("read domain name: %w", err)
		}
		dstAddr = string(domain)
	case 0x04: // IPv6
		ip := make([]byte, 16)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", 0, fmt.Errorf("read IPv6 address: %w", err)
		}
		dstAddr = net.IP(ip).String()
	default:
		return "", 0, fmt.Errorf("unsupported address type: %d", addrType)
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", 0, fmt.Errorf("read port: %w", err)
	}
	dstPort := uint16(portBuf[0])<<8 | uint16(portBuf[1])

	return dstAddr, dstPort, nil
}

// sendSOCKS5Reply sends a SOCKS5 reply to the client.
func (h *SOCKS5Handler) sendSOCKS5Reply(conn net.Conn, rep byte) {
	// Version(0x05) | Reply | Reserved(0x00) | AddrType(0x01=IPv4) | 0.0.0.0 | Port(0)
	reply := []byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	conn.Write(reply)
}
```

**Note on `selectFrpcFn`:** This is a function closure provided by the Service layer (see Task 9). It encapsulates: `SessionManager.SelectFrpc(username)` → `ctl.GetWorkConn()` → `workConn.Start(&msg.StartWorkConn{DstAddr, DstPort})` → returns `net.Conn`. The handler does not need to know about `WorkConn`, `StartWorkConn`, or `ControlManager`. Signature:

```go
selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)
```

- [ ] **Step 2: Run `make build`**

Run: `make build`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add server/socks5proxy/handler.go
git commit -m "feat(server): add SOCKS5Handler with auth, connect request parsing, and traffic bridging"
```

---

### Task 8: HTTP CONNECT Handler

**Files:**
- Create: `server/socks5proxy/http_connect.go`

- [ ] **Step 1: Implement HTTPConnectHandler**

Create `server/socks5proxy/http_connect.go`:

```go
// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package socks5proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"

	libio "github.com/fatedier/golib/io"

	"github.com/fatedier/frp/pkg/util/xlog"
)

// HTTPConnectHandler is a server-level HTTP CONNECT proxy listener.
type HTTPConnectHandler struct {
	listener     net.Listener
	authPassword string
	selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)
}

func NewHTTPConnectHandler(listener net.Listener, authPassword string, selectFrpcFn func(username string, dstAddr string, dstPort uint16) (net.Conn, error)) *HTTPConnectHandler {
	return &HTTPConnectHandler{
		listener:     listener,
		authPassword: authPassword,
		selectFrpcFn: selectFrpcFn,
	}
}

func (h *HTTPConnectHandler) Run(ctx context.Context) {
	xl := xlog.FromContextSafe(ctx)
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				xl.Warnf("http connect listener accept error: %v", err)
				continue
			}
		}
		go h.handleConn(ctx, conn)
	}
}

func (h *HTTPConnectHandler) Close() error {
	if h.listener != nil {
		return h.listener.Close()
	}
	return nil
}

func (h *HTTPConnectHandler) handleConn(ctx context.Context, conn net.Conn) {
	xl := xlog.FromContextSafe(ctx)
	defer conn.Close()

	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		xl.Debugf("read http request error: %v", err)
		return
	}

	if req.Method != http.MethodConnect {
		http.Error(newRespWriter(conn), "only CONNECT method supported", http.StatusMethodNotAllowed)
		return
	}

	// Extract username from Proxy-Authorization header
	username, err := h.extractUsername(req)
	if err != nil {
		resp := &http.Response{
			StatusCode: http.StatusProxyAuthRequired,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     http.Header{"Proxy-Authenticate": {"Basic"}},
		}
		resp.Write(conn)
		return
	}

	// Parse target host:port
	host, portStr, err := net.SplitHostPort(req.Host)
	if err != nil {
		http.Error(newRespWriter(conn), "invalid host", http.StatusBadRequest)
		return
	}
	var port uint16
	fmt.Sscanf(portStr, "%d", &port)

	// Select frpc and get work connection
	workConn, err := h.selectFrpcFn(username, host, port)
	if err != nil {
		xl.Warnf("select frpc for group [%s] error: %v", username, err)
		http.Error(newRespWriter(conn), "bad gateway", http.StatusBadGateway)
		return
	}
	defer workConn.Close()

	// Send 200 Connection Established
	resp := &http.Response{
		StatusCode: http.StatusOK,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Body:       http.NoBody,
	}
	resp.Write(conn)

	// Bridge traffic
	_, _, _ = libio.Join(conn, workConn)
}

func (h *HTTPConnectHandler) extractUsername(req *http.Request) (string, error) {
	authHeader := req.Header.Get("Proxy-Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("no proxy authorization header")
	}

	const prefix = "Basic "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", fmt.Errorf("invalid auth scheme")
	}

	decoded, err := base64.StdEncoding.DecodeString(authHeader[len(prefix):])
	if err != nil {
		return "", fmt.Errorf("decode auth error: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid auth format")
	}

	if parts[1] != h.authPassword {
		return "", fmt.Errorf("auth failed")
	}

	return parts[0], nil
}

// respWriter wraps a net.Conn to implement http.ResponseWriter for error responses.
type respWriter struct {
	conn   net.Conn
	header http.Header
	wrote  bool
}

func newRespWriter(conn net.Conn) *respWriter {
	return &respWriter{conn: conn, header: make(http.Header)}
}

func (w *respWriter) Header() http.Header        { return w.header }
func (w *respWriter) Write(b []byte) (int, error) { return w.conn.Write(b) }
func (w *respWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	statusText := http.StatusText(code)
	fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n", code, statusText)
}
```

- [ ] **Step 2: Run `make build`**

Run: `make build`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add server/socks5proxy/http_connect.go
git commit -m "feat(server): add HTTPConnectHandler with Basic auth and traffic bridging"
```

---

### Task 9: Service Wiring

**Files:**
- Modify: `server/service.go` (add fields, init in NewService, start in Run, close in Close)

- [ ] **Step 1: Add fields to Service struct**

In `server/service.go`, find the `Service` struct. Add after the `sshTunnelGateway` field:

```go
// SOCKS5/HTTP CONNECT proxy handlers
socks5Handler      *socks5proxy.SOCKS5Handler
httpConnectHandler *socks5proxy.HTTPConnectHandler

// SOCKS5 relay session components
groupRegistry  *Socks5RelayGroupRegistry
sessionManager *SessionManager
```

Add import for the new package:

```go
"github.com/fatedier/frp/server/socks5proxy"
```

- [ ] **Step 2: Initialize components in NewService()**

In `NewService()`, after the existing group controller initialization (`svr.rc.TCPMuxGroupCtl = ...`), add:

```go
	// Initialize SOCKS5 relay components
	svr.groupRegistry = NewSocks5RelayGroupRegistry()
	svr.sessionManager = NewSessionManager(svr.groupRegistry, svr.ctlManager)
	svr.rc.Socks5RelayGroupRegistry = svr.groupRegistry
	svr.rc.Socks5SessionManager = svr.sessionManager // interface only has RemoveSession
```

After the SSH tunnel gateway setup block, add:

```go
	// SOCKS5 proxy listener
	if cfg.Socks5ProxyPort > 0 {
		address := net.JoinHostPort(cfg.ProxyBindAddr, strconv.Itoa(cfg.Socks5ProxyPort))
		l, err := net.Listen("tcp", address)
		if err != nil {
			return nil, fmt.Errorf("create socks5 proxy listener error: %v", err)
		}
		svr.socks5Handler = socks5proxy.NewSOCKS5Handler(l, cfg.Socks5ProxyAuthPassword, svr.makeSelectFrpcFn())
		log.Infof("socks5 proxy listen on %s", address)
	}

	// HTTP CONNECT proxy listener
	if cfg.HTTPConnectProxyPort > 0 {
		address := net.JoinHostPort(cfg.ProxyBindAddr, strconv.Itoa(cfg.HTTPConnectProxyPort))
		l, err := net.Listen("tcp", address)
		if err != nil {
			return nil, fmt.Errorf("create http connect proxy listener error: %v", err)
		}
		svr.httpConnectHandler = socks5proxy.NewHTTPConnectHandler(l, cfg.HTTPConnectProxyAuthPassword, svr.makeSelectFrpcFn())
		log.Infof("http connect proxy listen on %s", address)
	}
```

Add the `makeSelectFrpcFn` helper method on Service:

```go
func (svr *Service) makeSelectFrpcFn() func(username string, dstAddr string, dstPort uint16) (net.Conn, error) {
	return func(username string, dstAddr string, dstPort uint16) (net.Conn, error) {
		ctl, err := svr.sessionManager.SelectFrpc(username)
		if err != nil {
			return nil, err
		}
		workConn, err := ctl.GetWorkConn()
		if err != nil {
			return nil, fmt.Errorf("get work connection error: %w", err)
		}
		conn, err := workConn.Start(&msg.StartWorkConn{
			DstAddr: dstAddr,
			DstPort: dstPort,
		})
		if err != nil {
			workConn.Close()
			return nil, fmt.Errorf("start work connection error: %w", err)
		}
		return conn, nil
	}
}
```

- [ ] **Step 3: Start handlers in Run()**

In `Service.Run()`, after the SSH tunnel gateway goroutine, add:

```go
	if svr.socks5Handler != nil {
		go svr.socks5Handler.Run(svr.ctx)
	}
	if svr.httpConnectHandler != nil {
		go svr.httpConnectHandler.Run(svr.ctx)
	}
```

- [ ] **Step 4: Close handlers in Close()**

In `Service.Close()`, after the sshTunnelGateway close, add:

```go
	if svr.socks5Handler != nil {
		svr.socks5Handler.Close()
	}
	if svr.httpConnectHandler != nil {
		svr.httpConnectHandler.Close()
	}
```

- [ ] **Step 5: Run `make build`**

Run: `make build`
Expected: builds without errors

- [ ] **Step 6: Run existing tests**

Run: `make test`
Expected: all tests pass

- [ ] **Step 7: Commit**

```bash
git add server/service.go
git commit -m "feat(server): wire SOCKS5/HTTP CONNECT proxy handlers into Service lifecycle"
```

---

### Task 10: End-to-End Test

**Files:**
- Create: `test/e2e/v1/features/socks5_relay.go`

- [ ] **Step 1: Write E2E test for basic SOCKS5 proxy flow**

Create `test/e2e/v1/features/socks5_relay.go`:

```go
// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");

package features

import (
	"fmt"
	"net"
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/fatedier/frp/test/e2e/framework"
	"github.com/fatedier/frp/test/e2e/framework/consts"
	"github.com/fatedier/frp/test/e2e/mock/streamserver"
	"github.com/fatedier/frp/test/e2e/pkg/request"
)

var _ = ginkgo.Describe("[Feature: Socks5Relay]", func() {
	f := framework.NewDefaultFramework()

	ginkgo.It("SOCKS5 proxy through frpc", func() {
		// Allocate ports
		socks5Port := f.AllocPort()
		httpConnectPort := f.AllocPort()
		tcpEchoPort := f.AllocPort()

		// Start a TCP echo server as the external target
		f.RunServer("", streamserver.New(streamserver.TCP, streamserver.WithBindPort(tcpEchoPort)))

		// Server config with SOCKS5 and HTTP CONNECT proxy ports
		serverConf := consts.DefaultServerConfig + fmt.Sprintf(`
socks5ProxyPort = %d
socks5ProxyAuthPassword = "testpass"
httpConnectProxyPort = %d
httpConnectProxyAuthPassword = "testpass"
`, socks5Port, httpConnectPort)

		// Client config with socks5_relay proxy
		clientConf := consts.DefaultClientConfig + fmt.Sprintf(`
[[proxies]]
name = "relay-test"
type = "socks5_relay"
`)

		f.RunProcesses(serverConf, []string{clientConf})

		// Wait for proxy to be ready
		time.Sleep(2 * time.Second)

		// Test SOCKS5 proxy: connect to echo server through frps
		framework.NewRequestExpect(f).
			RequestModify(func(r *request.Request) {
				r.Socks5Proxy(
					net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", socks5Port)),
					"testgroup", "testpass",
				).TCP().Addr(net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", tcpEchoPort)))
			}).
			ExpectResp([]byte("test")).
			Ensure()

		// Test HTTP CONNECT proxy
		framework.NewRequestExpect(f).
			RequestModify(func(r *request.Request) {
				r.HTTPProxy(
					fmt.Sprintf("http://testgroup:testpass@127.0.0.1:%d", httpConnectPort),
				).HTTP().Addr(net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", tcpEchoPort)))
			}).
			Ensure()
	})

	ginkgo.It("empty group returns error", func() {
		socks5Port := f.AllocPort()

		serverConf := consts.DefaultServerConfig + fmt.Sprintf(`
socks5ProxyPort = %d
socks5ProxyAuthPassword = "testpass"
`, socks5Port)

		// No client configured — group "nogroup" has no members
		f.RunProcesses(serverConf, []string{consts.DefaultClientConfig})

		time.Sleep(2 * time.Second)

		// SOCKS5 request should fail
		framework.NewRequestExpect(f).
			RequestModify(func(r *request.Request) {
				r.Socks5Proxy(
					net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", socks5Port)),
					"nogroup", "testpass",
				).TCP().Addr("127.0.0.1:80")
			}).
			ExpectError(true).
			Ensure()
	})
})
```

**Note:** The test uses `request.Request.Socks5Proxy()` and `request.Request.HTTPProxy()` methods. If these don't exist in the test framework yet, they need to be added to `test/e2e/pkg/request/request.go`. The framework already supports `TCP()` and `HTTP()` request types. The SOCKS5/HTTP proxy support can be added by configuring the underlying transport to route through the proxy.

If `Socks5Proxy()` and `HTTPProxy()` helpers don't exist, use Go's standard `net.Dial` with SOCKS5 handshake or `http.Transport` with proxy configured as a simpler alternative:

```go
// Simplified SOCKS5 test without framework helpers
func testSocks5Proxy(t *testing.T, proxyAddr, username, password, targetAddr string) {
	conn, err := net.Dial("tcp", proxyAddr)
	// ... perform SOCKS5 handshake + auth + CONNECT ...
	// ... send test data and verify response ...
}
```

- [ ] **Step 2: Run E2E tests**

Run: `make e2e`
Expected: all E2E tests pass (including the new SOCKS5 tests)

- [ ] **Step 3: Commit**

```bash
git add test/e2e/v1/features/socks5_relay.go
git commit -m "test(e2e): add SOCKS5/HTTP CONNECT relay proxy e2e tests"
```

---

## Implementation Order Summary

1. Task 1: Config (foundation - no dependencies)
2. Task 2: Group Registry (depends on nothing, needed by Tasks 3, 5, 9)
3. Task 3: Session Manager (depends on Task 2)
4. Task 4: ResourceController extension (depends on Task 3 concepts)
5. Task 5: Server-side proxy (depends on Tasks 1, 4)
6. Task 6: Client-side proxy (depends on Task 1)
7. Task 7: SOCKS5 Handler (depends on Task 4 concepts)
8. Task 8: HTTP CONNECT Handler (depends on Task 4 concepts)
9. Task 9: Service Wiring (depends on Tasks 2, 3, 7, 8)
10. Task 10: E2E Test (depends on all above)

Tasks 5+6 and 7+8 can be implemented in parallel since they're independent.
