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
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
)

type memKeypairMgtSuite struct {
	keypairMgr asserts.KeypairManager
}

var _ = Suite(&memKeypairMgtSuite{})

func (mkms *memKeypairMgtSuite) SetUpTest(c *C) {
	mkms.keypairMgr = asserts.NewMemoryKeypairMananager()
}

func (mkms *memKeypairMgtSuite) TestImportAndKey(c *C) {
	pk1 := asserts.OpenPGPPrivateKey(testPrivKey1)
	fingerp, err := mkms.keypairMgr.ImportKey("auth-id1", pk1)
	c.Assert(err, IsNil)
	c.Check(fingerp, Equals, pk1.PublicKey().Fingerprint())

	got, err := mkms.keypairMgr.Key("auth-id1", fingerp)
	c.Assert(err, IsNil)
	c.Assert(got, NotNil)
	c.Check(got.PublicKey().Fingerprint(), Equals, fingerp)
}

func (mkms *memKeypairMgtSuite) TestKeyNotFound(c *C) {
	pk1 := asserts.OpenPGPPrivateKey(testPrivKey1)
	fingerp := pk1.PublicKey().Fingerprint()

	got, err := mkms.keypairMgr.Key("auth-id1", fingerp)
	c.Check(got, IsNil)
	c.Check(err, ErrorMatches, "no matching key pair found")

	_, err = mkms.keypairMgr.ImportKey("auth-id1", pk1)
	c.Assert(err, IsNil)

	got, err = mkms.keypairMgr.Key("auth-id1", "")
	c.Check(got, IsNil)
	c.Check(err, ErrorMatches, "no matching key pair found")
}

func (mkms *memKeypairMgtSuite) TestFindKey(c *C) {
	fingerp, err := mkms.keypairMgr.ImportKey("auth-id1", asserts.OpenPGPPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)

	got, err := mkms.keypairMgr.FindKey("auth-id1", fingerp[len(fingerp)-4:])
	c.Assert(err, IsNil)
	c.Assert(got, NotNil)
	c.Check(got.PublicKey().Fingerprint(), Equals, fingerp)
}

func (mkms *memKeypairMgtSuite) TestFindKeyNotFound(c *C) {
	got, err := mkms.keypairMgr.FindKey("auth-id1", "f")
	c.Check(got, IsNil)
	c.Check(err, ErrorMatches, "no matching key pair found")

	_, err = mkms.keypairMgr.ImportKey("auth-id1", asserts.OpenPGPPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)

	got, err = mkms.keypairMgr.FindKey("auth-id1", "z")
	c.Check(got, IsNil)
	c.Check(err, ErrorMatches, "no matching key pair found")
}

func (mkms *memKeypairMgtSuite) TestFindKeyAmbiguous(c *C) {
	_, err := mkms.keypairMgr.ImportKey("auth-id1", asserts.OpenPGPPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)
	_, err = mkms.keypairMgr.ImportKey("auth-id1", asserts.OpenPGPPrivateKey(testPrivKey2))
	c.Assert(err, IsNil)

	got, err := mkms.keypairMgr.FindKey("auth-id1", "")
	c.Check(got, IsNil)
	c.Check(err, ErrorMatches, "ambiguous search, more than one key pair found:.*")
}
