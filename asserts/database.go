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

// Package asserts implements snappy assertions and a database
// abstraction for managing and holding them.
package asserts

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/openpgp/packet"

	"github.com/ubuntu-core/snappy/helpers"
)

// PublicKey is a public key as used by the assertion database.
type PublicKey interface {
	// Fingerprint returns the key fingerprint.
	Fingerprint() string
	// Verify verifies signature is valid for content using the key.
	Verify(content []byte, sig Signature) error
	// IsValidAt returns whether the public key is valid at 'when' time
	IsValidAt(when time.Time) bool
}

// DatabaseConfig for an assertion database.
type DatabaseConfig struct {
	// database backstore path
	Path string
	// trusted keys maps authority-ids to list of trusted keys.
	TrustedKeys map[string][]PublicKey
}

// Database holds assertions and can be used to sign or check
// further assertions.
type Database struct {
	root string
	cfg  *DatabaseConfig
}

const (
	privateKeysLayoutVersion = "v0"
	privateKeysRoot          = "private-keys-" + privateKeysLayoutVersion
)

// OpenDatabase opens the assertion database based on the configuration.
func OpenDatabase(cfg *DatabaseConfig) (*Database, error) {
	err := os.MkdirAll(cfg.Path, 0775)
	if err != nil {
		return nil, fmt.Errorf("failed to create assert database root: %v", err)
	}
	info, err := os.Stat(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to create assert database root: %v", err)
	}
	if info.Mode().Perm()&0002 != 0 {
		return nil, fmt.Errorf("assert database root unexpectedly world-writable: %v", cfg.Path)
	}
	return &Database{root: cfg.Path, cfg: cfg}, nil
}

func (db *Database) atomicWriteEntry(data []byte, secret bool, subpath ...string) error {
	fpath := filepath.Join(db.root, filepath.Join(subpath...))
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
func (db *Database) GenerateKey(authorityID string) (fingerprint string, err error) {
	// TODO: support specifying different key types/algorithms
	privKey, err := generatePrivateKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate private key: %v", err)
	}

	return db.ImportKey(authorityID, privKey)
}

// ImportKey stores the given private/public key pair for identity and
// returns its fingerprint
func (db *Database) ImportKey(authorityID string, privKey *packet.PrivateKey) (fingerprint string, err error) {
	buf := new(bytes.Buffer)
	err = privKey.Serialize(buf)
	if err != nil {
		return "", fmt.Errorf("failed to store private key: %v", err)
	}

	fingerp := hex.EncodeToString(privKey.PublicKey.Fingerprint[:])
	err = db.atomicWriteEntry(buf.Bytes(), true, privateKeysRoot, authorityID, fingerp)
	if err != nil {
		return "", fmt.Errorf("failed to store private key: %v", err)
	}
	return fingerp, nil
}

// use a generalized matching style along what PGP does where keys can be
// retrieved by giving suffixes of their fingerprint
// TODO: may need more details about the kind of key we are looking for
func (db *Database) findPublicKeys(authorityID, fingerprintSuffix string) []PublicKey {
	res := make([]PublicKey, 0, 1)
	cands := db.cfg.TrustedKeys[authorityID]
	for _, cand := range cands {
		if strings.HasSuffix(cand.Fingerprint(), fingerprintSuffix) {
			res = append(res, cand)
		}
	}
	// TODO: consider other stored public key assertions
	return res
}

// Check tests whether the assertion is properly signed and consistent with all the stored knowledge.
func (db *Database) Check(assert Assertion) error {
	content, signature := assert.Signature()
	sig, err := parseSignature(signature)
	if err != nil {
		return err
	}
	// TODO: later may need to consider type of assert to find candidate keys
	pubKeys := db.findPublicKeys(assert.AuthorityID(), sig.KeyID())
	now := time.Now()
	err = nil
	for _, pubKey := range pubKeys {
		if pubKey.IsValidAt(now) {
			err = pubKey.Verify(content, sig)
			if err == nil {
				// TODO: further checks about consistency of assert and validity of the key for this kind of assert, likely delegating to the assert
				return nil
			}
		}
	}
	if err == nil {
		return fmt.Errorf("no valid known public key verifies assertion")
	}
	return err
}
