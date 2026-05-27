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

import (
	v1 "github.com/fatedier/frp/pkg/config/v1"
)

type ServerInfoResp struct {
	Version               string `json:"version"`
	BindPort              int    `json:"bindPort"`
	VhostHTTPPort         int    `json:"vhostHTTPPort"`
	VhostHTTPSPort        int    `json:"vhostHTTPSPort"`
	TCPMuxHTTPConnectPort int    `json:"tcpmuxHTTPConnectPort"`
	KCPBindPort           int    `json:"kcpBindPort"`
	QUICBindPort          int    `json:"quicBindPort"`
	SubdomainHost         string `json:"subdomainHost"`
	MaxPoolCount          int64  `json:"maxPoolCount"`
	MaxPortsPerClient     int64  `json:"maxPortsPerClient"`
	HeartBeatTimeout      int64  `json:"heartbeatTimeout"`
	AllowPortsStr         string `json:"allowPortsStr,omitempty"`
	TLSForce              bool   `json:"tlsForce,omitempty"`

	TotalTrafficIn  int64            `json:"totalTrafficIn"`
	TotalTrafficOut int64            `json:"totalTrafficOut"`
	CurConns        int64            `json:"curConns"`
	ClientCounts    int64            `json:"clientCounts"`
	ProxyTypeCounts map[string]int64 `json:"proxyTypeCount"`

	Socks5ProxyPort      int `json:"socks5ProxyPort"`
	HTTPConnectProxyPort int `json:"httpConnectProxyPort"`
	WebServerPort        int `json:"webServerPort"`
}

type ClientInfoResp struct {
	Key              string `json:"key"`
	User             string `json:"user"`
	ClientID         string `json:"clientID"`
	RunID            string `json:"runID"`
	Version          string `json:"version,omitempty"`
	WireProtocol     string `json:"wireProtocol,omitempty"`
	Hostname         string `json:"hostname"`
	ClientIP         string `json:"clientIP,omitempty"`
	FirstConnectedAt int64  `json:"firstConnectedAt"`
	LastConnectedAt  int64  `json:"lastConnectedAt"`
	DisconnectedAt   int64  `json:"disconnectedAt,omitempty"`
	Online           bool   `json:"online"`
}

type BaseOutConf struct {
	v1.ProxyBaseConfig
}

type TCPOutConf struct {
	BaseOutConf
	RemotePort int `json:"remotePort"`
}

type TCPMuxOutConf struct {
	BaseOutConf
	v1.DomainConfig
	Multiplexer     string `json:"multiplexer"`
	RouteByHTTPUser string `json:"routeByHTTPUser"`
}

type UDPOutConf struct {
	BaseOutConf
	RemotePort int `json:"remotePort"`
}

type HTTPOutConf struct {
	BaseOutConf
	v1.DomainConfig
	Locations         []string `json:"locations"`
	HostHeaderRewrite string   `json:"hostHeaderRewrite"`
}

type HTTPSOutConf struct {
	BaseOutConf
	v1.DomainConfig
}

type STCPOutConf struct {
	BaseOutConf
}

type XTCPOutConf struct {
	BaseOutConf
}

type Socks5RelayOutConf struct {
	BaseOutConf
}

type Socks5RelayGroupInfo struct {
	Name     string                   `json:"name"`
	Members  []Socks5RelayGroupMember `json:"members"`
	Disabled bool                     `json:"disabled"` // group disabled status
}

type Socks5RelayGroupMember struct {
	RunID     string `json:"runID"`
	ProxyName string `json:"proxyName"`
	Online    bool   `json:"online"`
	Key       string `json:"key"`      // client key for disable operations (= runID)
	Disabled  bool   `json:"disabled"` // disabled status
}

// DisableStateResponse represents the response for disable/enable operations.
type DisableStateResponse struct {
	Success  bool   `json:"success"`
	Key      string `json:"key,omitempty"`
	Group    string `json:"group,omitempty"`
	Disabled bool   `json:"disabled"`
}

type Socks5RelaySessionInfo struct {
	Username string `json:"username"`
	RunID    string `json:"runID"`
}

type RelayConnectionInfo struct {
	ID        string `json:"id"`
	SourceIP  string `json:"sourceIP"`
	Protocol  string `json:"protocol"`
	Group     string `json:"group"`
	DstAddr   string `json:"dstAddr"`
	DstPort   int    `json:"dstPort"`
	ProxyName string `json:"proxyName"`
	RunID     string `json:"runID"`
	StartTime int64  `json:"startTime"`
	EndTime   *int64 `json:"endTime,omitempty"`
	BytesIn   int64  `json:"bytesIn"`
	BytesOut  int64  `json:"bytesOut"`
	IsActive  bool   `json:"isActive"`
}

type RelayConnectionStats struct {
	TotalConnections int   `json:"totalConnections"`
	TotalBytesIn     int64 `json:"totalBytesIn"`
	TotalBytesOut    int64 `json:"totalBytesOut"`
}

// Get proxy info.
type ProxyStatsInfo struct {
	Name            string `json:"name"`
	Conf            any    `json:"conf"`
	User            string `json:"user,omitempty"`
	ClientID        string `json:"clientID,omitempty"`
	TodayTrafficIn  int64  `json:"todayTrafficIn"`
	TodayTrafficOut int64  `json:"todayTrafficOut"`
	CurConns        int64  `json:"curConns"`
	LastStartTime   string `json:"lastStartTime"`
	LastCloseTime   string `json:"lastCloseTime"`
	Status          string `json:"status"`
}

type GetProxyInfoResp struct {
	Proxies []*ProxyStatsInfo `json:"proxies"`
}

// Get proxy info by name.
type GetProxyStatsResp struct {
	Name            string `json:"name"`
	Conf            any    `json:"conf"`
	User            string `json:"user,omitempty"`
	ClientID        string `json:"clientID,omitempty"`
	TodayTrafficIn  int64  `json:"todayTrafficIn"`
	TodayTrafficOut int64  `json:"todayTrafficOut"`
	CurConns        int64  `json:"curConns"`
	LastStartTime   string `json:"lastStartTime"`
	LastCloseTime   string `json:"lastCloseTime"`
	Status          string `json:"status"`
}

// /api/traffic/:name
type GetProxyTrafficResp struct {
	Name       string  `json:"name"`
	TrafficIn  []int64 `json:"trafficIn"`
	TrafficOut []int64 `json:"trafficOut"`
}
