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

package release_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/release"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ReleaseTestSuite struct {
}

var _ = Suite(&ReleaseTestSuite{})

func (s *ReleaseTestSuite) TestSetup(c *C) {
	c.Check(release.Series, Equals, "16")
}

func mockOSRelease(c *C) string {
	// FIXME: use AddCleanup here once available so that we
	//        can do release.SetLSBReleasePath() here directly
	mockOSRelease := filepath.Join(c.MkDir(), "mock-os-release")
	s := `
NAME="Ubuntu"
VERSION="18.09 (Awesome Artichoke)"
ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="I'm not real!"
VERSION_ID="18.09"
HOME_URL="http://www.ubuntu.com/"
SUPPORT_URL="http://help.ubuntu.com/"
BUG_REPORT_URL="http://bugs.launchpad.net/ubuntu/"
`
	err := ioutil.WriteFile(mockOSRelease, []byte(s), 0644)
	c.Assert(err, IsNil)

	return mockOSRelease
}

func (s *ReleaseTestSuite) TestReadOSRelease(c *C) {
	reset := release.MockOSReleasePath(mockOSRelease(c))
	defer reset()

	os := release.ReadOSRelease()
	c.Check(os.ID, Equals, "ubuntu")
	c.Check(os.VersionID, Equals, "18.09")
}

func (s *ReleaseTestSuite) TestReadWonkyOSRelease(c *C) {
	mockOSRelease := filepath.Join(c.MkDir(), "mock-os-release")
	dump := `NAME="elementary OS"
VERSION="0.4 Loki"
ID="elementary OS"
ID_LIKE=ubuntu
PRETTY_NAME="elementary OS Loki"
VERSION_ID="0.4"
HOME_URL="http://elementary.io/"
SUPPORT_URL="http://elementary.io/support/"
BUG_REPORT_URL="https://bugs.launchpad.net/elementary/+filebug"`
	err := ioutil.WriteFile(mockOSRelease, []byte(dump), 0644)
	c.Assert(err, IsNil)

	reset := release.MockOSReleasePath(mockOSRelease)
	defer reset()

	os := release.ReadOSRelease()
	c.Check(os.ID, Equals, "elementary")
	c.Check(os.VersionID, Equals, "0.4")
}

func (s *ReleaseTestSuite) TestReadOSReleaseNotFound(c *C) {
	reset := release.MockOSReleasePath("not-there")
	defer reset()

	os := release.ReadOSRelease()
	c.Assert(os, DeepEquals, release.OS{ID: "linux", VersionID: "unknown"})
}

func (s *ReleaseTestSuite) TestOnClassic(c *C) {
	reset := release.MockOnClassic(true)
	defer reset()
	c.Assert(release.OnClassic, Equals, true)

	reset = release.MockOnClassic(false)
	defer reset()
	c.Assert(release.OnClassic, Equals, false)
}

func (s *ReleaseTestSuite) TestReleaseInfo(c *C) {
	reset := release.MockReleaseInfo(&release.OS{
		ID: "distro-id",
	})
	defer reset()
	c.Assert(release.ReleaseInfo.ID, Equals, "distro-id")
}

func (s *ReleaseTestSuite) TestForceDevMode(c *C) {
	// Restore real OS info at the end of this function.
	defer release.MockReleaseInfo(&release.OS{})()
	distros := []struct {
		id        string
		idVersion string
		devmode   bool
	}{
		// Please keep this list sorted
		{id: "arch", devmode: true},
		{id: "debian", devmode: true},
		{id: "elementary", devmode: true},
		{id: "elementary", idVersion: "0.4", devmode: false},
		{id: "fedora", devmode: true},
		{id: "gentoo", devmode: true},
		{id: "neon", devmode: false},
		{id: "opensuse", devmode: true},
		{id: "rhel", devmode: true},
		{id: "ubuntu", devmode: false},
		{id: "ubuntu-core", devmode: false},
		{id: "linuxmint", devmode: true},
		{id: "linuxmint", idVersion: "18.1", devmode: false},
	}
	for _, distro := range distros {
		rel := &release.OS{ID: distro.id, VersionID: distro.idVersion}
		c.Logf("checking distribution %#v", rel)
		release.MockReleaseInfo(rel)
		c.Assert(release.ReleaseInfo.ForceDevMode(), Equals, distro.devmode)
	}
}
