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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type memKeypairMgtSuite struct {
	keypairMgr asserts.KeypairManager
}

var _ = Suite(&memKeypairMgtSuite{})

func (mkms *memKeypairMgtSuite) SetUpTest(c *C) {
	mkms.keypairMgr = asserts.NewMemoryKeypairManager()
}

func (mkms *memKeypairMgtSuite) TestPutAndGet(c *C) {
	pk1 := testPrivKey1
	keyID := pk1.PublicKey().ID()
	err := mkms.keypairMgr.Put("auth-id1", pk1)
	c.Assert(err, IsNil)

	got, err := mkms.keypairMgr.Get("auth-id1", keyID)
	c.Assert(err, IsNil)
	c.Assert(got, NotNil)
	c.Check(got.PublicKey().Fingerprint(), Equals, pk1.PublicKey().Fingerprint())
}

func (mkms *memKeypairMgtSuite) TestPutAlreadyExists(c *C) {
	pk1 := testPrivKey1
	err := mkms.keypairMgr.Put("auth-id1", pk1)
	c.Assert(err, IsNil)

	err = mkms.keypairMgr.Put("auth-id1", pk1)
	c.Check(err, ErrorMatches, "key pair with given key id already exists")
}

func (mkms *memKeypairMgtSuite) TestGetNotFound(c *C) {
	pk1 := testPrivKey1
	keyID := pk1.PublicKey().ID()

	got, err := mkms.keypairMgr.Get("auth-id1", keyID)
	c.Check(got, IsNil)
	c.Check(err, ErrorMatches, "cannot find key pair")

	err = mkms.keypairMgr.Put("auth-id1", pk1)
	c.Assert(err, IsNil)

	got, err = mkms.keypairMgr.Get("auth-id1", keyID+"x")
	c.Check(got, IsNil)
	c.Check(err, ErrorMatches, "cannot find key pair")
}
