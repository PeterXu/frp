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
	"net"
	"time"

	libio "github.com/fatedier/golib/io"

	"github.com/fatedier/frp/pkg/util/socks5"
	"github.com/fatedier/frp/pkg/util/xlog"
)

// SOCKS5Handler is a server-level SOCKS5 proxy listener.
type SOCKS5Handler struct {
	listener     net.Listener
	authPassword string
	selectFrpcFn func(group, userID, targetUser string, dstAddr string, dstPort uint16) (net.Conn, string, string, error)
	connTracker  *RelayConnTracker
	connLimit    chan struct{} // nil = unlimited
}

func NewSOCKS5Handler(
	listener net.Listener,
	authPassword string,
	selectFrpcFn func(group, userID, targetUser string, dstAddr string, dstPort uint16) (net.Conn, string, string, error),
	connTracker *RelayConnTracker,
	connLimit chan struct{},
) *SOCKS5Handler {
	h := &SOCKS5Handler{
		listener:     listener,
		authPassword: authPassword,
		selectFrpcFn: selectFrpcFn,
		connTracker:  connTracker,
		connLimit:    connLimit,
	}
	return h
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
		if h.connLimit != nil {
			select {
			case h.connLimit <- struct{}{}:
			default:
				xl.Warnf("socks5 max connections reached, rejecting %s", conn.RemoteAddr())
				conn.Close()
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

const socks5HandshakeTimeout = 30 * time.Second

func (h *SOCKS5Handler) handleConn(ctx context.Context, clientConn net.Conn) {
	xl := xlog.FromContextSafe(ctx)
	defer clientConn.Close()
	if h.connLimit != nil {
		defer func() { <-h.connLimit }()
	}

	// Set deadline for the entire handshake phase to prevent slowloris attacks
	clientConn.SetDeadline(time.Now().Add(socks5HandshakeTimeout))

	// SOCKS5 handshake
	method, err := socks5.Handshake(clientConn, false)
	if err != nil {
		xl.Debugf("socks5 handshake error: %v", err)
		return
	}

	// SOCKS5 auth (username = group[@userID] or group[=targetUser], password = authPassword)
	group, userID, targetUser, err := socks5.Authenticate(clientConn, method, h.authPassword)
	if err != nil {
		xl.Debugf("socks5 auth error: %v", err)
		return
	}

	// SOCKS5 connect request - get target address
	dstAddr, dstPort, err := socks5.ReadConnectRequest(clientConn)
	if err != nil {
		xl.Debugf("socks5 connect request error: %v", err)
		return
	}

	// Log the proxy connection with complete URL
	xl.Infof("socks5 proxy connection: src [%s] group [%s] userID [%s] targetUser [%s], target [socks5://%s:%d]",
		clientConn.RemoteAddr(), group, userID, targetUser, dstAddr, dstPort)

	// Select frpc and get work connection with target address
	workConn, proxyName, runID, err := h.selectFrpcFn(group, userID, targetUser, dstAddr, dstPort)
	if err != nil {
		xl.Warnf("select frpc for group [%s] error: %v", group, err)
		clientConn.SetDeadline(time.Time{})    // clear deadline so error reply can be sent
		_ = socks5.SendReply(clientConn, 0x01) // general SOCKS server failure (best-effort)
		return
	}
	defer workConn.Close()

	// Register connection in tracker
	connID := h.connTracker.Track(RelayConnInfo{
		SourceIP:  clientConn.RemoteAddr().String(),
		Protocol:  "socks5",
		Group:     group,
		UserID:    userID,
		DstAddr:   dstAddr,
		DstPort:   dstPort,
		ProxyName: proxyName,
		RunID:     runID,
		StartTime: time.Now(),
		BytesIn:   0,
		BytesOut:  0,
	})

	// Wrap connections for byte tracking
	clientConn = newTrackingConn(clientConn, func(bytesIn, bytesOut int64) {
		h.connTracker.UpdateBytes(connID, bytesIn, bytesOut)
	})
	workConn = newTrackingConn(workConn, func(bytesIn, bytesOut int64) {
		h.connTracker.UpdateBytes(connID, bytesOut, bytesIn) // reversed for workConn
	})

	// Close tracking wrappers first (flush final byte counts), then remove from tracker.
	// Earlier safety-net defers for raw conns are harmless double-closes on TCP.
	defer h.connTracker.Remove(connID)
	defer func() { workConn.Close() }()
	defer func() { clientConn.Close() }()

	// Clear deadline before sending reply — selectFrpcFn retries may have consumed most of it
	clientConn.SetDeadline(time.Time{})

	// Send success reply to client; abort relaying if it never reached the client.
	if err := socks5.SendReply(clientConn, 0x00); err != nil {
		xl.Debugf("send success reply: %v", err)
		return
	}

	// Bridge traffic
	_, _, errs := libio.Join(clientConn, workConn)
	for _, err := range errs {
		if err != nil {
			xl.Debugf("socks5 relay error for %s: %v", clientConn.RemoteAddr(), err)
		}
	}
}
