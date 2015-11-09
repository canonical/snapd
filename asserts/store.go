// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

// Package asserts implements snappy assertions and a store
// abstraction for managing and holding them.
package asserts

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/crypto/openpgp/packet"

	"github.com/ubuntu-core/snappy/helpers"
)

// StoreConfig for an assertion store.
type StoreConfig struct {
	// store backstore path
	Path string
}

// AssertStore holds assertions and can be used to sign or check
// further assertions.
type AssertStore struct {
	root string
}

const (
	privateKeysLayoutVersion = "v0"
	privateKeysRoot          = "private-keys-" + privateKeysLayoutVersion
)

// errors
var (
	ErrStoreRootCreate        = errors.New("failed to create assert store root")
	ErrStoreRootWorldReadable = errors.New("assert store root unexpectedly world-writable")
	ErrStoreKeyGen            = errors.New("failed to generate private key")
	ErrStoreStoringKey        = errors.New("failed to store private key")
)

// OpenStore opens the assertion store based on the configuration.
func OpenStore(cfg *StoreConfig) (*AssertStore, error) {
	err := os.MkdirAll(cfg.Path, 0775)
	if err != nil {
		return nil, ErrStoreRootCreate
	}
	info, err := os.Stat(cfg.Path)
	if err != nil {
		return nil, ErrStoreRootCreate
	}
	if info.Mode().Perm()&0002 != 0 {
		return nil, ErrStoreRootWorldReadable
	}
	return &AssertStore{root: cfg.Path}, nil
}

func (astore *AssertStore) atomicWriteEntry(data []byte, secret bool, hier ...string) error {
	fpath := filepath.Join(astore.root, filepath.Join(hier...))
	dir := filepath.Dir(fpath)
	err := os.MkdirAll(dir, 0775)
	if err != nil {
		return err
	}
	fperm := 0664
	if secret {
		fperm = 0600
	}
	return helpers.AtomicWriteFile(fpath, data, os.FileMode(fperm), 0)
}

// GenerateKey generates a private/public key pair for identity and
// stores it returning its fingerprint.
func (astore *AssertStore) GenerateKey(authorityID string) (fingerprint []byte, err error) {
	// TODO: support specifying different key types/algorithms
	privKey, err := generatePrivateKey()
	if err != nil {
		return nil, ErrStoreKeyGen
	}
	return astore.ImportKey(authorityID, privKey)
}

// ImportKey stores the given private/public key pair for identity and
// returns its fingerprint
func (astore *AssertStore) ImportKey(authorityID string, privKey *packet.PrivateKey) (fingerprint []byte, err error) {
	buf := new(bytes.Buffer)
	err = privKey.Serialize(buf)
	if err != nil {
		return nil, ErrStoreStoringKey
	}
	fingerp := privKey.PublicKey.Fingerprint[:]
	err = astore.atomicWriteEntry(buf.Bytes(), true, privateKeysRoot, authorityID, hex.EncodeToString(fingerp))
	if err != nil {
		return nil, ErrStoreStoringKey
	}
	return fingerp, nil
}
