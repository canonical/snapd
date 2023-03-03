// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-bootstrap"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/blkid"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type scanDiskSuite struct {
	testutil.BaseTest

	probeMap     map[string]*blkid.FakeBlkidProbe
	efiVariables map[string]string
	cmdlineFile  string
	env          map[string]string
}

var _ = Suite(&scanDiskSuite{})

func (s *scanDiskSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())

	s.probeMap = make(map[string]*blkid.FakeBlkidProbe)
	cleanupBlkid := blkid.MockBlkidMap(s.probeMap)
	s.AddCleanup(cleanupBlkid)

	s.efiVariables = make(map[string]string)
	cleanupEfiVars := main.MockEfiVars(s.efiVariables)
	s.AddCleanup(cleanupEfiVars)

	disk_values := make(map[string]string)
	disk_values["PTTYPE"] = "gpt"
	disk_probe := blkid.BuildFakeProbe(disk_values)
	for _, partition := range []struct {
		node  string
		label string
		uuid  string
	}{
		{"/dev/foop1", "ubuntu-seed", "6ae5a792-912e-43c9-ac92-e36723bbda12"},
		{"/dev/foop2", "ubuntu-boot", "29261148-b8ba-4335-b934-417ed71e9e91"},
		{"/dev/foop3", "ubuntu-data-enc", "c01a272d-fc72-40de-92fb-242c2da82533"},
		{"/dev/foop4", "ubuntu-save-enc", "050ee326-ab58-4eb4-ba7d-13694b2d0c8a"},
	} {
		values := make(map[string]string)
		values["PART_ENTRY_UUID"] = partition.uuid
		s.probeMap[partition.node] = blkid.BuildFakeProbe(values)
		disk_probe.AddPartition(partition.label, partition.uuid)
	}
	s.probeMap["/dev/foo"] = disk_probe

	s.cmdlineFile = filepath.Join(c.MkDir(), "proc-cmdline")
	err := ioutil.WriteFile(s.cmdlineFile, []byte(""), 0644)
	c.Assert(err, IsNil)
	cmdlineCleanup := osutil.MockProcCmdline(s.cmdlineFile)
	s.AddCleanup(cmdlineCleanup)

	s.env = make(map[string]string)
	cleanupEnv := main.MockGetenv(s.env)
	s.AddCleanup(cleanupEnv)
}

func (s *scanDiskSuite) setCmdLine(c *C, value string) {
	err := ioutil.WriteFile(s.cmdlineFile, []byte(value), 0644)
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
	s.efiVariables["LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"] = "29261148-B8BA-4335-B934-417ED71E9E91"

	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	_, hasSeed := lines["UBUNTU_SEED_UUID=6ae5a792-912e-43c9-ac92-e36723bbda12"]
	_, hasBoot := lines["UBUNTU_BOOT_UUID=29261148-b8ba-4335-b934-417ed71e9e91"]
	_, hasData := lines["UBUNTU_DATA_UUID=c01a272d-fc72-40de-92fb-242c2da82533"]
	_, hasSave := lines["UBUNTU_SAVE_UUID=050ee326-ab58-4eb4-ba7d-13694b2d0c8a"]
	c.Assert(hasSeed, Equals, true)
	c.Assert(hasBoot, Equals, true)
	c.Assert(hasData, Equals, true)
	c.Assert(hasSave, Equals, true)
	c.Assert(len(lines), Equals, 5)
}

func (s *scanDiskSuite) TestDetectBootDiskNotUEFIBoot(c *C) {
	s.efiVariables["LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"] = "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF"

	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	c.Assert(len(lines), Equals, 0)
}

func (s *scanDiskSuite) TestDetectBootDiskFallback(c *C) {
	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	_, hasSeed := lines["UBUNTU_SEED_UUID=6ae5a792-912e-43c9-ac92-e36723bbda12"]
	_, hasBoot := lines["UBUNTU_BOOT_UUID=29261148-b8ba-4335-b934-417ed71e9e91"]
	_, hasData := lines["UBUNTU_DATA_UUID=c01a272d-fc72-40de-92fb-242c2da82533"]
	_, hasSave := lines["UBUNTU_SAVE_UUID=050ee326-ab58-4eb4-ba7d-13694b2d0c8a"]
	c.Assert(hasSeed, Equals, true)
	c.Assert(hasBoot, Equals, true)
	c.Assert(hasData, Equals, true)
	c.Assert(hasSave, Equals, true)
	c.Assert(len(lines), Equals, 5)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackMissingSeed(c *C) {
	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	disk_values := make(map[string]string)
	disk_values["PTTYPE"] = "gpt"
	disk_probe := blkid.BuildFakeProbe(disk_values)
	disk_probe.AddPartition("ubuntu-boot", "29261148-b8ba-4335-b934-417ed71e9e91")
	disk_probe.AddPartition("ubuntu-data-enc", "c01a272d-fc72-40de-92fb-242c2da82533")
	disk_probe.AddPartition("ubuntu-save-enc", "050ee326-ab58-4eb4-ba7d-13694b2d0c8a")
	s.probeMap["/dev/foo"] = disk_probe
	delete(s.probeMap, "/dev/foop1")

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	c.Assert(len(lines), Equals, 0)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackKernelParam(c *C) {
	devFoo := filepath.Join(dirs.GlobalRootDir, "/dev/foo")
	c.Assert(os.MkdirAll(filepath.Dir(devFoo), 0755), IsNil)
	c.Assert(ioutil.WriteFile(devFoo, []byte{}, 0644), IsNil)

	s.setCmdLine(c, "snapd_system_disk=/dev/foo")

	s.env["DEVPATH"] = "/sys/devices/foo"
	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	_, hasSeed := lines["UBUNTU_SEED_UUID=6ae5a792-912e-43c9-ac92-e36723bbda12"]
	_, hasBoot := lines["UBUNTU_BOOT_UUID=29261148-b8ba-4335-b934-417ed71e9e91"]
	_, hasData := lines["UBUNTU_DATA_UUID=c01a272d-fc72-40de-92fb-242c2da82533"]
	_, hasSave := lines["UBUNTU_SAVE_UUID=050ee326-ab58-4eb4-ba7d-13694b2d0c8a"]
	c.Assert(hasSeed, Equals, true)
	c.Assert(hasBoot, Equals, true)
	c.Assert(hasData, Equals, true)
	c.Assert(hasSave, Equals, true)
	c.Assert(len(lines), Equals, 5)
}

func (s *scanDiskSuite) TestDetectBootDiskFallbackKernelParamDevPath(c *C) {
	devFoo := filepath.Join(dirs.GlobalRootDir, "/sys/devices/foo")
	c.Assert(os.MkdirAll(filepath.Dir(devFoo), 0755), IsNil)
	c.Assert(ioutil.WriteFile(devFoo, []byte{}, 0644), IsNil)

	s.setCmdLine(c, "snapd_system_disk=/sys/devices/foo")

	s.env["DEVPATH"] = "/sys/devices/foo"
	s.env["DEVNAME"] = "/dev/foo"
	s.env["DEVTYPE"] = "disk"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasDisk := lines["UBUNTU_DISK=1"]
	c.Assert(hasDisk, Equals, true)
	_, hasSeed := lines["UBUNTU_SEED_UUID=6ae5a792-912e-43c9-ac92-e36723bbda12"]
	_, hasBoot := lines["UBUNTU_BOOT_UUID=29261148-b8ba-4335-b934-417ed71e9e91"]
	_, hasData := lines["UBUNTU_DATA_UUID=c01a272d-fc72-40de-92fb-242c2da82533"]
	_, hasSave := lines["UBUNTU_SAVE_UUID=050ee326-ab58-4eb4-ba7d-13694b2d0c8a"]
	c.Assert(hasSeed, Equals, true)
	c.Assert(hasBoot, Equals, true)
	c.Assert(hasData, Equals, true)
	c.Assert(hasSave, Equals, true)
	c.Assert(len(lines), Equals, 5)
}

func (s *scanDiskSuite) TestDetectDataPartition(c *C) {
	s.env["DEVNAME"] = "/dev/foop3"
	s.env["DEVTYPE"] = "partition"
	s.env["UBUNTU_SEED_UUID"] = "6ae5a792-912e-43c9-ac92-e36723bbda12"
	s.env["UBUNTU_BOOT_UUID"] = "29261148-b8ba-4335-b934-417ed71e9e91"
	s.env["UBUNTU_DATA_UUID"] = "c01a272d-fc72-40de-92fb-242c2da82533"
	s.env["UBUNTU_SAVE_UUID"] = "050ee326-ab58-4eb4-ba7d-13694b2d0c8a"

	output := newBuffer()
	err := main.ScanDisk(output.File())
	c.Assert(err, IsNil)
	lines := output.GetLines()

	_, hasData := lines["UBUNTU_DATA=1"]
	c.Assert(hasData, Equals, true)
	c.Assert(len(lines), Equals, 1)
}
