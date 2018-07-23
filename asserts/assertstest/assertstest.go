// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package assertstest provides helpers for testing code that involves assertions.
package assertstest

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/strutil"
)

// GenerateKey generates a private/public key pair of the given bits. It panics on error.
func GenerateKey(bits int) (asserts.PrivateKey, *rsa.PrivateKey) {
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		panic(fmt.Errorf("failed to create private key: %v", err))
	}
	return asserts.RSAPrivateKey(priv), priv
}

// ReadPrivKey reads a PGP private key (either armored or simply base64 encoded). It panics on error.
func ReadPrivKey(pk string) (asserts.PrivateKey, *rsa.PrivateKey) {
	rd := bytes.NewReader([]byte(pk))
	blk, err := armor.Decode(rd)
	var body io.Reader
	if err == nil {
		body = blk.Body
	} else {
		rd.Seek(0, 0)
		// try unarmored
		body = base64.NewDecoder(base64.StdEncoding, rd)
	}
	pkt, err := packet.Read(body)
	if err != nil {
		panic(err)
	}

	pkPkt := pkt.(*packet.PrivateKey)
	rsaPrivKey, ok := pkPkt.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		panic("not a RSA key")
	}

	return asserts.RSAPrivateKey(rsaPrivKey), rsaPrivKey
}

// A sample developer key.
// See systestkeys for a prebuilt set of trusted keys and assertions.
const (
	DevKey = `-----BEGIN PGP PRIVATE KEY BLOCK-----
Version: GnuPG v1

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
=5Myf
-----END PGP PRIVATE KEY BLOCK-----
`

	DevKeyID = "EAD4DbLxK_kn0gzNCXOs3kd6DeMU3f-L6BEsSEuJGBqCORR0gXkdDxMbOm11mRFu"

	DevKeyPGPFingerprint = "966e70f4b9f257a2772f8f354a423d28aaea4b4f"
)

// GPGImportKey imports the given PGP armored key into the GnuPG setup at homedir. It panics on error.
func GPGImportKey(homedir, armoredKey string) {
	path, err := exec.LookPath("gpg1")
	if err != nil {
		path, err = exec.LookPath("gpg")
	}
	if err != nil {
		panic(err)
	}
	gpg := exec.Command(path, "--homedir", homedir, "-q", "--batch", "--import", "--armor")
	gpg.Stdin = bytes.NewBufferString(armoredKey)
	out, err := gpg.CombinedOutput()
	if err != nil {
		panic(fmt.Errorf("cannot import test key into GPG setup at %q: %v (%q)", homedir, err, out))
	}
}

// A SignerDB can sign assertions using its key pairs.
type SignerDB interface {
	Sign(assertType *asserts.AssertionType, headers map[string]interface{}, body []byte, keyID string) (asserts.Assertion, error)
}

// NewAccount creates an account assertion for username, it fills in values for other missing headers as needed. It panics on error.
func NewAccount(db SignerDB, username string, otherHeaders map[string]interface{}, keyID string) *asserts.Account {
	if otherHeaders == nil {
		otherHeaders = make(map[string]interface{})
	}
	otherHeaders["username"] = username
	if otherHeaders["account-id"] == nil {
		otherHeaders["account-id"] = strutil.MakeRandomString(32)
	}
	if otherHeaders["display-name"] == nil {
		otherHeaders["display-name"] = strings.ToTitle(username[:1]) + username[1:]
	}
	if otherHeaders["validation"] == nil {
		otherHeaders["validation"] = "unproven"
	}
	if otherHeaders["timestamp"] == nil {
		otherHeaders["timestamp"] = time.Now().Format(time.RFC3339)
	}
	a, err := db.Sign(asserts.AccountType, otherHeaders, nil, keyID)
	if err != nil {
		panic(err)
	}
	return a.(*asserts.Account)
}

// NewAccountKey creates an account-key assertion for the account, it fills in values for missing headers as needed. In panics on error.
func NewAccountKey(db SignerDB, acct *asserts.Account, otherHeaders map[string]interface{}, pubKey asserts.PublicKey, keyID string) *asserts.AccountKey {
	if otherHeaders == nil {
		otherHeaders = make(map[string]interface{})
	}
	otherHeaders["account-id"] = acct.AccountID()
	otherHeaders["public-key-sha3-384"] = pubKey.ID()
	if otherHeaders["name"] == nil {
		otherHeaders["name"] = "default"
	}
	if otherHeaders["since"] == nil {
		otherHeaders["since"] = time.Now().Format(time.RFC3339)
	}
	encodedPubKey, err := asserts.EncodePublicKey(pubKey)
	if err != nil {
		panic(err)
	}
	a, err := db.Sign(asserts.AccountKeyType, otherHeaders, encodedPubKey, keyID)
	if err != nil {
		panic(err)
	}
	return a.(*asserts.AccountKey)
}

// SigningDB embeds a signing assertion database with a default private key and assigned authority id.
// Sign will use the assigned authority id.
// "" can be passed for keyID to Sign and PublicKey to use the default key.
type SigningDB struct {
	AuthorityID string
	KeyID       string

	*asserts.Database
}

// NewSigningDB creates a test signing assertion db with the given defaults. It panics on error.
func NewSigningDB(authorityID string, privKey asserts.PrivateKey) *SigningDB {
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{})
	if err != nil {
		panic(err)
	}
	err = db.ImportKey(privKey)
	if err != nil {
		panic(err)
	}
	return &SigningDB{
		AuthorityID: authorityID,
		KeyID:       privKey.PublicKey().ID(),
		Database:    db,
	}
}

func (db *SigningDB) Sign(assertType *asserts.AssertionType, headers map[string]interface{}, body []byte, keyID string) (asserts.Assertion, error) {
	if _, ok := headers["authority-id"]; !ok {
		// copy before modifying
		headers2 := make(map[string]interface{}, len(headers)+1)
		for h, v := range headers {
			headers2[h] = v
		}
		headers = headers2
		headers["authority-id"] = db.AuthorityID
	}
	if keyID == "" {
		keyID = db.KeyID
	}
	return db.Database.Sign(assertType, headers, body, keyID)
}

func (db *SigningDB) PublicKey(keyID string) (asserts.PublicKey, error) {
	if keyID == "" {
		keyID = db.KeyID
	}
	return db.Database.PublicKey(keyID)
}

// StoreStack realises a store-like set of founding trusted assertions and signing setup.
type StoreStack struct {
	// Trusted authority assertions.
	TrustedAccount *asserts.Account
	TrustedKey     *asserts.AccountKey
	Trusted        []asserts.Assertion

	// Generic authority assertions.
	GenericAccount      *asserts.Account
	GenericKey          *asserts.AccountKey
	GenericModelsKey    *asserts.AccountKey
	Generic             []asserts.Assertion
	GenericClassicModel *asserts.Model

	// Signing assertion db that signs with the root private key.
	RootSigning *SigningDB

	// The store-like signing functionality that signs with a store key, setup to also store assertions if desired. It stores a default account-key for the store private key, see also the StoreStack.Key method.
	*SigningDB
}

// StoreKeys holds a set of store private keys.
type StoreKeys struct {
	Root          asserts.PrivateKey
	Store         asserts.PrivateKey
	Generic       asserts.PrivateKey
	GenericModels asserts.PrivateKey
}

var (
	rootPrivKey, _          = GenerateKey(1024)
	storePrivKey, _         = GenerateKey(752)
	genericPrivKey, _       = GenerateKey(752)
	genericModelsPrivKey, _ = GenerateKey(752)

	pregenKeys = StoreKeys{
		Root:          rootPrivKey,
		Store:         storePrivKey,
		Generic:       genericPrivKey,
		GenericModels: genericModelsPrivKey,
	}
)

// NewStoreStack creates a new store assertion stack. It panics on error.
// Optional keys specify private keys to use for the various roles.
func NewStoreStack(authorityID string, keys *StoreKeys) *StoreStack {
	if keys == nil {
		keys = &pregenKeys
	}

	rootSigning := NewSigningDB(authorityID, keys.Root)
	ts := time.Now().Format(time.RFC3339)
	trustedAcct := NewAccount(rootSigning, authorityID, map[string]interface{}{
		"account-id": authorityID,
		"validation": "verified",
		"timestamp":  ts,
	}, "")
	trustedKey := NewAccountKey(rootSigning, trustedAcct, map[string]interface{}{
		"name":  "root",
		"since": ts,
	}, keys.Root.PublicKey(), "")
	trusted := []asserts.Assertion{trustedAcct, trustedKey}

	genericAcct := NewAccount(rootSigning, "generic", map[string]interface{}{
		"account-id": "generic",
		"validation": "verified",
		"timestamp":  ts,
	}, "")

	err := rootSigning.ImportKey(keys.GenericModels)
	if err != nil {
		panic(err)
	}
	genericModelsKey := NewAccountKey(rootSigning, genericAcct, map[string]interface{}{
		"name":  "models",
		"since": ts,
	}, keys.GenericModels.PublicKey(), "")
	generic := []asserts.Assertion{genericAcct, genericModelsKey}

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         trusted,
		OtherPredefined: generic,
	})
	if err != nil {
		panic(err)
	}
	err = db.ImportKey(keys.Store)
	if err != nil {
		panic(err)
	}
	storeKey := NewAccountKey(rootSigning, trustedAcct, map[string]interface{}{
		"name": "store",
	}, keys.Store.PublicKey(), "")
	err = db.Add(storeKey)
	if err != nil {
		panic(err)
	}

	err = db.ImportKey(keys.Generic)
	if err != nil {
		panic(err)
	}
	genericKey := NewAccountKey(rootSigning, genericAcct, map[string]interface{}{
		"name":  "serials",
		"since": ts,
	}, keys.Generic.PublicKey(), "")
	err = db.Add(genericKey)
	if err != nil {
		panic(err)
	}

	a, err := rootSigning.Sign(asserts.ModelType, map[string]interface{}{
		"authority-id": "generic",
		"series":       "16",
		"brand-id":     "generic",
		"model":        "generic-classic",
		"classic":      "true",
		"timestamp":    ts,
	}, nil, genericModelsKey.PublicKeyID())
	if err != nil {
		panic(err)
	}
	genericClassicMod := a.(*asserts.Model)

	return &StoreStack{
		TrustedAccount: trustedAcct,
		TrustedKey:     trustedKey,
		Trusted:        trusted,

		GenericAccount:      genericAcct,
		GenericKey:          genericKey,
		GenericModelsKey:    genericModelsKey,
		Generic:             generic,
		GenericClassicModel: genericClassicMod,

		RootSigning: rootSigning,

		SigningDB: &SigningDB{
			AuthorityID: authorityID,
			KeyID:       storeKey.PublicKeyID(),
			Database:    db,
		},
	}
}

// StoreAccountKey retrieves one of the account-key assertions for the signing keys of the simulated store signing database.
// "" for keyID means the default one. It panics on error.
func (ss *StoreStack) StoreAccountKey(keyID string) *asserts.AccountKey {
	if keyID == "" {
		keyID = ss.KeyID
	}
	key, err := ss.Find(asserts.AccountKeyType, map[string]string{
		"account-id":          ss.AuthorityID,
		"public-key-sha3-384": keyID,
	})
	if asserts.IsNotFound(err) {
		return nil
	}
	if err != nil {
		panic(err)
	}
	return key.(*asserts.AccountKey)
}

// MockBuiltinBaseDeclaration mocks the builtin base-declaration exposed by asserts.BuiltinBaseDeclaration.
func MockBuiltinBaseDeclaration(headers []byte) (restore func()) {
	var prevHeaders []byte
	decl := asserts.BuiltinBaseDeclaration()
	if decl != nil {
		prevHeaders, _ = decl.Signature()
	}

	err := asserts.InitBuiltinBaseDeclaration(headers)
	if err != nil {
		panic(err)
	}

	return func() {
		err := asserts.InitBuiltinBaseDeclaration(prevHeaders)
		if err != nil {
			panic(err)
		}
	}
}
