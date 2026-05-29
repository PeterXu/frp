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
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// RelayConnInfo tracks metadata for an active SOCKS5/HTTP CONNECT relay connection.
type RelayConnInfo struct {
	ID        string
	SourceIP  string
	Protocol  string // "socks5" or "http_connect"
	Group     string // username (= group name)
	DstAddr   string
	DstPort   uint16
	ProxyName string
	RunID     string
	StartTime time.Time
	EndTime   *time.Time // nil if active, set if closed
	BytesIn   int64
	BytesOut  int64
	IsActive  bool // true for active connections, false for closed
}

// RelayConnEvent represents a connection lifecycle event.
type RelayConnEvent struct {
	Type string        `json:"type"` // "created", "updated", "deleted"
	Conn RelayConnInfo `json:"conn"`
}

// RelayConnTracker tracks active relay connections with thread-safe operations.
type RelayConnTracker struct {
	connections       map[string]*RelayConnInfo
	closedConnections map[string]*RelayConnInfo // Recently closed connections
	mu                sync.RWMutex
	nextID            atomic.Uint64

	// Retention duration for closed connections
	retentionDuration time.Duration

	// Event broadcasting
	subscribers map[chan<- RelayConnEvent]struct{}
	subMu       sync.RWMutex

	// Cleanup context
	ctx    context.Context
	cancel context.CancelFunc
}

// NewRelayConnTracker creates a new connection tracker.
func NewRelayConnTracker(retentionDuration time.Duration) *RelayConnTracker {
	ctx, cancel := context.WithCancel(context.Background())
	tracker := &RelayConnTracker{
		connections:       make(map[string]*RelayConnInfo),
		closedConnections: make(map[string]*RelayConnInfo),
		retentionDuration: retentionDuration,
		subscribers:       make(map[chan<- RelayConnEvent]struct{}),
		ctx:               ctx,
		cancel:            cancel,
	}
	go tracker.cleanupLoop()
	return tracker
}

// Track registers a new connection and returns its unique ID.
func (t *RelayConnTracker) Track(info RelayConnInfo) string {
	id := fmt.Sprintf("conn-%d", t.nextID.Add(1))
	info.ID = id
	info.IsActive = true

	t.mu.Lock()
	t.connections[id] = &info
	t.mu.Unlock()

	t.broadcast(RelayConnEvent{Type: "created", Conn: info})
	return id
}

// UpdateBytes updates the byte counts for a connection.
func (t *RelayConnTracker) UpdateBytes(id string, bytesIn, bytesOut int64) {
	var connCopy RelayConnInfo
	var shouldBroadcast bool

	t.mu.Lock()
	if conn, ok := t.connections[id]; ok {
		conn.BytesIn = bytesIn
		conn.BytesOut = bytesOut
		connCopy = *conn
		shouldBroadcast = true
	}
	t.mu.Unlock()

	if shouldBroadcast {
		t.broadcast(RelayConnEvent{Type: "updated", Conn: connCopy})
	}
}

// Remove removes a connection from the tracker and moves it to closed connections.
func (t *RelayConnTracker) Remove(id string) {
	var connCopy RelayConnInfo
	var shouldBroadcast bool

	t.mu.Lock()
	if conn, ok := t.connections[id]; ok {
		// Check if already in closedConnections to prevent duplicates
		if _, exists := t.closedConnections[id]; exists {
			t.mu.Unlock()
			return
		}
		delete(t.connections, id)
		connCopy = *conn
		now := time.Now()
		connCopy.EndTime = &now
		connCopy.IsActive = false
		// Only store in closedConnections when retention is enabled
		if t.retentionDuration > 0 {
			t.closedConnections[id] = &connCopy
		}
		shouldBroadcast = true
	}
	t.mu.Unlock()

	// Broadcast outside the lock to prevent blocking if subscribers are slow
	if shouldBroadcast {
		t.broadcast(RelayConnEvent{Type: "deleted", Conn: connCopy})
	}
}

// GetAll returns a snapshot of all active connections.
func (t *RelayConnTracker) GetAll() []RelayConnInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]RelayConnInfo, 0, len(t.connections))
	for _, conn := range t.connections {
		result = append(result, *conn)
	}
	return result
}

// GetAllIncludingClosed returns both active and recently closed connections.
func (t *RelayConnTracker) GetAllIncludingClosed() []RelayConnInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]RelayConnInfo, 0, len(t.connections)+len(t.closedConnections))
	for _, conn := range t.connections {
		result = append(result, *conn)
	}
	for _, conn := range t.closedConnections {
		result = append(result, *conn)
	}
	return result
}

// cleanupLoop periodically removes old closed connections.
func (t *RelayConnTracker) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.cleanupOldConnections()
		}
	}
}

// cleanupOldConnections removes closed connections older than retention duration.
func (t *RelayConnTracker) cleanupOldConnections() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// If retention is disabled (0), clear all closed connections immediately
	if t.retentionDuration == 0 {
		for id := range t.closedConnections {
			delete(t.closedConnections, id)
		}
		return
	}

	cutoff := time.Now().Add(-t.retentionDuration)
	for id, conn := range t.closedConnections {
		if conn.EndTime != nil && conn.EndTime.Before(cutoff) {
			delete(t.closedConnections, id)
		}
	}

	// Early cleanup if map grows too large (prevent memory pressure under high churn)
	if len(t.closedConnections) > 10000 {
		cutoff := time.Now().Add(-time.Minute) // Force cleanup of older entries
		for id, conn := range t.closedConnections {
			if conn.EndTime != nil && conn.EndTime.Before(cutoff) {
				delete(t.closedConnections, id)
			}
		}
	}
}

// Subscribe registers a channel to receive connection events.
// The channel will receive events until the context is cancelled or Unsubscribe is called.
func (t *RelayConnTracker) Subscribe(ctx context.Context) <-chan RelayConnEvent {
	ch := make(chan RelayConnEvent, 100)

	t.subMu.Lock()
	t.subscribers[ch] = struct{}{}
	t.subMu.Unlock()

	// Unsubscribe when context is cancelled
	go func() {
		<-ctx.Done()
		t.Unsubscribe(ch)
		close(ch)
	}()

	return ch
}

// Unsubscribe removes a channel from receiving events.
func (t *RelayConnTracker) Unsubscribe(ch chan<- RelayConnEvent) {
	t.subMu.Lock()
	defer t.subMu.Unlock()
	delete(t.subscribers, ch)
}

// broadcast sends an event to all subscribers.
func (t *RelayConnTracker) broadcast(event RelayConnEvent) {
	t.subMu.RLock()
	defer t.subMu.RUnlock()

	for ch := range t.subscribers {
		select {
		case ch <- event:
		default:
			// Channel full, skip this subscriber
		}
	}
}

// MarshalJSON converts RelayConnEvent to JSON with Unix timestamps for SSE events.
func (e RelayConnEvent) MarshalJSON() ([]byte, error) {
	type jsonAlias struct {
		Type string `json:"type"`
		Conn struct {
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
		} `json:"conn"`
	}

	var endTime *int64
	if e.Conn.EndTime != nil {
		unix := e.Conn.EndTime.Unix()
		endTime = &unix
	}

	result := jsonAlias{
		Type: e.Type,
	}
	result.Conn.ID = e.Conn.ID
	result.Conn.SourceIP = e.Conn.SourceIP
	result.Conn.Protocol = e.Conn.Protocol
	result.Conn.Group = e.Conn.Group
	result.Conn.DstAddr = e.Conn.DstAddr
	result.Conn.DstPort = int(e.Conn.DstPort)
	result.Conn.ProxyName = e.Conn.ProxyName
	result.Conn.RunID = e.Conn.RunID
	result.Conn.StartTime = e.Conn.StartTime.Unix()
	result.Conn.EndTime = endTime
	result.Conn.BytesIn = e.Conn.BytesIn
	result.Conn.BytesOut = e.Conn.BytesOut
	result.Conn.IsActive = e.Conn.IsActive

	return json.Marshal(result)
}

// SetRetentionDuration updates the retention duration.
func (t *RelayConnTracker) SetRetentionDuration(duration time.Duration) {
	t.mu.Lock()
	t.retentionDuration = duration
	if duration == 0 {
		// Immediate cleanup when disabled
		for id := range t.closedConnections {
			delete(t.closedConnections, id)
		}
	}
	t.mu.Unlock()
}

// GetRetentionDuration returns the current retention duration.
func (t *RelayConnTracker) GetRetentionDuration() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.retentionDuration
}

// Close stops the cleanup goroutine and releases resources.
func (t *RelayConnTracker) Close() {
	t.cancel()
}
