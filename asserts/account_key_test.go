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
	//"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
)

type accountKeySuite struct{}

var _ = Suite(&accountKeySuite{})

func (aks *accountKeySuite) TestDecodeOK(c *C) {
	pk, err := asserts.GeneratePrivateKeyInTest()
	c.Assert(err, IsNil)
	fp := hex.EncodeToString(pk.PublicKey.Fingerprint[:])
	since, err := time.Parse(time.RFC822, "16 Nov 15 15:04 UTC")
	c.Assert(err, IsNil)
	until := since.AddDate(1, 0, 0)
	buf := new(bytes.Buffer)
	err = pk.PublicKey.Serialize(buf)
	body := "openpgp" + base64.StdEncoding.EncodeToString(buf.Bytes())

	encoded := "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: acc-id1\n" +
		"fingerprint: " + fp + "\n" +
		"since: " + since.Format(time.RFC3339) + "\n" +
		"until: " + until.Format(time.RFC3339) + "\n" +
		fmt.Sprintf("body-length: %v", len(body)) + "\n\n" +
		body + "\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountKeyType)
	accKey := a.(*asserts.AccountKey)
	c.Check(accKey.AccountID(), Equals, "acc-id1")
	c.Check(accKey.Since(), Equals, since)
	c.Check(accKey.Until(), Equals, until)
}
