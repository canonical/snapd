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

	task := s.state.NewTask("setup-snap-security", "")
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

func (s *interfaceManagerSuite) TestDoSetupSnapSecuirty(c *C) {
	s.mockSnap(c, osSnapYaml)
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	mgr := s.manager(c)

	// Run the setup-snap-security task
	change := s.addSetupSnapSecurityChange(c, snapInfo.Name())
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(change.Status(), Equals, state.DoneStatus)
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	// Auto-connection data was saved into the state
	c.Check(conns, DeepEquals, map[string]interface{}{
		"snap:network ubuntu-core:network": map[string]interface{}{
			"interface": "network", "auto": true,
		},
	})
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyKeepsExistingConnectionState(c *C) {
	s.mockSnap(c, osSnapYaml)
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	mgr := s.manager(c)

	// Put information about connections for another snap into the state
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"other-snap:network ubuntu-core:network": map[string]interface{}{
			"interface": "network",
		},
	})
	s.state.Unlock()

	// Run the setup-snap-security task
	change := s.addSetupSnapSecurityChange(c, snapInfo.Name())
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(change.Status(), Equals, state.DoneStatus)
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	// Information from other snaps is not damaged
	c.Check(conns, DeepEquals, map[string]interface{}{
		"other-snap:network ubuntu-core:network": map[string]interface{}{
			"interface": "network",
		},
		"snap:network ubuntu-core:network": map[string]interface{}{
			"interface": "network", "auto": true,
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

	// Run the setup-snap-security task
	change := s.addSetupSnapSecurityChange(c, snapName)
	mgr.Ensure()
	mgr.Wait()
	mgr.Stop()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(change.Status(), Equals, state.DoneStatus)

	repo := mgr.Repository()
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, HasLen, 1)
	c.Assert(slot.Connections, HasLen, 1)
	c.Check(plug.Connections[0], DeepEquals, interfaces.SlotRef{Snap: "producer", Name: "slot"})
	c.Check(slot.Connections[0], DeepEquals, interfaces.PlugRef{Snap: "consumer", Name: "plug"})
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
