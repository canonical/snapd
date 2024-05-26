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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapKeysSuite) TestExportKeyNonexistent(c *C) {
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"export-key", "nonexistent"}))
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot export key named \"nonexistent\": cannot find key pair in GPG keyring")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestExportKeyDefault(c *C) {
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"export-key"}))

	c.Assert(rest, DeepEquals, []string{})
	pubKey := mylog.Check2(asserts.DecodePublicKey(s.stdout.Bytes()))

	c.Check(pubKey.ID(), Equals, "g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestExportKeyNonDefault(c *C) {
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"export-key", "another"}))

	c.Assert(rest, DeepEquals, []string{})
	pubKey := mylog.Check2(asserts.DecodePublicKey(s.stdout.Bytes()))

	c.Check(pubKey.ID(), Equals, "DVQf1U4mIsuzlQqAebjjTPYtYJ-GEhJy0REuj3zvpQYTZ7EJj7adBxIXLJ7Vmk3L")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestExportKeyAccount(c *C) {
	storeSigning := assertstest.NewStoreStack("canonical", nil)
	manager := asserts.NewGPGKeypairManager()
	assertstest.NewAccount(storeSigning, "developer1", nil, "")
	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"export-key", "another", "--account=developer1"}))

	c.Assert(rest, DeepEquals, []string{})
	assertion := mylog.Check2(asserts.Decode(s.stdout.Bytes()))

	c.Check(assertion.Type(), Equals, asserts.AccountKeyRequestType)
	c.Check(assertion.Revision(), Equals, 0)
	c.Check(assertion.HeaderString("account-id"), Equals, "developer1")
	c.Check(assertion.HeaderString("name"), Equals, "another")
	c.Check(assertion.HeaderString("public-key-sha3-384"), Equals, "DVQf1U4mIsuzlQqAebjjTPYtYJ-GEhJy0REuj3zvpQYTZ7EJj7adBxIXLJ7Vmk3L")
	since := mylog.Check2(time.Parse(time.RFC3339, assertion.HeaderString("since")))

	zone, offset := since.Zone()
	c.Check(zone, Equals, "UTC")
	c.Check(offset, Equals, 0)
	c.Check(s.Stderr(), Equals, "")
	privKey := mylog.Check2(manager.Get(assertion.HeaderString("public-key-sha3-384")))

	mylog.Check(asserts.SignatureCheck(assertion, privKey.PublicKey()))

}
