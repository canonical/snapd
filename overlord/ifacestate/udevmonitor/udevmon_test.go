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

type udevMonitorSuite struct {
	testutil.BaseTest
}

var _ = Suite(&udevMonitorSuite{})

func (s *udevMonitorSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	cmd := testutil.MockCommand(c, "udevadm", `echo "udev not mocked in tests"; exit 1`)
	s.AddCleanup(cmd.Restore)
}

func (s *udevMonitorSuite) TestSmoke(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", `exit 0`)
	s.AddCleanup(cmd.Restore)

	mon := udevmonitor.New(nil, nil, nil)
	c.Assert(mon, NotNil)
	c.Assert(mon.Connect(), IsNil)
	c.Assert(mon.Run(), IsNil)
	c.Assert(mon.Stop(), IsNil)
}

func (s *udevMonitorSuite) TestDiscovery(c *C) {
	var addInfos []*hotplug.HotplugDeviceInfo
	var remInfo *hotplug.HotplugDeviceInfo
	var enumerationDone bool

	callbackChannel := make(chan struct{})
	defer close(callbackChannel)

	added := func(inf *hotplug.HotplugDeviceInfo) {
		addInfos = append(addInfos, inf)
		callbackChannel <- struct{}{}
	}
	removed := func(inf *hotplug.HotplugDeviceInfo) {
		// we should see just one removal
		c.Check(remInfo, IsNil)
		remInfo = inf
		callbackChannel <- struct{}{}
	}
	enumerationFinished := func() {
		// enumerationDone is signalled after udevadm parsing ends and before other devices are reported
		c.Assert(addInfos, HasLen, 2)
		c.Check(addInfos[0].DevicePath(), Equals, "/sys/a/path")
		c.Check(addInfos[1].DevicePath(), Equals, "/sys/def")

		enumerationDone = true
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
__END__
`)
	defer cmd.Restore()

	udevmon := udevmonitor.New(added, removed, enumerationFinished).(*udevmonitor.Monitor)
	events := udevmon.EventsChannel()

	c.Assert(udevmon.Run(), IsNil)

	go func() {
		events <- netlink.UEvent{
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
		events <- netlink.UEvent{
			Action: netlink.ADD,
			KObj:   "foo",
			Env: map[string]string{
				"DEVPATH":   "/a/path",
				"SUBSYSTEM": "tty",
				"DEVNAME":   "name",
			},
		}
		events <- netlink.UEvent{
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
	}()

	calls := 0
Loop:
	for {
		select {
		case <-callbackChannel:
			calls++
			if calls == 5 {
				break Loop
			}
		case <-time.After(3 * time.Second):
			c.Error("Did not receive expected devices before timeout")
			break Loop
		}
	}

	c.Check(calls, Equals, 5)
	c.Check(enumerationDone, Equals, true)
	// expect three add events - one from udev event, two from enumeration.
	c.Assert(addInfos, HasLen, 3)
	c.Assert(remInfo, NotNil)

	stopChannel := make(chan struct{})
	defer close(stopChannel)
	go func() {
		c.Assert(udevmon.Stop(), IsNil)
		stopChannel <- struct{}{}
	}()
	select {
	case <-stopChannel:
	case <-time.After(3 * time.Second):
		c.Error("udev monitor did not stop before timeout")
	}

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
