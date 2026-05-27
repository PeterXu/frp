# SOCKS5 Relay Client Disable Feature Design

## Overview

Add ability to disable clients and groups in frps dashboard SOCKS5 Relay page. Disabled clients/groups are excluded from SOCKS5 forwarding, but other proxy types remain unaffected.

## Requirements

- Disable individual clients for SOCKS5 forwarding
- Disable entire groups for SOCKS5 forwarding
- State persists across frps restarts (bbolt storage)
- State persists across client reconnects (keyed by stable identifier)
- Existing SOCKS5 connections continue uninterrupted
- Only new connections are blocked

## Architecture

### Storage: bbolt

**Library:** `github.com/etcd-io/bbolt`

**Config option:** Add `stateFile` to `ServerConfig` (v1) and `ServerCommonConf` (legacy)
- Default: `{config_file_dir}/frps_state.db` (same directory as frps config file)
- If config file path unknown: `./frps_state.db` (working directory)

**Buckets:**
- `socks5_disabled_groups` → key: groupName, value: []byte("1") or []byte("0")
- `socks5_disabled_clients` → key: clientKey (user.clientID or user.runID), value: []byte("1") or []byte("0")

### New Component: StateStore

**File:** `server/state_store.go`

```go
type StateStore struct {
    db *bbolt.DB
}

func (s *StateStore) Init(dbPath string) error
func (s *StateStore) Close() error

func (s *StateStore) DisableGroup(group string) error
func (s *StateStore) EnableGroup(group string) error
func (s *StateStore) IsGroupDisabled(group string) bool

func (s *StateStore) DisableClient(key string) error
func (s *StateStore) EnableClient(key string) error
func (s *StateStore) IsClientDisabled(key string) bool
```

### Integration: Socks5RelayGroupRegistry

- Holds reference to `StateStore`
- Selection logic queries `StateStore.IsGroupDisabled()` and `StateStore.IsClientDisabled()`

### Initialization

**File:** `server/service.go` — frps main service initialization

- `StateStore.Init()` called during `Service.Start()`
- `StateStore.Close()` called during `Service.Close()`
- StateStore passed to `Socks5RelayGroupRegistry` and `SessionManager` constructors

### Selection Logic Changes

**File:** `server/session_manager.go` — `SelectFrpc()` function

```go
func (sm *SessionManager) SelectFrpc(group string) (*Control, error) {
    // 1. Check if group is disabled
    if sm.stateStore.IsGroupDisabled(group) {
        return nil, errors.New("group is disabled")
    }

    // 2. Get all clients in group from registry
    clients := sm.registry.GetGroupMembers(group)

    // 3. Filter out disabled clients
    enabledClients := []string{}
    for _, clientKey := range clients {
        if !sm.stateStore.IsClientDisabled(clientKey) {
            enabledClients = append(enabledClients, clientKey)
        }
    }

    // 4. If no enabled clients, return error
    if len(enabledClients) == 0 {
        return nil, errors.New("no enabled clients available in group")
    }

    // 5. Select from enabled clients (round-robin or sticky session)
    // ... existing selection logic
}
```

## API Endpoints

**File:** `server/api_router.go`

| Method | Path | Description |
|--------|------|-------------|
| PUT | `/api/socks5relay/group/{group}/disable` | Disable entire group |
| PUT | `/api/socks5relay/group/{group}/enable` | Enable group |
| PUT | `/api/socks5relay/client/{key}/disable` | Disable client |
| PUT | `/api/socks5relay/client/{key}/enable` | Enable client |

**Response format:**
```json
{
    "success": true,
    "disabled": true
}
```

**Error responses:**
- 400 Bad Request — invalid group/key format
- 500 Internal Server Error — bbolt operation failure

## Frontend: SOCKS5 Relay Page

**File:** `web/frps/src/views/Socks5Relay.vue`

**Group row:**
- Toggle switch (el-switch) to enable/disable entire group
- Status badge: "Enabled" / "Disabled"

**Client rows:**
- Each client has individual toggle
- Status: "Enabled" / "Disabled"

**Visual behavior:**
- Group disabled → client toggles dimmed/locked
- Group enabled → client toggles active

**API calls:**
- Group toggle → `PUT /api/socks5relay/group/{group}/disable|enable`
- Client toggle → `PUT /api/socks5relay/client/{key}/disable|enable`

## Error Handling & Edge Cases

| Scenario | Behavior |
|----------|----------|
| All clients in group disabled | `SelectFrpc()` returns error, SOCKS5 connection rejected |
| Group disabled | All clients implicitly disabled, SOCKS5 connections rejected immediately |
| frps restart (db exists) | Load state from bbolt, disabled state persists |
| frps restart (db missing) | Create new db, all groups/clients enabled by default |
| Client reconnects | Disabled state persists (keyed by stable clientKey) |
| Group/client not in registry | Stale entries kept in db, ignored during selection |
| Concurrent access | bbolt handles via transactions, no mutex needed |

## Testing

### Unit Tests

**`server/state_store_test.go`:**
- Enable/disable group → verify state
- Enable/disable client → verify state
- Close and reopen db → verify state survives
- Concurrent read/write operations

**`server/socks5_group_registry_test.go`:**
- `SelectFrpc()` skips disabled clients
- `SelectFrpc()` error when all clients disabled
- `SelectFrpc()` error when group disabled

### E2E Tests

- Disable client via API → new SOCKS5 requests don't route to it
- Disable group via API → SOCKS5 requests rejected
- Restart frps → disabled state persists
- Enable after disable → traffic resumes

## Implementation Files

| File | Changes |
|------|---------|
| `server/state_store.go` | New file — bbolt state store |
| `server/service.go` | Initialize and close StateStore |
| `server/socks5_group_registry.go` | Add StateStore reference, helper methods |
| `server/session_manager.go` | Modify `SelectFrpc()` to filter disabled |
| `server/api_router.go` | Add disable/enable API endpoints |
| `server/http/controller.go` | Add API handlers |
| `pkg/config/v1/server.go` | Add `stateFile` config option |
| `pkg/config/legacy/server.go` | Add `StateFile` config option |
| `web/frps/src/views/Socks5Relay.vue` | Add toggle UI |
| `go.mod` | Add `github.com/etcd-io/bbolt` dependency |