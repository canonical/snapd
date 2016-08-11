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
=vsmN
-----END PGP PRIVATE KEY BLOCK-----
`

	DevKeyID = "ukBXMs6SJYkeChOBhuZr4jw3Go2zJUqPitYeYJRoEH_y2p4tueu6XHnaC3GaJs9m"

	DevKeyPGPFingerprint = "6eb134408271d1393b235bc7b8c918d236415515"
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
	if otherHeaders["since"] == nil {
		otherHeaders["since"] = time.Now().Format(time.RFC3339)
	}
	if otherHeaders["until"] == nil {
		since, err := time.Parse(time.RFC3339, otherHeaders["since"].(string))
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

func (db *SigningDB) Sign(assertType *asserts.AssertionType, headers map[string]interface{}, body []byte, keyID string) (asserts.Assertion, error) {
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
	trustedAcct := NewAccount(rootSigning, authorityID, map[string]interface{}{
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
			KeyID:       storeKey.PublicKeySHA3_384(),
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
	if err == asserts.ErrNotFound {
		return nil
	}
	if err != nil {
		panic(err)
	}
	return key.(*asserts.AccountKey)
}
