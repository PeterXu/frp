// Copyright 2026 The frP Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package server

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// SessionManager manages username→frpc session binding with round-robin selection.
type SessionManager struct {
	sessions      map[string]string         // username -> runID binding
	groupIndex    map[string]*atomic.Uint64 // group -> round-robin counter
	groupRegistry *Socks5RelayGroupRegistry
	ctlManager    *ControlManager
	mu            sync.RWMutex
}

func NewSessionManager(groupRegistry *Socks5RelayGroupRegistry, ctlManager *ControlManager) *SessionManager {
	return &SessionManager{
		sessions:      make(map[string]string),
		groupIndex:    make(map[string]*atomic.Uint64),
		groupRegistry: groupRegistry,
		ctlManager:    ctlManager,
	}
}

// SelectFrpc selects a frpc Control for the given username (= group name).
// Uses session affinity: if the username was previously bound to a live frpc, return it.
// Otherwise, select a new frpc via round-robin from the group.
func (sm *SessionManager) SelectFrpc(username string) (*Control, error) {
	sm.mu.RLock()
	boundRunID, hasBinding := sm.sessions[username]
	sm.mu.RUnlock()

	if hasBinding {
		ctl, ok := sm.ctlManager.GetByID(boundRunID)
		if ok {
			return ctl, nil
		}
	}

	members := sm.groupRegistry.GetGroupMembers(username)
	if len(members) == 0 {
		return nil, fmt.Errorf("no available frpc in group [%s]", username)
	}

	sm.mu.Lock()
	counter, ok := sm.groupIndex[username]
	if !ok {
		counter = &atomic.Uint64{}
		sm.groupIndex[username] = counter
	}
	sm.mu.Unlock()

	idx := counter.Add(1) - 1
	runID := members[idx%uint64(len(members))]

	ctl, ok := sm.ctlManager.GetByID(runID)
	if !ok {
		return nil, fmt.Errorf("selected frpc [%s] is not available", runID)
	}

	sm.mu.Lock()
	sm.sessions[username] = runID
	sm.mu.Unlock()

	return ctl, nil
}

// RemoveSession removes all session bindings for a given runID (called on frpc disconnect).
func (sm *SessionManager) RemoveSession(runID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for username, boundRunID := range sm.sessions {
		if boundRunID == runID {
			delete(sm.sessions, username)
		}
	}
}
