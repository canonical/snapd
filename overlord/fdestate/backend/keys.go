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
	"errors"
	"time"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot/keys"
)

type RecoveryKeyCache interface {
	// AddKey adds a recovery key with the specified id.
	AddKey(keyID string, rkeyInfo CachedRecoverKey) (err error)
	// Key gets the recovery key associated with the specified id.
	// ErrNoRecoveryKey is returned if no recovery key entry exists
	// for the given key id.
	Key(keyID string) (rkeyInfo CachedRecoverKey, err error)
	// RemoveKey removes the recovery key associated with the specified id.
	RemoveKey(keyID string) error
}

// ErrNoRecoveryKey represents the case of no recovery key entry for a given key-id.
var ErrNoRecoveryKey = errors.New("no recovery key entry for key-id")

// NewInMemoryRecoveryKeyCache returns a memory-backed recovery key cache.
//
// Note: This store might not survive snapd restarts.
func NewInMemoryRecoveryKeyCache(st *state.State) RecoveryKeyCache {
	return &inMemoryRecoveryKeyCache{st}
}

type CachedRecoverKey struct {
	Key keys.RecoveryKey `json:"key"`
	// Expiration indicates the expiration date for the recovery key.
	// If unset, this means that the key will never expire.
	Expiration time.Time `json:"expiration,omitzero"`
}

func (rkeyInfo *CachedRecoverKey) Expired(currTime time.Time) bool {
	if rkeyInfo.Expiration.IsZero() {
		return false
	}
	return currTime.After(rkeyInfo.Expiration)
}

type inMemoryRecoveryKeyCache struct {
	st *state.State
}

func rkeySecretID(keyID string) string {
	return "rkey:" + keyID
}

func (s *inMemoryRecoveryKeyCache) AddKey(keyID string, rkeyInfo CachedRecoverKey) (err error) {
	secretID := rkeySecretID(keyID)
	if s.st.HasSecret(secretID) {
		return errors.New("recovery key id already exists")
	}
	return s.st.SetSecret(secretID, rkeyInfo)
}

func (s *inMemoryRecoveryKeyCache) Key(keyID string) (rkeyInfo CachedRecoverKey, err error) {
	err = s.st.GetSecret(rkeySecretID(keyID), &rkeyInfo)
	if errors.Is(err, state.ErrNoState) {
		return CachedRecoverKey{}, ErrNoRecoveryKey
	} else if err != nil {
		return CachedRecoverKey{}, err
	}
	return rkeyInfo, nil
}

func (s *inMemoryRecoveryKeyCache) RemoveKey(keyID string) error {
	return s.st.SetSecret(rkeySecretID(keyID), nil)
}
