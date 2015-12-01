// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/dirs"
)

type sysDBSuite struct {
	probeAssert asserts.Assertion
}

var _ = Suite(&sysDBSuite{})

func (sdbs *sysDBSuite) SetUpTest(c *C) {
	tmpdir := c.MkDir()
	cfg0 := &asserts.DatabaseConfig{Path: filepath.Join(tmpdir, "asserts-db0")}
	db0, err := asserts.OpenDatabase(cfg0)
	c.Assert(err, IsNil)
	trustedFingerp, err := db0.ImportKey("canonical", asserts.WrapPrivateKey(testPrivKey0))
	c.Assert(err, IsNil)
	trustedPubKey, err := db0.ExportPublicKey("canonical", trustedFingerp)
	c.Assert(err, IsNil)
	trustedPubKeyEncoded, err := asserts.EncodePublicKey(trustedPubKey)
	c.Assert(err, IsNil)
	// self-signed
	headers := map[string]string{
		"authority-id": "canonical",
		"account-id":   "canonical",
		"fingerprint":  trustedFingerp,
		"since":        "2015-11-20T15:04:00Z",
		"until":        "2500-11-20T15:04:00Z",
	}
	trustedAccKey, err := db0.Sign(asserts.AccountKeyType, headers, trustedPubKeyEncoded, trustedFingerp)
	c.Assert(err, IsNil)

	fakeRoot := filepath.Join(tmpdir, "root")
	err = os.Mkdir(fakeRoot, os.ModePerm)
	c.Assert(err, IsNil)
	dirs.SetRootDir(fakeRoot)

	err = os.MkdirAll(filepath.Dir(dirs.SnapTrustedAccountKey), os.ModePerm)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapTrustedAccountKey, asserts.Encode(trustedAccKey), os.ModePerm)
	c.Assert(err, IsNil)

	headers = map[string]string{
		"authority-id": "canonical",
		"primary-key":  "0",
	}
	sdbs.probeAssert, err = db0.Sign(asserts.AssertionType("test-only"), headers, nil, "")
	c.Assert(err, IsNil)
}

func (sdbs *sysDBSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (sdbs *sysDBSuite) TestOpenSysDatabase(c *C) {
	db, err := asserts.OpenSysDatabase()
	c.Assert(err, IsNil)
	c.Check(db, NotNil)

	err = db.Check(sdbs.probeAssert)
	c.Check(err, IsNil)
}
