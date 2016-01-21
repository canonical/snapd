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
	"regexp"
	"time"
)

// A Backstore stores assertions. It can store and retrieve assertions
// by type under unique primary key headers (whose names are available
// from assertType.PrimaryKey). Plus it supports searching by headers.
type Backstore interface {
	// Put stores an assertion.
	// It is responsible for checking that assert is newer than a
	// previously stored revision with the same primary key headers.
	Put(assertType *AssertionType, assert Assertion) error
	// Get returns the assertion with the given unique key for its primary key headers.
	// If none is present it returns ErrNotFound.
	Get(assertType *AssertionType, key []string) (Assertion, error)
	// Search returns assertions matching the given headers.
	// It invokes foundCb for each found assertion.
	Search(assertType *AssertionType, headers map[string]string, foundCb func(Assertion)) error
}

type nullBackstore struct{}

func (nbs nullBackstore) Put(t *AssertionType, a Assertion) error {
	return fmt.Errorf("cannot store assertions without setting a proper assertion backstore implementation")
}

func (nbs nullBackstore) Get(t *AssertionType, k []string) (Assertion, error) {
	return nil, ErrNotFound
}

func (nbs nullBackstore) Search(t *AssertionType, h map[string]string, f func(Assertion)) error {
	return nil
}

// A KeypairManager is a manager and backstore for private/public key pairs.
type KeypairManager interface {
	// Put stores the given private/public key pair for identity,
	// making sure it can be later retrieved by authority-id and
	// key id with Get().
	// Trying to store a key with an already present key id should
	// result in an error.
	Put(authorityID string, privKey PrivateKey) error
	// Get returns the private/public key pair with the given key id.
	Get(authorityID, keyID string) (PrivateKey, error)
}

// TODO: for more flexibility plugging the keypair manager make PrivatKey private encoding methods optional, and add an explicit sign method.

// DatabaseConfig for an assertion database.
type DatabaseConfig struct {
	// trusted account keys
	TrustedKeys []*AccountKey
	// backstore for assertions, left unset storing assertions will error
	Backstore Backstore
	// manager/backstore for keypairs, mandatory
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

	if bs == nil {
		bs = nullBackstore{}
	}
	if keypairMgr == nil {
		panic("database cannot be used without setting a keypair manager")
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
// stores it returning its key id.
func (db *Database) GenerateKey(authorityID string) (keyID string, err error) {
	// TODO: optionally delegate the whole thing to the keypair mgr

	// TODO: support specifying different key types/algorithms
	privKey, err := generatePrivateKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate private key: %v", err)
	}

	pk := OpenPGPPrivateKey(privKey)
	err = db.ImportKey(authorityID, pk)
	if err != nil {
		return "", err
	}
	return pk.PublicKey().ID(), nil
}

// ImportKey stores the given private/public key pair for identity.
func (db *Database) ImportKey(authorityID string, privKey PrivateKey) error {
	return db.keypairMgr.Put(authorityID, privKey)
}

var (
	// for sanity checking of fingerprint-like strings
	fingerprintLike = regexp.MustCompile("^[0-9a-f]*$")
)

func (db *Database) safeGetPrivateKey(authorityID, keyID string) (PrivateKey, error) {
	if keyID == "" {
		return nil, fmt.Errorf("key id is empty")
	}
	if !fingerprintLike.MatchString(keyID) {
		return nil, fmt.Errorf("key id contains unexpected chars: %q", keyID)
	}
	return db.keypairMgr.Get(authorityID, keyID)
}

// PublicKey returns the public key owned by authorityID that has the given key id.
func (db *Database) PublicKey(authorityID string, keyID string) (PublicKey, error) {
	privKey, err := db.safeGetPrivateKey(authorityID, keyID)
	if err != nil {
		return nil, err
	}
	return privKey.PublicKey(), nil
}

// Sign assembles an assertion with the provided information and signs it
// with the private key from `headers["authority-id"]` that has the provided key id.
func (db *Database) Sign(assertType *AssertionType, headers map[string]string, body []byte, keyID string) (Assertion, error) {
	authorityID, err := checkMandatory(headers, "authority-id")
	if err != nil {
		return nil, err
	}
	privKey, err := db.safeGetPrivateKey(authorityID, keyID)
	if err != nil {
		return nil, err
	}
	return assembleAndSign(assertType, headers, body, privKey)
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
	a, err := db.bs.Get(AccountKeyType, []string{authorityID, keyID})
	switch err {
	case nil:
		res = append(res, a.(*AccountKey))
	case ErrNotFound:
		// nothing to do
	default:
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
	assertType := assert.Type()
	err := db.Check(assert)
	if err != nil {
		return err
	}
	for _, k := range assertType.PrimaryKey {
		if assert.Header(k) == "" {
			return fmt.Errorf("missing primary key header: %v", k)
		}
	}
	return db.bs.Put(assertType, assert)
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
func (db *Database) Find(assertionType *AssertionType, headers map[string]string) (Assertion, error) {
	err := checkAssertType(assertionType)
	if err != nil {
		return nil, err
	}
	keyValues := make([]string, len(assertionType.PrimaryKey))
	for i, k := range assertionType.PrimaryKey {
		keyVal := headers[k]
		if keyVal == "" {
			return nil, fmt.Errorf("must provide primary key: %v", k)
		}
		keyValues[i] = keyVal
	}
	assert, err := db.bs.Get(assertionType, keyValues)
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
func (db *Database) FindMany(assertionType *AssertionType, headers map[string]string) ([]Assertion, error) {
	err := checkAssertType(assertionType)
	if err != nil {
		return nil, err
	}
	res := []Assertion{}

	foundCb := func(assert Assertion) {
		res = append(res, assert)
	}
	err = db.bs.Search(assertionType, headers, foundCb)
	if err != nil {
		return nil, err
	}

	if len(res) == 0 {
		return nil, ErrNotFound
	}
	return res, nil
}
