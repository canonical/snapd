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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type hotplugSuite struct {
	testutil.BaseTest
}

var _ = Suite(&hotplugSuite{})

func (s *hotplugSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir("/")

	restore := osutil.MockMountInfo("")
	s.AddCleanup(restore)
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

	di := mylog.Check2(NewHotplugDeviceInfo(env))


	c.Assert(di.DeviceName(), Equals, "bus/usb/002/003")
	c.Assert(di.DeviceType(), Equals, "usb_device")
	c.Assert(di.DevicePath(), Equals, "/sys/devices/pci0000:00/0000:00:14.0/usb2/2-3")
	c.Assert(di.Subsystem(), Equals, "usb")
	c.Assert(di.Major(), Equals, "189")
	c.Assert(di.Minor(), Equals, "130")

	minor, ok := di.Attribute("MINOR")
	c.Assert(ok, Equals, true)
	c.Assert(minor, Equals, "130")

	_, ok = di.Attribute("FOO")
	c.Assert(ok, Equals, false)
}

func (s *hotplugSuite) TestPropertiesMissing(c *C) {
	env := map[string]string{
		"DEVPATH": "/devices/pci0000:00/0000:00:14.0/usb2/2-3",
		"ACTION":  "add", "SUBSYSTEM": "usb",
	}

	_ := mylog.Check2(NewHotplugDeviceInfo(map[string]string{}))
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `missing device path attribute`)

	di := mylog.Check2(NewHotplugDeviceInfo(env))


	c.Assert(di.DeviceName(), Equals, "")
	c.Assert(di.DeviceType(), Equals, "")
	c.Assert(di.DevicePath(), Equals, "/sys/devices/pci0000:00/0000:00:14.0/usb2/2-3")
	c.Assert(di.Subsystem(), Equals, "usb")
	c.Assert(di.Major(), Equals, "")
	c.Assert(di.Minor(), Equals, "")
}

func (s *hotplugSuite) TestStringFormat(c *C) {
	tests := []struct {
		env map[string]string
		out string
	}{
		{
			env: map[string]string{
				"DEVPATH":                 "/devices/a/b/c",
				"DEVNAME":                 "/dev/xyz",
				"ID_VENDOR_FROM_DATABASE": "foo",
				"ID_MODEL_FROM_DATABASE":  "bar",
				"ID_SERIAL":               "999000",
				"ACTION":                  "add",
				"SUBSYSTEM":               "usb",
			},
			out: "/dev/xyz (bar; serial: 999000)",
		},
		{
			env: map[string]string{
				"DEVPATH":         "/devices/a/b/c",
				"ID_SERIAL":       "Foo 999000",
				"ID_SERIAL_SHORT": "999000",
				"ACTION":          "add",
				"SUBSYSTEM":       "usb",
			},
			out: "/sys/devices/a/b/c (serial: Foo 999000)",
		},
		{
			env: map[string]string{
				"DEVPATH":      "/devices/a/b/c",
				"ID_VENDOR_ID": "foo",
				"ID_MODEL_ID":  "bar",
				"ID_SERIAL":    "noserial",
				"ACTION":       "add",
			},
			out: "/sys/devices/a/b/c (bar)",
		},
		{
			env: map[string]string{
				"DEVPATH":                 "/devices/a/b/c",
				"ID_VENDOR_FROM_DATABASE": "very long vendor name abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
				"ID_SERIAL_SHORT":         "123",
				"ACTION":                  "add",
			},
			out: "/sys/devices/a/b/c (very long vendor name abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUV…; serial: 123)",
		},
		{
			env: map[string]string{
				"DEVPATH":                "/devices/a/b/c",
				"ID_MODEL_FROM_DATABASE": "very long model name abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
				"ACTION":                 "add",
				"MAJOR":                  "189", "MINOR": "1",
			},
			out: "/sys/devices/a/b/c (very long model name abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVW…)",
		},
		{
			env: map[string]string{
				"DEVPATH": "/devices/a/b/c",
				"ACTION":  "add",
			},
			out: "/sys/devices/a/b/c",
		},
		{
			env: map[string]string{
				"DEVNAME": "/dev/a",
				"DEVPATH": "/devices/a/b/c",
				"ACTION":  "add",
			},
			out: "/dev/a",
		},
	}

	for _, tst := range tests {
		di := mylog.Check2(NewHotplugDeviceInfo(tst.env))


		c.Check(di.String(), Equals, tst.out)
	}
}

func (s *hotplugSuite) TestShortStringFormat(c *C) {
	di := mylog.Check2(NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":                 "/devices/a",
		"ID_VENDOR_FROM_DATABASE": "very long vendor name",
		"ACTION":                  "add",
	}))

	c.Check(di.ShortString(), Equals, "/sys/devices/a (very long vendor…)")
}
