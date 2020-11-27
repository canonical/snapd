// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package secboot

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

const (
	// The encryption key size is set so it has the same entropy as the derived
	// key.
	encryptionKeySize = 64

	// XXX: needs to be in sync with
	//      github.com/snapcore/secboot/crypto.go:"type RecoveryKey"
	// Size of the recovery key.
	recoveryKeySize = 16
)

// EncryptionKey is the key used to encrypt the data partition.
type EncryptionKey [encryptionKeySize]byte

func NewEncryptionKey() (EncryptionKey, error) {
	var key EncryptionKey
	// rand.Read() is protected against short reads
	_, err := rand.Read(key[:])
	// On return, n == len(b) if and only if err == nil
	return key, err
}

// Save writes the key in the location specified by filename.
func (key EncryptionKey) Save(filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(filename, key[:], 0600, 0)
}

// RecoveryKey is a key used to unlock the encrypted partition when
// the encryption key can't be used, for example when unseal fails.
type RecoveryKey [recoveryKeySize]byte

func NewRecoveryKey() (RecoveryKey, error) {
	var key RecoveryKey
	// rand.Read() is protected against short reads
	_, err := rand.Read(key[:])
	// On return, n == len(b) if and only if err == nil
	return key, err
}

// Save writes the recovery key in the location specified by filename.
func (key RecoveryKey) Save(filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(filename, key[:], 0600, 0)
}

func RecoveryKeyFromFile(recoveryKeyFile string) (*RecoveryKey, error) {
	f, err := os.Open(recoveryKeyFile)
	if err != nil {
		return nil, fmt.Errorf("cannot open recovery key: %v", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat recovery key: %v", err)
	}
	if st.Size() != int64(len(RecoveryKey{})) {
		return nil, fmt.Errorf("cannot read recovery key: unexpected size %v for the recovery key file %s", st.Size(), recoveryKeyFile)
	}

	var rkey RecoveryKey
	if _, err := io.ReadFull(f, rkey[:]); err != nil {
		return nil, fmt.Errorf("cannot read recovery key: %v", err)
	}
	return &rkey, nil
}
