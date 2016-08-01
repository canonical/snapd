// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

type accountKeySuite struct {
	pubKeyBody           string
	fp, keyid            string
	since, until         time.Time
	sinceLine, untilLine string
}

var _ = Suite(&accountKeySuite{})

func (aks *accountKeySuite) SetUpSuite(c *C) {
	cfg1 := &asserts.DatabaseConfig{
		KeypairManager: asserts.NewMemoryKeypairManager(),
	}
	accDb, err := asserts.OpenDatabase(cfg1)
	c.Assert(err, IsNil)
	pk := testPrivKey1
	err = accDb.ImportKey("acc-id1", pk)
	c.Assert(err, IsNil)
	aks.fp = pk.PublicKey().Fingerprint()
	aks.keyid = pk.PublicKey().ID()

	pubKey, err := accDb.PublicKey("acc-id1", aks.keyid)
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
		"public-key-id: " + aks.keyid + "\n" +
		"public-key-fingerprint: " + aks.fp + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountKeyType)
	accKey := a.(*asserts.AccountKey)
	c.Check(accKey.AccountID(), Equals, "acc-id1")
	c.Check(accKey.PublicKeyFingerprint(), Equals, aks.fp)
	c.Check(accKey.PublicKeyID(), Equals, aks.keyid)
	c.Check(accKey.Since(), Equals, aks.since)
	c.Check(accKey.Until(), Equals, aks.until)
}

const (
	accKeyErrPrefix = "assertion account-key: "
)

func (aks *accountKeySuite) TestDecodeInvalidHeaders(c *C) {
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"public-key-id: " + aks.keyid + "\n" +
		"public-key-fingerprint: " + aks.fp + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"

	invalidHeaderTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: acc-id1\n", "", `"account-id" header is mandatory`},
		{"account-id: acc-id1\n", "account-id: \n", `"account-id" header should not be empty`},
		{"public-key-id: " + aks.keyid + "\n", "", `"public-key-id" header is mandatory`},
		{"public-key-id: " + aks.keyid + "\n", "public-key-id: \n", `"public-key-id" header should not be empty`},
		{"public-key-fingerprint: " + aks.fp + "\n", "", `"public-key-fingerprint" header is mandatory`},
		{"public-key-fingerprint: " + aks.fp + "\n", "public-key-fingerprint: \n", `"public-key-fingerprint" header should not be empty`},
		{aks.sinceLine, "", `"since" header is mandatory`},
		{aks.sinceLine, "since: \n", `"since" header should not be empty`},
		{aks.sinceLine, "since: 12:30\n", `"since" header is not a RFC3339 date: .*`},
		{aks.sinceLine, "since: \n", `"since" header should not be empty`},
		{aks.untilLine, "", `"until" header is mandatory`},
		{aks.untilLine, "until: \n", `"until" header should not be empty`},
		{aks.untilLine, "until: " + aks.since.Format(time.RFC3339) + "\n", `invalid 'since' and 'until' times \(no gap after 'since' till 'until'\)`},
		{aks.untilLine, "until: \n", `"until" header should not be empty`},
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
		"public-key-id: " + aks.keyid + "\n" +
		"public-key-fingerprint: " + aks.fp + "\n" +
		aks.sinceLine +
		aks.untilLine

	invalidPublicKeyTests := []struct{ body, expectedErr string }{
		{"", "empty public key"},
		{"stuff", "public key: expected format and base64 data separated by space"},
		{"openpgp _", "public key: cannot decode base64 data: .*"},
		{strings.Replace(aks.pubKeyBody, "openpgp", "mystery", 1), `unsupported public key format: "mystery"`},
		{"openpgp anVuaw==", "cannot decode public key data: .*"},
	}

	for _, test := range invalidPublicKeyTests {
		invalid := headers +
			fmt.Sprintf("body-length: %v", len(test.body)) + "\n\n" +
			test.body + "\n\n" +
			"openpgp c2ln"

		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, accKeyErrPrefix+test.expectedErr)
	}
}

func (aks *accountKeySuite) TestDecodeFingerprintMismatch(c *C) {
	invalid := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"public-key-id: " + aks.keyid + "\n" +
		"public-key-fingerprint: 00\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"

	_, err := asserts.Decode([]byte(invalid))
	c.Check(err, ErrorMatches, accKeyErrPrefix+"public key does not match provided fingerprint")
}

func (aks *accountKeySuite) TestDecodeKeyIDMismatch(c *C) {
	invalid := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"public-key-id: aa\n" +
		"public-key-fingerprint: " + aks.fp + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"

	_, err := asserts.Decode([]byte(invalid))
	c.Check(err, ErrorMatches, accKeyErrPrefix+"public key does not match provided key id")
}

func (aks *accountKeySuite) openDB(c *C) *asserts.Database {
	trustedKey := testPrivKey0

	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs, err := asserts.OpenFSBackstore(topDir)
	c.Assert(err, IsNil)
	cfg := &asserts.DatabaseConfig{
		Backstore:      bs,
		KeypairManager: asserts.NewMemoryKeypairManager(),
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
		"authority-id":           "canonical",
		"account-id":             "acc-id1",
		"public-key-id":          aks.keyid,
		"public-key-fingerprint": aks.fp,
		"since":                  aks.since.Format(time.RFC3339),
		"until":                  aks.until.Format(time.RFC3339),
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
		"authority-id":           "canonical",
		"account-id":             "acc-id1",
		"public-key-id":          aks.keyid,
		"public-key-fingerprint": aks.fp,
		"since":                  aks.since.Format(time.RFC3339),
		"until":                  aks.until.Format(time.RFC3339),
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
		"account-id":             "acc-id1",
		"public-key-id":          aks.keyid,
		"public-key-fingerprint": aks.fp,
		"since":                  aks.since.Format(time.RFC3339),
		"until":                  aks.until.Format(time.RFC3339),
	}
	accKey, err := otherDB.Sign(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), "")
	c.Assert(err, IsNil)

	err = db.Check(accKey)
	c.Assert(err, ErrorMatches, `account-key assertion for "acc-id1" is not signed by a directly trusted authority:.*`)
}

func (aks *accountKeySuite) TestAccountKeyAddAndFind(c *C) {
	trustedKey := testPrivKey0

	headers := map[string]interface{}{
		"authority-id":           "canonical",
		"account-id":             "acc-id1",
		"public-key-id":          aks.keyid,
		"public-key-fingerprint": aks.fp,
		"since":                  aks.since.Format(time.RFC3339),
		"until":                  aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	db := aks.openDB(c)

	aks.prereqAccount(c, db)

	err = db.Add(accKey)
	c.Assert(err, IsNil)

	found, err := db.Find(asserts.AccountKeyType, map[string]string{
		"account-id":    "acc-id1",
		"public-key-id": aks.keyid,
	})
	c.Assert(err, IsNil)
	c.Assert(found, NotNil)
	c.Check(found.Body(), DeepEquals, []byte(aks.pubKeyBody))
}

func (aks *accountKeySuite) TestPublicKeyIsValidAt(c *C) {
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"public-key-id: " + aks.keyid + "\n" +
		"public-key-fingerprint: " + aks.fp + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	accKey := a.(*asserts.AccountKey)

	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since), Equals, true)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since.AddDate(0, 0, -1)), Equals, false)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.since.AddDate(0, 0, 1)), Equals, true)

	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.until), Equals, false)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.until.AddDate(0, -1, 0)), Equals, true)
	c.Check(asserts.AccountKeyIsKeyValidAt(accKey, aks.until.AddDate(0, 1, 0)), Equals, false)
}

func (aks *accountKeySuite) TestPrerequisites(c *C) {
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"public-key-id: " + aks.keyid + "\n" +
		"public-key-fingerprint: " + aks.fp + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	prereqs := a.Prerequisites()
	c.Assert(prereqs, HasLen, 1)
	c.Check(prereqs[0], DeepEquals, &asserts.Ref{
		Type:       asserts.AccountType,
		PrimaryKey: []string{"acc-id1"},
	})
}
