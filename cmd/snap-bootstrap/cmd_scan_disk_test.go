// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package main_test

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-bootstrap"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/blkid"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/testutil"
)

type scanDiskSuite struct {
	testutil.BaseTest

	diskProbeMap map[string]*blkid.FakeBlkidProbe
	partProbeMap map[int64]*blkid.FakeBlkidProbe
	cmdlineFile  string
	env          map[string]string
}

var _ = Suite(&scanDiskSuite{})

func (s *scanDiskSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())

	s.diskProbeMap = make(map[string]*blkid.FakeBlkidProbe)
	s.partProbeMap = make(map[int64]*blkid.FakeBlkidProbe)

	s.AddCleanup(blkid.MockBlkidMap(s.diskProbeMap))
	s.AddCleanup(blkid.MockBlkidPartitionMap(s.partProbeMap))

	disk_values := make(map[string]string)
	disk_values["PTTYPE"] = "gpt"
	disk_probe := blkid.BuildFakeProbe(disk_values)
	for i, partition := range []struct {
		node  string
		label string
		uuid  string
	}{
		{"/dev/foop1", "ubuntu-seed", "6ae5a792-912e-43c9-ac92-e36723bbda12"},
		{"/dev/foop2", "ubuntu-boot", "29261148-b8ba-4335-b934-417ed71e9e91"},
		{"/dev/foop3", "ubuntu-data-enc", "c01a272d-fc72-40de-92fb-242c2da82533"},
		{"/dev/foop4", "ubuntu-save-enc", "050ee326-ab58-4eb4-ba7d-13694b2d0c8a"},
	} {
		s.partProbeMap[int64(i)] = disk_probe.AddPartitionProbe(partition.label, partition.uuid, int64(i))
	}
	s.diskProbeMap["/dev/foo"] = disk_probe

	s.cmdlineFile = filepath.Join(c.MkDir(), "proc-cmdline")
	err := os.WriteFile(s.cmdlineFile, []byte("snapd_recovery_mode=run"), 0644)
	c.Assert(err, IsNil)
	cmdlineCleanup := kcmdline.MockProcCmdline(s.cmdlineFile)
	s.AddCleanup(cmdlineCleanup)

	s.env = make(map[string]string)
	cleanupEnv := main.MockOsGetenv(func(envVar string) string {
		return s.env[envVar]
	})
	s.AddCleanup(cleanupEnv)
}

func (s *scanDiskSuite) setCmdLine(c *C, value string) {
	err := os.WriteFile(s.cmdlineFile, []byte(value), 0644)
	c.Assert(err, IsNil)
}

type outputScanner struct {
	buffer *bytes.Buffer
}

func newBuffer() *outputScanner {
	return &outputScanner{&bytes.Buffer{}}
}

func (o *outputScanner) File() *bytes.Buffer {
	return o.buffer
}

func (o *outputScanner) GetLines() map[string]struct{} {
	scanner := bufio.NewScanner(bytes.NewReader(o.buffer.Bytes()))
	lines := make(map[string]struct{})
	for scanner.Scan() {
		lines[scanner.Text()] = struct{}{}
	}
	return lines
}

func (s *scanDiskSuite) TestDetectBootDisk(c *C) {
	main.MockPartitionUUIDForBootedKernelDisk("29261148-b8ba-4335-b934-417ed71e9e91")

	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	c.Assert(len(lines), Equals, 1)
}

func (s *scanDiskSuite) TestDetectBootDiskCVM(c *C) {
	main.MockPartitionUUIDForBootedKernelDisk("29261148-b8ba-4335-b934-417ed71e9e91")

	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	s.diskProbeMap = make(map[string]*blkid.FakeBlkidProbe)
	defer blkid.MockBlkidMap(s.diskProbeMap)()

	disk_values := make(map[string]string)
	disk_values["PTTYPE"] = "gpt"
	disk_probe := blkid.BuildFakeProbe(disk_values)
	for i, partition := range []struct {
		node  string
		label string
		uuid  string
	}{
		{"/dev/foop1", "SEED", "6ae5a792-912e-43c9-ac92-e36723bbda12"},
		{"/dev/foop2", "BOOT", "29261148-b8ba-4335-b934-417ed71e9e91"},
	} {
		s.partProbeMap[int64(i)] = disk_probe.AddPartitionProbe(partition.label, partition.uuid, int64(i))
	}
	s.diskProbeMap["/dev/foo"] = disk_probe

	mockProcCmdline := filepath.Join(c.MkDir(), "proc-cmdline")
	c.Assert(os.WriteFile(mockProcCmdline, []byte("snapd_recovery_mode=cloudimg-rootfs"), 0644), IsNil)
	defer kcmdline.MockProcCmdline(mockProcCmdline)()

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	c.Assert(len(lines), Equals, 1)
}

func (s *scanDiskSuite) TestDetectBootDiskNotUEFIBoot(c *C) {
	main.MockPartitionUUIDForBootedKernelDisk("ffffffff-ffff-ffff-ffff-ffffffffffff")

	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	c.Assert(len(lines), Equals, 0)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackGPT(c *C) {
	main.MockPartitionUUIDForBootedKernelDisk("")

	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	c.Assert(len(lines), Equals, 1)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackMBR(c *C) {
	main.MockPartitionUUIDForBootedKernelDisk("")

	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	// default is setup to be PTTYPE=gpt, remake as MBR
	disk_values := make(map[string]string)
	disk_probe := blkid.BuildFakeProbe(disk_values)
	s.partProbeMap[0] = disk_probe.AddPartitionProbe("ubuntu-seed", "6ae5a792-912e-43c9-ac92-e36723bbda12", 0)
	s.partProbeMap[1] = disk_probe.AddPartitionProbe("ubuntu-boot", "29261148-b8ba-4335-b934-417ed71e9e91", 1)
	s.partProbeMap[2] = disk_probe.AddPartitionProbe("ubuntu-data-enc", "c01a272d-fc72-40de-92fb-242c2da82533", 2)
	s.partProbeMap[3] = disk_probe.AddPartitionProbe("ubuntu-save-enc", "050ee326-ab58-4eb4-ba7d-13694b2d0c8a", 3)
	s.diskProbeMap["/dev/foo"] = disk_probe

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	c.Assert(len(lines), Equals, 1)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackInstall(c *C) {
	s.setCmdLine(c, "snapd_recovery_mode=install snapd_recovery_system=20191118")
	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	disk_values := make(map[string]string)
	disk_values["PTTYPE"] = "gpt"
	disk_probe := blkid.BuildFakeProbe(disk_values)
	s.partProbeMap[0] = disk_probe.AddPartitionProbe("ubuntu-seed", "6ae5a792-912e-43c9-ac92-e36723bbda12", 0)
	s.diskProbeMap["/dev/foo"] = disk_probe
	delete(s.partProbeMap, 1)
	delete(s.partProbeMap, 2)
	delete(s.partProbeMap, 3)

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	c.Assert(len(lines), Equals, 1)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackMissingBoot(c *C) {
	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	disk_values := make(map[string]string)
	disk_values["PTTYPE"] = "gpt"
	disk_probe := blkid.BuildFakeProbe(disk_values)
	s.partProbeMap[0] = disk_probe.AddPartitionProbe("ubuntu-seed", "6ae5a792-912e-43c9-ac92-e36723bbda12", 0)
	s.partProbeMap[2] = disk_probe.AddPartitionProbe("ubuntu-data-enc", "c01a272d-fc72-40de-92fb-242c2da82533", 2)
	s.partProbeMap[3] = disk_probe.AddPartitionProbe("ubuntu-save-enc", "050ee326-ab58-4eb4-ba7d-13694b2d0c8a", 3)
	s.diskProbeMap["/dev/foo"] = disk_probe
	delete(s.partProbeMap, 1)

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	c.Assert(len(lines), Equals, 0)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackMissingSeedRecover(c *C) {
	s.setCmdLine(c, "snapd_recovery_mode=recover")

	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	disk_values := make(map[string]string)
	disk_values["PTTYPE"] = "gpt"
	disk_probe := blkid.BuildFakeProbe(disk_values)
	s.partProbeMap[1] = disk_probe.AddPartitionProbe("ubuntu-boot", "29261148-b8ba-4335-b934-417ed71e9e91", 1)
	s.partProbeMap[2] = disk_probe.AddPartitionProbe("ubuntu-data-enc", "c01a272d-fc72-40de-92fb-242c2da82533", 2)
	s.partProbeMap[3] = disk_probe.AddPartitionProbe("ubuntu-save-enc", "050ee326-ab58-4eb4-ba7d-13694b2d0c8a", 3)

	s.diskProbeMap["/dev/foo"] = disk_probe
	delete(s.partProbeMap, 0)

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	c.Assert(len(lines), Equals, 0)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackKernelParam(c *C) {
	devFoo := filepath.Join(dirs.GlobalRootDir, "/dev/foo")
	c.Assert(os.MkdirAll(filepath.Dir(devFoo), 0755), IsNil)
	c.Assert(os.WriteFile(devFoo, []byte{}, 0644), IsNil)

	s.setCmdLine(c, "snapd_system_disk=/dev/foo snapd_recovery_mode=run")

	s.env["DEVPATH"] = "/sys/devices/foo"
	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	c.Assert(len(lines), Equals, 1)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackKernelParamDevPath(c *C) {
	devFoo := filepath.Join(dirs.GlobalRootDir, "/sys/devices/foo")
	c.Assert(os.MkdirAll(filepath.Dir(devFoo), 0755), IsNil)
	c.Assert(os.WriteFile(devFoo, []byte{}, 0644), IsNil)

	s.setCmdLine(c, "snapd_system_disk=/sys/devices/foo snapd_recovery_mode=run")

	s.env["DEVPATH"] = "/sys/devices/foo"
	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	c.Assert(len(lines), Equals, 1)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackKernelParamNotMatching(c *C) {
	devFoo := filepath.Join(dirs.GlobalRootDir, "/dev/foo")
	c.Assert(os.MkdirAll(filepath.Dir(devFoo), 0755), IsNil)
	c.Assert(os.WriteFile(devFoo, []byte{}, 0644), IsNil)
	devBar := filepath.Join(dirs.GlobalRootDir, "/dev/bar")
	c.Assert(os.MkdirAll(filepath.Dir(devBar), 0755), IsNil)
	c.Assert(os.WriteFile(devBar, []byte{}, 0644), IsNil)

	// Ask for /dev/bar instead of /dev/foo
	s.setCmdLine(c, "snapd_system_disk=/dev/bar snapd_recovery_mode=run")

	s.env["DEVPATH"] = "/sys/devices/foo"
	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()
	c.Check(len(lines), Equals, 0)
}
