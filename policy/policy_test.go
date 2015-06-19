// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package policy

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"sort"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type policySuite struct {
	orig string
	dest string
	appg string

	secbase string
}

var _ = Suite(&policySuite{})

func (s *policySuite) SetUpTest(c *C) {
	s.orig = c.MkDir()
	s.dest = c.MkDir()
	s.appg = filepath.Join(s.orig, "meta", "framework-policy", "apparmor", "policygroups")
	for _, i := range []string{"apparmor", "seccomp"} {
		for _, j := range []string{"policygroups", "templates"} {
			base := filepath.Join(s.orig, "meta", "framework-policy", i, j)
			c.Assert(os.MkdirAll(base, 0755), IsNil)
			for k := 0; k < 3; k++ {
				name := filepath.Join(base, fmt.Sprintf("%s%d", j, k))
				content := fmt.Sprintf("%s::%s%d", i, j, k)
				c.Assert(ioutil.WriteFile(name, []byte(content), 0644), IsNil)
			}
		}
	}
	s.secbase = SecBase
}

func (s *policySuite) TearDownTest(c *C) {
	SecBase = s.secbase
}

func (s *policySuite) TestIterOpInstallRemove(c *C) {
	err := iterOp(install, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, IsNil)
	g, err := filepath.Glob(filepath.Join(s.dest, "*"))
	c.Check(err, IsNil)
	c.Assert(g, HasLen, 3)
	// Glob already sorts the returned list, but that's not documented
	sort.Strings(g)
	c.Check(filepath.Base(g[0]), Equals, "foo_policygroups0")
	c.Check(filepath.Base(g[1]), Equals, "foo_policygroups1")
	c.Check(filepath.Base(g[2]), Equals, "foo_policygroups2")
	// check the contents of one of them
	bs, err := ioutil.ReadFile(g[0])
	c.Check(err, IsNil)
	c.Check(string(bs), Equals, "apparmor::policygroups0")
	// now, remove it
	err = iterOp(remove, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, IsNil)
	g, err = filepath.Glob(filepath.Join(s.dest, "*"))
	c.Check(err, IsNil)
	c.Check(g, HasLen, 0)
}

func (s *policySuite) TestIterOpInstallMkdir(c *C) {
	dest := filepath.Join(s.dest, "bar")
	_, err := os.Stat(dest)
	c.Assert(os.IsNotExist(err), Equals, true)
	err = iterOp(install, filepath.Join(s.appg, "*"), dest, "foo_")
	c.Check(err, IsNil)
	bs, err := ioutil.ReadFile(filepath.Join(dest, "foo_policygroups0"))
	c.Check(err, IsNil)
	c.Check(string(bs), Equals, "apparmor::policygroups0")
}

func (s *policySuite) TestIterOpBadTargetdir(c *C) {
	err := iterOp(42, "/*", "/root/if-you-see-this-directory-something-is-horribly-wrong", "__")
	c.Check(err, ErrorMatches, `.*unable.*make.*directory.*`)
}

func (s *policySuite) TestIterOpBadFile(c *C) {
	fn := filepath.Join(s.appg, "badbad")
	c.Assert(os.Symlink(fn, fn), IsNil)
	err := iterOp(42, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, ErrorMatches, ".*not a regular file.*")
}

func (s *policySuite) TestIterOpBadOp(c *C) {
	err := iterOp(42, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, ErrorMatches, ".*unknown operation.*")
}

func (s *policySuite) TestIterOpInstallBadFilemode(c *C) {
	fn := filepath.Join(s.appg, "policygroups0")
	c.Assert(os.Chmod(fn, 0), IsNil)
	err := iterOp(install, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, ErrorMatches, ".*unable to open.*")
}

func (s *policySuite) TestIterOpInstallBadTarget(c *C) {
	c.Assert(os.Chmod(s.dest, 0), IsNil)
	defer os.Chmod(s.dest, 0755)
	err := iterOp(install, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, ErrorMatches, ".*unable to create.*")
}

func (s *policySuite) TestIterOpRemoveBadDirmode(c *C) {
	err := iterOp(install, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Assert(err, IsNil)
	c.Assert(os.Chmod(s.dest, 0), IsNil)
	defer os.Chmod(s.dest, 0755)
	err = iterOp(remove, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Assert(err, ErrorMatches, ".*unable to remove.*")
}

func (s *policySuite) TestFrameworkRoundtrip(c *C) {
	rootDir := c.MkDir()
	SecBase = s.dest
	c.Check(Install("foo", s.orig, rootDir), IsNil)
	// check the files were copied, with the packagename prepended properly
	g, err := filepath.Glob(filepath.Join(rootDir, SecBase, "*", "*", "foo_*"))
	c.Check(err, IsNil)
	c.Check(g, HasLen, 4*3)
	c.Check(Remove("foo", s.orig, rootDir), IsNil)
	g, err = filepath.Glob(filepath.Join(SecBase, "*", "*", "*"))
	c.Check(err, IsNil)
	c.Check(g, HasLen, 0)
}

func (s *policySuite) TestFrameworkError(c *C) {
	// check we get errors from the iterOp, is all
	SecBase = s.dest
	c.Check(frameworkOp(42, "foo", s.orig, ""), ErrorMatches, ".*unknown operation.*")
}

func (s *policySuite) TestOpString(c *C) {
	c.Check(fmt.Sprintf("%s", install), Equals, "Install")
	c.Check(fmt.Sprintf("%s", remove), Equals, "Remove")
}

func (s *policySuite) TestDelta(c *C) {
	base := filepath.Join(s.dest, "meta", "framework-policy", "apparmor", "policygroups")
	c.Assert(os.MkdirAll(base, 0755), IsNil)
	for k := 0; k < 3; k++ {
		name := filepath.Join(base, fmt.Sprintf("policygroups%d", k))
		content := fmt.Sprintf("apparmor::policygroups%d 2", k)
		c.Assert(ioutil.WriteFile(name, []byte(content), 0644), IsNil)
	}

	ps, ts := AppArmorDelta(s.orig, s.dest, "x-")
	// policies are the same files, all different
	c.Check(ps, DeepEquals, map[string]bool{
		"x-policygroups0": true,
		"x-policygroups1": true,
		"x-policygroups2": true,
	})
	// templates are all different files => no updates
	c.Check(ts, HasLen, 0)
}
