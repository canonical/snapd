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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/ifacestate/udevmonitor"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type hotplugSuite struct {
	testutil.BaseTest
	o       *overlord.Overlord
	state   *state.State
	udevMon *udevMonitorMock
	mgr     *ifacestate.InterfaceManager
}

var _ = Suite(&hotplugSuite{})

func (s *hotplugSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
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

	hookMgr, err := hookstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(err, IsNil)
	s.o.AddManager(hookMgr)

	s.mgr, err = ifacestate.Manager(s.state, hookMgr, s.o.TaskRunner(), nil, nil)
	c.Assert(err, IsNil)

	testIface1 := &ifacetest.TestInterface{
		InterfaceName: "test-a",
		HotplugDeviceKeyCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (string, error) {
			return "key-1", nil
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
			return spec.SetSlot(&hotplug.SlotSpec{
				Name: "hotplugslot-a",
				Attrs: map[string]interface{}{
					"slot-a-attr1": "a",
				},
			})
		},
	}
	testIface2 := &ifacetest.TestInterface{
		InterfaceName: "test-b",
		HotplugDeviceKeyCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (string, error) {
			return "key-2", nil
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
			return spec.SetSlot(&hotplug.SlotSpec{
				Name: "hotplugslot-b",
			})
		},
	}
	// 3rd hotplug interface doesn't create hotplug slot (to simulate a case where doesn't device is not supported)
	testIface3 := &ifacetest.TestInterface{
		InterfaceName: "test-c",
		HotplugDeviceKeyCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (string, error) {
			return "key-3", nil
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
			return nil
		},
	}
	testIface4 := &ifacetest.TestInterface{
		InterfaceName: "test-d",
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
			return spec.SetSlot(&hotplug.SlotSpec{
				Name: "hotplugslot-d",
			})
		},
	}
	s.mgr.Repository().AddInterface(testIface1)
	s.mgr.Repository().AddInterface(testIface2)
	s.mgr.Repository().AddInterface(testIface3)
	s.mgr.Repository().AddInterface(testIface4)

	s.o.AddManager(s.mgr)
	s.o.AddManager(s.o.TaskRunner())

	// single Ensure to have udev monitor created and wired up by interface manager
	c.Assert(s.mgr.Ensure(), IsNil)
}

func (s *hotplugSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *hotplugSuite) TestHotplugAdd(c *C) {
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
	repo := s.mgr.Repository()
	ok, err := repo.HasHotplugSlot("key-1", "test-a")
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)
	slots := repo.AllSlots("test-a")
	c.Assert(slots, HasLen, 1)
	c.Assert(slots[0].Name, Equals, "hotplugslot-a")
	c.Assert(slots[0].Attrs, DeepEquals, map[string]interface{}{
		"slot-a-attr1": "a",
	})
	c.Assert(slots[0].HotplugDeviceKey, Equals, "key-1")

	ok, err = repo.HasHotplugSlot("key-2", "test-b")
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	ok, err = repo.HasHotplugSlot("key-3", "test-c")
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, false)
}

func (s *hotplugSuite) TestHotplugAddWithDefaultKey(c *C) {
	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":         "a/path",
		"ACTION":          "add",
		"SUBSYSTEM":       "foo",
		"ID_VENDOR_ID":    "vendor",
		"ID_MODEL_ID":     "model",
		"ID_REVISION":     "revision",
		"ID_SERIAL_SHORT": "serial",
	})
	c.Assert(err, IsNil)
	s.udevMon.AddDevice(di)

	c.Assert(s.o.Settle(5*time.Second), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// make sure the slot has been created
	repo := s.mgr.Repository()
	slots := repo.AllSlots("test-d")
	c.Assert(slots, HasLen, 1)
	c.Assert(slots[0].Name, Equals, "hotplugslot-d")
	c.Assert(slots[0].HotplugDeviceKey, Equals, "vendor:model:revision:serial")
}

var testSnapYaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: test-a
`

func (s *hotplugSuite) TestHotplugRemove(c *C) {
	st := s.state
	st.Lock()

	conns := map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":       "test-a",
			"hotplug-key":     "key-1",
			"hotplug-removed": false,
		},
	}
	st.Set("conns", conns)

	repo := s.mgr.Repository()

	si := &snap.SideInfo{Revision: snap.R(1)}
	testSnap := snaptest.MockSnapInstance(c, "", testSnapYaml, si)
	c.Assert(repo.AddPlug(&snap.PlugInfo{
		Interface: "test-a",
		Name:      "plug",
		Attrs:     map[string]interface{}{},
		Snap:      testSnap,
	}), IsNil)
	snapstate.Set(s.state, "consumer", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "app",
	})

	core, err := snapstate.CoreInfo(s.state)
	c.Assert(err, IsNil)
	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Interface:        "test-a",
		Name:             "hotplugslot",
		Attrs:            map[string]interface{}{},
		Snap:             core,
		HotplugDeviceKey: "key-1",
	}), IsNil)

	conn, err := repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "hotplugslot"},
	}, nil, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(conn, NotNil)

	st.Unlock()

	// sanity check
	c.Assert(repo.Slots("core"), HasLen, 1)
	slot, _ := repo.SlotForDeviceKey("key-1", "test-a")
	c.Assert(slot, NotNil)

	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":   "a/path",
		"ACTION":    "add",
		"SUBSYSTEM": "foo",
	})
	c.Assert(err, IsNil)
	s.udevMon.RemoveDevice(di)

	c.Assert(s.o.Settle(5*time.Second), IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(repo.Slots("core"), HasLen, 0)
	slot, _ = repo.SlotForDeviceKey("key-1", "test-a")
	c.Assert(slot, IsNil)

	var newconns map[string]interface{}
	c.Assert(st.Get("conns", &newconns), IsNil)
	c.Assert(newconns, DeepEquals, map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":       "test-a",
			"hotplug-key":     "key-1",
			"hotplug-removed": true,
		}})
}
