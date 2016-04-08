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

package snappy

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
)

func mockRegenerateAppArmorRules() *bool {
	regenerateAppArmorRulesWasCalled := false
	regenerateAppArmorRules = func(string) error {
		regenerateAppArmorRulesWasCalled = true
		return nil
	}
	return &regenerateAppArmorRulesWasCalled
}

func (s *SnapTestSuite) TestAddHWAccessSimple(c *C) {
	makeInstalledMockSnap("")
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()

	err := AddHWAccess("hello-snap", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorAdditionalDir, "hello-snap.hwaccess.yaml"))
	c.Assert(err, IsNil)
	c.Assert("\n"+string(content), Equals, `
read-paths:
- /run/udev/data/*
write-paths:
- /dev/ttyUSB0
`)
	// ensure the regenerate code was called
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, true)
}

func (s *SnapTestSuite) TestAddHWAccessInvalidDevice(c *C) {
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	makeInstalledMockSnap("")

	err := AddHWAccess("hello-snap", "ttyUSB0")
	c.Assert(err, Equals, ErrInvalidHWDevice)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
}

func (s *SnapTestSuite) TestAddHWAccessMultiplePaths(c *C) {
	makeInstalledMockSnap("")

	err := AddHWAccess("hello-snap", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	err = AddHWAccess("hello-snap", "/sys/devices/gpio1")
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorAdditionalDir, "hello-snap.hwaccess.yaml"))
	c.Assert(err, IsNil)
	c.Assert("\n"+string(content), Equals, `
read-paths:
- /run/udev/data/*
write-paths:
- /dev/ttyUSB0
- /sys/devices/gpio1
`)

}

func (s *SnapTestSuite) TestAddHWAccessAddSameDeviceTwice(c *C) {
	makeInstalledMockSnap("")

	err := AddHWAccess("hello-snap", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	err = AddHWAccess("hello-snap", "/dev/ttyUSB0")
	c.Assert(err, Equals, ErrHWAccessAlreadyAdded)

	writePaths, err := ListHWAccess("hello-snap")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0"})
}

func (s *SnapTestSuite) TestAddHWAccessUnknownPackage(c *C) {
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()

	err := AddHWAccess("xxx", "/dev/ttyUSB0")
	c.Assert(err, Equals, ErrPackageNotFound)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
}

func (s *SnapTestSuite) TestAddHWAccessIllegalPackage(c *C) {
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()

	err := AddHWAccess("hello_svc1", "/dev/ttyUSB0")
	c.Assert(err, Equals, ErrPackageNotFound)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
}

func (s *SnapTestSuite) TestListHWAccessNoAdditionalAccess(c *C) {
	makeInstalledMockSnap("")

	writePaths, err := ListHWAccess("hello-snap")
	c.Assert(err, IsNil)
	c.Assert(writePaths, HasLen, 0)
}

func (s *SnapTestSuite) TestListHWAccess(c *C) {
	makeInstalledMockSnap("")
	err := AddHWAccess("hello-snap", "/dev/ttyUSB0")
	c.Assert(err, IsNil)

	err = AddHWAccess("hello-snap", "/sys/devices/gpio1")
	c.Assert(err, IsNil)

	err = AddHWAccess("hello-snap", "/sys/class/gpio/export")
	c.Assert(err, IsNil)

	err = AddHWAccess("hello-snap", "/sys/class/gpio/unexport")
	c.Assert(err, IsNil)

	writePaths, err := ListHWAccess("hello-snap")
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0", "/sys/devices/gpio1", "/sys/class/gpio/export", "/sys/class/gpio/unexport"})
}

func (s *SnapTestSuite) TestRemoveHWAccessInvalidDevice(c *C) {
	err := RemoveHWAccess("hello-snap", "meep")
	c.Assert(err, Equals, ErrInvalidHWDevice)
}

func (s *SnapTestSuite) TestRemoveHWAccess(c *C) {
	makeInstalledMockSnap("")
	err := AddHWAccess("hello-snap", "/dev/ttyUSB0")

	// check that the udev rules file got created
	udevRulesFilename := "70-snappy_hwassign_hello-snap.rules"
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapUdevRulesDir, udevRulesFilename)), Equals, true)

	writePaths, err := ListHWAccess("hello-snap")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0"})

	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	err = RemoveHWAccess("hello-snap", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, true)

	writePaths, err = ListHWAccess("hello-snap")
	c.Assert(err, IsNil)
	c.Assert(writePaths, HasLen, 0)

	// check that the udev rules file got removed on unassign
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapUdevRulesDir, udevRulesFilename)), Equals, false)

	// check the json.additional got cleaned out
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorAdditionalDir, "hello-snap.hwaccess.yaml"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "{}\n")
}

func (s *SnapTestSuite) TestRemoveHWAccessMultipleDevices(c *C) {
	makeInstalledMockSnap("")

	// setup
	err := AddHWAccess("hello-snap", "/dev/bar")
	AddHWAccess("hello-snap", "/dev/bar*")
	// ensure its there
	writePaths, _ := ListHWAccess("hello-snap")
	c.Assert(writePaths, DeepEquals, []string{"/dev/bar", "/dev/bar*"})

	// check the file only lists udevReadGlob once
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorAdditionalDir, "hello-snap.hwaccess.yaml"))
	c.Assert(err, IsNil)
	c.Assert("\n"+string(content), Equals, `
read-paths:
- /run/udev/data/*
write-paths:
- /dev/bar
- /dev/bar*
`)

	// check the udev rule file contains all the rules
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapUdevRulesDir, "70-snappy_hwassign_hello-snap.rules"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals,
		`KERNEL=="bar", TAG:="snappy-assign", ENV{SNAPPY_APP}:="hello-snap.hello"
KERNEL=="bar", TAG:="snappy-assign", ENV{SNAPPY_APP}:="hello-snap.svc1"
KERNEL=="bar*", TAG:="snappy-assign", ENV{SNAPPY_APP}:="hello-snap.hello"
KERNEL=="bar*", TAG:="snappy-assign", ENV{SNAPPY_APP}:="hello-snap.svc1"
`)
	// remove
	err = RemoveHWAccess("hello-snap", "/dev/bar")
	c.Assert(err, IsNil)

	// ensure the right thing was removed
	writePaths, _ = ListHWAccess("hello-snap")
	c.Assert(writePaths, DeepEquals, []string{"/dev/bar*"})

	// check udevReadGlob is still there
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorAdditionalDir, "hello-snap.hwaccess.yaml"))
	c.Assert(err, IsNil)
	c.Assert("\n"+string(content), Equals, `
read-paths:
- /run/udev/data/*
write-paths:
- /dev/bar*
`)
	// check the udevReadGlob Udev rule is still there
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapUdevRulesDir, "70-snappy_hwassign_hello-snap.rules"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals,
		`KERNEL=="bar*", TAG:="snappy-assign", ENV{SNAPPY_APP}:="hello-snap.hello"
KERNEL=="bar*", TAG:="snappy-assign", ENV{SNAPPY_APP}:="hello-snap.svc1"
`)
}

func makeRunUdevAdmMock(a *[][]string) func(args ...string) error {
	return func(args ...string) error {
		*a = append(*a, args)
		return nil
	}
}

func verifyUdevAdmActivateRules(c *C, runUdevAdmCalls [][]string) {
	c.Assert(runUdevAdmCalls, HasLen, 2)
	c.Assert(runUdevAdmCalls[0], DeepEquals, []string{"udevadm", "control", "--reload-rules"})
	c.Assert(runUdevAdmCalls[1], DeepEquals, []string{"udevadm", "trigger"})
}

func (s *SnapTestSuite) TestRemoveHWAccessFail(c *C) {
	var runUdevAdmCalls [][]string
	runUdevAdm = makeRunUdevAdmMock(&runUdevAdmCalls)

	makeInstalledMockSnap("")
	err := AddHWAccess("hello-snap", "/dev/ttyUSB0")
	c.Assert(err, IsNil)

	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	err = RemoveHWAccess("hello-snap", "/dev/something")
	c.Assert(err, Equals, ErrHWAccessRemoveNotFound)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
	verifyUdevAdmActivateRules(c, runUdevAdmCalls)
}

func (s *SnapTestSuite) TestWriteUdevRulesForDeviceCgroup(c *C) {
	makeInstalledMockSnap(`
name: foo-snap
version: 1.0
apps:
  app:
   command: cmd
`)
	var runUdevAdmCalls [][]string
	runUdevAdm = makeRunUdevAdmMock(&runUdevAdmCalls)

	snapapp := "foo-snap_meep_1.0"
	err := writeUdevRuleForDeviceCgroup(snapapp, "/dev/ttyS0")
	c.Assert(err, IsNil)

	got, err := ioutil.ReadFile(filepath.Join(dirs.SnapUdevRulesDir, "70-snappy_hwassign_foo-snap.rules"))
	c.Assert(err, IsNil)
	c.Assert(string(got), Equals, `KERNEL=="ttyS0", TAG:="snappy-assign", ENV{SNAPPY_APP}:="foo-snap.app"
`)

	verifyUdevAdmActivateRules(c, runUdevAdmCalls)
}

func (s *SnapTestSuite) TestRemoveAllHWAccess(c *C) {
	makeInstalledMockSnap("")

	err := AddHWAccess("hello-snap", "/dev/ttyUSB0")
	c.Assert(err, IsNil)

	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	c.Check(*regenerateAppArmorRulesWasCalled, Equals, false)
	c.Check(RemoveAllHWAccess("hello-snap"), IsNil)

	c.Check(osutil.FileExists(filepath.Join(dirs.SnapUdevRulesDir, "70-snappy_hwassign_foo-app.rules")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapAppArmorAdditionalDir, "hello-snap.hwaccess.yaml")), Equals, false)
	c.Check(*regenerateAppArmorRulesWasCalled, Equals, true)
}

func (s *SnapTestSuite) TestAddSysDevice(c *C) {
	makeInstalledMockSnap("")
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()

	err := AddHWAccess("hello-snap", "/sys/devices/foo1")
	c.Assert(err, IsNil)
	err = AddHWAccess("hello-snap", "/sys/class/gpio/foo2")
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorAdditionalDir, "hello-snap.hwaccess.yaml"))
	c.Assert(err, IsNil)
	c.Assert("\n"+string(content), Equals, `
read-paths:
- /run/udev/data/*
write-paths:
- /sys/devices/foo1
- /sys/class/gpio/foo2
`)
	// ensure that no udev rule has been generated
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapUdevRulesDir, "70-snappy_hwassign_hello-snap.rules"))
	c.Assert(content, IsNil)

	// ensure the regenerate code was called
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, true)
}
