// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"fmt"
	"strings"
)

type memoryKeypairManager struct {
	pairs map[string]map[string]PrivateKey
}

// NewMemoryKeypairMananager creates a new key pair manager with a memory backstore.
func NewMemoryKeypairMananager() KeypairManager {
	return memoryKeypairManager{
		pairs: make(map[string]map[string]PrivateKey),
	}
}

func (mskm memoryKeypairManager) ImportKey(authorityID string, privKey PrivateKey) (fingerprint string, err error) {
	fingerp := privKey.PublicKey().Fingerprint()
	perAuthID := mskm.pairs[authorityID]
	if perAuthID == nil {
		perAuthID = make(map[string]PrivateKey)
		mskm.pairs[authorityID] = perAuthID
	}
	perAuthID[fingerp] = privKey
	return fingerp, nil
}

// return fmt.Errorf("ambiguous search, more than one key pair found: %q and %q", keyPath, relpath)

func (mskm memoryKeypairManager) Key(authorityID, fingeprint string) (PrivateKey, error) {
	privKey := mskm.pairs[authorityID][fingeprint]
	if privKey == nil {
		return nil, errKeypairNotFound
	}
	return privKey, nil
}

func (mskm memoryKeypairManager) FindKey(authorityID, fingerprintSuffix string) (PrivateKey, error) {
	var found PrivateKey
	for fingerp, privKey := range mskm.pairs[authorityID] {
		if strings.HasSuffix(fingerp, fingerprintSuffix) {
			if found == nil {
				found = privKey
			} else {
				return nil, fmt.Errorf("ambiguous search, more than one key pair found: %q and %q", found.PublicKey().Fingerprint(), privKey.PublicKey().Fingerprint())
			}
		}
	}
	if found == nil {
		return nil, errKeypairNotFound
	}
	return found, nil
}
