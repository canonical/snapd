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

package udevmonitor_test

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/osutil/udev/netlink"
	"github.com/snapcore/snapd/overlord/ifacestate/udevmonitor"
)

func TestHotplug(t *testing.T) { TestingT(t) }

type udevMonitorSuite struct{}

var _ = Suite(&udevMonitorSuite{})

func (s *udevMonitorSuite) TestSmoke(c *C) {
	mon := udevmonitor.New(nil, nil)
	c.Assert(mon, NotNil)
	c.Assert(mon.Connect(), IsNil)
	c.Assert(mon.Run(), IsNil)
	c.Assert(mon.Stop(), IsNil)
}

func (s *udevMonitorSuite) TestDiscovery(c *C) {
	var addInfo, remInfo *hotplug.HotplugDeviceInfo

	callbackChannel := make(chan struct{})
	defer close(callbackChannel)

	added := func(inf *hotplug.HotplugDeviceInfo) {
		addInfo = inf
		callbackChannel <- struct{}{}
	}
	removed := func(inf *hotplug.HotplugDeviceInfo) {
		remInfo = inf
		callbackChannel <- struct{}{}
	}

	udevmon := udevmonitor.New(added, removed).(*udevmonitor.Monitor)
	events := udevmon.EventsChannel()

	c.Assert(udevmon.Run(), IsNil)

	go func() {
		events <- netlink.UEvent{
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
		events <- netlink.UEvent{
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
	}()

Loop:
	for {
		select {
		case <-callbackChannel:
			if addInfo != nil && remInfo != nil {
				break Loop
			}
		case <-time.After(3 * time.Second):
			c.Error("Did not receive expected devices before timeout")
			break Loop
		default:
		}
	}

	c.Assert(udevmon.Stop(), IsNil)

	c.Assert(addInfo, NotNil)
	c.Assert(remInfo, NotNil)

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
