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
	BytesIn   int64
	BytesOut  int64
}

// RelayConnEvent represents a connection lifecycle event.
type RelayConnEvent struct {
	Type  string        `json:"type"` // "created", "updated", "deleted"
	Conn  RelayConnInfo `json:"conn"`
}

// RelayConnTracker tracks active relay connections with thread-safe operations.
type RelayConnTracker struct {
	connections map[string]*RelayConnInfo
	mu          sync.RWMutex
	nextID      atomic.Uint64

	// Event broadcasting
	subscribers map[chan<- RelayConnEvent]struct{}
	subMu       sync.RWMutex
}

// NewRelayConnTracker creates a new connection tracker.
func NewRelayConnTracker() *RelayConnTracker {
	return &RelayConnTracker{
		connections: make(map[string]*RelayConnInfo),
		subscribers: make(map[chan<- RelayConnEvent]struct{}),
	}
}

// Track registers a new connection and returns its unique ID.
func (t *RelayConnTracker) Track(info RelayConnInfo) string {
	id := fmt.Sprintf("conn-%d", t.nextID.Add(1))
	info.ID = id

	t.mu.Lock()
	t.connections[id] = &info
	t.mu.Unlock()

	t.broadcast(RelayConnEvent{Type: "created", Conn: info})
	return id
}

// UpdateBytes updates the byte counts for a connection.
func (t *RelayConnTracker) UpdateBytes(id string, bytesIn, bytesOut int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if conn, ok := t.connections[id]; ok {
		conn.BytesIn = bytesIn
		conn.BytesOut = bytesOut
		t.broadcast(RelayConnEvent{Type: "updated", Conn: *conn})
	}
}

// Remove removes a connection from the tracker.
func (t *RelayConnTracker) Remove(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if conn, ok := t.connections[id]; ok {
		delete(t.connections, id)
		t.broadcast(RelayConnEvent{Type: "deleted", Conn: *conn})
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

// MarshalJSON converts RelayConnInfo to JSON for SSE events.
func (e RelayConnEvent) MarshalJSON() ([]byte, error) {
	type alias RelayConnEvent
	return json.Marshal((*alias)(&e))
}
