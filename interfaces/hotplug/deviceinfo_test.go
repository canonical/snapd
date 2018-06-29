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

package hotplug

import (
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type hotplugSuite struct {
	testutil.BaseTest
}

var _ = Suite(&hotplugSuite{})

func (s *hotplugSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
}

func (s *hotplugSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

func (s *hotplugSuite) TestBasicProperties(c *C) {
	env := map[string]string{
		"DEVPATH": "/devices/pci0000:00/0000:00:14.0/usb2/2-3", "DEVNAME": "bus/usb/002/003",
		"DEVTYPE": "usb_device",
		"PRODUCT": "1d50/6108/0", "DEVNUM": "003",
		"SEQNUM": "4053",
		"ACTION": "add", "SUBSYSTEM": "usb",
		"MAJOR": "189", "MINOR": "130",
		"TYPE": "0/0/0", "BUSNUM": "002",
	}

	di := NewHotplugDeviceInfo("/devices/pci0000:00/0000:00:14.0/usb2/2-3", env)

	c.Assert(di.Data, DeepEquals, env)
	c.Assert(di.Object(), Equals, "/devices/pci0000:00/0000:00:14.0/usb2/2-3")
	c.Assert(di.DeviceName(), Equals, "bus/usb/002/003")
	c.Assert(di.DeviceType(), Equals, "usb_device")
	c.Assert(di.Path(), Equals, filepath.Join(dirs.SysfsDir, "/devices/pci0000:00/0000:00:14.0/usb2/2-3"))
	c.Assert(di.Subsystem(), Equals, "usb")
	c.Assert(di.Major(), Equals, "189")
	c.Assert(di.Minor(), Equals, "130")
}

func (s *hotplugSuite) TestPropertiesMissing(c *C) {
	env := map[string]string{
		"DEVPATH": "/devices/pci0000:00/0000:00:14.0/usb2/2-3",
		"ACTION":  "add", "SUBSYSTEM": "usb",
	}

	di := NewHotplugDeviceInfo("/devices/pci0000:00/0000:00:14.0/usb2/2-3", env)

	c.Assert(di.Data, DeepEquals, env)
	c.Assert(di.Object(), Equals, "/devices/pci0000:00/0000:00:14.0/usb2/2-3")
	c.Assert(di.DeviceName(), Equals, "")
	c.Assert(di.DeviceType(), Equals, "")
	c.Assert(di.Path(), Equals, filepath.Join(dirs.SysfsDir, "/devices/pci0000:00/0000:00:14.0/usb2/2-3"))
	c.Assert(di.Subsystem(), Equals, "usb")
	c.Assert(di.Major(), Equals, "")
	c.Assert(di.Minor(), Equals, "")
}
