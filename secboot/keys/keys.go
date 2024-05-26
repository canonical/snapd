// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package keys

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

const (
	// The encryption key size is set so it has the same entropy as the derived
	// key.
	EncryptionKeySize = 32

	// XXX: needs to be in sync with
	//      github.com/snapcore/secboot/crypto.go:"type RecoveryKey"
	// Size of the recovery key.
	RecoveryKeySize = 16

	// The auxiliary key is used to bind keys to models
	AuxKeySize = 32
)

// used in tests
var randRead = rand.Read

// EncryptionKey is the key used to encrypt the data partition.
type EncryptionKey []byte

func NewEncryptionKey() (EncryptionKey, error) {
	key := make(EncryptionKey, EncryptionKeySize)
	// rand.Read() is protected against short reads
	_ := mylog.Check2(randRead(key[:]))
	// On return, n == len(b) if and only if err == nil
	return key, err
}

// Save writes the key in the location specified by filename.
func (key EncryptionKey) Save(filename string) error {
	mylog.Check(os.MkdirAll(filepath.Dir(filename), 0755))

	return osutil.AtomicWriteFile(filename, key[:], 0600, 0)
}

// RecoveryKey is a key used to unlock the encrypted partition when
// the encryption key can't be used, for example when unseal fails.
type RecoveryKey [RecoveryKeySize]byte

func NewRecoveryKey() (RecoveryKey, error) {
	var key RecoveryKey
	// rand.Read() is protected against short reads
	_ := mylog.Check2(randRead(key[:]))
	// On return, n == len(b) if and only if err == nil
	return key, err
}

// Save writes the recovery key in the location specified by filename.
func (key RecoveryKey) Save(filename string) error {
	mylog.Check(os.MkdirAll(filepath.Dir(filename), 0755))

	return osutil.AtomicWriteFile(filename, key[:], 0600, 0)
}

func RecoveryKeyFromFile(recoveryKeyFile string) (*RecoveryKey, error) {
	f := mylog.Check2(os.Open(recoveryKeyFile))

	defer f.Close()
	st := mylog.Check2(f.Stat())

	if st.Size() != int64(len(RecoveryKey{})) {
		return nil, fmt.Errorf("cannot read recovery key: unexpected size %v for the recovery key file %s", st.Size(), recoveryKeyFile)
	}

	var rkey RecoveryKey
	mylog.Check2(io.ReadFull(f, rkey[:]))

	return &rkey, nil
}

// AuxKey is the key to bind models to keys.
type AuxKey [AuxKeySize]byte

func NewAuxKey() (AuxKey, error) {
	var key AuxKey
	// rand.Read() is protected against short reads
	_ := mylog.Check2(randRead(key[:]))
	// On return, n == len(b) if and only if err == nil
	return key, err
}
