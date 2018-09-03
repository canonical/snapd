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

package ifacestate_test

import (
	"time"

	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/ifacestate/udevmonitor"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type hotplugSuite struct {
	testutil.BaseTest
	o       *overlord.Overlord
	state   *state.State
	udevMon *udevMonitorMock
}

var _ = Suite(&hotplugSuite{})

func (s *hotplugSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.o = overlord.Mock()
	s.state = s.o.State()

	restoreTimeout := ifacestate.MockUDevInitRetryTimeout(0 * time.Second)
	s.BaseTest.AddCleanup(restoreTimeout)

	s.udevMon = &udevMonitorMock{}
	restoreCreate := ifacestate.MockCreateUDevMonitor(func(add udevmonitor.DeviceAddedFunc, remove udevmonitor.DeviceRemovedFunc) udevmonitor.Interface {
		s.udevMon.AddDevice = add
		s.udevMon.RemoveDevice = remove
		return s.udevMon
	})
	s.BaseTest.AddCleanup(restoreCreate)

	s.state.Lock()
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.hotplug", true)
	tr.Commit()

	s.state.Unlock()
}

func (s *hotplugSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *hotplugSuite) TestHotplugAdd(c *C) {
	mgr, err := ifacestate.Manager(s.state, nil, s.o.TaskRunner(), nil, nil)
	c.Assert(err, IsNil)

	testIface1 := &ifacetest.TestInterface{
		InterfaceName: "test-a",
		HotplugDeviceKeyCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (string, error) {
			return "KEY-1", nil
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
			spec.SetSlot(&hotplug.SlotSpec{
				Name: "hotplugslot-a",
				Attrs: map[string]interface{}{
					"slot-a-attr1": "a",
				},
			})
			return nil
		},
	}
	testIface2 := &ifacetest.TestInterface{
		InterfaceName: "test-b",
		HotplugDeviceKeyCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (string, error) {
			return "KEY-2", nil
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
			spec.SetSlot(&hotplug.SlotSpec{
				Name: "hotplugslot-b",
			})
			return nil
		},
	}
	// 3rd hotplug interface doesn't create a slot (doesn't support the device)
	testIface3 := &ifacetest.TestInterface{
		InterfaceName: "test-c",
		HotplugDeviceKeyCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (string, error) {
			return "KEY-3", nil
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
			return nil
		},
	}
	mgr.Repository().AddInterface(testIface1)
	mgr.Repository().AddInterface(testIface2)
	mgr.Repository().AddInterface(testIface3)

	// single Ensure to have udev monitor created and wired up by interface manager
	c.Assert(mgr.Ensure(), IsNil)

	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":   "a/path",
		"ACTION":    "add",
		"SUBSYSTEM": "foo",
	})
	c.Assert(err, IsNil)
	s.udevMon.AddDevice(di)

	c.Assert(s.o.Settle(5*time.Second), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// make sure slots have been created in the repo
	repo := mgr.Repository()
	ok, err := repo.HasHotplugSlot("KEY-1", "test-a")
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)
	slots := repo.AllSlots("test-a")
	c.Assert(slots, HasLen, 1)
	c.Assert(slots[0].Name, Equals, "hotplugslot-a")
	c.Assert(slots[0].Attrs, DeepEquals, map[string]interface{}{
		"slot-a-attr1": "a",
	})

	ok, err = repo.HasHotplugSlot("KEY-2", "test-b")
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	ok, err = repo.HasHotplugSlot("KEY-3", "test-c")
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, false)
}
