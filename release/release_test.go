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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ReleaseTestSuite struct {
}

var _ = Suite(&ReleaseTestSuite{})

func (s *ReleaseTestSuite) TestSetup(c *C) {
	c.Check(release.Series, Equals, "16")
}

const refMockOSRelease = `
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

const refMockUbuntuCoreInitrdRelease = `
NAME="Ubuntu Core Initramfs"
VERSION="1"
ID=ubuntucoreinitramfs
PRETTY_NAME="Ubuntu Core 24 Initramfs"
VERSION_ID="1"
VERSION_CODENAME=24
`

func mockOSRelease(c *C, root string, filesWithContent [][]string) {
	for _, e := range filesWithContent {
		target := filepath.Join(root, e[0])
		c.Assert(os.MkdirAll(filepath.Dir(target), 0o755), IsNil)
		c.Assert(os.WriteFile(target, []byte(e[1]), 0o644), IsNil)
	}
}

// MockFilesystemRootType changes relase.ProcMountsPath so that it points to a temp file
// generated to contain the provided filesystem type
func MockFilesystemRootType(c *C, fsType string) (restorer func()) {
	tmpfile, err := os.CreateTemp(c.MkDir(), "proc_mounts_mock_")
	c.Assert(err, IsNil)

	// Sample contents of /proc/mounts. The second line is the one that matters.
	_, err = tmpfile.Write([]byte(fmt.Sprintf(`none /usr/lib/wsl/lib overlay rw,relatime,lowerdir=/gpu_lib_packaged:/gpu_lib_inbox,upperdir=/gpu_lib/rw/upper,workdir=/gpu_lib/rw/work 0 0
/dev/sdc / %s rw,relatime,discard,errors=remount-ro,data=ordered 0 0
none /mnt/wslg tmpfs rw,relatime 0 0
`, fsType)))
	c.Assert(err, IsNil)

	restorer = testutil.Backup(release.ProcMountsPath)
	*release.ProcMountsPath = tmpfile.Name()
	return restorer
}

type mockWsl struct {
	ExistsInterop bool
	ExistsRunWSL  bool
	FsType        string
}

func mockWSLsetup(c *C, settings mockWsl) func() {
	restoreFileExists := release.MockFileExists(func(s string) bool {
		if s == "/proc/sys/fs/binfmt_misc/WSLInterop" {
			return settings.ExistsInterop
		}
		if s == "/run/WSL" {
			return settings.ExistsRunWSL
		}
		return osutil.FileExists(s)
	})

	restoreFilesystemRootType := MockFilesystemRootType(c, settings.FsType)

	return func() {
		restoreFileExists()
		restoreFilesystemRootType()
	}
}

func (s *ReleaseTestSuite) TestReadOSRelease(c *C) {
	root := c.MkDir()
	mockOSRelease(c, root, [][]string{
		{"/etc/os-release", refMockOSRelease},
	})

	os := release.ReadOSReleaseFromRoot(root)
	c.Check(os.ID, Equals, "ubuntu")
	c.Check(os.VersionID, Equals, "18.09")
}

func (s *ReleaseTestSuite) TestReadOSReleaseSymlink(c *C) {
	root := c.MkDir()
	mockOSRelease(c, root, [][]string{
		{"/etc/mock-release", refMockOSRelease},
	})

	// now try again but with a symlink
	c.Assert(os.Symlink("mock-release", filepath.Join(root, "/etc/os-release")), IsNil)

	osRel := release.ReadOSReleaseFromRoot(root)
	c.Check(osRel.ID, Equals, "ubuntu")
	c.Check(osRel.VersionID, Equals, "18.09")
}

func (s *ReleaseTestSuite) TestReadOSReleaseFallback(c *C) {
	root := c.MkDir()
	mockOSRelease(c, root, [][]string{
		{"/usr/lib/os-release", refMockOSRelease},
	})

	os := release.ReadOSReleaseFromRoot(root)
	c.Check(os.ID, Equals, "ubuntu")
	c.Check(os.VersionID, Equals, "18.09")
}

func (s *ReleaseTestSuite) TestReadOSReleaseInitrdDirect(c *C) {
	root := c.MkDir()
	mockOSRelease(c, root, [][]string{
		{"/etc/initrd-release", refMockUbuntuCoreInitrdRelease},
	})

	os := release.ReadOSReleaseFromRoot(root)
	c.Check(os.ID, Equals, "ubuntucoreinitramfs")
	c.Check(os.VersionID, Equals, "1")
}

func (s *ReleaseTestSuite) TestReadOSReleaseInitrdSymlink(c *C) {
	root := c.MkDir()
	mockOSRelease(c, root, [][]string{
		{"/etc/mock-initrd-release", refMockUbuntuCoreInitrdRelease},
	})

	// now try again but with a symlink
	c.Assert(os.Symlink("mock-initrd-release", filepath.Join(root, "/etc/os-release")), IsNil)

	osRel := release.ReadOSReleaseFromRoot(root)
	c.Check(osRel.ID, Equals, "ubuntucoreinitramfs")
	c.Check(osRel.VersionID, Equals, "1")
}

func (s *ReleaseTestSuite) TestReadWonkyOSRelease(c *C) {
	dump := `NAME="elementary OS"
VERSION="0.4 Loki"
ID="elementary OS"
ID_LIKE=ubuntu
PRETTY_NAME="elementary OS Loki"
VERSION_ID="0.4"
HOME_URL="http://elementary.io/"
SUPPORT_URL="http://elementary.io/support/"
BUG_REPORT_URL="https://bugs.launchpad.net/elementary/+filebug"`

	root := c.MkDir()
	mockOSRelease(c, root, [][]string{
		{"/etc/os-release", dump},
	})

	os := release.ReadOSReleaseFromRoot(root)
	c.Check(os.ID, Equals, "elementary")
	c.Check(os.VersionID, Equals, "0.4")
}

func (s *ReleaseTestSuite) TestFamilyOSRelease(c *C) {
	dump := `NAME="CentOS Stream"
VERSION="9"
ID="centos"
ID_LIKE="rhel fedora"
VERSION_ID="9"
PLATFORM_ID="platform:el9"
PRETTY_NAME="CentOS Stream 9"
ANSI_COLOR="0;31"
LOGO="fedora-logo-icon"
CPE_NAME="cpe:/o:centos:centos:9"
HOME_URL="https://centos.org/"
BUG_REPORT_URL="https://issues.redhat.com/"
REDHAT_SUPPORT_PRODUCT="Red Hat Enterprise Linux 9"
REDHAT_SUPPORT_PRODUCT_VERSION="CentOS Stream"`

	root := c.MkDir()
	mockOSRelease(c, root, [][]string{
		{"/etc/os-release", dump},
	})

	os := release.ReadOSReleaseFromRoot(root)
	c.Check(os.ID, Equals, "centos")
	c.Check(os.VersionID, Equals, "9")
	c.Check(os.IDLike, DeepEquals, []string{"rhel", "fedora"})
}

func (s *ReleaseTestSuite) TestUbuntuCoreVariantRelease(c *C) {
	dump := `NAME="Ubuntu Core Desktop"
VERSION="22"
ID="ubuntu-core"
VARIANT_ID="desktop"
VERSION_ID="22"
"`

	root := c.MkDir()
	mockOSRelease(c, root, [][]string{
		{"/etc/os-release", dump},
	})

	os := release.ReadOSReleaseFromRoot(root)
	c.Check(os.ID, Equals, "ubuntu-core")
	c.Check(os.VariantID, Equals, "desktop")
	c.Check(os.VersionID, Equals, "22")
}

func (s *ReleaseTestSuite) TestReadOSReleaseNotFound(c *C) {
	root := c.MkDir()
	os := release.ReadOSReleaseFromRoot(root)
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

func (s *ReleaseTestSuite) TestOnCoreDesktop(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()
	c.Assert(release.OnCoreDesktop, Equals, true)

	reset = release.MockOnCoreDesktop(false)
	defer reset()
	c.Assert(release.OnCoreDesktop, Equals, false)
}

func (s *ReleaseTestSuite) TestReleaseInfo(c *C) {
	reset := release.MockReleaseInfo(&release.OS{
		ID: "distro-id",
	})
	defer reset()
	c.Assert(release.ReleaseInfo.ID, Equals, "distro-id")
}

func (s *ReleaseTestSuite) TestNonWSL(c *C) {
	defer mockWSLsetup(c, mockWsl{ExistsInterop: false, ExistsRunWSL: false, FsType: "ext4"})()
	v := release.GetWSLVersion()
	c.Check(v, Equals, 0)
}

func (s *ReleaseTestSuite) TestWSL1(c *C) {
	defer mockWSLsetup(c, mockWsl{ExistsInterop: true, ExistsRunWSL: true, FsType: "wslfs"})()
	v := release.GetWSLVersion()
	c.Check(v, Equals, 1)
}

func (s *ReleaseTestSuite) TestWSL1Old(c *C) {
	defer mockWSLsetup(c, mockWsl{ExistsInterop: true, ExistsRunWSL: true, FsType: "lxfs"})()
	v := release.GetWSLVersion()
	c.Check(v, Equals, 1)
}

func (s *ReleaseTestSuite) TestWSL2(c *C) {
	defer mockWSLsetup(c, mockWsl{ExistsInterop: true, ExistsRunWSL: true, FsType: "ext4"})()
	v := release.GetWSLVersion()
	c.Check(v, Equals, 2)
}

func (s *ReleaseTestSuite) TestWSL2NoInterop(c *C) {
	defer mockWSLsetup(c, mockWsl{ExistsInterop: false, ExistsRunWSL: true, FsType: "ext4"})()
	v := release.GetWSLVersion()
	c.Check(v, Equals, 2)
}

func (s *ReleaseTestSuite) TestLXDInWSL2(c *C) {
	defer mockWSLsetup(c, mockWsl{ExistsInterop: true, ExistsRunWSL: false, FsType: "btrfs"})()
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
