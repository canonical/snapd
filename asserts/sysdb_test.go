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
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
)

type sysDBSuite struct {
	extraTrustedAccKey asserts.Assertion
	probeAssert        asserts.Assertion
}

var _ = Suite(&sysDBSuite{})

func (sdbs *sysDBSuite) SetUpTest(c *C) {
	tmpdir := c.MkDir()

	pk := testPrivKey0
	trustedPubKey := pk.PublicKey()
	trustedPubKeyEncoded, err := asserts.EncodePublicKey(trustedPubKey)
	c.Assert(err, IsNil)
	// self-signed
	headers := map[string]string{
		"authority-id":           "can0nical",
		"account-id":             "can0nical",
		"public-key-id":          trustedPubKey.ID(),
		"public-key-fingerprint": trustedPubKey.Fingerprint(),
		"since":                  "2015-11-20T15:04:00Z",
		"until":                  "2500-11-20T15:04:00Z",
	}
	trustedAccKey, err := asserts.AssembleAndSignInTest(asserts.AccountKeyType, headers, trustedPubKeyEncoded, pk)
	c.Assert(err, IsNil)
	sdbs.extraTrustedAccKey = trustedAccKey

	fakeRoot := filepath.Join(tmpdir, "root")
	err = os.Mkdir(fakeRoot, os.ModePerm)
	c.Assert(err, IsNil)
	dirs.SetRootDir(fakeRoot)

	headers = map[string]string{
		"authority-id": "can0nical",
		"primary-key":  "0",
	}
	sdbs.probeAssert, err = asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, pk)
	c.Assert(err, IsNil)
}

func (sdbs *sysDBSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (sdbs *sysDBSuite) TestTrusted(c *C) {
	trusted := asserts.Trusted()
	c.Check(trusted, HasLen, 2)

	restore := asserts.InjectTrusted([]asserts.Assertion{sdbs.extraTrustedAccKey})
	defer restore()

	trustedEx := asserts.Trusted()
	c.Check(trustedEx, HasLen, 3)
}

func (sdbs *sysDBSuite) TestOpenSysDatabase(c *C) {
	db, err := asserts.OpenSysDatabase()
	c.Assert(err, IsNil)
	c.Check(db, NotNil)

	// check trusted
	_, err = db.Find(asserts.AccountKeyType, map[string]string{
		"account-id":    "canonical",
		"public-key-id": "d4a55bea97d83720",
	})
	c.Assert(err, IsNil)

	trustedAcc, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": "canonical",
	})
	c.Assert(err, IsNil)

	err = db.Check(trustedAcc)
	c.Check(err, IsNil)

	// extraneous
	err = db.Check(sdbs.probeAssert)
	c.Check(err, ErrorMatches, "no matching public key.*")
}

func (sdbs *sysDBSuite) TestOpenSysDatabaseExtras(c *C) {
	restore := asserts.InjectTrusted([]asserts.Assertion{sdbs.extraTrustedAccKey})
	defer restore()

	db, err := asserts.OpenSysDatabase()
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

	db, err := asserts.OpenSysDatabase()
	c.Assert(err, ErrorMatches, "assert storage root unexpectedly world-writable: .*")
	c.Check(db, IsNil)
}

func (sdbs *sysDBSuite) TestOpenSysDatabaseKeypairManagerOpenFail(c *C) {
	// make it not world-writeable
	oldUmask := syscall.Umask(0)
	os.MkdirAll(filepath.Join(dirs.SnapAssertsDBDir, "private-keys-v0"), 0777)
	syscall.Umask(oldUmask)

	db, err := asserts.OpenSysDatabase()
	c.Assert(err, ErrorMatches, "assert storage root unexpectedly world-writable: .*")
	c.Check(db, IsNil)
}
