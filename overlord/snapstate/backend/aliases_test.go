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
	"io/ioutil"
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
	c.Assert(os.MkdirAll(dirs.CompletersDir, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteSh), 0755), IsNil)
	c.Assert(ioutil.WriteFile(dirs.CompleteSh, nil, 0644), IsNil)
}

func (s *aliasesSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func missingAliases(aliases []*backend.Alias) (missingAs, missingCs []*backend.Alias, err error) {
	for _, cand := range aliases {
		_, err = os.Lstat(filepath.Join(dirs.SnapBinariesDir, cand.Name))
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, nil, err
			}
			missingAs = append(missingAs, cand)
		}

		_, err = os.Lstat(filepath.Join(dirs.CompletersDir, cand.Name))
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, nil, err
			}
			missingCs = append(missingCs, cand)
		}
	}
	return missingAs, missingCs, nil
}

func (s *aliasesSuite) TestMissingAliases(c *C) {
	err := os.Symlink("x.foo", filepath.Join(dirs.SnapBinariesDir, "foo"))
	c.Assert(err, IsNil)
	c.Assert(os.Symlink("x.a", filepath.Join(dirs.CompletersDir, "a")), IsNil)

	missingAs, missingCs, err := missingAliases([]*backend.Alias{{"a", "x.a"}, {"foo", "x.foo"}})
	c.Assert(err, IsNil)
	c.Check(missingAs, DeepEquals, []*backend.Alias{{"a", "x.a"}})
	c.Check(missingCs, DeepEquals, []*backend.Alias{{"foo", "x.foo"}})
}

func matchingAliases(aliases []*backend.Alias) (matchingAs, matchingCs []*backend.Alias, err error) {
	for _, cand := range aliases {
		target, err := os.Readlink(filepath.Join(dirs.SnapBinariesDir, cand.Name))
		if err == nil {
			if target == cand.Target {
				matchingAs = append(matchingAs, cand)
			}
		} else if !os.IsNotExist(err) {
			return nil, nil, err
		}

		target, err = os.Readlink(filepath.Join(dirs.CompletersDir, cand.Name))
		if err == nil {
			if target == cand.Target {
				matchingCs = append(matchingCs, cand)
			}
		} else if !os.IsNotExist(err) {
			return nil, nil, err
		}
	}
	return matchingAs, matchingCs, nil
}

func (s *aliasesSuite) TestMatchingAliases(c *C) {
	err := os.Symlink("x.foo", filepath.Join(dirs.SnapBinariesDir, "foo"))
	c.Assert(err, IsNil)
	err = os.Symlink("y.bar", filepath.Join(dirs.SnapBinariesDir, "bar"))
	c.Assert(err, IsNil)
	c.Assert(os.Symlink("y.foo", filepath.Join(dirs.CompletersDir, "foo")), IsNil)
	c.Assert(os.Symlink("x.bar", filepath.Join(dirs.CompletersDir, "bar")), IsNil)

	matchingAs, matchingCs, err := matchingAliases([]*backend.Alias{{"a", "x.a"}, {"foo", "x.foo"}, {"bar", "x.bar"}})
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, []*backend.Alias{{"foo", "x.foo"}})
	c.Check(matchingCs, DeepEquals, []*backend.Alias{{"bar", "x.bar"}})
}

func (s *aliasesSuite) TestUpdateAliasesAdd(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, aliases)
	c.Check(matchingCs, HasLen, 0)
}

func mkCompleters(c *C, apps ...string) {
	for _, app := range apps {
		c.Assert(os.Symlink(dirs.CompleteSh, filepath.Join(dirs.CompletersDir, app)), IsNil)
	}
}

func (s *aliasesSuite) TestUpdateAliasesAddWithCompleter(c *C) {
	mkCompleters(c, "x.bar", "x.foo")
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, aliases)
	c.Check(matchingCs, DeepEquals, aliases)
}

func (s *aliasesSuite) TestUpdateAliasesAddIdempot(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, aliases)
	c.Check(matchingCs, HasLen, 0)
}

func (s *aliasesSuite) TestUpdateAliasesAddWithCompleterIdempot(c *C) {
	mkCompleters(c, "x.foo", "x.bar")
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, aliases)
	c.Check(matchingCs, DeepEquals, aliases)
}

func (s *aliasesSuite) TestUpdateAliasesRemove(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, aliases)
	c.Check(matchingCs, HasLen, 0)

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	missingAs, missingCs, err := missingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(missingAs, DeepEquals, aliases)
	c.Check(missingCs, DeepEquals, aliases)

	matchingAs, matchingCs, err = matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, HasLen, 0)
	c.Check(matchingCs, HasLen, 0)
}

func (s *aliasesSuite) TestUpdateAliasesWithCompleterRemove(c *C) {
	mkCompleters(c, "x.foo", "x.bar")
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, HasLen, len(aliases))
	c.Check(matchingCs, HasLen, len(aliases))

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	missingAs, missingCs, err := missingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(missingAs, DeepEquals, aliases)
	c.Check(missingCs, DeepEquals, aliases)

	matchingAs, matchingCs, err = matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, HasLen, 0)
	c.Check(matchingCs, HasLen, 0)
}

func (s *aliasesSuite) TestUpdateAliasesRemoveIdempot(c *C) {
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	missingAs, missingCs, err := missingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(missingAs, DeepEquals, aliases)
	c.Check(missingCs, DeepEquals, aliases)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, HasLen, 0)
	c.Check(matchingCs, HasLen, 0)
}

func (s *aliasesSuite) TestUpdateAliasesWithCompleterRemoveIdempot(c *C) {
	mkCompleters(c, "x.foo", "x.bar")
	aliases := []*backend.Alias{{"foo", "x.foo"}, {"bar", "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(nil, aliases)
	c.Assert(err, IsNil)

	missingAs, missingCs, err := missingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(missingAs, DeepEquals, aliases)
	c.Check(missingCs, DeepEquals, aliases)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, HasLen, 0)
	c.Check(matchingCs, HasLen, 0)
}

func (s *aliasesSuite) TestUpdateAliasesAddRemoveOverlap(c *C) {
	before := []*backend.Alias{{"bar", "x.bar"}}
	after := []*backend.Alias{{"bar", "x.baz"}}

	err := s.be.UpdateAliases(before, nil)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(after, before)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(before)
	c.Assert(err, IsNil)
	c.Check(matchingAs, HasLen, 0)
	c.Check(matchingCs, HasLen, 0)
	matchingAs, matchingCs, err = matchingAliases(after)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, after)
	c.Check(matchingCs, HasLen, 0)
}

func (s *aliasesSuite) TestUpdateAliasesWithCompleterAddRemoveOverlap(c *C) {
	mkCompleters(c, "x.baz", "x.bar")
	before := []*backend.Alias{{"bar", "x.bar"}}
	after := []*backend.Alias{{"bar", "x.baz"}}

	err := s.be.UpdateAliases(before, nil)
	c.Assert(err, IsNil)

	err = s.be.UpdateAliases(after, before)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(before)
	c.Assert(err, IsNil)
	c.Check(matchingAs, HasLen, 0)
	c.Check(matchingCs, HasLen, 0)
	matchingAs, matchingCs, err = matchingAliases(after)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, after)
	c.Check(matchingCs, DeepEquals, after)
}

func (s *aliasesSuite) TestRemoveSnapAliases(c *C) {
	aliases := []*backend.Alias{{"bar", "x.bar"}, {"baz", "y.baz"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.RemoveSnapAliases("x")
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, []*backend.Alias{{"baz", "y.baz"}})
	// no completion for the commands -> no completion for the aliases
	c.Check(matchingCs, HasLen, 0)
}

func (s *aliasesSuite) TestRemoveSnapAliasesWithCompleter(c *C) {
	mkCompleters(c, "x", "x.bar", "y", "y.baz")
	aliases := []*backend.Alias{{"xx", "x"}, {"bar", "x.bar"}, {"baz", "y.baz"}, {"yy", "y"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.RemoveSnapAliases("x")
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, []*backend.Alias{{"baz", "y.baz"}, {"yy", "y"}})
	c.Check(matchingCs, DeepEquals, []*backend.Alias{{"baz", "y.baz"}, {"yy", "y"}})
}
