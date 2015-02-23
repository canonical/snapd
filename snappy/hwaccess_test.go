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
	content, err := ioutil.ReadFile(filepath.Join(snapAppArmorDir, "hello-app_hello_1.10.json.additional"))
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

	content, err := ioutil.ReadFile(filepath.Join(snapAppArmorDir, "hello-app_hello_1.10.json.additional"))
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
