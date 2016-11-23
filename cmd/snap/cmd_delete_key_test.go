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
	"encoding/json"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapKeysSuite) TestDeleteKeyRequiresName(c *C) {
	_, err := snap.Parser().ParseArgs([]string{"delete-key"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "the required argument `<key-name>` was not provided")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestDeleteKeyNonexistent(c *C) {
	_, err := snap.Parser().ParseArgs([]string{"delete-key", "nonexistent"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot find key named \"nonexistent\" in GPG keyring")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestDeleteKey(c *C) {
	rest, err := snap.Parser().ParseArgs([]string{"delete-key", "another"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
	_, err = snap.Parser().ParseArgs([]string{"keys", "--json"})
	c.Assert(err, IsNil)
	expectedResponse := []snap.Key{
		{
			Name:     "default",
			Sha3_384: "g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ",
		},
	}
	var obtainedResponse []snap.Key
	json.Unmarshal(s.stdout.Bytes(), &obtainedResponse)
	c.Check(obtainedResponse, DeepEquals, expectedResponse)
	c.Check(s.Stderr(), Equals, "")
}
