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
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/ifacestate/udevmonitor"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type hotplugSuite struct {
	testutil.BaseTest
	AssertsMock

	o           *overlord.Overlord
	state       *state.State
	secBackend  *ifacetest.TestSecurityBackend
	mockSnapCmd *testutil.MockCmd

	udevMon               *udevMonitorMock
	mgr                   *ifacestate.InterfaceManager
	handledByGadgetCalled int

	ifaceTestAAutoConnect bool
}

type hotplugTasksWitness struct {
	seenHooks              map[string]string
	seenHotplugRemoveKeys  map[snap.HotplugKey]string
	seenHotplugConnectKeys map[snap.HotplugKey]string
	seenHotplugUpdateKeys  map[snap.HotplugKey]string
	seenHotplugAddKeys     map[snap.HotplugKey]string
	seenTasks              map[string]int
	hotplugDisconnects     map[snap.HotplugKey]string
	connects               []string
	disconnects            []string
}

func (w *hotplugTasksWitness) checkTasks(c *C, st *state.State) {
	w.seenTasks = make(map[string]int)
	w.seenHotplugRemoveKeys = make(map[snap.HotplugKey]string)
	w.seenHotplugConnectKeys = make(map[snap.HotplugKey]string)
	w.seenHotplugUpdateKeys = make(map[snap.HotplugKey]string)
	w.seenHotplugAddKeys = make(map[snap.HotplugKey]string)
	w.hotplugDisconnects = make(map[snap.HotplugKey]string)
	w.seenHooks = make(map[string]string)
	for _, t := range st.Tasks() {
		c.Check(t.Status(), Equals, state.DoneStatus)
		if t.Kind() == "run-hook" {
			var hookSup hookstate.HookSetup
			c.Assert(t.Get("hook-setup", &hookSup), IsNil)
			_, ok := w.seenHooks[hookSup.Hook]
			c.Assert(ok, Equals, false)
			w.seenHooks[hookSup.Hook] = hookSup.Snap
			continue
		}
		w.seenTasks[t.Kind()]++
		if t.Kind() == "connect" || t.Kind() == "disconnect" {
			var plugRef interfaces.PlugRef
			var slotRef interfaces.SlotRef
			c.Assert(t.Get("plug", &plugRef), IsNil)
			c.Assert(t.Get("slot", &slotRef), IsNil)
			if t.Kind() == "connect" {
				w.connects = append(w.connects, fmt.Sprintf("%s %s", plugRef, slotRef))
			} else {
				testByHotplugTaskFlag(c, t)
				w.disconnects = append(w.disconnects, fmt.Sprintf("%s %s", plugRef, slotRef))
			}
			continue
		}

		if t.Kind() == "hotplug-seq-wait" {
			continue
		}

		iface, key := mylog.Check3(ifacestate.GetHotplugAttrs(t))
		c.Check(err, IsNil)

		switch {
		case t.Kind() == "hotplug-add-slot":
			w.seenHotplugAddKeys[key] = iface
		case t.Kind() == "hotplug-connect":
			w.seenHotplugConnectKeys[key] = iface
		case t.Kind() == "hotplug-update-slot":
			w.seenHotplugUpdateKeys[key] = iface
		case t.Kind() == "hotplug-remove-slot":
			w.seenHotplugRemoveKeys[key] = iface
		case t.Kind() == "hotplug-disconnect":
			w.hotplugDisconnects[key] = iface
		}
	}
}

var _ = Suite(&hotplugSuite{})

func (s *hotplugSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.secBackend = &ifacetest.TestSecurityBackend{}
	s.BaseTest.AddCleanup(ifacestate.MockSecurityBackends([]interfaces.SecurityBackend{s.secBackend}))

	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapSystemKeyFile), 0755), IsNil)

	restore := osutil.MockMountInfo("")
	s.AddCleanup(restore)

	s.o = overlord.Mock()
	s.state = s.o.State()

	s.mockSnapCmd = testutil.MockCommand(c, "snap", "")

	s.SetupAsserts(c, s.state, &s.BaseTest)

	restoreTimeout := ifacestate.MockUDevInitRetryTimeout(0 * time.Second)
	s.BaseTest.AddCleanup(restoreTimeout)

	s.udevMon = &udevMonitorMock{}
	restoreCreate := ifacestate.MockCreateUDevMonitor(func(add udevmonitor.DeviceAddedFunc, remove udevmonitor.DeviceRemovedFunc, done udevmonitor.EnumerationDoneFunc) udevmonitor.Interface {
		s.udevMon.AddDevice = add
		s.udevMon.RemoveDevice = remove
		s.udevMon.EnumerationDone = done
		return s.udevMon
	})
	s.BaseTest.AddCleanup(restoreCreate)

	// mock core snap
	si := &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
	snaptest.MockSnapInstance(c, "", coreSnapYaml, si)
	s.state.Lock()
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.hotplug", true)
	tr.Commit()

	s.state.Unlock()

	hookMgr := mylog.Check2(hookstate.Manager(s.state, s.o.TaskRunner()))

	s.o.AddManager(hookMgr)

	s.mgr = mylog.Check2(ifacestate.Manager(s.state, hookMgr, s.o.TaskRunner(), nil, nil))


	s.o.AddManager(s.mgr)
	s.o.AddManager(s.o.TaskRunner())
	mylog.

		// startup
		Check(s.o.StartUp())


	autoConnectNo := func(*snap.PlugInfo, *snap.SlotInfo) bool {
		return false
	}
	s.ifaceTestAAutoConnect = false
	testAAutoConnect := func(*snap.PlugInfo, *snap.SlotInfo) bool {
		return s.ifaceTestAAutoConnect
	}

	testIface1 := &ifacetest.TestHotplugInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName:       "test-a",
			AutoConnectCallback: testAAutoConnect,
		},
		HotplugKeyCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (snap.HotplugKey, error) {
			return "key-1", nil
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (*hotplug.ProposedSlot, error) {
			return &hotplug.ProposedSlot{
				Name: "hotplugslot-a",
				Attrs: map[string]interface{}{
					"slot-a-attr1": "a",
					"path":         deviceInfo.DevicePath(),
				},
			}, nil
		},
	}
	testIface2 := &ifacetest.TestHotplugInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName:       "test-b",
			AutoConnectCallback: autoConnectNo,
		},
		HotplugKeyCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (snap.HotplugKey, error) {
			return "key-2", nil
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (*hotplug.ProposedSlot, error) {
			return &hotplug.ProposedSlot{Name: "hotplugslot-b"}, nil
		},
		HandledByGadgetCallback: func(di *hotplug.HotplugDeviceInfo, slot *snap.SlotInfo) bool {
			s.handledByGadgetCalled++
			var path string
			slot.Attr("path", &path)
			return di.DeviceName() == path
		},
	}
	// 3rd hotplug interface doesn't create hotplug slot (to simulate a case where doesn't device is not supported)
	testIface3 := &ifacetest.TestHotplugInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName:       "test-c",
			AutoConnectCallback: autoConnectNo,
		},
		HotplugKeyCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (snap.HotplugKey, error) {
			return "key-3", nil
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (*hotplug.ProposedSlot, error) {
			return nil, nil
		},
	}
	// 3rd hotplug interface will only create a slot if default hotplug key can be computed
	testIface4 := &ifacetest.TestHotplugInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName:       "test-d",
			AutoConnectCallback: autoConnectNo,
		},
		HotplugDeviceDetectedCallback: func(deviceInfo *hotplug.HotplugDeviceInfo) (*hotplug.ProposedSlot, error) {
			return &hotplug.ProposedSlot{Name: "hotplugslot-d"}, nil
		},
	}

	for _, iface := range []interfaces.Interface{testIface1, testIface2, testIface3, testIface4} {
		c.Assert(s.mgr.Repository().AddInterface(iface), IsNil)
		s.AddCleanup(builtin.MockInterface(iface))
	}

	// single Ensure to have udev monitor created and wired up by interface manager
	c.Assert(s.mgr.Ensure(), IsNil)
}

func (s *hotplugSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
	s.mockSnapCmd.Restore()
}

func testHotplugTaskAttrs(c *C, t *state.Task, ifaceName, hotplugKey string) {
	iface, key := mylog.Check3(ifacestate.GetHotplugAttrs(t))

	c.Assert(key, Equals, snap.HotplugKey(hotplugKey))
	c.Assert(iface, Equals, ifaceName)
}

func testByHotplugTaskFlag(c *C, t *state.Task) {
	var byHotplug bool
	c.Assert(t.Get("by-hotplug", &byHotplug), IsNil)
	c.Assert(byHotplug, Equals, true)
}

func (s *hotplugSuite) TestHotplugAddBasic(c *C) {
	s.MockModel(c, nil)

	di := mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "a/path", "ACTION": "add", "SUBSYSTEM": "foo"}))

	s.udevMon.AddDevice(di)

	c.Assert(s.o.Settle(5*time.Second), IsNil)

	st := s.state
	st.Lock()
	defer st.Unlock()

	var hp hotplugTasksWitness
	hp.checkTasks(c, st)
	c.Check(hp.seenTasks, DeepEquals, map[string]int{"hotplug-seq-wait": 2, "hotplug-add-slot": 2, "hotplug-connect": 2})
	c.Check(hp.seenHotplugAddKeys, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a", "key-2": "test-b"})
	c.Check(hp.seenHotplugConnectKeys, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a", "key-2": "test-b"})
	c.Check(hp.seenHooks, HasLen, 0)
	c.Check(hp.connects, HasLen, 0)

	// make sure slots have been created in the repo
	repo := s.mgr.Repository()
	slot := mylog.Check2(repo.SlotForHotplugKey("test-a", "key-1"))

	c.Assert(slot, NotNil)
	slots := repo.AllSlots("test-a")
	c.Assert(slots, HasLen, 1)
	c.Check(slots[0].Name, Equals, "hotplugslot-a")
	c.Check(slots[0].Attrs, DeepEquals, map[string]interface{}{
		"path":         di.DevicePath(),
		"slot-a-attr1": "a",
	})
	c.Check(slots[0].HotplugKey, DeepEquals, snap.HotplugKey("key-1"))

	slot = mylog.Check2(repo.SlotForHotplugKey("test-b", "key-2"))

	c.Assert(slot, NotNil)

	slot = mylog.Check2(repo.SlotForHotplugKey("test-c", "key-3"))

	c.Assert(slot, IsNil)

	c.Check(s.handledByGadgetCalled, Equals, 0)
}

func (s *hotplugSuite) TestHotplugConnectWithGadgetSlot(c *C) {
	s.MockModel(c, map[string]interface{}{
		"gadget": "the-gadget",
	})

	st := s.state
	st.Lock()
	defer st.Unlock()

	gadgetSideInfo := &snap.SideInfo{RealName: "the-gadget", SnapID: "the-gadget-id", Revision: snap.R(1)}
	gadgetInfo := snaptest.MockSnap(c, `
name: the-gadget
type: gadget
version: 1.0

slots:
  slot1:
    interface: test-b
    path: /dev/path
`, gadgetSideInfo)
	snapstate.Set(s.state, "the-gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&gadgetInfo.SideInfo}),
		Current:  snap.R(1),
		SnapType: "gadget",
	})
	st.Unlock()

	di := mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVNAME":   "/dev/path",
		"DEVPATH":   "a/path",
		"ACTION":    "add",
		"SUBSYSTEM": "foo",
	}))

	s.udevMon.AddDevice(di)

	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()

	c.Check(s.handledByGadgetCalled, Equals, 1)

	// make sure hotplug slot has been created in the repo
	repo := s.mgr.Repository()
	slot := mylog.Check2(repo.SlotForHotplugKey("test-a", "key-1"))

	c.Assert(slot, NotNil)

	// but no hotplug slot has been created for the device path defined by gadget
	slot = mylog.Check2(repo.SlotForHotplugKey("test-b", "key-2"))

	c.Assert(slot, IsNil)
}

func (s *hotplugSuite) TestHotplugAddWithDefaultKey(c *C) {
	s.MockModel(c, nil)

	di := mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":         "a/path",
		"ACTION":          "add",
		"SUBSYSTEM":       "foo",
		"ID_VENDOR_ID":    "vendor",
		"ID_MODEL_ID":     "model",
		"ID_SERIAL_SHORT": "serial",
	}))

	s.udevMon.AddDevice(di)

	c.Assert(s.o.Settle(5*time.Second), IsNil)

	st := s.state
	st.Lock()
	defer st.Unlock()

	var hp hotplugTasksWitness
	hp.checkTasks(c, st)
	c.Check(hp.seenTasks, DeepEquals, map[string]int{"hotplug-seq-wait": 3, "hotplug-add-slot": 3, "hotplug-connect": 3})
	c.Check(hp.seenHooks, HasLen, 0)
	c.Check(hp.connects, HasLen, 0)
	testIfaceDkey := keyHelper("ID_VENDOR_ID\x00vendor\x00ID_MODEL_ID\x00model\x00ID_SERIAL_SHORT\x00serial\x00")
	c.Assert(hp.seenHotplugAddKeys, DeepEquals, map[snap.HotplugKey]string{
		"key-1":       "test-a",
		"key-2":       "test-b",
		testIfaceDkey: "test-d",
	})
	c.Assert(hp.seenHotplugConnectKeys, DeepEquals, map[snap.HotplugKey]string{
		"key-1":       "test-a",
		"key-2":       "test-b",
		testIfaceDkey: "test-d",
	})

	// make sure the slot has been created
	repo := s.mgr.Repository()
	slots := repo.AllSlots("test-d")
	c.Assert(slots, HasLen, 1)
	c.Check(slots[0].Name, Equals, "hotplugslot-d")
	c.Check(slots[0].HotplugKey, Equals, testIfaceDkey)
}

func (s *hotplugSuite) TestHotplugAddWithAutoconnect(c *C) {
	s.MockModel(c, nil)

	s.ifaceTestAAutoConnect = true

	repo := s.mgr.Repository()
	st := s.state

	st.Lock()
	// mock the consumer snap/plug
	si := &snap.SideInfo{RealName: "consumer", Revision: snap.R(1)}
	testSnap := snaptest.MockSnapInstance(c, "", testSnapYaml, si)
	c.Assert(testSnap.Plugs, HasLen, 1)
	c.Assert(testSnap.Plugs["plug"], NotNil)
	c.Assert(repo.AddPlug(testSnap.Plugs["plug"]), IsNil)
	snapstate.Set(s.state, "consumer", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	st.Unlock()

	di := mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "a/path", "ACTION": "add", "SUBSYSTEM": "foo"}))

	s.udevMon.AddDevice(di)

	c.Assert(s.o.Settle(5*time.Second), IsNil)
	st.Lock()
	defer st.Unlock()

	// verify hotplug tasks
	var hp hotplugTasksWitness
	hp.checkTasks(c, st)
	c.Check(hp.seenTasks, DeepEquals, map[string]int{"hotplug-seq-wait": 2, "hotplug-add-slot": 2, "hotplug-connect": 2, "connect": 1})
	c.Check(hp.seenHooks, DeepEquals, map[string]string{"prepare-plug-plug": "consumer", "connect-plug-plug": "consumer"})
	c.Check(hp.seenHotplugAddKeys, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a", "key-2": "test-b"})
	c.Check(hp.seenHotplugConnectKeys, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a", "key-2": "test-b"})
	c.Check(hp.connects, DeepEquals, []string{"consumer:plug core:hotplugslot-a"})

	// make sure slots have been created in the repo
	slot := mylog.Check2(repo.SlotForHotplugKey("test-a", "key-1"))

	c.Assert(slot, NotNil)

	conn := mylog.Check2(repo.Connection(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "hotplugslot-a"},
	}))

	c.Assert(conn, NotNil)
}

var testSnapYaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: test-a
hooks:
 prepare-plug-plug:
 connect-plug-plug:
 disconnect-plug-plug:
`

func (s *hotplugSuite) TestHotplugRemove(c *C) {
	st := s.state
	st.Lock()

	st.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":    "test-a",
			"hotplug-key":  "key-1",
			"hotplug-gone": false,
		},
	})
	st.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":         "hotplugslot",
			"interface":    "test-a",
			"hotplug-key":  "key-1",
			"hotplug-gone": false,
		},
	})

	repo := s.mgr.Repository()
	si := &snap.SideInfo{RealName: "consumer", Revision: snap.R(1)}
	testSnap := snaptest.MockSnapInstance(c, "", testSnapYaml, si)
	c.Assert(repo.AddPlug(&snap.PlugInfo{
		Interface: "test-a",
		Name:      "plug",
		Attrs:     map[string]interface{}{},
		Snap:      testSnap,
	}), IsNil)
	snapstate.Set(s.state, "consumer", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	core := mylog.Check2(snapstate.CurrentInfo(s.state, "core"))

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Interface:  "test-a",
		Name:       "hotplugslot",
		Attrs:      map[string]interface{}{},
		Snap:       core,
		HotplugKey: "key-1",
	}), IsNil)

	conn := mylog.Check2(repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "hotplugslot"},
	}, nil, nil, nil, nil, nil))

	c.Assert(conn, NotNil)

	restore := s.mgr.MockObservedDevicePath(filepath.Join(dirs.SysfsDir, "a/path"), "test-a", "key-1")
	defer restore()

	st.Unlock()

	slot, _ := repo.SlotForHotplugKey("test-a", "key-1")
	c.Assert(slot, NotNil)

	di := mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "a/path", "ACTION": "remove", "SUBSYSTEM": "foo"}))

	s.udevMon.RemoveDevice(di)

	c.Assert(s.o.Settle(5*time.Second), IsNil)

	st.Lock()
	defer st.Unlock()

	// verify hotplug tasks
	var hp hotplugTasksWitness
	hp.checkTasks(c, st)
	c.Check(hp.seenHooks, DeepEquals, map[string]string{"disconnect-plug-plug": "consumer"})
	c.Check(hp.seenHotplugRemoveKeys, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a"})
	c.Check(hp.hotplugDisconnects, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a"})
	c.Check(hp.seenTasks, DeepEquals, map[string]int{"hotplug-seq-wait": 1, "hotplug-disconnect": 1, "disconnect": 1, "hotplug-remove-slot": 1})
	c.Check(hp.disconnects, DeepEquals, []string{"consumer:plug core:hotplugslot"})

	slot, _ = repo.SlotForHotplugKey("test-a", "key-1")
	c.Assert(slot, IsNil)

	var newconns map[string]interface{}
	c.Assert(st.Get("conns", &newconns), IsNil)
	c.Assert(newconns, DeepEquals, map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":    "test-a",
			"hotplug-key":  "key-1",
			"hotplug-gone": true,
		},
	})
}

func (s *hotplugSuite) TestHotplugEnumerationDone(c *C) {
	s.MockModel(c, nil)

	st := s.state
	st.Lock()

	// existing connection
	st.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":    "test-a",
			"hotplug-key":  "key-other-device",
			"hotplug-gone": false,
		},
	})

	repo := s.mgr.Repository()

	si := &snap.SideInfo{RealName: "consumer", Revision: snap.R(1)}
	testSnap := snaptest.MockSnapInstance(c, "", testSnapYaml, si)
	c.Assert(repo.AddPlug(&snap.PlugInfo{
		Interface: "test-a",
		Name:      "plug",
		Attrs:     map[string]interface{}{},
		Snap:      testSnap,
	}), IsNil)
	snapstate.Set(s.state, "consumer", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	core := mylog.Check2(snapstate.CurrentInfo(s.state, "core"))

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Interface:  "test-a",
		Name:       "hotplugslot",
		Attrs:      map[string]interface{}{},
		Snap:       core,
		HotplugKey: "key-other-device",
	}), IsNil)

	conn := mylog.Check2(repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "hotplugslot"},
	}, nil, nil, nil, nil, nil))

	c.Assert(conn, NotNil)

	st.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test-a",
			"hotplug-key": "key-other-device",
		},
		"anotherslot": map[string]interface{}{
			"name":        "anotherslot",
			"interface":   "test-a",
			"hotplug-key": "yet-another-device",
		},
	})

	// precondition
	slot, _ := repo.SlotForHotplugKey("test-a", "key-other-device")
	c.Assert(slot, NotNil)

	st.Unlock()

	// new device added; device for existing connection not present when enumeration is finished
	di := mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "a/path", "ACTION": "add", "SUBSYSTEM": "foo"}))

	s.udevMon.AddDevice(di)
	s.udevMon.EnumerationDone()

	c.Assert(s.o.Settle(5*time.Second), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// make sure slots for new device have been created in the repo
	hpslot, _ := repo.SlotForHotplugKey("test-a", "key-1")
	c.Assert(hpslot, NotNil)
	hpslot, _ = repo.SlotForHotplugKey("test-b", "key-2")
	c.Assert(hpslot, NotNil)

	// make sure slots for missing device got disconnected and removed
	hpslot, _ = repo.SlotForHotplugKey("test-a", "key-other-device")
	c.Assert(hpslot, IsNil)

	// and the connection for missing device is marked with hotplug-gone: true;
	// "anotherslot" is removed completely since there was no connection for it.
	var newconns map[string]interface{}
	c.Assert(st.Get("conns", &newconns), IsNil)
	c.Check(newconns, DeepEquals, map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"hotplug-gone": true,
			"hotplug-key":  "key-other-device",
			"interface":    "test-a",
		},
	})

	var newHotplugSlots map[string]interface{}
	c.Assert(st.Get("hotplug-slots", &newHotplugSlots), IsNil)
	c.Check(newHotplugSlots, DeepEquals, map[string]interface{}{
		"hotplugslot-a": map[string]interface{}{
			"interface": "test-a", "hotplug-gone": false, "static-attrs": map[string]interface{}{"slot-a-attr1": "a", "path": di.DevicePath()}, "hotplug-key": "key-1", "name": "hotplugslot-a",
		},
		"hotplugslot-b": map[string]interface{}{
			"name": "hotplugslot-b", "hotplug-gone": false, "interface": "test-b", "hotplug-key": "key-2",
		},
		"hotplugslot": map[string]interface{}{"name": "hotplugslot", "hotplug-gone": true, "interface": "test-a", "hotplug-key": "key-other-device"},
	})
}

func (s *hotplugSuite) TestHotplugDeviceUpdate(c *C) {
	s.MockModel(c, nil)
	st := s.state
	st.Lock()

	// existing connection
	st.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot-a": map[string]interface{}{
			"interface":    "test-a",
			"hotplug-key":  "key-1",
			"hotplug-gone": false,
			"slot-static":  map[string]interface{}{"path": "/path-1"},
		},
	})

	repo := s.mgr.Repository()
	si := &snap.SideInfo{RealName: "consumer", Revision: snap.R(1)}
	testSnap := snaptest.MockSnapInstance(c, "", testSnapYaml, si)
	c.Assert(repo.AddPlug(&snap.PlugInfo{
		Interface: "test-a",
		Name:      "plug",
		Attrs:     map[string]interface{}{},
		Snap:      testSnap,
	}), IsNil)
	snapstate.Set(s.state, "consumer", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	core := mylog.Check2(snapstate.CurrentInfo(s.state, "core"))

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Interface:  "test-a",
		Name:       "hotplugslot-a",
		Attrs:      map[string]interface{}{"path": "/path-1"},
		Snap:       core,
		HotplugKey: "key-1",
	}), IsNil)

	conn := mylog.Check2(repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "hotplugslot-a"},
	}, nil, nil, nil, nil, nil))

	c.Assert(conn, NotNil)

	st.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot-a": map[string]interface{}{
			"name":         "hotplugslot-a",
			"interface":    "test-a",
			"hotplug-key":  "key-1",
			"static-attrs": map[string]interface{}{"path": "/path-1"},
		},
	})
	st.Unlock()

	// simulate device update
	di := mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "a/path", "ACTION": "add", "SUBSYSTEM": "foo"}))

	s.udevMon.AddDevice(di)
	s.udevMon.EnumerationDone()

	c.Assert(s.o.Settle(5*time.Second), IsNil)

	st.Lock()
	defer st.Unlock()

	// verify hotplug tasks
	var hp hotplugTasksWitness
	hp.checkTasks(c, s.state)

	// we see 2 hotplug-connect tasks because of interface test-a and test-b (the latter does nothing as there is no change)
	c.Check(hp.seenTasks, DeepEquals, map[string]int{"hotplug-seq-wait": 2, "hotplug-connect": 2, "hotplug-disconnect": 1, "connect": 1, "disconnect": 1, "hotplug-add-slot": 2, "hotplug-update-slot": 1})
	c.Check(hp.seenHooks, DeepEquals, map[string]string{
		"disconnect-plug-plug": "consumer",
		"prepare-plug-plug":    "consumer",
		"connect-plug-plug":    "consumer",
	})
	c.Check(hp.hotplugDisconnects, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a"})
	c.Check(hp.seenHotplugAddKeys, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a", "key-2": "test-b"})
	c.Check(hp.seenHotplugConnectKeys, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a", "key-2": "test-b"})
	c.Check(hp.seenHotplugUpdateKeys, DeepEquals, map[snap.HotplugKey]string{"key-1": "test-a"})
	c.Check(hp.connects, DeepEquals, []string{"consumer:plug core:hotplugslot-a"})
	c.Check(hp.disconnects, DeepEquals, []string{"consumer:plug core:hotplugslot-a"})

	// make sure slots for new device have been updated in the repo
	slot := mylog.Check2(repo.SlotForHotplugKey("test-a", "key-1"))

	c.Check(slot.Attrs, DeepEquals, map[string]interface{}{"path": di.DevicePath(), "slot-a-attr1": "a"})

	// and the connection attributes have been updated
	var newconns map[string]interface{}
	c.Assert(st.Get("conns", &newconns), IsNil)
	c.Check(newconns, DeepEquals, map[string]interface{}{
		"consumer:plug core:hotplugslot-a": map[string]interface{}{
			"hotplug-key": "key-1",
			"interface":   "test-a",
			"slot-static": map[string]interface{}{"path": di.DevicePath(), "slot-a-attr1": "a"},
		},
	})

	var newHotplugSlots map[string]interface{}
	c.Assert(st.Get("hotplug-slots", &newHotplugSlots), IsNil)
	c.Check(newHotplugSlots["hotplugslot-a"], DeepEquals, map[string]interface{}{
		"interface":    "test-a",
		"static-attrs": map[string]interface{}{"slot-a-attr1": "a", "path": di.DevicePath()},
		"hotplug-key":  "key-1",
		"name":         "hotplugslot-a",
		"hotplug-gone": false,
	})
}

func keyHelper(input string) snap.HotplugKey {
	return snap.HotplugKey(fmt.Sprintf("0%x", sha256.Sum256([]byte(input))))
}

func (s *hotplugSuite) TestDefaultDeviceKey(c *C) {
	di := mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":        "a/path",
		"ACTION":         "add",
		"SUBSYSTEM":      "foo",
		"ID_V4L_PRODUCT": "v4lproduct",
		"NAME":           "name",
		"ID_VENDOR_ID":   "vendor",
		"ID_MODEL_ID":    "model",
		"ID_SERIAL":      "serial",
		"ID_REVISION":    "revision",
	}))

	key := mylog.Check2(ifacestate.DefaultDeviceKey(di, 0))


	// validity check
	c.Check(key, HasLen, 65)
	c.Check(key, Equals, snap.HotplugKey("08bcbdcda3fee3534c0288506d9b75d4e26fe3692a36a11e75d05eac9ebf5ca7d"))
	c.Assert(key, Equals, keyHelper("ID_V4L_PRODUCT\x00v4lproduct\x00ID_VENDOR_ID\x00vendor\x00ID_MODEL_ID\x00model\x00ID_SERIAL\x00serial\x00"))

	di = mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":      "a/path",
		"ACTION":       "add",
		"SUBSYSTEM":    "foo",
		"NAME":         "name",
		"ID_WWN":       "wnn",
		"ID_MODEL_ENC": "modelenc",
		"ID_REVISION":  "revision",
	}))

	key = mylog.Check2(ifacestate.DefaultDeviceKey(di, 0))

	c.Assert(key, Equals, keyHelper("NAME\x00name\x00ID_WWN\x00wnn\x00ID_MODEL_ENC\x00modelenc\x00ID_REVISION\x00revision\x00"))

	di = mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":       "a/path",
		"ACTION":        "add",
		"SUBSYSTEM":     "foo",
		"PCI_SLOT_NAME": "pcislot",
		"ID_MODEL_ENC":  "modelenc",
	}))

	key = mylog.Check2(ifacestate.DefaultDeviceKey(di, 0))
	c.Assert(key, Equals, keyHelper("PCI_SLOT_NAME\x00pcislot\x00ID_MODEL_ENC\x00modelenc\x00"))


	// real device #1 - Lime SDR device
	di = mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVNAME":                 "/dev/bus/usb/002/002",
		"DEVNUM":                  "002",
		"DEVPATH":                 "/devices/pci0000:00/0000:00:14.0/usb2/2-3",
		"DEVTYPE":                 "usb_device",
		"DRIVER":                  "usb",
		"ID_BUS":                  "usb",
		"ID_MODEL":                "LimeSDR-USB",
		"ID_MODEL_ENC":            "LimeSDR-USB",
		"ID_MODEL_FROM_DATABASE":  "Myriad-RF LimeSDR",
		"ID_MODEL_ID":             "6108",
		"ID_REVISION":             "0000",
		"ID_SERIAL":               "Myriad-RF_LimeSDR-USB_0009060B00492E2C",
		"ID_SERIAL_SHORT":         "0009060B00492E2C",
		"ID_USB_INTERFACES":       ":ff0000:",
		"ID_VENDOR":               "Myriad-RF",
		"ID_VENDOR_ENC":           "Myriad-RF",
		"ID_VENDOR_FROM_DATABASE": "OpenMoko, Inc.",
		"ID_VENDOR_ID":            "1d50",
		"MAJOR":                   "189",
		"MINOR":                   "129",
		"PRODUCT":                 "1d50/6108/0",
		"SUBSYSTEM":               "usb",
		"TYPE":                    "0/0/0",
		"USEC_INITIALIZED":        "6125378086 ",
	}))

	key = mylog.Check2(ifacestate.DefaultDeviceKey(di, 0))

	c.Assert(key, Equals, keyHelper("ID_VENDOR_ID\x001d50\x00ID_MODEL_ID\x006108\x00ID_SERIAL\x00Myriad-RF_LimeSDR-USB_0009060B00492E2C\x00"))

	// real device #2 - usb-serial port adapter
	di = mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVLINKS":                       "/dev/serial/by-id/usb-FTDI_FT232R_USB_UART_AH06W0EQ-if00-port0 /dev/serial/by-path/pci-0000:00:14.0-usb-0:2:1.0-port0",
		"DEVNAME":                        "/dev/ttyUSB0",
		"DEVPATH":                        "/devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0/tty/ttyUSB0",
		"ID_BUS":                         "usb",
		"ID_MM_CANDIDATE":                "1",
		"ID_MODEL_ENC":                   "FT232R\x20USB\x20UART",
		"MODEL_FROM_DATABASE":            "FT232 Serial (UART) IC",
		"ID_MODEL_ID":                    "6001",
		"ID_PATH":                        "pci-0000:00:14.0-usb-0:2:1.0",
		"ID_PATH_TAG":                    "pci-0000_00_14_0-usb-0_2_1_0",
		"ID_PCI_CLASS_FROM_DATABASE":     "Serial bus controller",
		"ID_PCI_INTERFACE_FROM_DATABASE": "XHCI",
		"ID_PCI_SUBCLASS_FROM_DATABASE":  "USB controller",
		"ID_REVISION":                    "0600",
		"ID_SERIAL":                      "FTDI_FT232R_USB_UART_AH06W0EQ",
		"ID_SERIAL_SHORT":                "AH06W0EQ",
		"ID_TYPE":                        "generic",
		"ID_USB_DRIVER":                  "ftdi_sio",
		"ID_USB_INTERFACES":              ":ffffff:",
		"ID_USB_INTERFACE_NUM":           "00",
		"ID_VENDOR":                      "FTDI",
		"ID_VENDOR_ENC":                  "FTDI",
		"ID_VENDOR_FROM_DATABASE":        "Future Technology Devices International, Ltd",
		"ID_VENDOR_ID":                   "0403",
		"MAJOR":                          "188",
		"MINOR":                          "0",
		"SUBSYSTEM":                      "tty",
		"TAGS":                           ":systemd:",
		"USEC_INITIALIZED":               "6571662103",
	}))

	key = mylog.Check2(ifacestate.DefaultDeviceKey(di, 0))

	c.Assert(key, Equals, keyHelper("ID_VENDOR_ID\x000403\x00ID_MODEL_ID\x006001\x00ID_SERIAL\x00FTDI_FT232R_USB_UART_AH06W0EQ\x00"))

	// real device #3 - integrated web camera
	di = mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"COLORD_DEVICE":        "1",
		"COLORD_KIND":          "camera",
		"DEVLINKS":             "/dev/v4l/by-path/pci-0000:00:14.0-usb-0:11:1.0-video-index0 /dev/v4l/by-id/usb-CN0J8NNP7248766FA3H3A01_Integrated_Webcam_HD_200901010001-video-index0",
		"DEVNAME":              "/dev/video0",
		"DEVPATH":              "/devices/pci0000:00/0000:00:14.0/usb1/1-11/1-11:1.0/video4linux/video0",
		"ID_BUS":               "usb",
		"ID_FOR_SEAT":          "video4linux-pci-0000_00_14_0-usb-0_11_1_0",
		"ID_MODEL":             "Integrated_Webcam_HD",
		"ID_MODEL_ENC":         "Integrated_Webcam_HD",
		"ID_MODEL_ID":          "57c3",
		"ID_PATH":              "pci-0000:00:14.0-usb-0:11:1.0",
		"ID_PATH_TAG":          "pci-0000_00_14_0-usb-0_11_1_0",
		"ID_REVISION":          "5806",
		"ID_SERIAL":            "CN0J8NNP7248766FA3H3A01_Integrated_Webcam_HD_200901010001",
		"ID_SERIAL_SHORT":      "200901010001",
		"ID_TYPE":              "video",
		"ID_USB_DRIVER":        "uvcvideo",
		"ID_USB_INTERFACES":    ":0e0100:0e0200:",
		"ID_USB_INTERFACE_NUM": "00",
		"ID_V4L_CAPABILITIES":  ":capture:",
		"ID_V4L_PRODUCT":       "Integrated_Webcam_HD: Integrate",
		"ID_V4L_VERSION":       "2",
		"ID_VENDOR":            "CN0J8NNP7248766FA3H3A01",
		"ID_VENDOR_ENC":        "CN0J8NNP7248766FA3H3A01",
		"ID_VENDOR_ID":         "0bda",
		"MAJOR":                "81",
		"MINOR":                "0",
		"SUBSYSTEM":            "video4linux",
		"TAGS":                 ":uaccess:seat:",
		"USEC_INITIALIZED":     "3411321",
	}))

	key = mylog.Check2(ifacestate.DefaultDeviceKey(di, 0))

	c.Assert(key, Equals, keyHelper("ID_V4L_PRODUCT\x00Integrated_Webcam_HD: Integrate\x00ID_VENDOR_ID\x000bda\x00ID_MODEL_ID\x0057c3\x00ID_SERIAL\x00CN0J8NNP7248766FA3H3A01_Integrated_Webcam_HD_200901010001\x00"))

	// key cannot be computed - empty string
	di = mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":   "a/path",
		"ACTION":    "add",
		"SUBSYSTEM": "foo",
	}))

	key = mylog.Check2(ifacestate.DefaultDeviceKey(di, 0))

	c.Assert(key, Equals, snap.HotplugKey(""))
}

func (s *hotplugSuite) TestDefaultDeviceKeyError(c *C) {
	di := mylog.Check2(hotplug.NewHotplugDeviceInfo(map[string]string{
		"DEVPATH":      "a/path",
		"ACTION":       "add",
		"SUBSYSTEM":    "foo",
		"NAME":         "name",
		"ID_VENDOR_ID": "vendor",
		"ID_MODEL_ID":  "model",
		"ID_SERIAL":    "serial",
	}))

	_ = mylog.Check2(ifacestate.DefaultDeviceKey(di, 16))
	c.Assert(err, ErrorMatches, "internal error: invalid key version 16")
}

func (s *hotplugSuite) TestEnsureUniqueName(c *C) {
	fakeRepositoryLookup := func(n string) bool {
		reserved := map[string]bool{
			"slot1":    true,
			"slot":     true,
			"slot1234": true,
			"slot-1":   true,
		}
		return !reserved[n]
	}

	names := []struct{ proposedName, resultingName string }{
		{"foo", "foo"},
		{"slot", "slot-2"},
		{"slot1234", "slot1234-1"},
		{"slot-1", "slot-1-1"},
	}

	for _, name := range names {
		c.Assert(ifacestate.EnsureUniqueName(name.proposedName, fakeRepositoryLookup), Equals, name.resultingName)
	}
}

func (s *hotplugSuite) TestMakeSlotName(c *C) {
	names := []struct{ proposedName, resultingName string }{
		{"", ""},
		{"-", ""},
		{"slot1", "slot1"},
		{"-slot1", "slot1"},
		{"a--slot-1", "a-slot-1"},
		{"(-slot", "slot"},
		{"(--slot", "slot"},
		{"slot-", "slot"},
		{"slot---", "slot"},
		{"slot-(", "slot"},
		{"Integrated_Webcam_HD", "integratedwebcamhd"},
		{"Xeon E3-1200 v5/E3-1500 v5/6th Gen Core Processor Host Bridge/DRAM Registers", "xeone3-1200v5e3-1500"},
	}
	for _, name := range names {
		c.Assert(ifacestate.MakeSlotName(name.proposedName), Equals, name.resultingName)
	}
}

func (s *hotplugSuite) TestSuggestedSlotName(c *C) {
	events := []struct {
		eventData map[string]string
		outName   string
	}{
		{
			map[string]string{
				"DEVPATH":                "a/path",
				"ACTION":                 "add",
				"SUBSYSTEM":              "foo",
				"NAME":                   "Name",
				"ID_MODEL":               "Longer Name",
				"ID_MODEL_FROM_DATABASE": "Longest Name",
			},
			"name",
		}, {
			map[string]string{
				"DEVPATH":                "a/path",
				"ACTION":                 "add",
				"SUBSYSTEM":              "foo",
				"ID_MODEL":               "Longer Name",
				"ID_MODEL_FROM_DATABASE": "Longest Name",
			},
			"longername",
		}, {
			map[string]string{
				"DEVPATH":                "a/path",
				"ACTION":                 "add",
				"SUBSYSTEM":              "foo",
				"ID_MODEL_FROM_DATABASE": "Longest Name",
			},
			"longestname",
		}, {
			map[string]string{
				"DEVPATH":   "a/path",
				"ACTION":    "add",
				"SUBSYSTEM": "foo",
			},
			"fallbackname",
		},
	}

	for _, data := range events {
		di := mylog.Check2(hotplug.NewHotplugDeviceInfo(data.eventData))


		slotName := ifacestate.SuggestedSlotName(di, "fallbackname")
		c.Assert(slotName, Equals, data.outName)
	}
}

func (s *hotplugSuite) TestHotplugSlotName(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	testData := []struct {
		slotSpecName string
		deviceData   map[string]string
		expectedName string
	}{
		// names dervied from slotSpecName
		{"hdcamera", map[string]string{"DEVPATH": "a", "NAME": "Video Camera"}, "hdcamera"},
		{"hdcamera", map[string]string{"DEVPATH": "a", "NAME": "Video Camera"}, "hdcamera-1"},
		{"ieee1394", map[string]string{"DEVPATH": "a"}, "ieee1394"},
		{"ieee1394", map[string]string{"DEVPATH": "b"}, "ieee1394-1"},
		{"ieee1394", map[string]string{"DEVPATH": "c"}, "ieee1394-2"},
		// names derived from device attributes, since slotSpecName is empty
		{"", map[string]string{"DEVPATH": "a", "NAME": "Video Camera"}, "videocamera"},
		{"", map[string]string{"DEVPATH": "b", "NAME": "Video Camera"}, "videocamera-1"},
		{"", map[string]string{"DEVPATH": "b", "NAME": "Video Camera"}, "videocamera-2"},
		// names derived from interface name, since slotSpecName and relevant device attributes are not present
		{"", map[string]string{"DEVPATH": "a"}, "ifacename"},
		{"", map[string]string{"DEVPATH": "a"}, "ifacename-1"},
	}

	repo := interfaces.NewRepository()
	iface := &ifacetest.TestInterface{InterfaceName: "camera"}
	repo.AddInterface(iface)

	stateSlots := mylog.Check2(ifacestate.GetHotplugSlots(st))


	for _, data := range testData {
		devinfo := mylog.Check2(hotplug.NewHotplugDeviceInfo(data.deviceData))

		c.Check(ifacestate.HotplugSlotName("key", "core", data.slotSpecName, "ifacename", devinfo, repo, stateSlots), Equals, data.expectedName)
		// store the slot to affect ensureUniqueName
		stateSlots[data.expectedName] = &ifacestate.HotplugSlotInfo{}
	}
}

func (s *hotplugSuite) TestUpdateDeviceTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	tss := ifacestate.UpdateDevice(st, "interface", "key", map[string]interface{}{"attr": "value"})
	c.Assert(tss, NotNil)
	c.Assert(tss.Tasks(), HasLen, 2)

	task1 := tss.Tasks()[0]
	c.Assert(task1.Kind(), Equals, "hotplug-disconnect")
	testHotplugTaskAttrs(c, task1, "interface", "key")

	task2 := tss.Tasks()[1]
	c.Assert(task2.Kind(), Equals, "hotplug-update-slot")
	testHotplugTaskAttrs(c, task2, "interface", "key")

	var attrs map[string]interface{}
	c.Assert(task2.Get("slot-attrs", &attrs), IsNil)
	c.Assert(attrs, DeepEquals, map[string]interface{}{"attr": "value"})

	wt := task2.WaitTasks()
	c.Assert(wt, HasLen, 1)
	c.Assert(wt[0], DeepEquals, task1)
}

func (s *hotplugSuite) TestRemoveDeviceTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	tss := ifacestate.RemoveDevice(st, "interface", "key")
	c.Assert(tss, NotNil)
	c.Assert(tss.Tasks(), HasLen, 2)

	task1 := tss.Tasks()[0]
	c.Assert(task1.Kind(), Equals, "hotplug-disconnect")
	testHotplugTaskAttrs(c, task1, "interface", "key")

	task2 := tss.Tasks()[1]
	c.Assert(task2.Kind(), Equals, "hotplug-remove-slot")
	testHotplugTaskAttrs(c, task2, "interface", "key")

	wt := task2.WaitTasks()
	c.Assert(wt, HasLen, 1)
	c.Assert(wt[0], DeepEquals, task1)
}
