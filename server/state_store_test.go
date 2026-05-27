// Copyright 2026 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStateStore_InitAndClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	store := NewStateStore()
	err := store.Init(dbPath)
	assert.NoError(t, err)

	assert.FileExists(t, dbPath)

	err = store.Close()
	assert.NoError(t, err)
}

func TestStateStore_PersistAndLoadDisabledGroups(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	store := NewStateStore()
	err := store.Init(dbPath)
	assert.NoError(t, err)
	defer store.Close()

	// Initially empty
	groups, err := store.LoadDisabledGroups()
	assert.NoError(t, err)
	assert.Empty(t, groups)

	// Persist disabled group
	err = store.PersistDisabledGroup("group1")
	assert.NoError(t, err)

	groups, err = store.LoadDisabledGroups()
	assert.NoError(t, err)
	assert.True(t, groups["group1"])

	// Remove disabled group
	err = store.RemoveDisabledGroup("group1")
	assert.NoError(t, err)

	groups, err = store.LoadDisabledGroups()
	assert.NoError(t, err)
	assert.False(t, groups["group1"])
}

func TestStateStore_PersistAndLoadDisabledClients(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	store := NewStateStore()
	err := store.Init(dbPath)
	assert.NoError(t, err)
	defer store.Close()

	// Initially empty
	clients, err := store.LoadDisabledClients()
	assert.NoError(t, err)
	assert.Empty(t, clients)

	// Persist disabled client
	err = store.PersistDisabledClient("user.client1")
	assert.NoError(t, err)

	clients, err = store.LoadDisabledClients()
	assert.NoError(t, err)
	assert.True(t, clients["user.client1"])

	// Remove disabled client
	err = store.RemoveDisabledClient("user.client1")
	assert.NoError(t, err)

	clients, err = store.LoadDisabledClients()
	assert.NoError(t, err)
	assert.False(t, clients["user.client1"])
}

func TestStateStore_PersistenceAcrossRestart(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	// First instance - persist some entries
	store1 := NewStateStore()
	err := store1.Init(dbPath)
	assert.NoError(t, err)

	err = store1.PersistDisabledGroup("group1")
	assert.NoError(t, err)
	err = store1.PersistDisabledClient("user.client1")
	assert.NoError(t, err)

	err = store1.Close()
	assert.NoError(t, err)

	// Second instance - verify state persists
	store2 := NewStateStore()
	err = store2.Init(dbPath)
	assert.NoError(t, err)
	defer store2.Close()

	groups, err := store2.LoadDisabledGroups()
	assert.NoError(t, err)
	assert.True(t, groups["group1"])
	assert.False(t, groups["group2"])

	clients, err := store2.LoadDisabledClients()
	assert.NoError(t, err)
	assert.True(t, clients["user.client1"])
	assert.False(t, clients["user.client2"])
}
