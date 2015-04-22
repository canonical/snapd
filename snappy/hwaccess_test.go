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

	"launchpad.net/snappy/helpers"

	. "launchpad.net/gocheck"
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
	content, err := ioutil.ReadFile(filepath.Join(snapAppArmorDir, "hello-app.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
  "write_path": [
    "/dev/ttyUSB0"
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

	content, err := ioutil.ReadFile(filepath.Join(snapAppArmorDir, "hello-app.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
  "write_path": [
    "/dev/ttyUSB0",
    "/sys/devices/gpio1"
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
	c.Assert(err.Error(), Equals, "exit status 1")
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
	err = AddHWAccess("hello-app", "/sys/devices/gpio1")

	writePaths, err := ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0", "/sys/devices/gpio1"})
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
	c.Assert(helpers.FileExists(filepath.Join(snapUdevRulesDir, udevRulesFilename)), Equals, true)

	writePaths, err := ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0"})

	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	err = RemoveHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, true)

	writePaths, err = ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{})

	// check that the udev rules file got removed on unassign
	c.Assert(helpers.FileExists(filepath.Join(snapUdevRulesDir, udevRulesFilename)), Equals, false)
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

	// remove
	err = RemoveHWAccess("hello-app", "/dev/bar")
	c.Assert(err, IsNil)

	// ensure the right thing was removed
	writePaths, _ = ListHWAccess("hello-app")
	c.Assert(writePaths, DeepEquals, []string{"/dev/bar*"})
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

	got, err := ioutil.ReadFile(filepath.Join(snapUdevRulesDir, "70-snappy_hwassign_foo-app.rules"))
	c.Assert(err, IsNil)
	c.Assert(string(got), Equals, `
KERNEL=="ttyS0", TAG:="snappy-assign", ENV{SNAPPY_APP}:="foo-app"
`)

	verifyUdevAdmActivateRules(c, runUdevAdmCalls)
}
