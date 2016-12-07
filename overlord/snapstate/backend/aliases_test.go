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

package backend_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
)

type aliasesSuite struct {
	be backend.Backend
}

var _ = Suite(&aliasesSuite{})

func (s *aliasesSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, IsNil)
}

func (s *aliasesSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *aliasesSuite) TestMissingAliases(c *C) {
	err := os.Symlink("x.foo", filepath.Join(dirs.SnapBinariesDir, "foo"))
	c.Assert(err, IsNil)

	aliases, err := s.be.MissingAliases([]*backend.Alias{{"a", "a"}, {"foo", "foo"}})
	c.Assert(err, IsNil)
	c.Check(aliases, DeepEquals, []*backend.Alias{{"a", "a"}})
}

func (s *aliasesSuite) TestMatchingAliases(c *C) {
	err := os.Symlink("x.foo", filepath.Join(dirs.SnapBinariesDir, "foo"))
	c.Assert(err, IsNil)
	err = os.Symlink("y.bar", filepath.Join(dirs.SnapBinariesDir, "bar"))
	c.Assert(err, IsNil)

	aliases, err := s.be.MatchingAliases([]*backend.Alias{{"a", "a"}, {"foo", "x.foo"}, {"bar", "x.bar"}})
	c.Assert(err, IsNil)
	c.Check(aliases, DeepEquals, []*backend.Alias{{"foo", "x.foo"}})
}

func (s *aliasesSuite) TestUpdateAliasesAdd(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	match, err := s.be.MatchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, len(aliases))
}

func (s *aliasesSuite) TestUpdateAliasesRemove(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	match, err := s.be.MatchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, len(aliases))

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	missing, err := s.be.MissingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(missing, HasLen, len(aliases))

	match, err = s.be.MatchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, 0)
}

func (s *aliasesSuite) TestRemoveSnapAliases(c *C) {
	aliases := []*backend.Alias{{"x", "x"}, {"bar", "x.bar"}, {"baz", "y.baz"}, {"y", "y"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.RemoveSnapAliases("x")
	c.Assert(err, IsNil)

	match, err := s.be.MatchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, DeepEquals, []*backend.Alias{{"baz", "y.baz"}, {"y", "y"}})
}
