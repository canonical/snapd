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

package dirs_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&DirsTestSuite{})

type DirsTestSuite struct{}

func (s *DirsTestSuite) TestStripRootDir(c *C) {
	// strip does nothing if the default (empty) root directory is used
	c.Check(dirs.StripRootDir("/foo/bar"), Equals, "/foo/bar")
	// strip only works on absolute paths
	c.Check(func() { dirs.StripRootDir("relative") }, Panics, `supplied path is not absolute "relative"`)
	// with an alternate root
	dirs.SetRootDir("/alt/")
	defer dirs.SetRootDir("")
	// strip behaves as expected, returning absolute paths without the prefix
	c.Check(dirs.StripRootDir("/alt/foo/bar"), Equals, "/foo/bar")
	// strip only works on paths that begin with the global root directory
	c.Check(func() { dirs.StripRootDir("/other/foo/bar") }, Panics, `supplied path is not related to global root "/other/foo/bar"`)
}

func (s *DirsTestSuite) TestClassicConfinementSupport(c *C) {
	dirs.SetRootDir("/")
	c.Assert(dirs.SupportsClassicConfinement(), Equals, true)
	dirs.SetRootDir("/alt")
	c.Assert(dirs.SupportsClassicConfinement(), Equals, false)
}

func (s *DirsTestSuite) TestClassicConfinementSupportOnSpecificDistributions(c *C) {
	for _, current := range []struct {
		Name     string
		Expected bool
	}{
		{"fedora", false},
		{"rhel", false},
		{"centos", false},
		{"ubuntu", true},
		{"debian", true},
		{"suse", true},
		{"yocto", true}} {
		reset := release.MockReleaseInfo(&release.OS{ID: current.Name})
		defer reset()
		dirs.SetRootDir("/")
		c.Assert(dirs.SupportsClassicConfinement(), Equals, current.Expected)
	}
}
