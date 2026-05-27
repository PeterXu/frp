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
	sessions      map[string]string              // "group@userID" -> runID binding
	runIDSessions map[string]map[string]struct{} // runID -> set of sessionKeys (reverse index)
	groupIndex    map[string]*atomic.Uint64      // group -> round-robin counter
	groupRegistry *Socks5RelayGroupRegistry
	ctlManager    *ControlManager
	mu            sync.RWMutex
}

func NewSessionManager(groupRegistry *Socks5RelayGroupRegistry, ctlManager *ControlManager) *SessionManager {
	return &SessionManager{
		sessions:      make(map[string]string),
		runIDSessions: make(map[string]map[string]struct{}),
		groupIndex:    make(map[string]*atomic.Uint64),
		groupRegistry: groupRegistry,
		ctlManager:    ctlManager,
	}
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
	if sm.groupRegistry.IsGroupDisabled(group) {
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
			if sm.groupRegistry.IsClientDisabled(boundRunID) {
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
		if sm.groupRegistry.IsClientDisabled(runID) {
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

	// Try members starting from round-robin position, skip offline ones
	startIdx := counter.Add(1) - 1
	for i := 0; i < len(enabledMembers); i++ {
		runID := enabledMembers[(startIdx+uint64(i))%uint64(len(enabledMembers))]
		ctl, ok := sm.ctlManager.GetByID(runID)
		if !ok {
			continue // skip offline member
		}

		proxyName := sm.groupRegistry.GetProxyName(group, runID)
		if proxyName == "" {
			continue // skip member without proxy
		}

		// Only bind session after confirming proxyName is valid
		if userID != "" {
			sm.setBinding(group+"@"+userID, runID)
		}
		return ctl, proxyName, nil
	}

	return nil, "", fmt.Errorf("no online frpc in group [%s]", group)
}

// setBinding stores a session binding and maintains the reverse index.
func (sm *SessionManager) setBinding(sessionKey, runID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Clean up old binding's reverse index entry
	if oldRunID, ok := sm.sessions[sessionKey]; ok {
		if keys, ok := sm.runIDSessions[oldRunID]; ok {
			delete(keys, sessionKey)
			if len(keys) == 0 {
				delete(sm.runIDSessions, oldRunID)
			}
		}
	}

	sm.sessions[sessionKey] = runID

	if _, ok := sm.runIDSessions[runID]; !ok {
		sm.runIDSessions[runID] = make(map[string]struct{})
	}
	sm.runIDSessions[runID][sessionKey] = struct{}{}
}

// RemoveSession removes all session bindings for a given runID (called on frpc disconnect).
func (sm *SessionManager) RemoveSession(runID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	keys, ok := sm.runIDSessions[runID]
	if !ok {
		return
	}
	for sessionKey := range keys {
		delete(sm.sessions, sessionKey)
	}
	delete(sm.runIDSessions, runID)
}

// ClearBinding removes the session binding for a specific session key.
// Called when a work connection to the bound frpc fails, so the next request
// picks a different frpc instead of retrying the same dead one.
func (sm *SessionManager) ClearBinding(sessionKey string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	runID, ok := sm.sessions[sessionKey]
	if !ok {
		return
	}
	delete(sm.sessions, sessionKey)

	if keys, ok := sm.runIDSessions[runID]; ok {
		delete(keys, sessionKey)
		if len(keys) == 0 {
			delete(sm.runIDSessions, runID)
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
