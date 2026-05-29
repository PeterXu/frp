// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package socks5proxy

import (
	"net"
	"sync"
	"time"
)

// trackingConn wraps a net.Conn and reports cumulative byte totals to a callback.
// Updates are debounced: the callback fires at most once per minUpdateInterval,
// or immediately when the connection is closed via Close().
type trackingConn struct {
	net.Conn
	updateFn          func(bytesIn, bytesOut int64)
	minUpdateInterval time.Duration
	mu                sync.Mutex
	bytesIn           int64
	bytesOut          int64
	lastUpdate        time.Time
}

func newTrackingConn(conn net.Conn, updateFn func(bytesIn, bytesOut int64)) *trackingConn {
	return &trackingConn{
		Conn:              conn,
		updateFn:          updateFn,
		minUpdateInterval: time.Second,
		lastUpdate:        time.Now(),
	}
}

func (tc *trackingConn) Read(p []byte) (int, error) {
	n, err := tc.Conn.Read(p)
	if n > 0 {
		tc.record(int64(n), 0)
	}
	return n, err
}

func (tc *trackingConn) Write(p []byte) (int, error) {
	n, err := tc.Conn.Write(p)
	if n > 0 {
		tc.record(0, int64(n))
	}
	return n, err
}

func (tc *trackingConn) record(deltaIn, deltaOut int64) {
	tc.mu.Lock()
	tc.bytesIn += deltaIn
	tc.bytesOut += deltaOut
	now := time.Now()
	if now.Sub(tc.lastUpdate) >= tc.minUpdateInterval {
		tc.lastUpdate = now
		in, out := tc.bytesIn, tc.bytesOut
		tc.mu.Unlock()
		tc.updateFn(in, out)
		return
	}
	tc.mu.Unlock()
}

func (tc *trackingConn) Close() error {
	// Flush final byte counts on close
	tc.mu.Lock()
	in, out := tc.bytesIn, tc.bytesOut
	tc.mu.Unlock()
	tc.updateFn(in, out)
	return tc.Conn.Close()
}
