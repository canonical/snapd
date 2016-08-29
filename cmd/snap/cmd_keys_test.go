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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

type SnapKeysSuite struct {
	BaseSnapSuite
}

var _ = Suite(&SnapKeysSuite{})

func (s *SnapKeysSuite) SetUpTest(c *C) {
	s.BaseSnapSuite.SetUpTest(c)

	tempdir := c.MkDir()
	for _, fileName := range []string{"pubring.gpg", "secring.gpg", "trustdb.gpg"} {
		data, err := ioutil.ReadFile(filepath.Join("test-data", fileName))
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(tempdir, fileName), data, 0644)
		c.Assert(err, IsNil)
	}
	os.Setenv("SNAP_GNUPG_HOME", tempdir)
}

func (s *SnapKeysSuite) TearDownTest(c *C) {
	os.Unsetenv("SNAP_GNUPG_HOME")
	s.BaseSnapSuite.TearDownTest(c)
}

func (s *SnapKeysSuite) TestKeys(c *C) {
	rest, err := snap.Parser().ParseArgs([]string{"keys"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches, `Name +SHA3-384
default +g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ
another +DVQf1U4mIsuzlQqAebjjTPYtYJ-GEhJy0REuj3zvpQYTZ7EJj7adBxIXLJ7Vmk3L
`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapKeysSuite) TestKeysJSON(c *C) {
	rest, err := snap.Parser().ParseArgs([]string{"keys", "--json"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	expectedResponse := []snap.Key{
		snap.Key{
			Name:     "default",
			Sha3_384: "g4Pks54W_US4pZuxhgG_RHNAf_UeZBBuZyGRLLmMj1Do3GkE_r_5A5BFjx24ZwVJ",
		},
		snap.Key{
			Name:     "another",
			Sha3_384: "DVQf1U4mIsuzlQqAebjjTPYtYJ-GEhJy0REuj3zvpQYTZ7EJj7adBxIXLJ7Vmk3L",
		},
	}
	var obtainedResponse []snap.Key
	json.Unmarshal(s.stdout.Bytes(), &obtainedResponse)
	c.Check(obtainedResponse, DeepEquals, expectedResponse)
	c.Check(s.Stderr(), Equals, "")
}
