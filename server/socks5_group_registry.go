// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package server

import (
	"cmp"
	"slices"
	"sync"
)

// Socks5RelayGroupRegistry tracks which frpc instances (by runID) belong to which groups.
type Socks5RelayGroupRegistry struct {
	groups      map[string]map[string]struct{} // group -> set of runIDs
	runIDGroups map[string]map[string]struct{} // runID -> set of groups (reverse index)
	proxyNames  map[groupRunID]string          // (group, runID) -> proxyName
	mu          sync.RWMutex
}

type groupRunID struct {
	group string
	runID string
}

func NewSocks5RelayGroupRegistry() *Socks5RelayGroupRegistry {
	return &Socks5RelayGroupRegistry{
		groups:      make(map[string]map[string]struct{}),
		runIDGroups: make(map[string]map[string]struct{}),
		proxyNames:  make(map[groupRunID]string),
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
	r.proxyNames[groupRunID{group, runID}] = proxyName
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
		delete(r.proxyNames, groupRunID{group, runID})
	}
	delete(r.runIDGroups, runID)
}

func (r *Socks5RelayGroupRegistry) GetProxyName(group, runID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.proxyNames[groupRunID{group, runID}]
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
	slices.Sort(result)
	return result
}

type GroupDetail struct {
	Name    string              `json:"name"`
	Members []GroupMemberDetail `json:"members"`
}

type GroupMemberDetail struct {
	RunID     string `json:"runID"`
	ProxyName string `json:"proxyName"`
	Online    bool   `json:"online"`
}

func (r *Socks5RelayGroupRegistry) GetAllGroups(onlineFn func(runID string) bool) []GroupDetail {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]GroupDetail, 0, len(r.groups))
	for groupName := range r.groups {
		detail := GroupDetail{Name: groupName}
		for runID := range r.groups[groupName] {
			proxyName := r.proxyNames[groupRunID{group: groupName, runID: runID}]
			detail.Members = append(detail.Members, GroupMemberDetail{
				RunID:     runID,
				ProxyName: proxyName,
				Online:    onlineFn(runID),
			})
		}
		result = append(result, detail)
	}

	slices.SortFunc(result, func(a, b GroupDetail) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return result
}
