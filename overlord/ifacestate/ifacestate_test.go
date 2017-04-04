// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func TestInterfaceManager(t *testing.T) { TestingT(t) }

var (
	rootKey, _  = assertstest.GenerateKey(752)
	storeKey, _ = assertstest.GenerateKey(752)
)

type interfaceManagerSuite struct {
	state           *state.State
	db              *asserts.Database
	privateMgr      *ifacestate.InterfaceManager
	privateHookMgr  *hookstate.HookManager
	extraIfaces     []interfaces.Interface
	extraBackends   []interfaces.SecurityBackend
	secBackend      *ifacetest.TestSecurityBackend
	restoreBackends func()
	mockSnapCmd     *testutil.MockCmd
	storeSigning    *assertstest.StoreStack
}

var _ = Suite(&interfaceManagerSuite{})

func (s *interfaceManagerSuite) SetUpTest(c *C) {
	s.storeSigning = assertstest.NewStoreStack("canonical", rootKey, storeKey)

	s.mockSnapCmd = testutil.MockCommand(c, "snap", "")

	dirs.SetRootDir(c.MkDir())
	state := state.New(nil)
	s.state = state
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	s.db = db
	err = db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	s.state.Lock()
	assertstate.ReplaceDB(state, s.db)
	s.state.Unlock()

	s.privateHookMgr = nil
	s.privateMgr = nil
	s.extraIfaces = nil
	s.extraBackends = nil
	s.secBackend = &ifacetest.TestSecurityBackend{}
	// TODO: transition this so that we don't load real backends and instead
	// just load the test backend here and this is nicely integrated with
	// extraBackends above.
	s.restoreBackends = ifacestate.MockSecurityBackends([]interfaces.SecurityBackend{s.secBackend})
}

func (s *interfaceManagerSuite) TearDownTest(c *C) {
	s.mockSnapCmd.Restore()

	if s.privateMgr != nil {
		s.privateMgr.Stop()
	}
	dirs.SetRootDir("")
	s.restoreBackends()
}

func (s *interfaceManagerSuite) manager(c *C) *ifacestate.InterfaceManager {
	if s.privateMgr == nil {
		mgr, err := ifacestate.Manager(s.state, s.hookManager(c), s.extraIfaces, s.extraBackends)
		c.Assert(err, IsNil)
		mgr.AddForeignTaskHandlers()
		s.privateMgr = mgr
	}
	return s.privateMgr
}

func (s *interfaceManagerSuite) hookManager(c *C) *hookstate.HookManager {
	if s.privateHookMgr == nil {
		mgr, err := hookstate.Manager(s.state)
		c.Assert(err, IsNil)
		s.privateHookMgr = mgr
	}
	return s.privateHookMgr
}

func (s *interfaceManagerSuite) settle(c *C) {
	for i := 0; i < 50; i++ {
		s.hookManager(c).Ensure()
		s.manager(c).Ensure()
		s.hookManager(c).Wait()
		s.manager(c).Wait()
	}
}

func (s *interfaceManagerSuite) TestSmoke(c *C) {
	mgr := s.manager(c)
	mgr.Ensure()
	mgr.Wait()
}

func (s *interfaceManagerSuite) TestConnectTask(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)

	var hs hookstate.HookSetup
	i := 0
	task := ts.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	var hookSetup hookstate.HookSetup
	err = task.Get("hook-setup", &hookSetup)
	c.Assert(err, IsNil)
	c.Assert(hookSetup, Equals, hookstate.HookSetup{Snap: "consumer", Hook: "prepare-plug-plug", Optional: true})
	i++
	task = ts.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	err = task.Get("hook-setup", &hookSetup)
	c.Assert(err, IsNil)
	c.Assert(hookSetup, Equals, hookstate.HookSetup{Snap: "producer", Hook: "prepare-slot-slot", Optional: true})
	i++
	task = ts.Tasks()[i]
	c.Assert(task.Kind(), Equals, "connect")
	var plug interfaces.PlugRef
	err = task.Get("plug", &plug)
	c.Assert(err, IsNil)
	c.Assert(plug.Snap, Equals, "consumer")
	c.Assert(plug.Name, Equals, "plug")
	var slot interfaces.SlotRef
	err = task.Get("slot", &slot)
	c.Assert(err, IsNil)
	c.Assert(slot.Snap, Equals, "producer")
	c.Assert(slot.Name, Equals, "slot")
	// verify initial attributes are present in connect task
	var attrs map[string]interface{}
	err = task.Get("plug-attrs", &attrs)
	c.Assert(err, IsNil)
	c.Assert(attrs["attr1"], Equals, "value1")
	err = task.Get("slot-attrs", &attrs)
	c.Assert(err, IsNil)
	c.Assert(attrs["attr2"], Equals, "value2")
	i++
	task = ts.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	err = task.Get("hook-setup", &hs)
	c.Assert(err, IsNil)
	c.Assert(hs, Equals, hookstate.HookSetup{Snap: "producer", Hook: "connect-slot-slot", Optional: true})
	i++
	task = ts.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	err = task.Get("hook-setup", &hs)
	c.Assert(err, IsNil)
	c.Assert(hs, Equals, hookstate.HookSetup{Snap: "consumer", Hook: "connect-plug-plug", Optional: true})
}

func (s *interfaceManagerSuite) testConnectDisconnectConflicts(c *C, f func(*state.State, string, string, string, string) (*state.TaskSet, error), snapName string) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("other-chg", "...")
	t := s.state.NewTask("link-snap", "...")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName},
	})
	chg.AddTask(t)

	_, err := f(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, ErrorMatches, fmt.Sprintf(`snap "%s" has changes in progress`, snapName))
}

func (s *interfaceManagerSuite) TestConnectConflictsPugSnap(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Connect, "consumer")
}

func (s *interfaceManagerSuite) TestConnectConflictsSlotSnap(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Connect, "producer")
}

func (s *interfaceManagerSuite) TestDisconnectConflictsPugSnap(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Disconnect, "consumer")
}

func (s *interfaceManagerSuite) TestDisconnectConflictsSlotSnap(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Disconnect, "producer")
}

func (s *interfaceManagerSuite) TestEnsureProcessesConnectTask(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	change := s.state.NewChange("kind", "summary")
	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")

	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 5)
	ts.Tasks()[2].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	i := 0
	c.Assert(change.Err(), IsNil)
	task := change.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	c.Check(task.Status(), Equals, state.DoneStatus)
	i++
	task = change.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	c.Check(task.Status(), Equals, state.DoneStatus)
	i++
	task = change.Tasks()[i]
	c.Check(task.Kind(), Equals, "connect")
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(change.Status(), Equals, state.DoneStatus)

	repo := s.manager(c).Repository()
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, HasLen, 1)
	c.Assert(slot.Connections, HasLen, 1)
	c.Check(plug.Connections[0], DeepEquals, interfaces.SlotRef{Snap: "producer", Name: "slot"})
	c.Check(slot.Connections[0], DeepEquals, interfaces.PlugRef{Snap: "consumer", Name: "plug"})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckInterfaceMismatch(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	change := s.state.NewChange("kind", "summary")
	ts, err := ifacestate.Connect(s.state, "consumer", "otherplug", "producer", "slot")
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 5)
	c.Check(ts.Tasks()[2].Kind(), Equals, "connect")
	ts.Tasks()[2].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(change.Err(), ErrorMatches, `cannot perform the following tasks:\n- Connect consumer:otherplug to producer:slot \(cannot connect plug "consumer:otherplug" \(interface "test2"\) to "producer:slot" \(interface "test".*`)
	task := change.Tasks()[2]
	c.Check(task.Kind(), Equals, "connect")
	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(change.Status(), Equals, state.ErrorStatus)
}

func (s *interfaceManagerSuite) TestConnectTaskNoSuchSlot(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	_ = s.state.NewChange("kind", "summary")
	_, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "whatslot")
	c.Assert(err, ErrorMatches, `snap "producer" has no slot named "whatslot"`)
}

func (s *interfaceManagerSuite) TestConnectTaskNoSuchPlug(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	_ = s.state.NewChange("kind", "summary")
	_, err := ifacestate.Connect(s.state, "consumer", "whatplug", "producer", "slot")
	c.Assert(err, ErrorMatches, `snap "consumer" has no plug named "whatplug"`)
}

func (s *interfaceManagerSuite) TestConnectTaskCheckNotAllowed(c *C) {
	s.testConnectTaskCheck(c, func() {
		s.mockSnapDecl(c, "consumer", "consumer-publisher", nil)
		s.mockSnap(c, consumerYaml)
		s.mockSnapDecl(c, "producer", "producer-publisher", nil)
		s.mockSnap(c, producerYaml)
	}, func(change *state.Change) {
		c.Check(change.Err(), ErrorMatches, `(?s).*connection not allowed by slot rule of interface "test".*`)
		c.Check(change.Status(), Equals, state.ErrorStatus)

		repo := s.manager(c).Repository()
		plug := repo.Plug("consumer", "plug")
		slot := repo.Slot("producer", "slot")
		c.Check(plug.Connections, HasLen, 0)
		c.Check(slot.Connections, HasLen, 0)
	})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckNotAllowedButNoDecl(c *C) {
	s.testConnectTaskCheck(c, func() {
		s.mockSnap(c, consumerYaml)
		s.mockSnap(c, producerYaml)
	}, func(change *state.Change) {
		c.Check(change.Err(), IsNil)
		c.Check(change.Status(), Equals, state.DoneStatus)

		repo := s.manager(c).Repository()
		plug := repo.Plug("consumer", "plug")
		slot := repo.Slot("producer", "slot")
		c.Assert(plug.Connections, HasLen, 1)
		c.Check(plug.Connections[0], DeepEquals, interfaces.SlotRef{Snap: "producer", Name: "slot"})
		c.Check(slot.Connections[0], DeepEquals, interfaces.PlugRef{Snap: "consumer", Name: "plug"})
	})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckAllowed(c *C) {
	s.testConnectTaskCheck(c, func() {
		s.mockSnapDecl(c, "consumer", "one-publisher", nil)
		s.mockSnap(c, consumerYaml)
		s.mockSnapDecl(c, "producer", "one-publisher", nil)
		s.mockSnap(c, producerYaml)
	}, func(change *state.Change) {
		c.Assert(change.Err(), IsNil)
		c.Check(change.Status(), Equals, state.DoneStatus)

		repo := s.manager(c).Repository()
		plug := repo.Plug("consumer", "plug")
		slot := repo.Slot("producer", "slot")
		c.Assert(plug.Connections, HasLen, 1)
		c.Check(plug.Connections[0], DeepEquals, interfaces.SlotRef{Snap: "producer", Name: "slot"})
		c.Check(slot.Connections[0], DeepEquals, interfaces.PlugRef{Snap: "consumer", Name: "plug"})
	})
}

func (s *interfaceManagerSuite) testConnectTaskCheck(c *C, setup func(), check func(*state.Change)) {
	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    allow-connection:
      plug-publisher-id:
        - $SLOT_PUBLISHER_ID
`))
	defer restore()
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})

	setup()
	_ = s.manager(c)

	s.state.Lock()
	change := s.state.NewChange("kind", "summary")
	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 5)
	ts.Tasks()[0].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	check(change)
}

func (s *interfaceManagerSuite) TestDisconnectTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := ifacestate.Disconnect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)

	task := ts.Tasks()[0]
	c.Assert(task.Kind(), Equals, "disconnect")
	var plug interfaces.PlugRef
	err = task.Get("plug", &plug)
	c.Assert(err, IsNil)
	c.Assert(plug.Snap, Equals, "consumer")
	c.Assert(plug.Name, Equals, "plug")
	var slot interfaces.SlotRef
	err = task.Get("slot", &slot)
	c.Assert(err, IsNil)
	c.Assert(slot.Snap, Equals, "producer")
	c.Assert(slot.Name, Equals, "slot")
}

// Disconnect works when both plug and slot are specified
func (s *interfaceManagerSuite) TestDisconnectFull(c *C) {
	s.testDisconnect(c, "consumer", "plug", "producer", "slot")
}

// Disconnect works when just the slot is fully specified.
func (s *interfaceManagerSuite) TestDisconnectSlot(c *C) {
	s.testDisconnect(c, "", "", "producer", "slot")
}

// Disconnect works when just the plug is fully specified.
func (s *interfaceManagerSuite) TestDisconnectPlug(c *C) {
	s.testDisconnect(c, "consumer", "plug", "", "")
}

func (s *interfaceManagerSuite) testDisconnect(c *C, plugSnap, plugName, slotSnap, slotName string) {
	// Put two snaps in place They consumer has an plug that can be connected
	// to slot on the producer.
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	// Put a connection in the state so that it automatically gets set up when
	// we create the manager.
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	// Initialize the manager. This registers both snaps and reloads the connection.
	mgr := s.manager(c)

	// Run the disconnect task and let it finish.
	s.state.Lock()
	change := s.state.NewChange("disconnect", "...")
	ts, err := ifacestate.Disconnect(s.state, plugSnap, plugName, slotSnap, slotName)
	ts.Tasks()[0].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	c.Assert(err, IsNil)
	change.AddAll(ts)
	s.state.Unlock()
	mgr.Ensure()
	mgr.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	task := change.Tasks()[0]
	c.Check(task.Kind(), Equals, "disconnect")
	c.Check(task.Status(), Equals, state.DoneStatus)

	c.Check(change.Status(), Equals, state.DoneStatus)

	// Ensure that the connection has been removed from the state
	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, HasLen, 0)

	// Ensure that the connection has been removed from the repository
	repo := mgr.Repository()
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, HasLen, 0)
	c.Assert(slot.Connections, HasLen, 0)

	// Ensure that the backend was used to setup security of both snaps
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Name(), Equals, "producer")

	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) mockIface(c *C, iface interfaces.Interface) {
	s.extraIfaces = append(s.extraIfaces, iface)
}

func (s *interfaceManagerSuite) mockSnapDecl(c *C, name, publisher string, extraHeaders map[string]interface{}) {
	_, err := s.db.Find(asserts.AccountType, map[string]string{
		"account-id": publisher,
	})
	if err == asserts.ErrNotFound {
		acct := assertstest.NewAccount(s.storeSigning, publisher, map[string]interface{}{
			"account-id": publisher,
		}, "")
		err = s.db.Add(acct)
	}
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-name":    name,
		"publisher-id": publisher,
		"snap-id":      (name + strings.Repeat("id", 16))[:32],
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = s.db.Add(snapDecl)
	c.Assert(err, IsNil)
}

func (s *interfaceManagerSuite) mockSnap(c *C, yamlText string) *snap.Info {
	sideInfo := &snap.SideInfo{
		Revision: snap.R(1),
	}
	snapInfo := snaptest.MockSnap(c, yamlText, "", sideInfo)
	sideInfo.RealName = snapInfo.Name()

	a, err := s.db.FindMany(asserts.SnapDeclarationType, map[string]string{
		"snap-name": sideInfo.RealName,
	})
	if err == nil {
		decl := a[0].(*asserts.SnapDeclaration)
		snapInfo.SnapID = decl.SnapID()
		sideInfo.SnapID = decl.SnapID()
	} else if err == asserts.ErrNotFound {
		err = nil
	}
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// Put a side info into the state
	snapstate.Set(s.state, snapInfo.Name(), &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  sideInfo.Revision,
	})
	return snapInfo
}

func (s *interfaceManagerSuite) mockUpdatedSnap(c *C, yamlText string, revision int) *snap.Info {
	sideInfo := &snap.SideInfo{Revision: snap.R(revision)}
	snapInfo := snaptest.MockSnap(c, yamlText, "", sideInfo)
	sideInfo.RealName = snapInfo.Name()

	s.state.Lock()
	defer s.state.Unlock()

	// Put the new revision (stored in SideInfo) into the state
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, snapInfo.Name(), &snapst)
	c.Assert(err, IsNil)
	snapst.Sequence = append(snapst.Sequence, sideInfo)
	snapstate.Set(s.state, snapInfo.Name(), &snapst)

	return snapInfo
}

func (s *interfaceManagerSuite) addSetupSnapSecurityChange(c *C, snapsup *snapstate.SnapSetup) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	task := s.state.NewTask("setup-profiles", "")
	task.Set("snap-setup", snapsup)
	taskset := state.NewTaskSet(task)
	change := s.state.NewChange("test", "")
	change.AddAll(taskset)
	return change
}

func (s *interfaceManagerSuite) addRemoveSnapSecurityChange(c *C, snapName string) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	task := s.state.NewTask("remove-profiles", "")
	snapsup := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
		},
	}
	task.Set("snap-setup", snapsup)
	taskset := state.NewTaskSet(task)
	change := s.state.NewChange("test", "")
	change.AddAll(taskset)
	return change
}

func (s *interfaceManagerSuite) addDiscardConnsChange(c *C, snapName string) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	task := s.state.NewTask("discard-conns", "")
	snapsup := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
		},
	}
	task.Set("snap-setup", snapsup)
	taskset := state.NewTaskSet(task)
	change := s.state.NewChange("test", "")
	change.AddAll(taskset)
	return change
}

var osSnapYaml = `
name: ubuntu-core
version: 1
type: os
`

var sampleSnapYaml = `
name: snap
version: 1
apps:
 app:
   command: foo
plugs:
 network:
  interface: network
`

var consumerYaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: test
  attr1: value1
 otherplug:
  interface: test2
`

var producerYaml = `
name: producer
version: 1
slots:
 slot:
  interface: test
  attr2: value2
`

// The setup-profiles task will not auto-connect an plug that was previously
// explicitly disconnected by the user.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityHonorsDisconnect(c *C) {
	c.Skip("feature disabled until redesign/reimpl")
	// Add an OS snap as well as a sample snap with a "network" plug.
	// The plug is normally auto-connected.
	s.mockSnap(c, osSnapYaml)
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Initialize the manager. This registers the two snaps.
	mgr := s.manager(c)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			Revision: snapInfo.Revision,
		},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that "network" is not saved in the state as auto-connected.
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, HasLen, 0)

	// Ensure that "network" is really disconnected.
	repo := mgr.Repository()
	plug := repo.Plug("snap", "network")
	c.Assert(plug, Not(IsNil))
	c.Check(plug.Connections, HasLen, 0)
}

// The setup-profiles task will auto-connect plugs with viable candidates.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsPlugs(c *C) {
	// Add an OS snap.
	s.mockSnap(c, osSnapYaml)

	// Initialize the manager. This registers the OS snap.
	mgr := s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			Revision: snapInfo.Revision,
		},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that "network" is now saved in the state as auto-connected.
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"snap:network ubuntu-core:network": map[string]interface{}{
			"interface": "network", "auto": true,
		},
	})

	// Ensure that "network" is really connected.
	repo := mgr.Repository()
	plug := repo.Plug("snap", "network")
	c.Assert(plug, Not(IsNil))
	c.Check(plug.Connections, HasLen, 1)
}

// The setup-profiles task will auto-connect plugs with viable candidates.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsSlots(c *C) {
	// Mock the interface that will be used by the test
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	// Add an OS snap.
	s.mockSnap(c, osSnapYaml)
	// Add a consumer snap with unconnect plug (interface "test")
	s.mockSnap(c, consumerYaml)

	// Initialize the manager. This registers the OS snap.
	mgr := s.manager(c)

	// Add a producer snap with a "slot" slot of the "test" interface.
	snapInfo := s.mockSnap(c, producerYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			Revision: snapInfo.Revision,
		},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that "slot" is now saved in the state as auto-connected.
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test", "auto": true,
		},
	})

	// Ensure that "slot" is really connected.
	repo := mgr.Repository()
	slot := repo.Slot("producer", "slot")
	c.Assert(slot, Not(IsNil))
	c.Check(slot.Connections, HasLen, 1)
}

// The setup-profiles task will auto-connect plugs with viable candidates also condidering snap declarations.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBased(c *C) {
	s.testDoSetupSnapSecurityAutoConnectsDeclBased(c, true, func(conns map[string]interface{}, plug *interfaces.Plug) {
		// Ensure that "test" plug is now saved in the state as auto-connected.
		c.Check(conns, DeepEquals, map[string]interface{}{
			"consumer:plug producer:slot": map[string]interface{}{"auto": true, "interface": "test"},
		})
		// Ensure that "test" is really connected.
		c.Check(plug.Connections, HasLen, 1)
	})
}

// The setup-profiles task will *not* auto-connect plugs with viable candidates when snap declarations are missing.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedWhenMissingDecl(c *C) {
	s.testDoSetupSnapSecurityAutoConnectsDeclBased(c, false, func(conns map[string]interface{}, plug *interfaces.Plug) {
		// Ensure nothing is connected.
		c.Check(conns, HasLen, 0)
		c.Check(plug.Connections, HasLen, 0)
	})
}

func (s *interfaceManagerSuite) testDoSetupSnapSecurityAutoConnectsDeclBased(c *C, withDecl bool, check func(map[string]interface{}, *interfaces.Plug)) {
	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    allow-auto-connection:
      plug-publisher-id:
        - $SLOT_PUBLISHER_ID
`))
	defer restore()
	// Add the producer snap
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnapDecl(c, "producer", "one-publisher", nil)
	s.mockSnap(c, producerYaml)

	// Initialize the manager. This registers the producer snap.
	mgr := s.manager(c)

	// Add a sample snap with a plug with the "test" interface which should be auto-connected.
	if withDecl {
		s.mockSnapDecl(c, "consumer", "one-publisher", nil)
	}
	snapInfo := s.mockSnap(c, consumerYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			SnapID:   snapInfo.SnapID,
			Revision: snapInfo.Revision,
		},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)

	repo := mgr.Repository()
	plug := repo.Plug("consumer", "plug")
	c.Assert(plug, Not(IsNil))

	check(conns, plug)
}

// The setup-profiles task will only touch connection state for the task it
// operates on or auto-connects to and will leave other state intact.
func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyKeepsExistingConnectionState(c *C) {
	// Add an OS snap in place.
	s.mockSnap(c, osSnapYaml)

	// Initialize the manager. This registers the two snaps.
	mgr := s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Put fake information about connections for another snap into the state.
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"other-snap:network ubuntu-core:network": map[string]interface{}{
			"interface": "network",
		},
	})
	s.state.Unlock()

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			Revision: snapInfo.Revision,
		},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		// The sample snap was auto-connected, as expected.
		"snap:network ubuntu-core:network": map[string]interface{}{
			"interface": "network", "auto": true,
		},
		// Connection state for the fake snap is preserved.
		// The task didn't alter state of other snaps.
		"other-snap:network ubuntu-core:network": map[string]interface{}{
			"interface": "network",
		},
	})
}

// The setup-profiles task will add implicit slots necessary for the OS snap.
func (s *interfaceManagerSuite) TestDoSetupProfilesAddsImplicitSlots(c *C) {
	// Initialize the manager.
	mgr := s.manager(c)

	// Add an OS snap.
	snapInfo := s.mockSnap(c, osSnapYaml)

	// Run the setup-profiles task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			Revision: snapInfo.Revision,
		},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that we have slots on the OS snap.
	repo := mgr.Repository()
	slots := repo.Slots(snapInfo.Name())
	// NOTE: This is not an exact test as it duplicates functionality elsewhere
	// and is was a pain to update each time. This is correctly handled by the
	// implicit slot tests in snap/implicit_test.go
	c.Assert(len(slots) > 18, Equals, true)
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOnPlugSide(c *C) {
	snapInfo := s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	s.testDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOn(c, snapInfo.Name(), snapInfo.Revision)
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOnSlotSide(c *C) {
	s.mockSnap(c, consumerYaml)
	snapInfo := s.mockSnap(c, producerYaml)
	s.testDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOn(c, snapInfo.Name(), snapInfo.Revision)
}

func (s *interfaceManagerSuite) testDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOn(c *C, snapName string, revision snap.Revision) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	mgr := s.manager(c)

	// Run the setup-profiles task
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			Revision: revision,
		},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	// Change succeeds
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(change.Status(), Equals, state.DoneStatus)

	repo := mgr.Repository()

	// Repository shows the connection
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, HasLen, 1)
	c.Assert(slot.Connections, HasLen, 1)
	c.Check(plug.Connections[0], DeepEquals, interfaces.SlotRef{Snap: "producer", Name: "slot"})
	c.Check(slot.Connections[0], DeepEquals, interfaces.PlugRef{Snap: "consumer", Name: "plug"})
}

// The setup-profiles task will honor snapstate.DevMode flag by storing it
// in the SnapState.Flags and by actually setting up security
// using that flag. Old copy of SnapState.Flag's DevMode is saved for the undo
// handler under `old-devmode`.
func (s *interfaceManagerSuite) TestSetupProfilesHonorsDevMode(c *C) {
	// Put the OS snap in place.
	mgr := s.manager(c)

	// Initialize the manager. This registers the OS snap.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-profiles task and let it finish.
	// Note that the task will see SnapSetup.Flags equal to DeveloperMode.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			Revision: snapInfo.Revision,
		},
		Flags: snapstate.Flags{DevMode: true},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Check(change.Status(), Equals, state.DoneStatus)

	// The snap was setup with DevModeConfinement
	c.Assert(s.secBackend.SetupCalls, HasLen, 1)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, "snap")
	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{DevMode: true})
}

// setup-profiles uses the new snap.Info when setting up security for the new
// snap when it had prior connections and DisconnectSnap() returns it as a part
// of the affected set.
func (s *interfaceManagerSuite) TestSetupProfilesUsesFreshSnapInfo(c *C) {
	// Put the OS and the sample snaps in place.
	coreSnapInfo := s.mockSnap(c, osSnapYaml)
	oldSnapInfo := s.mockSnap(c, sampleSnapYaml)

	// Put connection information between the OS snap and the sample snap.
	// This is done so that DisconnectSnap returns both snaps as "affected"
	// and so that the previously broken code path is exercised.
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"snap:network ubuntu-core:network": map[string]interface{}{"interface": "network"},
	})
	s.state.Unlock()

	// Initialize the manager. This registers both of the snaps and reloads the
	// connection between them.
	mgr := s.manager(c)

	// Put a new revision of the sample snap in place.
	newSnapInfo := s.mockUpdatedSnap(c, sampleSnapYaml, 42)

	// Sanity check, the revisions are different.
	c.Assert(oldSnapInfo.Revision, Not(Equals), 42)
	c.Assert(newSnapInfo.Revision, Equals, snap.R(42))

	// Run the setup-profiles task for the new revision and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: newSnapInfo.Name(),
			Revision: newSnapInfo.Revision,
		},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	// Ensure that both snaps were setup correctly.
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	// The sample snap was setup, with the correct new revision.
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, newSnapInfo.Name())
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Revision, Equals, newSnapInfo.Revision)
	// The OS snap was setup (because it was affected).
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Name(), Equals, coreSnapInfo.Name())
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Revision, Equals, coreSnapInfo.Revision)
}

// setup-profiles needs to setup security for connected slots after autoconnection
func (s *interfaceManagerSuite) TestAutoConnectSetupSecurityForConnectedSlots(c *C) {
	// Add an OS snap.
	coreSnapInfo := s.mockSnap(c, osSnapYaml)

	// Initialize the manager. This registers the OS snap.
	mgr := s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			Revision: snapInfo.Revision,
		},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that both snaps were setup correctly.
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	// The sample snap was setup, with the correct new revision.
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, snapInfo.Name())
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Revision, Equals, snapInfo.Revision)
	// The OS snap was setup (because its connected to sample snap).
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Name(), Equals, coreSnapInfo.Name())
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Revision, Equals, coreSnapInfo.Revision)
}

func (s *interfaceManagerSuite) TestDoDiscardConnsPlug(c *C) {
	s.testDoDicardConns(c, "consumer")
}

func (s *interfaceManagerSuite) TestDoDiscardConnsSlot(c *C) {
	s.testDoDicardConns(c, "producer")
}

func (s *interfaceManagerSuite) TestUndoDiscardConnsPlug(c *C) {
	s.testUndoDicardConns(c, "consumer")
}

func (s *interfaceManagerSuite) TestUndoDiscardConnsSlot(c *C) {
	s.testUndoDicardConns(c, "producer")
}

func (s *interfaceManagerSuite) testDoDicardConns(c *C, snapName string) {
	s.state.Lock()
	// Store information about a connection in the state.
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	// Store empty snap state. This snap has an empty sequence now.
	snapstate.Set(s.state, snapName, &snapstate.SnapState{})
	s.state.Unlock()

	mgr := s.manager(c)

	// Run the discard-conns task and let it finish
	change := s.addDiscardConnsChange(c, snapName)
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(change.Status(), Equals, state.DoneStatus)

	// Information about the connection was removed
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{})

	// But removed connections are preserved in the task for undo.
	var removed map[string]interface{}
	err = change.Tasks()[0].Get("removed", &removed)
	c.Assert(err, IsNil)
	c.Check(removed, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
}

func (s *interfaceManagerSuite) testUndoDicardConns(c *C, snapName string) {
	s.state.Lock()
	// Store information about a connection in the state.
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	// Store empty snap state. This snap has an empty sequence now.
	snapstate.Set(s.state, snapName, &snapstate.SnapState{})
	s.state.Unlock()

	mgr := s.manager(c)

	// Run the discard-conns task and let it finish
	change := s.addDiscardConnsChange(c, snapName)

	// Add a dummy task just to hold the change not ready.
	s.state.Lock()
	dummy := s.state.NewTask("dummy", "")
	change.AddTask(dummy)
	s.state.Unlock()

	mgr.Ensure()
	mgr.Wait()

	s.state.Lock()
	c.Check(change.Status(), Equals, state.DoStatus)
	change.Abort()
	s.state.Unlock()

	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(change.Status(), Equals, state.UndoneStatus)

	// Information about the connection is intact
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})

	var removed map[string]interface{}
	err = change.Tasks()[0].Get("removed", &removed)
	c.Check(err, Equals, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestDoRemove(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	mgr := s.manager(c)

	// Run the remove-security task
	change := s.addRemoveSnapSecurityChange(c, "consumer")
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	// Change succeeds
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(change.Status(), Equals, state.DoneStatus)

	repo := mgr.Repository()

	// Snap is removed from repository
	c.Check(repo.Plug("consumer", "slot"), IsNil)

	// Security of the snap was removed
	c.Check(s.secBackend.RemoveCalls, DeepEquals, []string{"consumer"})

	// Security of the related snap was configured
	c.Check(s.secBackend.SetupCalls, HasLen, 1)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, "producer")

	// Connection state was left intact
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
}

func (s *interfaceManagerSuite) TestConnectTracksConnectionsInState(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	_ = s.manager(c)

	s.state.Lock()
	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 5)

	ts.Tasks()[2].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change := s.state.NewChange("connect", "")
	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)
	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
		},
	})
}

func (s *interfaceManagerSuite) TestConnectSetsUpSecurity(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	_ = s.manager(c)

	s.state.Lock()
	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	ts.Tasks()[0].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change := s.state.NewChange("connect", "")
	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, "producer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Name(), Equals, "consumer")

	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestDisconnectSetsUpSecurity(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	mgr := s.manager(c)

	s.state.Lock()
	ts, err := ifacestate.Disconnect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	ts.Tasks()[0].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change := s.state.NewChange("disconnect", "")
	change.AddAll(ts)
	s.state.Unlock()

	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Name(), Equals, "producer")

	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestDisconnectTracksConnectionsInState(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	mgr := s.manager(c)

	s.state.Lock()
	ts, err := ifacestate.Disconnect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	ts.Tasks()[0].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change := s.state.NewChange("disconnect", "")
	change.AddAll(ts)
	s.state.Unlock()

	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)
	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{})
}

func (s *interfaceManagerSuite) TestManagerReloadsConnections(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	mgr := s.manager(c)
	repo := mgr.Repository()

	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, HasLen, 1)
	c.Assert(slot.Connections, HasLen, 1)
	c.Check(plug.Connections[0], DeepEquals, interfaces.SlotRef{Snap: "producer", Name: "slot"})
	c.Check(slot.Connections[0], DeepEquals, interfaces.PlugRef{Snap: "consumer", Name: "plug"})
}

func (s *interfaceManagerSuite) TestSetupProfilesDevModeMultiple(c *C) {
	mgr := s.manager(c)
	repo := mgr.Repository()

	// setup two snaps that are connected
	siP := s.mockSnap(c, producerYaml)
	siC := s.mockSnap(c, consumerYaml)
	err := repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test",
	})
	c.Assert(err, IsNil)
	err = repo.AddSlot(&interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      siC,
			Name:      "slot",
			Interface: "test",
		},
	})
	c.Assert(err, IsNil)
	err = repo.AddPlug(&interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      siP,
			Name:      "plug",
			Interface: "test",
		},
	})
	c.Assert(err, IsNil)
	connRef := interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: siP.Name(), Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: siC.Name(), Name: "slot"},
	}
	err = repo.Connect(connRef)
	c.Assert(err, IsNil)

	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: siC.Name(),
			Revision: siC.Revision,
		},
		Flags: snapstate.Flags{DevMode: true},
	})
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Check(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	// The first snap is setup in devmode, the second is not
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, siC.Name())
	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{DevMode: true})
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Name(), Equals, siP.Name())
	c.Check(s.secBackend.SetupCalls[1].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestCheckInterfacesDeny(c *C) {
	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})

	s.mockSnapDecl(c, "producer", "producer-publisher", nil)
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo), ErrorMatches, "installation denied.*")
}

func (s *interfaceManagerSuite) TestCheckInterfacesDenySkippedIfNoDecl(c *C) {
	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})

	// crucially, this test is missing this: s.mockSnapDecl(c, "producer", "producer-publisher", nil)
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo), IsNil)
}

func (s *interfaceManagerSuite) TestCheckInterfacesAllow(c *C) {
	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})

	s.mockSnapDecl(c, "producer", "producer-publisher", map[string]interface{}{
		"format": "1",
		"slots": map[string]interface{}{
			"test": "true",
		},
	})
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo), IsNil)
}

func (s *interfaceManagerSuite) TestCheckInterfacesConsidersImplicitSlots(c *C) {
	snapInfo := s.mockSnap(c, osSnapYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo), IsNil)
	c.Check(snapInfo.Slots["home"], NotNil)
}

// Test that setup-snap-security gets undone correctly when a snap is installed
// but the installation fails (the security profiles are removed).
func (s *interfaceManagerSuite) TestUndoSetupProfilesOnInstall(c *C) {
	// Create the interface manager
	mgr := s.manager(c)

	// Mock a snap and remove the side info from the state (it is implicitly
	// added by mockSnap) so that we can emulate a undo during a fresh
	// install.
	snapInfo := s.mockSnap(c, sampleSnapYaml)
	s.state.Lock()
	snapstate.Set(s.state, snapInfo.Name(), nil)
	s.state.Unlock()

	// Add a change that undoes "setup-snap-security"
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			Revision: snapInfo.Revision,
		},
	})
	s.state.Lock()
	change.Tasks()[0].SetStatus(state.UndoStatus)
	s.state.Unlock()

	// Turn the crank
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the change got undone.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.UndoneStatus)

	// Ensure that since we had no prior revisions of this snap installed the
	// undo task removed the security profile from the system.
	c.Assert(s.secBackend.SetupCalls, HasLen, 0)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 1)
	c.Check(s.secBackend.RemoveCalls, DeepEquals, []string{snapInfo.Name()})
}

// Test that setup-snap-security gets undone correctly when a snap is refreshed
// but the installation fails (the security profiles are restored to the old state).
func (s *interfaceManagerSuite) TestUndoSetupProfilesOnRefresh(c *C) {
	// Create the interface manager
	mgr := s.manager(c)

	// Mock a snap. The mockSnap call below also puts the side info into the
	// state so it seems like it was installed already.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Add a change that undoes "setup-snap-security"
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.Name(),
			Revision: snapInfo.Revision,
		},
	})
	s.state.Lock()
	change.Tasks()[0].SetStatus(state.UndoStatus)
	s.state.Unlock()

	// Turn the crank
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the change got undone.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.UndoneStatus)

	// Ensure that since had a revision in the state the undo task actually
	// setup the security of the snap we had in the state.
	c.Assert(s.secBackend.SetupCalls, HasLen, 1)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, snapInfo.Name())
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Revision, Equals, snapInfo.Revision)
	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
}

var ubuntuCoreYaml = `name: ubuntu-core
version: 1
type: os
`

var coreYaml = `name: ubuntu-core
version: 1
type: os
`

var httpdSnapYaml = `name: httpd
version: 1
plugs:
 network:
  interface: network
`

func (s *interfaceManagerSuite) TestManagerTransitionConnectionsCore(c *C) {
	s.mockSnap(c, ubuntuCoreYaml)
	s.mockSnap(c, coreYaml)
	s.mockSnap(c, httpdSnapYaml)

	mgr := s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("conns", map[string]interface{}{
		"httpd:network ubuntu-core:network": map[string]interface{}{
			"interface": "network", "auto": true,
		},
	})

	task := s.state.NewTask("transition-ubuntu-core", "...")
	task.Set("old-name", "ubuntu-core")
	task.Set("new-name", "core")
	change := s.state.NewChange("test-migrate", "")
	change.AddTask(task)

	s.state.Unlock()
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()
	s.state.Lock()

	c.Assert(change.Status(), Equals, state.DoneStatus)
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	// ensure the connection went from "ubuntu-core" to "core"
	c.Check(conns, DeepEquals, map[string]interface{}{
		"httpd:network core:network": map[string]interface{}{
			"interface": "network", "auto": true,
		},
	})
}

func (s *interfaceManagerSuite) TestManagerTransitionConnectionsCoreUndo(c *C) {
	s.mockSnap(c, ubuntuCoreYaml)
	s.mockSnap(c, coreYaml)
	s.mockSnap(c, httpdSnapYaml)

	mgr := s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("conns", map[string]interface{}{
		"httpd:network ubuntu-core:network": map[string]interface{}{
			"interface": "network", "auto": true,
		},
	})

	t := s.state.NewTask("transition-ubuntu-core", "...")
	t.Set("old-name", "ubuntu-core")
	t.Set("new-name", "core")
	change := s.state.NewChange("test-migrate", "")
	change.AddTask(t)
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	change.AddTask(terr)

	s.state.Unlock()
	for i := 0; i < 10; i++ {
		mgr.Ensure()
		mgr.Wait()
	}
	mgr.Stop()
	s.state.Lock()

	c.Assert(change.Status(), Equals, state.ErrorStatus)
	c.Check(t.Status(), Equals, state.UndoneStatus)

	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	// ensure the connection have not changed (still ubuntu-core)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"httpd:network ubuntu-core:network": map[string]interface{}{
			"interface": "network", "auto": true,
		},
	})
}
