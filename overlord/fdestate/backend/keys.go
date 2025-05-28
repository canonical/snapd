// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package backend

import (
	"fmt"
	"sync"
	"time"

	"github.com/snapcore/snapd/secboot/keys"
)

type RecoveryKeyStore interface {
	// AddRecoveryKey adds a recovery key with the specified id.
	AddRecoveryKey(keyID string, rkeyInfo RecoveryKeyInfo) (err error)
	// GetRecoveryKey gets the recovery key associated with the specified id.
	GetRecoveryKey(keyID string) (rkeyInfo RecoveryKeyInfo, err error)
	// DeleteRecoveryKey deletes the recovery key associated with the specified id.
	DeleteRecoveryKey(keyID string) error
}

// NewInMemoryRecoveryKeyStore returns a memory-backed recovery key store.
//
// Note: This store will not survive snapd restarts.
func NewInMemoryRecoveryKeyStore() RecoveryKeyStore {
	return &inMemoryRecoveryKeyStore{
		rkeys: make(map[string]RecoveryKeyInfo),
	}
}

type RecoveryKeyInfo struct {
	Key keys.RecoveryKey `json:"key"`
	// Expiration indicates the expiration date for the recovery key.
	// If unset, this means that the key will never expire.
	Expiration time.Time `json:"expiration,omitzero"`
}

func (rkeyInfo *RecoveryKeyInfo) Expired(currTime time.Time) bool {
	if rkeyInfo.Expiration.IsZero() {
		return false
	}
	return currTime.After(rkeyInfo.Expiration)
}

type inMemoryRecoveryKeyStore struct {
	rkeys map[string]RecoveryKeyInfo

	mu sync.RWMutex
}

type inMemoryRecoveryKeyStoreKey struct{}

func (s *inMemoryRecoveryKeyStore) AddRecoveryKey(keyID string, rkeyInfo RecoveryKeyInfo) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rkeys[keyID]; exists {
		return fmt.Errorf("recovery key with id %q already exists", keyID)
	}
	s.rkeys[keyID] = rkeyInfo
	return nil
}

func (s *inMemoryRecoveryKeyStore) GetRecoveryKey(keyID string) (rkeyInfo RecoveryKeyInfo, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rkeyInfo, exists := s.rkeys[keyID]
	if !exists {
		return RecoveryKeyInfo{}, fmt.Errorf("recovery key with id %q does not exist", keyID)
	}

	return rkeyInfo, nil
}

func (s *inMemoryRecoveryKeyStore) DeleteRecoveryKey(keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.rkeys, keyID)
	return nil
}
