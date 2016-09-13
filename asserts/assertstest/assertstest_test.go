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

package assertstest_test

import (
	"encoding/hex"
	"testing"
	"time"

	"golang.org/x/crypto/openpgp/packet"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

func TestAssertsTest(t *testing.T) { TestingT(t) }

type helperSuite struct{}

var _ = Suite(&helperSuite{})

func (s *helperSuite) TestReadPrivKeyArmored(c *C) {
	pk, rsaPrivKey := assertstest.ReadPrivKey(assertstest.DevKey)
	c.Check(pk, NotNil)
	c.Check(rsaPrivKey, NotNil)
	c.Check(pk.PublicKey().ID(), Equals, assertstest.DevKeyID)
	pkt := packet.NewRSAPrivateKey(time.Date(2016, time.January, 1, 0, 0, 0, 0, time.UTC), rsaPrivKey)
	c.Check(hex.EncodeToString(pkt.Fingerprint[:]), Equals, assertstest.DevKeyPGPFingerprint)
}

const (
	base64PrivKey = `
xcLYBFaU5cgBCAC/2wUYK7YzvL6f0ZxBfptFVfNmI7G9J9Eszdoq1NZZXaV+aYeC7eNU
1sKdO6wIRcw3lvybtq5W1n4D/jJAb2qXbB6BukuCGVXCLMEUdvheaVVcIZ/LwdbxmgMJsDFoHsDC
RzjkUVTU2b8sK6MwANIsSS5r8Lwm7FazD1qq50UdebsIx8dkjFR5VwrCYgOu1MO2Bqka7UU9as2q
4ZsFzpcS/so41kd4IPFEmNMlejhSjgCaixehpLeXypQVHLluV+oSPMV7GtE7Z6HO4V5cT2c9RdXg
l4jSKY91rHInkmSizF03laL3T/I6oj0FdZG9GB6QzqRCBTzK05cnVP1k7WFJABEBAAEAB/9spiIa
cBa88fSaGWB+Dq7r8yLmAuzTDEt/LgyRGPtSnJ/uGOEvGn0VPJH17ScdgDmIea8Ql8HfV5UBueDH
cNFSc15LZS8BvEs+rY2ig0VgYhJ/HGOcRmftZqS1xdwU9OWAoEjts8lwyOdkoknGE5Dyl3b8ldZX
zJvEx7s28cXITH4UwGEAMHEXrAMCjkcKPVbM7vW81uOWn0U1jMzmfmqrcLkSfvaCnep6+4QphKPy
B4DxJAI34EvJAru4iL5bWWvMeXkBZgmBy4g2SlYbk09cfTmhzw6di5GZtg+77yGACltPBA8MSbzF
v30apQ5iuI/hVin7U2/QtQHP4d0zUDbpBADusynnaFcDnPEUm4RdvNpujaBC/HfIpOstiS36RZy8
lZeVtffa/+DqzodZD9YF7zEVWeUiC5Os4THirYOZ04dM5yqR/GlKXMHGHaT+mnhD8g1hORx/LrMO
k5wUpD1NmloSjP/0pJRccuXq7O1QQfls1Hq1vOSh3cZ/aIvTONJ/YwQAzcK0/2SrnaUc3oCxMEuI
2FX0LsYDQiXzMK/x/lfZ/ywxt5J/q6CuaG3xXgSHlsk0M8Uo4acZqpCIFA9mwCPxKbrIOGnwJsI/
+sZBkngtZMSS88Vl32gnzpVWLGpbW2F7hnWrj1YigTcFUdi6TFNa7zHPASzCKxKKiz9YxEWWymME
AIbURnQJJOSfYgFyloQuA2QWyAK5Zu7qPworBoRo+PZPVb5yQmSUQ21VqNfzqIJz1EgiDZ0NyGid
uXAjn58O9tAq7IN5pTeHoTacZ75cI82kQkUxEnfiKjBO/AU30Y3COsIXhtbIXbtcitHSicp4lnpU
NejDkxUnC2wIvJzHWo1FQ18=
`
)

func (s *helperSuite) TestReadPrivKeyUnarmored(c *C) {
	pk, rsaPrivKey := assertstest.ReadPrivKey(base64PrivKey)
	c.Check(pk, NotNil)
	c.Check(rsaPrivKey, NotNil)
}

func (s *helperSuite) TestStoreStack(c *C) {
	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)

	store := assertstest.NewStoreStack("super", rootPrivKey, storePrivKey)

	c.Check(store.TrustedAccount.AccountID(), Equals, "super")
	c.Check(store.TrustedAccount.IsCertified(), Equals, true)

	c.Check(store.TrustedKey.AccountID(), Equals, "super")
	c.Check(store.TrustedKey.Name(), Equals, "root")

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   store.Trusted,
	})
	c.Assert(err, IsNil)

	storeAccKey := store.StoreAccountKey("")
	c.Assert(storeAccKey, NotNil)

	c.Check(storeAccKey.AccountID(), Equals, "super")
	c.Check(storeAccKey.AccountID(), Equals, store.AuthorityID)
	c.Check(storeAccKey.PublicKeyID(), Equals, store.KeyID)
	c.Check(storeAccKey.Name(), Equals, "store")

	acct := assertstest.NewAccount(store, "devel1", nil, "")
	c.Check(acct.Username(), Equals, "devel1")
	c.Check(acct.AccountID(), HasLen, 32)
	c.Check(acct.IsCertified(), Equals, false)

	err = db.Add(storeAccKey)
	c.Assert(err, IsNil)

	err = db.Add(acct)
	c.Assert(err, IsNil)

	devKey, _ := assertstest.GenerateKey(752)

	acctKey := assertstest.NewAccountKey(store, acct, nil, devKey.PublicKey(), "")

	err = db.Add(acctKey)
	c.Assert(err, IsNil)

	c.Check(acctKey.Name(), Equals, "default")
}
