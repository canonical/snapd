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
	"io/ioutil"
	"os"
	"path/filepath"
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
	// Ensure that we have a distribution as base which supports classic confinement
	reset := release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer reset()
	dirs.SetRootDir("/")
	c.Check(dirs.SupportsClassicConfinement(), Equals, true)

	dirs.SnapMountDir = "/alt"
	defer dirs.SetRootDir("/")
	c.Check(dirs.SupportsClassicConfinement(), Equals, false)
}

func (s *DirsTestSuite) TestClassicConfinementSymlinkWorkaround(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "fedora"})
	defer restore()

	altRoot := c.MkDir()
	dirs.SetRootDir(altRoot)
	defer dirs.SetRootDir("/")
	c.Check(dirs.SupportsClassicConfinement(), Equals, false)
	d := filepath.Join(altRoot, "/var/lib/snapd/snap")
	os.MkdirAll(d, 0755)
	os.Symlink(d, filepath.Join(altRoot, "snap"))
	c.Check(dirs.SupportsClassicConfinement(), Equals, true)
}

func (s *DirsTestSuite) TestClassicConfinementSupportOnSpecificDistributions(c *C) {
	// the test changes RootDir, restore correct one when retuning
	defer dirs.SetRootDir("/")

	for _, t := range []struct {
		ID       string
		IDLike   []string
		Expected bool
	}{
		{"fedora", nil, false},
		{"rhel", []string{"fedora"}, false},
		{"centos", []string{"fedora"}, false},
		{"ubuntu", []string{"debian"}, true},
		{"debian", nil, true},
		{"suse", nil, true},
		{"yocto", nil, true},
	} {
		reset := release.MockReleaseInfo(&release.OS{ID: t.ID, IDLike: t.IDLike})
		defer reset()

		// make a new root directory each time to isolate the test from
		// local filesystem state and any previous test runs
		dirs.SetRootDir(c.MkDir())
		c.Check(dirs.SupportsClassicConfinement(), Equals, t.Expected, Commentf("unexpected result for %v", t.ID))
	}
}

func (s *DirsTestSuite) TestInsideBaseSnap(c *C) {
	d := c.MkDir()

	snapYaml := filepath.Join(d, "snap.yaml")
	restore := dirs.MockMetaSnapPath(snapYaml)
	defer restore()

	inside, err := dirs.IsInsideBaseSnap()
	c.Assert(err, IsNil)
	c.Assert(inside, Equals, false)

	err = ioutil.WriteFile(snapYaml, []byte{}, 0755)
	c.Assert(err, IsNil)

	inside, err = dirs.IsInsideBaseSnap()
	c.Assert(err, IsNil)
	c.Assert(inside, Equals, true)
}
