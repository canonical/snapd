package snappy

import (
	"io/ioutil"
	"path/filepath"

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
	makeMockSnap(s.tempdir)
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()

	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	content, err := ioutil.ReadFile(filepath.Join(snapAppArmorDir, "hello-app.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
  "write_path": [
    "/dev/ttyUSB0"
  ]
}`)
	// ensure the regenerate code was called
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, true)
}

func (s *SnapTestSuite) TestAddHWAccessInvalidDevice(c *C) {
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	makeMockSnap(s.tempdir)

	err := AddHWAccess("hello-app", "ttyUSB0")
	c.Assert(err, Equals, ErrInvalidHWDevice)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
}

func (s *SnapTestSuite) TestAddHWAccessMultiplePaths(c *C) {
	aaClickHookCmd = "true"
	makeMockSnap(s.tempdir)

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
}`)

}

func (s *SnapTestSuite) TestAddHWAccessUnknownPackage(c *C) {
	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()

	err := AddHWAccess("xxx", "/dev/ttyUSB0")
	c.Assert(err, Equals, ErrPackageNotFound)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
}

func (s *SnapTestSuite) TestAddHWAccessHookFails(c *C) {
	aaClickHookCmd = "false"
	makeMockSnap(s.tempdir)

	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err.Error(), Equals, "exit status 1")
}

func (s *SnapTestSuite) TestListHWAccessNoAdditionalAccess(c *C) {
	makeMockSnap(s.tempdir)

	writePaths, err := ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(len(writePaths), Equals, 0)
}

func (s *SnapTestSuite) TestListHWAccess(c *C) {
	makeMockSnap(s.tempdir)
	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	err = AddHWAccess("hello-app", "/sys/devices/gpio1")

	writePaths, err := ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0", "/sys/devices/gpio1"})
}

func (s *SnapTestSuite) TestRemoveHWAccess(c *C) {
	aaClickHookCmd = "true"

	makeMockSnap(s.tempdir)
	err := AddHWAccess("hello-app", "/dev/ttyUSB0")

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
}

func (s *SnapTestSuite) TestRemoveHWAccessFail(c *C) {
	makeMockSnap(s.tempdir)
	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)

	regenerateAppArmorRulesWasCalled := mockRegenerateAppArmorRules()
	err = RemoveHWAccess("hello-app", "/dev/something")
	c.Assert(err, Equals, ErrHWAccessRemoveNotFound)
	c.Assert(*regenerateAppArmorRulesWasCalled, Equals, false)
}
