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
	"errors"

	bbolt "go.etcd.io/bbolt"
)

const (
	bucketDisabledGroups  = "socks5_disabled_groups"
	bucketDisabledClients = "socks5_disabled_clients"
)

// StateStore provides persistent storage for server state using bbolt.
// It is a write-through persistence layer — the registry holds in-memory
// disabled flags and calls StateStore only when toggling state.
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

	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(bucketDisabledGroups)); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists([]byte(bucketDisabledClients))
		return err
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

// PersistDisabledGroup writes a disabled group entry to state.db.
func (s *StateStore) PersistDisabledGroup(group string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledGroups))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Put([]byte(group), []byte("1"))
	})
}

// RemoveDisabledGroup deletes a disabled group entry from state.db.
func (s *StateStore) RemoveDisabledGroup(group string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledGroups))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Delete([]byte(group))
	})
}

// PersistDisabledClient writes a disabled client entry to state.db.
func (s *StateStore) PersistDisabledClient(key string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledClients))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Put([]byte(key), []byte("1"))
	})
}

// RemoveDisabledClient deletes a disabled client entry from state.db.
func (s *StateStore) RemoveDisabledClient(key string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledClients))
		if b == nil {
			return errors.New("bucket not found")
		}
		return b.Delete([]byte(key))
	})
}

// LoadDisabledGroups returns all disabled group names from state.db.
func (s *StateStore) LoadDisabledGroups() (map[string]bool, error) {
	result := make(map[string]bool)
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledGroups))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			result[string(k)] = true
			return nil
		})
	})
	return result, err
}

// LoadDisabledClients returns all disabled client keys from state.db.
func (s *StateStore) LoadDisabledClients() (map[string]bool, error) {
	result := make(map[string]bool)
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucketDisabledClients))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			result[string(k)] = true
			return nil
		})
	})
	return result, err
}
