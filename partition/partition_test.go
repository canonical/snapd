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

package partition

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// partition specific testsuite
type PartitionTestSuite struct {
	tempdir string
}

var _ = Suite(&PartitionTestSuite{})

func mockRunCommand(args ...string) (err error) {
	return err
}

func (s *PartitionTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	runLsblk = mockRunLsblkDualSnappy

	// custom mount target
	mountTarget = c.MkDir()

	// setup fake paths for grub
	bootloaderGrubDir = filepath.Join(s.tempdir, "boot", "grub")
	bootloaderGrubConfigFile = filepath.Join(bootloaderGrubDir, "grub.cfg")
	bootloaderGrubEnvFile = filepath.Join(bootloaderGrubDir, "grubenv")

	// and uboot
	bootloaderUbootDir = filepath.Join(s.tempdir, "boot", "uboot")
	bootloaderUbootConfigFile = filepath.Join(bootloaderUbootDir, "uEnv.txt")
	bootloaderUbootEnvFile = filepath.Join(bootloaderUbootDir, "uEnv.txt")
	bootloaderUbootFwEnvFile = filepath.Join(bootloaderUbootDir, "uboot.env")
	bootloaderUbootStampFile = filepath.Join(bootloaderUbootDir, "snappy-stamp.txt")

	c.Assert(mounts, DeepEquals, mountEntryArray(nil))
}

func (s *PartitionTestSuite) TearDownTest(c *C) {
	os.RemoveAll(s.tempdir)

	// always restore what we might have mocked away
	runCommand = runCommandImpl
	bootloader = bootloaderImpl
	cacheDir = cacheDirReal
	hardwareSpecFile = hardwareSpecFileReal
	mountTarget = mountTargetReal

	// grub vars
	bootloaderGrubConfigFile = bootloaderGrubConfigFileReal
	bootloaderGrubEnvFile = bootloaderGrubEnvFileReal

	// uboot vars
	bootloaderUbootDir = bootloaderUbootDirReal
	bootloaderUbootConfigFile = bootloaderUbootConfigFileReal
	bootloaderUbootEnvFile = bootloaderUbootEnvFileReal
	bootloaderUbootStampFile = bootloaderUbootStampFileReal

	c.Assert(mounts, DeepEquals, mountEntryArray(nil))
}

func makeHardwareYaml(c *C, hardwareYaml string) (outPath string) {
	tmp, err := ioutil.TempFile(c.MkDir(), "hw-")
	c.Assert(err, IsNil)
	defer tmp.Close()

	if hardwareYaml == "" {
		hardwareYaml = `
kernel: assets/vmlinuz
initrd: assets/initrd.img
dtbs: assets/dtbs
partition-layout: system-AB
bootloader: u-boot
`
	}
	_, err = tmp.Write([]byte(hardwareYaml))
	c.Assert(err, IsNil)

	return tmp.Name()
}

func mockRunLsblkDualSnappy() (output []string, err error) {
	dualData := `
NAME="sda" LABEL="" PKNAME="" MOUNTPOINT=""
NAME="sda1" LABEL="" PKNAME="sda" MOUNTPOINT=""
NAME="sda2" LABEL="system-boot" PKNAME="sda" MOUNTPOINT="/boot/efi"
NAME="sda3" LABEL="system-a" PKNAME="sda" MOUNTPOINT="/"
NAME="sda4" LABEL="system-b" PKNAME="sda" MOUNTPOINT=""
NAME="sda5" LABEL="writable" PKNAME="sda" MOUNTPOINT="/writable"
NAME="sr0" LABEL="" PKNAME="" MOUNTPOINT=""
`
	return strings.Split(dualData, "\n"), err
}

func (s *PartitionTestSuite) TestSnappyDualRoot(c *C) {
	p := New()
	c.Assert(p.dualRootPartitions(), Equals, true)
	c.Assert(p.singleRootPartition(), Equals, false)

	rootPartitions := p.rootPartitions()
	c.Assert(rootPartitions[0].name, Equals, "system-a")
	c.Assert(rootPartitions[0].device, Equals, "/dev/sda3")
	c.Assert(rootPartitions[0].parentName, Equals, "/dev/sda")
	c.Assert(rootPartitions[1].name, Equals, "system-b")
	c.Assert(rootPartitions[1].device, Equals, "/dev/sda4")
	c.Assert(rootPartitions[1].parentName, Equals, "/dev/sda")

	wp := p.writablePartition()
	c.Assert(wp.name, Equals, "writable")
	c.Assert(wp.device, Equals, "/dev/sda5")
	c.Assert(wp.parentName, Equals, "/dev/sda")

	boot := p.bootPartition()
	c.Assert(boot.name, Equals, "system-boot")
	c.Assert(boot.device, Equals, "/dev/sda2")
	c.Assert(boot.parentName, Equals, "/dev/sda")

	root := p.rootPartition()
	c.Assert(root.name, Equals, "system-a")
	c.Assert(root.device, Equals, "/dev/sda3")
	c.Assert(root.parentName, Equals, "/dev/sda")

	other := p.otherRootPartition()
	c.Assert(other.name, Equals, "system-b")
	c.Assert(other.device, Equals, "/dev/sda4")
	c.Assert(other.parentName, Equals, "/dev/sda")
}

func (s *PartitionTestSuite) TestRunWithOtherDualParitionRO(c *C) {
	p := New()
	reportedRoot := ""
	err := p.RunWithOther(RO, func(otherRoot string) (err error) {
		reportedRoot = otherRoot
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(reportedRoot, Equals, mountTarget)
}

func (s *PartitionTestSuite) TestRunWithOtherDualParitionRWFuncErr(c *C) {
	c.Assert(mounts, DeepEquals, mountEntryArray(nil))

	runCommand = mockRunCommand

	p := New()
	err := p.RunWithOther(RW, func(otherRoot string) (err error) {
		return errors.New("canary")
	})

	// ensure we actually got the right error
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "canary")

	// ensure cleanup happend

	// FIXME: mounts are global
	expected := mountEntry{
		source:    "/dev/sda4",
		target:    mountTarget,
		options:   "",
		bindMount: false,
	}

	// At program exit, "other" should still be mounted
	c.Assert(mounts, DeepEquals, mountEntryArray{expected})

	undoMounts(false)

	c.Assert(mounts, DeepEquals, mountEntryArray(nil))
}

func (s *PartitionTestSuite) TestRunWithOtherSingleParitionRO(c *C) {
	runLsblk = mockRunLsblkSingleRootSnappy
	p := New()
	err := p.RunWithOther(RO, func(otherRoot string) (err error) {
		return nil
	})
	c.Assert(err, Equals, ErrNoDualPartition)
}

func mockRunLsblkSingleRootSnappy() (output []string, err error) {
	dualData := `
NAME="sda" LABEL="" PKNAME="" MOUNTPOINT=""
NAME="sda1" LABEL="" PKNAME="sda" MOUNTPOINT=""
NAME="sda2" LABEL="system-boot" PKNAME="sda" MOUNTPOINT=""
NAME="sda3" LABEL="system-a" PKNAME="sda" MOUNTPOINT="/"
NAME="sda5" LABEL="writable" PKNAME="sda" MOUNTPOINT="/writable"
`
	return strings.Split(dualData, "\n"), err
}
func (s *PartitionTestSuite) TestSnappySingleRoot(c *C) {
	runLsblk = mockRunLsblkSingleRootSnappy

	p := New()
	c.Assert(p.dualRootPartitions(), Equals, false)
	c.Assert(p.singleRootPartition(), Equals, true)

	root := p.rootPartition()
	c.Assert(root.name, Equals, "system-a")
	c.Assert(root.device, Equals, "/dev/sda3")
	c.Assert(root.parentName, Equals, "/dev/sda")

	other := p.otherRootPartition()
	c.Assert(other, IsNil)

	rootPartitions := p.rootPartitions()
	c.Assert(&rootPartitions[0], DeepEquals, root)
}

func (s *PartitionTestSuite) TestMountUnmountTracking(c *C) {
	runCommand = mockRunCommand

	p := New()
	c.Assert(p, NotNil)

	p.mountOtherRootfs(false)
	expected := mountEntry{
		source:    "/dev/sda4",
		target:    mountTarget,
		options:   "",
		bindMount: false,
	}

	c.Assert(mounts, DeepEquals, mountEntryArray{expected})

	p.unmountOtherRootfs()
	c.Assert(mounts, DeepEquals, mountEntryArray(nil))
}

func (s *PartitionTestSuite) TestUnmountRequiredFilesystems(c *C) {
	runCommand = mockRunCommand
	s.makeFakeGrubEnv(c)

	p := New()
	c.Assert(c, NotNil)

	p.bindmountRequiredFilesystems()
	c.Assert(mounts, DeepEquals, mountEntryArray{
		mountEntry{source: "/dev", target: mountTarget + "/dev",
			options: "bind", bindMount: true},

		mountEntry{source: "/proc", target: mountTarget + "/proc",
			options: "bind", bindMount: true},

		mountEntry{source: "/sys", target: mountTarget + "/sys",
			options: "bind", bindMount: true},
		mountEntry{source: "/boot/efi", target: mountTarget + "/boot/efi",
			options: "bind", bindMount: true},

		// Required to allow grub inside the chroot to access
		// the "current" rootfs outside the chroot (used
		// to generate the grub menuitems).
		mountEntry{source: "/",
			target:  mountTarget + mountTarget,
			options: "bind,ro", bindMount: true},
	})
	p.unmountRequiredFilesystems()
	c.Assert(mounts, DeepEquals, mountEntryArray(nil))
}

func (s *PartitionTestSuite) TestUndoMounts(c *C) {
	runCommand = mockRunCommand

	p := New()
	c.Assert(c, NotNil)

	err := p.remountOther(RW)
	c.Assert(err, IsNil)

	p.bindmountRequiredFilesystems()
	c.Assert(mounts, DeepEquals, mountEntryArray{

		mountEntry{source: "/dev/sda4", target: mountTarget,
			options: "", bindMount: false},

		mountEntry{source: "/dev", target: mountTarget + "/dev",
			options: "bind", bindMount: true},

		mountEntry{source: "/proc", target: mountTarget + "/proc",
			options: "bind", bindMount: true},

		mountEntry{source: "/sys", target: mountTarget + "/sys",
			options: "bind", bindMount: true},

		mountEntry{source: "/boot/efi", target: mountTarget + "/boot/efi",
			options: "bind", bindMount: true},

		mountEntry{source: "/",
			target:  mountTarget + mountTarget,
			options: "bind,ro", bindMount: true},
	})

	// should leave non-bind mounts
	undoMounts(true)

	c.Assert(mounts, DeepEquals, mountEntryArray{
		mountEntry{
			source:    "/dev/sda4",
			target:    mountTarget,
			options:   "",
			bindMount: false,
		},
	})

	// should unmount everything
	undoMounts(false)

	c.Assert(mounts, DeepEquals, mountEntryArray(nil))
}

func mockRunLsblkNoSnappy() (output []string, err error) {
	dualData := `
NAME="sda" LABEL="" PKNAME="" MOUNTPOINT=""
NAME="sda1" LABEL="meep" PKNAME="sda" MOUNTPOINT="/"
NAME="sr0" LABEL="" PKNAME="" MOUNTPOINT=""
`
	return strings.Split(dualData, "\n"), err
}

func (s *PartitionTestSuite) TestSnappyNoSnappyPartitions(c *C) {
	runLsblk = mockRunLsblkNoSnappy

	p := New()
	err := p.getPartitionDetails()
	c.Assert(err, Equals, ErrPartitionDetection)

	c.Assert(p.dualRootPartitions(), Equals, false)
	c.Assert(p.singleRootPartition(), Equals, false)

	c.Assert(p.rootPartition(), IsNil)
	c.Assert(p.bootPartition(), IsNil)
	c.Assert(p.writablePartition(), IsNil)
	c.Assert(p.otherRootPartition(), IsNil)
}

// mock bootloader for the tests
type mockBootloader struct {
	ToggleRootFSCalled              bool
	HandleAssetsCalled              bool
	MarkCurrentBootSuccessfulCalled bool
	SyncBootFilesCalled             bool
}

func (b *mockBootloader) Name() BootloaderName {
	return "mocky"
}
func (b *mockBootloader) ToggleRootFS(otherRootfs string) error {
	b.ToggleRootFSCalled = true
	return nil
}
func (b *mockBootloader) SyncBootFiles(bootAssets map[string]string) error {
	b.SyncBootFilesCalled = true
	return nil
}
func (b *mockBootloader) HandleAssets() error {
	b.HandleAssetsCalled = true
	return nil
}
func (b *mockBootloader) GetBootVar(name string) (string, error) {
	return "", nil
}
func (b *mockBootloader) SetBootVar(name, value string) error {
	return nil
}
func (b *mockBootloader) GetNextBootRootFSName() (string, error) {
	return "", nil
}
func (b *mockBootloader) MarkCurrentBootSuccessful(currentRootfs string) error {
	b.MarkCurrentBootSuccessfulCalled = true
	return nil
}
func (b *mockBootloader) BootDir() string {
	return ""
}

func (s *PartitionTestSuite) TestToggleBootloaderRootfs(c *C) {
	runCommand = mockRunCommand
	b := &mockBootloader{}
	bootloader = func(p *Partition) (BootLoader, error) {
		return b, nil
	}

	p := New()
	c.Assert(c, NotNil)

	err := p.toggleBootloaderRootfs()
	c.Assert(err, IsNil)
	c.Assert(b.ToggleRootFSCalled, Equals, true)
	c.Assert(b.HandleAssetsCalled, Equals, true)

	p.unmountOtherRootfs()
	c.Assert(mounts, DeepEquals, mountEntryArray(nil))
}

func (s *PartitionTestSuite) TestMarkBootSuccessful(c *C) {
	runCommand = mockRunCommand
	b := &mockBootloader{}
	bootloader = func(p *Partition) (BootLoader, error) {
		return b, nil
	}

	p := New()
	c.Assert(c, NotNil)

	err := p.MarkBootSuccessful()
	c.Assert(err, IsNil)
	c.Assert(b.MarkCurrentBootSuccessfulCalled, Equals, true)
}

func (s *PartitionTestSuite) TestSyncBootFiles(c *C) {
	runCommand = mockRunCommand
	b := &mockBootloader{}
	bootloader = func(p *Partition) (BootLoader, error) {
		return b, nil
	}

	p := New()
	c.Assert(c, NotNil)

	err := p.SyncBootloaderFiles(nil)
	c.Assert(err, IsNil)
	c.Assert(b.SyncBootFilesCalled, Equals, true)
}
