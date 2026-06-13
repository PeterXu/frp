// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package socks5proxy

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/util/xlog"
)

// fakeSelectFn returns a work conn that the test reads from to verify bridging,
// and records the args it was called with.
type fakeSelectFn struct {
	mu       sync.Mutex
	called   bool
	group    string
	userID   string
	target   string
	dstAddr  string
	dstPort  uint16
	workConn net.Conn
}

func (f *fakeSelectFn) call() SelectFrpcFn {
	return func(group, userID, targetUser, dstAddr string, dstPort uint16) (net.Conn, string, string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.called = true
		f.group = group
		f.userID = userID
		f.target = targetUser
		f.dstAddr = dstAddr
		f.dstPort = dstPort
		return f.workConn, "proxy-fake", "runid-fake", nil
	}
}

// msgPipe returns a connected pair: one side is a *msg.Conn (the visitor
// stream, as HandleNewSocks5VisitorConn expects) and the other is also a
// *msg.Conn so the test driver can read the resp via ReadMsgInto.
//
// Uses V1 protocol (JSON over length-prefixed frames) — simplest wire format
// available in the msg package.
func msgPipe(t *testing.T) (*msg.Conn, *msg.Conn) {
	c1, c2 := net.Pipe()
	rw1 := msg.NewV1ReadWriter(c1)
	rw2 := msg.NewV1ReadWriter(c2)
	return msg.NewConn(c1, rw1), msg.NewConn(c2, rw2)
}

func TestHandleNewSocks5VisitorConn_SuccessBridgesAndForwardsArgs(t *testing.T) {
	// Two *msg.Conns over a net.Pipe, one for the handler to use as
	// visitorConn, one for the test driver to read resp + write bytes.
	visitorMsgConn, driverMsgConn := msgPipe(t)
	defer driverMsgConn.Close()

	// work conn: a plain net.Pipe pair. selectFn returns workClient; the
	// test reads from workServer to confirm bridged bytes.
	workServer, workClient := net.Pipe()
	defer workServer.Close()

	fn := &fakeSelectFn{workConn: workClient}
	xl := xlog.New()

	// Drive the handler in a goroutine; it blocks on libio.Join after resp.
	go func() {
		_ = HandleNewSocks5VisitorConn(xl, visitorMsgConn, &msg.NewSocks5VisitorConn{
			Group: "g1", UserID: "u1", TargetUser: "", DstAddr: "1.2.3.4", DstPort: 80,
		}, fn.call(), nil)
	}()

	// The driver side should receive a success resp first.
	var resp msg.NewSocks5VisitorConnResp
	if err := driverMsgConn.ReadMsgInto(&resp); err != nil {
		t.Fatalf("read resp: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("expected empty Error, got %q", resp.Error)
	}

	// Verify the selectFn was called with the right args.
	if !fn.called || fn.group != "g1" || fn.userID != "u1" || fn.dstAddr != "1.2.3.4" || fn.dstPort != 80 {
		t.Fatalf("selectFn args wrong: %+v", fn)
	}

	// Write bytes to the driver side; expect them bridged through to workServer.
	if _, err := driverMsgConn.Write([]byte("hello-work")); err != nil {
		t.Fatalf("driver write: %v", err)
	}
	buf := make([]byte, 32)
	n, err := workServer.Read(buf)
	if err != nil {
		t.Fatalf("work read: %v", err)
	}
	if !bytes.Equal(buf[:n], []byte("hello-work")) {
		t.Fatalf("work got %q want hello-work", buf[:n])
	}
}

func TestHandleNewSocks5VisitorConn_SelectErrorWritesResp(t *testing.T) {
	visitorMsgConn, driverMsgConn := msgPipe(t)
	defer driverMsgConn.Close()

	errFn := func(group, userID, targetUser, dstAddr string, dstPort uint16) (net.Conn, string, string, error) {
		return nil, "", "", io.EOF
	}

	// Start reading the response in a goroutine so the handler's WriteMsg doesn't block.
	type respResult struct {
		resp *msg.NewSocks5VisitorConnResp
		err  error
	}
	respCh := make(chan respResult, 1)
	go func() {
		var resp msg.NewSocks5VisitorConnResp
		err := driverMsgConn.ReadMsgInto(&resp)
		respCh <- respResult{resp: &resp, err: err}
	}()

	err := HandleNewSocks5VisitorConn(xlog.New(), visitorMsgConn, &msg.NewSocks5VisitorConn{
		Group: "g1", DstAddr: "x", DstPort: 1,
	}, errFn, nil)
	if err == nil {
		t.Fatal("expected error from handler")
	}

	result := <-respCh
	if result.err != nil {
		t.Fatalf("read resp: %v", result.err)
	}
	if result.resp.Error == "" {
		t.Fatal("expected non-empty Error in resp")
	}
}

func TestHandleNewSocks5VisitorConn_TracksConnection(t *testing.T) {
	tracker := NewRelayConnTracker(time.Minute)
	defer tracker.Close()

	visitorMsgConn, driverMsgConn := msgPipe(t)
	defer driverMsgConn.Close()

	workServer, workClient := net.Pipe()
	defer workServer.Close()

	fn := &fakeSelectFn{workConn: workClient}

	go func() {
		_ = HandleNewSocks5VisitorConn(xlog.New(), visitorMsgConn, &msg.NewSocks5VisitorConn{
			Group: "g1", UserID: "u1", DstAddr: "1.2.3.4", DstPort: 80,
		}, fn.call(), tracker)
	}()

	var resp msg.NewSocks5VisitorConnResp
	if err := driverMsgConn.ReadMsgInto(&resp); err != nil {
		t.Fatalf("read resp: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("expected empty Error, got %q", resp.Error)
	}

	// The relay is tracked with the visitor protocol and forwarded metadata.
	conns := tracker.GetAll()
	if len(conns) != 1 {
		t.Fatalf("expected 1 tracked conn, got %d", len(conns))
	}
	c := conns[0]
	if c.Protocol != "socks5_visitor" || c.Group != "g1" || c.UserID != "u1" ||
		c.DstAddr != "1.2.3.4" || c.DstPort != 80 || c.ProxyName != "proxy-fake" {
		t.Fatalf("tracked conn metadata wrong: %+v", c)
	}

	// Closing the visitor side ends the bridge and removes the active entry.
	driverMsgConn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(tracker.GetAll()) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("tracked conn was not removed after close")
}
