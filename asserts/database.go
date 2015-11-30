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

// Package asserts implements snappy assertions and a database
// abstraction for managing and holding them.
package asserts

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	ErrNotFound = errors.New("assertion not found")
)

// A consistencyChecker performs further checks based on the full
// assertion database knowledge and its own signing key.
type consistencyChecker interface {
	checkConsistency(db *Database, signingPubKey PublicKey) error
}

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

	return db.ImportKey(authorityID, WrapPrivateKey(privKey))
}

// ImportKey stores the given private/public key pair for identity and
// returns its fingerprint
func (db *Database) ImportKey(authorityID string, privKey PrivateKey) (fingerprint string, err error) {
	encoded, err := encodePrivateKey(privKey)
	if err != nil {
		return "", fmt.Errorf("failed to store private key: %v", err)
	}

	fingerp := privKey.PublicKey().Fingerprint()
	err = db.atomicWriteEntry(encoded, true, privateKeysRoot, authorityID, fingerp)
	if err != nil {
		return "", fmt.Errorf("failed to store private key: %v", err)
	}
	return fingerp, nil
}

// ExportPublicKey exports the public part of a stored key pair for identity
// by matching the given fingerprint suffix, it's an error if no or more
// than one key pair is found.
func (db *Database) ExportPublicKey(authorityID string, fingerprintSuffix string) (PublicKey, error) {
	keyPath := ""
	foundPrivKeyCb := func(relpath string) error {
		if keyPath != "" {
			return fmt.Errorf("ambiguous search, more than one key pair found: %q and %q", keyPath, relpath)

		}
		keyPath = relpath
		return nil
	}
	privKeysTop := filepath.Join(db.root, privateKeysRoot)
	err := findWildcard(privKeysTop, []string{authorityID, "*" + fingerprintSuffix}, foundPrivKeyCb)
	if err != nil {
		return nil, err
	}
	if keyPath == "" {
		return nil, fmt.Errorf("no matching key pair found")
	}
	encoded, err := db.readEntry(privateKeysRoot, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key pair: %v", err)
	}
	privKey, err := parsePrivateKey(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key pair: %v", err)
	}
	return privKey.PublicKey(), nil
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
	// consider stored account keys
	accountKeysTop := filepath.Join(db.root, assertionsRoot, string(AccountKeyType))
	foundKeyCb := func(primaryPath string) error {
		a, err := db.readAssertion(AccountKeyType, primaryPath)
		if err != nil {
			return err
		}
		var accKey PublicKey
		accKey, ok := a.(*AccountKey)
		if !ok {
			return fmt.Errorf("something that is not an account-key under their storage tree")
		}
		res = append(res, accKey)
		return nil
	}
	err := findWildcard(accountKeysTop, []string{url.QueryEscape(authorityID), "*" + fingerprintSuffix}, foundKeyCb)
	if err != nil {
		return nil, fmt.Errorf("broken assertion storage, scanning: %v", err)
	}

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
				// see if the assertion requires further checks
				if checker, ok := assert.(consistencyChecker); ok {
					err := checker.checkConsistency(db, pubKey)
					if err != nil {
						return fmt.Errorf("signature verifies but assertion violates other knownledge: %v", err)
					}
				}
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

func (db *Database) readAssertion(assertType AssertionType, primaryPath string) (Assertion, error) {
	encoded, err := db.readEntry(assertionsRoot, string(assertType), primaryPath)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("broken assertion storage, failed to read assertion: %v", err)
	}
	assert, err := Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("broken assertion storage, failed to decode assertion: %v", err)
	}
	return assert, nil
}

// Add persists the assertion after ensuring it is properly signed and consistent with all the stored knowledge.
// It will return an error when trying to add an older revision of the assertion than the one currently stored.
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
	primaryPath := filepath.Join(primaryKey...)
	curAssert, err := db.readAssertion(assert.Type(), primaryPath)
	if err == nil {
		curRev := curAssert.Revision()
		rev := assert.Revision()
		if curRev >= rev {
			return fmt.Errorf("assertion added must have more recent revision than current one (adding %d, currently %d)", rev, curRev)
		}
	} else if err != ErrNotFound {
		return err
	}
	err = db.atomicWriteEntry(Encode(assert), false, assertionsRoot, string(assert.Type()), primaryPath)
	if err != nil {
		return fmt.Errorf("broken assertion storage, failed to write assertion: %v", err)
	}
	return nil
}

// Find an assertion based on arbitrary headers.
// Provided headers must contain the primary key for the assertion type.
// It returns ErrNotFound if the assertion cannot be found.
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
	primaryPath := filepath.Join(primaryKey...)
	assert, err := db.readAssertion(assertionType, primaryPath)
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
