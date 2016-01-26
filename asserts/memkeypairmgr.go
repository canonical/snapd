// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package asserts

import (
	"sync"
)

type memoryKeypairManager struct {
	pairs map[string]map[string]PrivateKey
	mu    sync.RWMutex
}

// NewMemoryKeypairManager creates a new key pair manager with a memory backstore.
func NewMemoryKeypairManager() KeypairManager {
	return &memoryKeypairManager{
		pairs: make(map[string]map[string]PrivateKey),
	}
}

func (mkm *memoryKeypairManager) Put(authorityID string, privKey PrivateKey) error {
	mkm.mu.Lock()
	defer mkm.mu.Unlock()

	keyID := privKey.PublicKey().ID()
	perAuthID := mkm.pairs[authorityID]
	if perAuthID == nil {
		perAuthID = make(map[string]PrivateKey)
		mkm.pairs[authorityID] = perAuthID
	} else if perAuthID[keyID] != nil {
		return errKeypairAlreadyExists
	}
	perAuthID[keyID] = privKey
	return nil
}

func (mkm *memoryKeypairManager) Get(authorityID, keyID string) (PrivateKey, error) {
	mkm.mu.RLock()
	defer mkm.mu.RUnlock()

	privKey := mkm.pairs[authorityID][keyID]
	if privKey == nil {
		return nil, errKeypairNotFound
	}
	return privKey, nil
}
