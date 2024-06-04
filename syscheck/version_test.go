// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/syscheck"
)

type versionSuite struct{}

var _ = Suite(&versionSuite{})

func (s *syscheckSuite) TestFreshInstallOfSnapdOnTrusty(c *C) {
	// Mock an Ubuntu 14.04 system running a 3.13.0 kernel
	restore := release.MockOnClassic(true)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer restore()
	restore = osutil.MockKernelVersion("3.13.0-35-generic")
	defer restore()

	// Check for the given advice.
	err := syscheck.CheckKernelVersion()
	c.Assert(err, ErrorMatches, "you need to reboot into a 4.4 kernel to start using snapd")
}

func (s *syscheckSuite) TestRebootedOnTrusty(c *C) {
	// Mock an Ubuntu 14.04 system running a 4.4.0 kernel
	restore := release.MockOnClassic(true)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer restore()
	restore = osutil.MockKernelVersion("4.4.0-112-generic")
	defer restore()

	// Check for the given advice.
	err := syscheck.CheckKernelVersion()
	c.Assert(err, IsNil)
}

func (s *syscheckSuite) TestRHEL80OK(c *C) {
	// Mock an Ubuntu 14.04 system running a 4.4.0 kernel
	restore := release.MockOnClassic(true)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "rhel", VersionID: "8.0"})
	defer restore()
	// RHEL 8 beta
	restore = osutil.MockKernelVersion("4.18.0-32.el8.x86_64")
	defer restore()

	// Check for the given advice.
	err := syscheck.CheckKernelVersion()
	c.Assert(err, IsNil)
}

func (s *syscheckSuite) TestRHEL7x(c *C) {
	dir := c.MkDir()
	dirs.SetRootDir(dir)
	defer dirs.SetRootDir("/")
	// mock RHEL 7.6
	restore := release.MockOnClassic(true)
	defer restore()
	// VERSION="7.6 (Maipo)"
	// ID="rhel"
	// ID_LIKE="fedora"
	// VERSION_ID="7.6"
	restore = release.MockReleaseInfo(&release.OS{ID: "rhel", VersionID: "7.6"})
	defer restore()
	restore = osutil.MockKernelVersion("3.10.0-957.el7.x86_64")
	defer restore()

	// pretend the kernel knob is not there
	err := syscheck.CheckKernelVersion()
	c.Assert(err, ErrorMatches, "cannot read the value of fs.may_detach_mounts kernel parameter: .*")

	p := filepath.Join(dir, "/proc/sys/fs/may_detach_mounts")
	err = os.MkdirAll(filepath.Dir(p), 0755)
	c.Assert(err, IsNil)

	// the knob is there, but disabled
	err = os.WriteFile(p, []byte("0\n"), 0644)
	c.Assert(err, IsNil)

	err = syscheck.CheckKernelVersion()
	c.Assert(err, ErrorMatches, "fs.may_detach_mounts kernel parameter is supported but disabled")

	// actually enabled
	err = os.WriteFile(p, []byte("1\n"), 0644)
	c.Assert(err, IsNil)

	err = syscheck.CheckKernelVersion()
	c.Assert(err, IsNil)

	// custom kernel version, which is old and we have no knowledge about
	restore = osutil.MockKernelVersion("3.10.0-1024.foo.x86_64")
	defer restore()
	err = syscheck.CheckKernelVersion()
	c.Assert(err, ErrorMatches, `unsupported kernel version "3.10.0-1024.foo.x86_64", you need to switch to the stock kernel`)

	// custom kernel version, but new enough
	restore = osutil.MockKernelVersion("4.18.0-32.foo.x86_64")
	defer restore()
	err = syscheck.CheckKernelVersion()
	c.Assert(err, IsNil)
}

func (s *syscheckSuite) TestCentOS7x(c *C) {
	dir := c.MkDir()
	dirs.SetRootDir(dir)
	defer dirs.SetRootDir("/")
	// mock CentOS 7.5
	restore := release.MockOnClassic(true)
	defer restore()
	// NAME="CentOS Linux"
	// VERSION="7 (Core)"
	// ID="centos"
	// ID_LIKE="rhel fedora"
	// VERSION_ID="7"
	restore = release.MockReleaseInfo(&release.OS{ID: "centos", VersionID: "7"})
	defer restore()
	restore = osutil.MockKernelVersion("3.10.0-862.14.4.el7.x86_64")
	defer restore()

	p := filepath.Join(dir, "/proc/sys/fs/may_detach_mounts")
	err := os.MkdirAll(filepath.Dir(p), 0755)
	c.Assert(err, IsNil)

	// the knob there, but disabled
	err = os.WriteFile(p, []byte("0\n"), 0644)
	c.Assert(err, IsNil)

	err = syscheck.CheckKernelVersion()
	c.Assert(err, ErrorMatches, "fs.may_detach_mounts kernel parameter is supported but disabled")

	// actually enabled
	err = os.WriteFile(p, []byte("1\n"), 0644)
	c.Assert(err, IsNil)

	err = syscheck.CheckKernelVersion()
	c.Assert(err, IsNil)
}
