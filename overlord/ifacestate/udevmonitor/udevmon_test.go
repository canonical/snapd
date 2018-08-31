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
	"github.com/snapcore/snapd/testutil"
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
	var addCalled, removeCalled bool
	var addInfos []*hotplug.HotplugDeviceInfo
	var remInfo *hotplug.HotplugDeviceInfo

	callbackChannel := make(chan struct{}, 4)
	added := func(inf *hotplug.HotplugDeviceInfo) {
		addCalled = true
		addInfos = append(addInfos, inf)
		callbackChannel <- struct{}{}
	}
	removed := func(inf *hotplug.HotplugDeviceInfo) {
		removeCalled = true
		remInfo = inf
		callbackChannel <- struct{}{}
	}

	cmd := testutil.MockCommand(c, "udevadm", `#!/bin/sh
cat << __END__
P: /a/path
N: name
E: DEVNAME=name
E: foo=bar
E: DEVPATH=/a/path
E: SUBSYSTEM=tty

P: def
N: bar
E: DEVPATH=def
E: SUBSYSTEM=tty
E: MINOR=3
E: MAJOR=0
E: DEVNAME=ghi
E: DEVTYPE=bzz
`)
	defer cmd.Restore()

	udevmon := udevmonitor.New(added, removed).(*udevmonitor.Monitor)

	// stop channels are normally created by netlink crawler/monitor, but since
	// we don't create them with Connect(), they must be mocked.
	mstop := make(chan struct{})
	event := make(chan netlink.UEvent, 3)
	udevmonitor.MockUDevMonitorStopChannel(udevmon, mstop)
	udevmonitor.MockUDevMonitorChannel(udevmon, event)

	c.Assert(udevmon.Run(), IsNil)

	event <- netlink.UEvent{
		Action: netlink.ADD,
		KObj:   "foo",
		Env: map[string]string{
			"DEVPATH":   "abc",
			"SUBSYSTEM": "tty",
			"MINOR":     "1",
			"MAJOR":     "2",
			"DEVNAME":   "def",
			"DEVTYPE":   "boo",
		},
	}
	// the 2nd device will be ignored by de-duplication logic since it's also reported by udevadm mock.
	event <- netlink.UEvent{
		Action: netlink.ADD,
		KObj:   "foo",
		Env: map[string]string{
			"DEVPATH":   "/a/path",
			"SUBSYSTEM": "tty",
			"DEVNAME":   "name",
		},
	}
	event <- netlink.UEvent{
		Action: netlink.REMOVE,
		KObj:   "bar",
		Env: map[string]string{
			"DEVPATH":   "def",
			"SUBSYSTEM": "tty",
			"MINOR":     "3",
			"MAJOR":     "0",
			"DEVNAME":   "ghi",
			"DEVTYPE":   "bzz",
		},
	}

	// expect three add events - one from udev event, two from enumeration.
	const numExpectedDevices = 3
Loop:
	for {
		select {
		case <-callbackChannel:
			if len(addInfos) >= numExpectedDevices && removeCalled == true {
				close(callbackChannel)
				break Loop
			}
		case <-time.After(3 * time.Second):
			c.Error("Did not receive expected devices before timeout")
			break Loop
		}
	}

	c.Assert(addCalled, Equals, true)
	c.Assert(removeCalled, Equals, true)
	c.Assert(addInfos, HasLen, numExpectedDevices)

	c.Assert(udevmon.Stop(), IsNil)

	// test that stop channel was closed
	more := true
	timeout := time.After(2 * time.Second)
	select {
	case _, more = <-mstop:
	case <-timeout:
		c.Fatalf("mstop channel was not closed")
	}
	c.Assert(more, Equals, false)

	addInfo := addInfos[0]
	c.Assert(addInfo.DeviceName(), Equals, "name")
	c.Assert(addInfo.DevicePath(), Equals, "/sys/a/path")
	c.Assert(addInfo.Subsystem(), Equals, "tty")

	addInfo = addInfos[1]
	c.Assert(addInfo.DeviceName(), Equals, "ghi")
	c.Assert(addInfo.DevicePath(), Equals, "/sys/def")
	c.Assert(addInfo.Subsystem(), Equals, "tty")

	addInfo = addInfos[2]
	c.Assert(addInfo.DeviceName(), Equals, "def")
	c.Assert(addInfo.DeviceType(), Equals, "boo")
	c.Assert(addInfo.Subsystem(), Equals, "tty")
	c.Assert(addInfo.DevicePath(), Equals, "/sys/abc")
	c.Assert(addInfo.Major(), Equals, "2")
	c.Assert(addInfo.Minor(), Equals, "1")

	c.Assert(remInfo.DeviceName(), Equals, "ghi")
	c.Assert(remInfo.DeviceType(), Equals, "bzz")
	c.Assert(remInfo.Subsystem(), Equals, "tty")
	c.Assert(remInfo.DevicePath(), Equals, "/sys/def")
	c.Assert(remInfo.Major(), Equals, "0")
	c.Assert(remInfo.Minor(), Equals, "3")
}
