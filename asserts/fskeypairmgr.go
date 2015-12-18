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
	"net/url"
	"path/filepath"
)

// the default simple filesystem based keypair manager/backstore

const (
	privateKeysLayoutVersion = "v0"
	privateKeysRoot          = "private-keys-" + privateKeysLayoutVersion
)

type filesystemKeypairManager struct {
	top string
}

func newFilesystemKeypairMananager(path string) *filesystemKeypairManager {
	return &filesystemKeypairManager{top: filepath.Join(path, privateKeysRoot)}
}

func (fskm *filesystemKeypairManager) ImportKey(authorityID string, privKey PrivateKey) (fingerprint string, err error) {
	encoded, err := encodePrivateKey(privKey)
	if err != nil {
		return "", fmt.Errorf("failed to store private key: %v", err)
	}

	fingerp := privKey.PublicKey().Fingerprint()
	err = atomicWriteEntry(encoded, true, fskm.top, url.QueryEscape(authorityID), fingerp)
	if err != nil {
		return "", fmt.Errorf("failed to store private key: %v", err)
	}
	return fingerp, nil
}

// findPrivateKey will return an error if not eactly one private key is found
func (fskm *filesystemKeypairManager) findPrivateKey(authorityID, fingerprintWildcard string) (PrivateKey, error) {
	keyPath := ""
	foundPrivKeyCb := func(relpath string) error {
		if keyPath != "" {
			return fmt.Errorf("ambiguous search, more than one key pair found: %q and %q", keyPath, relpath)

		}
		keyPath = relpath
		return nil
	}
	err := findWildcard(fskm.top, []string{url.QueryEscape(authorityID), fingerprintWildcard}, foundPrivKeyCb)
	if err != nil {
		return nil, err
	}
	if keyPath == "" {
		return nil, fmt.Errorf("no matching key pair found")
	}
	encoded, err := readEntry(fskm.top, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key pair: %v", err)
	}
	privKey, err := decodePrivateKey(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key pair: %v", err)
	}
	return privKey, nil
}

func (fskm *filesystemKeypairManager) Key(authorityID, fingeprint string) (PrivateKey, error) {
	return fskm.findPrivateKey(authorityID, fingeprint)
}

func (fskm *filesystemKeypairManager) FindKey(authorityID, fingerprintSuffix string) (PrivateKey, error) {
	return fskm.findPrivateKey(authorityID, "*"+fingerprintSuffix)
}
