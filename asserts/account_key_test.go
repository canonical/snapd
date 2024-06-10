// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2021 Canonical Ltd
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

package asserts_test

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

type accountKeySuite struct {
	privKey              asserts.PrivateKey
	pubKeyBody           string
	keyID                string
	since, until         time.Time
	sinceLine, untilLine string
}

var _ = Suite(&accountKeySuite{})

func (aks *accountKeySuite) SetUpSuite(c *C) {
	cfg1 := &asserts.DatabaseConfig{}
	accDb, err := asserts.OpenDatabase(cfg1)
	c.Assert(err, IsNil)
	aks.privKey = testPrivKey1
	err = accDb.ImportKey(aks.privKey)
	c.Assert(err, IsNil)
	aks.keyID = aks.privKey.PublicKey().ID()

	pubKey, err := accDb.PublicKey(aks.keyID)
	c.Assert(err, IsNil)
	pubKeyEncoded, err := asserts.EncodePublicKey(pubKey)
	c.Assert(err, IsNil)
	aks.pubKeyBody = string(pubKeyEncoded)

	aks.since, err = time.Parse(time.RFC822, "16 Nov 15 15:04 UTC")
	c.Assert(err, IsNil)
	aks.until = aks.since.AddDate(1, 0, 0)
	aks.sinceLine = "since: " + aks.since.Format(time.RFC3339) + "\n"
	aks.untilLine = "until: " + aks.until.Format(time.RFC3339) + "\n"
}

func (aks *accountKeySuite) TestDecodeOK(c *C) {
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountKeyType)
	accKey := a.(*asserts.AccountKey)
	c.Check(accKey.AccountID(), Equals, "acc-id1")
	c.Check(accKey.Name(), Equals, "default")
	c.Check(accKey.PublicKeyID(), Equals, aks.keyID)
	c.Check(accKey.Since(), Equals, aks.since)

	// no constraints, anything goes
	c.Check(accKey.ConstraintsPrecheck(asserts.AccountKeyType, nil), IsNil)
}

func (aks *accountKeySuite) TestDecodeNoName(c *C) {
	// XXX: remove this test once name is mandatory
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountKeyType)
	accKey := a.(*asserts.AccountKey)
	c.Check(accKey.AccountID(), Equals, "acc-id1")
	c.Check(accKey.Name(), Equals, "")
	c.Check(accKey.PublicKeyID(), Equals, aks.keyID)
	c.Check(accKey.Since(), Equals, aks.since)
}

func (aks *accountKeySuite) TestUntil(c *C) {

	untilSinceLine := "until: " + aks.since.Format(time.RFC3339) + "\n"

	tests := []struct {
		untilLine string
		until     time.Time
	}{
		{"", time.Time{}},           // zero time default
		{aks.untilLine, aks.until},  // in the future
		{untilSinceLine, aks.since}, // same as since
	}

	for _, test := range tests {
		c.Log(test)
		encoded := "type: account-key\n" +
			"authority-id: canonical\n" +
			"account-id: acc-id1\n" +
			"name: default\n" +
			"public-key-sha3-384: " + aks.keyID + "\n" +
			aks.sinceLine +
			test.untilLine +
			fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
			"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
			aks.pubKeyBody + "\n\n" +
			"openpgp c2ln"
		a, err := asserts.Decode([]byte(encoded))
		c.Assert(err, IsNil)
		accKey := a.(*asserts.AccountKey)
		c.Check(accKey.Until(), Equals, test.until)
	}
}

const (
	accKeyErrPrefix    = "assertion account-key: "
	accKeyReqErrPrefix = "assertion account-key-request: "
)

func (aks *accountKeySuite) TestDecodeInvalidHeaders(c *C) {

	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="

	untilPast := aks.since.AddDate(-1, 0, 0)
	untilPastLine := "until: " + untilPast.Format(time.RFC3339) + "\n"

	invalidHeaderTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: acc-id1\n", "", `"account-id" header is mandatory`},
		{"account-id: acc-id1\n", "account-id: \n", `"account-id" header should not be empty`},
		// XXX: enable this once name is mandatory
		// {"name: default\n", "", `"name" header is mandatory`},
		{"name: default\n", "name: \n", `"name" header should not be empty`},
		{"name: default\n", "name: a b\n", `"name" header contains invalid characters: "a b"`},
		{"name: default\n", "name: -default\n", `"name" header contains invalid characters: "-default"`},
		{"name: default\n", "name: foo:bar\n", `"name" header contains invalid characters: "foo:bar"`},
		{"name: default\n", "name: a--b\n", `"name" header contains invalid characters: "a--b"`},
		{"name: default\n", "name: 42\n", `"name" header contains invalid characters: "42"`},
		{"public-key-sha3-384: " + aks.keyID + "\n", "", `"public-key-sha3-384" header is mandatory`},
		{"public-key-sha3-384: " + aks.keyID + "\n", "public-key-sha3-384: \n", `"public-key-sha3-384" header should not be empty`},
		{aks.sinceLine, "", `"since" header is mandatory`},
		{aks.sinceLine, "since: \n", `"since" header should not be empty`},
		{aks.sinceLine, "since: 12:30\n", `"since" header is not a RFC3339 date: .*`},
		{aks.sinceLine, "since: \n", `"since" header should not be empty`},
		{aks.untilLine, "until: \n", `"until" header is not a RFC3339 date: .*`},
		{aks.untilLine, "until: 12:30\n", `"until" header is not a RFC3339 date: .*`},
		{aks.untilLine, untilPastLine, `'until' time cannot be before 'since' time`},
	}

	for _, test := range invalidHeaderTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, accKeyErrPrefix+test.expectedErr)
	}
}

func (aks *accountKeySuite) TestDecodeInvalidPublicKey(c *C) {
	headers := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		aks.untilLine

	raw, err := base64.StdEncoding.DecodeString(aks.pubKeyBody)
	c.Assert(err, IsNil)
	spurious := base64.StdEncoding.EncodeToString(append(raw, "gorp"...))

	invalidPublicKeyTests := []struct{ body, expectedErr string }{
		{"", "cannot decode public key: no data"},
		{"==", "cannot decode public key: .*"},
		{"stuff", "cannot decode public key: .*"},
		{"AnNpZw==", "unsupported public key format version: 2"},
		{"AUJST0tFTg==", "cannot decode public key: .*"},
		{spurious, "public key has spurious trailing data"},
	}

	for _, test := range invalidPublicKeyTests {
		invalid := headers +
			fmt.Sprintf("body-length: %v", len(test.body)) + "\n" +
			"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
			test.body + "\n\n" +
			"AXNpZw=="

		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, accKeyErrPrefix+test.expectedErr)
	}
}

func (aks *accountKeySuite) TestDecodeKeyIDMismatch(c *C) {
	invalid := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: aa\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="

	_, err := asserts.Decode([]byte(invalid))
	c.Check(err, ErrorMatches, accKeyErrPrefix+"public key does not match provided key id")
}

func (aks *accountKeySuite) openDB(c *C) *asserts.Database {
	trustedKey := testPrivKey0

	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs, err := asserts.OpenFSBackstore(topDir)
	c.Assert(err, IsNil)
	cfg := &asserts.DatabaseConfig{
		Backstore: bs,
		Trusted: []asserts.Assertion{
			asserts.BootstrapAccountForTest("canonical"),
			asserts.BootstrapAccountKeyForTest("canonical", trustedKey.PublicKey()),
		},
	}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)
	return db
}

func (aks *accountKeySuite) prereqAccount(c *C, db *asserts.Database) {
	trustedKey := testPrivKey0

	headers := map[string]interface{}{
		"authority-id": "canonical",
		"display-name": "Acct1",
		"account-id":   "acc-id1",
		"username":     "acc-id1",
		"validation":   "unproven",
		"timestamp":    aks.since.Format(time.RFC3339),
	}
	acct1, err := asserts.AssembleAndSignInTest(asserts.AccountType, headers, nil, trustedKey)
	c.Assert(err, IsNil)

	// prereq
	db.Add(acct1)
}

func (aks *accountKeySuite) TestAccountKeyCheck(c *C) {
	trustedKey := testPrivKey0

	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "acc-id1",
		"name":                "default",
		"public-key-sha3-384": aks.keyID,
		"since":               aks.since.Format(time.RFC3339),
		"until":               aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)

	aks.prereqAccount(c, db)

	err = db.Check(accKey)
	c.Assert(err, IsNil)
}

func (aks *accountKeySuite) TestAccountKeyCheckNoAccount(c *C) {
	trustedKey := testPrivKey0

	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "acc-id1",
		"name":                "default",
		"public-key-sha3-384": aks.keyID,
		"since":               aks.since.Format(time.RFC3339),
		"until":               aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)

	err = db.Check(accKey)
	c.Assert(err, ErrorMatches, `account-key assertion for "acc-id1" does not have a matching account assertion`)
}

func (aks *accountKeySuite) TestAccountKeyCheckUntrustedAuthority(c *C) {
	trustedKey := testPrivKey0

	db := aks.openDB(c)
	storeDB := assertstest.NewSigningDB("canonical", trustedKey)
	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := map[string]interface{}{
		"account-id":          "acc-id1",
		"name":                "default",
		"public-key-sha3-384": aks.keyID,
		"since":               aks.since.Format(time.RFC3339),
		"until":               aks.until.Format(time.RFC3339),
	}
	accKey, err := otherDB.Sign(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), "")
	c.Assert(err, IsNil)

	err = db.Check(accKey)
	c.Assert(err, ErrorMatches, `account-key assertion for "acc-id1" is not signed by a directly trusted authority:.*`)
}

func (aks *accountKeySuite) TestAccountKeyCheckSameNameAndNewRevision(c *C) {
	trustedKey := testPrivKey0

	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "acc-id1",
		"name":                "default",
		"public-key-sha3-384": aks.keyID,
		"since":               aks.since.Format(time.RFC3339),
		"until":               aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)
	aks.prereqAccount(c, db)

	err = db.Add(accKey)
	c.Assert(err, IsNil)

	headers["revision"] = "1"
	newAccKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	err = db.Check(newAccKey)
	c.Assert(err, IsNil)
}

func (aks *accountKeySuite) TestAccountKeyCheckSameAccountAndDifferentName(c *C) {
	trustedKey := testPrivKey0

	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "acc-id1",
		"name":                "default",
		"public-key-sha3-384": aks.keyID,
		"since":               aks.since.Format(time.RFC3339),
		"until":               aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)
	aks.prereqAccount(c, db)

	err = db.Add(accKey)
	c.Assert(err, IsNil)

	newPrivKey, _ := assertstest.GenerateKey(752)
	err = db.ImportKey(newPrivKey)
	c.Assert(err, IsNil)
	newPubKey, err := db.PublicKey(newPrivKey.PublicKey().ID())
	c.Assert(err, IsNil)
	newPubKeyEncoded, err := asserts.EncodePublicKey(newPubKey)
	c.Assert(err, IsNil)

	headers["name"] = "another"
	headers["public-key-sha3-384"] = newPubKey.ID()
	newAccKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, newPubKeyEncoded, trustedKey)
	c.Assert(err, IsNil)

	err = db.Check(newAccKey)
	c.Assert(err, IsNil)
}

func (aks *accountKeySuite) TestAccountKeyCheckSameNameAndDifferentAccount(c *C) {
	trustedKey := testPrivKey0

	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "acc-id1",
		"name":                "default",
		"public-key-sha3-384": aks.keyID,
		"since":               aks.since.Format(time.RFC3339),
		"until":               aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)
	err = db.ImportKey(trustedKey)
	c.Assert(err, IsNil)
	aks.prereqAccount(c, db)

	err = db.Add(accKey)
	c.Assert(err, IsNil)

	newPrivKey, _ := assertstest.GenerateKey(752)
	err = db.ImportKey(newPrivKey)
	c.Assert(err, IsNil)
	newPubKey, err := db.PublicKey(newPrivKey.PublicKey().ID())
	c.Assert(err, IsNil)
	newPubKeyEncoded, err := asserts.EncodePublicKey(newPubKey)
	c.Assert(err, IsNil)

	acct2 := assertstest.NewAccount(db, "acc-id2", map[string]interface{}{
		"authority-id": "canonical",
		"account-id":   "acc-id2",
	}, trustedKey.PublicKey().ID())
	db.Add(acct2)

	headers["account-id"] = "acc-id2"
	headers["public-key-sha3-384"] = newPubKey.ID()
	headers["revision"] = "1"
	newAccKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, newPubKeyEncoded, trustedKey)
	c.Assert(err, IsNil)

	err = db.Check(newAccKey)
	c.Assert(err, IsNil)
}

func (aks *accountKeySuite) TestAccountKeyCheckNameClash(c *C) {
	trustedKey := testPrivKey0

	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "acc-id1",
		"name":                "default",
		"public-key-sha3-384": aks.keyID,
		"since":               aks.since.Format(time.RFC3339),
		"until":               aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)
	aks.prereqAccount(c, db)

	err = db.Add(accKey)
	c.Assert(err, IsNil)

	newPrivKey, _ := assertstest.GenerateKey(752)
	err = db.ImportKey(newPrivKey)
	c.Assert(err, IsNil)
	newPubKey, err := db.PublicKey(newPrivKey.PublicKey().ID())
	c.Assert(err, IsNil)
	newPubKeyEncoded, err := asserts.EncodePublicKey(newPubKey)
	c.Assert(err, IsNil)

	headers["public-key-sha3-384"] = newPubKey.ID()
	headers["revision"] = "1"
	newAccKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, newPubKeyEncoded, trustedKey)
	c.Assert(err, IsNil)

	err = db.Check(newAccKey)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`account-key assertion for "acc-id1" with ID %q has the same name "default" as existing ID %q`, newPubKey.ID(), aks.keyID))
}

func (aks *accountKeySuite) TestAccountKeyAddAndFind(c *C) {
	trustedKey := testPrivKey0

	headers := map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "acc-id1",
		"name":                "default",
		"public-key-sha3-384": aks.keyID,
		"since":               aks.since.Format(time.RFC3339),
		"until":               aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)

	aks.prereqAccount(c, db)

	err = db.Add(accKey)
	c.Assert(err, IsNil)

	found, err := db.Find(asserts.AccountKeyType, map[string]string{
		"account-id":          "acc-id1",
		"public-key-sha3-384": aks.keyID,
	})
	c.Assert(err, IsNil)
	c.Assert(found, NotNil)
	c.Check(found.Body(), DeepEquals, []byte(aks.pubKeyBody))
}

func (aks *accountKeySuite) TestPublicKeyIsValidAt(c *C) {
	// With since and until, i.e. signing account-key expires.
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	accKey := a.(*asserts.AccountKey)

	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since), Equals, true)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since.AddDate(0, 0, -1)), Equals, false)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since.AddDate(0, 0, 1)), Equals, true)

	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.until), Equals, false)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.until.AddDate(0, -1, 0)), Equals, true)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.until.AddDate(0, 1, 0)), Equals, false)

	// With no until, i.e. signing account-key never expires.
	encoded = "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	accKey = a.(*asserts.AccountKey)

	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since), Equals, true)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since.AddDate(0, 0, -1)), Equals, false)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since.AddDate(0, 0, 1)), Equals, true)

	// With since == until, i.e. signing account-key has been revoked.
	encoded = "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		"until: " + aks.since.Format(time.RFC3339) + "\n" +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"
	a, err = asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	accKey = a.(*asserts.AccountKey)

	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since), Equals, false)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since.AddDate(0, 0, -1)), Equals, false)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since.AddDate(0, 0, 1)), Equals, false)

	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.until), Equals, false)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.until.AddDate(0, -1, 0)), Equals, false)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.until.AddDate(0, 1, 0)), Equals, false)
}

func (aks *accountKeySuite) TestPublicKeyIsValidAssumingCurTimeWithinWithUntilPunctual(c *C) {
	// With since and until, i.e. signing account-key expires.
	// Key is valid over [since, until)
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	accKey := a.(*asserts.AccountKey)

	tests := []struct {
		timePt time.Time
		valid  bool
	}{
		{aks.since, true},
		{aks.since.AddDate(0, 3, 0), true},
		{aks.since.AddDate(0, -2, 0), false},
		{aks.until, false},
		{aks.until.AddDate(0, 3, 0), false},
		{aks.until.AddDate(0, -2, 0), true},
	}

	for _, t := range tests {
		c.Check(asserts.IsValidAssumingCurTimeWithin(accKey, t.timePt, t.timePt), Equals, t.valid)
	}
}

func (aks *accountKeySuite) TestPublicKeyIsValidAssumingCurTimeWithinNoUntilPunctual(c *C) {
	// With since but no until, i.e. signing account-key never expires.
	// Key is valid for time >= since.
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	accKey := a.(*asserts.AccountKey)

	later := aks.until
	tests := []struct {
		timePt time.Time
		valid  bool
	}{
		{aks.since, true},
		{aks.since.AddDate(0, 3, 0), true},
		{aks.since.AddDate(0, -2, 0), false},
		{later, true},
		{later.AddDate(0, 3, 0), true},
	}

	for _, t := range tests {
		c.Check(asserts.IsValidAssumingCurTimeWithin(accKey, t.timePt, t.timePt), Equals, t.valid)
	}
}

func (aks *accountKeySuite) TestPublicKeyIsValidAssumingCurTimeWithinWithUntilInterval(c *C) {
	// With since and until, i.e. signing account-key expires.
	// Key is valid over [since, until)
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	accKey := a.(*asserts.AccountKey)

	z := time.Time{}

	tests := []struct {
		earliest time.Time
		latest   time.Time
		valid    bool
	}{
		{aks.since, aks.until, true},
		{aks.since, aks.since.AddDate(0, 3, 0), true},
		{aks.since.AddDate(0, 1, 0), aks.since.AddDate(0, 3, 0), true},
		{aks.since.AddDate(0, 1, 0), aks.until, true},
		{aks.until, aks.until.AddDate(0, 3, 0), false},
		{aks.until.AddDate(0, 2, 0), aks.until.AddDate(0, 3, 0), false},
		{aks.since.AddDate(0, -1, 0), aks.since, true},
		{aks.since.AddDate(0, -1, 0), aks.since.AddDate(0, 1, 0), true},
		{aks.since.AddDate(0, -2, 0), aks.since.AddDate(0, -2, 0), false},
		{aks.until.AddDate(0, -1, 0), aks.until.AddDate(0, 1, 0), true},
		{aks.since, z, true},
		{aks.since.AddDate(0, 1, 0), z, true},
		{aks.since.AddDate(0, -3, 0), z, true},
		{aks.until, z, false},
		{aks.until.AddDate(0, 1, 0), z, false},
		// with earliest set to time.Time zero
		{z, aks.since, true},
		{z, aks.since.AddDate(0, 1, 0), true},
		{z, aks.since.AddDate(0, -2, 0), false},
		{z, aks.until.AddDate(0, 1, 0), true},
		{z, z, true},
	}

	for _, t := range tests {
		c.Check(asserts.IsValidAssumingCurTimeWithin(accKey, t.earliest, t.latest), Equals, t.valid)
	}

}

func (aks *accountKeySuite) TestPublicKeyIsValidAssumingCurTimeWithinNoUntilInterval(c *C) {
	// With since but no until, i.e. signing account-key never expires.
	// Key is valid for time >= since.
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	accKey := a.(*asserts.AccountKey)

	z := time.Time{}
	later := aks.until

	tests := []struct {
		earliest time.Time
		latest   time.Time
		valid    bool
	}{
		{aks.since, later, true},
		{aks.since, aks.since.AddDate(0, 3, 0), true},
		{aks.since.AddDate(0, 1, 0), aks.since.AddDate(0, 3, 0), true},
		{aks.since.AddDate(0, 1, 0), later, true},
		{later, later.AddDate(0, 3, 0), true},
		{later.AddDate(0, 2, 0), later.AddDate(0, 3, 0), true},
		{aks.since.AddDate(0, -1, 0), aks.since, true},
		{aks.since.AddDate(0, -1, 0), aks.since.AddDate(0, 1, 0), true},
		{aks.since.AddDate(0, -2, 0), aks.since.AddDate(0, -2, 0), false},
		{later.AddDate(0, -1, 0), later.AddDate(0, 1, 0), true},
		{aks.since, z, true},
		{aks.since.AddDate(0, 1, 0), z, true},
		{aks.since.AddDate(0, -3, 0), z, true},
		{later, z, true},
		{later.AddDate(0, 1, 0), z, true},
		// with earliest set to time.Time zero
		{z, aks.since, true},
		{z, aks.since.AddDate(0, 1, 0), true},
		{z, aks.since.AddDate(0, -2, 0), false},
		{z, later.AddDate(0, 1, 0), true},
		{z, z, true},
	}

	for _, t := range tests {
		c.Check(asserts.IsValidAssumingCurTimeWithin(accKey, t.earliest, t.latest), Equals, t.valid)
	}

}

func (aks *accountKeySuite) TestPrerequisites(c *C) {
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	prereqs := a.Prerequisites()
	c.Assert(prereqs, HasLen, 1)
	c.Check(prereqs[0], DeepEquals, &asserts.Ref{
		Type:       asserts.AccountType,
		PrimaryKey: []string{"acc-id1"},
	})
}

func (aks *accountKeySuite) TestAccountKeyRequestHappy(c *C) {
	akr, err := asserts.SignWithoutAuthority(asserts.AccountKeyRequestType,
		map[string]interface{}{
			"account-id":          "acc-id1",
			"name":                "default",
			"public-key-sha3-384": aks.keyID,
			"since":               aks.since.Format(time.RFC3339),
		}, []byte(aks.pubKeyBody), aks.privKey)
	c.Assert(err, IsNil)

	// roundtrip
	a, err := asserts.Decode(asserts.Encode(akr))
	c.Assert(err, IsNil)

	akr2, ok := a.(*asserts.AccountKeyRequest)
	c.Assert(ok, Equals, true)

	db := aks.openDB(c)
	aks.prereqAccount(c, db)

	err = db.Check(akr2)
	c.Check(err, IsNil)

	c.Check(akr2.AccountID(), Equals, "acc-id1")
	c.Check(akr2.Name(), Equals, "default")
	c.Check(akr2.PublicKeyID(), Equals, aks.keyID)
	c.Check(akr2.Since(), Equals, aks.since)
}

func (aks *accountKeySuite) TestAccountKeyRequestUntil(c *C) {
	db := aks.openDB(c)
	aks.prereqAccount(c, db)

	tests := []struct {
		untilHeader string
		until       time.Time
	}{
		{"", time.Time{}}, // zero time default
		{aks.until.Format(time.RFC3339), aks.until}, // in the future
		{aks.since.Format(time.RFC3339), aks.since}, // same as since
	}

	for _, test := range tests {
		c.Log(test)
		headers := map[string]interface{}{
			"account-id":          "acc-id1",
			"name":                "default",
			"public-key-sha3-384": aks.keyID,
			"since":               aks.since.Format(time.RFC3339),
		}
		if test.untilHeader != "" {
			headers["until"] = test.untilHeader
		}
		akr, err := asserts.SignWithoutAuthority(asserts.AccountKeyRequestType, headers, []byte(aks.pubKeyBody), aks.privKey)
		c.Assert(err, IsNil)
		a, err := asserts.Decode(asserts.Encode(akr))
		c.Assert(err, IsNil)
		akr2 := a.(*asserts.AccountKeyRequest)
		c.Check(akr2.Until(), Equals, test.until)
		err = db.Check(akr2)
		c.Check(err, IsNil)
	}
}

func (aks *accountKeySuite) TestAccountKeyRequestAddAndFind(c *C) {
	akr, err := asserts.SignWithoutAuthority(asserts.AccountKeyRequestType,
		map[string]interface{}{
			"account-id":          "acc-id1",
			"name":                "default",
			"public-key-sha3-384": aks.keyID,
			"since":               aks.since.Format(time.RFC3339),
		}, []byte(aks.pubKeyBody), aks.privKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)
	aks.prereqAccount(c, db)

	err = db.Add(akr)
	c.Assert(err, IsNil)

	found, err := db.Find(asserts.AccountKeyRequestType, map[string]string{
		"account-id":          "acc-id1",
		"public-key-sha3-384": aks.keyID,
	})
	c.Assert(err, IsNil)
	c.Assert(found, NotNil)
	c.Check(found.Body(), DeepEquals, []byte(aks.pubKeyBody))
}

func (aks *accountKeySuite) TestAccountKeyRequestDecodeInvalid(c *C) {
	encoded := "type: account-key-request\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: " + aks.privKey.PublicKey().ID() + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="

	untilPast := aks.since.AddDate(-1, 0, 0)
	untilPastLine := "until: " + untilPast.Format(time.RFC3339) + "\n"

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: acc-id1\n", "", `"account-id" header is mandatory`},
		{"account-id: acc-id1\n", "account-id: \n", `"account-id" header should not be empty`},
		{"name: default\n", "", `"name" header is mandatory`},
		{"name: default\n", "name: \n", `"name" header should not be empty`},
		{"name: default\n", "name: a b\n", `"name" header contains invalid characters: "a b"`},
		{"name: default\n", "name: -default\n", `"name" header contains invalid characters: "-default"`},
		{"name: default\n", "name: foo:bar\n", `"name" header contains invalid characters: "foo:bar"`},
		{"public-key-sha3-384: " + aks.keyID + "\n", "", `"public-key-sha3-384" header is mandatory`},
		{"public-key-sha3-384: " + aks.keyID + "\n", "public-key-sha3-384: \n", `"public-key-sha3-384" header should not be empty`},
		{aks.sinceLine, "", `"since" header is mandatory`},
		{aks.sinceLine, "since: \n", `"since" header should not be empty`},
		{aks.sinceLine, "since: 12:30\n", `"since" header is not a RFC3339 date: .*`},
		{aks.sinceLine, "since: \n", `"since" header should not be empty`},
		{aks.untilLine, "until: \n", `"until" header is not a RFC3339 date: .*`},
		{aks.untilLine, "until: 12:30\n", `"until" header is not a RFC3339 date: .*`},
		{aks.untilLine, untilPastLine, `'until' time cannot be before 'since' time`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, accKeyReqErrPrefix+test.expectedErr)
	}
}

func (aks *accountKeySuite) TestAccountKeyRequestDecodeInvalidPublicKey(c *C) {
	headers := "type: account-key-request\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		aks.untilLine

	raw, err := base64.StdEncoding.DecodeString(aks.pubKeyBody)
	c.Assert(err, IsNil)
	spurious := base64.StdEncoding.EncodeToString(append(raw, "gorp"...))

	invalidPublicKeyTests := []struct{ body, expectedErr string }{
		{"", "cannot decode public key: no data"},
		{"==", "cannot decode public key: .*"},
		{"stuff", "cannot decode public key: .*"},
		{"AnNpZw==", "unsupported public key format version: 2"},
		{"AUJST0tFTg==", "cannot decode public key: .*"},
		{spurious, "public key has spurious trailing data"},
	}

	for _, test := range invalidPublicKeyTests {
		invalid := headers +
			fmt.Sprintf("body-length: %v", len(test.body)) + "\n" +
			"sign-key-sha3-384: " + aks.privKey.PublicKey().ID() + "\n\n" +
			test.body + "\n\n" +
			"AXNpZw=="

		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, accKeyReqErrPrefix+test.expectedErr)
	}
}

func (aks *accountKeySuite) TestAccountKeyRequestDecodeKeyIDMismatch(c *C) {
	invalid := "type: account-key-request\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"public-key-sha3-384: aa\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: " + aks.privKey.PublicKey().ID() + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="

	_, err := asserts.Decode([]byte(invalid))
	c.Check(err, ErrorMatches, "assertion account-key-request: public key does not match provided key id")
}

func (aks *accountKeySuite) TestAccountKeyRequestNoAccount(c *C) {
	headers := map[string]interface{}{
		"account-id":          "acc-id1",
		"name":                "default",
		"public-key-sha3-384": aks.keyID,
		"since":               aks.since.Format(time.RFC3339),
	}
	akr, err := asserts.SignWithoutAuthority(asserts.AccountKeyRequestType, headers, []byte(aks.pubKeyBody), aks.privKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)

	err = db.Check(akr)
	c.Assert(err, ErrorMatches, `account-key-request assertion for "acc-id1" does not have a matching account assertion`)
}

func (aks *accountKeySuite) TestDecodeConstraints(c *C) {
	encoded := "type: account-key\n" +
		"format: 1\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"constraints:\n" +
		"  -\n" +
		"    headers:\n" +
		"      type: model\n" +
		"      model: foo-.*\n" +
		"  -\n" +
		"    headers:\n" +
		"      type: preseed\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountKeyType)
	accKey := a.(*asserts.AccountKey)
	c.Check(accKey.AccountID(), Equals, "acc-id1")
	c.Check(accKey.Name(), Equals, "default")
	c.Check(accKey.PublicKeyID(), Equals, aks.keyID)
	c.Check(accKey.Since(), Equals, aks.since)
}

func (aks *accountKeySuite) TestDecodeConstraintsInvalid(c *C) {
	const constr = "\n" +
		"  -\n" +
		"    headers:\n" +
		"      type: model\n" +
		"      model: foo-.*\n"
	encoded := "type: account-key\n" +
		"format: 1\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"constraints:" +
		constr +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="

	invalidHeaderTests := []struct{ original, invalid, expectedErr string }{
		{constr, " x\n", "assertions constraints must be a list of maps"}, {constr, "\n  - foo\n", "assertions constraints must be a list of maps"},
		{constr, "\n  -\n    headers: x\n", `"headers" constraint must be a map`},
		{constr, "\n  -\n    header:\n      t: x\n", `"headers" constraint mandatory in asserions constraints`},
		{constr, "\n  -\n    headers:\n      t: x\n", "type header constraint mandatory in asserions constraints"},
		{constr, "\n  -\n    headers:\n      type:\n        - foo\n", "type header constraint must be a string"},
		{constr, "\n  -\n    headers:\n      type: preseed|model\n", "type header constraint must be a precise string and not a regexp"},
		{constr, "\n  -\n    headers:\n      type: foo\n      model: $X\n", `cannot compile headers constraint: cannot compile "model" constraint "\$X": no \$OP\(\) or \$REF constraints supported`},
	}
	for _, test := range invalidHeaderTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, accKeyErrPrefix+test.expectedErr)
	}
}

func (s *accountKeySuite) TestSuggestedFormat(c *C) {
	fmtnum, err := asserts.SuggestFormat(asserts.AccountKeyType, nil, nil)
	c.Assert(err, IsNil)
	c.Check(fmtnum, Equals, 0)

	headers := map[string]interface{}{
		"constraints": []interface{}{map[string]interface{}{"headers": nil}},
	}
	fmtnum, err = asserts.SuggestFormat(asserts.AccountKeyType, headers, nil)
	c.Assert(err, IsNil)
	c.Check(fmtnum, Equals, 1)
}

func (aks *accountKeySuite) TestCanSignAndConstraintsPrecheck(c *C) {
	encoded := "type: account-key\n" +
		"format: 1\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"name: default\n" +
		"constraints:\n" +
		"  -\n" +
		"    headers:\n" +
		"      type: model\n" +
		"      model: foo-.*\n" +
		"  -\n" +
		"    headers:\n" +
		"      type: preseed\n" +
		"public-key-sha3-384: " + aks.keyID + "\n" +
		aks.sinceLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountKeyType)
	accKey := a.(*asserts.AccountKey)
	headers := map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"series":       "16",
		"model":        "foo-200",
		"classic":      "true",
	}
	c.Check(accKey.ConstraintsPrecheck(asserts.ModelType, headers), IsNil)
	mfoo := assertstest.FakeAssertion(headers)
	c.Check(accKey.CanSign(mfoo), Equals, true)
	headers = map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"series":       "16",
		"model":        "goo-200",
		"classic":      "true",
	}
	c.Check(accKey.ConstraintsPrecheck(asserts.ModelType, headers), ErrorMatches, `headers do not match the account-key constraints`)
	mnotfoo := assertstest.FakeAssertion(headers)
	c.Check(accKey.CanSign(mnotfoo), Equals, false)
	headers = map[string]interface{}{
		"type":              "preseed",
		"authority-id":      "my-brand",
		"series":            "16",
		"brand-id":          "my-brand",
		"model":             "goo-200",
		"system-label":      "2023-07-17",
		"snaps":             []interface{}{},
		"artifact-sha3-384": "KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABs1No7BtXj",
	}
	c.Check(accKey.ConstraintsPrecheck(asserts.PreseedType, headers), IsNil)
	pr := assertstest.FakeAssertion(headers)
	c.Check(accKey.CanSign(pr), Equals, true)
	headers = map[string]interface{}{
		"type":         "snap-declaration",
		"authority-id": "my-brand",
		"series":       "16",
		"snap-id":      "snapid",
		"snap-name":    "foo",
		"publisher-id": "my-brand",
	}
	c.Check(accKey.ConstraintsPrecheck(asserts.ModelType, headers), ErrorMatches, `headers do not match the account-key constraints`)
	snapDecl := assertstest.FakeAssertion(headers)
	c.Check(accKey.CanSign(snapDecl), Equals, false)
}
