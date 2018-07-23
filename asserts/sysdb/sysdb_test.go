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

package sysdb_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
)

func TestSysDB(t *testing.T) { TestingT(t) }

type sysDBSuite struct {
	extraTrusted []asserts.Assertion
	extraGeneric []asserts.Assertion
	otherModel   *asserts.Model
	probeAssert  asserts.Assertion
}

var _ = Suite(&sysDBSuite{})

func (sdbs *sysDBSuite) SetUpTest(c *C) {
	tmpdir := c.MkDir()

	pk, _ := assertstest.GenerateKey(752)

	signingDB := assertstest.NewSigningDB("can0nical", pk)

	trustedAcct := assertstest.NewAccount(signingDB, "can0nical", map[string]interface{}{
		"account-id": "can0nical",
		"validation": "verified",
		"timestamp":  "2015-11-20T15:04:00Z",
	}, "")

	trustedAccKey := assertstest.NewAccountKey(signingDB, trustedAcct, map[string]interface{}{
		"account-id": "can0nical",
		"since":      "2015-11-20T15:04:00Z",
		"until":      "2500-11-20T15:04:00Z",
	}, pk.PublicKey(), "")

	sdbs.extraTrusted = []asserts.Assertion{trustedAcct, trustedAccKey}

	otherAcct := assertstest.NewAccount(signingDB, "gener1c", map[string]interface{}{
		"account-id": "gener1c",
		"validation": "verified",
		"timestamp":  "2015-11-20T15:04:00Z",
	}, "")

	sdbs.extraGeneric = []asserts.Assertion{otherAcct}

	a, err := signingDB.Sign(asserts.ModelType, map[string]interface{}{
		"series":    "16",
		"brand-id":  "can0nical",
		"model":     "other-model",
		"classic":   "true",
		"timestamp": "2015-11-20T15:04:00Z",
	}, nil, "")
	c.Assert(err, IsNil)
	sdbs.otherModel = a.(*asserts.Model)

	fakeRoot := filepath.Join(tmpdir, "root")

	err = os.Mkdir(fakeRoot, os.ModePerm)
	c.Assert(err, IsNil)
	dirs.SetRootDir(fakeRoot)

	sdbs.probeAssert = assertstest.NewAccount(signingDB, "probe", nil, "")
}

func (sdbs *sysDBSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (sdbs *sysDBSuite) TestTrusted(c *C) {
	trusted := sysdb.Trusted()
	c.Check(trusted, HasLen, 2)

	restore := sysdb.InjectTrusted(sdbs.extraTrusted)
	defer restore()

	trustedEx := sysdb.Trusted()
	c.Check(trustedEx, HasLen, 4)
}

func (sdbs *sysDBSuite) TestGeneric(c *C) {
	generic := sysdb.Generic()
	c.Check(generic, HasLen, 2)

	restore := sysdb.InjectGeneric(sdbs.extraGeneric)
	defer restore()

	genericEx := sysdb.Generic()
	c.Check(genericEx, HasLen, 3)
}

func (sdbs *sysDBSuite) TestGenericClassicModel(c *C) {
	m := sysdb.GenericClassicModel()
	c.Assert(m, NotNil)

	c.Check(m.AuthorityID(), Equals, "generic")
	c.Check(m.BrandID(), Equals, "generic")
	c.Check(m.Model(), Equals, "generic-classic")
	c.Check(m.Classic(), Equals, true)

	r := sysdb.MockGenericClassicModel(sdbs.otherModel)
	defer r()

	m = sysdb.GenericClassicModel()
	c.Check(m, Equals, sdbs.otherModel)
}

func (sdbs *sysDBSuite) TestOpenSysDatabase(c *C) {
	db, err := sysdb.Open()
	c.Assert(err, IsNil)
	c.Check(db, NotNil)

	// check trusted
	_, err = db.Find(asserts.AccountKeyType, map[string]string{
		"account-id":          "canonical",
		"public-key-sha3-384": "-CvQKAwRQ5h3Ffn10FILJoEZUXOv6km9FwA80-Rcj-f-6jadQ89VRswHNiEB9Lxk",
	})
	c.Assert(err, IsNil)

	trustedAcc, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": "canonical",
	})
	c.Assert(err, IsNil)

	c.Check(trustedAcc.(*asserts.Account).Validation(), Equals, "verified")

	err = db.Check(trustedAcc)
	c.Check(err, IsNil)

	// check generic
	genericAcc, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": "generic",
	})
	c.Assert(err, IsNil)
	_, err = db.FindMany(asserts.AccountKeyType, map[string]string{
		"account-id": "generic",
		"name":       "models",
	})
	c.Assert(err, IsNil)

	c.Check(genericAcc.(*asserts.Account).Validation(), Equals, "verified")

	err = db.Check(genericAcc)
	c.Check(err, IsNil)

	err = db.Check(sysdb.GenericClassicModel())
	c.Check(err, IsNil)

	// extraneous
	err = db.Check(sdbs.probeAssert)
	c.Check(err, ErrorMatches, "no matching public key.*")
}

func (sdbs *sysDBSuite) TestOpenSysDatabaseExtras(c *C) {
	restore := sysdb.InjectTrusted(sdbs.extraTrusted)
	defer restore()

	db, err := sysdb.Open()
	c.Assert(err, IsNil)
	c.Check(db, NotNil)

	err = db.Check(sdbs.probeAssert)
	c.Check(err, IsNil)
}

func (sdbs *sysDBSuite) TestOpenSysDatabaseBackstoreOpenFail(c *C) {
	// make it not world-writeable
	oldUmask := syscall.Umask(0)
	os.MkdirAll(filepath.Join(dirs.SnapAssertsDBDir, "asserts-v0"), 0777)
	syscall.Umask(oldUmask)

	db, err := sysdb.Open()
	c.Assert(err, ErrorMatches, "assert storage root unexpectedly world-writable: .*")
	c.Check(db, IsNil)
}

func (sdbs *sysDBSuite) TestOpenSysDatabaseKeypairManagerOpenFail(c *C) {
	// make it not world-writeable
	oldUmask := syscall.Umask(0)
	os.MkdirAll(filepath.Join(dirs.SnapAssertsDBDir, "private-keys-v1"), 0777)
	syscall.Umask(oldUmask)

	db, err := sysdb.Open()
	c.Assert(err, ErrorMatches, "assert storage root unexpectedly world-writable: .*")
	c.Check(db, IsNil)
}
