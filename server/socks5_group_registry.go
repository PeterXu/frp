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
	"fmt"
	"slices"
	"sync"
)

// Socks5RelayGroupRegistry tracks which frpc instances (by runID) belong to which groups.
// It also holds in-memory disabled flags for groups and clients, persisted via StateStore.
type Socks5RelayGroupRegistry struct {
	groups      map[string]map[string]struct{} // group -> set of runIDs
	runIDGroups map[string]map[string]struct{} // runID -> set of groups (reverse index)
	proxyNames  map[groupRunID]string          // (group, runID) -> proxyName
	mu          sync.RWMutex

	// In-memory disabled flags (source of truth for reads).
	disabledGroups  map[string]bool // group -> disabled
	disabledClients map[string]bool // runID -> disabled

	// stateStore for persistence only (nil if stateFile not configured).
	stateStore *StateStore

	// OnClientDisabled is called after a client is successfully disabled.
	OnClientDisabled func(runID string)
}

type groupRunID struct {
	group string
	runID string
}

func NewSocks5RelayGroupRegistry() *Socks5RelayGroupRegistry {
	return &Socks5RelayGroupRegistry{
		groups:          make(map[string]map[string]struct{}),
		runIDGroups:     make(map[string]map[string]struct{}),
		proxyNames:      make(map[groupRunID]string),
		disabledGroups:  make(map[string]bool),
		disabledClients: make(map[string]bool),
	}
}

// SetStateStore sets the StateStore and loads persisted disabled state into memory.
func (r *Socks5RelayGroupRegistry) SetStateStore(store *StateStore) error {
	// Load from bbolt first, then assign under lock to avoid TOCTOU.
	groups, err := store.LoadDisabledGroups()
	if err != nil {
		return fmt.Errorf("load disabled groups: %w", err)
	}

	clients, err := store.LoadDisabledClients()
	if err != nil {
		return fmt.Errorf("load disabled clients: %w", err)
	}

	r.mu.Lock()
	r.stateStore = store
	r.disabledGroups = groups
	r.disabledClients = clients
	r.mu.Unlock()
	return nil
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

	groups, ok := r.runIDGroups[runID]
	if !ok {
		r.mu.Unlock()
		return
	}

	var emptyGroups []string
	for group := range groups {
		if members, ok := r.groups[group]; ok {
			delete(members, runID)
			if len(members) == 0 {
				delete(r.groups, group)
				delete(r.disabledGroups, group)
				emptyGroups = append(emptyGroups, group)
			}
		}
		delete(r.proxyNames, groupRunID{group, runID})
	}
	delete(r.runIDGroups, runID)
	wasDisabled := r.disabledClients[runID]
	delete(r.disabledClients, runID)
	store := r.stateStore
	r.mu.Unlock()

	// Persist cleanup to bbolt outside the lock.
	if store != nil && wasDisabled {
		_ = store.RemoveDisabledClient(runID)
	}
	for _, g := range emptyGroups {
		if store != nil {
			_ = store.RemoveDisabledGroup(g)
		}
	}
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

// IsGroupDisabled returns true if the group is disabled (in-memory check).
func (r *Socks5RelayGroupRegistry) IsGroupDisabled(group string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.disabledGroups[group]
}

// IsClientDisabled returns true if the client (runID) is disabled (in-memory check).
func (r *Socks5RelayGroupRegistry) IsClientDisabled(runID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.disabledClients[runID]
}

// DisableGroup disables a group: sets in-memory flag and persists if stateStore is configured.
func (r *Socks5RelayGroupRegistry) DisableGroup(group string) error {
	r.mu.Lock()
	if _, ok := r.groups[group]; !ok {
		r.mu.Unlock()
		return fmt.Errorf("group [%s] not found", group)
	}
	r.disabledGroups[group] = true
	store := r.stateStore
	r.mu.Unlock()

	if store != nil {
		if err := store.PersistDisabledGroup(group); err != nil {
			r.mu.Lock()
			delete(r.disabledGroups, group)
			r.mu.Unlock()
			return err
		}
	}
	return nil
}

// EnableGroup enables a group: clears in-memory flag and removes from state.db if configured.
func (r *Socks5RelayGroupRegistry) EnableGroup(group string) error {
	r.mu.Lock()
	if _, ok := r.groups[group]; !ok {
		r.mu.Unlock()
		return fmt.Errorf("group [%s] not found", group)
	}
	delete(r.disabledGroups, group)
	store := r.stateStore
	r.mu.Unlock()

	if store != nil {
		if err := store.RemoveDisabledGroup(group); err != nil {
			r.mu.Lock()
			r.disabledGroups[group] = true
			r.mu.Unlock()
			return err
		}
	}
	return nil
}

// DisableClient disables a client: sets in-memory flag and persists if stateStore is configured.
func (r *Socks5RelayGroupRegistry) DisableClient(runID string) error {
	r.mu.Lock()
	if _, ok := r.runIDGroups[runID]; !ok {
		r.mu.Unlock()
		return fmt.Errorf("client [%s] not found", runID)
	}
	r.disabledClients[runID] = true
	store := r.stateStore
	r.mu.Unlock()

	if store != nil {
		if err := store.PersistDisabledClient(runID); err != nil {
			r.mu.Lock()
			delete(r.disabledClients, runID)
			r.mu.Unlock()
			return err
		}
	}

	if r.OnClientDisabled != nil {
		r.OnClientDisabled(runID)
	}
	return nil
}

// EnableClient enables a client: clears in-memory flag and removes from state.db if configured.
func (r *Socks5RelayGroupRegistry) EnableClient(runID string) error {
	r.mu.Lock()
	if _, ok := r.runIDGroups[runID]; !ok {
		r.mu.Unlock()
		return fmt.Errorf("client [%s] not found", runID)
	}
	delete(r.disabledClients, runID)
	store := r.stateStore
	r.mu.Unlock()

	if store != nil {
		if err := store.RemoveDisabledClient(runID); err != nil {
			r.mu.Lock()
			r.disabledClients[runID] = true
			r.mu.Unlock()
			return err
		}
	}
	return nil
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
