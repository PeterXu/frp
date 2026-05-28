// Copyright 2025 The frp Authors
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

package model

const SourceStore = "store"

// StatusResp is the response for GET /api/status
type StatusResp map[string][]ProxyStatusResp

// ProxyStatusResp contains proxy status information
type ProxyStatusResp struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Err        string `json:"err"`
	LocalAddr  string `json:"local_addr"`
	Plugin     string `json:"plugin"`
	RemoteAddr string `json:"remote_addr"`
	Source     string `json:"source,omitempty"` // "store" or "config"
}

// ProxyListResp is the response for GET /api/store/proxies
type ProxyListResp struct {
	Proxies []ProxyDefinition `json:"proxies"`
}

// VisitorListResp is the response for GET /api/store/visitors
type VisitorListResp struct {
	Visitors []VisitorDefinition `json:"visitors"`
}

// PoolStatusResp is the response for GET /api/pool/status
type PoolStatusResp struct {
	PoolID      string           `json:"pool_id"`
	Protocols   []string         `json:"protocols"`
	Active      string           `json:"active"`
	Connections []PoolConnStatus `json:"connections"`
}

// PoolConnStatus describes one connection in the pool.
type PoolConnStatus struct {
	Protocol string `json:"protocol"`
	Active   bool   `json:"active"`
	RTT      string `json:"rtt"`
	Alive    bool   `json:"alive"`
	LastPong int64  `json:"last_pong,omitempty"`
}
