// Copyright 2026 The frp Authors
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
	sessions      map[string]string         // "group@userID" -> runID binding
	groupIndex    map[string]*atomic.Uint64 // group -> round-robin counter
	groupRegistry *Socks5RelayGroupRegistry
	ctlManager    *ControlManager
	mu            sync.RWMutex

	// stateStore for checking disabled state
	stateStore *StateStore
}

func NewSessionManager(groupRegistry *Socks5RelayGroupRegistry, ctlManager *ControlManager, stateStore *StateStore) *SessionManager {
	return &SessionManager{
		sessions:      make(map[string]string),
		groupIndex:    make(map[string]*atomic.Uint64),
		groupRegistry: groupRegistry,
		ctlManager:    ctlManager,
		stateStore:    stateStore,
	}
}

// SetStateStore sets the StateStore reference. Used for late binding.
func (sm *SessionManager) SetStateStore(store *StateStore) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.stateStore = store
}

// SelectFrpc selects a frpc Control for the given group and optional userID.
// When userID is present, uses session affinity keyed by "group@userID":
//   - if previously bound to a live frpc, return it (sticky session)
//   - otherwise, select a new frpc via round-robin and store the binding
//
// When userID is empty, always round-robin without session binding.
// Returns the Control and the proxyName needed for work connection dispatch.
func (sm *SessionManager) SelectFrpc(group, userID string) (*Control, string, error) {
	// Check if group is disabled
	if sm.stateStore != nil && sm.stateStore.IsGroupDisabled(group) {
		return nil, "", fmt.Errorf("group [%s] is disabled", group)
	}

	// Session affinity: only when userID is present
	if userID != "" {
		sessionKey := group + "@" + userID
		sm.mu.RLock()
		boundRunID, hasBinding := sm.sessions[sessionKey]
		sm.mu.RUnlock()

		if hasBinding {
			// Check if the bound client is disabled
			if sm.stateStore != nil && sm.stateStore.IsClientDisabled(boundRunID) {
				// Clear the binding and fall through to select a new client
				sm.mu.Lock()
				delete(sm.sessions, sessionKey)
				sm.mu.Unlock()
			} else {
				ctl, ok := sm.ctlManager.GetByID(boundRunID)
				if ok {
					proxyName := sm.groupRegistry.GetProxyName(group, boundRunID)
					return ctl, proxyName, nil
				}
			}
		}
	}

	members := sm.groupRegistry.GetGroupMembers(group)

	// Filter out disabled clients
	enabledMembers := make([]string, 0, len(members))
	for _, runID := range members {
		if sm.stateStore != nil && sm.stateStore.IsClientDisabled(runID) {
			continue
		}
		enabledMembers = append(enabledMembers, runID)
	}

	if len(enabledMembers) == 0 {
		return nil, "", fmt.Errorf("no available frpc in group [%s]", group)
	}

	sm.mu.Lock()
	counter, ok := sm.groupIndex[group]
	if !ok {
		counter = &atomic.Uint64{}
		sm.groupIndex[group] = counter
	}
	sm.mu.Unlock()

	idx := counter.Add(1) - 1
	runID := enabledMembers[idx%uint64(len(enabledMembers))]

	ctl, ok := sm.ctlManager.GetByID(runID)
	if !ok {
		return nil, "", fmt.Errorf("selected frpc [%s] is not available", runID)
	}

	// Only bind session when userID is present
	if userID != "" {
		sm.mu.Lock()
		sm.sessions[group+"@"+userID] = runID
		sm.mu.Unlock()
	}

	proxyName := sm.groupRegistry.GetProxyName(group, runID)
	if proxyName == "" {
		return nil, "", fmt.Errorf("no proxy registered for group [%s] runID [%s]", group, runID)
	}
	return ctl, proxyName, nil
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

func (sm *SessionManager) GetAllSessions() map[string]string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]string, len(sm.sessions))
	for username, runID := range sm.sessions {
		result[username] = runID
	}
	return result
}
