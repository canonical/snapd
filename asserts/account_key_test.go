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

package asserts_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
)

type accountKeySuite struct {
	pubKeyBody           string
	fp                   string
	since, until         time.Time
	sinceLine, untilLine string
}

var _ = Suite(&accountKeySuite{})

func (aks *accountKeySuite) SetUpSuite(c *C) {
	pk, err := asserts.GeneratePrivateKeyInTest()
	c.Assert(err, IsNil)
	aks.fp = hex.EncodeToString(pk.PublicKey.Fingerprint[:])
	aks.since, err = time.Parse(time.RFC822, "16 Nov 15 15:04 UTC")
	c.Assert(err, IsNil)
	aks.until = aks.since.AddDate(1, 0, 0)
	buf := new(bytes.Buffer)
	err = pk.PublicKey.Serialize(buf)
	c.Assert(err, IsNil)
	aks.pubKeyBody = "openpgp " + base64.StdEncoding.EncodeToString(buf.Bytes())
	aks.sinceLine = "since: " + aks.since.Format(time.RFC3339) + "\n"
	aks.untilLine = "until: " + aks.until.Format(time.RFC3339) + "\n"
}

func (aks *accountKeySuite) TestDecodeOK(c *C) {
	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"fingerprint: " + aks.fp + "\n" +
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
		"fingerprint: " + aks.fp + "\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"

	invalidHeaderTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: acc-id1\n", "", "account-id header is mandatory"},
		{aks.sinceLine, "", "since header is mandatory"},
		{aks.untilLine, "", "until header is mandatory"},
		{aks.sinceLine, "since: 12:30\n", "since header is not a RFC3339 date: .*"},
		{aks.untilLine, "until: " + aks.since.Format(time.RFC3339) + "\n", `invalid 'since' and 'until' times \(no gap after 'since' till 'until'\)`},
		{"fingerprint: " + aks.fp + "\n", "", "missing fingerprint header"},
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
		"fingerprint: " + aks.fp + "\n" +
		aks.sinceLine +
		aks.untilLine

	invalidPublicKeyTests := []struct{ body, expectedErr string }{
		{"", "empty public key"},
		{"stuff", "public key: expected format and base64 data separated by space"},
		{"openpgp _", "public key: could not decode base64 data: .*"},
		{strings.Replace(aks.pubKeyBody, "openpgp", "mystery", 1), `unsupported public key format: "mystery"`},
		{"openpgp anVuaw==", "could not parse public key data: .*"},
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
		"fingerprint: 00\n" +
		aks.sinceLine +
		aks.untilLine +
		fmt.Sprintf("body-length: %v", len(aks.pubKeyBody)) + "\n\n" +
		aks.pubKeyBody + "\n\n" +
		"openpgp c2ln"

	_, err := asserts.Decode([]byte(invalid))
	c.Check(err, ErrorMatches, accKeyErrPrefix+"public key does not match provided fingerprint")
}

func (aks *accountKeySuite) TestAccountKeyCheck(c *C) {
	trustedKey, err := asserts.GeneratePrivateKeyInTest()
	c.Assert(err, IsNil)

	headers := map[string]string{
		"authority-id": "canonical",
		"account-id":   "acc-id1",
		"fingerprint":  aks.fp,
		"since":        aks.since.Format(time.RFC3339),
		"until":        aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.BuildAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	cfg := &asserts.DatabaseConfig{
		Path: rootDir,
		TrustedKeys: map[string][]asserts.PublicKey{
			"canonical": {asserts.WrapPublicKey(&trustedKey.PublicKey)},
		},
	}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)

	err = db.Check(accKey)
	c.Assert(err, IsNil)
}

func (aks *accountKeySuite) TestAccountKeyAddAndFind(c *C) {
	trustedKey, err := asserts.GeneratePrivateKeyInTest()
	c.Assert(err, IsNil)

	headers := map[string]string{
		"authority-id": "canonical",
		"account-id":   "acc-id1",
		"fingerprint":  aks.fp,
		"since":        aks.since.Format(time.RFC3339),
		"until":        aks.until.Format(time.RFC3339),
	}
	accKey, err := asserts.BuildAndSignInTest(asserts.AccountKeyType, headers, []byte(aks.pubKeyBody), trustedKey)
	c.Assert(err, IsNil)

	rootDir := filepath.Join(c.MkDir(), "asserts-db")
	cfg := &asserts.DatabaseConfig{
		Path: rootDir,
		TrustedKeys: map[string][]asserts.PublicKey{
			"canonical": {asserts.WrapPublicKey(&trustedKey.PublicKey)},
		},
	}
	db, err := asserts.OpenDatabase(cfg)
	c.Assert(err, IsNil)

	err = db.Add(accKey)
	c.Assert(err, IsNil)

	found, err := db.Find(asserts.AccountKeyType, map[string]string{
		"account-id":  "acc-id1",
		"fingerprint": aks.fp,
	})
	c.Assert(err, IsNil)
	c.Assert(found, NotNil)
	c.Check(found.Body(), DeepEquals, []byte(aks.pubKeyBody))
}
