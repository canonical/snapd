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

	. "launchpad.net/gocheck"
	"sort"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type policySuite struct {
	orig string
	dest string
	appg string
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
				name := fmt.Sprintf(filepath.Join(base, "%s%d"), j, k)
				content := fmt.Sprintf("%s/%s%d", i, j, k)
				c.Assert(ioutil.WriteFile(name, []byte(content), 0644), IsNil)
			}
		}
	}
}

func (s *policySuite) TestHelperInstallRemove(c *C) {
	err := helper(install, filepath.Join(s.appg, "*"), s.dest, "foo_")
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
	c.Check(string(bs), Equals, "apparmor/policygroups0")
	// now, remove it
	err = helper(remove, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, IsNil)
	g, err = filepath.Glob(filepath.Join(s.dest, "*"))
	c.Check(err, IsNil)
	c.Check(g, HasLen, 0)
}

func (s *policySuite) TestHelperInstallMkdir(c *C) {
	dest := filepath.Join(s.dest, "bar")
	_, err := os.Stat(dest)
	c.Assert(os.IsNotExist(err), Equals, true)
	err = helper(install, filepath.Join(s.appg, "*"), dest, "foo_")
	c.Check(err, IsNil)
	bs, err := ioutil.ReadFile(filepath.Join(dest, "foo_policygroups0"))
	c.Check(err, IsNil)
	c.Check(string(bs), Equals, "apparmor/policygroups0")
}

func (s *policySuite) TestHelperBadTargetdir(c *C) {
	err := helper(42, "/*", "/root/if-you-see-this-directory-something-is-horribly-wrong", "__")
	c.Check(err, ErrorMatches, `.*unable.*make.*directory.*`)
}

func (s *policySuite) TestHelperBadFile(c *C) {
	fn := filepath.Join(s.appg, "badbad")
	c.Assert(os.Symlink(fn, fn), IsNil)
	err := helper(42, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, ErrorMatches, ".*not a regular file.*")
}

func (s *policySuite) TestHelperBadOp(c *C) {
	err := helper(42, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, ErrorMatches, ".*unknown operation.*")
}

func (s *policySuite) TestHelperInstallBadFilemode(c *C) {
	fn := filepath.Join(s.appg, "policygroups0")
	c.Assert(os.Chmod(fn, 0), IsNil)
	err := helper(install, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, ErrorMatches, ".*unable to read.*")
}

func (s *policySuite) TestHelperInstallBadTarget(c *C) {
	c.Assert(os.Chmod(s.dest, 0), IsNil)
	defer os.Chmod(s.dest, 0755)
	err := helper(install, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Check(err, ErrorMatches, ".*unable to create.*")
}

func (s *policySuite) TestHelperRemoveBadDirmode(c *C) {
	err := helper(install, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Assert(err, IsNil)
	c.Assert(os.Chmod(s.dest, 0), IsNil)
	defer os.Chmod(s.dest, 0755)
	err = helper(remove, filepath.Join(s.appg, "*"), s.dest, "foo_")
	c.Assert(err, ErrorMatches, ".*unable to remove.*")
}

func (s *policySuite) TestFrameworkRoundtrip(c *C) {
	origSecbase := secbase
	secbase = s.dest
	defer func() { secbase = origSecbase }()
	c.Check(Install("foo", s.orig), IsNil)
	// check the files were copied, with the packagename prepended properly
	g, err := filepath.Glob(filepath.Join(secbase, "*", "*", "foo_*"))
	c.Check(err, IsNil)
	c.Check(g, HasLen, 4*3)
	c.Check(Remove("foo", s.orig), IsNil)
	g, err = filepath.Glob(filepath.Join(secbase, "*", "*", "*"))
	c.Check(err, IsNil)
	c.Check(g, HasLen, 0)
}

func (s *policySuite) TestFrameworkError(c *C) {
	// check we get errors from the helper, is all
	c.Check(frameworkOp(42, "foo", s.orig), ErrorMatches, ".*unknown operation.*")
}

func (s *policySuite) TestOpString(c *C) {
	c.Check(fmt.Sprintf("%s", install), Equals, "Install")
	c.Check(fmt.Sprintf("%s", remove), Equals, "Remove")
}
