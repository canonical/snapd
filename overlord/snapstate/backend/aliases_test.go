// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

func missingAliases(aliases []*backend.Alias) ([]*backend.Alias, error) {
	var res []*backend.Alias
	for _, cand := range aliases {
		_, err := os.Lstat(filepath.Join(dirs.SnapBinariesDir, cand.Name))
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			res = append(res, cand)
		}
	}
	return res, nil
}

func (s *aliasesSuite) TestMissingAliases(c *C) {
	err := os.Symlink("x.foo", filepath.Join(dirs.SnapBinariesDir, "foo"))
	c.Assert(err, IsNil)

	aliases, err := missingAliases([]*backend.Alias{{"a", "a"}, {"foo", "foo"}})
	c.Assert(err, IsNil)
	c.Check(aliases, DeepEquals, []*backend.Alias{{"a", "a"}})
}

func matchingAliases(aliases []*backend.Alias) ([]*backend.Alias, error) {
	var res []*backend.Alias
	for _, cand := range aliases {
		fn := filepath.Join(dirs.SnapBinariesDir, cand.Name)
		fileInfo, err := os.Lstat(fn)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			continue
		}
		if (fileInfo.Mode() & os.ModeSymlink) != 0 {
			target, err := os.Readlink(fn)
			if err != nil {
				return nil, err
			}
			if target == cand.Target {
				res = append(res, cand)
			}
		}
	}
	return res, nil
}

func (s *aliasesSuite) TestMatchingAliases(c *C) {
	err := os.Symlink("x.foo", filepath.Join(dirs.SnapBinariesDir, "foo"))
	c.Assert(err, IsNil)
	err = os.Symlink("y.bar", filepath.Join(dirs.SnapBinariesDir, "bar"))
	c.Assert(err, IsNil)

	aliases, err := matchingAliases([]*backend.Alias{{"a", "a"}, {"foo", "x.foo"}, {"bar", "x.bar"}})
	c.Assert(err, IsNil)
	c.Check(aliases, DeepEquals, []*backend.Alias{{"foo", "x.foo"}})
}

func (s *aliasesSuite) TestUpdateAliasesAdd(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	match, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, len(aliases))
}

func (s *aliasesSuite) TestUpdateAliasesAddIdempot(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	match, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, len(aliases))
}

func (s *aliasesSuite) TestUpdateAliasesRemove(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	match, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, len(aliases))

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	missing, err := missingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(missing, HasLen, len(aliases))

	match, err = matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, 0)
}

func (s *aliasesSuite) TestUpdateAliasesRemoveIdempot(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	missing, err := missingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(missing, HasLen, len(aliases))

	match, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, 0)
}

func (s *aliasesSuite) TestUpdateAliasesAddRemoveOverlap(c *C) {
	before := []*backend.Alias{{"bar", "x.bar"}}
	after := []*backend.Alias{{"bar", "x.baz"}}

	err := s.be.UpdateAliases(before, nil)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(after, before)
	c.Assert(err, IsNil)

	match, err := matchingAliases(before)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, 0)
	match, err = matchingAliases(after)
	c.Assert(err, IsNil)
	c.Check(match, HasLen, len(after))
}

func (s *aliasesSuite) TestRemoveSnapAliases(c *C) {
	aliases := []*backend.Alias{{"x", "x"}, {"bar", "x.bar"}, {"baz", "y.baz"}, {"y", "y"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.RemoveSnapAliases("x")
	c.Assert(err, IsNil)

	match, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(match, DeepEquals, []*backend.Alias{{"baz", "y.baz"}, {"y", "y"}})
}
