// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package socks5proxy

import (
	"fmt"
	"net"
	"time"

	libio "github.com/fatedier/golib/io"

	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/util/xlog"
)

// SelectFrpcFn mirrors the signature returned by Service.makeSelectFrpcFn.
// Pulled out as an interface so the dial handler is unit-testable without
// spinning up a full frps Service.
type SelectFrpcFn func(group, userID, targetUser string, dstAddr string, dstPort uint16) (net.Conn, string, string, error)

// HandleNewSocks5VisitorConn runs on frps when a stream from a frpc1 socks5
// visitor arrives carrying a NewSocks5VisitorConn first message. It routes
// through the existing selectFrpcFn (which finds a frpc2 in the group and
// returns its work conn), then bridges the visitor stream end-to-end with
// the frpc2 work conn.
//
// On any routing error, writes a NewSocks5VisitorConnResp{Error: ...} back
// to the visitor and closes the stream. On success, writes an empty Error
// response, then blocks on libio.Join until either side closes.
//
// visitorConn is the *msg.Conn from the incoming yamux stream — it embeds a
// net.Conn (so satisfies libio.Join) and exposes WriteMsg. The caller in
// server/service.go will pass acceptedConn.conn (already a *msg.Conn in the
// existing handleConnection path).
//
// connTracker, when non-nil, records the relay in the dashboard connection
// view (Protocol "socks5_visitor") with byte counters, mirroring
// SOCKS5Handler.handleConn. It may be nil in tests.
func HandleNewSocks5VisitorConn(
	xl *xlog.Logger,
	visitorConn *msg.Conn,
	m *msg.NewSocks5VisitorConn,
	selectFn SelectFrpcFn,
	connTracker *RelayConnTracker,
) error {
	workConn, proxyName, runID, err := selectFn(m.Group, m.UserID, m.TargetUser, m.DstAddr, m.DstPort)
	if err != nil {
		_ = visitorConn.WriteMsg(&msg.NewSocks5VisitorConnResp{
			Error: fmt.Sprintf("select frpc for group [%s]: %v", m.Group, err),
		})
		return fmt.Errorf("select frpc for group [%s] error: %w", m.Group, err)
	}
	defer workConn.Close()

	// Track before replying so the entry exists by the time the visitor reads
	// its success response (deterministic for the dashboard/tests).
	var connID string
	tracked := connTracker != nil
	if tracked {
		connID = connTracker.Track(RelayConnInfo{
			SourceIP:  visitorConn.RemoteAddr().String(),
			Protocol:  "socks5_visitor",
			Group:     m.Group,
			UserID:    m.UserID,
			DstAddr:   m.DstAddr,
			DstPort:   m.DstPort,
			ProxyName: proxyName,
			RunID:     runID,
			StartTime: time.Now(),
		})
		defer connTracker.Remove(connID)
	}

	if err := visitorConn.WriteMsg(&msg.NewSocks5VisitorConnResp{Error: ""}); err != nil {
		return fmt.Errorf("write visitor resp: %w", err)
	}

	// Wrap both sides with byte counters so the dashboard shows live traffic.
	var dirConn net.Conn = visitorConn
	tranConn := workConn
	if tracked {
		dirConn = newTrackingConn(visitorConn, func(in, out int64) {
			connTracker.UpdateBytes(connID, in, out)
		})
		tranConn = newTrackingConn(workConn, func(in, out int64) {
			connTracker.UpdateBytes(connID, out, in) // reversed: workConn read = bytes out to target
		})
		defer tranConn.Close()
		defer dirConn.Close()
	}

	xl.Infof("socks5 visitor bridged: group [%s] userID [%s] targetUser [%s] proxy [%s] runID [%s] target [%s:%d]",
		m.Group, m.UserID, m.TargetUser, proxyName, runID, m.DstAddr, m.DstPort)

	_, _, errs := libio.Join(dirConn, tranConn)
	for _, e := range errs {
		if e != nil {
			xl.Debugf("socks5 visitor relay end: %v", e)
		}
	}
	return nil
}
