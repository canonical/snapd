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

package main_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapKeysSuite) TestExportKeyNonexistent(c *C) {
	_, err := snap.Parser().ParseArgs([]string{"export-key", "nonexistent"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot find key named \"nonexistent\" in GPG keyring")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestExportKeyDefault(c *C) {
	rest, err := snap.Parser().ParseArgs([]string{"export-key"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	pubKey, err := asserts.DecodePublicKey(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(pubKey.ID(), Equals, "g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestExportKeyNonDefault(c *C) {
	rest, err := snap.Parser().ParseArgs([]string{"export-key", "another"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	pubKey, err := asserts.DecodePublicKey(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(pubKey.ID(), Equals, "DVQf1U4mIsuzlQqAebjjTPYtYJ-GEhJy0REuj3zvpQYTZ7EJj7adBxIXLJ7Vmk3L")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestExportKeyAccount(c *C) {
	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)
	storeSigning := assertstest.NewStoreStack("canonical", rootPrivKey, storePrivKey)
	manager := asserts.NewGPGKeypairManager()
	assertstest.NewAccount(storeSigning, "developer1", nil, "")
	rest, err := snap.Parser().ParseArgs([]string{"export-key", "another", "--account=developer1"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	assertion, err := asserts.Decode(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(assertion.Type(), Equals, asserts.AccountKeyRequestType)
	c.Check(assertion.Revision(), Equals, 0)
	c.Check(assertion.HeaderString("account-id"), Equals, "developer1")
	c.Check(assertion.HeaderString("name"), Equals, "another")
	c.Check(assertion.HeaderString("public-key-sha3-384"), Equals, "DVQf1U4mIsuzlQqAebjjTPYtYJ-GEhJy0REuj3zvpQYTZ7EJj7adBxIXLJ7Vmk3L")
	since, err := time.Parse(time.RFC3339, assertion.HeaderString("since"))
	c.Assert(err, IsNil)
	zone, offset := since.Zone()
	c.Check(zone, Equals, "UTC")
	c.Check(offset, Equals, 0)
	c.Check(s.Stderr(), Equals, "")
	privKey, err := manager.Get(assertion.HeaderString("public-key-sha3-384"))
	c.Assert(err, IsNil)
	err = asserts.SignatureCheck(assertion, privKey.PublicKey())
	c.Assert(err, IsNil)
}
