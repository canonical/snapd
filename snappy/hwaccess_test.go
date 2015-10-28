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
	"github.com/ubuntu-core/snappy/helpers"
)

func mockRegenerateAppArmorRules() *bool {
	regenerateAppArmorRulesWasCalled := false
	regenerateAppArmorRules = func() error {
		regenerateAppArmorRulesWasCalled = true
		return nil
	}
	return &regenerateAppArmorRulesWasCalled
}

func (s *SnapTestSuite) TestAddHWAccessSimple(c *C) {
	makeInstalledMockSnap(s.tempdir, "")
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()

	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorDir, "hello-app.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
  "write_path": [
    "/dev/ttyUSB0"
  ],
  "read_path": [
    "/run/udev/data/*"
  ]
}
`)
	// ensure the regenerate code was called
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, true)
}

func (s *SnapTestSuite) TestAddHWAccessInvalidDevice(c *C) {
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	makeInstalledMockSnap(s.tempdir, "")

	err := AddHWAccess("hello-app", "ttyUSB0")
	c.Assert(err, Equals, ErrInvalidHWDevice)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
}

func (s *SnapTestSuite) TestAddHWAccessMultiplePaths(c *C) {
	aaClickHookCmd = "true"
	makeInstalledMockSnap(s.tempdir, "")

	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	err = AddHWAccess("hello-app", "/sys/devices/gpio1")
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorDir, "hello-app.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
  "write_path": [
    "/dev/ttyUSB0",
    "/sys/devices/gpio1"
  ],
  "read_path": [
    "/run/udev/data/*"
  ]
}
`)

}

func (s *SnapTestSuite) TestAddHWAccessAddSameDeviceTwice(c *C) {
	aaClickHookCmd = "true"
	makeInstalledMockSnap(s.tempdir, "")

	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	err = AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, Equals, ErrHWAccessAlreadyAdded)

	writePaths, err := ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0"})
}

func (s *SnapTestSuite) TestAddHWAccessUnknownPackage(c *C) {
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()

	err := AddHWAccess("xxx", "/dev/ttyUSB0")
	c.Assert(err, Equals, ErrPackageNotFound)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
}

func (s *SnapTestSuite) TestAddHWAccessHookFails(c *C) {
	aaClickHookCmd = "false"
	makeInstalledMockSnap(s.tempdir, "")

	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err.Error(), Equals, "apparmor generate fails with 1: ''")
}

func (s *SnapTestSuite) TestListHWAccessNoAdditionalAccess(c *C) {
	makeInstalledMockSnap(s.tempdir, "")

	writePaths, err := ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, HasLen, 0)
}

func (s *SnapTestSuite) TestListHWAccess(c *C) {
	makeInstalledMockSnap(s.tempdir, "")
	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)

	err = AddHWAccess("hello-app", "/sys/devices/gpio1")
	c.Assert(err, IsNil)

	err = AddHWAccess("hello-app", "/sys/class/gpio/export")
	c.Assert(err, IsNil)

	err = AddHWAccess("hello-app", "/sys/class/gpio/unexport")
	c.Assert(err, IsNil)

	writePaths, err := ListHWAccess("hello-app")
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0", "/sys/devices/gpio1", "/sys/class/gpio/export", "/sys/class/gpio/unexport"})
}

func (s *SnapTestSuite) TestRemoveHWAccessInvalidDevice(c *C) {
	err := RemoveHWAccess("hello-app", "meep")
	c.Assert(err, Equals, ErrInvalidHWDevice)
}

func (s *SnapTestSuite) TestRemoveHWAccess(c *C) {
	aaClickHookCmd = "true"

	makeInstalledMockSnap(s.tempdir, "")
	err := AddHWAccess("hello-app", "/dev/ttyUSB0")

	// check that the udev rules file got created
	udevRulesFilename := "70-snappy_hwassign_hello-app.rules"
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapUdevRulesDir, udevRulesFilename)), Equals, true)

	writePaths, err := ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0"})

	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	err = RemoveHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, true)

	writePaths, err = ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, HasLen, 0)

	// check that the udev rules file got removed on unassign
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapUdevRulesDir, udevRulesFilename)), Equals, false)

	// check the json.additional got cleaned out
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorDir, "hello-app.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "{}\n")
}

func (s *SnapTestSuite) TestRemoveHWAccessMultipleDevices(c *C) {
	aaClickHookCmd = "true"
	makeInstalledMockSnap(s.tempdir, "")

	// setup
	err := AddHWAccess("hello-app", "/dev/bar")
	AddHWAccess("hello-app", "/dev/bar*")
	// ensure its there
	writePaths, _ := ListHWAccess("hello-app")
	c.Assert(writePaths, DeepEquals, []string{"/dev/bar", "/dev/bar*"})

	// check the file only lists udevReadGlob once
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorDir, "hello-app.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
  "write_path": [
    "/dev/bar",
    "/dev/bar*"
  ],
  "read_path": [
    "/run/udev/data/*"
  ]
}
`)

	// check the udev rule file contains all the rules
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapUdevRulesDir, "70-snappy_hwassign_hello-app.rules"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `
KERNEL=="bar", TAG:="snappy-assign", ENV{SNAPPY_APP}:="hello-app"

KERNEL=="bar*", TAG:="snappy-assign", ENV{SNAPPY_APP}:="hello-app"
`)
	// remove
	err = RemoveHWAccess("hello-app", "/dev/bar")
	c.Assert(err, IsNil)

	// ensure the right thing was removed
	writePaths, _ = ListHWAccess("hello-app")
	c.Assert(writePaths, DeepEquals, []string{"/dev/bar*"})

	// check udevReadGlob is still there
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorDir, "hello-app.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
  "write_path": [
    "/dev/bar*"
  ],
  "read_path": [
    "/run/udev/data/*"
  ]
}
`)
	// check the udevReadGlob Udev rule is still there
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapUdevRulesDir, "70-snappy_hwassign_hello-app.rules"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `KERNEL=="bar*", TAG:="snappy-assign", ENV{SNAPPY_APP}:="hello-app"
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

	makeInstalledMockSnap(s.tempdir, "")
	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)

	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	err = RemoveHWAccess("hello-app", "/dev/something")
	c.Assert(err, Equals, ErrHWAccessRemoveNotFound)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
	verifyUdevAdmActivateRules(c, runUdevAdmCalls)
}

func (s *SnapTestSuite) TestWriteUdevRulesForDeviceCgroup(c *C) {
	var runUdevAdmCalls [][]string
	runUdevAdm = makeRunUdevAdmMock(&runUdevAdmCalls)

	snapapp := "foo-app_meep_1.0"
	err := writeUdevRuleForDeviceCgroup(snapapp, "/dev/ttyS0")
	c.Assert(err, IsNil)

	got, err := ioutil.ReadFile(filepath.Join(dirs.SnapUdevRulesDir, "70-snappy_hwassign_foo-app.rules"))
	c.Assert(err, IsNil)
	c.Assert(string(got), Equals, `
KERNEL=="ttyS0", TAG:="snappy-assign", ENV{SNAPPY_APP}:="foo-app"
`)

	verifyUdevAdmActivateRules(c, runUdevAdmCalls)
}

func (s *SnapTestSuite) TestRemoveAllHWAccess(c *C) {
	makeInstalledMockSnap(s.tempdir, "")

	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)

	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	c.Check(*regenerateAppArmorRulesWasCalled, Equals, false)
	c.Check(RemoveAllHWAccess("hello-app"), IsNil)

	c.Check(helpers.FileExists(filepath.Join(dirs.SnapUdevRulesDir, "70-snappy_hwassign_foo-app.rules")), Equals, false)
	c.Check(helpers.FileExists(filepath.Join(dirs.SnapAppArmorDir, "hello-app.json.additional")), Equals, false)
	c.Check(*regenerateAppArmorRulesWasCalled, Equals, true)
}

func (s *SnapTestSuite) TestRegenerateAppaArmorRulesErr(c *C) {
	script := `#!/bin/sh
echo meep
exit 1`
	mockFailHookFile := filepath.Join(c.MkDir(), "failing-aa-hook")
	err := ioutil.WriteFile(mockFailHookFile, []byte(script), 0755)
	c.Assert(err, IsNil)
	aaClickHookCmd = mockFailHookFile

	err = regenerateAppArmorRulesImpl()
	c.Assert(err, DeepEquals, &ErrApparmorGenerate{
		ExitCode: 1,
		Output:   []byte("meep\n"),
	})
}
