// Copyright 2019 fatedier, fatedier@gmail.com
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

package controller

import (
	"github.com/fatedier/frp/pkg/nathole"
	plugin "github.com/fatedier/frp/pkg/plugin/server"
	"github.com/fatedier/frp/pkg/util/tcpmux"
	"github.com/fatedier/frp/pkg/util/vhost"
	"github.com/fatedier/frp/server/group"
	"github.com/fatedier/frp/server/ports"
	"github.com/fatedier/frp/server/visitor"
)

// All resource managers and controllers
type ResourceController struct {
	// Manage all visitor listeners
	VisitorManager *visitor.Manager

	// TCP Group Controller
	TCPGroupCtl *group.TCPGroupCtl

	// HTTP Group Controller
	HTTPGroupCtl *group.HTTPGroupController

	// HTTPS Group Controller
	HTTPSGroupCtl *group.HTTPSGroupController

	// TCP Mux Group Controller
	TCPMuxGroupCtl *group.TCPMuxGroupCtl

	// Manage all TCP ports
	TCPPortManager *ports.Manager

	// Manage all UDP ports
	UDPPortManager *ports.Manager

	// For HTTP proxies, forwarding HTTP requests
	HTTPReverseProxy *vhost.HTTPReverseProxy

	// For HTTPS proxies, route requests to different clients by hostname and other information
	VhostHTTPSMuxer *vhost.HTTPSMuxer

	// Controller for nat hole connections
	NatHoleController *nathole.Controller

	// TCPMux HTTP CONNECT multiplexer
	TCPMuxHTTPConnectMuxer *tcpmux.HTTPConnectTCPMuxer

	// All server manager plugin
	PluginManager *plugin.Manager

	// Socks5 relay group registry (interface satisfied by server.Socks5RelayGroupRegistry)
	Socks5RelayGroupRegistry Socks5GroupRegistry
	// Socks5 relay session manager (interface satisfied by server.SessionManager)
	Socks5SessionManager Socks5SessionManager
	// StateStore for persistent state (interface satisfied by server.StateStore)
	StateStore Socks5StateStore
}

// Socks5GroupRegistry manages socks5_relay group → frpc runID mappings.
type Socks5GroupRegistry interface {
	Register(group, runID, proxyName string)
	Unregister(runID string)
	GetGroupMembers(group string) []string
	GetProxyName(group, runID string) string
}

// Socks5SessionManager manages socks5_relay session cleanup.
// SelectFrpc is NOT in this interface — only the handlers call it, via the selectFrpcFn closure
// from Service (which accesses SessionManager directly since they're in the same package).
type Socks5SessionManager interface {
	RemoveSession(runID string)
}

// Socks5StateStore provides persistent storage for socks5 state.
type Socks5StateStore interface {
	DisableGroup(group string) error
	EnableGroup(group string) error
	IsGroupDisabled(group string) bool
	DisableClient(key string) error
	EnableClient(key string) error
	IsClientDisabled(key string) bool
}

func (rc *ResourceController) Close() error {
	if rc.VhostHTTPSMuxer != nil {
		rc.VhostHTTPSMuxer.Close()
	}
	if rc.TCPMuxHTTPConnectMuxer != nil {
		rc.TCPMuxHTTPConnectMuxer.Close()
	}
	return nil
}
