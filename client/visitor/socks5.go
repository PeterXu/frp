// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package visitor

import (
	"net"
	"strconv"
	"time"

	libio "github.com/fatedier/golib/io"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/util/socks5"
	"github.com/fatedier/frp/pkg/util/xlog"
)

const socks5VisitorHandshakeTimeout = 30 * time.Second
const socks5VisitorRespTimeout = 10 * time.Second

// Socks5Visitor listens on a local TCP port, accepts SOCKS5 clients, and for
// each accepted connection opens a yamux stream to frps carrying a
// NewSocks5VisitorConn first message. frps routes the request to a frpc2
// socks5_relay proxy; bytes are then bridged end-to-end.
type Socks5Visitor struct {
	*BaseVisitor
	cfg *v1.Socks5VisitorConfig

	tokenPool chan struct{} // nil = unlimited
}

func (sv *Socks5Visitor) Run() (err error) {
	if sv.cfg.MaxConcurrent > 0 {
		sv.tokenPool = make(chan struct{}, sv.cfg.MaxConcurrent)
	}
	if sv.cfg.BindPort > 0 {
		sv.l, err = net.Listen("tcp", net.JoinHostPort(sv.cfg.BindAddr, strconv.Itoa(sv.cfg.BindPort)))
		if err != nil {
			return
		}
		go sv.acceptLoop(sv.l, "socks5 local", sv.handleConn)
	}

	go sv.acceptLoop(sv.internalLn, "socks5 internal", sv.handleConn)

	if sv.plugin != nil {
		sv.plugin.Start()
	}
	return
}

func (sv *Socks5Visitor) handleConn(userConn net.Conn) {
	xl := xlog.FromContextSafe(sv.ctx)
	defer userConn.Close()

	if sv.tokenPool != nil {
		select {
		case sv.tokenPool <- struct{}{}:
			defer func() { <-sv.tokenPool }()
		default:
			xl.Warnf("socks5 visitor maxConcurrent reached, rejecting %s", userConn.RemoteAddr())
			return
		}
	}

	// Stagger the handshake phase to prevent slowloris.
	userConn.SetDeadline(time.Now().Add(socks5VisitorHandshakeTimeout))

	if err := socks5.Handshake(userConn); err != nil {
		xl.Debugf("socks5 handshake error: %v", err)
		return
	}

	group, userID, targetUser, err := socks5.Authenticate(userConn, sv.cfg.AuthPassword)
	if err != nil {
		xl.Debugf("socks5 auth error: %v", err)
		return
	}

	// Static fallback when the SOCKS5 client supplies no username.
	if group == "" {
		if sv.cfg.ServerName != "" {
			group = sv.cfg.ServerName
			targetUser = sv.cfg.ServerUser
			xl.Debugf("using static fallback group=%s targetUser=%s", group, targetUser)
		} else {
			xl.Debugf("reject: no group (empty SOCKS5 username and no serverName configured)")
			userConn.SetDeadline(time.Time{})
			socks5.SendReply(userConn, 0x01)
			return
		}
	}

	dstAddr, dstPort, err := socks5.ReadConnectRequest(userConn)
	if err != nil {
		xl.Debugf("socks5 connect request error: %v", err)
		return
	}

	// Clear deadline before opening the tunnel — selectFrpcFn may retry.
	userConn.SetDeadline(time.Time{})

	xl.Infof("socks5 visitor request: src [%s] group [%s] userID [%s] targetUser [%s] target [%s:%d]",
		userConn.RemoteAddr(), group, userID, targetUser, dstAddr, dstPort)

	frpsConn, err := sv.helper.ConnectServer()
	if err != nil {
		xl.Warnf("connect to frps error: %v", err)
		socks5.SendReply(userConn, 0x01)
		return
	}
	defer frpsConn.Close()

	err = frpsConn.WriteMsg(&msg.NewSocks5VisitorConn{
		RunID:        sv.helper.RunID(),
		Group:        group,
		UserID:       userID,
		TargetUser:   targetUser,
		DstAddr:      dstAddr,
		DstPort:      dstPort,
		AuthPassword: sv.cfg.AuthPassword,
	})
	if err != nil {
		xl.Warnf("send NewSocks5VisitorConn error: %v", err)
		socks5.SendReply(userConn, 0x01)
		return
	}

	_ = frpsConn.SetReadDeadline(time.Now().Add(socks5VisitorRespTimeout))
	var resp msg.NewSocks5VisitorConnResp
	if err := frpsConn.ReadMsgInto(&resp); err != nil {
		xl.Warnf("read NewSocks5VisitorConnResp error: %v", err)
		socks5.SendReply(userConn, 0x01)
		return
	}
	_ = frpsConn.SetReadDeadline(time.Time{})

	if resp.Error != "" {
		xl.Warnf("NewSocks5VisitorConn resp error: %s", resp.Error)
		socks5.SendReply(userConn, 0x01)
		return
	}

	socks5.SendReply(userConn, 0x00)
	libio.Join(userConn, frpsConn)
}

func (sv *Socks5Visitor) Close() {
	sv.BaseVisitor.Close()
}
