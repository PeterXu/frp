// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package msg

// NewSocks5VisitorConn is sent by the frpc1 socks5 visitor as the first
// message on a yamux stream to frps, requesting frps to route a SOCKS5
// CONNECT to a frpc2 socks5_relay proxy in the named group.
//
// Routing fields (Group/UserID/TargetUser) mirror the SOCKS5 username
// semantics documented in pkg/util/socks5.ParseGroupUserID.
//
// AuthPassword is carried for forward compatibility; frpc2 currently does
// not verify it (frpc1 already validates the SOCKS5 client auth locally).
type NewSocks5VisitorConn struct {
	RunID        string `json:"run_id,omitempty"`
	Group        string `json:"group,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	TargetUser   string `json:"target_user,omitempty"`
	DstAddr      string `json:"dst_addr,omitempty"`
	DstPort      uint16 `json:"dst_port,omitempty"`
	AuthPassword string `json:"auth_password,omitempty"`
}

// NewSocks5VisitorConnResp is frps's reply. Error == "" means routing +
// work-conn acquisition succeeded and the stream is now bridged end-to-end.
type NewSocks5VisitorConnResp struct {
	Error string `json:"error,omitempty"`
}
