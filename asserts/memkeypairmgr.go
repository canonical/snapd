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

	keyHash := privKey.PublicKey().SHA3_384()
	perAuthID := mkm.pairs[authorityID]
	if perAuthID == nil {
		perAuthID = make(map[string]PrivateKey)
		mkm.pairs[authorityID] = perAuthID
	} else if perAuthID[keyHash] != nil {
		return errKeypairAlreadyExists
	}
	perAuthID[keyHash] = privKey
	return nil
}

func (mkm *memoryKeypairManager) Get(authorityID, keyHash string) (PrivateKey, error) {
	mkm.mu.RLock()
	defer mkm.mu.RUnlock()

	privKey := mkm.pairs[authorityID][keyHash]
	if privKey == nil {
		return nil, errKeypairNotFound
	}
	return privKey, nil
}
