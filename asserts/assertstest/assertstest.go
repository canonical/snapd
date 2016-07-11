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
func GenerateKey(bits int) (asserts.PrivateKey, *packet.PrivateKey) {
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		panic(fmt.Errorf("failed to create private key: %v", err))
	}
	pkt := packet.NewRSAPrivateKey(time.Now(), priv)
	return asserts.OpenPGPPrivateKey(pkt), pkt
}

// ReadPrivKey reads a PGP private key (either armored or simply base64 encoded). It panics on error.
func ReadPrivKey(pk string) (asserts.PrivateKey, *packet.PrivateKey) {
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
	return asserts.OpenPGPPrivateKey(pkPkt), pkPkt
}

// A sample developer key.
// See systestkeys for a prebuilt set of trusted keys and assertions.
const (
	DevKey = `-----BEGIN PGP PRIVATE KEY BLOCK-----
Version: GnuPG v1

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
=r01O
-----END PGP PRIVATE KEY BLOCK-----
`

	DevKeyID = "b52cf438a2d078f7"

	DevKeyFingerprint = "42a3050d365c10d5c093abeeb52cf438a2d078f7"
)

// GPGImportKey imports the given PGP armored key into the GnuPG setup at homedir. It panics on error.
func GPGImportKey(homedir, armoredKey string) {
	gpg := exec.Command("gpg", "--homedir", homedir, "-q", "--batch", "--import", "--armor")
	gpg.Stdin = bytes.NewBufferString(armoredKey)
	out, err := gpg.CombinedOutput()
	if err != nil {
		panic(fmt.Errorf("cannot import test key into GPG setup at %q: %v (%q)", homedir, err, out))
	}
}

// A SignerDB can sign assertions using its key pairs.
type SignerDB interface {
	Sign(assertType *asserts.AssertionType, headers map[string]string, body []byte, keyID string) (asserts.Assertion, error)
}

// NewAccount creates an account assertion for username, it fills in values for other missing headers as needed. It panics on error.
func NewAccount(db SignerDB, username string, otherHeaders map[string]string, keyID string) *asserts.Account {
	if otherHeaders == nil {
		otherHeaders = make(map[string]string)
	}
	otherHeaders["username"] = username
	if otherHeaders["account-id"] == "" {
		otherHeaders["account-id"] = strutil.MakeRandomString(32)
	}
	if otherHeaders["display-name"] == "" {
		otherHeaders["display-name"] = strings.ToTitle(username)
	}
	if otherHeaders["validation"] == "" {
		otherHeaders["validation"] = "unproven"
	}
	if otherHeaders["timestamp"] == "" {
		otherHeaders["timestamp"] = time.Now().Format(time.RFC3339)
	}
	a, err := db.Sign(asserts.AccountType, otherHeaders, nil, keyID)
	if err != nil {
		panic(err)
	}
	return a.(*asserts.Account)
}

// NewAccountKey creates an account-key assertion for the account, it fills in values for missing headers as needed. In panics on error.
func NewAccountKey(db SignerDB, acct *asserts.Account, otherHeaders map[string]string, pubKey asserts.PublicKey, keyID string) *asserts.AccountKey {
	if otherHeaders == nil {
		otherHeaders = make(map[string]string)
	}
	otherHeaders["account-id"] = acct.AccountID()
	otherHeaders["public-key-id"] = pubKey.ID()
	otherHeaders["public-key-fingerprint"] = pubKey.Fingerprint()
	if otherHeaders["since"] == "" {
		otherHeaders["since"] = time.Now().Format(time.RFC3339)
	}
	if otherHeaders["until"] == "" {
		since, err := time.Parse(time.RFC3339, otherHeaders["since"])
		if err != nil {
			panic(err)
		}
		otherHeaders["until"] = since.AddDate(5, 0, 0).Format(time.RFC3339)
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
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: asserts.NewMemoryKeypairManager(),
	})
	if err != nil {
		panic(err)
	}
	err = db.ImportKey(authorityID, privKey)
	if err != nil {
		panic(err)
	}
	return &SigningDB{
		AuthorityID: authorityID,
		KeyID:       privKey.PublicKey().ID(),
		Database:    db,
	}
}

func (db *SigningDB) Sign(assertType *asserts.AssertionType, headers map[string]string, body []byte, keyID string) (asserts.Assertion, error) {
	headers["authority-id"] = db.AuthorityID
	if keyID == "" {
		keyID = db.KeyID
	}
	return db.Database.Sign(assertType, headers, body, keyID)
}

func (db *SigningDB) PublicKey(keyID string) (asserts.PublicKey, error) {
	if keyID == "" {
		keyID = db.KeyID
	}
	return db.Database.PublicKey(db.AuthorityID, keyID)
}

// StoreStack realises a store-like set of founding trusted assertions and signing setup.
type StoreStack struct {
	// Trusted authority assertions.
	TrustedAccount *asserts.Account
	TrustedKey     *asserts.AccountKey
	Trusted        []asserts.Assertion

	// Signing assertion db that signs with the root private key.
	RootSigning *SigningDB

	// The store-like signing functionality that signs with a store key, setup to also store assertions if desired. It stores a default account-key for the store private key, see also the StoreStack.Key method.
	*SigningDB
}

// NewStoreStack creates a new store assertion stack. It panics on error.
func NewStoreStack(authorityID string, rootPrivKey, storePrivKey asserts.PrivateKey) *StoreStack {
	rootSigning := NewSigningDB(authorityID, rootPrivKey)
	trustedAcct := NewAccount(rootSigning, authorityID, map[string]string{
		"account-id": authorityID,
		"validation": "certified",
	}, "")
	trustedKey := NewAccountKey(rootSigning, trustedAcct, nil, rootPrivKey.PublicKey(), "")
	trusted := []asserts.Assertion{trustedAcct, trustedKey}

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: asserts.NewMemoryKeypairManager(),
		Backstore:      asserts.NewMemoryBackstore(),
		Trusted:        trusted,
	})
	if err != nil {
		panic(err)
	}
	err = db.ImportKey(authorityID, storePrivKey)
	if err != nil {
		panic(err)
	}
	storeKey := NewAccountKey(rootSigning, trustedAcct, nil, storePrivKey.PublicKey(), "")
	err = db.Add(storeKey)
	if err != nil {
		panic(err)
	}

	return &StoreStack{
		TrustedAccount: trustedAcct,
		TrustedKey:     trustedKey,
		Trusted:        trusted,

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
		"account-id":    ss.AuthorityID,
		"public-key-id": keyID,
	})
	if err == asserts.ErrNotFound {
		return nil
	}
	if err != nil {
		panic(err)
	}
	return key.(*asserts.AccountKey)
}
