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
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
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

func (s *ReleaseTestSuite) TestFamilyOSRelease(c *C) {
	mockOSRelease := filepath.Join(c.MkDir(), "mock-os-release")
	dump := `NAME="CentOS Linux"
VERSION="7 (Core)"
ID="centos"
ID_LIKE="rhel fedora"
VERSION_ID="7"
PRETTY_NAME="CentOS Linux 7 (Core)"
ANSI_COLOR="0;31"
CPE_NAME="cpe:/o:centos:centos:7"
HOME_URL="https://www.centos.org/"
BUG_REPORT_URL="https://bugs.centos.org/"

CENTOS_MANTISBT_PROJECT="CentOS-7"
CENTOS_MANTISBT_PROJECT_VERSION="7"
REDHAT_SUPPORT_PRODUCT="centos"
REDHAT_SUPPORT_PRODUCT_VERSION="7"`
	err := ioutil.WriteFile(mockOSRelease, []byte(dump), 0644)
	c.Assert(err, IsNil)

	reset := release.MockOSReleasePath(mockOSRelease)
	defer reset()

	os := release.ReadOSRelease()
	c.Check(os.ID, Equals, "centos")
	c.Check(os.VersionID, Equals, "7")
	c.Check(os.IDLike, DeepEquals, []string{"rhel", "fedora"})
}

func (s *ReleaseTestSuite) TestReadOSReleaseNotFound(c *C) {
	reset := release.MockOSReleasePath("not-there")
	defer reset()

	os := release.ReadOSRelease()
	c.Assert(os, DeepEquals, release.OS{ID: "linux"})
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

	runTest := func(apparmorLevel apparmor.LevelType, cgroupVersion int, expect bool) {
		restore := apparmor.MockLevel(apparmorLevel)
		defer restore()
		restore = cgroup.MockVersion(cgroupVersion, nil)
		defer restore()
		devMode := release.ReleaseInfo.ForceDevMode()
		c.Check(devMode, Equals, expect, Commentf("unexpected force-dev-mode for AppArmor level %v cgroup v%v", apparmorLevel, cgroupVersion))
	}

	for _, tc := range []struct {
		apparmorLevel apparmor.LevelType
		cgroupVersion int
		exp           bool
	}{
		{apparmor.Full, cgroup.V1, false},
		{apparmor.Partial, cgroup.V1, true},
		// unified mode
		{apparmor.Full, cgroup.V2, true},
		{apparmor.Partial, cgroup.V2, true},
	} {
		runTest(tc.apparmorLevel, tc.cgroupVersion, tc.exp)
	}
}

func (s *ReleaseTestSuite) TestMockForceDevMode(c *C) {
	for _, devmode := range []bool{true, false} {
		restore := release.MockForcedDevmode(devmode)
		defer restore()
		c.Assert(release.ReleaseInfo.ForceDevMode(), Equals, devmode, Commentf("wrong result for %#v", devmode))
	}
}

func (s *ReleaseTestSuite) TestNonWSL(c *C) {
	defer release.MockIoutilReadfile(func(s string) ([]byte, error) {
		c.Check(s, Equals, "/proc/version")
		return []byte("Linux version 2.2.19 (herbert@gondolin) (gcc version 2.7.2.3) #1 Wed Mar 20 19:41:41 EST 2002"), nil
	})()

	c.Check(release.IsWSL(), Equals, false)
}

func (s *ReleaseTestSuite) TestWSL(c *C) {
	defer release.MockIoutilReadfile(func(s string) ([]byte, error) {
		c.Check(s, Equals, "/proc/version")
		return []byte("Linux version 3.4.0-Microsoft (Microsoft@Microsoft.com) (gcc version 4.7 (GCC) ) #1 SMP PREEMPT Wed Dec 31 14:42:53 PST 2014"), nil
	})()

	c.Check(release.IsWSL(), Equals, true)
}
