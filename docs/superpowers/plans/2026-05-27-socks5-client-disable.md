# SOCKS5 Client Disable Feature Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add ability to disable/enable SOCKS5 relay clients and groups in frps dashboard, with persistent state via bbolt.

**Architecture:** StateStore (bbolt) holds disabled state, integrated into Socks5RelayGroupRegistry and SessionManager.SelectFrpc() to filter disabled clients/groups during SOCKS5 forwarding selection.

**Tech Stack:** Go, bbolt (`github.com/etcd-io/bbolt`), Vue 3 + Element Plus for frontend

---

## File Structure

| File | Purpose |
|------|---------|
| `server/state_store.go` | New — bbolt state store for disabled groups/clients |
| `server/state_store_test.go` | New — unit tests for StateStore |
| `pkg/config/v1/server.go` | Modify — add `stateFile` config option |
| `pkg/config/legacy/server.go` | Modify — add `StateFile` config option |
| `server/socks5_group_registry.go` | Modify — add StateStore reference |
| `server/session_manager.go` | Modify — filter disabled in SelectFrpc() |
| `server/api_router.go` | Modify — add disable/enable API routes |
| `server/http/model/types.go` | Modify — add response model for disable API |
| `server/service.go` | Modify — initialize/close StateStore |
| `web/frps/src/views/Socks5Relay.vue` | Modify — add toggle UI |

---

### Task 1: Add bbolt dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add bbolt to go.mod**

Run: `go get github.com/etcd-io/bbolt@v1.4.0`

Expected: go.mod updated with bbolt dependency

- [ ] **Step 2: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add bbolt dependency for state persistence"
```

---

### Task 2: Add stateFile config option

**Files:**
- Modify: `pkg/config/v1/server.go`
- Modify: `pkg/config/legacy/server.go`

- [ ] **Step 1: Add StateFile to v1 ServerConfig**

In `pkg/config/v1/server.go`, add field to `ServerConfig` struct (after `ConnRetentionDuration`):

```go
// StateFile specifies the path to the bbolt database file for persistent state storage.
// Default is empty, which means no persistent state storage.
StateFile string `json:"stateFile,omitempty"`
```

- [ ] **Step 2: Add StateFile to legacy ServerCommonConf**

In `pkg/config/legacy/server.go`, add field to `ServerCommonConf` struct (around line 100, after dashboard fields):

```go
// StateFile specifies the path to the bbolt database file for persistent state storage.
// If this value is "", no persistent state storage will be used. By default, this value is "".
StateFile string `ini:"state_file" json:"state_file"`
```

- [ ] **Step 3: Update conversion function**

In `pkg/config/legacy/conversion.go`, update `Convert_ServerCommonConf_To_v1` to map the field:

Find the section where fields are mapped and add:
```go
ret.StateFile = conf.StateFile
```

- [ ] **Step 4: Commit**

```bash
git add pkg/config/v1/server.go pkg/config/legacy/server.go pkg/config/legacy/conversion.go
git commit -m "config: add stateFile option for persistent state storage"
```

---

### Task 3: Create StateStore component (TDD)

**Files:**
- Create: `server/state_store.go`
- Create: `server/state_store_test.go`

- [ ] **Step 1: Write the failing test for StateStore Init and Close**

Create `server/state_store_test.go`:

```go
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
	"os"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server -run TestStateStore -v`

Expected: FAIL — undefined: NewStateStore, StateStore

- [ ] **Step 3: Write minimal StateStore implementation**

Create `server/state_store.go`:

```go
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
	"errors"

	bbolt "github.com/etcd-io/bbolt"
)

const (
	bucketDisabledGroups  = "socks5_disabled_groups"
	bucketDisabledClients = "socks5_disabled_clients"
)

// StateStore provides persistent storage for server state using bbolt.
type StateStore struct {
	db *bbolt.DB
}

// NewStateStore creates a new StateStore instance.
func NewStateStore() *StateStore {
	return &StateStore{}
}

// Init opens or creates the bbolt database at the given path.
func (s *StateStore) Init(dbPath string) error {
	db, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		return err
	}
	s.db = db

	// Create buckets if they don't exist
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketDisabledGroups))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(bucketDisabledClients))
		if err != nil {
			return err
		}
		return nil
	})
	return err
}

// Close closes the bbolt database.
func (s *StateStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DisableGroup marks a group as disabled.
func (s *StateStore) DisableGroup(group string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledGroups))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Put([]byte(group), []byte("1"))
	})
}

// EnableGroup marks a group as enabled (removes disabled state).
func (s *StateStore) EnableGroup(group string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledGroups))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Delete([]byte(group))
	})
}

// IsGroupDisabled returns true if the group is disabled.
func (s *StateStore) IsGroupDisabled(group string) bool {
	if s.db == nil {
		return false
	}
	var disabled bool
	s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledGroups))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(group))
		disabled = v != nil && string(v) == "1"
		return nil
	})
	return disabled
}

// DisableClient marks a client as disabled.
func (s *StateStore) DisableClient(key string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledClients))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Put([]byte(key), []byte("1"))
	})
}

// EnableClient marks a client as enabled (removes disabled state).
func (s *StateStore) EnableClient(key string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledClients))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Delete([]byte(key))
	})
}

// IsClientDisabled returns true if the client is disabled.
func (s *StateStore) IsClientDisabled(key string) bool {
	if s.db == nil {
		return false
	}
	var disabled bool
	s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledClients))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		disabled = v != nil && string(v) == "1"
		return nil
	})
	return disabled
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server -run TestStateStore_Init -v`

Expected: PASS

- [ ] **Step 5: Write test for enable/disable operations**

Add to `server/state_store_test.go`:

```go
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
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./server -run TestStateStore -v`

Expected: All tests PASS

- [ ] **Step 7: Write test for persistence across restart**

Add to `server/state_store_test.go`:

```go
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
```

- [ ] **Step 8: Run test to verify persistence**

Run: `go test ./server -run TestStateStore_Persistence -v`

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add server/state_store.go server/state_store_test.go
git commit -m "feat: add StateStore for persistent disabled state with bbolt"
```

---

### Task 4: Integrate StateStore into Socks5RelayGroupRegistry

**Files:**
- Modify: `server/socks5_group_registry.go`

- [ ] **Step 1: Add StateStore reference to Socks5RelayGroupRegistry**

In `server/socks5_group_registry.go`, modify the struct:

```go
type Socks5RelayGroupRegistry struct {
	groups      map[string]map[string]struct{} // group -> set of runIDs
	runIDGroups map[string]map[string]struct{} // runID -> set of groups (reverse index)
	proxyNames  map[groupRunID]string          // (group, runID) -> proxyName
	mu          sync.RWMutex

	// StateStore for checking disabled state
	stateStore *StateStore
}
```

- [ ] **Step 2: Update constructor to accept StateStore**

```go
func NewSocks5RelayGroupRegistry(stateStore *StateStore) *Socks5RelayGroupRegistry {
	return &Socks5RelayGroupRegistry{
		groups:      make(map[string]map[string]struct{}),
		runIDGroups: make(map[string]map[string]struct{}),
		proxyNames:  make(map[groupRunID]string),
		stateStore:  stateStore,
	}
}
```

- [ ] **Step 3: Add SetStateStore method for late binding**

If StateStore may not be available at construction time, add:

```go
// SetStateStore sets the StateStore reference. Used for late binding.
func (r *Socks5RelayGroupRegistry) SetStateStore(store *StateStore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stateStore = store
}
```

- [ ] **Step 4: Commit**

```bash
git add server/socks5_group_registry.go
git commit -m "feat: integrate StateStore into Socks5RelayGroupRegistry"
```

---

### Task 5: Modify SelectFrpc() to filter disabled

**Files:**
- Modify: `server/session_manager.go`

- [ ] **Step 1: Add StateStore reference to SessionManager**

In `server/session_manager.go`, modify the struct:

```go
type SessionManager struct {
	sessions      map[string]string        // sessionKey -> runID
	groupIndex    map[string]*atomic.Uint64 // group -> round-robin index
	groupRegistry *Socks5RelayGroupRegistry
	ctlManager    *ControlManager
	mu            sync.RWMutex

	// StateStore for checking disabled state
	stateStore *StateStore
}
```

- [ ] **Step 2: Update constructor**

```go
func NewSessionManager(groupRegistry *Socks5RelayGroupRegistry, ctlManager *ControlManager, stateStore *StateStore) *SessionManager {
	return &SessionManager{
		sessions:      make(map[string]string),
		groupIndex:    make(map[string]*atomic.Uint64),
		groupRegistry: groupRegistry,
		ctlManager:    ctlManager,
		stateStore:    stateStore,
	}
}
```

- [ ] **Step 3: Modify SelectFrpc() to check disabled state**

Update the `SelectFrpc()` function to filter disabled groups and clients. Add checks at the beginning:

```go
func (sm *SessionManager) SelectFrpc(group, userID string) (*Control, string, error) {
	// Check if group is disabled
	if sm.stateStore != nil && sm.stateStore.IsGroupDisabled(group) {
		return nil, "", errors.New("group is disabled")
	}

	// ... existing session affinity check code ...

	// Before round-robin selection, filter disabled clients
	members := sm.groupRegistry.GetGroupMembers(group)
	enabledMembers := make([]string, 0, len(members))
	for _, runID := range members {
		// Get client key from Control for disabled check
		// Note: We need to check by clientKey (user.clientID), not runID
		// The registry stores runID, so we need to look up the key
		if sm.stateStore != nil {
			// For now, check by runID as a proxy
			// This will be refined when we have clientKey mapping
			if sm.stateStore.IsClientDisabled(runID) {
				continue
			}
		}
		enabledMembers = append(enabledMembers, runID)
	}

	if len(enabledMembers) == 0 {
		return nil, "", errors.New("no enabled clients available in group")
	}

	// Continue with round-robin using enabledMembers instead of members
	// ... modify existing round-robin logic to use enabledMembers ...
}
```

Note: The client key lookup needs refinement. The registry stores runID, but disabled state is keyed by `user.clientID`. We need either:
1. A mapping from runID to clientKey, or
2. Check by runID directly (simpler but less precise)

For this implementation, we'll check by runID directly. This can be refined later.

- [ ] **Step 4: Add test for SelectFrpc filtering**

Create or update `server/session_manager_test.go`:

```go
func TestSessionManager_SelectFrpc_DisabledGroup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	store := NewStateStore()
	err := store.Init(dbPath)
	require.NoError(t, err)
	defer store.Close()

	groupRegistry := NewSocks5RelayGroupRegistry(nil)
	ctlManager := NewControlManager()
	sm := NewSessionManager(groupRegistry, ctlManager, store)

	// Disable the group
	err = store.DisableGroup("test-group")
	require.NoError(t, err)

	// SelectFrpc should return error
	_, _, err = sm.SelectFrpc("test-group", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "group is disabled")
}

func TestSessionManager_SelectFrpc_DisabledClient(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_state.db")

	store := NewStateStore()
	err := store.Init(dbPath)
	require.NoError(t, err)
	defer store.Close()

	groupRegistry := NewSocks5RelayGroupRegistry(nil)
	ctlManager := NewControlManager()
	sm := NewSessionManager(groupRegistry, ctlManager, store)

	// Register a client in the group
	groupRegistry.Register("test-group", "runID1", "proxy1")

	// Disable the client
	err = store.DisableClient("runID1")
	require.NoError(t, err)

	// SelectFrpc should return error (no enabled clients)
	_, _, err = sm.SelectFrpc("test-group", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no enabled clients")
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./server -run TestSessionManager_SelectFrpc -v`

Expected: Tests may need adjustments based on existing test setup

- [ ] **Step 6: Commit**

```bash
git add server/session_manager.go server/session_manager_test.go
git commit -m "feat: filter disabled groups and clients in SelectFrpc"
```

---

### Task 6: Add disable/enable API endpoints

**Files:**
- Modify: `server/http/model/types.go`
- Modify: `server/api_router.go`

- [ ] **Step 1: Update existing response models to include disabled**

In `server/http/model/types.go`, update `Socks5RelayGroupMember`:

```go
type Socks5RelayGroupMember struct {
	RunID     string `json:"runID"`
	ProxyName string `json:"proxyName"`
	Online    bool   `json:"online"`
	Key       string `json:"key"`       // Add: client key for disable operations
	Disabled  bool   `json:"disabled"`  // Add: disabled status
}
```

Update `Socks5RelayGroupInfo`:

```go
type Socks5RelayGroupInfo struct {
	Name    string                   `json:"name"`
	Members []Socks5RelayGroupMember `json:"members"`
	Disabled bool                    `json:"disabled"` // Add: group disabled status
}
```

Add new response model for disable operations:

```go
// DisableStateResponse represents the response for disable/enable operations.
type DisableStateResponse struct {
	Success  bool   `json:"success"`
	Key      string `json:"key,omitempty"`
	Group    string `json:"group,omitempty"`
	Disabled bool   `json:"disabled"`
}
```

- [ ] **Step 2: Update apiSocks5RelayGroups to include disabled state**

In `server/api_router.go`, update `apiSocks5RelayGroups()` to populate disabled state:

```go
func (svr *Service) apiSocks5RelayGroups(ctx *httppkg.Context) (any, error) {
	groups := svr.groupRegistry.GetAllGroups(func(runID string) bool {
		_, ok := svr.ctlManager.GetByID(runID)
		return ok
	})

	resp := make([]model.Socks5RelayGroupInfo, 0, len(groups))
	for _, group := range groups {
		// Check if group is disabled
		groupDisabled := false
		if svr.stateStore != nil {
			groupDisabled = svr.stateStore.IsGroupDisabled(group.Name)
		}

		members := make([]model.Socks5RelayGroupMember, 0, len(group.Members))
		for _, m := range group.Members {
			// Get client key from registry (runID for now, refine later)
			key := m.RunID
			memberDisabled := false
			if svr.stateStore != nil {
				memberDisabled = svr.stateStore.IsClientDisabled(key)
			}

			members = append(members, model.Socks5RelayGroupMember{
				RunID:     m.RunID,
				ProxyName: m.ProxyName,
				Online:    m.Online,
				Key:       key,
				Disabled:  memberDisabled,
			})
		}

		resp = append(resp, model.Socks5RelayGroupInfo{
			Name:     group.Name,
			Members:  members,
			Disabled: groupDisabled,
		})
	}
	return resp, nil
}
```

- [ ] **Step 3: Add disable/enable routes**

In `registerRouteHandlers()` function, add routes after existing socks5relay routes:

```go
// Disable/enable endpoints
subRouter.HandleFunc("/api/socks5relay/group/{group}/disable", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayGroupDisable)).Methods("PUT")
subRouter.HandleFunc("/api/socks5relay/group/{group}/enable", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayGroupEnable)).Methods("PUT")
subRouter.HandleFunc("/api/socks5relay/client/{key}/disable", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayClientDisable)).Methods("PUT")
subRouter.HandleFunc("/api/socks5relay/client/{key}/enable", httppkg.MakeHTTPHandlerFunc(svr.apiSocks5RelayClientEnable)).Methods("PUT")
```

- [ ] **Step 4: Add handler functions**

Add these handler functions to `server/api_router.go`:

```go
func (svr *Service) apiSocks5RelayGroupDisable(ctx *httppkg.Context) (any, error) {
	group := ctx.Param("group")
	if group == "" {
		return nil, httppkg.NewError(http.StatusBadRequest, "group name required")
	}

	if svr.stateStore == nil {
		return nil, httppkg.NewError(http.StatusInternalServerError, "state store not initialized")
	}

	err := svr.stateStore.DisableGroup(group)
	if err != nil {
		return nil, httppkg.NewError(http.StatusInternalServerError, err.Error())
	}

	return model.DisableStateResponse{
		Success:  true,
		Group:    group,
		Disabled: true,
	}, nil
}

func (svr *Service) apiSocks5RelayGroupEnable(ctx *httppkg.Context) (any, error) {
	group := ctx.Param("group")
	if group == "" {
		return nil, httppkg.NewError(http.StatusBadRequest, "group name required")
	}

	if svr.stateStore == nil {
		return nil, httppkg.NewError(http.StatusInternalServerError, "state store not initialized")
	}

	err := svr.stateStore.EnableGroup(group)
	if err != nil {
		return nil, httppkg.NewError(http.StatusInternalServerError, err.Error())
	}

	return model.DisableStateResponse{
		Success:  true,
		Group:    group,
		Disabled: false,
	}, nil
}

func (svr *Service) apiSocks5RelayClientDisable(ctx *httppkg.Context) (any, error) {
	key := ctx.Param("key")
	if key == "" {
		return nil, httppkg.NewError(http.StatusBadRequest, "client key required")
	}

	if svr.stateStore == nil {
		return nil, httppkg.NewError(http.StatusInternalServerError, "state store not initialized")
	}

	err := svr.stateStore.DisableClient(key)
	if err != nil {
		return nil, httppkg.NewError(http.StatusInternalServerError, err.Error())
	}

	return model.DisableStateResponse{
		Success:  true,
		Key:      key,
		Disabled: true,
	}, nil
}

func (svr *Service) apiSocks5RelayClientEnable(ctx *httppkg.Context) (any, error) {
	key := ctx.Param("key")
	if key == "" {
		return nil, httppkg.NewError(http.StatusBadRequest, "client key required")
	}

	if svr.stateStore == nil {
		return nil, httppkg.NewError(http.StatusInternalServerError, "state store not initialized")
	}

	err := svr.stateStore.EnableClient(key)
	if err != nil {
		return nil, httppkg.NewError(http.StatusInternalServerError, err.Error())
	}

	return model.DisableStateResponse{
		Success:  true,
		Key:      key,
		Disabled: false,
	}, nil
}
```

- [ ] **Step 4: Add stateStore field to Service struct**

In `server/service.go`, add to the Service struct:

```go
type Service struct {
	// ... existing fields ...
	
	// StateStore for persistent state
	stateStore *StateStore
}
```

- [ ] **Step 5: Commit**

```bash
git add server/http/model/types.go server/api_router.go server/service.go
git commit -m "feat: add API endpoints for disabling/enabling groups and clients"
```

---

### Task 7: Initialize StateStore in Service

**Files:**
- Modify: `server/service.go`

- [ ] **Step 1: Initialize StateStore in NewService**

In `server/service.go`, in the `NewService()` function, after other component initialization, add:

Find the section where components are initialized and add:

```go
// Initialize StateStore if stateFile is configured
if cfg.StateFile != "" {
	svr.stateStore = NewStateStore()
	if err := svr.stateStore.Init(cfg.StateFile); err != nil {
		return nil, fmt.Errorf("failed to initialize state store: %v", err)
	}
}
```

- [ ] **Step 2: Pass StateStore to dependent components**

Update the initialization of `groupRegistry` and `sessionManager`:

```go
// Create Socks5RelayGroupRegistry with StateStore
svr.groupRegistry = NewSocks5RelayGroupRegistry(svr.stateStore)

// Create SessionManager with StateStore
svr.sessionManager = NewSessionManager(svr.groupRegistry, svr.ctlManager, svr.stateStore)
```

- [ ] **Step 3: Close StateStore in Service.Close()**

In the `Close()` method, add cleanup:

```go
func (svr *Service) Close() error {
	// ... existing cleanup code ...

	// Close StateStore
	if svr.stateStore != nil {
		svr.stateStore.Close()
	}

	return nil
}
```

- [ ] **Step 4: Register in ResourceController**

Update the ResourceController registration:

```go
svr.rc.StateStore = svr.stateStore
```

Add `StateStore` field to `ResourceController` struct in `server/controller/resource.go`:

```go
type ResourceController struct {
	// ... existing fields ...
	
	// StateStore for state management
	StateStore *StateStore
}
```

- [ ] **Step 5: Commit**

```bash
git add server/service.go server/controller/resource.go
git commit -m "feat: initialize and close StateStore in Service lifecycle"
```

---

### Task 8: Add toggle UI in SOCKS5 Relay page

**Files:**
- Modify: `web/frps/src/types/socks5relay.ts`
- Modify: `web/frps/src/api/socks5relay.ts`
- Modify: `web/frps/src/views/Socks5Relay.vue`

- [ ] **Step 1: Update types for disabled state**

In `web/frps/src/types/socks5relay.ts`, modify interfaces:

```typescript
export interface Socks5RelayGroupMember {
  runID: string
  proxyName: string
  online: boolean
  disabled: boolean    // Add: disabled status for this member
  key: string          // Add: client key (user.clientID or runID fallback)
}

export interface Socks5RelayGroupInfo {
  name: string
  members: Socks5RelayGroupMember[]
  disabled: boolean    // Add: disabled status for this group
}
```

- [ ] **Step 2: Add API functions for disable/enable**

In `web/frps/src/api/socks5relay.ts`, add new functions:

```typescript
export const disableSocks5RelayGroup = (group: string) => {
  return http.put<{ success: boolean; disabled: boolean }>(`../api/socks5relay/group/${group}/disable`)
}

export const enableSocks5RelayGroup = (group: string) => {
  return http.put<{ success: boolean; disabled: boolean }>(`../api/socks5relay/group/${group}/enable`)
}

export const disableSocks5RelayClient = (key: string) => {
  return http.put<{ success: boolean; disabled: boolean }>(`../api/socks5relay/client/${key}/disable`)
}

export const enableSocks5RelayClient = (key: string) => {
  return http.put<{ success: boolean; disabled: boolean }>(`../api/socks5relay/client/${key}/enable`)
}
```

- [ ] **Step 3: Add toggle to group card header**

In `web/frps/src/views/Socks5Relay.vue`, modify the group card header (around line 58-65):

Replace:
```vue
<div class="group-header">
  <span class="group-name">{{ group.name }}</span>
  <el-tag size="small" type="info">
    {{ group.members.length }} member{{ group.members.length !== 1 ? 's' : '' }}
    ({{ onlineCount(group.members) }} online)
  </el-tag>
</div>
```

With:
```vue
<div class="group-header">
  <div class="group-header-left">
    <span class="group-name">{{ group.name }}</span>
    <el-tag size="small" :type="group.disabled ? 'danger' : 'info'">
      {{ group.disabled ? 'Disabled' : `${group.members.length} member${group.members.length !== 1 ? 's' : ''} (${onlineCount(group.members)} online)` }}
    </el-tag>
  </div>
  <el-switch
    v-model="group.enabled"
    :loading="group.loading"
    active-text="Enabled"
    inactive-text="Disabled"
    @change="handleGroupToggle(group)"
  />
</div>
```

- [ ] **Step 4: Add toggle to member table**

In the members table (around line 67-77), add a new column after "Online":

```vue
<el-table :data="group.members" class="members-table">
  <el-table-column prop="proxyName" label="Proxy Name" />
  <el-table-column prop="runID" label="Run ID" />
  <el-table-column label="Online" width="80">
    <template #default="{ row }">
      <el-tag :type="row.online ? 'success' : 'danger'" size="small">
        {{ row.online ? 'Yes' : 'No' }}
      </el-tag>
    </template>
  </el-table-column>
  <el-table-column label="Enabled" width="100">
    <template #default="{ row }">
      <el-switch
        v-model="row.enabled"
        :disabled="group.disabled"
        :loading="row.loading"
        size="small"
        @change="handleClientToggle(group, row)"
      />
    </template>
  </el-table-column>
</el-table>
```

- [ ] **Step 5: Add handler functions in script section**

In `<script setup>`, add imports and handlers:

```typescript
import {
  getSocks5RelayGroups,
  getSocks5RelaySessions,
  disableSocks5RelayGroup,
  enableSocks5RelayGroup,
  disableSocks5RelayClient,
  enableSocks5RelayClient,
} from '../api/socks5relay'
```

Add extended type for local state:
```typescript
interface GroupWithState extends Socks5RelayGroupInfo {
  enabled: boolean
  loading: boolean
}

interface MemberWithState extends Socks5RelayGroupMember {
  enabled: boolean
  loading: boolean
}

const groups = ref<GroupWithState[]>([])
```

Add handler functions:
```typescript
const handleGroupToggle = async (group: GroupWithState) => {
  group.loading = true
  try {
    if (group.enabled) {
      await enableSocks5RelayGroup(group.name)
      group.disabled = false
    } else {
      await disableSocks5RelayGroup(group.name)
      group.disabled = true
    }
    ElMessage({
      showClose: true,
      message: `Group "${group.name}" ${group.enabled ? 'enabled' : 'disabled'}`,
      type: 'success',
    })
  } catch (error: any) {
    // Revert on error
    group.enabled = !group.enabled
    group.disabled = !group.enabled
    ElMessage({
      showClose: true,
      message: 'Failed to toggle group: ' + error.message,
      type: 'error',
    })
  } finally {
    group.loading = false
  }
}

const handleClientToggle = async (group: GroupWithState, member: MemberWithState) => {
  member.loading = true
  try {
    const key = member.key || member.runID // Use runID as fallback
    if (member.enabled) {
      await enableSocks5RelayClient(key)
      member.disabled = false
    } else {
      await disableSocks5RelayClient(key)
      member.disabled = true
    }
    ElMessage({
      showClose: true,
      message: `Client "${key}" ${member.enabled ? 'enabled' : 'disabled'}`,
      type: 'success',
    })
  } catch (error: any) {
    // Revert on error
    member.enabled = !member.enabled
    member.disabled = !member.enabled
    ElMessage({
      showClose: true,
      message: 'Failed to toggle client: ' + error.message,
      type: 'error',
    })
  } finally {
    member.loading = false
  }
}
```

- [ ] **Step 6: Update fetchGroups to map state**

Update the `fetchGroups` function to add local state:

```typescript
const fetchGroups = async () => {
  loadingGroups.value = true
  try {
    const data = await getSocks5RelayGroups()
    groups.value = data.map(g => ({
      ...g,
      enabled: !g.disabled,
      loading: false,
      members: g.members.map(m => ({
        ...m,
        enabled: !m.disabled,
        loading: false,
      })),
    }))
  } catch (error: any) {
    ElMessage({
      showClose: true,
      message: 'Failed to fetch groups: ' + error.message,
      type: 'error',
    })
  } finally {
    loadingGroups.value = false
  }
}
```

- [ ] **Step 7: Add CSS for group header layout**

In `<style scoped>`, add:

```css
.group-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.group-header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}
```

- [ ] **Step 8: Build and test frontend**

Run: `cd web/frps && npm run build`

Expected: Build succeeds

- [ ] **Step 9: Commit**

```bash
git add web/frps/src/types/socks5relay.ts web/frps/src/api/socks5relay.ts web/frps/src/views/Socks5Relay.vue
cd web/frps && npm run build && cd ..
git add web/frps/dist
git commit -m "feat: add disable/enable toggle UI in SOCKS5 Relay dashboard"
```

---

### Task 9: Run full test suite and verify

**Files:**
- All modified files

- [ ] **Step 1: Run unit tests**

Run: `make test`

Expected: All tests pass

- [ ] **Step 2: Run e2e tests**

Run: `make e2e`

Expected: E2E tests pass (may need to skip if no SOCKS5 e2e tests exist)

- [ ] **Step 3: Manual verification**

Build frps and start with stateFile config:

```bash
make frps
./bin/frps -c test/frps.ini
```

In config, add:
```ini
state_file = "./frps_state.db"
```

Test via API:
```bash
curl -X PUT http://localhost:7500/api/socks5relay/group/mygroup/disable -u admin:admin
curl http://localhost:7500/api/socks5relay/groups -u admin:admin
```

- [ ] **Step 4: Final commit if needed**

```bash
git status
# Commit any remaining changes
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Add bbolt dependency | go.mod |
| 2 | Add stateFile config | config files |
| 3 | Create StateStore (TDD) | state_store.go |
| 4 | Integrate into registry | socks5_group_registry.go |
| 5 | Filter in SelectFrpc() | session_manager.go |
| 6 | Add API endpoints | api_router.go, types.go |
| 7 | Initialize in Service | service.go |
| 8 | Add frontend UI | Socks5Relay.vue |
| 9 | Test and verify | all |