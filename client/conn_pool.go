// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"context"
	"math"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/fatedier/frp/client/proxy"
	"github.com/fatedier/frp/pkg/auth"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/util/util"
	"github.com/fatedier/frp/pkg/util/xlog"
	"github.com/fatedier/frp/pkg/vnet"
)

const (
	evalInterval      = 5 * time.Second
	switchDelay       = 200 * time.Millisecond
	reconnectInterval = 5 * time.Second
)

// ConnQuality holds measured network metrics for one pool connection.
type ConnQuality struct {
	RTT      time.Duration
	Jitter   time.Duration
	LastPong time.Time
}

// Score returns a quality score (lower is better).
func (q ConnQuality) Score() float64 {
	return float64(q.RTT) + 0.5*float64(q.Jitter)
}

// poolEntry wraps one Control with its protocol metadata.
type poolEntry struct {
	protocol string
	ctl      *Control
	session  *SessionContext
}

// ConnPool manages multiple control connections and dynamically selects the
// best one for proxy traffic based on network quality.
type ConnPool struct {
	ctx    context.Context
	xl     *xlog.Logger
	cancel context.CancelCauseFunc

	mu          sync.RWMutex
	entries     []*poolEntry
	activeIdx   int
	switching   bool // true while a proactive switch is in progress
	proxyCfgs   []v1.ProxyConfigurer
	visitorCfgs []v1.VisitorConfigurer
	poolID      string

	common           *v1.ClientCommonConfig
	auth             *auth.ClientAuth
	clientSpec       *msg.ClientSpec
	vnetController   *vnet.Controller
	connectorCreator func(context.Context, *v1.ClientCommonConfig) Connector

	handleWorkConnCb func(*v1.ProxyBaseConfig, net.Conn, *msg.StartWorkConn) bool

	doneCh    chan struct{}
	closeOnce sync.Once
}

func NewConnPool(
	common *v1.ClientCommonConfig,
	auth *auth.ClientAuth,
	clientSpec *msg.ClientSpec,
	vnetController *vnet.Controller,
	connectorCreator func(context.Context, *v1.ClientCommonConfig) Connector,
) *ConnPool {
	poolID, _ := util.RandID()
	return &ConnPool{
		common:           common,
		auth:             auth,
		clientSpec:       clientSpec,
		vnetController:   vnetController,
		connectorCreator: connectorCreator,
		activeIdx:        -1,
		doneCh:           make(chan struct{}),
		poolID:           poolID,
	}
}

// Run dials all configured protocols concurrently, selects the best connection
// as active, and starts the quality monitoring loop.
func (p *ConnPool) Run(ctx context.Context, proxyCfgs []v1.ProxyConfigurer, visitorCfgs []v1.VisitorConfigurer) {
	p.ctx, p.cancel = context.WithCancelCause(ctx)
	p.ctx = xlog.NewContext(p.ctx, xlog.FromContextSafe(p.ctx))
	p.xl = xlog.FromContextSafe(p.ctx)
	p.proxyCfgs = proxyCfgs
	p.visitorCfgs = visitorCfgs

	p.establishAll()
	go p.monitorAndSwitch()
	go p.watchEntries()
}

// establishAll dials each configured protocol and adds successful connections to the pool.
func (p *ConnPool) establishAll() {
	protocols := p.common.Transport.Protocols
	type result struct {
		entry    *poolEntry
		protocol string
		err      error
	}
	ch := make(chan result, len(protocols))

	for _, proto := range protocols {
		go func(protocol string) {
			entry, err := p.dialProtocol(protocol)
			ch <- result{entry: entry, protocol: protocol, err: err}
		}(proto)
	}

	for range protocols {
		r := <-ch
		if r.err != nil {
			p.xl.Warnf("failed to establish %s connection: %v", r.protocol, r.err)
			p.mu.Lock()
			p.entries = append(p.entries, &poolEntry{protocol: r.protocol})
			p.mu.Unlock()
			continue
		}
		p.mu.Lock()
		idx := len(p.entries)
		p.entries = append(p.entries, r.entry)
		if p.activeIdx < 0 {
			p.activeIdx = idx
			r.entry.ctl.SetInWorkConnCallback(p.handleWorkConnCb)
			r.entry.ctl.Run(p.proxyCfgs, p.visitorCfgs)
			p.xl.Infof("[%s] selected as active connection (RTT %v)", r.protocol, r.entry.ctl.GetRTT())
		} else {
			go r.entry.ctl.worker()
			p.xl.Infof("[%s] added to pool as standby (RTT %v)", r.protocol, r.entry.ctl.GetRTT())
		}
		p.mu.Unlock()
	}
}

// dialProtocol creates a full control connection using the specified protocol.
func (p *ConnPool) dialProtocol(protocol string) (*poolEntry, error) {
	cfgCopy := *p.common
	cfgCopy.Transport.Protocol = protocol
	if protocol == "quic" {
		cfgCopy.Transport.TCPMux = boolPtr(false)
	}

	dialer := &controlSessionDialer{
		ctx:            p.ctx,
		common:         &cfgCopy,
		auth:           p.auth,
		clientSpec:     p.clientSpec,
		vnetController: p.vnetController,
		poolID:         p.poolID,
		connectorCreator: func(ctx context.Context, cfg *v1.ClientCommonConfig) Connector {
			return NewConnectorWithProtocol(ctx, cfg, protocol)
		},
	}

	sessionCtx, err := dialer.Dial("")
	if err != nil {
		return nil, err
	}

	ctl, err := NewControl(p.ctx, sessionCtx)
	if err != nil {
		sessionCtx.Conn.Close()
		sessionCtx.Connector.Close()
		return nil, err
	}

	return &poolEntry{
		protocol: protocol,
		ctl:      ctl,
		session:  sessionCtx,
	}, nil
}

// watchEntries monitors each entry's Done channel and triggers failover when needed.
func (p *ConnPool) watchEntries() {
	for {
		p.mu.RLock()
		entries := make([]*poolEntry, len(p.entries))
		copy(entries, p.entries)
		p.mu.RUnlock()

		type watch struct {
			idx      int
			protocol string
			ch       <-chan struct{}
		}
		var watches []watch
		for i, e := range entries {
			if e.ctl != nil {
				watches = append(watches, watch{idx: i, protocol: e.protocol, ch: e.ctl.Done()})
			}
		}

		if len(watches) == 0 {
			select {
			case <-p.ctx.Done():
				p.close()
				return
			case <-time.After(reconnectInterval):
				p.reconnectFailed()
				continue
			}
		}

		cases := make([]reflect.SelectCase, 0, len(watches)+1)
		for _, w := range watches {
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(w.ch)})
		}
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(p.ctx.Done())})

		chosen, _, _ := reflect.Select(cases)

		if chosen == len(watches) {
			p.close()
			return
		}

		w := watches[chosen]
		p.xl.Warnf("[%s] connection lost", w.protocol)

		p.mu.Lock()
		isActive := (w.idx == p.activeIdx)
		p.mu.Unlock()

		if isActive {
			p.handleActiveFailure(w.idx)
		}

		go p.reconnectEntry(w.idx)
	}
}

// monitorAndSwitch periodically evaluates connection quality and switches
// to a better connection when the improvement exceeds the tolerance threshold.
func (p *ConnPool) monitorAndSwitch() {
	ticker := time.NewTicker(evalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.evaluateAndSwitch()
		}
	}
}

func (p *ConnPool) evaluateAndSwitch() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.entries) < 2 || p.activeIdx < 0 || p.switching {
		return
	}

	bestIdx := -1
	bestScore := math.MaxFloat64
	for i, entry := range p.entries {
		if entry.ctl == nil || !entry.ctl.IsAlive() {
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

	activeScore := activeQ.Score()
	if activeScore == 0 {
		activeScore = 1
	}
	improvement := (activeScore - bestScore) / activeScore
	if improvement > p.common.Transport.SwitchTolerance {
		p.xl.Infof("[%s] quality (%v) is %.0f%% better than active [%s] (%v), switching",
			p.entries[bestIdx].protocol, bestQ.RTT, improvement*100,
			p.entries[p.activeIdx].protocol, activeQ.RTT)
		go p.switchActiveTo(bestIdx)
	}
}

// collectQuality reads current metrics from a pool entry's Control.
func (p *ConnPool) collectQuality(entry *poolEntry) ConnQuality {
	return ConnQuality{
		RTT:      entry.ctl.GetRTT(),
		LastPong: entry.ctl.lastPong.Load().(time.Time),
	}
}

// switchActiveTo moves proxy registration from the current active to a new connection.
func (p *ConnPool) switchActiveTo(newIdx int) {
	// Phase 1: mark switching and collect what we need under lock.
	p.mu.Lock()
	if p.switching || p.activeIdx < 0 || p.activeIdx == newIdx {
		p.mu.Unlock()
		return
	}
	p.switching = true
	oldIdx := p.activeIdx
	oldEntry := p.entries[oldIdx]
	newEntry := p.entries[newIdx]
	if newEntry == nil || newEntry.ctl == nil {
		p.switching = false
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	p.xl.Infof("switching active: [%s] -> [%s] (RTT %v -> %v)",
		oldEntry.protocol, newEntry.protocol,
		oldEntry.ctl.GetRTT(), newEntry.ctl.GetRTT())

	// Phase 2: deregister on old active (no pool lock held).
	if oldEntry.ctl != nil && oldEntry.ctl.IsAlive() {
		oldEntry.ctl.DeregisterAll()
	}

	// Brief delay to let server process CloseProxy messages.
	time.Sleep(switchDelay)

	// Phase 3: register on new active and update index.
	newEntry.ctl.SetInWorkConnCallback(p.handleWorkConnCb)
	newEntry.ctl.RegisterAll(p.proxyCfgs, p.visitorCfgs)

	p.mu.Lock()
	p.activeIdx = newIdx
	p.switching = false
	p.mu.Unlock()
}

// handleActiveFailure is called when the active connection dies.
func (p *ConnPool) handleActiveFailure(failedIdx int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	bestIdx := -1
	bestScore := math.MaxFloat64
	for i, entry := range p.entries {
		if i == failedIdx || entry.ctl == nil || !entry.ctl.IsAlive() {
			continue
		}
		q := p.collectQuality(entry)
		if s := q.Score(); s < bestScore {
			bestScore = s
			bestIdx = i
		}
	}

	if bestIdx >= 0 {
		p.xl.Infof("active connection lost, failing over to [%s]", p.entries[bestIdx].protocol)
		p.activeIdx = bestIdx
		p.entries[bestIdx].ctl.SetInWorkConnCallback(p.handleWorkConnCb)
		p.entries[bestIdx].ctl.RegisterAll(p.proxyCfgs, p.visitorCfgs)
	} else {
		p.activeIdx = -1
		p.xl.Warnf("all connections lost, waiting for reconnection")
	}
}

// reconnectEntry re-establishes a failed connection in the background.
func (p *ConnPool) reconnectEntry(idx int) {
	p.mu.RLock()
	protocol := p.entries[idx].protocol
	p.mu.RUnlock()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-time.After(reconnectInterval):
		}

		entry, err := p.dialProtocol(protocol)
		if err != nil {
			p.xl.Warnf("[%s] reconnect failed: %v", protocol, err)
			continue
		}

		p.mu.Lock()
		p.entries[idx] = entry
		if p.activeIdx < 0 {
			p.activeIdx = idx
			entry.ctl.SetInWorkConnCallback(p.handleWorkConnCb)
			entry.ctl.Run(p.proxyCfgs, p.visitorCfgs)
			p.xl.Infof("[%s] reconnected and selected as active", protocol)
		} else {
			go entry.ctl.worker()
			p.xl.Infof("[%s] reconnected as standby", protocol)
		}
		p.mu.Unlock()
		return
	}
}

// reconnectFailed retries all nil entries (ones that failed initial connect).
func (p *ConnPool) reconnectFailed() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, entry := range p.entries {
		if entry.ctl != nil {
			continue
		}
		go func(idx int, protocol string) {
			for {
				select {
				case <-p.ctx.Done():
					return
				case <-time.After(reconnectInterval):
				}
				newEntry, err := p.dialProtocol(protocol)
				if err != nil {
					p.xl.Warnf("[%s] reconnect failed: %v", protocol, err)
					continue
				}
				p.mu.Lock()
				p.entries[idx] = newEntry
				if p.activeIdx < 0 {
					p.activeIdx = idx
					newEntry.ctl.SetInWorkConnCallback(p.handleWorkConnCb)
					newEntry.ctl.Run(p.proxyCfgs, p.visitorCfgs)
					p.xl.Infof("[%s] connected and selected as active", protocol)
				} else {
					go newEntry.ctl.worker()
					p.xl.Infof("[%s] connected as standby", protocol)
				}
				p.mu.Unlock()
				return
			}
		}(i, entry.protocol)
	}
}

// SetInWorkConnCallback sets the work connection callback for the active connection.
func (p *ConnPool) SetInWorkConnCallback(cb func(*v1.ProxyBaseConfig, net.Conn, *msg.StartWorkConn) bool) {
	p.handleWorkConnCb = cb
}

func (p *ConnPool) Close() error {
	return p.GracefulClose(0)
}

func (p *ConnPool) GracefulClose(d time.Duration) error {
	if p.cancel != nil {
		p.cancel(nil)
	}
	p.close()
	return nil
}

func (p *ConnPool) close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, entry := range p.entries {
			if entry.ctl != nil && entry.ctl.IsAlive() {
				entry.ctl.GracefulClose(0)
			}
		}
		close(p.doneCh)
	})
}

// Done returns a channel closed when the pool has fully exited.
func (p *ConnPool) Done() <-chan struct{} {
	return p.doneCh
}

// UpdateAllConfigurer updates proxy/visitor configs and re-registers on the active connection.
func (p *ConnPool) UpdateAllConfigurer(proxyCfgs []v1.ProxyConfigurer, visitorCfgs []v1.VisitorConfigurer) error {
	p.mu.Lock()
	p.proxyCfgs = proxyCfgs
	p.visitorCfgs = visitorCfgs
	activeIdx := p.activeIdx
	var activeCtl *Control
	if activeIdx >= 0 && activeIdx < len(p.entries) && p.entries[activeIdx] != nil {
		activeCtl = p.entries[activeIdx].ctl
	}
	p.mu.Unlock()

	if activeCtl != nil {
		return activeCtl.UpdateAllConfigurer(proxyCfgs, visitorCfgs)
	}
	return nil
}

func (p *ConnPool) getProxyStatus(name string) (*proxy.WorkingStatus, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.activeIdx >= 0 && p.activeIdx < len(p.entries) && p.entries[p.activeIdx] != nil && p.entries[p.activeIdx].ctl != nil {
		return p.entries[p.activeIdx].ctl.pm.GetProxyStatus(name)
	}
	return nil, false
}

func (p *ConnPool) getVisitorCfg(name string) (v1.VisitorConfigurer, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.activeIdx >= 0 && p.activeIdx < len(p.entries) && p.entries[p.activeIdx] != nil && p.entries[p.activeIdx].ctl != nil {
		return p.entries[p.activeIdx].ctl.vm.GetVisitorCfg(name)
	}
	return nil, false
}

// PoolID returns the unique identifier shared by all connections in this pool.
func (p *ConnPool) PoolID() string {
	return p.poolID
}

// GetStatus returns a snapshot of the pool's current state for the admin API.
func (p *ConnPool) GetStatus() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()

	protocols := p.common.Transport.Protocols
	active := ""
	conns := make([]map[string]any, 0, len(p.entries))

	for i, entry := range p.entries {
		status := map[string]any{
			"protocol": entry.protocol,
			"active":   i == p.activeIdx,
		}
		if entry.ctl != nil {
			status["alive"] = entry.ctl.IsAlive()
			status["rtt"] = entry.ctl.GetRTT().String()
			if lp := entry.ctl.lastPong.Load(); lp != nil {
				status["last_pong"] = lp.(time.Time).Unix()
			}
		} else {
			status["alive"] = false
			status["rtt"] = "n/a"
		}
		conns = append(conns, status)
		if i == p.activeIdx {
			active = entry.protocol
		}
	}

	return map[string]any{
		"pool_id":     p.poolID,
		"protocols":   protocols,
		"active":      active,
		"connections": conns,
	}
}

func boolPtr(b bool) *bool { return &b }
