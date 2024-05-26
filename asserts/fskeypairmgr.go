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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ddkwork/golibrary/mylog"
)

// the default simple filesystem based keypair manager/backstore

const (
	privateKeysLayoutVersion = "v1"
	privateKeysRoot          = "private-keys-" + privateKeysLayoutVersion
)

type filesystemKeypairManager struct {
	top string
	mu  sync.RWMutex
}

// OpenFSKeypairManager opens a filesystem backed assertions backstore under path.
func OpenFSKeypairManager(path string) (KeypairManager, error) {
	top := filepath.Join(path, privateKeysRoot)
	mylog.Check(ensureTop(top))

	return &filesystemKeypairManager{top: top}, nil
}

var errKeypairAlreadyExists = errors.New("key pair with given key id already exists")

func (fskm *filesystemKeypairManager) Put(privKey PrivateKey) error {
	keyID := privKey.PublicKey().ID()
	if entryExists(fskm.top, keyID) {
		return errKeypairAlreadyExists
	}
	encoded := mylog.Check2(encodePrivateKey(privKey))

	fskm.mu.Lock()
	defer fskm.mu.Unlock()
	mylog.Check(atomicWriteEntry(encoded, true, fskm.top, keyID))

	return nil
}

var errKeypairNotFound = &keyNotFoundError{msg: "cannot find key pair"}

func (fskm *filesystemKeypairManager) Get(keyID string) (PrivateKey, error) {
	fskm.mu.RLock()
	defer fskm.mu.RUnlock()

	encoded := mylog.Check2(readEntry(fskm.top, keyID))
	if os.IsNotExist(err) {
		return nil, errKeypairNotFound
	}

	privKey := mylog.Check2(decodePrivateKey(encoded))

	return privKey, nil
}

func (fskm *filesystemKeypairManager) Delete(keyID string) error {
	fskm.mu.RLock()
	defer fskm.mu.RUnlock()
	mylog.Check(removeEntry(fskm.top, keyID))

	return nil
}
