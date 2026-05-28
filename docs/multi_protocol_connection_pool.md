# Multi-Protocol Connection Pool with Quality-Based Selection

## Context

frpc currently uses a single transport protocol to connect to frps. If that path degrades or fails, all services go offline until reconnection. This feature lets frpc establish connections via multiple protocols simultaneously (e.g., TCP + QUIC) to the same server, keep them all alive with heartbeats, and dynamically select the best one for traffic based on real-time network quality (RTT, jitter, packet loss).

## Architecture

```
frpc                                          frps
+---------+   TCP/yamux    +-----------+
| Pool[0] | ------------> | Control   | (active: proxies + heartbeat)
+---------+                | RunID-A   |
+---------+   QUIC         +-----------+
| Pool[1] | ------------> | Control   | (heartbeat only)
+---------+                | RunID-B   |
                           +-----------+

ConnPool monitors RTT on each connection.
When Pool[1] RTT < Pool[0] RTT by tolerance threshold:
  1. Pool[0] sends CloseProxy for all proxies
  2. Pool[1] sends NewProxy for all proxies  → becomes active
  3. Pool[0] switches to heartbeat-only
```

- Each pool connection is an independent `Control` with its own RunID
- Only the "active" connection registers proxies and handles work connections
- Others maintain heartbeat only, measuring network quality continuously
- No server-side changes required (each connection looks like a different client)

## Files Overview

| File | Change |
|------|--------|
| `pkg/config/v1/client.go` | Add `Protocols []string` to `ClientTransportConfig` + tolerance config |
| `client/conn_pool.go` | **New** - Connection pool manager with quality metrics and switching |
| `client/control.go` | Add `lastPingSent` for RTT measurement, add heartbeat-only mode, add `DeregisterAll()` / `RegisterAll()` |
| `client/service.go` | Replace single `ctl *Control` with `ConnPool` |
| `client/connector.go` | Support creating connectors with specific protocol override |

No server changes needed.

## Implementation Steps

### Step 1: Configuration (`pkg/config/v1/client.go`)

Add to `ClientTransportConfig`:

```go
// Protocols specifies a list of transport protocols to use simultaneously.
// frpc will establish one connection per protocol and select the best one
// for traffic based on network quality. Valid values: "tcp", "kcp", "quic", "websocket", "wss".
// When set, it overrides the single Protocol field.
Protocols []string `json:"protocols,omitempty"`

// SwitchTolerance is the minimum quality improvement ratio required to trigger
// a connection switch. For example, 0.2 means only switch if the new connection's
// RTT is at least 20% better than the current active. Default: 0.3
SwitchTolerance float64 `json:"switchTolerance,omitempty"`
```

In `Complete()`:
```go
// If Protocols is empty, derive from single Protocol for backward compatibility
if len(c.Protocols) == 0 && c.Protocol != "" {
    c.Protocols = []string{c.Protocol}
}
c.SwitchTolerance = util.EmptyOr(c.SwitchTolerance, 0.3)
```

Example config:
```toml
transport.protocols = ["tcp", "quic"]
transport.switchTolerance = 30
```

### Step 2: RTT Measurement in Control (`client/control.go`)

Add fields to `Control`:

```go
type Control struct {
    // ... existing fields ...

    // RTT measurement
    lastPingSent atomic.Value  // stores time.Time of last Ping send
    rttEWMA      atomic.Value  // stores time.Duration, exponentially weighted moving average
}

// GetRTT returns the current EWMA RTT.
func (ctl *Control) GetRTT() time.Duration {
    v := ctl.rttEWMA.Load()
    if v == nil {
        return 0
    }
    return v.(time.Duration)
}
```

Modify `heartbeatWorker()` to record Ping send time:

```go
sendHeartBeat := func() (bool, error) {
    pingMsg := &msg.Ping{}
    // ... existing auth ...
    ctl.lastPingSent.Store(time.Now())  // record send time
    _ = ctl.msgDispatcher.Send(pingMsg)
    return false, nil
}
```

Modify `handlePong()` to calculate RTT:

```go
func (ctl *Control) handlePong(m msg.Message) {
    // ... existing error check ...
    sentAt := ctl.lastPingSent.Load()
    if sentAt != nil {
        rtt := time.Since(sentAt.(time.Time))
        prev := ctl.rttEWMA.Load()
        if prev == nil {
            ctl.rttEWMA.Store(rtt)
        } else {
            // EWMA with weight 0.3 for new sample
            newRTT := time.Duration(0.3*float64(rtt) + 0.7*float64(prev.(time.Duration)))
            ctl.rttEWMA.Store(newRTT)
        }
    }
    ctl.lastPong.Store(time.Now())
}
```

Add `IsAlive()` method:

```go
func (ctl *Control) IsAlive() bool {
    select {
    case <-ctl.doneCh:
        return false
    default:
        return true
    }
}
```

### Step 3: Connector Protocol Override (`client/connector.go`)

Add a constructor that accepts a specific protocol:

```go
func NewConnectorWithProtocol(ctx context.Context, cfg *v1.ClientCommonConfig, protocol string) Connector {
    cfgCopy := *cfg
    cfgCopy.Transport.Protocol = protocol
    // For TCPMux, only enable when protocol is tcp (not quic/kcp)
    if protocol == "quic" {
        cfgCopy.Transport.TCPMux = lo.ToPtr(false)
    }
    return NewConnector(ctx, &cfgCopy)
}
```

### Step 4: Connection Pool (`client/conn_pool.go`) - NEW FILE

```go
package client

// ConnQuality holds measured network metrics for one connection.
type ConnQuality struct {
    RTT          time.Duration // EWMA round-trip time
    Jitter       time.Duration // RTT variance (standard deviation of recent samples)
    MissedBeats  int           // consecutive heartbeat misses
    LastPong     time.Time     // last successful pong
}

// Score returns a quality score (lower is better).
// Weighted combination of RTT, jitter, and missed beats.
func (q ConnQuality) Score() float64 {
    return float64(q.RTT) + 0.5*float64(q.Jitter) + float64(q.MissedBeats)*float64(500*time.Millisecond)
}

// poolEntry wraps one Control with its metadata.
type poolEntry struct {
    protocol string
    ctl      *Control
    session  *SessionContext
}

// ConnPool manages multiple control connections and selects the best one.
type ConnPool struct {
    ctx    context.Context
    xl     *xlog.Logger
    cancel context.CancelCauseFunc

    mu      sync.RWMutex
    entries []*poolEntry         // all connections in the pool
    activeIdx int                // index of the active connection (-1 if none)
    proxyCfgs   []v1.ProxyConfigurer
    visitorCfgs []v1.VisitorConfigurer

    // config
    common          *v1.ClientCommonConfig
    auth            *auth.ClientAuth
    clientSpec      *msg.ClientSpec
    vnetController  *vnet.Controller
    connectorCreator func(context.Context, *v1.ClientCommonConfig) Connector

    handleWorkConnCb func(*v1.ProxyBaseConfig, net.Conn, *msg.StartWorkConn) bool

    doneCh chan struct{}
}

func NewConnPool(ctx context.Context, ...) *ConnPool

// Run dials all protocols, starts monitoring, and blocks until context is cancelled.
func (p *ConnPool) Run()

// establishAll dials each protocol concurrently and adds successful ones to the pool.
func (p *ConnPool) establishAll()

// monitorAndSwitch runs the quality evaluation loop.
// Every evalInterval seconds, it:
//   1. Collects ConnQuality from each pool entry
//   2. Finds the best connection
//   3. If best is different from active AND improvement exceeds tolerance, switch
func (p *ConnPool) monitorAndSwitch()

// collectQuality reads current metrics from a Control.
func (p *ConnPool) collectQuality(entry *poolEntry) ConnQuality

// switchActiveTo moves proxy registration from old active to new.
func (p *ConnPool) switchActiveTo(newIdx int) error
//   1. old active: ctl.pm.CloseAll() → sends CloseProxy for every proxy
//   2. Wait briefly for server to process CloseProxy
//   3. new active: ctl.pm.UpdateAll(proxyCfgs) → sends NewProxy for every proxy
//   4. new active: ctl.vm.UpdateAll(visitorCfgs) → sends NewVisitorConn
//   5. Update activeIdx

// reconnectEntry re-establishes a failed connection.
func (p *ConnPool) reconnectEntry(idx int)

// Close / GracefulClose / Done / UpdateAllConfigurer - lifecycle methods
```

**Switching flow in detail:**

```go
func (p *ConnPool) switchActiveTo(newIdx int) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    oldIdx := p.activeIdx
    if oldIdx == newIdx {
        return nil
    }

    oldEntry := p.entries[oldIdx]
    newEntry := p.entries[newIdx]

    xl := p.xl
    xl.Infof("switching active connection: [%s] -> [%s] (RTT %v -> %v)",
        oldEntry.protocol, newEntry.protocol,
        oldEntry.ctl.GetRTT(), newEntry.ctl.GetRTT())

    // 1. Deregister all proxies on old active
    if oldEntry.ctl.IsAlive() {
        oldEntry.ctl.DeregisterAll()
    }

    // 2. Brief delay to let server process CloseProxy
    time.Sleep(200 * time.Millisecond)

    // 3. Register all proxies on new active
    newEntry.ctl.RegisterAll(p.proxyCfgs, p.visitorCfgs)

    p.activeIdx = newIdx
    return nil
}
```

**Quality evaluation loop:**

```go
func (p *ConnPool) monitorAndSwitch() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-p.ctx.Done():
            return
        case <-ticker.C:
            p.evaluateAndSwitch()
        case <-p.doneCh:
            return
        }
    }
}

func (p *ConnPool) evaluateAndSwitch() {
    p.mu.RLock()
    defer p.mu.RUnlock()

    if len(p.entries) < 2 || p.activeIdx < 0 {
        return
    }

    bestIdx := -1
    bestScore := math.MaxFloat64
    for i, entry := range p.entries {
        if !entry.ctl.IsAlive() {
            continue
        }
        q := p.collectQuality(entry)
        if s := q.Score(); s < bestScore {
            bestScore = s
            bestIdx = i
        }
    }

    if bestIdx < 0 || bestIdx == p.activeIdx {
        return
    }

    activeQ := p.collectQuality(p.entries[p.activeIdx])
    bestQ := p.collectQuality(p.entries[bestIdx])

    // Only switch if improvement exceeds tolerance
    improvement := (activeQ.Score() - bestQ.Score()) / activeQ.Score()
    if improvement > p.common.Transport.SwitchTolerance {
        // Switch in a separate goroutine to avoid blocking monitor
        go p.switchActiveTo(bestIdx)
    }
}
```

### Step 5: Control Helper Methods (`client/control.go`)

Add methods for pool switching:

```go
// DeregisterAll closes all proxies and visitors without closing the connection.
// Sends CloseProxy messages to server.
func (ctl *Control) DeregisterAll() {
    ctl.pm.CloseAll()    // sends CloseProxy for each proxy, stops listeners
    ctl.vm.CloseAll()    // closes visitors
}

// RegisterAll registers all proxies and visitors on this connection.
func (ctl *Control) RegisterAll(proxyCfgs []v1.ProxyConfigurer, visitorCfgs []v1.VisitorConfigurer) {
    ctl.pm.UpdateAll(proxyCfgs)
    ctl.vm.UpdateAll(visitorCfgs)
}
```

### Step 6: Service Integration (`client/service.go`)

Replace single `ctl` with `ConnPool`:

```go
type Service struct {
    ctlMu sync.RWMutex
    ctl   *ConnPool    // changed from *Control
    // ... rest unchanged ...
}
```

Modify `loopLoginUntilSuccess()`:
```go
loginFunc := func() (bool, error) {
    if len(svr.common.Transport.Protocols) > 1 {
        // Multi-protocol pool mode
        pool := NewConnPool(svr.ctx, ...)
        pool.SetInWorkConnCallback(svr.handleWorkConnCb)
        pool.Run()
        // replace previous
        svr.ctlMu.Lock()
        if svr.ctl != nil {
            svr.ctl.Close()
        }
        svr.ctl = pool
        svr.ctlMu.Unlock()
    } else {
        // Single-protocol mode (existing path, unchanged)
        // ... existing code ...
    }
    return true, nil
}
```

Modify `keepControllerWorking()` to watch `ConnPool.Done()` instead of `ctl.Done()`.

### Step 7: Handle Connection Failures in Pool

When a pool connection dies (heartbeat timeout):

```go
func (p *ConnPool) monitorEntry(idx int) {
    entry := p.entries[idx]
    <-entry.ctl.Done()

    p.mu.Lock()
    isOldActive := (idx == p.activeIdx)
    p.mu.Unlock()

    if isOldActive {
        // Active died - find best remaining or wait for one
        p.handleActiveFailure(idx)
    }

    // Try to reconnect this entry in background
    go p.reconnectEntry(idx)
}

func (p *ConnPool) handleActiveFailure(failedIdx int) {
    p.mu.Lock()
    defer p.mu.Unlock()

    // Find best alive connection
    bestIdx := -1
    bestScore := math.MaxFloat64
    for i, entry := range p.entries {
        if i == failedIdx || !entry.ctl.IsAlive() {
            continue
        }
        q := p.collectQuality(entry)
        if s := q.Score(); s < bestScore {
            bestScore = s
            bestIdx = i
        }
    }

    if bestIdx >= 0 {
        // Immediate failover (no tolerance check on failure)
        p.switchActiveTo(bestIdx)
    }
    // If no alive connections, reconnectAll will handle it
}
```

## Edge Cases

| Scenario | Handling |
|----------|----------|
| Only one protocol reaches server | Single-connection mode, same as current behavior |
| All connections die | Reconnect all with exponential backoff |
| Server doesn't listen on a protocol | Dial fails, that entry is skipped, retry periodically |
| Config reload | Update `proxyCfgs`/`visitorCfgs` in pool, active re-registers |
| Switch during active traffic | Brief gap (~200ms) while proxies re-register |
| New proxy added via API | `UpdateAllConfigurer` updates pool, active registers it |

## Verification

1. `make build` - compiles successfully
2. `make test` - existing tests pass
3. Manual test:
   - frps listening on TCP (7000) + QUIC (7001)
   - frpc with `transport.protocols = ["tcp", "quic"]`
   - Verify both connections established
   - Verify proxy works on the active connection
   - Simulate TCP degradation → switch to QUIC
   - Verify proxy still works after switch
