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
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func TestInterfaceManager(t *testing.T) { TestingT(t) }

type interfaceManagerSuite struct {
	testutil.BaseTest
	o              *overlord.Overlord
	state          *state.State
	db             *asserts.Database
	privateMgr     *ifacestate.InterfaceManager
	privateHookMgr *hookstate.HookManager
	extraIfaces    []interfaces.Interface
	extraBackends  []interfaces.SecurityBackend
	secBackend     *ifacetest.TestSecurityBackend
	mockSnapCmd    *testutil.MockCmd
	storeSigning   *assertstest.StoreStack
	log            *bytes.Buffer
}

var _ = Suite(&interfaceManagerSuite{})

func (s *interfaceManagerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.storeSigning = assertstest.NewStoreStack("canonical", nil)

	s.mockSnapCmd = testutil.MockCommand(c, "snap", "")

	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapSystemKeyFile), 0755), IsNil)

	s.o = overlord.Mock()
	s.state = s.o.State()
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	s.db = db
	err = db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.state.Lock()
	assertstate.ReplaceDB(s.state, s.db)
	s.state.Unlock()

	s.privateHookMgr = nil
	s.privateMgr = nil
	s.extraIfaces = nil
	s.extraBackends = nil
	s.secBackend = &ifacetest.TestSecurityBackend{}
	// TODO: transition this so that we don't load real backends and instead
	// just load the test backend here and this is nicely integrated with
	// extraBackends above.
	s.BaseTest.AddCleanup(ifacestate.MockSecurityBackends([]interfaces.SecurityBackend{s.secBackend}))

	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf
}

func (s *interfaceManagerSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)

	s.mockSnapCmd.Restore()

	if s.privateMgr != nil {
		s.privateMgr.Stop()
	}
	dirs.SetRootDir("")
}

func (s *interfaceManagerSuite) manager(c *C) *ifacestate.InterfaceManager {
	if s.privateMgr == nil {
		mgr, err := ifacestate.Manager(s.state, s.hookManager(c), s.extraIfaces, s.extraBackends)
		c.Assert(err, IsNil)
		ifacestate.AddForeignTaskHandlers(mgr)
		s.privateMgr = mgr
		s.o.AddManager(mgr)

		// ensure the re-generation of security profiles did not
		// confuse the tests
		s.secBackend.SetupCalls = nil
	}
	return s.privateMgr
}

func (s *interfaceManagerSuite) hookManager(c *C) *hookstate.HookManager {
	if s.privateHookMgr == nil {
		mgr, err := hookstate.Manager(s.state)
		c.Assert(err, IsNil)
		s.privateHookMgr = mgr
		s.o.AddManager(mgr)
	}
	return s.privateHookMgr
}

func (s *interfaceManagerSuite) settle(c *C) {
	err := s.o.Settle(5 * time.Second)
	c.Assert(err, IsNil)
}

func (s *interfaceManagerSuite) TestSmoke(c *C) {
	mgr := s.manager(c)
	mgr.Ensure()
	mgr.Wait()
}

func (s *interfaceManagerSuite) TestKnownTaskKinds(c *C) {
	mgr, err := ifacestate.Manager(s.state, s.hookManager(c), nil, nil)
	c.Assert(err, IsNil)
	kinds := mgr.KnownTaskKinds()
	sort.Strings(kinds)
	c.Assert(kinds, DeepEquals, []string{
		"auto-connect",
		"connect",
		"discard-conns",
		"disconnect",
		"gadget-connect",
		"remove-profiles",
		"setup-profiles",
		"transition-ubuntu-core"})
}

func (s *interfaceManagerSuite) TestRepoAvailable(c *C) {
	_ = s.manager(c)
	s.state.Lock()
	defer s.state.Unlock()
	repo := ifacerepo.Get(s.state)
	c.Check(repo, FitsTypeOf, &interfaces.Repository{})
}

func (s *interfaceManagerSuite) TestConnectTask(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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

	var autoconnect bool
	err = task.Get("auto", &autoconnect)
	c.Assert(err, Equals, state.ErrNoState)
	c.Assert(autoconnect, Equals, false)

	// verify initial attributes are present in connect task
	var plugStaticAttrs map[string]interface{}
	var plugDynamicAttrs map[string]interface{}
	err = task.Get("plug-static", &plugStaticAttrs)
	c.Assert(err, IsNil)
	c.Assert(plugStaticAttrs, DeepEquals, map[string]interface{}{"attr1": "value1"})
	err = task.Get("plug-dynamic", &plugDynamicAttrs)
	c.Assert(err, IsNil)
	c.Assert(plugDynamicAttrs, DeepEquals, map[string]interface{}{})

	var slotStaticAttrs map[string]interface{}
	var slotDynamicAttrs map[string]interface{}
	err = task.Get("slot-static", &slotStaticAttrs)
	c.Assert(err, IsNil)
	c.Assert(slotStaticAttrs, DeepEquals, map[string]interface{}{"attr2": "value2"})
	err = task.Get("slot-dynamic", &slotDynamicAttrs)
	c.Assert(err, IsNil)
	c.Assert(slotDynamicAttrs, DeepEquals, map[string]interface{}{})

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

func (s *interfaceManagerSuite) testConnectDisconnectConflicts(c *C, f func(*state.State, string, string, string, string) (*state.TaskSet, error), snapName string, otherTaskKind string, expectedErr string) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("other-chg", "...")
	t := s.state.NewTask(otherTaskKind, "...")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName},
	})
	chg.AddTask(t)

	_, err := f(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, ErrorMatches, expectedErr)
}

func (s *interfaceManagerSuite) TestConnectConflictsPlugSnapOnLinkSnap(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Connect, "consumer", "link-snap", `snap "consumer" has "other-chg" change in progress`)
}

func (s *interfaceManagerSuite) TestConnectConflictsPlugSnapOnUnlink(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Connect, "consumer", "unlink-snap", `snap "consumer" has "other-chg" change in progress`)
}

func (s *interfaceManagerSuite) TestConnectConflictsSlotSnap(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Connect, "producer", "link-snap", `snap "producer" has "other-chg" change in progress`)
}

func (s *interfaceManagerSuite) TestConnectConflictsSlotSnapOnUnlink(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Connect, "producer", "unlink-snap", `snap "producer" has "other-chg" change in progress`)
}

func (s *interfaceManagerSuite) TestDisconnectConflictsPlugSnapOnLink(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Disconnect, "consumer", "link-snap", `snap "consumer" has "other-chg" change in progress`)
}

func (s *interfaceManagerSuite) TestDisconnectConflictsSlotSnapOnLink(c *C) {
	s.testConnectDisconnectConflicts(c, ifacestate.Disconnect, "producer", "link-snap", `snap "producer" has "other-chg" change in progress`)
}

func (s *interfaceManagerSuite) TestConnectDoesntConflict(c *C) {
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("other-connect", "...")
	t := s.state.NewTask("connect", "other connect task")
	t.Set("slot", interfaces.SlotRef{Snap: "producer", Name: "slot"})
	t.Set("plug", interfaces.PlugRef{Snap: "consumer", Name: "plug"})
	chg.AddTask(t)

	_, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)

	_, err = ifacestate.Disconnect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
}

func (s *interfaceManagerSuite) TestAutoconnectDoesntConflictOnInstallingDifferentSnap(c *C) {
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()

	sup1 := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer"},
	}
	sup2 := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "othersnap"},
	}

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("link-snap", "...")
	t.Set("snap-setup", sup2)
	chg.AddTask(t)

	t = s.state.NewTask("auto-connect", "...")
	t.Set("snap-setup", sup1)
	chg.AddTask(t)

	ignore, err := ifacestate.FindSymmetricAutoconnect(s.state, "consumer", "producer", t)
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, false)
	c.Assert(ifacestate.CheckConnectConflicts(s.state, "consumer", "producer", true), IsNil)

	ts, err := ifacestate.ConnectPriv(s.state, "consumer", "plug", "producer", "slot", []string{"auto"})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 5)
	connectTask := ts.Tasks()[2]
	c.Assert(connectTask.Kind(), Equals, "connect")
	var auto bool
	connectTask.Get("auto", &auto)
	c.Assert(auto, Equals, true)
}

func (s *interfaceManagerSuite) createAutoconnectChange(c *C, conflictingTask *state.Task) error {
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()

	chg1 := s.state.NewChange("a change", "...")
	conflictingTask.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer"},
	})
	chg1.AddTask(conflictingTask)

	chg := s.state.NewChange("other-chg", "...")
	t2 := s.state.NewTask("auto-connect", "...")
	t2.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "producer"},
	})

	chg.AddTask(t2)

	ignore, err := ifacestate.FindSymmetricAutoconnect(s.state, "consumer", "producer", t2)
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, false)

	return ifacestate.CheckConnectConflicts(s.state, "consumer", "producer", true)
}

func (s *interfaceManagerSuite) testRetryError(c *C, err error) {
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `task should be retried`)
	rerr, ok := err.(*state.Retry)
	c.Assert(ok, Equals, true)
	c.Assert(rerr, NotNil)
}

func (s *interfaceManagerSuite) TestAutoconnectConflictOnUnlink(c *C) {
	s.state.Lock()
	task := s.state.NewTask("unlink-snap", "")
	s.state.Unlock()
	err := s.createAutoconnectChange(c, task)
	s.testRetryError(c, err)
}

func (s *interfaceManagerSuite) TestAutoconnectConflictOnLink(c *C) {
	s.state.Lock()
	task := s.state.NewTask("link-snap", "")
	s.state.Unlock()
	err := s.createAutoconnectChange(c, task)
	s.testRetryError(c, err)
}

func (s *interfaceManagerSuite) TestSymmetricAutoconnectIgnore(c *C) {
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()

	sup1 := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer"},
	}
	sup2 := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "producer"},
	}

	chg1 := s.state.NewChange("install", "...")
	t1 := s.state.NewTask("auto-connect", "...")
	t1.Set("snap-setup", sup1)
	chg1.AddTask(t1)

	chg2 := s.state.NewChange("install", "...")
	t2 := s.state.NewTask("auto-connect", "...")
	t2.Set("snap-setup", sup2)
	chg2.AddTask(t2)

	ignore, err := ifacestate.FindSymmetricAutoconnect(s.state, "consumer", "producer", t1)
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, true)

	ignore, err = ifacestate.FindSymmetricAutoconnect(s.state, "consumer", "producer", t2)
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, true)
}

func (s *interfaceManagerSuite) TestAutoconnectConflictOnConnectWithAutoFlag(c *C) {
	s.state.Lock()
	task := s.state.NewTask("connect", "")
	task.Set("slot", interfaces.SlotRef{Snap: "producer", Name: "slot"})
	task.Set("plug", interfaces.PlugRef{Snap: "consumer", Name: "plug"})
	task.Set("auto", true)
	s.state.Unlock()

	err := s.createAutoconnectChange(c, task)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `task should be retried`)
}

func (s *interfaceManagerSuite) TestAutoconnectNoConflictOnConnect(c *C) {
	// FIXME: flesh this test out once individual connect conflict check is fleshed out
	s.state.Lock()
	task := s.state.NewTask("connect", "")
	task.Set("slot", interfaces.SlotRef{Snap: "producer", Name: "slot"})
	task.Set("plug", interfaces.PlugRef{Snap: "consumer", Name: "plug"})
	task.Set("auto", false)
	s.state.Unlock()

	err := s.createAutoconnectChange(c, task)
	c.Assert(err, IsNil)
}

func (s *interfaceManagerSuite) TestEnsureProcessesConnectTask(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 1)
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{interfaces.PlugRef{Snap: "consumer", Name: "plug"}, interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckInterfaceMismatch(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	_ = s.state.NewChange("kind", "summary")
	_, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "whatslot")
	c.Assert(err, ErrorMatches, `snap "producer" has no slot named "whatslot"`)
}

func (s *interfaceManagerSuite) TestConnectTaskNoSuchPlug(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
		ifaces := repo.Interfaces()
		c.Check(ifaces.Connections, HasLen, 0)
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
		ifaces := repo.Interfaces()
		c.Assert(ifaces.Connections, HasLen, 1)
		c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{interfaces.PlugRef{Snap: "consumer", Name: "plug"}, interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
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
		ifaces := repo.Interfaces()
		c.Assert(ifaces.Connections, HasLen, 1)
		c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{interfaces.PlugRef{Snap: "consumer", Name: "plug"}, interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
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
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})

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

func (s *interfaceManagerSuite) testDisconnect(c *C, plugSnap, plugName, slotSnap, slotName string) {
	// Put two snaps in place They consumer has an plug that can be connected
	// to slot on the producer.
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 0)

	// Ensure that the backend was used to setup security of both snaps
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.InstanceName(), Equals, "producer")

	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestStaleConnectionsIgnoredInReloadConnections(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"})

	// Put a stray connection in the state so that it automatically gets set up
	// when we create the manager.
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	restore := ifacestate.MockRemoveStaleConnections(func(s *state.State) error { return nil })
	defer restore()
	mgr := s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that nothing got connected.
	repo := mgr.Repository()
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 0)

	// Ensure that nothing to setup.
	c.Assert(s.secBackend.SetupCalls, HasLen, 0)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)

	// Ensure that nothing, crucially, got logged about that connection.
	// We still have an error logged about the system key but this is just
	// a bit of test mocking missing.
	logLines := strings.Split(s.log.String(), "\n")
	c.Assert(logLines, HasLen, 2)
	c.Assert(logLines[0], testutil.Contains, "error trying to compare the snap system key:")
	c.Assert(logLines[1], Equals, "")
}

func (s *interfaceManagerSuite) TestStaleConnectionsRemoved(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"})

	s.state.Lock()
	// Add stale connection to the state
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	// Create the manager, this removes stale connections
	mgr := s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that nothing got connected and connection was removed
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, HasLen, 0)

	repo := mgr.Repository()
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 0)
}

func (s *interfaceManagerSuite) mockIface(c *C, iface interfaces.Interface) {
	s.extraIfaces = append(s.extraIfaces, iface)
}

func (s *interfaceManagerSuite) mockIfaces(c *C, ifaces ...interfaces.Interface) {
	s.extraIfaces = append(s.extraIfaces, ifaces...)
}

func (s *interfaceManagerSuite) mockSnapDecl(c *C, name, publisher string, extraHeaders map[string]interface{}) {
	_, err := s.db.Find(asserts.AccountType, map[string]string{
		"account-id": publisher,
	})
	if asserts.IsNotFound(err) {
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
	snapInfo := snaptest.MockSnap(c, yamlText, sideInfo)
	sideInfo.RealName = snapInfo.StoreName()

	a, err := s.db.FindMany(asserts.SnapDeclarationType, map[string]string{
		"snap-name": sideInfo.RealName,
	})
	if err == nil {
		decl := a[0].(*asserts.SnapDeclaration)
		snapInfo.SnapID = decl.SnapID()
		sideInfo.SnapID = decl.SnapID()
	} else if asserts.IsNotFound(err) {
		err = nil
	}
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// Put a side info into the state
	snapstate.Set(s.state, snapInfo.InstanceName(), &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  sideInfo.Revision,
		SnapType: string(snapInfo.Type),
	})
	return snapInfo
}

func (s *interfaceManagerSuite) mockUpdatedSnap(c *C, yamlText string, revision int) *snap.Info {
	sideInfo := &snap.SideInfo{Revision: snap.R(revision)}
	snapInfo := snaptest.MockSnap(c, yamlText, sideInfo)
	sideInfo.RealName = snapInfo.StoreName()

	s.state.Lock()
	defer s.state.Unlock()

	// Put the new revision (stored in SideInfo) into the state
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, snapInfo.InstanceName(), &snapst)
	c.Assert(err, IsNil)
	snapst.Sequence = append(snapst.Sequence, sideInfo)
	snapstate.Set(s.state, snapInfo.InstanceName(), &snapst)

	return snapInfo
}

func (s *interfaceManagerSuite) addSetupSnapSecurityChange(c *C, snapsup *snapstate.SnapSetup) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	change := s.state.NewChange("test", "")

	task1 := s.state.NewTask("setup-profiles", "")
	task1.Set("snap-setup", snapsup)
	change.AddTask(task1)

	task2 := s.state.NewTask("auto-connect", "")
	task2.Set("snap-setup", snapsup)
	task2.WaitFor(task1)
	change.AddTask(task2)

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

var ubuntuCoreSnapYaml = `
name: ubuntu-core
version: 1
type: os
`

var coreSnapYaml = `
name: core
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

var consumer2Yaml = `
name: consumer2
version: 1
plugs:
 plug:
  interface: test
  attr1: value1
`

var producerYaml = `
name: producer
version: 1
slots:
 slot:
  interface: test
  attr2: value2
`

var producer2Yaml = `
name: producer2
version: 1
slots:
 slot:
  interface: test
  attr2: value2
`

var httpdSnapYaml = `name: httpd
version: 1
plugs:
 network:
  interface: network
`

var selfconnectSnapYaml = `
name: producerconsumer
version: 1
slots:
 slot:
  interface: test
plugs:
 plug:
  interface: test
`

// The setup-profiles task will not auto-connect a plug that was previously
// explicitly disconnected by the user.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityHonorsUndesiredFlag(c *C) {
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"snap:network ubuntu-core:network": map[string]interface{}{
			"undesired": true,
		},
	})
	s.state.Unlock()

	// Add an OS snap as well as a sample snap with a "network" plug.
	// The plug is normally auto-connected.
	s.mockSnap(c, ubuntuCoreSnapYaml)
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Initialize the manager. This registers the two snaps.
	mgr := s.manager(c)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded
	c.Assert(change.Status(), Equals, state.DoneStatus)

	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"snap:network ubuntu-core:network": map[string]interface{}{
			"undesired": true,
		},
	})

	// Ensure that "network" is not connected
	repo := mgr.Repository()
	plug := repo.Plug("snap", "network")
	c.Assert(plug, Not(IsNil))
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 0)
}

// The setup-profiles task will auto-connect plugs with viable candidates.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsPlugs(c *C) {
	// Add an OS snap.
	s.mockSnap(c, ubuntuCoreSnapYaml)

	// Initialize the manager. This registers the OS snap.
	mgr := s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

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
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 1) //FIXME add deep eq
}

// The setup-profiles task will auto-connect slots with viable candidates.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsSlots(c *C) {
	// Mock the interface that will be used by the test
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	// Add an OS snap.
	s.mockSnap(c, ubuntuCoreSnapYaml)
	// Add a consumer snap with unconnect plug (interface "test")
	s.mockSnap(c, consumerYaml)

	// Initialize the manager. This registers the OS snap.
	mgr := s.manager(c)

	// Add a producer snap with a "slot" slot of the "test" interface.
	snapInfo := s.mockSnap(c, producerYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

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
			"plug-static": map[string]interface{}{"attr1": "value1"},
			"slot-static": map[string]interface{}{"attr2": "value2"},
		},
	})

	// Ensure that "slot" is really connected.
	repo := mgr.Repository()
	slot := repo.Slot("producer", "slot")
	c.Assert(slot, Not(IsNil))
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 1)
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{interfaces.PlugRef{Snap: "consumer", Name: "plug"}, interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
}

// The setup-profiles task will auto-connect slots with viable multiple candidates.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsSlotsMultiplePlugs(c *C) {
	// Mock the interface that will be used by the test
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	// Add an OS snap.
	s.mockSnap(c, ubuntuCoreSnapYaml)
	// Add a consumer snap with unconnect plug (interface "test")
	s.mockSnap(c, consumerYaml)
	// Add a 2nd consumer snap with unconnect plug (interface "test")
	s.mockSnap(c, consumer2Yaml)

	// Initialize the manager. This registers the OS snap.
	mgr := s.manager(c)

	// Add a producer snap with a "slot" slot of the "test" interface.
	snapInfo := s.mockSnap(c, producerYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

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
			"plug-static": map[string]interface{}{"attr1": "value1"},
			"slot-static": map[string]interface{}{"attr2": "value2"},
		},
		"consumer2:plug producer:slot": map[string]interface{}{
			"interface": "test", "auto": true,
			"plug-static": map[string]interface{}{"attr1": "value1"},
			"slot-static": map[string]interface{}{"attr2": "value2"},
		},
	})

	// Ensure that "slot" is really connected.
	repo := mgr.Repository()
	slot := repo.Slot("producer", "slot")
	c.Assert(slot, Not(IsNil))
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 2)
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{
		{interfaces.PlugRef{Snap: "consumer", Name: "plug"}, interfaces.SlotRef{Snap: "producer", Name: "slot"}},
		{interfaces.PlugRef{Snap: "consumer2", Name: "plug"}, interfaces.SlotRef{Snap: "producer", Name: "slot"}},
	})
}

// The setup-profiles task will not auto-connect slots if viable alternative slots are present.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityNoAutoConnectSlotsIfAlternative(c *C) {
	// Mock the interface that will be used by the test
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	// Add an OS snap.
	s.mockSnap(c, ubuntuCoreSnapYaml)
	// Add a consumer snap with unconnect plug (interface "test")
	s.mockSnap(c, consumerYaml)

	// alternative conflicting producer
	s.mockSnap(c, producer2Yaml)

	// Initialize the manager. This registers the OS snap.
	_ = s.manager(c)

	// Add a producer snap with a "slot" slot of the "test" interface.
	snapInfo := s.mockSnap(c, producerYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that no connections were made
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, Equals, state.ErrNoState)
	c.Check(conns, HasLen, 0)
}

// The setup-profiles task will auto-connect plugs with viable candidates also condidering snap declarations.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBased(c *C) {
	s.testDoSetupSnapSecurityAutoConnectsDeclBased(c, true, func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
		// Ensure that "test" plug is now saved in the state as auto-connected.
		c.Check(conns, DeepEquals, map[string]interface{}{
			"consumer:plug producer:slot": map[string]interface{}{"auto": true, "interface": "test",
				"plug-static": map[string]interface{}{"attr1": "value1"},
				"slot-static": map[string]interface{}{"attr2": "value2"},
			}})
		// Ensure that "test" is really connected.
		c.Check(repoConns, HasLen, 1)
	})
}

// The setup-profiles task will *not* auto-connect plugs with viable candidates when snap declarations are missing.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedWhenMissingDecl(c *C) {
	s.testDoSetupSnapSecurityAutoConnectsDeclBased(c, false, func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
		// Ensure nothing is connected.
		c.Check(conns, HasLen, 0)
		c.Check(repoConns, HasLen, 0)
	})
}

func (s *interfaceManagerSuite) testDoSetupSnapSecurityAutoConnectsDeclBased(c *C, withDecl bool, check func(map[string]interface{}, []*interfaces.ConnRef)) {
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
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
			RealName: snapInfo.StoreName(),
			SnapID:   snapInfo.SnapID,
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	var conns map[string]interface{}
	_ = s.state.Get("conns", &conns)

	repo := mgr.Repository()
	plug := repo.Plug("consumer", "plug")
	c.Assert(plug, Not(IsNil))

	check(conns, repo.Interfaces().Connections)
}

// The setup-profiles task will only touch connection state for the task it
// operates on or auto-connects to and will leave other state intact.
func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyKeepsExistingConnectionState(c *C) {
	// Add an OS snap in place.
	s.mockSnap(c, ubuntuCoreSnapYaml)

	// Initialize the manager. This registers the two snaps.
	_ = s.manager(c)

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
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

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

func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyIgnoresStrayConnection(c *C) {
	// Add an OS snap
	snapInfo := s.mockSnap(c, ubuntuCoreSnapYaml)

	_ = s.manager(c)

	// Put fake information about connections for another snap into the state.
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"removed-snap:network ubuntu-core:network": map[string]interface{}{
			"interface": "network",
		},
	})
	s.state.Unlock()

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that the tasks don't report errors caused by bad connections
	for _, t := range change.Tasks() {
		c.Assert(t.Log(), HasLen, 0)
	}
}

// The setup-profiles task will add implicit slots necessary for the OS snap.
func (s *interfaceManagerSuite) TestDoSetupProfilesAddsImplicitSlots(c *C) {
	// Initialize the manager.
	mgr := s.manager(c)

	// Add an OS snap.
	snapInfo := s.mockSnap(c, ubuntuCoreSnapYaml)

	// Run the setup-profiles task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that we have slots on the OS snap.
	repo := mgr.Repository()
	slots := repo.Slots(snapInfo.InstanceName())
	// NOTE: This is not an exact test as it duplicates functionality elsewhere
	// and is was a pain to update each time. This is correctly handled by the
	// implicit slot tests in snap/implicit_test.go
	c.Assert(len(slots) > 18, Equals, true)
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOnPlugSide(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	snapInfo := s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	s.testDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOn(c, snapInfo.InstanceName(), snapInfo.Revision)

	// Ensure that the backend was used to setup security of both snaps
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.InstanceName(), Equals, "producer")

	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOnSlotSide(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	snapInfo := s.mockSnap(c, producerYaml)
	s.testDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOn(c, snapInfo.InstanceName(), snapInfo.Revision)

	// Ensure that the backend was used to setup security of both snaps
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, "producer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.InstanceName(), Equals, "consumer")

	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) testDoSetupSnapSecuirtyReloadsConnectionsWhenInvokedOn(c *C, snapName string, revision snap.Revision) {
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
	s.settle(c)

	// Change succeeds
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(change.Status(), Equals, state.DoneStatus)

	repo := mgr.Repository()

	// Repository shows the connection
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 1)
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{interfaces.PlugRef{Snap: "consumer", Name: "plug"}, interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
}

// The setup-profiles task will honor snapstate.DevMode flag by storing it
// in the SnapState.Flags and by actually setting up security
// using that flag. Old copy of SnapState.Flag's DevMode is saved for the undo
// handler under `old-devmode`.
func (s *interfaceManagerSuite) TestSetupProfilesHonorsDevMode(c *C) {
	// Put the OS snap in place.
	_ = s.manager(c)

	// Initialize the manager. This registers the OS snap.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-profiles task and let it finish.
	// Note that the task will see SnapSetup.Flags equal to DeveloperMode.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
		Flags: snapstate.Flags{DevMode: true},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Check(change.Status(), Equals, state.DoneStatus)

	// The snap was setup with DevModeConfinement
	c.Assert(s.secBackend.SetupCalls, HasLen, 1)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, "snap")
	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{DevMode: true})
}

// setup-profiles uses the new snap.Info when setting up security for the new
// snap when it had prior connections and DisconnectSnap() returns it as a part
// of the affected set.
func (s *interfaceManagerSuite) TestSetupProfilesUsesFreshSnapInfo(c *C) {
	// Put the OS and the sample snaps in place.
	coreSnapInfo := s.mockSnap(c, ubuntuCoreSnapYaml)
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
	_ = s.manager(c)

	// Put a new revision of the sample snap in place.
	newSnapInfo := s.mockUpdatedSnap(c, sampleSnapYaml, 42)

	// Sanity check, the revisions are different.
	c.Assert(oldSnapInfo.Revision, Not(Equals), 42)
	c.Assert(newSnapInfo.Revision, Equals, snap.R(42))

	// Run the setup-profiles task for the new revision and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: newSnapInfo.StoreName(),
			Revision: newSnapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	// Ensure that both snaps were setup correctly.
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	// The sample snap was setup, with the correct new revision.
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, newSnapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Revision, Equals, newSnapInfo.Revision)
	// The OS snap was setup (because it was affected).
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.InstanceName(), Equals, coreSnapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.Revision, Equals, coreSnapInfo.Revision)
}

// setup-profiles needs to setup security for connected slots after autoconnection
func (s *interfaceManagerSuite) TestAutoConnectSetupSecurityForConnectedSlots(c *C) {
	// Add an OS snap.
	coreSnapInfo := s.mockSnap(c, ubuntuCoreSnapYaml)

	// Initialize the manager. This registers the OS snap.
	_ = s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that both snaps were setup correctly.
	c.Assert(s.secBackend.SetupCalls, HasLen, 3)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	// The sample snap was setup, with the correct new revision.
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Revision, Equals, snapInfo.Revision)
	// The OS snap was setup (because its connected to sample snap).
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.InstanceName(), Equals, coreSnapInfo.InstanceName())
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
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
		},
	})

	// Store empty snap state. This snap has an empty sequence now.
	s.state.Unlock()

	// mock the snaps or otherwise the manager will remove stale connections
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	mgr := s.manager(c)

	s.state.Lock()
	// remove the snaps so that discard-conns doesn't complain about snaps still installed
	snapstate.Set(s.state, "producer", nil)
	snapstate.Set(s.state, "consumer", nil)
	s.state.Unlock()

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
	mgr := s.manager(c)

	s.state.Lock()
	// Store information about a connection in the state.
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})

	// Store empty snap state. This snap has an empty sequence now.
	snapstate.Set(s.state, snapName, &snapstate.SnapState{})
	s.state.Unlock()

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
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, "producer")

	// Connection state was left intact
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
}

func (s *interfaceManagerSuite) TestConnectTracksConnectionsInState(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
			"interface":   "test",
			"plug-static": map[string]interface{}{"attr1": "value1"},
			"slot-static": map[string]interface{}{"attr2": "value2"},
		},
	})
}

func (s *interfaceManagerSuite) TestConnectSetsUpSecurity(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})

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
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, "producer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.InstanceName(), Equals, "consumer")

	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestDisconnectSetsUpSecurity(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.InstanceName(), Equals, "producer")

	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestDisconnectTracksConnectionsInState(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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

func (s *interfaceManagerSuite) TestDisconnectDisablesAutoConnect(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test", "auto": true},
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
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test", "auto": true, "undesired": true},
	})
}

func (s *interfaceManagerSuite) TestManagerReloadsConnections(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	mgr := s.manager(c)
	repo := mgr.Repository()

	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 1)
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{interfaces.PlugRef{Snap: "consumer", Name: "plug"}, interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
}

func (s *interfaceManagerSuite) TestManagerDoesntReloadUndesiredAutoconnections(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test",
			"auto":      true,
			"undesired": true,
		},
	})
	s.state.Unlock()

	mgr := s.manager(c)
	c.Assert(mgr.Repository().Interfaces().Connections, HasLen, 0)
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
	err = repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test2",
	})
	c.Assert(err, IsNil)

	err = repo.AddSlot(&snap.SlotInfo{
		Snap:      siC,
		Name:      "slot",
		Interface: "test",
	})
	c.Assert(err, IsNil)
	err = repo.AddPlug(&snap.PlugInfo{
		Snap:      siP,
		Name:      "plug",
		Interface: "test",
	})
	c.Assert(err, IsNil)
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: siP.InstanceName(), Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: siC.InstanceName(), Name: "slot"},
	}
	_, err = repo.Connect(connRef, nil, nil, nil)
	c.Assert(err, IsNil)

	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: siC.StoreName(),
			Revision: siC.Revision,
		},
		Flags: snapstate.Flags{DevMode: true},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Check(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	// The first snap is setup in devmode, the second is not
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, siC.InstanceName())
	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{DevMode: true})
	c.Check(s.secBackend.SetupCalls[1].SnapInfo.InstanceName(), Equals, siP.InstanceName())
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
	snapInfo := s.mockSnap(c, ubuntuCoreSnapYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo), IsNil)
	c.Check(snapInfo.Slots["home"], NotNil)
}

// Test that setup-snap-security gets undone correctly when a snap is installed
// but the installation fails (the security profiles are removed).
func (s *interfaceManagerSuite) TestUndoSetupProfilesOnInstall(c *C) {
	// Create the interface manager
	_ = s.manager(c)

	// Mock a snap and remove the side info from the state (it is implicitly
	// added by mockSnap) so that we can emulate a undo during a fresh
	// install.
	snapInfo := s.mockSnap(c, sampleSnapYaml)
	s.state.Lock()
	snapstate.Set(s.state, snapInfo.InstanceName(), nil)
	s.state.Unlock()

	// Add a change that undoes "setup-snap-security"
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.state.Lock()
	c.Assert(change.Tasks(), HasLen, 2)
	change.Tasks()[0].SetStatus(state.UndoStatus)
	change.Tasks()[1].SetStatus(state.UndoneStatus)
	s.state.Unlock()

	// Turn the crank
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the change got undone.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.UndoneStatus)

	// Ensure that since we had no prior revisions of this snap installed the
	// undo task removed the security profile from the system.
	c.Assert(s.secBackend.SetupCalls, HasLen, 0)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 1)
	c.Check(s.secBackend.RemoveCalls, DeepEquals, []string{snapInfo.InstanceName()})
}

// Test that setup-snap-security gets undone correctly when a snap is refreshed
// but the installation fails (the security profiles are restored to the old state).
func (s *interfaceManagerSuite) TestUndoSetupProfilesOnRefresh(c *C) {
	// Create the interface manager
	_ = s.manager(c)

	// Mock a snap. The mockSnap call below also puts the side info into the
	// state so it seems like it was installed already.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Add a change that undoes "setup-snap-security"
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})
	s.state.Lock()
	c.Assert(change.Tasks(), HasLen, 2)
	change.Tasks()[1].SetStatus(state.UndoStatus)
	s.state.Unlock()

	// Turn the crank
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the change got undone.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.UndoneStatus)

	// Ensure that since had a revision in the state the undo task actually
	// setup the security of the snap we had in the state.
	c.Assert(s.secBackend.SetupCalls, HasLen, 1)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[0].SnapInfo.Revision, Equals, snapInfo.Revision)
	c.Check(s.secBackend.SetupCalls[0].Options, Equals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestManagerTransitionConnectionsCore(c *C) {
	s.mockSnap(c, ubuntuCoreSnapYaml)
	s.mockSnap(c, coreSnapYaml)
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
	s.mockSnap(c, ubuntuCoreSnapYaml)
	s.mockSnap(c, coreSnapYaml)
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

// Test "core-support" connections that loop back to core is
// renamed to match the rename of the plug.
func (s *interfaceManagerSuite) TestCoreConnectionsRenamed(c *C) {
	// Put state with old connection data.
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"core:core-support core:core-support": map[string]interface{}{
			"interface": "core-support", "auto": true,
		},
		"snap:unrelated core:unrelated": map[string]interface{}{
			"interface": "unrelated", "auto": true,
		},
	})
	s.state.Unlock()

	// mock both snaps, otherwise the manager will remove stale connections
	s.mockSnap(c, coreSnapYaml)
	s.mockSnap(c, sampleSnapYaml)

	// Start the manager, this is where renames happen.
	s.manager(c)

	// Check that "core-support" connection got renamed.
	s.state.Lock()
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	s.state.Unlock()
	c.Assert(err, IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"core:core-support-plug core:core-support": map[string]interface{}{
			"interface": "core-support", "auto": true,
		},
		"snap:unrelated core:unrelated": map[string]interface{}{
			"interface": "unrelated", "auto": true,
		},
	})
}

// Test that "network-bind" and "core-support" plugs are renamed to
// "network-bind-plug" and "core-support-plug" in order not to clash with slots
// with the same names.
func (s *interfaceManagerSuite) TestAutomaticCorePlugsRenamed(c *C) {
	s.mockSnap(c, coreSnapYaml+`
plugs:
  network-bind:
  core-support:
`)
	mgr := s.manager(c)

	// old plugs are gone
	c.Assert(mgr.Repository().Plug("core", "network-bind"), IsNil)
	c.Assert(mgr.Repository().Plug("core", "core-support"), IsNil)
	// new plugs are present
	c.Assert(mgr.Repository().Plug("core", "network-bind-plug"), Not(IsNil))
	c.Assert(mgr.Repository().Plug("core", "core-support-plug"), Not(IsNil))
	// slots are present and unchanged
	c.Assert(mgr.Repository().Slot("core", "network-bind"), Not(IsNil))
	c.Assert(mgr.Repository().Slot("core", "core-support"), Not(IsNil))
}

func (s *interfaceManagerSuite) TestAutoConnectDuringCoreTransition(c *C) {
	// Add both the old and new core snaps
	s.mockSnap(c, ubuntuCoreSnapYaml)
	s.mockSnap(c, coreSnapYaml)

	// Initialize the manager. This registers both of the core snaps.
	mgr := s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	// Normally it would not be auto connected because there are multiple
	// provides but we have special support for this case so the old
	// ubuntu-core snap is ignored and we pick the new core snap.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.StoreName(),
			Revision: snapInfo.Revision,
		},
	})

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// Ensure that "network" is now saved in the state as auto-connected and
	// that it is connected to the new core snap rather than the old
	// ubuntu-core snap.
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"snap:network core:network": map[string]interface{}{
			"interface": "network", "auto": true,
		},
	})

	// Ensure that "network" is really connected.
	repo := mgr.Repository()
	plug := repo.Plug("snap", "network")
	c.Assert(plug, Not(IsNil))
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 1)
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{interfaces.PlugRef{Snap: "snap", Name: "network"}, interfaces.SlotRef{Snap: "core", Name: "network"}}})
}

func makeAutoConnectChange(st *state.State, plugSnap, plug, slotSnap, slot string) *state.Change {
	chg := st.NewChange("connect...", "...")

	t := st.NewTask("connect", "other connect task")
	t.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slot})
	t.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plug})
	var plugAttrs, slotAttrs map[string]interface{}
	t.Set("plug-dynamic", plugAttrs)
	t.Set("slot-dynamic", slotAttrs)
	t.Set("auto", true)

	// two fake tasks for connect-plug-/slot- hooks
	hs1 := hookstate.HookSetup{
		Snap:     slotSnap,
		Optional: true,
		Hook:     "connect-slot-" + slot,
	}
	ht1 := hookstate.HookTask(st, "connect-slot hook", &hs1, nil)
	ht1.WaitFor(t)
	hs2 := hookstate.HookSetup{
		Snap:     plugSnap,
		Optional: true,
		Hook:     "connect-plug-" + plug,
	}
	ht2 := hookstate.HookTask(st, "connect-plug hook", &hs2, nil)
	ht2.WaitFor(ht1)

	chg.AddTask(t)
	chg.AddTask(ht1)
	chg.AddTask(ht2)

	return chg
}

func (s *interfaceManagerSuite) TestUndoConnectRestoresOldConnState(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"})
	mgr := s.manager(c)
	producer := s.mockSnap(c, producerYaml)
	consumer := s.mockSnap(c, consumerYaml)

	repo := s.manager(c).Repository()
	err := repo.AddPlug(&snap.PlugInfo{
		Snap:      consumer,
		Name:      "plug",
		Interface: "test",
	})
	c.Assert(err, IsNil)
	err = repo.AddSlot(&snap.SlotInfo{
		Snap:      producer,
		Name:      "slot",
		Interface: "test",
	})
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface":    "test",
			"plug-static":  map[string]interface{}{"foo1": "bar1"},
			"slot-static":  map[string]interface{}{"foo2": "bar2"},
			"plug-dynamic": map[string]interface{}{"foo3": "bar3"},
			"slot-dynamic": map[string]interface{}{"foo4": "bar4"},
		},
	})

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot")
	// Add a dummy task just to hold the change not ready.
	dummy := s.state.NewTask("dummy", "")
	chg.AddTask(dummy)

	s.state.Unlock()
	mgr.Ensure()
	mgr.Wait()
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoStatus)
	chg.Abort()

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status(), Equals, state.UndoneStatus)

	// Information about the connection is intact
	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface":    "test",
			"plug-static":  map[string]interface{}{"foo1": "bar1"},
			"plug-dynamic": map[string]interface{}{"foo3": "bar3"},
			"slot-static":  map[string]interface{}{"foo2": "bar2"},
			"slot-dynamic": map[string]interface{}{"foo4": "bar4"},
		}})
}

func (s *interfaceManagerSuite) TestConnectErrorMissingSlotSnapOnAutoConnect(c *C) {
	_ = s.manager(c)
	s.mockSnap(c, producerYaml)
	s.mockSnap(c, consumerYaml)

	s.state.Lock()

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot")
	// remove producer snap from the state, doConnect should complain
	snapstate.Set(s.state, "producer", nil)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*snap "producer" is no longer available for auto-connecting.*`)

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), Equals, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestConnectErrorMissingPlugSnapOnAutoConnect(c *C) {
	_ = s.manager(c)
	s.mockSnap(c, producerYaml)
	s.mockSnap(c, consumerYaml)

	s.state.Lock()
	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot")
	// remove consumer snap from the state, doConnect should complain
	snapstate.Set(s.state, "consumer", nil)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*snap "consumer" is no longer available for auto-connecting.*`)

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), Equals, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestConnectErrorMissingPlugOnAutoConnect(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"})
	_ = s.manager(c)
	producer := s.mockSnap(c, producerYaml)
	// consumer snap has no plug, doConnect should complain
	s.mockSnap(c, consumerYaml)

	repo := s.manager(c).Repository()
	err := repo.AddSlot(&snap.SlotInfo{
		Snap:      producer,
		Name:      "slot",
		Interface: "test",
	})
	c.Assert(err, IsNil)

	s.state.Lock()

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*snap "consumer" has no "plug" plug.*`)

	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestConnectErrorMissingSlotOnAutoConnect(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"})
	_ = s.manager(c)
	// producer snap has no slot, doConnect should complain
	s.mockSnap(c, producerYaml)
	consumer := s.mockSnap(c, consumerYaml)

	repo := s.manager(c).Repository()
	err := repo.AddPlug(&snap.PlugInfo{
		Snap:      consumer,
		Name:      "plug",
		Interface: "test",
	})
	c.Assert(err, IsNil)

	s.state.Lock()

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*snap "producer" has no "slot" slot.*`)

	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestConnectHandlesAutoconnect(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"})
	_ = s.manager(c)
	producer := s.mockSnap(c, producerYaml)
	consumer := s.mockSnap(c, consumerYaml)

	repo := s.manager(c).Repository()
	err := repo.AddPlug(&snap.PlugInfo{
		Snap:      consumer,
		Name:      "plug",
		Interface: "test",
	})
	c.Assert(err, IsNil)
	err = repo.AddSlot(&snap.SlotInfo{
		Snap:      producer,
		Name:      "slot",
		Interface: "test",
	})
	c.Assert(err, IsNil)

	s.state.Lock()

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	task := chg.Tasks()[0]
	c.Assert(task.Status(), Equals, state.DoneStatus)

	// Ensure that "slot" is now auto-connected.
	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test", "auto": true,
		},
	})
}

func (s *interfaceManagerSuite) TestRegenerateAllSecurityProfilesWritesSystemKeyFile(c *C) {
	restore := interfaces.MockSystemKey(`{"core": "123"}`)
	defer restore()

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	c.Assert(osutil.FileExists(dirs.SnapSystemKeyFile), Equals, false)

	_ = s.manager(c)
	c.Check(dirs.SnapSystemKeyFile, testutil.FileMatches, `{.*"build-id":.*`)

	stat, err := os.Stat(dirs.SnapSystemKeyFile)
	c.Assert(err, IsNil)

	// run manager again, but this time the snapsystemkey file should
	// not be rewriten as the systemKey inputs have not changed
	time.Sleep(20 * time.Millisecond)
	s.privateMgr = nil
	_ = s.manager(c)
	stat2, err := os.Stat(dirs.SnapSystemKeyFile)
	c.Assert(err, IsNil)
	c.Check(stat.ModTime(), DeepEquals, stat2.ModTime())
}

func (s *interfaceManagerSuite) TestAutoconnectSelf(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, selfconnectSnapYaml)
	repo := s.manager(c).Repository()
	c.Assert(repo.Slots("producerconsumer"), HasLen, 1)

	s.state.Lock()

	sup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			Revision: snap.R(1),
			RealName: "producerconsumer"},
	}

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("auto-connect", "...")
	t.Set("snap-setup", sup)
	chg.AddTask(t)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	hooktypes := make(map[string]int)
	for _, t := range s.state.Tasks() {
		if t.Kind() == "run-hook" {
			var hsup hookstate.HookSetup
			c.Assert(t.Get("hook-setup", &hsup), IsNil)
			count := hooktypes[hsup.Hook]
			hooktypes[hsup.Hook] = count + 1
		}
	}

	// verify that every hook was run once
	for _, ht := range []string{"prepare-plug-plug", "prepare-slot-slot", "connect-slot-slot", "connect-plug-plug"} {
		c.Assert(hooktypes[ht], Equals, 1)
	}
}

func (s *interfaceManagerSuite) TestAutoconnectForDefaultContentProvider(c *C) {
	restore := ifacestate.MockContentLinkRetryTimeout(5 * time.Millisecond)
	defer restore()

	s.mockSnap(c, `name: snap-content-plug
version: 1
plugs:
 shared-content-plug:
  interface: content
  default-provider: snap-content-slot
  content: shared-content
`)
	s.mockSnap(c, `name: snap-content-slot
version: 1
slots:
 shared-content-slot:
  interface: content
  content: shared-content
`)
	mgr := s.manager(c)

	s.state.Lock()

	supContentPlug := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			Revision: snap.R(1),
			RealName: "snap-content-plug"},
	}
	supContentSlot := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			Revision: snap.R(1),
			RealName: "snap-content-slot"},
	}
	chg := s.state.NewChange("install", "...")

	tInstPlug := s.state.NewTask("link-snap", "Install snap-content-plug")
	tInstPlug.Set("snap-setup", supContentPlug)
	chg.AddTask(tInstPlug)

	tInstSlot := s.state.NewTask("link-snap", "Install snap-content-slot")
	tInstSlot.Set("snap-setup", supContentSlot)
	chg.AddTask(tInstSlot)

	tConnectPlug := s.state.NewTask("auto-connect", "...")
	tConnectPlug.Set("snap-setup", supContentPlug)
	chg.AddTask(tConnectPlug)

	tConnectSlot := s.state.NewTask("auto-connect", "...")
	tConnectSlot.Set("snap-setup", supContentSlot)
	chg.AddTask(tConnectSlot)

	// run the change
	s.state.Unlock()
	for i := 0; i < 5; i++ {
		mgr.Ensure()
		mgr.Wait()
	}

	// change did a retry
	s.state.Lock()
	c.Check(tConnectPlug.Status(), Equals, state.DoingStatus)

	// pretent install of content slot is done
	tInstSlot.SetStatus(state.DoneStatus)
	// wait for contentLinkRetryTimeout
	time.Sleep(10 * time.Millisecond)

	s.state.Unlock()

	// run again
	for i := 0; i < 5; i++ {
		mgr.Ensure()
		mgr.Wait()
	}

	// check that the connect plug task is now in done state
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tConnectPlug.Status(), Equals, state.DoneStatus)
}

func (s *interfaceManagerSuite) TestSnapsWithSecurityProfiles(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si0 := &snap.SideInfo{
		RealName: "snap0",
		Revision: snap.R(10),
	}
	snaptest.MockSnap(c, `name: snap0`, si0)
	snapstate.Set(s.state, "snap0", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si0},
		Current:  si0.Revision,
	})

	snaps := []struct {
		name        string
		setupStatus state.Status
		linkStatus  state.Status
	}{
		{"snap0", state.DoneStatus, state.DoneStatus},
		{"snap1", state.DoneStatus, state.DoStatus},
		{"snap2", state.DoneStatus, state.ErrorStatus},
		{"snap3", state.DoneStatus, state.UndoingStatus},
		{"snap4", state.DoingStatus, state.DoStatus},
		{"snap6", state.DoStatus, state.DoStatus},
	}

	for i, snp := range snaps {
		var si *snap.SideInfo

		if snp.name != "snap0" {
			si = &snap.SideInfo{
				RealName: snp.name,
				Revision: snap.R(i),
			}
			snaptest.MockSnap(c, "name: "+snp.name, si)
		}

		chg := s.state.NewChange("linking", "linking 1")
		t1 := s.state.NewTask("setup-profiles", "setup profiles 1")
		t1.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: si,
		})
		t1.SetStatus(snp.setupStatus)
		t2 := s.state.NewTask("link-snap", "link snap 1")
		t2.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: si,
		})
		t2.WaitFor(t1)
		t2.SetStatus(snp.linkStatus)
		chg.AddTask(t1)
		chg.AddTask(t2)
	}

	infos, err := ifacestate.SnapsWithSecurityProfiles(s.state)
	c.Assert(err, IsNil)
	c.Check(infos, HasLen, 3)
	got := make(map[string]snap.Revision)
	for _, info := range infos {
		got[info.InstanceName()] = info.Revision
	}
	c.Check(got, DeepEquals, map[string]snap.Revision{
		"snap0": snap.R(10),
		"snap1": snap.R(1),
		"snap3": snap.R(3),
	})
}

func (s *interfaceManagerSuite) setupGadgetConnect(c *C) {
	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnapDecl(c, "consumer", "publisher1", nil)
	s.mockSnap(c, consumerYaml)
	s.mockSnapDecl(c, "producer", "publisher2", nil)
	s.mockSnap(c, producerYaml)

	gadgetInfo := s.mockSnap(c, `name: gadget
type: gadget
`)

	gadgetYaml := []byte(`
connections:
   - plug: consumeridididididididididididid:plug
     slot: produceridididididididididididid:slot

volumes:
    volume-id:
        bootloader: grub
`)

	err := ioutil.WriteFile(filepath.Join(gadgetInfo.MountDir(), "meta", "gadget.yaml"), gadgetYaml, 0644)
	c.Assert(err, IsNil)

}

func (s *interfaceManagerSuite) TestGadgetConnect(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.setupGadgetConnect(c)
	mgr := s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("gadget-connect", "gadget connections")
	chg.AddTask(t)

	s.state.Unlock()
	mgr.Ensure()
	mgr.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 6)

	gotConnect := false
	for _, t := range tasks {
		switch t.Kind() {
		default:
			c.Fatalf("unexpected task kind: %s", t.Kind())
		case "gadget-connect":
		case "run-hook":
		case "connect":
			gotConnect = true
			var autoConnect, byGadget bool
			err := t.Get("auto", &autoConnect)
			c.Assert(err, IsNil)
			err = t.Get("by-gadget", &byGadget)
			c.Assert(err, IsNil)
			c.Check(autoConnect, Equals, true)
			c.Check(byGadget, Equals, true)

			var plug interfaces.PlugRef
			err = t.Get("plug", &plug)
			c.Assert(err, IsNil)
			c.Assert(plug.Snap, Equals, "consumer")
			c.Assert(plug.Name, Equals, "plug")
			var slot interfaces.SlotRef
			err = t.Get("slot", &slot)
			c.Assert(err, IsNil)
			c.Assert(slot.Snap, Equals, "producer")
			c.Assert(slot.Name, Equals, "slot")
		}
	}

	c.Assert(gotConnect, Equals, true)
}

func (s *interfaceManagerSuite) TestGadgetConnectAlreadyConnected(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.setupGadgetConnect(c)
	mgr := s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test", "auto": true,
		},
	})

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("gadget-connect", "gadget connections")
	chg.AddTask(t)

	s.state.Unlock()
	mgr.Ensure()
	mgr.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Check(chg.Status().Ready(), Equals, true)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)
}

func (s *interfaceManagerSuite) TestGadgetConnectConflictRetry(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.setupGadgetConnect(c)
	mgr := s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	otherChg := s.state.NewChange("other-chg", "...")
	t := s.state.NewTask("link-snap", "...")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "producer"},
	})
	otherChg.AddTask(t)

	chg := s.state.NewChange("setting-up", "...")
	t = s.state.NewTask("gadget-connect", "gadget connections")
	chg.AddTask(t)

	s.state.Unlock()
	mgr.Ensure()
	mgr.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Check(chg.Status().Ready(), Equals, false)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)

	c.Check(t.Status(), Equals, state.DoingStatus)
	c.Check(t.Log()[0], Matches, `.*gadget connect will be retried because of "consumer" - "producer" conflict`)
}

func (s *interfaceManagerSuite) TestGadgetConnectSkipUnknown(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnapDecl(c, "consumer", "publisher1", nil)
	s.mockSnap(c, consumerYaml)
	s.mockSnapDecl(c, "producer", "publisher2", nil)
	s.mockSnap(c, producerYaml)

	mgr := s.manager(c)

	gadgetInfo := s.mockSnap(c, `name: gadget
type: gadget
`)

	gadgetYaml := []byte(`
connections:
   - plug: consumeridididididididididididid:plug
     slot: produceridididididididididididid:unknown
   - plug: unknownididididididididididididi:plug
     slot: produceridididididididididididid:slot

volumes:
    volume-id:
        bootloader: grub
`)

	err := ioutil.WriteFile(filepath.Join(gadgetInfo.MountDir(), "meta", "gadget.yaml"), gadgetYaml, 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("gadget-connect", "gadget connections")
	chg.AddTask(t)

	s.state.Unlock()
	mgr.Ensure()
	mgr.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)

	logs := t.Log()
	c.Check(logs, HasLen, 2)
	c.Check(logs[0], Matches, `.*ignoring missing slot produceridididididididididididid:unknown`)
	c.Check(logs[1], Matches, `.* ignoring missing plug unknownididididididididididididi:plug`)
}

func (s *interfaceManagerSuite) TestGadgetConnectHappyPolicyChecks(c *C) {
	// network-control does not auto-connect so this test also
	// checks that the right policy checker (for "*-connection"
	// rules) is used for gadget connections
	r1 := release.MockOnClassic(false)
	defer r1()

	s.mockSnap(c, coreSnapYaml)

	s.mockSnapDecl(c, "foo", "publisher1", nil)
	s.mockSnap(c, `name: foo
version: 1.0
plugs:
  network-control:
`)

	mgr := s.manager(c)

	gadgetInfo := s.mockSnap(c, `name: gadget
type: gadget
`)

	gadgetYaml := []byte(`
connections:
   - plug: fooididididididididididididididi:network-control

volumes:
    volume-id:
        bootloader: grub
`)

	err := ioutil.WriteFile(filepath.Join(gadgetInfo.MountDir(), "meta", "gadget.yaml"), gadgetYaml, 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("gadget-connect", "gadget connections")
	chg.AddTask(t)

	s.state.Unlock()
	mgr.Ensure()
	mgr.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 6)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status().Ready(), Equals, true)

	// check connection
	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, HasLen, 1)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"foo:network-control core:network-control": map[string]interface{}{
			"interface": "network-control", "auto": true, "by-gadget": true,
		},
	})
}
