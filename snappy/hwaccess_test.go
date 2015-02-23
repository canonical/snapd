package snappy

import (
	"io/ioutil"
	"path/filepath"

	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestAddHWAccessSimple(c *C) {
	aaClickHookCmd = "true"
	makeMockSnap(s.tempdir)

	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)
	content, err := ioutil.ReadFile(filepath.Join(snapAppArmorDir, "hello-app.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
  "write_path": [
    "/dev/ttyUSB0"
  ]
}`)

}

func (s *SnapTestSuite) TestAddHWAccessInvalidDevice(c *C) {
	aaClickHookCmd = "true"
	makeMockSnap(s.tempdir)

	err := AddHWAccess("hello-app", "ttyUSB0")
	c.Assert(err, Equals, ErrInvalidHWDevice)
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
	err := AddHWAccess("xxx", "/dev/ttyUSB0")
	c.Assert(err, Equals, ErrPackageNotFound)
}

func (s *SnapTestSuite) TestAddHWAccessHookFails(c *C) {
	aaClickHookCmd = "false"
	makeMockSnap(s.tempdir)

	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err.Error(), Equals, "exit status 1")
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
	makeMockSnap(s.tempdir)
	err := AddHWAccess("hello-app", "/dev/ttyUSB0")

	writePaths, err := ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{"/dev/ttyUSB0"})

	err = RemoveHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)

	writePaths, err = ListHWAccess("hello-app")
	c.Assert(err, IsNil)
	c.Assert(writePaths, DeepEquals, []string{})
}

func (s *SnapTestSuite) TestRemoveHWAccessFail(c *C) {
	makeMockSnap(s.tempdir)
	err := AddHWAccess("hello-app", "/dev/ttyUSB0")
	c.Assert(err, IsNil)

	err = RemoveHWAccess("hello-app", "/dev/something")
	c.Assert(err.Error(), Equals, "Can not find '/dev/something' access for 'hello-app'")
}
