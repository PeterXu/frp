// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package server

import "sync"

// Socks5RelayGroupRegistry tracks which frpc instances (by runID) belong to which groups.
type Socks5RelayGroupRegistry struct {
	groups         map[string]map[string]struct{} // group -> set of runIDs
	runIDGroups    map[string]map[string]struct{} // runID -> set of groups (reverse index)
	runIDProxyName map[string]string              // runID -> proxyName (for dispatch)
	mu             sync.RWMutex
}

func NewSocks5RelayGroupRegistry() *Socks5RelayGroupRegistry {
	return &Socks5RelayGroupRegistry{
		groups:         make(map[string]map[string]struct{}),
		runIDGroups:    make(map[string]map[string]struct{}),
		runIDProxyName: make(map[string]string),
	}
}

func (r *Socks5RelayGroupRegistry) Register(group, runID, proxyName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.groups[group]; !ok {
		r.groups[group] = make(map[string]struct{})
	}
	r.groups[group][runID] = struct{}{}

	if _, ok := r.runIDGroups[runID]; !ok {
		r.runIDGroups[runID] = make(map[string]struct{})
	}
	r.runIDGroups[runID][group] = struct{}{}
	r.runIDProxyName[runID] = proxyName
}

func (r *Socks5RelayGroupRegistry) Unregister(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	groups, ok := r.runIDGroups[runID]
	if !ok {
		return
	}
	for group := range groups {
		if members, ok := r.groups[group]; ok {
			delete(members, runID)
			if len(members) == 0 {
				delete(r.groups, group)
			}
		}
	}
	delete(r.runIDGroups, runID)
	delete(r.runIDProxyName, runID)
}

func (r *Socks5RelayGroupRegistry) GetProxyName(runID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.runIDProxyName[runID]
}

func (r *Socks5RelayGroupRegistry) GetGroupMembers(group string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	members, ok := r.groups[group]
	if !ok {
		return nil
	}
	result := make([]string, 0, len(members))
	for runID := range members {
		result = append(result, runID)
	}
	return result
}
