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

package hotplug_test

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/osutil/udev/netlink"
	hp "github.com/snapcore/snapd/overlord/ifacestate/hotplug"
)

func TestHotplug(t *testing.T) { TestingT(t) }

type udevMonitorSuite struct{}

var _ = Suite(&udevMonitorSuite{})

func (s *udevMonitorSuite) TestSmoke(c *C) {
	mon := hp.CreateUDevMonitor(nil, nil)
	c.Assert(mon, NotNil)
	c.Assert(mon.Connect(), IsNil)
	c.Assert(mon.Run(), IsNil)
	c.Assert(mon.Stop(), IsNil)
}

func (s *udevMonitorSuite) TestDiscovery(c *C) {
	var addCalled, removeCalled bool
	var addInfo, remInfo *hotplug.HotplugDeviceInfo
	added := func(inf *hotplug.HotplugDeviceInfo) {
		addCalled = true
		addInfo = inf
	}
	removed := func(inf *hotplug.HotplugDeviceInfo) {
		removeCalled = true
		remInfo = inf
	}
	mon := hp.CreateUDevMonitor(added, removed)
	c.Assert(mon, NotNil)
	udevmon, _ := mon.(*hp.UDevMonitor)

	// stop channels are normally created by netlink crawler/monitor, but since
	// we don't create them with Connect(), they must be mocked.
	mstop := make(chan struct{})

	event := make(chan netlink.UEvent)

	hp.MockUDevMonitorStopChannel(udevmon, mstop)
	hp.MockUDevMonitorChannel(udevmon, event)

	c.Assert(udevmon.Run(), IsNil)

	event <- netlink.UEvent{
		Action: netlink.ADD,
		KObj:   "foo",
		Env: map[string]string{
			"DEVPATH":   "abc",
			"SUBSYSTEM": "usb",
			"MINOR":     "1",
			"MAJOR":     "2",
			"DEVNAME":   "def",
			"DEVTYPE":   "boo",
		},
	}
	event <- netlink.UEvent{
		Action: netlink.REMOVE,
		KObj:   "bar",
		Env: map[string]string{
			"DEVPATH":   "def",
			"SUBSYSTEM": "usb",
			"MINOR":     "3",
			"MAJOR":     "0",
			"DEVNAME":   "ghi",
			"DEVTYPE":   "bzz",
		},
	}
	c.Assert(udevmon.Stop(), IsNil)

	c.Assert(addCalled, Equals, true)
	c.Assert(removeCalled, Equals, true)

	// test that stop channel was closed
	more := true
	timeout := time.After(2 * time.Second)
	select {
	case _, more = <-mstop:
	case <-timeout:
		c.Fatalf("mstop channel was not closed")
	}
	c.Assert(more, Equals, false)

	c.Assert(addInfo.DeviceName(), Equals, "def")
	c.Assert(addInfo.DeviceType(), Equals, "boo")
	c.Assert(addInfo.Subsystem(), Equals, "usb")
	c.Assert(addInfo.DevicePath(), Equals, "/sys/abc")
	c.Assert(addInfo.Major(), Equals, "2")
	c.Assert(addInfo.Minor(), Equals, "1")

	c.Assert(remInfo.DeviceName(), Equals, "ghi")
	c.Assert(remInfo.DeviceType(), Equals, "bzz")
	c.Assert(remInfo.Subsystem(), Equals, "usb")
	c.Assert(remInfo.DevicePath(), Equals, "/sys/def")
	c.Assert(remInfo.Major(), Equals, "0")
	c.Assert(remInfo.Minor(), Equals, "3")
}
