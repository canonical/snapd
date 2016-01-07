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
	"os"
	"regexp"
	"time"
)

// BuilderFromComps can build an assertion from its components.
type BuilderFromComps func(headers map[string]string, body, content, signature []byte) (Assertion, error)

// Backstore is a backstore for assertions. It can store and retrieve
// assertions by type under primary paths (tuples of strings). Plus it
// supports more general searches.
type Backstore interface {
	// Init initializes the backstore. It is provided with a function
	// to build assertions from their components.
	Init(buildAssert BuilderFromComps) error
	// Put stores an assertion under the given unique primaryPath.
	// It is responsible for checking that assert is newer than a
	// previously stored revision.
	Put(assertType AssertionType, primaryPath []string, assert Assertion) error
	// Get loads an assertion with the given unique primaryPath.
	// If none is present it returns ErrNotFound.
	Get(assertType AssertionType, primaryPath []string) (Assertion, error)
	// Search searches for assertions matching the given headers.
	// It invokes foundCb for each found assertion.
	// pathHint is an incomplete primary path pattern (with ""
	// representing omitted components) that covers a superset of
	// the results, it can be used for the search if helpful.
	Search(assertType AssertionType, headers map[string]string, pathHint []string, foundCb func(Assertion)) error
}

// KeypairManager is a manager and backstore for private/public key pairs.
type KeypairManager interface {
	// Import stores the given private/public key pair for identity and
	// returns its fingerprint
	Import(authorityID string, privKey PrivateKey) (fingerprint string, err error)
	// Get returns the private/public key pair with the given fingeprint.
	Get(authorityID, fingeprint string) (PrivateKey, error)
	// Find finds the private/public key pair with the given fingeprint suffix.
	// Find will return an error if not eactly one key pair is found.
	Find(authorityID, fingerprintSuffix string) (PrivateKey, error)
}

// TODO: for more flexibility plugging the keypair manager make PrivatKey private encoding methods optional, and add an explicit sign method.

// DatabaseConfig for an assertion database.
type DatabaseConfig struct {
	// database filesystem backstores path
	Path string
	// trusted account keys
	TrustedKeys []*AccountKey
	// backstore for assertions, falls back to a filesystem based backstrore
	// if not set
	Backstore Backstore
	// manager/backstore for keypairs, falls back to a filesystem based manager
	KeypairManager KeypairManager
}

// Well-known errors
var (
	ErrNotFound = errors.New("assertion not found")
)

// A consistencyChecker performs further checks based on the full
// assertion database knowledge and its own signing key.
type consistencyChecker interface {
	checkConsistency(db *Database, signingKey *AccountKey) error
}

// Database holds assertions and can be used to sign or check
// further assertions.
type Database struct {
	bs          Backstore
	keypairMgr  KeypairManager
	trustedKeys map[string][]*AccountKey
}

// OpenDatabase opens the assertion database based on the configuration.
func OpenDatabase(cfg *DatabaseConfig) (*Database, error) {
	bs := cfg.Backstore
	keypairMgr := cfg.KeypairManager

	// falling back to at least one of the filesytem backstores,
	// ensure the main directory cfg.Path
	// TODO: decide what should be the final defaults/fallbacks
	if bs == nil || keypairMgr == nil {
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

		if bs == nil {
			bs = newFilesystemBackstore(cfg.Path)
		}
		if keypairMgr == nil {
			keypairMgr = newFilesystemKeypairMananager(cfg.Path)
		}
	}

	err := bs.Init(buildAssertion)
	if err != nil {
		return nil, err
	}

	trustedKeys := make(map[string][]*AccountKey)
	for _, accKey := range cfg.TrustedKeys {
		authID := accKey.AccountID()
		trustedKeys[authID] = append(trustedKeys[authID], accKey)
	}
	return &Database{
		bs:          bs,
		keypairMgr:  keypairMgr,
		trustedKeys: trustedKeys,
	}, nil
}

// GenerateKey generates a private/public key pair for identity and
// stores it returning its fingerprint.
func (db *Database) GenerateKey(authorityID string) (fingerprint string, err error) {
	// TODO: optionally delegate the whole thing to the keypair mgr

	// TODO: support specifying different key types/algorithms
	privKey, err := generatePrivateKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate private key: %v", err)
	}

	return db.keypairMgr.Import(authorityID, OpenPGPPrivateKey(privKey))
}

// ImportKey stores the given private/public key pair for identity and
// returns its fingerprint
func (db *Database) ImportKey(authorityID string, privKey PrivateKey) (fingerprint string, err error) {
	return db.keypairMgr.Import(authorityID, privKey)
}

var (
	// for sanity checking of fingerprint-like strings
	fingerprintLike = regexp.MustCompile("^[0-9a-f]*$")
)

// PublicKey exports the public part of a stored key pair for identity
// by matching the given fingerprint suffix, it is an error if no or more
// than one key pair is found.
func (db *Database) PublicKey(authorityID string, fingerprintSuffix string) (PublicKey, error) {
	if !fingerprintLike.MatchString(fingerprintSuffix) {
		return nil, fmt.Errorf("fingerprint suffix contains unexpected chars: %q", fingerprintSuffix)
	}
	privKey, err := db.keypairMgr.Find(authorityID, fingerprintSuffix)
	if err != nil {
		return nil, err
	}
	return privKey.PublicKey(), nil
}

// Sign builds an assertion with the provided information and signs it
// with the private key from `headers["authority-id"]` that has the provided fingerprint.
func (db *Database) Sign(assertType AssertionType, headers map[string]string, body []byte, fingerprint string) (Assertion, error) {
	if fingerprint == "" {
		return nil, fmt.Errorf("fingerprint is empty")
	}
	if !fingerprintLike.MatchString(fingerprint) {
		return nil, fmt.Errorf("fingerprint contains unexpected chars: %q", fingerprint)
	}
	authorityID, err := checkMandatory(headers, "authority-id")
	if err != nil {
		return nil, err
	}
	privKey, err := db.keypairMgr.Get(authorityID, fingerprint)
	if err != nil {
		return nil, err
	}
	return buildAndSign(assertType, headers, body, privKey)
}

// find account keys exactly by account id and key id
// TODO: may need more details about the kind of key we are looking for
// and use an interface for the results.
func (db *Database) findAccountKeys(authorityID, keyID string) ([]*AccountKey, error) {
	res := make([]*AccountKey, 0, 1)
	cands := db.trustedKeys[authorityID]
	for _, cand := range cands {
		if cand.PublicKeyID() == keyID {
			res = append(res, cand)
		}
	}
	// consider stored account keys
	foundKeyCb := func(a Assertion) {
		res = append(res, a.(*AccountKey))
	}
	err := db.bs.Search(AccountKeyType, map[string]string{
		"account-id":    authorityID,
		"public-key-id": keyID,
	}, []string{authorityID, keyID}, foundKeyCb)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Check tests whether the assertion is properly signed and consistent with all the stored knowledge.
func (db *Database) Check(assert Assertion) error {
	content, signature := assert.Signature()
	sig, err := decodeSignature(signature)
	if err != nil {
		return err
	}
	// TODO: later may need to consider type of assert to find candidate keys
	accKeys, err := db.findAccountKeys(assert.AuthorityID(), sig.KeyID())
	if err != nil {
		return fmt.Errorf("error finding matching public key for signature: %v", err)
	}
	now := time.Now()
	var lastErr error
	for _, accKey := range accKeys {
		if accKey.isKeyValidAt(now) {
			err := accKey.publicKey().verify(content, sig)
			if err == nil {
				// see if the assertion requires further checks
				if checker, ok := assert.(consistencyChecker); ok {
					err := checker.checkConsistency(db, accKey)
					if err != nil {
						return fmt.Errorf("signature verifies but assertion violates other knowledge: %v", err)
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
		primaryKey[i] = keyVal
	}
	return db.bs.Put(assert.Type(), primaryKey, assert)
}

func searchMatch(assert Assertion, expectedHeaders map[string]string) bool {
	// check non-primary-key headers as well
	for expectedKey, expectedValue := range expectedHeaders {
		if assert.Header(expectedKey) != expectedValue {
			return false
		}
	}
	return true
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
		primaryKey[i] = keyVal
	}
	assert, err := db.bs.Get(assertionType, primaryKey)
	if err != nil {
		return nil, err
	}
	if !searchMatch(assert, headers) {
		return nil, ErrNotFound
	}
	return assert, nil
}

// FindMany finds assertions based on arbitrary headers.
// It returns ErrNotFound if no assertion can be found.
func (db *Database) FindMany(assertionType AssertionType, headers map[string]string) ([]Assertion, error) {
	reg, err := checkAssertType(assertionType)
	if err != nil {
		return nil, err
	}
	res := []Assertion{}
	primaryKey := make([]string, len(reg.primaryKey))
	for i, k := range reg.primaryKey {
		primaryKey[i] = headers[k]
	}

	foundCb := func(assert Assertion) {
		res = append(res, assert)
	}
	err = db.bs.Search(assertionType, headers, primaryKey, foundCb)
	if err != nil {
		return nil, err
	}

	if len(res) == 0 {
		return nil, ErrNotFound
	}
	return res, nil
}
