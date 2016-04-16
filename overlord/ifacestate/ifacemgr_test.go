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
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/overlord/ifacestate"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
)

func TestInterfaceManager(t *testing.T) { TestingT(t) }

type interfaceManagerSuite struct {
	state           *state.State
	privateMgr      *ifacestate.InterfaceManager
	extraIfaces     []interfaces.Interface
	secBackend      *interfaces.TestSecurityBackend
	restoreBackends func()
}

var _ = Suite(&interfaceManagerSuite{})

func (s *interfaceManagerSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	state := state.New(nil)
	s.state = state
	s.privateMgr = nil
	s.extraIfaces = nil
	s.secBackend = &interfaces.TestSecurityBackend{}
	s.restoreBackends = ifacestate.MockSecurityBackends([]interfaces.SecurityBackend{s.secBackend})
}

func (s *interfaceManagerSuite) TearDownTest(c *C) {
	if s.privateMgr != nil {
		s.privateMgr.Stop()
	}
	dirs.SetRootDir("")
	s.restoreBackends()
}

func (s *interfaceManagerSuite) manager(c *C) *ifacestate.InterfaceManager {
	if s.privateMgr == nil {
		mgr, err := ifacestate.Manager(s.state, s.extraIfaces)
		c.Assert(err, IsNil)
		s.privateMgr = mgr
	}
	return s.privateMgr
}

func (s *interfaceManagerSuite) TestSmoke(c *C) {
	mgr := s.manager(c)
	mgr.Ensure()
	mgr.Wait()
}

func (s *interfaceManagerSuite) TestConnectTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)

	task := ts.Tasks()[0]
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
}

func (s *interfaceManagerSuite) TestEnsureProcessesConnectTask(c *C) {
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	change := s.state.NewChange("kind", "summary")
	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	change.AddAll(ts)
	s.state.Unlock()

	mgr := s.manager(c)
	mgr.Ensure()
	mgr.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	task := change.Tasks()[0]
	c.Check(task.Kind(), Equals, "connect")
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(change.Status(), Equals, state.DoneStatus)

	repo := mgr.Repository()
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, HasLen, 1)
	c.Assert(slot.Connections, HasLen, 1)
	c.Check(plug.Connections[0], DeepEquals, interfaces.SlotRef{Snap: "producer", Name: "slot"})
	c.Check(slot.Connections[0], DeepEquals, interfaces.PlugRef{Snap: "consumer", Name: "plug"})
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

func (s *interfaceManagerSuite) TestEnsureProcessesDisconnectTask(c *C) {
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	s.state.Lock()
	change := s.state.NewChange("kind", "summary")
	ts, err := ifacestate.Disconnect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	change.AddAll(ts)
	s.state.Unlock()

	mgr := s.manager(c)
	mgr.Ensure()
	mgr.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	task := change.Tasks()[0]
	c.Check(task.Kind(), Equals, "disconnect")
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(change.Status(), Equals, state.DoneStatus)

	// The connection is gone
	repo := mgr.Repository()
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, HasLen, 0)
	c.Assert(slot.Connections, HasLen, 0)
}

func (s *interfaceManagerSuite) mockIface(c *C, iface interfaces.Interface) {
	s.extraIfaces = append(s.extraIfaces, iface)
}

func (s *interfaceManagerSuite) mockSnap(c *C, yamlText string) *snap.Info {
	s.state.Lock()
	defer s.state.Unlock()

	// Parse the yaml
	snapInfo, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	snap.AddImplicitSlots(snapInfo)

	// Create on-disk yaml file (it is read by snapstate)
	dname := filepath.Join(dirs.SnapSnapsDir, snapInfo.Name(),
		strconv.Itoa(snapInfo.Revision), "meta")
	fname := filepath.Join(dname, "snap.yaml")
	err = os.MkdirAll(dname, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(fname, []byte(yamlText), 0644)
	c.Assert(err, IsNil)

	// Put a side info into the state
	snapstate.Set(s.state, snapInfo.Name(), &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{Revision: snapInfo.Revision}},
	})
	return snapInfo
}

func (s *interfaceManagerSuite) addSetupSnapSecurityChange(c *C, snapName string) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	task := s.state.NewTask("setup-profiles", "")
	ss := snapstate.SnapSetup{Name: snapName}
	task.Set("snap-setup", ss)
	taskset := state.NewTaskSet(task)
	change := s.state.NewChange("test", "")
	change.AddAll(taskset)
	return change
}

func (s *interfaceManagerSuite) addRemoveSnapSecurityChange(c *C, snapName string) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	task := s.state.NewTask("remove-profiles", "")
	ss := snapstate.SnapSetup{Name: snapName}
	task.Set("snap-setup", ss)
	taskset := state.NewTaskSet(task)
	change := s.state.NewChange("test", "")
	change.AddAll(taskset)
	return change
}

func (s *interfaceManagerSuite) addDiscardConnsChange(c *C, snapName string) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	task := s.state.NewTask("discard-conns", "")
	ss := snapstate.SnapSetup{Name: snapName}
	task.Set("snap-setup", ss)
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
`

var producerYaml = `
name: producer
version: 1
slots:
 slot:
  interface: test
`

// The setup-profiles task will not auto-connect an plug that was previously
// explicitly disconnected by the user.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityHonorsDisconnect(c *C) {
	// Add an OS snap as well as a sample snap with a "network" plug.
	// The plug is normally auto-connected.
	s.mockSnap(c, osSnapYaml)
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Initialize the manager. This registers the two snaps.
	mgr := s.manager(c)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, snapInfo.Name())
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
func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyAutoConnects(c *C) {
	// Add an OS snap.
	s.mockSnap(c, osSnapYaml)

	// Initialize the manager. This registers the OS snap.
	mgr := s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, snapInfo.Name())
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
	change := s.addSetupSnapSecurityChange(c, snapInfo.Name())
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

func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOnPlugSide(c *C) {
	s.testDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOn(c, "consumer")
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOnSlotSide(c *C) {
	s.testDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOn(c, "producer")
}

func (s *interfaceManagerSuite) testDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOn(c *C, snapName string) {
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	mgr := s.manager(c)

	// Run the setup-profiles task
	change := s.addSetupSnapSecurityChange(c, snapName)
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

	mgr.Ensure()
	mgr.Wait()

	s.state.Lock()
	c.Check(change.Status(), Equals, state.DoneStatus)
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
	c.Assert(err, IsNil)
	c.Check(removed, HasLen, 0)
}

func (s *interfaceManagerSuite) TestDoRemove(c *C) {
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
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
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	mgr := s.manager(c)

	s.state.Lock()
	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	change := s.state.NewChange("connect", "")
	change.AddAll(ts)
	s.state.Unlock()

	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

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
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	mgr := s.manager(c)

	s.state.Lock()
	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	change := s.state.NewChange("connect", "")
	change.AddAll(ts)
	s.state.Unlock()

	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Name(), Equals, "producer")

	c.Check(s.secBackend.SetupCalls[0].DevMode, Equals, false)
	c.Check(s.secBackend.SetupCalls[1].DevMode, Equals, false)
}

func (s *interfaceManagerSuite) TestDisconnectSetsUpSecurity(c *C) {
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
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
	change := s.state.NewChange("disconnect", "")
	change.AddAll(ts)
	s.state.Unlock()

	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Name(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Name(), Equals, "producer")

	c.Check(s.secBackend.SetupCalls[0].DevMode, Equals, false)
	c.Check(s.secBackend.SetupCalls[1].DevMode, Equals, false)
}

func (s *interfaceManagerSuite) TestDisconnectTracksConnectionsInState(c *C) {
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
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
	change := s.state.NewChange("disconnect", "")
	change.AddAll(ts)
	s.state.Unlock()

	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(change.Status(), Equals, state.DoneStatus)
	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{})
}

func (s *interfaceManagerSuite) TestManagerReloadsConnections(c *C) {
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
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
