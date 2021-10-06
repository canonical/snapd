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
	be   backend.Backend
	base string
}

// silly wrappers to get better failure messages
type noBaseAliasesSuite struct{ aliasesSuite }
type withBaseAliasesSuite struct{ aliasesSuite }
type withSnapdAliasesSuite struct{ aliasesSuite }

var _ = Suite(&noBaseAliasesSuite{})
var _ = Suite(&withBaseAliasesSuite{aliasesSuite{base: "core99"}})
var _ = Suite(&withSnapdAliasesSuite{aliasesSuite{base: "core-with-snapd"}})

func (s *aliasesSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	if s.base == "core-with-snapd" {
		c.Check(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current/usr/lib/snapd"), 0755), IsNil)
	}
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.CompletersDir, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(dirs.CompleteShPath(s.base)), 0755), IsNil)
	c.Assert(ioutil.WriteFile(dirs.CompleteShPath(s.base), nil, 0644), IsNil)
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

	missingAs, missingCs, err := missingAliases([]*backend.Alias{{Name: "a", Target: "x.a"}, {Name: "foo", Target: "x.foo"}})
	c.Assert(err, IsNil)
	c.Check(missingAs, DeepEquals, []*backend.Alias{{Name: "a", Target: "x.a"}})
	c.Check(missingCs, DeepEquals, []*backend.Alias{{Name: "foo", Target: "x.foo"}})
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

	matchingAs, matchingCs, err := matchingAliases([]*backend.Alias{{Name: "a", Target: "x.a"}, {Name: "foo", Target: "x.foo"}, {Name: "bar", Target: "x.bar"}})
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, []*backend.Alias{{Name: "foo", Target: "x.foo"}})
	c.Check(matchingCs, DeepEquals, []*backend.Alias{{Name: "bar", Target: "x.bar"}})
}

func (s *aliasesSuite) TestUpdateAliasesAdd(c *C) {
	aliases := []*backend.Alias{{Name: "foo", Target: "x.foo"}, {Name: "bar", Target: "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, aliases)
	c.Check(matchingCs, HasLen, 0)
}

func mkCompleters(c *C, base string, apps ...string) {
	for _, app := range apps {
		c.Assert(os.Symlink(dirs.CompleteShPath(base), filepath.Join(dirs.CompletersDir, app)), IsNil)
	}
}

func (s *aliasesSuite) TestUpdateAliasesAddWithCompleter(c *C) {
	mkCompleters(c, s.base, "x.bar", "x.foo")
	aliases := []*backend.Alias{{Name: "foo", Target: "x.foo"}, {Name: "bar", Target: "x.bar"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, aliases)
	c.Check(matchingCs, DeepEquals, aliases)
}

func (s *aliasesSuite) TestUpdateAliasesAddIdempot(c *C) {
	aliases := []*backend.Alias{{Name: "foo", Target: "x.foo"}, {Name: "bar", Target: "x.bar"}}

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
	mkCompleters(c, s.base, "x.foo", "x.bar")
	aliases := []*backend.Alias{{Name: "foo", Target: "x.foo"}, {Name: "bar", Target: "x.bar"}}

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
	aliases := []*backend.Alias{{Name: "foo", Target: "x.foo"}, {Name: "bar", Target: "x.bar"}}

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
	mkCompleters(c, s.base, "x.foo", "x.bar")
	aliases := []*backend.Alias{{Name: "foo", Target: "x.foo"}, {Name: "bar", Target: "x.bar"}}

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
	aliases := []*backend.Alias{{Name: "foo", Target: "x.foo"}, {Name: "bar", Target: "x.bar"}}

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
	mkCompleters(c, s.base, "x.foo", "x.bar")
	aliases := []*backend.Alias{{Name: "foo", Target: "x.foo"}, {Name: "bar", Target: "x.bar"}}

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
	before := []*backend.Alias{{Name: "bar", Target: "x.bar"}}
	after := []*backend.Alias{{Name: "bar", Target: "x.baz"}}

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
	mkCompleters(c, s.base, "x.baz", "x.bar")
	before := []*backend.Alias{{Name: "bar", Target: "x.bar"}}
	after := []*backend.Alias{{Name: "bar", Target: "x.baz"}}

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
	aliases := []*backend.Alias{{Name: "bar", Target: "x.bar"}, {Name: "baz", Target: "y.baz"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.RemoveSnapAliases("x")
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, []*backend.Alias{{Name: "baz", Target: "y.baz"}})
	// no completion for the commands -> no completion for the aliases
	c.Check(matchingCs, HasLen, 0)
}

func (s *aliasesSuite) TestRemoveSnapAliasesWithCompleter(c *C) {
	mkCompleters(c, s.base, "x", "x.bar", "y", "y.baz")
	aliases := []*backend.Alias{
		{Name: "xx", Target: "x"},
		{Name: "bar", Target: "x.bar"},
		{Name: "baz", Target: "y.baz"},
		{Name: "yy", Target: "y"}}

	err := s.be.UpdateAliases(aliases, nil)
	c.Assert(err, IsNil)

	err = s.be.RemoveSnapAliases("x")
	c.Assert(err, IsNil)

	matchingAs, matchingCs, err := matchingAliases(aliases)
	c.Assert(err, IsNil)
	c.Check(matchingAs, DeepEquals, []*backend.Alias{{Name: "baz", Target: "y.baz"}, {Name: "yy", Target: "y"}})
	c.Check(matchingCs, DeepEquals, []*backend.Alias{{Name: "baz", Target: "y.baz"}, {Name: "yy", Target: "y"}})
}
