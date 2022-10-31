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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
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

// Kernel magic numbers
const (
	wslfs int64 = 0x53464846
	ext4  int64 = 0xef53
)

func mockWSLsetup(c *C, existsWSLinterop bool, existsRunWSL bool, filesystemID int64) func() {
	restoreFileExists := release.MockFileExists(func(s string) bool {
		if s == "/proc/sys/fs/binfmt_misc/WSLInterop" {
			return existsWSLinterop
		}
		if s == "/run/WSL" {
			return existsRunWSL
		}
		return osutil.FileExists(s)
	})
	restoreFilesystemRootType := release.MockFilesystemRootType(filesystemID)

	return func() {
		restoreFileExists()
		restoreFilesystemRootType()
	}
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

func (s *ReleaseTestSuite) TestFilesystemRootType(c *C) {
	reported_type, err := release.FilesystemRootType()
	c.Assert(err, IsNil)

	// From man stat:
	// %t   major device type in hex, for character/block device special files
	output, err := exec.Command("stat", "-f", "-c", "%t", "/").CombinedOutput()
	c.Assert(err, IsNil)

	outstr := strings.TrimSpace(string(output[:]))
	statted_type, err := strconv.ParseInt(outstr, 16, 64)
	c.Assert(err, IsNil)

	c.Check(reported_type, Equals, statted_type)
}

func (s *ReleaseTestSuite) TestNonWSL(c *C) {
	defer mockWSLsetup(c, false, false, ext4)()
	v := release.GetWSLVersion()
	c.Check(v, Equals, 0)
}

func (s *ReleaseTestSuite) TestWSL1(c *C) {
	defer mockWSLsetup(c, true, true, wslfs)()
	v := release.GetWSLVersion()
	c.Check(v, Equals, 1)
}

func (s *ReleaseTestSuite) TestWSL2(c *C) {
	defer mockWSLsetup(c, true, true, ext4)()
	v := release.GetWSLVersion()
	c.Check(v, Equals, 2)
}

func (s *ReleaseTestSuite) TestWSL2NoInterop(c *C) {
	defer mockWSLsetup(c, false, true, ext4)()
	v := release.GetWSLVersion()
	c.Check(v, Equals, 2)
}

func (s *ReleaseTestSuite) TestSystemctlSupportsUserUnits(c *C) {
	for _, t := range []struct {
		id, versionID string
		supported     bool
	}{
		// Non-Ubuntu releases are assumed to be new enough
		{"distro-id", "version", true},
		// Ubuntu 14.04's systemd is too old for user units
		{"ubuntu", "14.04", false},
		// Other Ubuntu releases are fine
		{"ubuntu", "16.04", true},
		{"ubuntu", "18.04", true},
		{"ubuntu", "20.04", true},
	} {
		reset := release.MockReleaseInfo(&release.OS{
			ID:        t.id,
			VersionID: t.versionID,
		})
		defer reset()

		c.Check(release.SystemctlSupportsUserUnits(), Equals, t.supported)
	}
}
