// Copyright 2024 The frp Authors
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
	// Create temp directory for test db
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	store := NewStateStore()
	err := store.Init(dbPath)
	assert.NoError(t, err)

	// Verify db file was created
	assert.FileExists(t, dbPath)

	err = store.Close()
	assert.NoError(t, err)
}

func TestStateStore_InitCreatesBuckets(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	store := NewStateStore()
	err := store.Init(dbPath)
	assert.NoError(t, err)
	defer store.Close()

	// Verify buckets exist by trying operations
	assert.False(t, store.IsGroupDisabled("test-group"))
	assert.False(t, store.IsClientDisabled("test-client"))
}

func TestStateStore_EnableDisableGroup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	store := NewStateStore()
	err := store.Init(dbPath)
	assert.NoError(t, err)
	defer store.Close()

	// Initially not disabled
	assert.False(t, store.IsGroupDisabled("group1"))

	// Disable
	err = store.DisableGroup("group1")
	assert.NoError(t, err)
	assert.True(t, store.IsGroupDisabled("group1"))

	// Enable
	err = store.EnableGroup("group1")
	assert.NoError(t, err)
	assert.False(t, store.IsGroupDisabled("group1"))
}

func TestStateStore_EnableDisableClient(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	store := NewStateStore()
	err := store.Init(dbPath)
	assert.NoError(t, err)
	defer store.Close()

	// Initially not disabled
	assert.False(t, store.IsClientDisabled("user.client1"))

	// Disable
	err = store.DisableClient("user.client1")
	assert.NoError(t, err)
	assert.True(t, store.IsClientDisabled("user.client1"))

	// Enable
	err = store.EnableClient("user.client1")
	assert.NoError(t, err)
	assert.False(t, store.IsClientDisabled("user.client1"))
}

func TestStateStore_PersistenceAcrossRestart(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	// First instance - disable some entries
	store1 := NewStateStore()
	err := store1.Init(dbPath)
	assert.NoError(t, err)

	err = store1.DisableGroup("group1")
	assert.NoError(t, err)
	err = store1.DisableClient("user.client1")
	assert.NoError(t, err)

	err = store1.Close()
	assert.NoError(t, err)

	// Second instance - verify state persists
	store2 := NewStateStore()
	err = store2.Init(dbPath)
	assert.NoError(t, err)
	defer store2.Close()

	assert.True(t, store2.IsGroupDisabled("group1"))
	assert.True(t, store2.IsClientDisabled("user.client1"))
	assert.False(t, store2.IsGroupDisabled("group2"))
	assert.False(t, store2.IsClientDisabled("user.client2"))
}