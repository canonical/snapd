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
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	// IsValidAt returns whether the public key is valid at 'when' time.
	IsValidAt(when time.Time) bool
}

// DatabaseConfig for an assertion database.
type DatabaseConfig struct {
	// database backstore path
	Path string
	// trusted keys maps authority-ids to list of trusted keys.
	TrustedKeys map[string][]PublicKey
}

// Well-known errors
var (
	ErrNotFound       = errors.New("assertion not found")
	ErrNotSuperseding = errors.New("assertion does not supersede current one")
)

// Database holds assertions and can be used to sign or check
// further assertions.
type Database struct {
	root string
	cfg  DatabaseConfig
}

const (
	privateKeysLayoutVersion = "v0"
	privateKeysRoot          = "private-keys-" + privateKeysLayoutVersion
	assertionsLayoutVersion  = "v0"
	assertionsRoot           = "assertions-" + assertionsLayoutVersion
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
	return &Database{root: cfg.Path, cfg: *cfg}, nil
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

func (db *Database) removeEntry(subpath ...string) error {
	fpath := filepath.Join(db.root, filepath.Join(subpath...))
	return os.Remove(fpath)
}

func (db *Database) symlinkEntry(entry string, subpath ...string) error {
	fpath := filepath.Join(db.root, filepath.Join(subpath...))
	// TODO: move the rest of this as a helper together with AtomicWriteFile?
	dir, err := os.Open(filepath.Dir(fpath))
	if err != nil {
		return err
	}
	defer dir.Close()
	err = os.Symlink(entry, fpath)
	if err != nil {
		return err
	}
	return dir.Sync()
}

func (db *Database) statEntry(subpath ...string) (os.FileInfo, error) {
	fpath := filepath.Join(db.root, filepath.Join(subpath...))
	return os.Stat(fpath)
}

func (db *Database) readlinkEntry(subpath ...string) (string, error) {
	fpath := filepath.Join(db.root, filepath.Join(subpath...))
	return os.Readlink(fpath)
}

func (db *Database) readEntry(subpath ...string) ([]byte, error) {
	fpath := filepath.Join(db.root, filepath.Join(subpath...))
	return ioutil.ReadFile(fpath)
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
// retrieved by giving suffixes of their fingerprint,
// for safety suffix must be at least 64 bits though
// TODO: may need more details about the kind of key we are looking for
func (db *Database) findPublicKeys(authorityID, fingerprintSuffix string) ([]PublicKey, error) {
	suffixLen := len(fingerprintSuffix)
	if suffixLen%2 == 1 {
		return nil, fmt.Errorf("key id/fingerprint suffix cannot specify a half byte")
	}
	if suffixLen < 16 {
		return nil, fmt.Errorf("key id/fingerprint suffix must be at least 64 bits")
	}
	res := make([]PublicKey, 0, 1)
	cands := db.cfg.TrustedKeys[authorityID]
	for _, cand := range cands {
		if strings.HasSuffix(cand.Fingerprint(), fingerprintSuffix) {
			res = append(res, cand)
		}
	}
	// TODO: consider other stored public key assertions
	return res, nil
}

// Check tests whether the assertion is properly signed and consistent with all the stored knowledge.
func (db *Database) Check(assert Assertion) error {
	content, signature := assert.Signature()
	sig, err := parseSignature(signature)
	if err != nil {
		return err
	}
	// TODO: later may need to consider type of assert to find candidate keys
	pubKeys, err := db.findPublicKeys(assert.AuthorityID(), sig.KeyID())
	if err != nil {
		return fmt.Errorf("error finding matching public key for signature: %v", err)
	}
	now := time.Now()
	var lastErr error
	for _, pubKey := range pubKeys {
		if pubKey.IsValidAt(now) {
			err := pubKey.Verify(content, sig)
			if err == nil {
				// TODO: further checks about consistency of assert and validity of the key for this kind of assert, likely delegating to the assert
				return nil
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		return fmt.Errorf("no valid known public key verifies assertion")
	}
	return fmt.Errorf("failed signature verification: %v", lastErr)
}

// Add persists the assertions after ensuring it is properly signed and consistent with all the stored knowledge.
// Returns ErrNotSuperseding if trying to add a previous revision of the assertion.
func (db *Database) Add(assert Assertion) error {
	reg, err := checkAssertType(assert.Type())
	if err != nil {
		return err
	}
	err = db.Check(assert)
	if err != nil {
		return err
	}
	primaryKey := make([]string, len(reg.primaryKey))
	for i, k := range reg.primaryKey {
		keyVal := assert.Header(k)
		if keyVal == "" {
			return fmt.Errorf("missing primary key header: %v", k)
		}
		// safety against '/' etc
		primaryKey[i] = url.QueryEscape(keyVal)
	}
	indexPath := filepath.Join(string(assert.Type()), filepath.Join(primaryKey...))
	_, err = db.statEntry(assertionsRoot, indexPath)
	switch {
	case err == nil:
		// directory for assertion is present
		curRevStr, err := db.readlinkEntry(assertionsRoot, indexPath, "latest")
		if err != nil {
			return fmt.Errorf("broken assertion storage, reading 'latest' revision symlink: %v", err)
		}
		curRev, err := strconv.Atoi(curRevStr)
		if err != nil {
			return fmt.Errorf("broken assertion storage, extracting revision from 'latest' revision symlink: %v", err)
		}
		if curRev >= assert.Revision() {
			return ErrNotSuperseding
		}
		err = db.removeEntry(assertionsRoot, indexPath, "latest")
		if err != nil {
			return fmt.Errorf("broken assertion storage, could not remove 'latest' revision symlink: %v", err)
		}
	case os.IsNotExist(err):
		// nothing there yet
	default:
		return fmt.Errorf("broken assertion storage, failed to stat assertion directory: %v", err)
	}
	revStr := strconv.Itoa(assert.Revision())
	err = db.atomicWriteEntry(Encode(assert), false, assertionsRoot, indexPath, revStr)
	if err != nil {
		return fmt.Errorf("broken assertion storage, failed to write assertion: %v", err)
	}
	err = db.symlinkEntry(revStr, assertionsRoot, indexPath, "latest")
	if err != nil {
		return fmt.Errorf("broken assertion storage, failed to create 'latest' revision symlink: %v", err)
	}
	return nil
}

// Find an assertion based on arbitrary headers.
// Provided headers must contain the primary key for the assertion type.
// Returns ErrNotFound if the assertion cannot be found.
func (db *Database) Find(assertionType AssertionType, headers map[string]string) (Assertion, error) {
	reg, err := checkAssertType(assertionType)
	if err != nil {
		return nil, err
	}
	primaryKey := make([]string, len(reg.primaryKey))
	for i, k := range reg.primaryKey {
		keyVal := headers[k]
		if keyVal == "" {
			return nil, fmt.Errorf("must provide primary key: %v", k)
		}
		primaryKey[i] = url.QueryEscape(keyVal)
	}
	indexPath := filepath.Join(string(assertionType), filepath.Join(primaryKey...))
	_, err = db.statEntry(assertionsRoot, indexPath)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("broken assertion storage, failed to stat assertion directory: %v", err)
	}
	encoded, err := db.readEntry(assertionsRoot, indexPath, "latest")
	if err != nil {
		return nil, fmt.Errorf("broken assertion storage, failed to read assertion: %v", err)
	}
	assert, err := Decode(encoded)
	if err != nil {
		return nil, err
	}
	// check non-primary-key headers as well
	for expectedKey, expectedValue := range headers {
		if assert.Header(expectedKey) != expectedValue {
			return nil, ErrNotFound
		}
	}
	return assert, nil
}
