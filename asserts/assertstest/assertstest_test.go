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

	"github.com/ddkwork/golibrary/mylog"
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
	store := assertstest.NewStoreStack("super", nil)

	c.Check(store.TrustedAccount.AccountID(), Equals, "super")
	c.Check(store.TrustedAccount.Validation(), Equals, "verified")

	c.Check(store.TrustedKey.AccountID(), Equals, "super")
	c.Check(store.TrustedKey.Name(), Equals, "root")

	c.Check(store.GenericAccount.AccountID(), Equals, "generic")
	c.Check(store.GenericAccount.Validation(), Equals, "verified")

	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         store.Trusted,
		OtherPredefined: store.Generic,
	}))


	storeAccKey := store.StoreAccountKey("")
	c.Assert(storeAccKey, NotNil)

	c.Check(storeAccKey.AccountID(), Equals, "super")
	c.Check(storeAccKey.AccountID(), Equals, store.AuthorityID)
	c.Check(storeAccKey.PublicKeyID(), Equals, store.KeyID)
	c.Check(storeAccKey.Name(), Equals, "store")

	c.Check(store.GenericKey.AccountID(), Equals, "generic")
	c.Check(store.GenericKey.Name(), Equals, "serials")

	c.Check(store.GenericModelsKey.AccountID(), Equals, "generic")
	c.Check(store.GenericModelsKey.Name(), Equals, "models")

	g := mylog.Check2(store.Find(asserts.AccountType, map[string]string{
		"account-id": "generic",
	}))

	c.Assert(g.Headers(), DeepEquals, store.GenericAccount.Headers())

	g = mylog.Check2(store.Find(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": store.GenericKey.PublicKeyID(),
	}))

	c.Assert(g.Headers(), DeepEquals, store.GenericKey.Headers())

	g = mylog.Check2(store.Find(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": store.GenericModelsKey.PublicKeyID(),
	}))

	c.Assert(g.Headers(), DeepEquals, store.GenericModelsKey.Headers())

	acct := assertstest.NewAccount(store, "devel1", nil, "")
	c.Check(acct.Username(), Equals, "devel1")
	c.Check(acct.AccountID(), HasLen, 32)
	c.Check(acct.Validation(), Equals, "unproven")
	mylog.Check(db.Add(storeAccKey))

	mylog.Check(db.Add(acct))


	devKey, _ := assertstest.GenerateKey(752)

	acctKey := assertstest.NewAccountKey(store, acct, nil, devKey.PublicKey(), "")
	mylog.Check(db.Add(acctKey))


	c.Check(acctKey.Name(), Equals, "default")

	a := mylog.Check2(db.Find(asserts.AccountType, map[string]string{
		"account-id": "generic",
	}))

	c.Assert(a.Headers(), DeepEquals, store.GenericAccount.Headers())

	c.Check(store.GenericClassicModel.AuthorityID(), Equals, "generic")
	c.Check(store.GenericClassicModel.BrandID(), Equals, "generic")
	c.Check(store.GenericClassicModel.Model(), Equals, "generic-classic")
	c.Check(store.GenericClassicModel.Classic(), Equals, true)
	mylog.Check(db.Check(store.GenericClassicModel))

	mylog.Check(db.Add(store.GenericKey))

}

func (s *helperSuite) TestSigningAccounts(c *C) {
	brandKey, _ := assertstest.GenerateKey(752)

	store := assertstest.NewStoreStack("super", nil)

	sa := assertstest.NewSigningAccounts(store)
	sa.Register("my-brand", brandKey, map[string]interface{}{
		"validation": "verified",
	})

	acct := sa.Account("my-brand")
	c.Check(acct.Username(), Equals, "my-brand")
	c.Check(acct.Validation(), Equals, "verified")

	c.Check(sa.AccountKey("my-brand").PublicKeyID(), Equals, brandKey.PublicKey().ID())

	c.Check(sa.PublicKey("my-brand").ID(), Equals, brandKey.PublicKey().ID())

	model := sa.Model("my-brand", "my-model", map[string]interface{}{
		"classic": "true",
	})
	c.Check(model.BrandID(), Equals, "my-brand")
	c.Check(model.Model(), Equals, "my-model")
	c.Check(model.Classic(), Equals, true)

	// can also sign models for store account-id
	model = sa.Model("super", "pc", map[string]interface{}{
		"classic": "true",
	})
	c.Check(model.BrandID(), Equals, "super")
	c.Check(model.Model(), Equals, "pc")
}

func (s *helperSuite) TestSigningAccountsAccountsAndKeysPlusAddMany(c *C) {
	brandKey, _ := assertstest.GenerateKey(752)

	store := assertstest.NewStoreStack("super", nil)

	sa := assertstest.NewSigningAccounts(store)
	sa.Register("my-brand", brandKey, map[string]interface{}{
		"validation": "verified",
	})

	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   store.Trusted,
	}))

	mylog.Check(db.Add(store.StoreAccountKey("")))


	assertstest.AddMany(db, sa.AccountsAndKeys("my-brand")...)
	as := mylog.Check2(db.FindMany(asserts.AccountKeyType, map[string]string{
		"account-id": "my-brand",
	}))
	c.Check(err, IsNil)
	c.Check(as, HasLen, 1)

	// idempotent
	assertstest.AddMany(db, sa.AccountsAndKeys("my-brand")...)
}
