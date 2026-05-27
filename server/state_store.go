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

	bbolt "go.etcd.io/bbolt"
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