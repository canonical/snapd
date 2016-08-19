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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapKeysSuite) TestExportKeyRequiresName(c *C) {
	_, err := snap.Parser().ParseArgs([]string{"export-key"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "the required argument `<key-name>` was not provided")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestExportKeyNonexistent(c *C) {
	_, err := snap.Parser().ParseArgs([]string{"export-key", "nonexistent"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot find key named \"nonexistent\" in GPG keyring")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestExportKey(c *C) {
	rest, err := snap.Parser().ParseArgs([]string{"export-key", "default"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	pubKey, err := asserts.DecodePublicKey(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(pubKey.ID(), Equals, "2uDFKgzxAPJ4takHsVbPFjmszLvaxg431C1KmhKFPwcD96MLKWcKj9cFEePrAZRs")
	c.Check(s.Stderr(), Equals, "")
}
