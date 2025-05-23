// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package syscheck_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/dirs/dirstest"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/syscheck"
	"github.com/snapcore/snapd/testutil"
)

type dirsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&dirsSuite{})

func (s *dirsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.AddCleanup(release.MockReleaseInfo(&release.OS{
		ID: "funkylunix",
	}))
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *dirsSuite) TestHappy(c *C) {
	dir := c.MkDir()

	dirstest.MustMockCanonicalSnapMountDir(dir)
	dirs.SetRootDir(dir)
	c.Check(dirs.SnapMountDirDetectionOutcome(), IsNil)

	c.Check(syscheck.CheckSnapMountDir(), IsNil)
}

func (s *dirsSuite) TestUndetermined(c *C) {
	d := c.MkDir()
	// pretend we have a relative symlink
	c.Assert(os.Symlink("foo/bar", filepath.Join(d, "/snap")), IsNil)
	dirs.SetRootDir(d)
	c.Logf("dir: %v", dirs.SnapMountDir)

	c.Check(dirs.SnapMountDirDetectionOutcome(), NotNil)

	c.Check(syscheck.CheckSnapMountDir(), ErrorMatches, "cannot resolve snap mount directory: .*")
}

var (
	known = []struct {
		ID           string
		IDLike       []string
		canonicalDir bool
	}{
		{"fedora", nil, false},
		{"rhel", []string{"fedora"}, false},
		{"centos", []string{"fedora"}, false},
		{"ubuntu", []string{"debian"}, true},
		{"debian", nil, true},
		{"suse", nil, true},
		{"yocto", nil, true},
		{"arch", []string{"archlinux"}, false},
		{"archlinux", nil, false},
		{"altlinux", nil, false},
	}

	knownSpecial = []struct {
		ID  string
		dir string
	}{
		{"ubuntucoreinitramfs", dirs.DefaultSnapMountDir},
	}
)

func (s *dirsSuite) TestMountDirKnownDistroHappy(c *C) {
	defer dirs.SetRootDir("")

	for _, tc := range known {
		c.Logf("happy case %+v", tc)
		func() {
			defer release.MockReleaseInfo(&release.OS{ID: tc.ID, IDLike: tc.IDLike})()
			d := c.MkDir()
			if tc.canonicalDir {
				dirstest.MustMockCanonicalSnapMountDir(d)
			} else {
				dirstest.MustMockAltSnapMountDir(d)
			}
			dirs.SetRootDir(d)

			c.Check(syscheck.CheckSnapMountDir(), IsNil)
		}()
	}
}

func (s *dirsSuite) TestMountDirKnownDistroMismatch(c *C) {
	defer dirs.SetRootDir("")

	for _, tc := range known {
		c.Logf("mismatch case %+v", tc)
		func() {
			defer release.MockReleaseInfo(&release.OS{ID: tc.ID, IDLike: tc.IDLike})()
			d := c.MkDir()
			// do the complete opposite so that the mount directory does not
			// have the expected value based on what we know about distributions
			// packaging snapd
			if tc.canonicalDir {
				dirstest.MustMockAltSnapMountDir(d)
			} else {
				dirstest.MustMockCanonicalSnapMountDir(d)
			}
			dirs.SetRootDir(d)

			c.Check(syscheck.CheckSnapMountDir(), ErrorMatches,
				fmt.Sprintf("unexpected snap mount directory /.*snap on %s", tc.ID))
		}()
	}
}

func (s *dirsSuite) TestMountDirKnownDistroSpecial(c *C) {
	defer dirs.SetRootDir("")

	for _, tc := range knownSpecial {
		c.Logf("distro special case %+v", tc)
		func() {
			defer release.MockReleaseInfo(&release.OS{ID: tc.ID})()
			d := c.MkDir()
			dirs.SetRootDir(d)

			c.Check(syscheck.CheckSnapMountDir(), IsNil)
		}()
	}
}

func (s *dirsSuite) TestLibExecDirDefault(c *C) {
	defer dirs.SetRootDir("")

	for _, tc := range syscheck.DefaultLibExecDirDistros {
		c.Logf("distro libexecdir default case %+v", tc)
		func() {
			defer release.MockReleaseInfo(&release.OS{ID: tc})()
			d := c.MkDir()
			dirstest.MustMockDefaultLibExecDir(d)
			dirs.SetRootDir(d)

			c.Check(syscheck.CheckLibExecDir(), IsNil)
		}()
	}
}

func (s *dirsSuite) TestLibExecDirAlt(c *C) {
	defer dirs.SetRootDir("")

	for _, tc := range syscheck.AltLibExecDirDistros {
		c.Logf("distro libexecdir alt case %+v", tc)
		func() {
			defer release.MockReleaseInfo(&release.OS{ID: tc})()
			d := c.MkDir()
			dirstest.MustMockAltLibExecDir(d)
			dirs.SetRootDir(d)

			c.Check(syscheck.CheckLibExecDir(), IsNil)
		}()
	}
}

func (s *dirsSuite) TestLibExecDirUnexpected(c *C) {
	defer dirs.SetRootDir("")

	for _, tc := range append(syscheck.DefaultLibExecDirDistros, syscheck.AltLibExecDirDistros...) {
		c.Logf("distro mismatch case %+v", tc)
		func() {
			defer release.MockReleaseInfo(&release.OS{ID: tc})()
			d := c.MkDir()
			switch tc {
			case "fedora", "opensuse-tumbleweed", "opensuse-slowroll":
				dirstest.MustMockDefaultLibExecDir(d)
			default:
				dirstest.MustMockAltLibExecDir(d)
			}
			dirs.SetRootDir(d)

			c.Check(syscheck.CheckLibExecDir(), ErrorMatches, "unexpected snapd tooling directory /usr/lib(exec)?/snapd on "+tc)
		}()
	}
}

func (s *dirsSuite) TestLibExecDirUnknownDistro(c *C) {
	defer dirs.SetRootDir("")

	func() {
		defer release.MockReleaseInfo(&release.OS{ID: "not-fedora"})()
		d := c.MkDir()
		dirstest.MustMockDefaultLibExecDir(d)
		dirs.SetRootDir(d)

		c.Check(syscheck.CheckLibExecDir(), IsNil)
	}()

	func() {
		defer release.MockReleaseInfo(&release.OS{ID: "not-ubuntu"})()
		d := c.MkDir()
		dirstest.MustMockAltLibExecDir(d)
		dirs.SetRootDir(d)

		c.Check(syscheck.CheckLibExecDir(), IsNil)
	}()
}
