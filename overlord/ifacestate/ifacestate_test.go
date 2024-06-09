// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/ifacestate/udevmonitor"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	seccomp_compiler "github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func TestInterfaceManager(t *testing.T) { TestingT(t) }

type cleaner interface {
	AddCleanup(func())
}

type AssertsMock struct {
	Db           *asserts.Database
	storeSigning *assertstest.StoreStack
	st           *state.State

	cleaner cleaner
}

func (am *AssertsMock) SetupAsserts(c *C, st *state.State, cleaner cleaner) {
	am.st = st
	am.cleaner = cleaner
	am.storeSigning = assertstest.NewStoreStack("canonical", nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   am.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	am.Db = db
	err = db.Add(am.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	st.Lock()
	assertstate.ReplaceDB(st, am.Db)
	st.Unlock()
}

func (am *AssertsMock) mockModel(extraHeaders map[string]interface{}) *asserts.Model {
	model := map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"gadget":       "gadget",
		"kernel":       "krnl",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	return assertstest.FakeAssertion(model, extraHeaders).(*asserts.Model)
}

func (am *AssertsMock) MockModel(c *C, extraHeaders map[string]interface{}) {
	model := am.mockModel(extraHeaders)
	am.cleaner.AddCleanup(snapstatetest.MockDeviceModel(model))
}

func (am *AssertsMock) TrivialDeviceContext(c *C, extraHeaders map[string]interface{}) *snapstatetest.TrivialDeviceContext {
	model := am.mockModel(extraHeaders)
	return &snapstatetest.TrivialDeviceContext{DeviceModel: model}
}

func (am *AssertsMock) MockSnapDecl(c *C, name, publisher string, extraHeaders map[string]interface{}) {
	_, err := am.Db.Find(asserts.AccountType, map[string]string{
		"account-id": publisher,
	})
	if errors.Is(err, &asserts.NotFoundError{}) {
		acct := assertstest.NewAccount(am.storeSigning, publisher, map[string]interface{}{
			"account-id": publisher,
		}, "")
		err = am.Db.Add(acct)
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

	snapDecl, err := am.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = am.Db.Add(snapDecl)
	c.Assert(err, IsNil)
}

func (am *AssertsMock) MockStore(c *C, st *state.State, storeID string, extraHeaders map[string]interface{}) {
	headers := map[string]interface{}{
		"store":       storeID,
		"operator-id": am.storeSigning.AuthorityID,
		"timestamp":   time.Now().Format(time.RFC3339),
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	storeAs, err := am.storeSigning.Sign(asserts.StoreType, headers, nil, "")
	c.Assert(err, IsNil)
	st.Lock()
	defer st.Unlock()
	err = assertstate.Add(st, storeAs)
	c.Assert(err, IsNil)
}

type interfaceManagerSuite struct {
	testutil.BaseTest
	AssertsMock
	o              *overlord.Overlord
	state          *state.State
	se             *overlord.StateEngine
	privateMgr     *ifacestate.InterfaceManager
	privateHookMgr *hookstate.HookManager
	extraIfaces    []interfaces.Interface
	extraBackends  []interfaces.SecurityBackend
	secBackend     *ifacetest.TestSecurityBackend
	mockSnapCmd    *testutil.MockCmd
	log            *bytes.Buffer
	coreSnap       *interfaces.SnapAppSet
	snapdSnap      *interfaces.SnapAppSet

	consumer     *interfaces.SnapAppSet
	consumerPlug *snap.PlugInfo

	producer     *interfaces.SnapAppSet
	producerSlot *snap.SlotInfo
}

var _ = Suite(&interfaceManagerSuite{})

const consumerYaml4 = `
name: consumer
version: 0
apps:
    app:
hooks:
    configure:
plugs:
    plug:
        interface: interface
        label: label
        attr: value
`

const producerYaml4 = `
name: producer
version: 0
apps:
    app:
hooks:
    configure:
slots:
    slot:
        interface: interface
        label: label
        attr: value
plugs:
    self:
        interface: interface
        label: label
`

func (s *interfaceManagerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.mockSnapCmd = testutil.MockCommand(c, "snap", "")

	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapSystemKeyFile), 0755), IsNil)

	// needed for system key generation
	s.AddCleanup(osutil.MockMountInfo(""))

	s.o = overlord.Mock()
	s.state = s.o.State()
	s.se = s.o.StateEngine()

	s.SetupAsserts(c, s.state, &s.BaseTest)

	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.state.Lock()
	defer s.state.Unlock()

	s.privateHookMgr = nil
	s.privateMgr = nil
	s.extraIfaces = nil
	s.extraBackends = nil
	s.secBackend = &ifacetest.TestSecurityBackend{}
	// TODO: transition this so that we don't load real backends and instead
	// just load the test backend here and this is nicely integrated with
	// extraBackends above.
	s.BaseTest.AddCleanup(ifacestate.MockSecurityBackends([]interfaces.SecurityBackend{s.secBackend}))
	s.secBackend.SetupCalls = nil

	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf

	s.BaseTest.AddCleanup(ifacestate.MockConnectRetryTimeout(0))
	restore = seccomp_compiler.MockCompilerVersionInfo("abcdef 1.2.3 1234abcd -")
	s.BaseTest.AddCleanup(restore)

	// NOTE: The core snap has a slot so that it shows up in the
	// repository. The repository doesn't record snaps unless they
	// have at least one interface.
	s.coreSnap = ifacetest.MockInfoAndAppSet(c, `
name: core
version: 0
type: os
slots:
    slot:
        interface: interface
`, nil, nil)
	s.snapdSnap = ifacetest.MockInfoAndAppSet(c, `
name: snapd
version: 0
type: app
slots:
    slot:
        interface: interface
`, nil, nil)

	s.consumer = ifacetest.MockInfoAndAppSet(c, consumerYaml4, nil, nil)
	s.consumerPlug = s.consumer.Info().Plugs["plug"]
	s.producer = ifacetest.MockInfoAndAppSet(c, producerYaml4, nil, nil)
	s.producerSlot = s.producer.Info().Slots["slot"]
	s.AddCleanup(ifacestate.MockSnapdAppArmorServiceIsDisabled(func() bool {
		// pretend the snapd.apparmor.service is enabled
		return false
	}))
}

func (s *interfaceManagerSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)

	s.mockSnapCmd.Restore()

	if s.privateMgr != nil {
		s.se.Stop()
	}
	dirs.SetRootDir("")
}

func addForeignTaskHandlers(runner *state.TaskRunner) {
	// Add handler to test full aborting of changes
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	runner.AddHandler("error-trigger", erroringHandler, nil)
}

func (s *interfaceManagerSuite) manager(c *C) *ifacestate.InterfaceManager {
	if s.privateMgr == nil {
		mgr, err := ifacestate.Manager(s.state, s.hookManager(c), s.o.TaskRunner(), s.extraIfaces, s.extraBackends)
		c.Assert(err, IsNil)
		addForeignTaskHandlers(s.o.TaskRunner())
		mgr.DisableUDevMonitor()
		s.privateMgr = mgr
		s.o.AddManager(mgr)

		s.o.AddManager(s.o.TaskRunner())

		c.Assert(s.o.StartUp(), IsNil)

		// ensure the re-generation of security profiles did not
		// confuse the tests
		s.secBackend.SetupCalls = nil
	}
	return s.privateMgr
}

func (s *interfaceManagerSuite) hookManager(c *C) *hookstate.HookManager {
	if s.privateHookMgr == nil {
		mgr, err := hookstate.Manager(s.state, s.o.TaskRunner())
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
	s.manager(c)
	s.se.Ensure()
	s.se.Wait()
}

func (s *interfaceManagerSuite) TestRepoAvailable(c *C) {
	_ = s.manager(c)
	s.state.Lock()
	defer s.state.Unlock()
	repo := ifacerepo.Get(s.state)
	c.Check(repo, FitsTypeOf, &interfaces.Repository{})
}

func (s *interfaceManagerSuite) TestConnectTask(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	var hookSetup, undoHookSetup hookstate.HookSetup
	c.Assert(task.Get("hook-setup", &hookSetup), IsNil)
	c.Assert(hookSetup, Equals, hookstate.HookSetup{Snap: "consumer", Hook: "prepare-plug-plug", Optional: true})
	c.Assert(task.Get("undo-hook-setup", &undoHookSetup), IsNil)
	c.Assert(undoHookSetup, Equals, hookstate.HookSetup{Snap: "consumer", Hook: "unprepare-plug-plug", Optional: true, IgnoreError: true})
	i++
	task = ts.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	c.Assert(task.Get("hook-setup", &hookSetup), IsNil)
	c.Assert(hookSetup, Equals, hookstate.HookSetup{Snap: "producer", Hook: "prepare-slot-slot", Optional: true})
	c.Assert(task.Get("undo-hook-setup", &undoHookSetup), IsNil)
	c.Assert(undoHookSetup, Equals, hookstate.HookSetup{Snap: "producer", Hook: "unprepare-slot-slot", Optional: true, IgnoreError: true})
	i++
	task = ts.Tasks()[i]
	c.Assert(task.Kind(), Equals, "connect")
	var flag bool
	c.Assert(task.Get("auto", &flag), testutil.ErrorIs, state.ErrNoState)
	c.Assert(task.Get("delayed-setup-profiles", &flag), testutil.ErrorIs, state.ErrNoState)
	c.Assert(task.Get("by-gadget", &flag), testutil.ErrorIs, state.ErrNoState)
	var plug interfaces.PlugRef
	c.Assert(task.Get("plug", &plug), IsNil)
	c.Assert(plug.Snap, Equals, "consumer")
	c.Assert(plug.Name, Equals, "plug")
	var slot interfaces.SlotRef
	c.Assert(task.Get("slot", &slot), IsNil)
	c.Assert(slot.Snap, Equals, "producer")
	c.Assert(slot.Name, Equals, "slot")

	// "connect" task edge is not present
	_, err = ts.Edge(ifacestate.ConnectTaskEdge)
	c.Assert(err, ErrorMatches, `internal error: missing .* edge in task set`)

	var autoconnect bool
	err = task.Get("auto", &autoconnect)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
	c.Assert(autoconnect, Equals, false)

	// verify initial attributes are present in connect task
	var plugStaticAttrs map[string]interface{}
	var plugDynamicAttrs map[string]interface{}
	c.Assert(task.Get("plug-static", &plugStaticAttrs), IsNil)
	c.Assert(plugStaticAttrs, DeepEquals, map[string]interface{}{"attr1": "value1"})
	c.Assert(task.Get("plug-dynamic", &plugDynamicAttrs), IsNil)
	c.Assert(plugDynamicAttrs, DeepEquals, map[string]interface{}{})

	var slotStaticAttrs map[string]interface{}
	var slotDynamicAttrs map[string]interface{}
	c.Assert(task.Get("slot-static", &slotStaticAttrs), IsNil)
	c.Assert(slotStaticAttrs, DeepEquals, map[string]interface{}{"attr2": "value2"})
	c.Assert(task.Get("slot-dynamic", &slotDynamicAttrs), IsNil)
	c.Assert(slotDynamicAttrs, DeepEquals, map[string]interface{}{})

	i++
	task = ts.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	c.Assert(task.Get("hook-setup", &hs), IsNil)
	c.Assert(hs, Equals, hookstate.HookSetup{Snap: "producer", Hook: "connect-slot-slot", Optional: true})
	c.Assert(task.Get("undo-hook-setup", &undoHookSetup), IsNil)
	c.Assert(undoHookSetup, Equals, hookstate.HookSetup{Snap: "producer", Hook: "disconnect-slot-slot", Optional: true, IgnoreError: true})
	i++
	task = ts.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	c.Assert(task.Get("hook-setup", &hs), IsNil)
	c.Assert(hs, Equals, hookstate.HookSetup{Snap: "consumer", Hook: "connect-plug-plug", Optional: true})
	c.Assert(task.Get("undo-hook-setup", &undoHookSetup), IsNil)
	c.Assert(undoHookSetup, Equals, hookstate.HookSetup{Snap: "consumer", Hook: "disconnect-plug-plug", Optional: true, IgnoreError: true})

	// after-connect-hooks task edge is not present
	_, err = ts.Edge(ifacestate.AfterConnectHooksEdge)
	c.Assert(err, ErrorMatches, `internal error: missing .* edge in task set`)
}

func (s *interfaceManagerSuite) TestConnectTasksDelayProfilesFlag(c *C) {
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()

	ts, err := ifacestate.ConnectPriv(s.state, "consumer", "plug", "producer", "slot", ifacestate.NewConnectOptsWithDelayProfilesSet())
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 5)
	connectTask := ts.Tasks()[2]
	c.Assert(connectTask.Kind(), Equals, "connect")
	var delayedSetupProfiles bool
	connectTask.Get("delayed-setup-profiles", &delayedSetupProfiles)
	c.Assert(delayedSetupProfiles, Equals, true)
}

func (s *interfaceManagerSuite) TestBatchConnectTasks(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, consumer2Yaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	snapsup := &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "snap"}}
	conns := make(map[string]*interfaces.ConnRef)
	connOpts := make(map[string]*ifacestate.ConnectOpts)

	// no connections and tasks created (also, no stray tasks in the state)
	ts, hasInterfaceHooks, err := ifacestate.BatchConnectTasks(s.state, snapsup, conns, connOpts)
	c.Assert(err, IsNil)
	c.Check(ts, IsNil)
	c.Check(hasInterfaceHooks, Equals, false)
	// state.TaskCount() is the only way of checking for stray tasks without a
	// change (state.Tasks() filters those out).
	c.Assert(s.state.TaskCount(), Equals, 0)

	// two connections
	cref1 := interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}
	cref2 := interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer2", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}
	conns[cref1.ID()] = &cref1
	conns[cref2.ID()] = &cref2
	// connOpts for cref1 will default to AutoConnect: true
	connOpts[cref2.ID()] = &ifacestate.ConnectOpts{AutoConnect: true, ByGadget: true}

	ts, hasInterfaceHooks, err = ifacestate.BatchConnectTasks(s.state, snapsup, conns, connOpts)
	c.Assert(err, IsNil)
	c.Check(ts.Tasks(), HasLen, 9)
	c.Check(hasInterfaceHooks, Equals, true)

	// "setup-profiles" task waits for "connect" tasks of both connections
	setupProfiles := ts.Tasks()[len(ts.Tasks())-1]
	c.Assert(setupProfiles.Kind(), Equals, "setup-profiles")

	wt := setupProfiles.WaitTasks()
	c.Assert(wt, HasLen, 2)
	for i := 0; i < 2; i++ {
		c.Check(wt[i].Kind(), Equals, "connect")
		// validity, check flags on "connect" tasks
		var flag bool
		c.Assert(wt[i].Get("delayed-setup-profiles", &flag), IsNil)
		c.Check(flag, Equals, true)
		c.Assert(wt[i].Get("auto", &flag), IsNil)
		c.Check(flag, Equals, true)
		// ... validity by-gadget flag
		var plugRef interfaces.PlugRef
		c.Check(wt[i].Get("plug", &plugRef), IsNil)
		err := wt[i].Get("by-gadget", &flag)
		if plugRef.Snap == "consumer2" {
			c.Assert(err, IsNil)
			c.Check(flag, Equals, true)
		} else {
			c.Assert(err, testutil.ErrorIs, state.ErrNoState)
		}

	}

	// connect-slot-slot hooks wait for "setup-profiles"
	ht := setupProfiles.HaltTasks()
	c.Assert(ht, HasLen, 2)
	for i := 0; i < 2; i++ {
		c.Check(ht[i].Kind(), Equals, "run-hook")
		c.Check(ht[i].Summary(), Matches, "Run hook connect-slot-slot .*")
	}
}

func (s *interfaceManagerSuite) TestBatchConnectTasksNoHooks(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumer2Yaml)
	s.mockSnap(c, producer2Yaml)
	_ = s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	snapsup := &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "snap"}}
	conns := make(map[string]*interfaces.ConnRef)
	connOpts := make(map[string]*ifacestate.ConnectOpts)

	// a connection
	cref := interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer2", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "producer2", Name: "slot"}}
	conns[cref.ID()] = &cref

	ts, hasInterfaceHooks, err := ifacestate.BatchConnectTasks(s.state, snapsup, conns, connOpts)
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 2)
	c.Check(ts.Tasks()[0].Kind(), Equals, "connect")
	c.Check(ts.Tasks()[1].Kind(), Equals, "setup-profiles")
	c.Check(hasInterfaceHooks, Equals, false)
}

type interfaceHooksTestData struct {
	consumer  []string
	producer  []string
	waitChain []string
}

func hookNameOrTaskKind(c *C, t *state.Task) string {
	if t.Kind() == "run-hook" {
		var hookSup hookstate.HookSetup
		c.Assert(t.Get("hook-setup", &hookSup), IsNil)
		return fmt.Sprintf("hook:%s", hookSup.Hook)
	}
	return fmt.Sprintf("task:%s", t.Kind())
}

func testInterfaceHooksTasks(c *C, tasks []*state.Task, waitChain []string, undoHooks map[string]string) {
	for i, t := range tasks {
		c.Assert(waitChain[i], Equals, hookNameOrTaskKind(c, t))
		waits := t.WaitTasks()
		if i == 0 {
			c.Assert(waits, HasLen, 0)
		} else {
			c.Assert(waits, HasLen, 1)
			waiting := hookNameOrTaskKind(c, waits[0])
			// check that this task waits on previous one
			c.Assert(waiting, Equals, waitChain[i-1])
		}

		// check undo hook setup if applicable
		if t.Kind() == "run-hook" {
			var hooksup hookstate.HookSetup
			var undosup hookstate.HookSetup
			c.Assert(t.Get("hook-setup", &hooksup), IsNil)
			c.Assert(t.Get("undo-hook-setup", &undosup), IsNil)
			c.Assert(undosup.Hook, Equals, undoHooks[hooksup.Hook], Commentf("unexpected undo hook: %s", undosup.Hook))
		}
	}

}

var connectHooksTests = []interfaceHooksTestData{{
	consumer:  []string{"prepare-plug-plug"},
	producer:  []string{"prepare-slot-slot"},
	waitChain: []string{"hook:prepare-plug-plug", "hook:prepare-slot-slot", "task:connect"},
}, {
	consumer:  []string{"prepare-plug-plug"},
	producer:  []string{"prepare-slot-slot", "connect-slot-slot"},
	waitChain: []string{"hook:prepare-plug-plug", "hook:prepare-slot-slot", "task:connect", "hook:connect-slot-slot"},
}, {
	consumer:  []string{"prepare-plug-plug"},
	producer:  []string{"connect-slot-slot"},
	waitChain: []string{"hook:prepare-plug-plug", "task:connect", "hook:connect-slot-slot"},
}, {
	consumer:  []string{"connect-plug-plug"},
	producer:  []string{"prepare-slot-slot", "connect-slot-slot"},
	waitChain: []string{"hook:prepare-slot-slot", "task:connect", "hook:connect-slot-slot", "hook:connect-plug-plug"},
}, {
	consumer:  []string{"connect-plug-plug"},
	producer:  []string{"connect-slot-slot"},
	waitChain: []string{"task:connect", "hook:connect-slot-slot", "hook:connect-plug-plug"},
}, {
	consumer:  []string{"prepare-plug-plug", "connect-plug-plug"},
	producer:  []string{"prepare-slot-slot"},
	waitChain: []string{"hook:prepare-plug-plug", "hook:prepare-slot-slot", "task:connect", "hook:connect-plug-plug"},
}, {
	consumer:  []string{"prepare-plug-plug", "connect-plug-plug"},
	producer:  []string{"prepare-slot-slot", "connect-slot-slot"},
	waitChain: []string{"hook:prepare-plug-plug", "hook:prepare-slot-slot", "task:connect", "hook:connect-slot-slot", "hook:connect-plug-plug"},
}}

func (s *interfaceManagerSuite) TestConnectTaskHookdEdges(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})

	_ = s.manager(c)
	for _, hooks := range connectHooksTests {
		var hooksYaml string
		for _, name := range hooks.consumer {
			hooksYaml = fmt.Sprintf("%s %s:\n", hooksYaml, name)
		}
		consumer := fmt.Sprintf(consumerYaml3, hooksYaml)

		hooksYaml = ""
		for _, name := range hooks.producer {
			hooksYaml = fmt.Sprintf("%s %s:\n", hooksYaml, name)
		}
		producer := fmt.Sprintf(producerYaml3, hooksYaml)

		s.mockSnap(c, consumer)
		s.mockSnap(c, producer)

		s.state.Lock()

		ts, err := ifacestate.ConnectPriv(s.state, "consumer", "plug", "producer", "slot", ifacestate.NewConnectOptsWithDelayProfilesSet())
		c.Assert(err, IsNil)

		// check task edges
		edge, err := ts.Edge(ifacestate.ConnectTaskEdge)
		c.Assert(err, IsNil)
		c.Check(edge.Kind(), Equals, "connect")

		// AfterConnectHooks task edge is set on "connect-slot-" or "connect-plug-" hook task (whichever comes first after "connect")
		// and is not present if neither of them exists.
		var expectedAfterConnectEdge string
		for _, hookName := range hooks.producer {
			if strings.HasPrefix(hookName, "connect-") {
				expectedAfterConnectEdge = "hook:" + hookName
			}
		}
		if expectedAfterConnectEdge == "" {
			for _, hookName := range hooks.consumer {
				if strings.HasPrefix(hookName, "connect-") {
					expectedAfterConnectEdge = "hook:" + hookName
				}
			}
		}
		edge, err = ts.Edge(ifacestate.AfterConnectHooksEdge)
		if expectedAfterConnectEdge != "" {
			c.Assert(err, IsNil)
			c.Check(hookNameOrTaskKind(c, edge), Equals, expectedAfterConnectEdge)
		} else {
			c.Assert(err, ErrorMatches, `internal error: missing .* edge in task set`)
		}

		s.state.Unlock()
	}
}

func (s *interfaceManagerSuite) TestConnectTaskHooksConditionals(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})

	_ = s.manager(c)
	for _, hooks := range connectHooksTests {
		var hooksYaml string
		for _, name := range hooks.consumer {
			hooksYaml = fmt.Sprintf("%s %s:\n", hooksYaml, name)
		}
		consumer := fmt.Sprintf(consumerYaml3, hooksYaml)

		hooksYaml = ""
		for _, name := range hooks.producer {
			hooksYaml = fmt.Sprintf("%s %s:\n", hooksYaml, name)
		}
		producer := fmt.Sprintf(producerYaml3, hooksYaml)

		s.mockSnap(c, consumer)
		s.mockSnap(c, producer)

		s.state.Lock()

		ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
		c.Assert(err, IsNil)
		c.Assert(ts.Tasks(), HasLen, len(hooks.producer)+len(hooks.consumer)+1)
		c.Assert(ts.Tasks(), HasLen, len(hooks.waitChain))

		undoHooks := map[string]string{
			"prepare-plug-plug": "unprepare-plug-plug",
			"prepare-slot-slot": "unprepare-slot-slot",
			"connect-plug-plug": "disconnect-plug-plug",
			"connect-slot-slot": "disconnect-slot-slot",
		}

		testInterfaceHooksTasks(c, ts.Tasks(), hooks.waitChain, undoHooks)
		s.state.Unlock()
	}
}

func (s *interfaceManagerSuite) TestDisconnectTaskHooksConditionals(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})

	hooksTests := []interfaceHooksTestData{{
		consumer:  []string{"disconnect-plug-plug"},
		producer:  []string{"disconnect-slot-slot"},
		waitChain: []string{"hook:disconnect-slot-slot", "hook:disconnect-plug-plug", "task:disconnect"},
	}, {
		producer:  []string{"disconnect-slot-slot"},
		waitChain: []string{"hook:disconnect-slot-slot", "task:disconnect"},
	}, {
		consumer:  []string{"disconnect-plug-plug"},
		waitChain: []string{"hook:disconnect-plug-plug", "task:disconnect"},
	}, {
		waitChain: []string{"task:disconnect"},
	}}

	_ = s.manager(c)
	for _, hooks := range hooksTests {
		var hooksYaml string
		for _, name := range hooks.consumer {
			hooksYaml = fmt.Sprintf("%s %s:\n", hooksYaml, name)
		}
		consumer := fmt.Sprintf(consumerYaml3, hooksYaml)

		hooksYaml = ""
		for _, name := range hooks.producer {
			hooksYaml = fmt.Sprintf("%s %s:\n", hooksYaml, name)
		}
		producer := fmt.Sprintf(producerYaml3, hooksYaml)

		plugAppSet := s.mockAppSet(c, consumer)
		slotAppSet := s.mockAppSet(c, producer)

		conn := &interfaces.Connection{
			Plug: interfaces.NewConnectedPlug(plugAppSet.Info().Plugs["plug"], plugAppSet, nil, nil),
			Slot: interfaces.NewConnectedSlot(slotAppSet.Info().Slots["slot"], slotAppSet, nil, nil),
		}

		s.state.Lock()

		ts, err := ifacestate.Disconnect(s.state, conn)
		c.Assert(err, IsNil)
		c.Assert(ts.Tasks(), HasLen, len(hooks.producer)+len(hooks.consumer)+1)
		c.Assert(ts.Tasks(), HasLen, len(hooks.waitChain))

		undoHooks := map[string]string{
			"disconnect-plug-plug": "connect-plug-plug",
			"disconnect-slot-slot": "connect-slot-slot",
		}

		testInterfaceHooksTasks(c, ts.Tasks(), hooks.waitChain, undoHooks)
		s.state.Unlock()
	}
}

func (s *interfaceManagerSuite) TestParallelInstallConnectTask(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnapInstance(c, "consumer_foo", consumerYaml)
	s.mockSnapInstance(c, "producer", producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	ts, err := ifacestate.Connect(s.state, "consumer_foo", "plug", "producer", "slot")
	c.Assert(err, IsNil)

	var hs hookstate.HookSetup
	i := 0
	task := ts.Tasks()[i]
	c.Check(task.Kind(), Equals, "run-hook")
	var hookSetup hookstate.HookSetup
	err = task.Get("hook-setup", &hookSetup)
	c.Assert(err, IsNil)
	c.Assert(hookSetup, Equals, hookstate.HookSetup{Snap: "consumer_foo", Hook: "prepare-plug-plug", Optional: true})
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
	c.Assert(plug.Snap, Equals, "consumer_foo")
	c.Assert(plug.Name, Equals, "plug")
	var slot interfaces.SlotRef
	err = task.Get("slot", &slot)
	c.Assert(err, IsNil)
	c.Assert(slot.Snap, Equals, "producer")
	c.Assert(slot.Name, Equals, "slot")

	var autoconnect bool
	err = task.Get("auto", &autoconnect)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
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
	c.Assert(hs, Equals, hookstate.HookSetup{Snap: "consumer_foo", Hook: "connect-plug-plug", Optional: true})
}

func (s *interfaceManagerSuite) TestConnectAlreadyConnected(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	conns := map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"auto": false,
		},
	}
	s.state.Set("conns", conns)

	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, NotNil)
	c.Assert(ts, IsNil)
	alreadyConnected, ok := err.(*ifacestate.ErrAlreadyConnected)
	c.Assert(ok, Equals, true)
	c.Assert(alreadyConnected.Connection, DeepEquals, interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}})
	c.Assert(err, ErrorMatches, `already connected: "consumer:plug producer:slot"`)

	conns = map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"auto":      true,
			"undesired": true,
		},
	}
	s.state.Set("conns", conns)

	// ErrAlreadyConnected is not reported if connection exists but is undesired
	ts, err = ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)

	conns = map[string]interface{}{"consumer:plug producer:slot": map[string]interface{}{"hotplug-gone": true}}
	s.state.Set("conns", conns)

	// ErrAlreadyConnected is not reported if connection was removed by hotplug
	ts, err = ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)
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

func (s *interfaceManagerSuite) testDisconnectConflicts(c *C, snapName string, otherTaskKind string, expectedErr string) {
	plugAppSet := s.mockAppSet(c, consumerYaml)
	slotAppSet := s.mockAppSet(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("other-chg", "...")
	t := s.state.NewTask(otherTaskKind, "...")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName},
	})
	chg.AddTask(t)

	conn := &interfaces.Connection{
		Plug: interfaces.NewConnectedPlug(plugAppSet.Info().Plugs["plug"], plugAppSet, nil, nil),
		Slot: interfaces.NewConnectedSlot(slotAppSet.Info().Slots["slot"], slotAppSet, nil, nil),
	}

	_, err := ifacestate.Disconnect(s.state, conn)
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
	s.testDisconnectConflicts(c, "consumer", "link-snap", `snap "consumer" has "other-chg" change in progress`)
}

func (s *interfaceManagerSuite) TestDisconnectConflictsSlotSnapOnLink(c *C) {
	s.testDisconnectConflicts(c, "producer", "link-snap", `snap "producer" has "other-chg" change in progress`)
}

func (s *interfaceManagerSuite) TestConnectDoesConflict(c *C) {
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})
	plugAppSet := s.mockAppSet(c, consumerYaml)
	slotAppSet := s.mockAppSet(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("other-connect", "...")
	t := s.state.NewTask("connect", "other connect task")
	t.Set("slot", interfaces.SlotRef{Snap: "producer", Name: "slot"})
	t.Set("plug", interfaces.PlugRef{Snap: "consumer", Name: "plug"})
	chg.AddTask(t)

	_, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, ErrorMatches, `snap "consumer" has "other-connect" change in progress`)

	conn := &interfaces.Connection{
		Plug: interfaces.NewConnectedPlug(plugAppSet.Info().Plugs["plug"], plugAppSet, nil, nil),
		Slot: interfaces.NewConnectedSlot(slotAppSet.Info().Slots["slot"], slotAppSet, nil, nil),
	}

	_, err = ifacestate.Disconnect(s.state, conn)
	c.Assert(err, ErrorMatches, `snap "consumer" has "other-connect" change in progress`)
}

func (s *interfaceManagerSuite) TestConnectBecomeOperationalNoConflict(c *C) {
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("become-operational", "...")
	hooksup := &hookstate.HookSetup{
		Snap: "producer",
		Hook: "prepare-device",
	}
	t := hookstate.HookTask(s.state, "prep", hooksup, nil)
	chg.AddTask(t)

	_, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
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

	ignore, err := ifacestate.FindSymmetricAutoconnectTask(s.state, "consumer", "producer", t)
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, false)
	c.Assert(ifacestate.CheckAutoconnectConflicts(s.state, t, "consumer", "producer"), IsNil)

	ts, err := ifacestate.ConnectPriv(s.state, "consumer", "plug", "producer", "slot", ifacestate.NewConnectOptsWithAutoSet())
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

	ignore, err := ifacestate.FindSymmetricAutoconnectTask(s.state, "consumer", "producer", t2)
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, false)

	return ifacestate.CheckAutoconnectConflicts(s.state, t2, "consumer", "producer")
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

func (s *interfaceManagerSuite) TestAutoconnectConflictOnDiscardSnap(c *C) {
	s.state.Lock()
	task := s.state.NewTask("discard-snap", "")
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

func (s *interfaceManagerSuite) TestAutoconnectConflictOnSetupProfiles(c *C) {
	s.state.Lock()
	task := s.state.NewTask("setup-profiles", "")
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

	ignore, err := ifacestate.FindSymmetricAutoconnectTask(s.state, "consumer", "producer", t1)
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, true)

	ignore, err = ifacestate.FindSymmetricAutoconnectTask(s.state, "consumer", "producer", t2)
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

func (s *interfaceManagerSuite) TestAutoconnectRetryOnConnect(c *C) {
	s.state.Lock()
	task := s.state.NewTask("connect", "")
	task.Set("slot", interfaces.SlotRef{Snap: "producer", Name: "slot"})
	task.Set("plug", interfaces.PlugRef{Snap: "consumer", Name: "plug"})
	task.Set("auto", false)
	s.state.Unlock()

	err := s.createAutoconnectChange(c, task)
	c.Assert(err, ErrorMatches, `task should be retried`)
}

func (s *interfaceManagerSuite) TestAutoconnectIgnoresSetupProfilesPhase2(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	_ = s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	sup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			Revision: snap.R(1),
			RealName: "consumer"},
	}

	chg := s.state.NewChange("install", "...")
	t1 := s.state.NewTask("auto-connect", "...")
	t1.Set("snap-setup", sup)

	t2 := s.state.NewTask("setup-profiles", "...")
	corePhase2 := true
	t2.Set("core-phase-2", corePhase2)
	t2.Set("snap-setup", sup)
	t2.WaitFor(t1)
	chg.AddTask(t1)
	chg.AddTask(t2)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	// auto-connect task is done
	c.Assert(t1.Status(), Equals, state.DoneStatus)
	// change not finished because of hook tasks
	c.Assert(chg.Status(), Equals, state.DoStatus)
}

func (s *interfaceManagerSuite) TestEnsureProcessesConnectTask(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckInterfaceMismatch(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	_ = s.state.NewChange("kind", "summary")
	_, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "whatslot")
	c.Assert(err, ErrorMatches, `snap "producer" has no slot named "whatslot"`)
}

func (s *interfaceManagerSuite) TestConnectTaskNoSuchPlug(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.state.Lock()
	_ = s.state.NewChange("kind", "summary")
	_, err := ifacestate.Connect(s.state, "consumer", "whatplug", "producer", "slot")
	c.Assert(err, ErrorMatches, `snap "consumer" has no plug named "whatplug"`)
}

func (s *interfaceManagerSuite) TestConnectTaskCheckNotAllowed(c *C) {
	s.MockModel(c, nil)

	s.testConnectTaskCheck(c, func() {
		s.MockSnapDecl(c, "consumer", "consumer-publisher", nil)
		s.mockSnap(c, consumerYaml)
		s.MockSnapDecl(c, "producer", "producer-publisher", nil)
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
	s.MockModel(c, nil)

	s.testConnectTaskCheck(c, func() {
		s.mockSnap(c, consumerYaml)
		s.mockSnap(c, producerYaml)
	}, func(change *state.Change) {
		c.Check(change.Err(), IsNil)
		c.Check(change.Status(), Equals, state.DoneStatus)

		repo := s.manager(c).Repository()
		ifaces := repo.Interfaces()
		c.Assert(ifaces.Connections, HasLen, 1)
		c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{
			PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
			SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
	})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckAllowed(c *C) {
	s.MockModel(c, nil)

	s.testConnectTaskCheck(c, func() {
		s.MockSnapDecl(c, "consumer", "one-publisher", nil)
		s.mockSnap(c, consumerYaml)
		s.MockSnapDecl(c, "producer", "one-publisher", nil)
		s.mockSnap(c, producerYaml)
	}, func(change *state.Change) {
		c.Assert(change.Err(), IsNil)
		c.Check(change.Status(), Equals, state.DoneStatus)

		repo := s.manager(c).Repository()
		ifaces := repo.Interfaces()
		c.Assert(ifaces.Connections, HasLen, 1)
		c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{
			PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
			SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
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
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})

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

func (s *interfaceManagerSuite) TestConnectTaskCheckDeviceScopeNoStore(c *C) {
	s.MockModel(c, nil)

	s.testConnectTaskCheckDeviceScope(c, func(change *state.Change) {
		c.Check(change.Err(), ErrorMatches, `(?s).*connection not allowed by plug rule of interface "test".*`)
		c.Check(change.Status(), Equals, state.ErrorStatus)

		repo := s.manager(c).Repository()
		ifaces := repo.Interfaces()
		c.Check(ifaces.Connections, HasLen, 0)
	})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckDeviceScopeWrongStore(c *C) {
	s.MockModel(c, map[string]interface{}{
		"store": "other-store",
	})

	s.testConnectTaskCheckDeviceScope(c, func(change *state.Change) {
		c.Check(change.Err(), ErrorMatches, `(?s).*connection not allowed by plug rule of interface "test".*`)
		c.Check(change.Status(), Equals, state.ErrorStatus)

		repo := s.manager(c).Repository()
		ifaces := repo.Interfaces()
		c.Check(ifaces.Connections, HasLen, 0)
	})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckDeviceScopeRightStore(c *C) {
	s.MockModel(c, map[string]interface{}{
		"store": "my-store",
	})

	s.testConnectTaskCheckDeviceScope(c, func(change *state.Change) {
		c.Assert(change.Err(), IsNil)
		c.Check(change.Status(), Equals, state.DoneStatus)

		repo := s.manager(c).Repository()
		ifaces := repo.Interfaces()
		c.Assert(ifaces.Connections, HasLen, 1)
		c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{
			PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
			SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
	})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckDeviceScopeWrongFriendlyStore(c *C) {
	s.MockModel(c, map[string]interface{}{
		"store": "my-substore",
	})

	s.MockStore(c, s.state, "my-substore", map[string]interface{}{
		"friendly-stores": []interface{}{"other-store"},
	})

	s.testConnectTaskCheckDeviceScope(c, func(change *state.Change) {
		c.Check(change.Err(), ErrorMatches, `(?s).*connection not allowed by plug rule of interface "test".*`)
		c.Check(change.Status(), Equals, state.ErrorStatus)

		repo := s.manager(c).Repository()
		ifaces := repo.Interfaces()
		c.Check(ifaces.Connections, HasLen, 0)
	})
}

func (s *interfaceManagerSuite) TestConnectTaskCheckDeviceScopeRightFriendlyStore(c *C) {
	s.MockModel(c, map[string]interface{}{
		"store": "my-substore",
	})

	s.MockStore(c, s.state, "my-substore", map[string]interface{}{
		"friendly-stores": []interface{}{"my-store"},
	})

	s.testConnectTaskCheckDeviceScope(c, func(change *state.Change) {
		c.Assert(change.Err(), IsNil)
		c.Check(change.Status(), Equals, state.DoneStatus)

		repo := s.manager(c).Repository()
		ifaces := repo.Interfaces()
		c.Assert(ifaces.Connections, HasLen, 1)
		c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{
			PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
			SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
	})
}

func (s *interfaceManagerSuite) testConnectTaskCheckDeviceScope(c *C, check func(*state.Change)) {
	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    allow-connection: false
`))
	defer restore()
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})

	s.MockSnapDecl(c, "producer", "one-publisher", nil)
	s.mockSnap(c, producerYaml)
	s.MockSnapDecl(c, "consumer", "one-publisher", map[string]interface{}{
		"format": "3",
		"plugs": map[string]interface{}{
			"test": map[string]interface{}{
				"allow-connection": map[string]interface{}{
					"on-store": []interface{}{"my-store"},
				},
			},
		},
	})
	s.mockSnap(c, consumerYaml)

	s.manager(c)

	s.state.Lock()
	change := s.state.NewChange("kind", "summary")
	ts, err := ifacestate.Connect(s.state, "consumer", "plug", "producer", "slot")
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 5)

	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	check(change)
}

func (s *interfaceManagerSuite) TestDisconnectTask(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	plugAppSet := s.mockAppSet(c, consumerYaml)
	slotAppSet := s.mockAppSet(c, producerYaml)

	conn := &interfaces.Connection{
		Plug: interfaces.NewConnectedPlug(plugAppSet.Info().Plugs["plug"], plugAppSet, nil, map[string]interface{}{"attr3": "value3"}),
		Slot: interfaces.NewConnectedSlot(slotAppSet.Info().Slots["slot"], slotAppSet, nil, map[string]interface{}{"attr4": "value4"}),
	}

	s.state.Lock()
	defer s.state.Unlock()

	ts, err := ifacestate.Disconnect(s.state, conn)
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 3)

	var hookSetup, undoHookSetup hookstate.HookSetup
	task := ts.Tasks()[0]
	c.Assert(task.Kind(), Equals, "run-hook")
	c.Assert(task.Get("hook-setup", &hookSetup), IsNil)
	c.Assert(hookSetup, Equals, hookstate.HookSetup{Snap: "producer", Hook: "disconnect-slot-slot", Optional: true, IgnoreError: false})
	c.Assert(task.Get("undo-hook-setup", &undoHookSetup), IsNil)
	c.Assert(undoHookSetup, Equals, hookstate.HookSetup{Snap: "producer", Hook: "connect-slot-slot", Optional: true, IgnoreError: false})

	task = ts.Tasks()[1]
	c.Assert(task.Kind(), Equals, "run-hook")
	err = task.Get("hook-setup", &hookSetup)
	c.Assert(err, IsNil)
	c.Assert(hookSetup, Equals, hookstate.HookSetup{Snap: "consumer", Hook: "disconnect-plug-plug", Optional: true})
	c.Assert(task.Get("undo-hook-setup", &undoHookSetup), IsNil)
	c.Assert(undoHookSetup, Equals, hookstate.HookSetup{Snap: "consumer", Hook: "connect-plug-plug", Optional: true, IgnoreError: false})

	task = ts.Tasks()[2]
	c.Assert(task.Kind(), Equals, "disconnect")
	var autoDisconnect bool
	c.Assert(task.Get("auto-disconnect", &autoDisconnect), testutil.ErrorIs, state.ErrNoState)
	c.Assert(autoDisconnect, Equals, false)

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

	// verify connection attributes are present in the disconnect task
	var plugStaticAttrs1, plugDynamicAttrs1, slotStaticAttrs1, slotDynamicAttrs1 map[string]interface{}

	c.Assert(task.Get("plug-static", &plugStaticAttrs1), IsNil)
	c.Assert(plugStaticAttrs1, DeepEquals, map[string]interface{}{"attr1": "value1"})
	c.Assert(task.Get("plug-dynamic", &plugDynamicAttrs1), IsNil)
	c.Assert(plugDynamicAttrs1, DeepEquals, map[string]interface{}{"attr3": "value3"})

	c.Assert(task.Get("slot-static", &slotStaticAttrs1), IsNil)
	c.Assert(slotStaticAttrs1, DeepEquals, map[string]interface{}{"attr2": "value2"})
	c.Assert(task.Get("slot-dynamic", &slotDynamicAttrs1), IsNil)
	c.Assert(slotDynamicAttrs1, DeepEquals, map[string]interface{}{"attr4": "value4"})
}

// Disconnect works when both plug and slot are specified
func (s *interfaceManagerSuite) TestDisconnectFull(c *C) {
	s.testDisconnect(c, "consumer", "plug", "producer", "slot")
}

func (s *interfaceManagerSuite) getConnection(c *C, plugSnap, plugName, slotSnap, slotName string) *interfaces.Connection {
	conn, err := s.manager(c).Repository().Connection(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: plugSnap, Name: plugName},
		SlotRef: interfaces.SlotRef{Snap: slotSnap, Name: slotName},
	})
	c.Assert(err, IsNil)
	c.Assert(conn, NotNil)
	return conn
}

func (s *interfaceManagerSuite) testDisconnect(c *C, plugSnap, plugName, slotSnap, slotName string) {
	// Put two snaps in place They consumer has an plug that can be connected
	// to slot on the producer.
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	consumer := s.mockSnap(c, consumerWithComponentYaml)
	producer := s.mockSnap(c, producerWithComponentYaml)

	s.mockComponentForSnap(c, "comp", "component: consumer+comp\ntype: test", consumer)
	s.mockComponentForSnap(c, "comp", "component: producer+comp\ntype: test", producer)

	// Put a connection in the state so that it automatically gets set up when
	// we create the manager.
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	// Initialize the manager. This registers both snaps and reloads the connection.
	mgr := s.manager(c)

	conn := s.getConnection(c, plugSnap, plugName, slotSnap, slotName)

	// Run the disconnect task and let it finish.
	s.state.Lock()
	change := s.state.NewChange("disconnect", "...")
	ts, err := ifacestate.Disconnect(s.state, conn)
	ts.Tasks()[0].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	c.Assert(err, IsNil)
	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Assert(change.Tasks(), HasLen, 3)
	task := change.Tasks()[2]
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

	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, DeepEquals, interfaces.ConfinementOptions{})

	consumerAppSet := s.secBackend.SetupCalls[0].AppSet
	c.Check(consumerAppSet.InstanceName(), Equals, "consumer")
	c.Check(consumerAppSet.Runnables(), testutil.DeepUnsortedMatches, consumerRunnablesFullSet)

	producerAppSet := s.secBackend.SetupCalls[1].AppSet
	c.Check(producerAppSet.InstanceName(), Equals, "producer")
	c.Check(producerAppSet.Runnables(), testutil.DeepUnsortedMatches, producerRunnablesFullSet)

}

func (s *interfaceManagerSuite) TestDisconnectUndo(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	var consumerYaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: test
  static: plug-static-value
components:
 comp:
  type: test
  hooks:
   install:
 not-installed:
  type: test
  hooks:
   install:
`
	var producerYaml = `
name: producer
version: 1
slots:
 slot:
  interface: test
  static: slot-static-value
components:
 comp:
  type: test
  hooks:
   install:
 not-installed:
  type: test
  hooks:
   install:
`
	consumer := s.mockSnap(c, consumerYaml)
	producer := s.mockSnap(c, producerYaml)

	s.mockComponentForSnap(c, "comp", "component: consumer+comp\ntype: test", consumer)
	s.mockComponentForSnap(c, "comp", "component: producer+comp\ntype: test", producer)

	connState := map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface":    "test",
			"slot-static":  map[string]interface{}{"static": "slot-static-value"},
			"slot-dynamic": map[string]interface{}{"dynamic": "slot-dynamic-value"},
			"plug-static":  map[string]interface{}{"static": "plug-static-value"},
			"plug-dynamic": map[string]interface{}{"dynamic": "plug-dynamic-value"},
		},
	}

	s.state.Lock()
	s.state.Set("conns", connState)
	s.state.Unlock()

	// Initialize the manager. This registers both snaps and reloads the connection.
	_ = s.manager(c)

	conn := s.getConnection(c, "consumer", "plug", "producer", "slot")

	// Run the disconnect task and let it finish.
	s.state.Lock()
	change := s.state.NewChange("disconnect", "...")
	ts, err := ifacestate.Disconnect(s.state, conn)

	c.Assert(err, IsNil)
	change.AddAll(ts)
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitAll(ts)
	change.AddTask(terr)
	c.Assert(change.Tasks(), HasLen, 2)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that disconnect tasks were undone
	for _, t := range ts.Tasks() {
		c.Assert(t.Status(), Equals, state.UndoneStatus)
	}

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, connState)

	_ = s.getConnection(c, "consumer", "plug", "producer", "slot")

	c.Assert(s.secBackend.SetupCalls, HasLen, 4)

	producerAppSet := s.secBackend.SetupCalls[2].AppSet
	c.Check(producerAppSet.InstanceName(), Equals, "producer")
	c.Check(producerAppSet.Runnables(), testutil.DeepUnsortedMatches, []snap.Runnable{
		{
			CommandName: "producer+comp.hook.install",
			SecurityTag: "snap.producer+comp.hook.install",
		},
	})

	consumerAppSet := s.secBackend.SetupCalls[3].AppSet
	c.Check(consumerAppSet.InstanceName(), Equals, "consumer")
	c.Check(consumerAppSet.Runnables(), testutil.DeepUnsortedMatches, []snap.Runnable{
		{
			CommandName: "consumer+comp.hook.install",
			SecurityTag: "snap.consumer+comp.hook.install",
		},
	})
}

func (s *interfaceManagerSuite) TestForgetUndo(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	// plug3 and slot3 do not exist, so the connection is not in the repository.
	connState := map[string]interface{}{
		"consumer:plug producer:slot":   map[string]interface{}{"interface": "test"},
		"consumer:plug3 producer:slot3": map[string]interface{}{"interface": "test2"},
	}

	s.state.Lock()
	s.state.Set("conns", connState)
	s.state.Unlock()

	// Initialize the manager. This registers both snaps and reloads the connection.
	mgr := s.manager(c)

	// validity
	s.getConnection(c, "consumer", "plug", "producer", "slot")

	s.state.Lock()
	change := s.state.NewChange("disconnect", "...")
	ts, err := ifacestate.Forget(s.state, mgr.Repository(), &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug3"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot3"}})
	c.Assert(err, IsNil)
	// inactive connection, only the disconnect task (no hooks)
	c.Assert(ts.Tasks(), HasLen, 1)
	task := ts.Tasks()[0]
	c.Check(task.Kind(), Equals, "disconnect")
	var forgetFlag bool
	c.Assert(task.Get("forget", &forgetFlag), IsNil)
	c.Check(forgetFlag, Equals, true)

	change.AddAll(ts)
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitAll(ts)
	change.AddTask(terr)

	// Run the disconnect task and let it finish.
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that disconnect task was undone
	c.Assert(task.Status(), Equals, state.UndoneStatus)

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, connState)

	s.getConnection(c, "consumer", "plug", "producer", "slot")
}

func (s *interfaceManagerSuite) TestStaleConnectionsIgnoredInReloadConnections(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})

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

func (s *interfaceManagerSuite) testStaleAutoConnectionsNotRemovedIfSnapBroken(c *C, brokenSnapName string) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	s.state.Lock()
	defer s.state.Unlock()

	restore := ifacestate.MockRemoveStaleConnections(func(s *state.State) error { return nil })
	defer restore()

	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot":             map[string]interface{}{"interface": "test", "auto": true},
		"other-consumer:plug other-producer:slot": map[string]interface{}{"interface": "test", "auto": true},
	})
	sideInfo := &snap.SideInfo{
		RealName: brokenSnapName,
		Revision: snap.R(1),
	}

	// have one of them in state, and broken due to missing snap.yaml
	snapstate.Set(s.state, brokenSnapName, &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  sideInfo.Revision,
		SnapType: "app",
	})

	// validity check - snap is broken
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, brokenSnapName, &snapst), IsNil)
	curInfo, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Check(curInfo.Broken, Matches, fmt.Sprintf(`cannot find installed snap "%s" at revision 1: missing file .*/1/meta/snap.yaml`, brokenSnapName))

	s.state.Unlock()
	defer s.state.Lock()
	mgr := s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that nothing is connected
	repo := mgr.Repository()
	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 0)

	// but the consumer:plug producer:slot connection is kept in the state and only the other one
	// got dropped.
	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test", "auto": true},
	})

	c.Check(s.log.String(), testutil.Contains, fmt.Sprintf("Snap %q is broken, ignored by reloadConnections", brokenSnapName))
}

func (s *interfaceManagerSuite) TestStaleAutoConnectionsNotRemovedIfPlugSnapBroken(c *C) {
	s.testStaleAutoConnectionsNotRemovedIfSnapBroken(c, "consumer")
}

func (s *interfaceManagerSuite) TestStaleAutoConnectionsNotRemovedIfSlotSnapBroken(c *C) {
	s.testStaleAutoConnectionsNotRemovedIfSnapBroken(c, "producer")
}

func (s *interfaceManagerSuite) TestStaleConnectionsRemoved(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})

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

func (s *interfaceManagerSuite) testForget(c *C, plugSnap, plugName, slotSnap, slotName string) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot":   map[string]interface{}{"interface": "test"},
		"consumer:plug2 producer:slot2": map[string]interface{}{"interface": "test2"},
	})
	s.state.Unlock()

	// Initialize the manager. This registers both snaps and reloads the
	// connections. Only one connection ends up in the repository.
	mgr := s.manager(c)

	// validity
	_ = s.getConnection(c, "consumer", "plug", "producer", "slot")

	// Run the disconnect --forget task and let it finish.
	s.state.Lock()
	change := s.state.NewChange("disconnect", "...")
	ts, err := ifacestate.Forget(s.state, mgr.Repository(),
		&interfaces.ConnRef{
			PlugRef: interfaces.PlugRef{Snap: plugSnap, Name: plugName},
			SlotRef: interfaces.SlotRef{Snap: slotSnap, Name: slotName}})
	c.Assert(err, IsNil)

	// check disconnect task
	var disconnectTask *state.Task
	for _, t := range ts.Tasks() {
		if t.Kind() == "disconnect" {
			disconnectTask = t
			break
		}
	}
	c.Assert(disconnectTask, NotNil)
	var forgetFlag bool
	c.Assert(disconnectTask.Get("forget", &forgetFlag), IsNil)
	c.Check(forgetFlag, Equals, true)

	c.Assert(err, IsNil)
	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	if plugName == "plug" {
		// active connection, disconnect + hooks expected
		c.Assert(change.Tasks(), HasLen, 3)
	} else {
		// inactive connection, just the disconnect task
		c.Assert(change.Tasks(), HasLen, 1)
	}

	c.Check(change.Status(), Equals, state.DoneStatus)
}

func (s *interfaceManagerSuite) TestForgetInactiveConnection(c *C) {
	// forget inactive connection, that means it's not in the repository,
	// only in the state.
	s.testForget(c, "consumer", "plug2", "producer", "slot2")

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the connection has been removed from the state
	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})

	mgr := s.manager(c)
	repo := mgr.Repository()

	// and the active connection remains in the repo
	repoConns, err := repo.Connections("consumer")
	c.Assert(err, IsNil)
	c.Assert(repoConns, HasLen, 1)
	c.Check(repoConns[0].PlugRef.Name, Equals, "plug")
}

func (s *interfaceManagerSuite) TestForgetActiveConnection(c *C) {
	// forget active connection, that means it's in the repository,
	// so it goes through normal disconnect logic and is removed
	// from the repository.
	s.testForget(c, "consumer", "plug", "producer", "slot")

	mgr := s.manager(c)
	// Ensure that the connection has been removed from the repository
	repo := mgr.Repository()
	repoConns, err := repo.Connections("consumer")
	c.Assert(err, IsNil)
	c.Check(repoConns, HasLen, 0)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the connection has been removed from the state
	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug2 producer:slot2": map[string]interface{}{"interface": "test2"},
	})
}

func (s *interfaceManagerSuite) mockSecBackend(backend interfaces.SecurityBackend) {
	s.extraBackends = append(s.extraBackends, backend)
}

func (s *interfaceManagerSuite) mockIface(iface interfaces.Interface) {
	s.extraIfaces = append(s.extraIfaces, iface)
}

func (s *interfaceManagerSuite) mockIfaces(ifaces ...interfaces.Interface) {
	s.extraIfaces = append(s.extraIfaces, ifaces...)
}

func (s *interfaceManagerSuite) mockAppSet(c *C, yamlText string) *interfaces.SnapAppSet {
	info := s.mockSnap(c, yamlText)
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	return set
}

func (s *interfaceManagerSuite) mockSnap(c *C, yamlText string) *snap.Info {
	return s.mockSnapInstance(c, "", yamlText)
}

func (s *interfaceManagerSuite) mockSnapInstance(c *C, instanceName, yamlText string) *snap.Info {
	sideInfo := &snap.SideInfo{
		Revision: snap.R(1),
	}
	snapInfo := snaptest.MockSnapInstance(c, instanceName, yamlText, sideInfo)
	sideInfo.RealName = snapInfo.SnapName()
	snapInfo.RealName = snapInfo.SnapName()

	a, err := s.Db.FindMany(asserts.SnapDeclarationType, map[string]string{
		"snap-name": sideInfo.RealName,
	})
	if err == nil {
		decl := a[0].(*asserts.SnapDeclaration)
		snapInfo.SnapID = decl.SnapID()
		sideInfo.SnapID = decl.SnapID()
	} else if errors.Is(err, &asserts.NotFoundError{}) {
		err = nil
	}
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// Put a side info into the state
	snapstate.Set(s.state, snapInfo.InstanceName(), &snapstate.SnapState{
		Active:      true,
		Sequence:    snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:     sideInfo.Revision,
		SnapType:    string(snapInfo.Type()),
		InstanceKey: snapInfo.InstanceKey,
	})
	return snapInfo
}

func (s *interfaceManagerSuite) mockUpdatedSnap(c *C, yamlText string, revision int) *snap.Info {
	sideInfo := &snap.SideInfo{Revision: snap.R(revision)}
	snapInfo := snaptest.MockSnap(c, yamlText, sideInfo)
	sideInfo.RealName = snapInfo.SnapName()

	s.state.Lock()
	defer s.state.Unlock()

	// Put the new revision (stored in SideInfo) into the state
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, snapInfo.InstanceName(), &snapst)
	c.Assert(err, IsNil)
	snapst.Sequence.Revisions = append(snapst.Sequence.Revisions, sequence.NewRevisionSideState(sideInfo, nil))
	snapstate.Set(s.state, snapInfo.InstanceName(), &snapst)

	return snapInfo
}

type setupSnapSecurityChangeOptions struct {
	active  bool
	install bool
}

func (s *interfaceManagerSuite) addSetupSnapSecurityChange(c *C, snapsup *snapstate.SnapSetup) *state.Change {
	// snaps are inactive at the time of setup-profiles
	return s.addSetupSnapSecurityChangeWithOptions(c, snapsup, setupSnapSecurityChangeOptions{active: false})
}

func (s *interfaceManagerSuite) addSetupSnapSecurityChangeFromComponent(c *C, snapsup *snapstate.SnapSetup, compsup *snapstate.ComponentSetup) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	// TODO: we'll need to update this to handle refreshing components once that
	// work is done

	s.o.TaskRunner().AddHandler("mock-link-component-n-witness", func(task *state.Task, tomb *tomb.Tomb) error { // do handler
		s.state.Lock()
		defer s.state.Unlock()

		var snapst snapstate.SnapState
		err := snapstate.Get(s.state, snapsup.InstanceName(), &snapst)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}

		info, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}

		cs := sequence.NewComponentState(compsup.CompSideInfo, compsup.CompType)

		if err := snapst.Sequence.AddComponentForRevision(info.Revision, cs); err != nil {
			return fmt.Errorf("internal error while linking component: %w", err)
		}

		snapstate.Set(s.state, snapsup.InstanceName(), &snapst)

		return nil
	}, func(task *state.Task, tomb *tomb.Tomb) error { // undo handler
		s.state.Lock()
		defer s.state.Unlock()

		var snapst snapstate.SnapState
		err := snapstate.Get(s.state, snapsup.InstanceName(), &snapst)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}

		info, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}

		removed := snapst.Sequence.RemoveComponentForRevision(
			info.Revision, compsup.CompSideInfo.Component,
		)

		c.Check(removed, NotNil)

		snapstate.Set(s.state, snapsup.InstanceName(), &snapst)

		return nil
	})

	// snap should already be installed if calling this function
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, snapsup.InstanceName(), &snapst)
	c.Assert(err, IsNil)

	change := s.state.NewChange("test", "")

	setupTask := s.state.NewTask("setup-profiles", "")
	setupTask.Set("snap-setup", snapsup)
	setupTask.Set("component-setup", compsup)
	change.AddTask(setupTask)

	linkTask := s.state.NewTask("mock-link-component-n-witness", "")
	linkTask.Set("snap-setup", snapsup)
	linkTask.Set("component-setup", compsup)
	linkTask.WaitFor(setupTask)
	change.AddTask(linkTask)

	autoConnectTask := s.state.NewTask("auto-connect", "")
	autoConnectTask.Set("snap-setup", snapsup)
	autoConnectTask.WaitFor(linkTask)
	change.AddTask(autoConnectTask)

	return change
}

func (s *interfaceManagerSuite) addSetupSnapSecurityChangeWithOptions(c *C, snapsup *snapstate.SnapSetup, opts setupSnapSecurityChangeOptions) *state.Change {
	s.state.Lock()
	defer s.state.Unlock()

	s.o.TaskRunner().AddHandler("mock-link-snap-n-witness", func(task *state.Task, tomb *tomb.Tomb) error { // do handler
		s.state.Lock()
		defer s.state.Unlock()
		var snapst snapstate.SnapState
		err := snapstate.Get(s.state, snapsup.InstanceName(), &snapst)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		snapst.Active = true
		if opts.install {
			c.Check(snapst.IsInstalled(), Equals, false)
			snapst.Current = snapsup.SideInfo.Revision
			snapst.Sequence = snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{snapsup.SideInfo})
		} else {
			c.Check(snapst.PendingSecurity, DeepEquals, &snapstate.PendingSecurityState{
				SideInfo: snapsup.SideInfo,
			})
		}
		snapstate.Set(s.state, snapsup.InstanceName(), &snapst)
		c.Check(ifacestate.OnSnapLinkageChanged(s.state, snapsup), IsNil)
		return nil
	}, func(task *state.Task, tomb *tomb.Tomb) error { // undo handler
		s.state.Lock()
		defer s.state.Unlock()
		var snapst snapstate.SnapState
		err := snapstate.Get(s.state, snapsup.InstanceName(), &snapst)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		if opts.install {
			// unlink completely
			snapstate.Set(s.state, snapsup.InstanceName(), nil)
		} else {
			snapst.Active = false
			snapstate.Set(s.state, snapsup.InstanceName(), &snapst)
		}
		// this is realistic and will move PendingSecurity.SideInfo
		// on undo already to the previous revision, this should
		// not be a problem because that is eventually the revision
		// we want when the snap is activated again
		c.Check(ifacestate.OnSnapLinkageChanged(s.state, snapsup), IsNil)
		if !opts.install {
			// perturb things to make sure undo-setup-profiles
			// sets the right value
			c.Assert(snapstate.Get(s.state, snapsup.InstanceName(), &snapst), IsNil)
			snapst.PendingSecurity.SideInfo = &snap.SideInfo{}
			snapstate.Set(s.state, snapsup.InstanceName(), &snapst)
		}
		return nil
	})

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, snapsup.InstanceName(), &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		panic(err)
	}
	if snapst.IsInstalled() {
		snapst.Active = opts.active
		snapstate.Set(s.state, snapsup.InstanceName(), &snapst)
	}

	change := s.state.NewChange("test", "")

	task1 := s.state.NewTask("setup-profiles", "")
	task1.Set("snap-setup", snapsup)
	change.AddTask(task1)

	task2 := s.state.NewTask("mock-link-snap-n-witness", "")
	task2.Set("snap-setup", snapsup)
	task2.WaitFor(task1)
	change.AddTask(task2)

	task3 := s.state.NewTask("auto-connect", "")
	task3.Set("snap-setup", snapsup)
	task3.WaitFor(task2)
	change.AddTask(task3)

	return change
}

func (s *interfaceManagerSuite) addRemoveSnapSecurityChange(snapName string) *state.Change {
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

func (s *interfaceManagerSuite) addDiscardConnsChange(snapName string) (*state.Change, *state.Task) {
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
	return change, task
}

var ubuntuCoreSnapYaml = `
name: ubuntu-core
version: 1
type: os
`

var ubuntuCoreSnapWithComponentYaml = ubuntuCoreSnapYaml + `
components:
  comp:
    type: test
    hooks:
      install:
`

var ubuntuCoreSnapYaml2 = `
name: ubuntu-core
version: 1
type: os
slots:
 test1:
   interface: test1
 test2:
   interface: test2
`

var coreSnapYaml = `
name: core
version: 1
type: os
slots:
 unrelated:
   interface: unrelated
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
 unrelated:
  interface: unrelated
`

const sampleComponentYaml = `
component: snap+comp1
type: test
version: 1.0
`

var sampleSnapWithComponentsYaml = sampleSnapYaml + `
components:
  comp1:
    type: test
    hooks:
      install:
  comp2:
    type: test
    hooks:
      pre-refresh:
`

var sampleSnapYamlManyPlugs = `
name: snap
version: 1
apps:
 app:
   command: foo
plugs:
 network:
  interface: network
 home:
  interface: home
 x11:
  interface: x11
 wayland:
  interface: wayland
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
hooks:
 prepare-plug-plug:
 unprepare-plug-plug:
 connect-plug-plug:
 disconnect-plug-plug:
 prepare-plug-otherplug:
 unprepare-plug-otherplug:
 connect-plug-otherplug:
 disconnect-plug-otherplug:
`

var consumerWithComponentYaml = consumerYaml + `
components:
  comp:
    type: test
    hooks:
      install:
  not-installed-comp:
    type: test
    hooks:
      install:
`

var consumerRunnablesFullSet = []snap.Runnable{
	{
		CommandName: "hook.connect-plug-otherplug",
		SecurityTag: "snap.consumer.hook.connect-plug-otherplug",
	},
	{
		CommandName: "hook.connect-plug-plug",
		SecurityTag: "snap.consumer.hook.connect-plug-plug",
	},
	{
		CommandName: "hook.disconnect-plug-otherplug",
		SecurityTag: "snap.consumer.hook.disconnect-plug-otherplug",
	},
	{
		CommandName: "hook.disconnect-plug-plug",
		SecurityTag: "snap.consumer.hook.disconnect-plug-plug",
	},
	{
		CommandName: "hook.prepare-plug-otherplug",
		SecurityTag: "snap.consumer.hook.prepare-plug-otherplug",
	},
	{
		CommandName: "hook.prepare-plug-plug",
		SecurityTag: "snap.consumer.hook.prepare-plug-plug",
	},
	{
		CommandName: "hook.unprepare-plug-otherplug",
		SecurityTag: "snap.consumer.hook.unprepare-plug-otherplug",
	},
	{
		CommandName: "hook.unprepare-plug-plug",
		SecurityTag: "snap.consumer.hook.unprepare-plug-plug",
	},
	{
		CommandName: "consumer+comp.hook.install",
		SecurityTag: "snap.consumer+comp.hook.install",
	},
}

var consumer2Yaml = `
name: consumer2
version: 1
plugs:
 plug:
  interface: test
  attr1: value1
`

var consumerYaml3 = `
name: consumer
version: 1
plugs:
 plug:
  interface: test
hooks:
%s
`

var producerYaml = `
name: producer
version: 1
slots:
 slot:
  interface: test
  attr2: value2
hooks:
  prepare-slot-slot:
  unprepare-slot-slot:
  connect-slot-slot:
  disconnect-slot-slot:
`

var producerWithComponentYaml = producerYaml + `
components:
  comp:
    type: test
    hooks:
      install:
  not-installed-comp:
    type: test
    hooks:
      install:
`

var producerRunnablesFullSet = []snap.Runnable{
	{
		CommandName: "hook.connect-slot-slot",
		SecurityTag: "snap.producer.hook.connect-slot-slot",
	},
	{
		CommandName: "hook.disconnect-slot-slot",
		SecurityTag: "snap.producer.hook.disconnect-slot-slot",
	},
	{
		CommandName: "hook.prepare-slot-slot",
		SecurityTag: "snap.producer.hook.prepare-slot-slot",
	},
	{
		CommandName: "hook.unprepare-slot-slot",
		SecurityTag: "snap.producer.hook.unprepare-slot-slot",
	},
	{
		CommandName: "producer+comp.hook.install",
		SecurityTag: "snap.producer+comp.hook.install",
	},
}

var producer2Yaml = `
name: producer2
version: 1
slots:
 slot:
  interface: test
  attr2: value2
  number: 1
`

var producerYaml3 = `
name: producer
version: 1
slots:
 slot:
  interface: test
hooks:
%s
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
hooks:
 prepare-plug-plug:
 unprepare-plug-plug:
 connect-plug-plug:
 disconnect-plug-plug:
 prepare-slot-slot:
 unprepare-slot-slot:
 connect-slot-slot:
 disconnect-slot-slot:
`

var refreshedSnapYaml = `
name: snap
version: 2
apps:
 app:
   command: foo
plugs:
 test2:
  interface: test2
`

var refreshedSnapYaml2 = `
name: snap
version: 2
apps:
 app:
   command: foo
plugs:
 test1:
  interface: test1
 test2:
  interface: test2
`

var slotSnapYaml = `
name: snap2
version: 2
apps:
 app:
   command: bar
slots:
 test2:
  interface: test2
`

// The auto-connect task will not auto-connect a plug that was previously
// explicitly disconnected by the user.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityHonorsUndesiredFlag(c *C) {
	s.MockModel(c, nil)

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
			RealName: snapInfo.SnapName(),
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

func (s *interfaceManagerSuite) TestBadInterfacesWarning(c *C) {
	restoreSanitize := snap.MockSanitizePlugsSlots(func(inf *snap.Info) {
		inf.BadInterfaces["plug-name"] = "reason-for-bad"
	})
	defer restoreSanitize()

	s.MockModel(c, nil)

	_ = s.manager(c)

	// sampleSnapYaml is valid but that's irrelevant for the test as we are
	// injecting the desired behavior via mocked SanitizePlugsSlots above.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Status(), Equals, state.DoneStatus)

	warns := s.state.AllWarnings()
	c.Assert(warns, HasLen, 1)
	c.Check(warns[0].String(), Matches, `snap "snap" has bad plugs or slots: plug-name \(reason-for-bad\)`)

	// validity, bad interfaces are logged in the task log.
	task := change.Tasks()[0]
	c.Assert(task.Kind(), Equals, "setup-profiles")
	c.Check(strings.Join(task.Log(), ""), Matches, `.* snap "snap" has bad plugs or slots: plug-name \(reason-for-bad\)`)
}

// The auto-connect task will auto-connect plugs with viable candidates.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsPlugs(c *C) {
	s.MockModel(c, nil)

	// Add an OS snap.
	s.mockSnap(c, ubuntuCoreSnapYaml)

	// Initialize the manager. This registers the OS snap.
	mgr := s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
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

// The auto-connect task will auto-connect slots with viable candidates.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsSlots(c *C) {
	s.MockModel(c, nil)

	// Mock the interface that will be used by the test
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
			RealName: snapInfo.SnapName(),
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
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
}

// The auto-connect task will auto-connect slots with viable multiple candidates.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsSlotsMultiplePlugs(c *C) {
	s.MockModel(c, nil)

	// Mock the interface that will be used by the test
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
			RealName: snapInfo.SnapName(),
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
		{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}},
		{PlugRef: interfaces.PlugRef{Snap: "consumer2", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}},
	})
}

// The auto-connect task will not auto-connect slots if viable alternative slots are present.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityNoAutoConnectSlotsIfAlternative(c *C) {
	s.MockModel(c, nil)

	// Mock the interface that will be used by the test
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})
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
			RealName: snapInfo.SnapName(),
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
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
	c.Check(conns, HasLen, 0)
}

// The auto-connect task will auto-connect plugs with viable candidates also condidering snap declarations.
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

// The auto-connect task will *not* auto-connect plugs with viable candidates when snap declarations are missing.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedWhenMissingDecl(c *C) {
	s.testDoSetupSnapSecurityAutoConnectsDeclBased(c, false, func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
		// Ensure nothing is connected.
		c.Check(conns, HasLen, 0)
		c.Check(repoConns, HasLen, 0)
	})
}

func (s *interfaceManagerSuite) testDoSetupSnapSecurityAutoConnectsDeclBased(c *C, withDecl bool, check func(map[string]interface{}, []*interfaces.ConnRef)) {
	s.MockModel(c, nil)

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
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.MockSnapDecl(c, "producer", "one-publisher", nil)
	s.mockSnap(c, producerYaml)

	// Initialize the manager. This registers the producer snap.
	mgr := s.manager(c)

	// Add a sample snap with a plug with the "test" interface which should be auto-connected.
	if withDecl {
		s.MockSnapDecl(c, "consumer", "one-publisher", nil)
	}
	snapInfo := s.mockSnap(c, consumerYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
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

// The auto-connect task will check snap declarations providing the
// model assertion to fulfill device scope constraints: here no store
// in the model assertion fails an on-store constraint.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScopeNoStore(c *C) {

	s.MockModel(c, nil)

	s.testDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScope(c, func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
		// Ensure nothing is connected.
		c.Check(conns, HasLen, 0)
		c.Check(repoConns, HasLen, 0)
	})
}

// The auto-connect task will check snap declarations providing the
// model assertion to fulfill device scope constraints: here the wrong
// store in the model assertion fails an on-store constraint.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScopeWrongStore(c *C) {

	s.MockModel(c, map[string]interface{}{
		"store": "other-store",
	})

	s.testDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScope(c, func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
		// Ensure nothing is connected.
		c.Check(conns, HasLen, 0)
		c.Check(repoConns, HasLen, 0)
	})
}

// The auto-connect task will check snap declarations providing the
// model assertion to fulfill device scope constraints: here the right
// store in the model assertion passes an on-store constraint.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScopeRightStore(c *C) {

	s.MockModel(c, map[string]interface{}{
		"store": "my-store",
	})

	s.testDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScope(c, func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
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

// The auto-connect task will check snap declarations providing the
// model assertion to fulfill device scope constraints: here the
// wrong "friendly store"s of the store in the model assertion fail an
// on-store constraint.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScopeWrongFriendlyStore(c *C) {

	s.MockModel(c, map[string]interface{}{
		"store": "my-substore",
	})

	s.MockStore(c, s.state, "my-substore", map[string]interface{}{
		"friendly-stores": []interface{}{"other-store"},
	})

	s.testDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScope(c, func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
		// Ensure nothing is connected.
		c.Check(conns, HasLen, 0)
		c.Check(repoConns, HasLen, 0)
	})
}

// The auto-connect task will check snap declarations providing the
// model assertion to fulfill device scope constraints: here a
// "friendly store" of the store in the model assertion passes an
// on-store constraint.
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScopeFriendlyStore(c *C) {

	s.MockModel(c, map[string]interface{}{
		"store": "my-substore",
	})

	s.MockStore(c, s.state, "my-substore", map[string]interface{}{
		"friendly-stores": []interface{}{"my-store"},
	})

	s.testDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScope(c, func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
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

func (s *interfaceManagerSuite) testDoSetupSnapSecurityAutoConnectsDeclBasedDeviceScope(c *C, check func(map[string]interface{}, []*interfaces.ConnRef)) {
	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    allow-auto-connection: false
`))
	defer restore()
	// Add the producer snap
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	s.MockSnapDecl(c, "producer", "one-publisher", nil)
	s.mockSnap(c, producerYaml)

	// Initialize the manager. This registers the producer snap.
	mgr := s.manager(c)

	s.MockSnapDecl(c, "consumer", "one-publisher", map[string]interface{}{
		"format": "3",
		"plugs": map[string]interface{}{
			"test": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"on-store": []interface{}{"my-store"},
				},
			},
		},
	})
	snapInfo := s.mockSnap(c, consumerYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
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
func (s *interfaceManagerSuite) TestDoSetupSnapSecurityKeepsExistingConnectionState(c *C) {
	s.MockModel(c, nil)

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
			RealName: snapInfo.SnapName(),
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

func (s *interfaceManagerSuite) TestReloadingConnectionsOnStartupUpdatesStaticAttributes(c *C) {
	// Put a connection in the state. The connection binds the two snaps we are
	// adding below. The connection contains a copy of the static attributes
	// but refers to the "old" values, in contrast to what the snaps define.
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface":   "content",
			"plug-static": map[string]interface{}{"content": "foo", "attr": "old-plug-attr"},
			"slot-static": map[string]interface{}{"content": "foo", "attr": "old-slot-attr"},
		},
	})
	s.state.Unlock()

	// Add consumer and producer snaps, with a plug and slot respectively, each
	// carrying a single attribute with a "new" value. The "new" value is in
	// contrast to the old value in the connection state.
	const consumerYaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: content
  content: foo
  attr: new-plug-attr
`
	const producerYaml = `
name: producer
version: 1
slots:
 slot:
  interface: content
  content: foo
  attr: new-slot-attr
`
	s.mockSnap(c, producerYaml)
	s.mockSnap(c, consumerYaml)

	// Create a connection reference, it's just verbose and used a few times
	// below so it's put up here.
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}

	// Add a test security backend and a test interface. We want to use them to
	// observe the interaction with the security backend and to allow the
	// interface manager to keep the test plug and slot of the consumer and
	// producer snaps we introduce below.
	secBackend := &ifacetest.TestSecurityBackend{
		BackendName: "test",
		SetupCallback: func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
			// Whenever this function is invoked to setup security for a snap
			// we check the connection attributes that it would act upon.
			// Because of how connection state is refreshed we never expect to
			// see the old attribute values.
			conn, err := repo.Connection(connRef)
			c.Assert(err, IsNil)
			c.Check(conn.Plug.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo", "attr": "new-plug-attr"})
			c.Check(conn.Slot.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo", "attr": "new-slot-attr"})
			return nil
		},
	}
	s.mockSecBackend(secBackend)
	//s.mockIfaces(c, &ifacetest.TestInterface{InterfaceName: "content"})

	// Create the interface manager. This indirectly adds the snaps to the
	// repository and re-connects them using the stored connection information.
	mgr := s.manager(c)

	// Inspect the repository connection data. The data no longer refers to the
	// old connection attributes because they were updated when the connections
	// were reloaded from the state.
	repo := mgr.Repository()
	conn, err := repo.Connection(connRef)
	c.Assert(err, IsNil)
	c.Check(conn.Plug.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo", "attr": "new-plug-attr"})
	c.Check(conn.Slot.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo", "attr": "new-slot-attr"})

	// Because of the fact that during testing the system key always
	// mismatches, the security setup is performed.
	c.Check(secBackend.SetupCalls, HasLen, 2)
}

// LP:#1825883; make sure static attributes in conns state are updated from the snap yaml on snap refresh (content interface only)
func (s *interfaceManagerSuite) testDoSetupProfilesUpdatesStaticAttributes(c *C, snapNameToSetup string) {
	// Put a connection in the state. The connection binds the two snaps we are
	// adding below. The connection reflects the snaps as they are now, and
	// carries no attribute data.
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "content",
		},
		"consumer:plug3 producer:slot2": map[string]interface{}{
			"interface": "system-files",
		},
		"unrelated-a:plug unrelated-b:slot": map[string]interface{}{
			"interface":   "unrelated",
			"plug-static": map[string]interface{}{"attr": "unrelated-stale"},
			"slot-static": map[string]interface{}{"attr": "unrelated-stale"},
		},
	})
	s.state.Unlock()

	// Add a pair of snap versions for producer and consumer snaps, with a plug
	// and slot respectively. The second version producer and consumer snaps
	// where the interfaces carry additional attributes.
	const consumerV1Yaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: content
  content: foo
 plug2:
  interface: content
  content: bar
 plug3:
  interface: system-files
`
	const producerV1Yaml = `
name: producer
version: 1
slots:
 slot:
  interface: content
  content: foo
 slot2:
  interface: system-files
`
	const consumerV2Yaml = `
name: consumer
version: 2
plugs:
 plug:
  interface: content
  content: foo
  attr: plug-value
 plug2:
  interface: content
  content: bar-changed
  attr: plug-value
 plug3:
  interface: system-files
  read:
    - /etc/foo
`
	const producerV2Yaml = `
name: producer
version: 2
slots:
 slot:
  interface: content
  content: foo
  attr: slot-value
 slot2:
  interface: system-files
`

	const unrelatedAYaml = `
name: unrelated-a
version: 1
plugs:
  plug:
   interface: unrelated
   attr: unrelated-new
`
	const unrelatedBYaml = `
name: unrelated-b
version: 1
slots:
  slot:
   interface: unrelated
   attr: unrelated-new
`

	// NOTE: s.mockSnap sets the state and calls MockSnapInstance internally,
	// which puts the snap on disk. This gives us all four YAMLs on disk and
	// just the first version of both in the state.
	s.mockSnap(c, producerV1Yaml)
	s.mockSnap(c, consumerV1Yaml)
	snaptest.MockSnapInstance(c, "", consumerV2Yaml, &snap.SideInfo{Revision: snap.R(2), RealName: "consumer"})
	snaptest.MockSnapInstance(c, "", producerV2Yaml, &snap.SideInfo{Revision: snap.R(2), RealName: "producer"})

	// Mock two unrelated snaps, those will show that the state of unrelated
	// snaps is not clobbered by the refresh process.
	s.mockSnap(c, unrelatedAYaml)
	s.mockSnap(c, unrelatedBYaml)

	// Create a connection reference, it's just verbose and used a few times
	// below so it's put up here.
	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}
	sysFilesConnRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug3"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot2"}}

	// Add a test security backend and a test interface. We want to use them to
	// observe the interaction with the security backend and to allow the
	// interface manager to keep the test plug and slot of the consumer and
	// producer snaps we introduce below.
	secBackend := &ifacetest.TestSecurityBackend{
		BackendName: "test",
		SetupCallback: func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
			// Whenever this function is invoked to setup security for a snap
			// we check the connection attributes that it would act upon.
			// Those attributes should always match those of the snap version.
			conn, err := repo.Connection(connRef)
			c.Assert(err, IsNil)
			sysFilesConn, err2 := repo.Connection(sysFilesConnRef)
			c.Assert(err2, IsNil)
			switch appSet.Info().Version {
			case "1":
				c.Check(conn.Plug.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo"})
				c.Check(conn.Slot.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo"})
				c.Check(sysFilesConn.Plug.StaticAttrs(), DeepEquals, map[string]interface{}{})
			case "2":
				switch snapNameToSetup {
				case "consumer":
					// When the consumer has security setup the consumer's plug attribute is updated.
					c.Check(conn.Plug.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo", "attr": "plug-value"})
					c.Check(conn.Slot.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo"})
					c.Check(sysFilesConn.Plug.StaticAttrs(), DeepEquals, map[string]interface{}{"read": []interface{}{"/etc/foo"}})
				case "producer":
					// When the producer has security setup the producer's slot attribute is updated.
					c.Check(conn.Plug.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo"})
					c.Check(conn.Slot.StaticAttrs(), DeepEquals, map[string]interface{}{"content": "foo", "attr": "slot-value"})
				}
			}
			return nil
		},
	}
	s.mockSecBackend(secBackend)
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "unrelated"})

	// Create the interface manager. This indirectly adds the snaps to the
	// repository and reloads the connection.
	s.manager(c)

	// Because in tests the system key mismatch always occurs, the backend is
	// invoked during the startup of the interface manager. The count
	// represents the number of snaps that are in the system.
	c.Check(secBackend.SetupCalls, HasLen, 4)

	// Alter the state of producer and consumer snaps to get new revisions.
	s.state.Lock()
	for _, snapName := range []string{"producer", "consumer"} {
		snapstate.Set(s.state, snapName, &snapstate.SnapState{
			Active: true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
				{Revision: snap.R(1), RealName: snapName},
				{Revision: snap.R(2), RealName: snapName},
			}),
			Current:  snap.R(2),
			SnapType: string("app"),
		})
	}
	s.state.Unlock()

	// Setup profiles for the given snap, either consumer or producer.
	s.state.Lock()
	change := s.state.NewChange("test", "")
	task := s.state.NewTask("setup-profiles", "")
	task.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: snapNameToSetup, Revision: snap.R(2)}})
	change.AddTask(task)
	s.state.Unlock()

	// Spin the wheels to run the tasks we added.
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()
	c.Logf("change failure: %v", change.Err())
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// We expect our security backend to be invoked for both snaps. See above
	// for explanation about why it has four calls already.
	c.Check(secBackend.SetupCalls, HasLen, 4+2)
}

func (s *interfaceManagerSuite) TestDoSetupProfilesUpdatesStaticAttributesPlugSnap(c *C) {
	s.testDoSetupProfilesUpdatesStaticAttributes(c, "consumer")
}

func (s *interfaceManagerSuite) TestDoSetupProfilesUpdatesStaticAttributesSlotSnap(c *C) {
	s.testDoSetupProfilesUpdatesStaticAttributes(c, "producer")
}

func (s *interfaceManagerSuite) TestUpdateStaticAttributesIgnoresContentMismatch(c *C) {
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "content",
			"content":   "foo",
		},
	})
	s.state.Unlock()

	// Add a pair of snap versions for producer and consumer snaps, with a plug
	// and slot respectively. The second version are producer and consumer snaps
	// where the interfaces carry additional attributes but there is a mismatch
	// on "content" attribute value.
	const consumerV1Yaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: content
  content: foo
`
	const producerV1Yaml = `
name: producer
version: 1
slots:
 slot:
  interface: content
  content: foo
`
	const consumerV2Yaml = `
name: consumer
version: 2
plugs:
 plug:
  interface: content
  content: foo-mismatch
  attr: plug-value
`
	const producerV2Yaml = `
name: producer
version: 2
slots:
 slot:
  interface: content
  content: foo
  attr: slot-value
`

	// NOTE: s.mockSnap sets the state and calls MockSnapInstance internally,
	// which puts the snap on disk. This gives us all four YAMLs on disk and
	// just the first version of both in the state.
	s.mockSnap(c, producerV1Yaml)
	s.mockSnap(c, consumerV1Yaml)
	snaptest.MockSnapInstance(c, "", consumerV2Yaml, &snap.SideInfo{Revision: snap.R(2)})
	snaptest.MockSnapInstance(c, "", producerV2Yaml, &snap.SideInfo{Revision: snap.R(2)})

	secBackend := &ifacetest.TestSecurityBackend{BackendName: "test"}
	s.mockSecBackend(secBackend)

	// Create the interface manager. This indirectly adds the snaps to the
	// repository and reloads the connection.
	s.manager(c)

	// Alter the state of producer and consumer snaps to get new revisions.
	s.state.Lock()
	for _, snapName := range []string{"producer", "consumer"} {
		snapstate.Set(s.state, snapName, &snapstate.SnapState{
			Active: true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
				{Revision: snap.R(1), RealName: snapName},
				{Revision: snap.R(2), RealName: snapName},
			}),
			Current:  snap.R(2),
			SnapType: string("app"),
		})
	}
	s.state.Unlock()

	s.state.Lock()
	change := s.state.NewChange("test", "")
	task := s.state.NewTask("setup-profiles", "")
	task.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "consumer", Revision: snap.R(2)}})
	change.AddTask(task)
	s.state.Unlock()

	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(change.Status(), Equals, state.DoneStatus)

	var conns map[string]interface{}
	s.state.Get("conns", &conns)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface":   "content",
			"plug-static": map[string]interface{}{"content": "foo"},
			"slot-static": map[string]interface{}{"content": "foo"},
		},
	})
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecurityIgnoresStrayConnection(c *C) {
	s.MockModel(c, nil)

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
			RealName: snapInfo.SnapName(),
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
	s.MockModel(c, nil)

	// Initialize the manager.
	mgr := s.manager(c)

	// Add an OS snap.
	snapInfo := s.mockSnap(c, ubuntuCoreSnapYaml)

	// Run the setup-profiles task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
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

func (s *interfaceManagerSuite) TestDoSetupSnapSecurityReloadsConnectionsWhenInvokedOnPlugSide(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	snapInfo := s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	s.testDoSetupSnapSecurityReloadsConnectionsWhenInvokedOn(c, snapInfo.InstanceName(), snapInfo.Revision)

	// Ensure that the backend was used to setup security of both snaps
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[1].AppSet.InstanceName(), Equals, "producer")

	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, DeepEquals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecurityReloadsConnectionsWhenInvokedOnSlotSide(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	snapInfo := s.mockSnap(c, producerYaml)
	s.testDoSetupSnapSecurityReloadsConnectionsWhenInvokedOn(c, snapInfo.InstanceName(), snapInfo.Revision)

	// Ensure that the backend was used to setup security of both snaps
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, "producer")
	c.Check(s.secBackend.SetupCalls[1].AppSet.InstanceName(), Equals, "consumer")

	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, DeepEquals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) testDoSetupSnapSecurityReloadsConnectionsWhenInvokedOn(c *C, snapName string, revision snap.Revision) {
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
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}})
}

// The setup-profiles task will honor snapstate.DevMode flag by storing it
// in the SnapState.Flags and by actually setting up security
// using that flag. Old copy of SnapState.Flag's DevMode is saved for the undo
// handler under `old-devmode`.
func (s *interfaceManagerSuite) TestSetupProfilesHonorsDevMode(c *C) {
	s.MockModel(c, nil)

	// Put the OS snap in place.
	_ = s.manager(c)

	// Initialize the manager. This registers the OS snap.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-profiles task and let it finish.
	// Note that the task will see SnapSetup.Flags equal to DeveloperMode.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
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
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, "snap")
	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{DevMode: true})
}

func (s *interfaceManagerSuite) TestSetupProfilesSetupManyError(c *C) {
	s.secBackend.SetupCallback = func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
		return fmt.Errorf("fail")
	}

	s.MockModel(c, nil)

	// Put the OS snap in place.
	_ = s.manager(c)

	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-profiles task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(change.Status(), Equals, state.ErrorStatus)
	c.Check(change.Err(), ErrorMatches, `cannot perform the following tasks:\n-  \(fail\)`)
}

func (s *interfaceManagerSuite) TestSetupSecurityByBackendInvalidNumberOfSnaps(c *C) {
	mgr := s.manager(c)

	st := s.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("foo", "")
	appSets := []*interfaces.SnapAppSet{}
	opts := []interfaces.ConfinementOptions{{}}
	err := mgr.SetupSecurityByBackend(task, appSets, opts, nil)
	c.Check(err, ErrorMatches, `internal error: setupSecurityByBackend received an unexpected number of snaps.*`)
}

// setup-profiles uses the new snap.Info when setting up security for the new
// snap when it had prior connections and DisconnectSnap() returns it as a part
// of the affected set.
func (s *interfaceManagerSuite) TestSetupProfilesUsesFreshSnapInfo(c *C) {
	s.MockModel(c, nil)

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

	// Validity check, the revisions are different.
	c.Assert(oldSnapInfo.Revision, Not(Equals), 42)
	c.Assert(newSnapInfo.Revision, Equals, snap.R(42))

	// Run the setup-profiles task for the new revision and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: newSnapInfo.SnapName(),
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
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, newSnapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[0].AppSet.Info().Revision, Equals, newSnapInfo.Revision)
	// The OS snap was setup (because it was affected).
	c.Check(s.secBackend.SetupCalls[1].AppSet.InstanceName(), Equals, coreSnapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[1].AppSet.Info().Revision, Equals, coreSnapInfo.Revision)
}

func (s *interfaceManagerSuite) TestSetupProfilesOnInstall(c *C) {
	s.MockModel(c, nil)

	installSnapInfo := s.mockSnap(c, sampleSnapYaml)
	// nothing is in the state yet on install
	s.state.Lock()
	snapstate.Set(s.state, "snap", nil)
	s.state.Unlock()

	// Initialize the manager. This registers both of the snaps and reloads the
	// connection between them.
	_ = s.manager(c)

	// Run the setup-profiles task for the new revision and let it finish.
	change := s.addSetupSnapSecurityChangeWithOptions(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: installSnapInfo.SnapName(),
			Revision: installSnapInfo.Revision,
		},
	}, setupSnapSecurityChangeOptions{
		install: true,
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 1)
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, installSnapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[0].AppSet.Info().Revision, Equals, installSnapInfo.Revision)
}

func (s *interfaceManagerSuite) TestSetupProfilesInstallComponent(c *C) {
	s.MockModel(c, nil)

	snapInfo := s.mockSnap(c, sampleSnapWithComponentsYaml)

	compInfo := snaptest.MockComponent(c, sampleComponentYaml, snapInfo, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	// initialize the manager
	_ = s.manager(c)

	change := s.addSetupSnapSecurityChangeFromComponent(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	}, &snapstate.ComponentSetup{
		CompSideInfo: &snap.ComponentSideInfo{
			Component: compInfo.Component,
			Revision:  snap.R(1),
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 1)

	appSet := s.secBackend.SetupCalls[0].AppSet
	c.Check(appSet.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(appSet.Info().Revision, Equals, snapInfo.Revision)

	// the snap defines another component, comp2. note that it is not listed
	// here because it is not installed.
	c.Check(appSet.Runnables(), testutil.DeepUnsortedMatches, []snap.Runnable{
		{
			CommandName: "app",
			SecurityTag: "snap.snap.app",
		},
		{
			CommandName: "snap+comp1.hook.install",
			SecurityTag: "snap.snap+comp1.hook.install",
		},
	})
}

func (s *interfaceManagerSuite) mockComponentForSnap(c *C, compName string, compYaml string, snapInfo *snap.Info) *snap.ComponentInfo {
	compInfo := snaptest.MockComponent(c, compYaml, snapInfo, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	s.state.Lock()
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapInfo.InstanceName(), &snapst), IsNil)

	snapst.Sequence.AddComponentForRevision(snapInfo.Revision, &sequence.ComponentState{
		SideInfo: &snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapInfo.SnapName(), compName),
			Revision:  snap.R(1),
		},
		CompType: snap.TestComponent,
	})

	snapstate.Set(s.state, snapInfo.InstanceName(), &snapst)

	return compInfo
}

func (s *interfaceManagerSuite) TestSetupProfilesInstallComponentSnapHasPreexistingComponent(c *C) {
	s.MockModel(c, nil)

	snapInfo := s.mockSnap(c, sampleSnapWithComponentsYaml)
	s.mockComponentForSnap(c, "comp2", "component: snap+comp2\ntype: test", snapInfo)

	compInfo := snaptest.MockComponent(c, sampleComponentYaml, snapInfo, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	// initialize the manager
	_ = s.manager(c)

	change := s.addSetupSnapSecurityChangeFromComponent(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	}, &snapstate.ComponentSetup{
		CompSideInfo: &snap.ComponentSideInfo{
			Component: compInfo.Component,
			Revision:  snap.R(1),
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 1)

	appSet := s.secBackend.SetupCalls[0].AppSet
	c.Check(appSet.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(appSet.Info().Revision, Equals, snapInfo.Revision)

	// the snap defines another component, comp2. note that it is not listed
	// here because it is not installed.
	c.Check(appSet.Runnables(), testutil.DeepUnsortedMatches, []snap.Runnable{
		{
			CommandName: "app",
			SecurityTag: "snap.snap.app",
		},
		{
			CommandName: "snap+comp1.hook.install",
			SecurityTag: "snap.snap+comp1.hook.install",
		},
		{
			CommandName: "snap+comp2.hook.pre-refresh",
			SecurityTag: "snap.snap+comp2.hook.pre-refresh",
		},
	})
}

func (s *interfaceManagerSuite) TestSetupProfilesUpdateSnapWithComponents(c *C) {
	s.MockModel(c, nil)

	snapInfo := s.mockSnap(c, sampleSnapWithComponentsYaml)

	s.mockComponentForSnap(c, "comp2", "component: snap+comp2\ntype: test", snapInfo)

	compInfo := snaptest.MockComponent(c, sampleComponentYaml, snapInfo, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	// initialize the manager
	_ = s.manager(c)

	change := s.addSetupSnapSecurityChangeFromComponent(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	}, &snapstate.ComponentSetup{
		CompSideInfo: &snap.ComponentSideInfo{
			Component: compInfo.Component,
			Revision:  snap.R(1),
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 1)

	appSet := s.secBackend.SetupCalls[0].AppSet
	c.Check(appSet.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(appSet.Info().Revision, Equals, snapInfo.Revision)

	// the snap defines another component, comp2. note that it is not listed
	// here because it is not installed.
	c.Check(appSet.Runnables(), testutil.DeepUnsortedMatches, []snap.Runnable{
		{
			CommandName: "app",
			SecurityTag: "snap.snap.app",
		},
		{
			CommandName: "snap+comp1.hook.install",
			SecurityTag: "snap.snap+comp1.hook.install",
		},
		{
			CommandName: "snap+comp2.hook.pre-refresh",
			SecurityTag: "snap.snap+comp2.hook.pre-refresh",
		},
	})
}

func (s *interfaceManagerSuite) TestSetupProfilesOfAffectedSnapWithComponents(c *C) {
	s.MockModel(c, nil)

	snapInfo := s.mockSnap(c, sampleSnapWithComponentsYaml)
	snaptest.MockComponent(c, "component: snap+comp2\ntype: test", snapInfo, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	// core snap is here so that it appears as an affected snap when "snap" has
	// its profiles setup
	coreSnapInfo := s.mockSnap(c, ubuntuCoreSnapWithComponentYaml)
	snaptest.MockComponent(c, "component: ubuntu-core+comp\ntype: test", coreSnapInfo, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	s.state.Lock()
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapInfo.InstanceName(), &snapst), IsNil)

	// add a preexisting component to make sure that we create an app set that
	// includes it
	snapst.Sequence.AddComponentForRevision(snapInfo.Revision, &sequence.ComponentState{
		SideInfo: &snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapInfo.SnapName(), "comp2"),
			Revision:  snap.R(1),
		},
		CompType: snap.TestComponent,
	})

	// add a component to the affected snap, we should see this in the final
	// call to Setup in the backend
	var coreSnapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, coreSnapInfo.InstanceName(), &coreSnapst), IsNil)
	coreSnapst.Sequence.AddComponentForRevision(snapInfo.Revision, &sequence.ComponentState{
		SideInfo: &snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapInfo.SnapName(), "comp"),
			Revision:  snap.R(1),
		},
		CompType: snap.TestComponent,
	})

	snapstate.Set(s.state, snapInfo.InstanceName(), &snapst)
	snapstate.Set(s.state, coreSnapInfo.InstanceName(), &coreSnapst)

	s.state.Unlock()

	compInfo := snaptest.MockComponent(c, sampleComponentYaml, snapInfo, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	// initialize the manager
	_ = s.manager(c)

	change := s.addSetupSnapSecurityChangeFromComponent(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	}, &snapstate.ComponentSetup{
		CompSideInfo: &snap.ComponentSideInfo{
			Component: compInfo.Component,
			Revision:  snap.R(1),
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 3)

	firstAppSet := s.secBackend.SetupCalls[0].AppSet
	c.Check(firstAppSet.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(firstAppSet.Info().Revision, Equals, snapInfo.Revision)

	secondAppSet := s.secBackend.SetupCalls[1].AppSet
	c.Check(secondAppSet.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(secondAppSet.Info().Revision, Equals, snapInfo.Revision)

	thirdAppSet := s.secBackend.SetupCalls[2].AppSet
	c.Check(thirdAppSet.InstanceName(), Equals, coreSnapInfo.InstanceName())
	c.Check(thirdAppSet.Info().Revision, Equals, coreSnapInfo.Revision)

	// the snap defines another component, comp2. note that it is not listed
	// here because it is not installed.
	c.Check(firstAppSet.Runnables(), testutil.DeepUnsortedMatches, []snap.Runnable{
		{
			CommandName: "app",
			SecurityTag: "snap.snap.app",
		},
		{
			CommandName: "snap+comp1.hook.install",
			SecurityTag: "snap.snap+comp1.hook.install",
		},
		{
			CommandName: "snap+comp2.hook.pre-refresh",
			SecurityTag: "snap.snap+comp2.hook.pre-refresh",
		},
	})

	c.Check(secondAppSet.Runnables(), testutil.DeepUnsortedMatches, []snap.Runnable{
		{
			CommandName: "app",
			SecurityTag: "snap.snap.app",
		},
		{
			CommandName: "snap+comp1.hook.install",
			SecurityTag: "snap.snap+comp1.hook.install",
		},
		{
			CommandName: "snap+comp2.hook.pre-refresh",
			SecurityTag: "snap.snap+comp2.hook.pre-refresh",
		},
	})

	c.Check(thirdAppSet.Runnables(), testutil.DeepUnsortedMatches, []snap.Runnable{
		{
			CommandName: "ubuntu-core+comp.hook.install",
			SecurityTag: "snap.ubuntu-core+comp.hook.install",
		},
	})
}

func (s *interfaceManagerSuite) TestSetupProfilesKeepsUndesiredConnection(c *C) {
	undesired := true
	byGadget := false
	s.testAutoconnectionsRemovedForMissingPlugs(c, undesired, byGadget, map[string]interface{}{
		"snap:test1 ubuntu-core:test1": map[string]interface{}{"interface": "test1", "auto": true, "undesired": true},
		"snap:test2 ubuntu-core:test2": map[string]interface{}{"interface": "test2", "auto": true},
	})
}

func (s *interfaceManagerSuite) TestSetupProfilesRemovesMissingAutoconnectedPlugs(c *C) {
	s.testAutoconnectionsRemovedForMissingPlugs(c, false, false, map[string]interface{}{
		"snap:test2 ubuntu-core:test2": map[string]interface{}{"interface": "test2", "auto": true},
	})
}

func (s *interfaceManagerSuite) TestSetupProfilesKeepsMissingGadgetAutoconnectedPlugs(c *C) {
	undesired := false
	byGadget := true
	s.testAutoconnectionsRemovedForMissingPlugs(c, undesired, byGadget, map[string]interface{}{
		"snap:test1 ubuntu-core:test1": map[string]interface{}{"interface": "test1", "auto": true, "by-gadget": true},
		"snap:test2 ubuntu-core:test2": map[string]interface{}{"interface": "test2", "auto": true},
	})
}

func (s *interfaceManagerSuite) testAutoconnectionsRemovedForMissingPlugs(c *C, undesired, byGadget bool, expectedConns map[string]interface{}) {
	s.MockModel(c, nil)

	// Mock the interface that will be used by the test
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test1"}, &ifacetest.TestInterface{InterfaceName: "test2"})

	// Put the OS and the sample snap in place.
	_ = s.mockSnap(c, ubuntuCoreSnapYaml2)
	newSnapInfo := s.mockSnap(c, refreshedSnapYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"snap:test1 ubuntu-core:test1": map[string]interface{}{"interface": "test1", "auto": true, "undesired": undesired, "by-gadget": byGadget},
	})
	s.state.Unlock()

	_ = s.manager(c)

	// Run the setup-profiles task for the new revision and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: newSnapInfo.SnapName(),
			Revision: newSnapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	// Verify that old connection is gone and new one got connected
	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Check(conns, DeepEquals, expectedConns)
}

func (s *interfaceManagerSuite) TestSetupProfilesRemovesMissingAutoconnectedSlots(c *C) {
	s.testAutoconnectionsRemovedForMissingSlots(c, map[string]interface{}{
		"snap:test2 snap2:test2": map[string]interface{}{"interface": "test2", "auto": true},
	})
}

func (s *interfaceManagerSuite) testAutoconnectionsRemovedForMissingSlots(c *C, expectedConns map[string]interface{}) {
	s.MockModel(c, nil)

	// Mock the interface that will be used by the test
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test1"}, &ifacetest.TestInterface{InterfaceName: "test2"})

	// Put sample snaps in place.
	newSnapInfo1 := s.mockSnap(c, refreshedSnapYaml2)
	_ = s.mockSnap(c, slotSnapYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"snap:test1 snap2:test1": map[string]interface{}{"interface": "test1", "auto": true},
	})
	s.state.Unlock()

	_ = s.manager(c)

	// Run the setup-profiles task for the new revision and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: newSnapInfo1.SnapName(),
			Revision: newSnapInfo1.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	// Verify that old connection is gone and new one got connected
	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Check(conns, DeepEquals, expectedConns)
}

// auto-connect needs to setup security for connected slots after autoconnection
func (s *interfaceManagerSuite) TestAutoConnectSetupSecurityForConnectedSlots(c *C) {
	s.MockModel(c, nil)

	// Add an OS snap.
	coreSnapInfo := s.mockSnap(c, ubuntuCoreSnapYaml)

	// Initialize the manager. This registers the OS snap.
	_ = s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
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

	// The sample snap was setup, with the correct new revision:
	// 1st call is for initial setup-profiles, 2nd call is for setup-profiles after connect task.
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[0].AppSet.Info().Revision, Equals, snapInfo.Revision)

	c.Check(s.secBackend.SetupCalls[1].AppSet.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[1].AppSet.Info().Revision, Equals, snapInfo.Revision)

	// The OS snap was setup (because its connected to sample snap).
	c.Check(s.secBackend.SetupCalls[2].AppSet.InstanceName(), Equals, coreSnapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[2].AppSet.Info().Revision, Equals, coreSnapInfo.Revision)
}

// auto-connect needs to setup security for connected slots after autoconnection
func (s *interfaceManagerSuite) TestAutoConnectSetupSecurityOnceWithMultiplePlugs(c *C) {
	s.MockModel(c, nil)

	// Add an OS snap.
	_ = s.mockSnap(c, ubuntuCoreSnapYaml)

	// Initialize the manager. This registers the OS snap.
	mgr := s.manager(c)

	// Add a sample snap with a multiple plugs which should be auto-connected.
	snapInfo := s.mockSnap(c, sampleSnapYamlManyPlugs)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	})
	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that the task succeeded.
	c.Assert(change.Err(), IsNil)
	c.Assert(change.Status(), Equals, state.DoneStatus)

	repo := mgr.Repository()

	for _, ifaceName := range []string{"network", "home", "x11", "wayland"} {
		cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap", Name: ifaceName}, SlotRef: interfaces.SlotRef{Snap: "ubuntu-core", Name: ifaceName}}
		conn, _ := repo.Connection(cref)
		c.Check(conn, NotNil, Commentf("missing connection for %s interface", ifaceName))
	}

	// Three backend calls: initial setup profiles, 2 setup calls for both core and snap.
	c.Assert(s.secBackend.SetupCalls, HasLen, 3)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	setupCalls := make(map[string]int)
	for _, sc := range s.secBackend.SetupCalls {
		setupCalls[sc.AppSet.InstanceName()]++
	}
	c.Check(setupCalls["snap"], Equals, 2)
	c.Check(setupCalls["ubuntu-core"], Equals, 1)
}

func (s *interfaceManagerSuite) TestDoDiscardConnsPlug(c *C) {
	s.testDoDiscardConns(c, "consumer")
}

func (s *interfaceManagerSuite) TestDoDiscardConnsSlot(c *C) {
	s.testDoDiscardConns(c, "producer")
}

func (s *interfaceManagerSuite) TestUndoDiscardConnsPlug(c *C) {
	s.testUndoDiscardConns(c, "consumer")
}

func (s *interfaceManagerSuite) TestUndoDiscardConnsSlot(c *C) {
	s.testUndoDiscardConns(c, "producer")
}

func (s *interfaceManagerSuite) testDoDiscardConns(c *C, snapName string) {
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

	s.manager(c)

	s.state.Lock()
	// remove the snaps so that discard-conns doesn't complain about snaps still installed
	snapstate.Set(s.state, "producer", nil)
	snapstate.Set(s.state, "consumer", nil)
	s.state.Unlock()

	// Run the discard-conns task and let it finish
	change, _ := s.addDiscardConnsChange(snapName)

	s.settle(c)

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

func (s *interfaceManagerSuite) testUndoDiscardConns(c *C, snapName string) {
	s.manager(c)

	s.state.Lock()
	// Store information about a connection in the state.
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})

	// Store empty snap state. This snap has an empty sequence now.
	snapstate.Set(s.state, snapName, &snapstate.SnapState{})
	s.state.Unlock()

	// Run the discard-conns task and let it finish
	change, t := s.addDiscardConnsChange(snapName)
	s.state.Lock()
	terr := s.state.NewTask("error-trigger", "provoking undo")
	terr.WaitFor(t)
	change.AddTask(terr)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(change.Status().Ready(), Equals, true)
	c.Assert(t.Status(), Equals, state.UndoneStatus)

	// Information about the connection is intact
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})

	var removed map[string]interface{}
	err = change.Tasks()[0].Get("removed", &removed)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestDoRemove(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	mgr := s.manager(c)

	func() {
		s.state.Lock()
		defer s.state.Unlock()
		// mock relevant unlink-snap behavior
		var snapst snapstate.SnapState
		c.Assert(snapstate.Get(s.state, "consumer", &snapst), IsNil)
		snapst.Active = false
		snapstate.Set(s.state, "consumer", &snapst)
		c.Check(ifacestate.OnSnapLinkageChanged(s.state, &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "consumer"}}), IsNil)
	}()

	// Run the remove-security task
	change := s.addRemoveSnapSecurityChange("consumer")
	s.se.Ensure()
	s.se.Wait()
	s.se.Stop()

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
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, "producer")

	// Connection state was left intact
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})

	// no pending SideInfo
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, "consumer", &snapst), IsNil)
	c.Check(snapst.PendingSecurity, DeepEquals, &snapstate.PendingSecurityState{})
}

func (s *interfaceManagerSuite) TestConnectTracksConnectionsInState(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})

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
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, "producer")
	c.Check(s.secBackend.SetupCalls[1].AppSet.InstanceName(), Equals, "consumer")

	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, DeepEquals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestConnectWithComponentsSetsUpSecurity(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})

	consumerInfo := s.mockSnap(c, consumerWithComponentYaml)
	producerInfo := s.mockSnap(c, producerWithComponentYaml)
	s.mockComponentForSnap(c, "comp", "component: consumer+comp\ntype: test", consumerInfo)
	s.mockComponentForSnap(c, "comp", "component: producer+comp\ntype: test", producerInfo)

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

	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, DeepEquals, interfaces.ConfinementOptions{})

	producerAppSet := s.secBackend.SetupCalls[0].AppSet
	consumerAppSet := s.secBackend.SetupCalls[1].AppSet

	c.Check(producerAppSet.InstanceName(), Equals, "producer")
	c.Check(consumerAppSet.InstanceName(), Equals, "consumer")

	c.Check(producerAppSet.Runnables(), testutil.DeepUnsortedMatches, producerRunnablesFullSet)
	c.Check(consumerAppSet.Runnables(), testutil.DeepUnsortedMatches, consumerRunnablesFullSet)
}

func (s *interfaceManagerSuite) TestConnectSetsHotplugKeyFromTheSlot(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumer2Yaml)
	s.mockSnap(c, coreSnapYaml)

	s.state.Lock()
	s.state.Set("hotplug-slots", map[string]interface{}{
		"slot": map[string]interface{}{
			"name":         "slot",
			"interface":    "test",
			"hotplug-key":  "1234",
			"static-attrs": map[string]interface{}{"attr2": "value2"}}})
	s.state.Unlock()

	_ = s.manager(c)

	s.state.Lock()
	ts, err := ifacestate.Connect(s.state, "consumer2", "plug", "core", "slot")
	c.Assert(err, IsNil)

	change := s.state.NewChange("connect", "")
	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer2:plug core:slot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
			"plug-static": map[string]interface{}{"attr1": "value1"},
			"slot-static": map[string]interface{}{"attr2": "value2"},
		},
	})
}

func (s *interfaceManagerSuite) TestDisconnectSetsUpSecurity(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	s.manager(c)
	conn := s.getConnection(c, "consumer", "plug", "producer", "slot")

	s.state.Lock()
	ts, err := ifacestate.Disconnect(s.state, conn)
	c.Assert(err, IsNil)
	ts.Tasks()[0].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change := s.state.NewChange("disconnect", "")
	change.AddAll(ts)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Err(), IsNil)
	c.Check(change.Status(), Equals, state.DoneStatus)

	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[1].AppSet.InstanceName(), Equals, "producer")

	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, DeepEquals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestDisconnectTracksConnectionsInState(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})
	s.state.Unlock()

	s.manager(c)

	conn := s.getConnection(c, "consumer", "plug", "producer", "slot")
	s.state.Lock()
	ts, err := ifacestate.Disconnect(s.state, conn)
	c.Assert(err, IsNil)
	ts.Tasks()[0].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change := s.state.NewChange("disconnect", "")
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
	c.Check(conns, DeepEquals, map[string]interface{}{})
}

func (s *interfaceManagerSuite) TestDisconnectDisablesAutoConnect(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	plugAppSet := s.mockAppSet(c, consumerYaml)
	slotAppSet := s.mockAppSet(c, producerYaml)
	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test", "auto": true},
	})
	s.state.Unlock()

	s.manager(c)

	s.state.Lock()

	conn := &interfaces.Connection{
		Plug: interfaces.NewConnectedPlug(plugAppSet.Info().Plugs["plug"], plugAppSet, nil, nil),
		Slot: interfaces.NewConnectedSlot(slotAppSet.Info().Slots["slot"], slotAppSet, nil, nil),
	}

	ts, err := ifacestate.Disconnect(s.state, conn)
	c.Assert(err, IsNil)
	ts.Tasks()[0].Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer",
		},
	})

	change := s.state.NewChange("disconnect", "")
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
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test", "auto": true, "undesired": true},
	})
}

func (s *interfaceManagerSuite) TestDisconnectByHotplug(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	var consumerYaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: test
  attr: plug-attr
`
	consumerAppSet := s.mockAppSet(c, consumerYaml)
	coreAppSet := s.mockAppSet(c, coreSnapYaml)
	coreAppSet.Info().Slots["hotplug-slot"] = &snap.SlotInfo{
		Snap:       coreAppSet.Info(),
		Name:       "hotplug-slot",
		HotplugKey: "1234",
	}

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplug-slot": map[string]interface{}{"interface": "test"},
		"consumer:plug core:slot2":        map[string]interface{}{"interface": "test"},
	})
	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplug-slot": map[string]interface{}{
			"name":        "hotplug-slot",
			"interface":   "test",
			"hotplug-key": "1234",
		}})
	s.state.Unlock()

	s.manager(c)

	s.state.Lock()

	conn := &interfaces.Connection{
		Plug: interfaces.NewConnectedPlug(consumerAppSet.Info().Plugs["plug"], consumerAppSet, nil, nil),
		Slot: interfaces.NewConnectedSlot(coreAppSet.Info().Slots["hotplug-slot"], coreAppSet, nil, nil),
	}

	ts, err := ifacestate.DisconnectPriv(s.state, conn, ifacestate.NewDisconnectOptsWithByHotplugSet())
	c.Assert(err, IsNil)

	change := s.state.NewChange("disconnect", "")
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
		"consumer:plug core:hotplug-slot": map[string]interface{}{
			"interface":    "test",
			"hotplug-gone": true,
		},
		"consumer:plug core:slot2": map[string]interface{}{
			"interface": "test",
		},
	})
}

func (s *interfaceManagerSuite) TestAutoDisconnectIgnoreHookError(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	mgr := s.hookManager(c)

	// fail when running the disconnect hooks
	hijackFunc := func(*hookstate.Context) error { return errors.New("test") }
	mgr.RegisterHijack("disconnect-plug-plug", "consumer", hijackFunc)
	mgr.RegisterHijack("disconnect-slot-slot", "producer", hijackFunc)

	var consumerYaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: test
hooks:
  disconnect-plug-plug:
`

	var producerYaml = `
name: producer
version: 1
slots:
 slot:
  interface: test
hooks:
  disconnect-slot-slot:
`
	consumerAppSet := s.mockAppSet(c, consumerYaml)
	producerAppSet := s.mockAppSet(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{"interface": "test"},
	})

	s.state.Unlock()
	s.manager(c)
	s.state.Lock()

	conn := &interfaces.Connection{
		Plug: interfaces.NewConnectedPlug(consumerAppSet.Info().Plugs["plug"], consumerAppSet, nil, nil),
		Slot: interfaces.NewConnectedSlot(producerAppSet.Info().Slots["slot"], producerAppSet, nil, nil),
	}

	// call disconnect with the AutoDisconnect flag (used when removing a snap)
	ts, err := ifacestate.DisconnectPriv(s.state, conn, ifacestate.NewDisconnectOptsWithAutoSet())
	c.Assert(err, IsNil)

	change := s.state.NewChange("disconnect", "")
	change.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	// the hook setup should be set to ignore errors
	var hooksCount int
	for _, t := range ts.Tasks() {
		if t.Kind() != "run-hook" {
			continue
		}

		var hooksetup hookstate.HookSetup
		err = t.Get("hook-setup", &hooksetup)
		c.Assert(err, IsNil)
		c.Check(hooksetup.IgnoreError, Equals, true)

		err = t.Get("undo-hook-setup", &hooksetup)
		c.Assert(err, IsNil)
		c.Check(hooksetup.IgnoreError, Equals, true)

		hooksCount++
	}

	// should have two disconnection tasks
	c.Assert(hooksCount, Equals, 2)

	// the change should not have failed
	c.Check(change.Err(), IsNil)
	c.Assert(change.Status(), Equals, state.DoneStatus)
}

func (s *interfaceManagerSuite) TestManagerReloadsConnections(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
	var consumerYaml = `
name: consumer
version: 1
plugs:
 plug:
  interface: content
  content: foo
  attr: plug-value
`
	var producerYaml = `
name: producer
version: 1
slots:
 slot:
  interface: content
  content: foo
  attr: slot-value
`
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "content",
			"plug-static": map[string]interface{}{
				"content":    "foo",
				"attr":       "stored-plug-value",
				"other-attr": "irrelevant-value",
			},
			"slot-static": map[string]interface{}{
				"interface":  "content",
				"content":    "foo",
				"attr":       "stored-slot-value",
				"other-attr": "irrelevant-value",
			},
		},
	})
	s.state.Unlock()

	mgr := s.manager(c)
	repo := mgr.Repository()

	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 1)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}}
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{cref})

	conn, err := repo.Connection(cref)
	c.Assert(err, IsNil)
	c.Assert(conn.Plug.Name(), Equals, "plug")
	c.Assert(conn.Plug.StaticAttrs(), DeepEquals, map[string]interface{}{
		"content": "foo",
		"attr":    "plug-value",
	})
	c.Assert(conn.Slot.Name(), Equals, "slot")
	c.Assert(conn.Slot.StaticAttrs(), DeepEquals, map[string]interface{}{
		"content": "foo",
		"attr":    "slot-value",
	})
}

func (s *interfaceManagerSuite) TestManagerDoesntReloadUndesiredAutoconnections(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})
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

func (s *interfaceManagerSuite) setupHotplugSlot(c *C) {
	s.mockIfaces(&ifacetest.TestHotplugInterface{TestInterface: ifacetest.TestInterface{InterfaceName: "test"}})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, coreSnapYaml)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"slot": map[string]interface{}{
			"name":        "slot",
			"interface":   "test",
			"hotplug-key": "abcd",
		}})
}

func (s *interfaceManagerSuite) TestManagerDoesntReloadHotlugGoneConnection(c *C) {
	s.setupHotplugSlot(c)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:slot": map[string]interface{}{
			"interface":    "test",
			"hotplug-gone": true,
		}})
	s.state.Unlock()

	mgr := s.manager(c)
	c.Assert(mgr.Repository().Interfaces().Connections, HasLen, 0)
}

func (s *interfaceManagerSuite) TestManagerReloadsHotlugConnection(c *C) {
	s.setupHotplugSlot(c)

	s.state.Lock()
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:slot": map[string]interface{}{
			"interface":    "test",
			"hotplug-gone": false,
		}})
	s.state.Unlock()

	mgr := s.manager(c)
	repo := mgr.Repository()
	c.Assert(repo.Interfaces().Connections, HasLen, 1)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "core", Name: "slot"}}
	conn, err := repo.Connection(cref)
	c.Assert(err, IsNil)
	c.Assert(conn, NotNil)
}

func (s *interfaceManagerSuite) TestSetupProfilesDevModeMultiple(c *C) {
	s.MockModel(c, nil)

	mgr := s.manager(c)
	repo := mgr.Repository()

	// setup two snaps that are connected
	siP := s.mockAppSet(c, producerYaml)
	siC := s.mockAppSet(c, consumerYaml)
	err := repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test",
	})
	c.Assert(err, IsNil)
	err = repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test2",
	})
	c.Assert(err, IsNil)

	err = repo.AddAppSet(siC)
	c.Assert(err, IsNil)

	err = repo.AddAppSet(siP)
	c.Assert(err, IsNil)

	connRef := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: siC.InstanceName(), Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: siP.InstanceName(), Name: "slot"},
	}
	_, err = repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: siC.Info().SnapName(),
			Revision: siC.Info().Revision,
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
	c.Assert(s.secBackend.SetupCalls, HasLen, 4)
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, siC.InstanceName())
	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{DevMode: true})
	c.Check(s.secBackend.SetupCalls[1].AppSet.InstanceName(), Equals, siP.InstanceName())
	c.Check(s.secBackend.SetupCalls[1].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[2].AppSet.InstanceName(), Equals, siC.InstanceName())
	c.Check(s.secBackend.SetupCalls[2].Options, DeepEquals, interfaces.ConfinementOptions{DevMode: true})
	c.Check(s.secBackend.SetupCalls[3].AppSet.InstanceName(), Equals, siP.InstanceName())
	c.Check(s.secBackend.SetupCalls[3].Options, DeepEquals, interfaces.ConfinementOptions{})
}

func (s *interfaceManagerSuite) TestCheckInterfacesDeny(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, nil)

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	s.MockSnapDecl(c, "producer", "producer-publisher", nil)
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), ErrorMatches, "installation denied.*")
}

func (s *interfaceManagerSuite) TestCheckInterfacesNoDenyIfNoDecl(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, nil)
	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	// crucially, this test is missing this: s.mockSnapDecl(c, "producer", "producer-publisher", nil)
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), IsNil)
}

func (s *interfaceManagerSuite) TestCheckInterfacesDisallowBasedOnSnapTypeNoSnapDecl(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, nil)

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    allow-installation:
      slot-snap-type:
        - core
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	// no snap decl
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), ErrorMatches, `installation not allowed by "slot" slot rule of interface "test"`)
}

func (s *interfaceManagerSuite) TestCheckInterfacesAllowBasedOnSnapTypeNoSnapDecl(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, nil)

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    allow-installation:
      slot-snap-type:
        - app
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	// no snap decl
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), IsNil)
}

func (s *interfaceManagerSuite) TestCheckInterfacesAllow(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, nil)

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	s.MockSnapDecl(c, "producer", "producer-publisher", map[string]interface{}{
		"format": "1",
		"slots": map[string]interface{}{
			"test": "true",
		},
	})
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), IsNil)
}

func (s *interfaceManagerSuite) TestCheckInterfacesDeviceScopeRightStore(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, map[string]interface{}{
		"store": "my-store",
	})

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	s.MockSnapDecl(c, "producer", "producer-publisher", map[string]interface{}{
		"format": "3",
		"slots": map[string]interface{}{
			"test": map[string]interface{}{
				"allow-installation": map[string]interface{}{
					"on-store": []interface{}{"my-store"},
				},
			},
		},
	})
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), IsNil)
}

func (s *interfaceManagerSuite) TestCheckInterfacesDeviceScopeNoStore(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, nil)

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	s.MockSnapDecl(c, "producer", "producer-publisher", map[string]interface{}{
		"format": "3",
		"slots": map[string]interface{}{
			"test": map[string]interface{}{
				"allow-installation": map[string]interface{}{
					"on-store": []interface{}{"my-store"},
				},
			},
		},
	})
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), ErrorMatches, `installation not allowed.*`)
}

func (s *interfaceManagerSuite) TestCheckInterfacesDeviceScopeWrongStore(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, map[string]interface{}{
		"store": "other-store",
	})

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	s.MockSnapDecl(c, "producer", "producer-publisher", map[string]interface{}{
		"format": "3",
		"slots": map[string]interface{}{
			"test": map[string]interface{}{
				"allow-installation": map[string]interface{}{
					"on-store": []interface{}{"my-store"},
				},
			},
		},
	})
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), ErrorMatches, `installation not allowed.*`)
}

func (s *interfaceManagerSuite) TestCheckInterfacesDeviceScopeRightFriendlyStore(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, map[string]interface{}{
		"store": "my-substore",
	})

	s.MockStore(c, s.state, "my-substore", map[string]interface{}{
		"friendly-stores": []interface{}{"my-store"},
	})

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	s.MockSnapDecl(c, "producer", "producer-publisher", map[string]interface{}{
		"format": "3",
		"slots": map[string]interface{}{
			"test": map[string]interface{}{
				"allow-installation": map[string]interface{}{
					"on-store": []interface{}{"my-store"},
				},
			},
		},
	})
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), IsNil)
}

func (s *interfaceManagerSuite) TestCheckInterfacesDeviceScopeWrongFriendlyStore(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, map[string]interface{}{
		"store": "my-substore",
	})

	s.MockStore(c, s.state, "my-substore", map[string]interface{}{
		"friendly-stores": []interface{}{"other-store"},
	})

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-installation: true
`))
	defer restore()
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})

	s.MockSnapDecl(c, "producer", "producer-publisher", map[string]interface{}{
		"format": "3",
		"slots": map[string]interface{}{
			"test": map[string]interface{}{
				"allow-installation": map[string]interface{}{
					"on-store": []interface{}{"my-store"},
				},
			},
		},
	})
	snapInfo := s.mockSnap(c, producerYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), ErrorMatches, `installation not allowed.*`)
}

func (s *interfaceManagerSuite) TestCheckInterfacesConsidersImplicitSlots(c *C) {
	deviceCtx := s.TrivialDeviceContext(c, nil)
	snapInfo := s.mockSnap(c, ubuntuCoreSnapYaml)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(ifacestate.CheckInterfaces(s.state, snapInfo, deviceCtx), IsNil)
	c.Check(snapInfo.Slots["home"], NotNil)
}

// Test that setup-snap-security gets undone correctly when a snap is installed
// but the installation fails (the security profiles are removed).
func (s *interfaceManagerSuite) TestUndoSetupProfilesOnInstall(c *C) {
	// Create the interface manager
	_ = s.manager(c)

	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Add a change that undoes "setup-snap-security"
	change := s.addSetupSnapSecurityChangeWithOptions(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	}, setupSnapSecurityChangeOptions{
		active:  true, // undo case, snap was active after link-snap
		install: true,
	})
	s.state.Lock()
	c.Assert(change.Tasks(), HasLen, 3)
	change.Tasks()[0].SetStatus(state.UndoStatus)
	change.Tasks()[1].SetStatus(state.UndoStatus)
	change.Tasks()[2].SetStatus(state.UndoneStatus)
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

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snap", &snapst)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestUndoSetupProfilesOnComponentInstall(c *C) {
	s.MockModel(c, nil)

	snapInfo := s.mockSnap(c, sampleSnapWithComponentsYaml)
	s.manager(c)

	compInfo := snaptest.MockComponent(c, sampleComponentYaml, snapInfo, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	change := s.addSetupSnapSecurityChangeFromComponent(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	}, &snapstate.ComponentSetup{
		CompSideInfo: &snap.ComponentSideInfo{
			Component: compInfo.Component,
			Revision:  snap.R(1),
		},
	})

	s.state.Lock()

	c.Assert(change.Tasks(), HasLen, 3)
	errorTask := s.state.NewTask("error-trigger", "...")
	errorTask.WaitFor(change.Tasks()[2])
	change.AddTask(errorTask)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Err(), ErrorMatches, "(?s).*error out.*")
	c.Check(change.Status(), Equals, state.ErrorStatus)

	// since we didn't remove the snap, we're just removing the component, the
	// profiles should be there, but the setup call shouldn't include anything
	// pertaining to the components
	c.Assert(s.secBackend.SetupCalls, HasLen, 2)
	appSetAfterRemoval := s.secBackend.SetupCalls[1].AppSet
	c.Check(appSetAfterRemoval.Runnables(), DeepEquals, []snap.Runnable{
		{
			CommandName: "app",
			SecurityTag: "snap.snap.app",
		},
	})
	c.Assert(s.secBackend.RemoveCalls, HasLen, 0)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snap", &snapst)
	c.Assert(err, IsNil)

	comps, err := snapst.CurrentComponentInfos()
	c.Assert(err, IsNil)

	// make sure that the component was removed
	c.Check(comps, HasLen, 0)
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
	change := s.addSetupSnapSecurityChangeWithOptions(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snap.R(snapInfo.Revision.N + 1),
		},
	}, setupSnapSecurityChangeOptions{
		active: true, // undo case, snap was active after link-snap
	})
	s.state.Lock()
	c.Assert(change.Tasks(), HasLen, 3)
	change.Tasks()[0].SetStatus(state.UndoStatus)
	change.Tasks()[1].SetStatus(state.UndoStatus)
	change.Tasks()[2].SetStatus(state.UndoStatus)
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
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, snapInfo.InstanceName())
	c.Check(s.secBackend.SetupCalls[0].AppSet.Info().Revision, Equals, snapInfo.Revision)
	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{})

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.PendingSecurity.SideInfo.Revision, Equals, snapInfo.Revision)
}

func (s *interfaceManagerSuite) TestManagerTransitionConnectionsCore(c *C) {
	s.mockSnap(c, ubuntuCoreSnapYaml)
	s.mockSnap(c, coreSnapYaml)
	s.mockSnap(c, httpdSnapYaml)

	s.manager(c)

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
	s.se.Ensure()
	s.se.Wait()
	s.se.Stop()
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

	s.manager(c)

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
		s.se.Ensure()
		s.se.Wait()
	}
	s.se.Stop()
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
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "unrelated"})

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
	s.MockModel(c, nil)

	// Add both the old and new core snaps
	s.mockSnap(c, ubuntuCoreSnapYaml)
	s.mockSnap(c, coreSnapYaml)

	// Initialize the manager. This registers both of the core snaps.
	mgr := s.manager(c)

	// Add a sample snap with a "network" plug which should be auto-connected.
	// Normally it would not be auto connected because there are multiple
	// providers but we have special support for this case so the old
	// ubuntu-core snap is ignored and we pick the new core snap.
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
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
	c.Check(ifaces.Connections, DeepEquals, []*interfaces.ConnRef{{
		PlugRef: interfaces.PlugRef{Snap: "snap", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "network"}}})
}

func (s *interfaceManagerSuite) TestAutoConnectSnapdAndCore(c *C) {
	s.MockModel(c, nil)

	const snapdSnapYaml = `
name: snapd
version: 1
type: snapd
`

	// we don't actually need to mock the mapper (since the test replaces it),
	// but we do need to put it back once the test is over
	restore := ifacestate.MockSnapMapper(&ifacestate.CoreCoreSystemMapper{})
	defer restore()

	// mock both core and snapd, since these will both provide the network slot.
	// when they are added to the repo, only the snapd snap should have gotten
	// implicit slots added to it. this test ensures that, since the auto
	// connection will only succeed if the plug has one connection candidate.
	s.mockSnap(c, snapdSnapYaml)
	s.mockSnap(c, coreSnapYaml)

	mgr := s.manager(c)

	// mock a snap with a network plug, this should connect to snapd, since core
	// shouldn't get any implicit slots added to it
	snapInfo := s.mockSnap(c, sampleSnapYaml)

	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	})

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(change.Status(), Equals, state.DoneStatus)

	// make sure that network is connected, note that it is still recorded as
	// connected to core, even though the is is actually connected to snapd
	var conns map[string]interface{}
	err := s.state.Get("conns", &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"snap:network core:network": map[string]interface{}{
			"interface": "network", "auto": true,
		},
	})

	// check the connection in the repo
	repo := mgr.Repository()
	plug := repo.Plug("snap", "network")
	c.Assert(plug, NotNil)

	ifaces := repo.Interfaces()
	c.Assert(ifaces.Connections, HasLen, 1)
	c.Check(ifaces.Connections[0], DeepEquals, &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "snap", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "snapd", Name: "network"},
	})
}

func makeAutoConnectChange(st *state.State, plugSnap, plug, slotSnap, slot string, delayedSetupProfiles bool) *state.Change {
	chg := st.NewChange("connect...", "...")

	t := st.NewTask("connect", "other connect task")
	t.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slot})
	t.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plug})
	var plugAttrs, slotAttrs map[string]interface{}
	t.Set("plug-dynamic", plugAttrs)
	t.Set("slot-dynamic", slotAttrs)
	t.Set("auto", true)
	t.Set("delayed-setup-profiles", delayedSetupProfiles)

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

func (s *interfaceManagerSuite) mockConnectForUndo(c *C, conns map[string]interface{}, delayedSetupProfiles bool) *state.Change {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	s.manager(c)

	producer := s.mockSnap(c, producerWithComponentYaml)
	consumer := s.mockSnap(c, consumerWithComponentYaml)

	consumerComp := s.mockComponentForSnap(c, "comp", "component: consumer+comp\ntype: test", consumer)
	producerComp := s.mockComponentForSnap(c, "comp", "component: producer+comp\ntype: test", producer)

	producerAppSet, err := interfaces.NewSnapAppSet(producer, []*snap.ComponentInfo{producerComp})
	c.Assert(err, IsNil)

	consumerAppSet, err := interfaces.NewSnapAppSet(consumer, []*snap.ComponentInfo{consumerComp})
	c.Assert(err, IsNil)

	repo := s.manager(c).Repository()
	err = repo.AddAppSet(consumerAppSet)
	c.Assert(err, IsNil)

	err = repo.AddAppSet(producerAppSet)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("conns", conns)

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot", delayedSetupProfiles)
	terr := s.state.NewTask("error-trigger", "provoking undo")
	connTasks := chg.Tasks()
	terr.WaitAll(state.NewTaskSet(connTasks...))
	chg.AddTask(terr)

	return chg
}

func (s *interfaceManagerSuite) TestUndoConnect(c *C) {
	// "consumer:plug producer:slot" wouldn't normally be present in conns when connecting because
	// ifacestate.Connect() checks for existing connection; it's used here to test removal on undo.
	conns := map[string]interface{}{
		"snap1:plug snap2:slot":       map[string]interface{}{},
		"consumer:plug producer:slot": map[string]interface{}{},
	}
	chg := s.mockConnectForUndo(c, conns, false)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status().Ready(), Equals, true)
	for _, t := range chg.Tasks() {
		if t.Kind() != "error-trigger" {
			c.Assert(t.Status(), Equals, state.UndoneStatus)
			var old interface{}
			c.Assert(t.Get("old-conn", &old), NotNil)
		}
	}

	// connection is removed from conns, other connection is left intact
	var realConns map[string]interface{}
	c.Assert(s.state.Get("conns", &realConns), IsNil)
	c.Check(realConns, DeepEquals, map[string]interface{}{
		"snap1:plug snap2:slot": map[string]interface{}{},
	})

	cref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	// and it's not in the repo
	_, err := s.manager(c).Repository().Connection(cref)
	notConnected, _ := err.(*interfaces.NotConnectedError)
	c.Check(notConnected, NotNil)

	c.Assert(s.secBackend.SetupCalls, HasLen, 4)
	c.Check(s.secBackend.SetupCalls[0].AppSet.InstanceName(), Equals, "producer")
	c.Check(s.secBackend.SetupCalls[1].AppSet.InstanceName(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[0].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[1].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[0].AppSet.Runnables(), testutil.DeepUnsortedMatches, producerRunnablesFullSet)
	c.Check(s.secBackend.SetupCalls[1].AppSet.Runnables(), testutil.DeepUnsortedMatches, consumerRunnablesFullSet)

	// by undo
	c.Check(s.secBackend.SetupCalls[2].AppSet.InstanceName(), Equals, "producer")
	c.Check(s.secBackend.SetupCalls[3].AppSet.InstanceName(), Equals, "consumer")
	c.Check(s.secBackend.SetupCalls[2].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[3].Options, DeepEquals, interfaces.ConfinementOptions{})
	c.Check(s.secBackend.SetupCalls[2].AppSet.Runnables(), testutil.DeepUnsortedMatches, producerRunnablesFullSet)
	c.Check(s.secBackend.SetupCalls[3].AppSet.Runnables(), testutil.DeepUnsortedMatches, consumerRunnablesFullSet)
}

func (s *interfaceManagerSuite) TestUndoConnectUndesired(c *C) {
	// "consumer:plug producer:slot" wouldn't normally be present in conns when connecting because
	// ifacestate.Connect() checks for existing connection; it's used here to test removal on undo.
	conns := map[string]interface{}{
		"snap1:plug snap2:slot":       map[string]interface{}{},
		"consumer:plug producer:slot": map[string]interface{}{"undesired": true},
	}
	chg := s.mockConnectForUndo(c, conns, false)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status().Ready(), Equals, true)
	for _, t := range chg.Tasks() {
		if t.Kind() != "error-trigger" {
			c.Assert(t.Status(), Equals, state.UndoneStatus)
			if t.Kind() == "connect" {
				var old interface{}
				c.Assert(t.Get("old-conn", &old), IsNil)
				c.Check(old, DeepEquals, map[string]interface{}{"undesired": true})
			}
		}
	}

	// connection is left in conns because of undesired flag
	var realConns map[string]interface{}
	c.Assert(s.state.Get("conns", &realConns), IsNil)
	c.Check(realConns, DeepEquals, map[string]interface{}{
		"snap1:plug snap2:slot":       map[string]interface{}{},
		"consumer:plug producer:slot": map[string]interface{}{"undesired": true},
	})

	// but it's not in the repo
	cref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	_, err := s.manager(c).Repository().Connection(cref)
	notConnected, _ := err.(*interfaces.NotConnectedError)
	c.Check(notConnected, NotNil)

	c.Assert(s.secBackend.SetupCalls, HasLen, 4)

	producerAppSet := s.secBackend.SetupCalls[2].AppSet
	c.Check(producerAppSet.InstanceName(), Equals, "producer")
	c.Check(producerAppSet.Runnables(), testutil.DeepUnsortedMatches, producerRunnablesFullSet)

	consumerAppSet := s.secBackend.SetupCalls[3].AppSet
	c.Check(consumerAppSet.InstanceName(), Equals, "consumer")
	c.Check(consumerAppSet.Runnables(), testutil.DeepUnsortedMatches, consumerRunnablesFullSet)
}

func (s *interfaceManagerSuite) TestUndoConnectNoSetupProfilesWithDelayedSetupProfiles(c *C) {
	conns := map[string]interface{}{"consumer:plug producer:slot": map[string]interface{}{}}

	delayedSetupProfiles := true
	chg := s.mockConnectForUndo(c, conns, delayedSetupProfiles)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status().Ready(), Equals, true)

	// connection is removed from conns
	var realConns map[string]interface{}
	c.Assert(s.state.Get("conns", &realConns), IsNil)
	c.Check(realConns, HasLen, 0)

	cref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	// and it's not in the repo
	_, err := s.manager(c).Repository().Connection(cref)
	notConnected, _ := err.(*interfaces.NotConnectedError)
	c.Check(notConnected, NotNil)

	// no backend calls because of delayed-setup-profiles flag
	c.Assert(s.secBackend.SetupCalls, HasLen, 0)
}

func (s *interfaceManagerSuite) TestConnectErrorMissingSlotSnapOnAutoConnect(c *C) {
	s.MockModel(c, nil)

	_ = s.manager(c)
	s.mockSnap(c, producerYaml)
	s.mockSnap(c, consumerYaml)

	s.state.Lock()

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot", false)
	// remove producer snap from the state, doConnect should complain
	snapstate.Set(s.state, "producer", nil)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*snap "producer" is no longer available for auto-connecting.*`)

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), testutil.ErrorIs, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestConnectErrorMissingPlugSnapOnAutoConnect(c *C) {
	s.MockModel(c, nil)

	_ = s.manager(c)
	s.mockSnap(c, producerYaml)
	s.mockSnap(c, consumerYaml)

	s.state.Lock()
	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot", false)
	// remove consumer snap from the state, doConnect should complain
	snapstate.Set(s.state, "consumer", nil)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*snap "consumer" is no longer available for auto-connecting.*`)

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), testutil.ErrorIs, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestConnectErrorMissingPlugOnAutoConnect(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	_ = s.manager(c)
	producer := s.mockAppSet(c, producerYaml)
	// consumer snap has no plug, doConnect should complain
	s.mockSnap(c, consumerYaml)

	repo := s.manager(c).Repository()
	err := repo.AddAppSet(producer)
	c.Assert(err, IsNil)

	s.state.Lock()

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot", false)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*snap "consumer" has no "plug" plug.*`)

	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestConnectErrorMissingSlotOnAutoConnect(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	_ = s.manager(c)
	// producer snap has no slot, doConnect should complain
	s.mockSnap(c, producerYaml)
	consumer := s.mockAppSet(c, consumerYaml)

	repo := s.manager(c).Repository()

	err := repo.AddAppSet(consumer)
	c.Assert(err, IsNil)

	s.state.Lock()

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot", false)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*snap "producer" has no "slot" slot.*`)

	var conns map[string]interface{}
	err = s.state.Get("conns", &conns)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *interfaceManagerSuite) TestConnectHandlesAutoconnect(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	_ = s.manager(c)
	producer := s.mockAppSet(c, producerYaml)
	consumer := s.mockAppSet(c, consumerYaml)

	repo := s.manager(c).Repository()

	err := repo.AddAppSet(consumer)
	c.Assert(err, IsNil)

	err = repo.AddAppSet(producer)
	c.Assert(err, IsNil)

	s.state.Lock()

	chg := makeAutoConnectChange(s.state, "consumer", "plug", "producer", "slot", false)
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
			"interface": "test",
			"auto":      true,
			"plug-static": map[string]interface{}{
				"attr1": "value1",
			},
			"slot-static": map[string]interface{}{
				"attr2": "value2",
			},
		},
	})
}

func (s *interfaceManagerSuite) TestRegenerateAllSecurityProfilesWritesSystemKeyFile(c *C) {
	restore := interfaces.MockSystemKey(`{"core": "123"}`)
	defer restore()

	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})
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

func (s *interfaceManagerSuite) TestStartupTimings(c *C) {
	restore := interfaces.MockSystemKey(`{"core": "123"}`)
	defer restore()

	s.extraBackends = []interfaces.SecurityBackend{&ifacetest.TestSecurityBackend{BackendName: "fake"}}
	s.mockIface(&ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)

	oldDurationThreshold := timings.DurationThreshold
	defer func() {
		timings.DurationThreshold = oldDurationThreshold
	}()
	timings.DurationThreshold = 0

	_ = s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	var allTimings []map[string]interface{}
	c.Assert(s.state.Get("timings", &allTimings), IsNil)
	c.Check(allTimings, HasLen, 1)

	timings, ok := allTimings[0]["timings"]
	c.Assert(ok, Equals, true)

	// one backed expected; the other fake backend from test setup doesn't have a name and is ignored by regenerateAllSecurityProfiles
	c.Assert(timings, HasLen, 1)
	timingsList, ok := timings.([]interface{})
	c.Assert(ok, Equals, true)
	tm := timingsList[0].(map[string]interface{})
	c.Check(tm["label"], Equals, "setup-security-backend")
	c.Check(tm["summary"], Matches, `setup security backend "fake" for snap "consumer"`)

	tags, ok := allTimings[0]["tags"]
	c.Assert(ok, Equals, true)
	c.Check(tags, DeepEquals, map[string]interface{}{"startup": "ifacemgr"})
}

func (s *interfaceManagerSuite) TestStartupWarningForDisabledAppArmor(c *C) {
	invocationCount := 0
	restore := ifacestate.MockSnapdAppArmorServiceIsDisabled(func() bool {
		invocationCount++
		return true
	})
	defer restore()
	_ = s.manager(c)

	c.Check(invocationCount, Equals, 1)

	s.state.Lock()
	defer s.state.Unlock()
	warns := s.state.AllWarnings()
	c.Assert(warns, HasLen, 1)
	c.Check(warns[0].String(), Matches, `the snapd\.apparmor service is disabled.*\nRun .* to correct this\.`)
}

func (s *interfaceManagerSuite) TestAutoconnectSelf(c *C) {
	s.MockModel(c, nil)

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
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
	s.MockModel(c, nil)

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
	s.manager(c)

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
		s.se.Ensure()
		s.se.Wait()
	}

	// change did a retry
	s.state.Lock()
	c.Check(tConnectPlug.Status(), Equals, state.DoingStatus)

	// pretend install of content slot is done
	tInstSlot.SetStatus(state.DoneStatus)
	// wait for contentLinkRetryTimeout
	time.Sleep(10 * time.Millisecond)

	s.state.Unlock()

	// run again
	for i := 0; i < 5; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	// check that the connect plug task is now in done state
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tConnectPlug.Status(), Equals, state.DoneStatus)
}

func (s *interfaceManagerSuite) TestAutoconnectForDefaultContentProviderWrongOrderWaitChain(c *C) {
	s.MockModel(c, nil)

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
	s.manager(c)

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

	// Setup a wait chain in the "wrong" order, i.e. pretend we seed
	// the consumer of the content interface before we seed the producer
	// (see LP:#1772844) for a real world example of this).
	tInstPlug := s.state.NewTask("link-snap", "Install snap-content-plug")
	tInstPlug.Set("snap-setup", supContentPlug)
	chg.AddTask(tInstPlug)

	tConnectPlug := s.state.NewTask("auto-connect", "...plug")
	tConnectPlug.Set("snap-setup", supContentPlug)
	tConnectPlug.WaitFor(tInstPlug)
	chg.AddTask(tConnectPlug)

	tInstSlot := s.state.NewTask("link-snap", "Install snap-content-slot")
	tInstSlot.Set("snap-setup", supContentSlot)
	tInstSlot.WaitFor(tInstPlug)
	tInstSlot.WaitFor(tConnectPlug)
	chg.AddTask(tInstSlot)

	tConnectSlot := s.state.NewTask("auto-connect", "...slot")
	tConnectSlot.Set("snap-setup", supContentSlot)
	tConnectSlot.WaitFor(tInstPlug)
	tConnectSlot.WaitFor(tInstSlot)
	tConnectSlot.WaitFor(tConnectPlug)
	chg.AddTask(tConnectSlot)

	// pretend plug install was done by snapstate
	tInstPlug.SetStatus(state.DoneStatus)

	// run the change, this will trigger the auto-connect of the plug
	s.state.Unlock()
	for i := 0; i < 5; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	// check that auto-connect did finish and not hang
	s.state.Lock()
	c.Check(tConnectPlug.Status(), Equals, state.DoneStatus)
	c.Check(tInstSlot.Status(), Equals, state.DoStatus)
	c.Check(tConnectSlot.Status(), Equals, state.DoStatus)

	// pretend snapstate finished installing the slot
	tInstSlot.SetStatus(state.DoneStatus)

	s.state.Unlock()

	// run again
	for i := 0; i < 5; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	// and now the slot side auto-connected
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(tConnectSlot.Status(), Equals, state.DoneStatus)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si0}),
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

	appSets, err := ifacestate.SnapsWithSecurityProfiles(s.state)
	c.Assert(err, IsNil)
	c.Check(appSets, HasLen, 3)
	got := make(map[string]snap.Revision)
	for _, set := range appSets {
		got[set.InstanceName()] = set.Info().Revision
	}
	c.Check(got, DeepEquals, map[string]snap.Revision{
		"snap0": snap.R(10),
		"snap1": snap.R(1),
		"snap3": snap.R(3),
	})
}

func (s *interfaceManagerSuite) TestSnapsWithSecurityProfilesUsesPendingSecuritySideInfo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si0 := &snap.SideInfo{
		RealName: "snap0",
		Revision: snap.R(10),
	}
	snaptest.MockSnap(c, `name: snap0`, si0)
	snapstate.Set(s.state, "snap0", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si0}),
		Current:  si0.Revision,
	})
	si1 := &snap.SideInfo{
		RealName: "snap1",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: snap1`, si1)
	snapstate.Set(s.state, "snap1", &snapstate.SnapState{
		Active:   false,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
		PendingSecurity: &snapstate.PendingSecurityState{
			SideInfo: si1,
		},
	})
	si2 := &snap.SideInfo{
		RealName: "snap2",
		Revision: snap.R(2),
	}
	snaptest.MockSnap(c, `name: snap2`, si2)
	snapstate.Set(s.state, "snap2", &snapstate.SnapState{
		Active:   false,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		PendingSecurity: &snapstate.PendingSecurityState{
			SideInfo: nil,
		},
	})

	appSets, err := ifacestate.SnapsWithSecurityProfiles(s.state)
	c.Assert(err, IsNil)
	c.Check(appSets, HasLen, 2)
	got := make(map[string]snap.Revision)
	for _, set := range appSets {
		got[set.InstanceName()] = set.Info().Revision
	}
	c.Check(got, DeepEquals, map[string]snap.Revision{
		"snap0": snap.R(10),
		"snap1": snap.R(1),
	})
}

func (s *interfaceManagerSuite) TestSnapsWithSecurityProfilesMiddleOfFirstBoot(c *C) {
	// make sure snapsWithSecurityProfiles does the right thing
	// if invoked after a restart in the middle of first boot

	s.state.Lock()
	defer s.state.Unlock()

	si0 := &snap.SideInfo{
		RealName: "snap0",
		Revision: snap.R(10),
	}
	snaptest.MockSnap(c, `name: snap0`, si0)
	snapstate.Set(s.state, "snap0", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si0}),
		Current:  si0.Revision,
	})

	si1 := &snap.SideInfo{
		RealName: "snap1",
		Revision: snap.R(11),
	}
	snaptest.MockSnap(c, `name: snap1`, si1)
	snapstate.Set(s.state, "snap1", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})

	chg := s.state.NewChange("linking", "linking")

	snaps := []struct {
		name        string
		setupStatus state.Status
		linkStatus  state.Status
		si          *snap.SideInfo
	}{
		{"snap0", state.DoneStatus, state.DoneStatus, si0},
		{"snap1", state.DoStatus, state.DoStatus, si1},
	}

	var tsPrev *state.TaskSet
	for i, snp := range snaps {
		t1 := s.state.NewTask("setup-profiles", fmt.Sprintf("setup profiles %d", i))
		t1.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: snp.si,
		})
		t1.SetStatus(snp.setupStatus)
		t2 := s.state.NewTask("link-snap", fmt.Sprintf("link snap %d", i))
		t2.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: snp.si,
		})
		t2.WaitFor(t1)
		t2.SetStatus(snp.linkStatus)
		chg.AddTask(t1)
		chg.AddTask(t2)

		// this is the kind of wait chain used by first boot
		ts := state.NewTaskSet(t1, t2)
		if tsPrev != nil {
			ts.WaitAll(tsPrev)
		}
		tsPrev = ts
	}

	infos, err := ifacestate.SnapsWithSecurityProfiles(s.state)
	c.Assert(err, IsNil)
	// snap1 link-snap waiting on snap0 setup-profiles didn't confuse
	// snapsWithSecurityProfiles
	c.Check(infos, HasLen, 1)
	c.Check(infos[0].InstanceName(), Equals, "snap0")
}

func (s *interfaceManagerSuite) TestDisconnectInterfaces(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	_ = s.manager(c)

	consumerInfo := s.mockSnap(c, consumerYaml)
	producerInfo := s.mockSnap(c, producerYaml)

	consumerAppSet, err := interfaces.NewSnapAppSet(consumerInfo, nil)
	c.Assert(err, IsNil)

	producerAppSet, err := interfaces.NewSnapAppSet(producerInfo, nil)
	c.Assert(err, IsNil)

	s.state.Lock()

	sup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer"},
	}

	repo := s.manager(c).Repository()
	c.Assert(repo.AddAppSet(consumerAppSet), IsNil)
	c.Assert(repo.AddAppSet(producerAppSet), IsNil)

	plugDynAttrs := map[string]interface{}{
		"attr3": "value3",
	}
	slotDynAttrs := map[string]interface{}{
		"attr4": "value4",
	}
	repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}, nil, plugDynAttrs, nil, slotDynAttrs, nil)

	chg := s.state.NewChange("install", "")
	t := s.state.NewTask("auto-disconnect", "")
	t.Set("snap-setup", sup)
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	ht := t.HaltTasks()
	c.Assert(ht, HasLen, 3)

	c.Assert(ht[2].Kind(), Equals, "disconnect")
	var autoDisconnect bool
	c.Assert(ht[2].Get("auto-disconnect", &autoDisconnect), IsNil)
	c.Assert(autoDisconnect, Equals, true)
	var plugDynamic, slotDynamic, plugStatic, slotStatic map[string]interface{}
	c.Assert(ht[2].Get("plug-static", &plugStatic), IsNil)
	c.Assert(ht[2].Get("plug-dynamic", &plugDynamic), IsNil)
	c.Assert(ht[2].Get("slot-static", &slotStatic), IsNil)
	c.Assert(ht[2].Get("slot-dynamic", &slotDynamic), IsNil)

	c.Assert(plugStatic, DeepEquals, map[string]interface{}{"attr1": "value1"})
	c.Assert(slotStatic, DeepEquals, map[string]interface{}{"attr2": "value2"})
	c.Assert(plugDynamic, DeepEquals, map[string]interface{}{"attr3": "value3"})
	c.Assert(slotDynamic, DeepEquals, map[string]interface{}{"attr4": "value4"})

	var expectedHooks = []struct{ snap, hook string }{
		{snap: "producer", hook: "disconnect-slot-slot"},
		{snap: "consumer", hook: "disconnect-plug-plug"},
	}

	for i := 0; i < 2; i++ {
		var hsup hookstate.HookSetup
		c.Assert(ht[i].Kind(), Equals, "run-hook")
		c.Assert(ht[i].Get("hook-setup", &hsup), IsNil)

		c.Assert(hsup.Snap, Equals, expectedHooks[i].snap)
		c.Assert(hsup.Hook, Equals, expectedHooks[i].hook)
	}
}

func (s *interfaceManagerSuite) testDisconnectInterfacesRetry(c *C, conflictingKind string) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	_ = s.manager(c)

	consumerInfo := s.mockSnap(c, consumerYaml)
	producerInfo := s.mockSnap(c, producerYaml)

	consumerAppSet, err := interfaces.NewSnapAppSet(consumerInfo, nil)
	c.Assert(err, IsNil)

	producerAppSet, err := interfaces.NewSnapAppSet(producerInfo, nil)
	c.Assert(err, IsNil)

	supprod := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "producer"},
	}

	s.state.Lock()

	repo := s.manager(c).Repository()
	c.Assert(repo.AddAppSet(consumerAppSet), IsNil)
	c.Assert(repo.AddAppSet(producerAppSet), IsNil)

	repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}, nil, nil, nil, nil, nil)

	sup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer"},
	}

	chg2 := s.state.NewChange("remove", "")
	t2 := s.state.NewTask("auto-disconnect", "")
	t2.Set("snap-setup", sup)
	chg2.AddTask(t2)

	// create conflicting task
	chg1 := s.state.NewChange("conflicting change", "")
	t1 := s.state.NewTask(conflictingKind, "")
	t1.Set("snap-setup", supprod)
	chg1.AddTask(t1)
	t3 := s.state.NewTask("other", "")
	t1.WaitFor(t3)
	chg1.AddTask(t3)
	t3.SetStatus(state.HoldStatus)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(strings.Join(t2.Log(), ""), Matches, `.*Waiting for conflicting change in progress...`)
	c.Assert(t2.Status(), Equals, state.DoingStatus)
}

func (s *interfaceManagerSuite) TestDisconnectInterfacesRetryLink(c *C) {
	s.testDisconnectInterfacesRetry(c, "link-snap")
}

func (s *interfaceManagerSuite) TestDisconnectInterfacesRetrySetupProfiles(c *C) {
	s.testDisconnectInterfacesRetry(c, "setup-profiles")
}

func (s *interfaceManagerSuite) setupAutoConnectGadget(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})

	r := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-auto-connection: true
`))
	s.AddCleanup(r)

	s.MockSnapDecl(c, "consumer", "publisher1", nil)
	s.mockSnap(c, consumerYaml)
	s.MockSnapDecl(c, "producer", "publisher2", nil)
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

	err := os.WriteFile(filepath.Join(gadgetInfo.MountDir(), "meta", "gadget.yaml"), gadgetYaml, 0644)
	c.Assert(err, IsNil)

	s.MockModel(c, nil)

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", nil)
}

func checkAutoConnectGadgetTasks(c *C, tasks []*state.Task) {
	gotConnect := false
	for _, t := range tasks {
		switch t.Kind() {
		default:
			c.Fatalf("unexpected task kind: %s", t.Kind())
		case "auto-connect":
		case "run-hook":
		case "setup-profiles":
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

func (s *interfaceManagerSuite) TestAutoConnectGadget(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.setupAutoConnectGadget(c)
	s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("auto-connect", "gadget connections")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer"},
	})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 7)
	checkAutoConnectGadgetTasks(c, tasks)
}

func (s *interfaceManagerSuite) TestAutoConnectGadgetProducer(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.setupAutoConnectGadget(c)
	s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("auto-connect", "gadget connections")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "producer"},
	})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 7)
	checkAutoConnectGadgetTasks(c, tasks)
}

func (s *interfaceManagerSuite) TestAutoConnectGadgetRemodeling(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.setupAutoConnectGadget(c)
	s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	// seeded but remodeling
	s.state.Set("seeded", true)
	remodCtx := s.TrivialDeviceContext(c, nil)
	remodCtx.Remodeling = true
	r2 := snapstatetest.MockDeviceContext(remodCtx)
	defer r2()

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("auto-connect", "gadget connections")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer"},
	})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 7)
	checkAutoConnectGadgetTasks(c, tasks)
}

func (s *interfaceManagerSuite) TestAutoConnectGadgetSeededNoop(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.setupAutoConnectGadget(c)
	s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	// seeded and not remodeling
	s.state.Set("seeded", true)

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("auto-connect", "gadget connections")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer"},
	})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	tasks := chg.Tasks()
	// nothing happens, no tasks added
	c.Assert(tasks, HasLen, 1)
}

func (s *interfaceManagerSuite) TestAutoConnectGadgetAlreadyConnected(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.setupAutoConnectGadget(c)
	s.manager(c)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("conns", map[string]interface{}{
		"consumer:plug producer:slot": map[string]interface{}{
			"interface": "test", "auto": true,
		},
	})

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("auto-connect", "gadget connections")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "producer"},
	})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Check(chg.Status().Ready(), Equals, true)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)
}

func (s *interfaceManagerSuite) TestAutoConnectGadgetConflictRetry(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()

	s.setupAutoConnectGadget(c)
	s.manager(c)

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
	t = s.state.NewTask("auto-connect", "gadget connections")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "consumer"},
	})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Check(chg.Status().Ready(), Equals, false)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)

	c.Check(t.Status(), Equals, state.DoingStatus)
	c.Check(t.Log()[0], Matches, `.*Waiting for conflicting change in progress: conflicting snap producer.*`)
}

func (s *interfaceManagerSuite) TestAutoConnectGadgetSkipUnknown(c *C) {
	r := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
slots:
  test:
    deny-auto-connection: true
`))
	defer r()

	r1 := release.MockOnClassic(false)
	defer r1()

	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	s.MockSnapDecl(c, "consumer", "publisher1", nil)
	s.mockSnap(c, consumerYaml)
	s.MockSnapDecl(c, "producer", "publisher2", nil)
	s.mockSnap(c, producerYaml)

	s.MockModel(c, nil)

	s.manager(c)

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

	err := os.WriteFile(filepath.Join(gadgetInfo.MountDir(), "meta", "gadget.yaml"), gadgetYaml, 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("auto-connect", "gadget connections")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "producer"},
	})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)

	logs := t.Log()
	c.Check(logs, HasLen, 2)
	c.Check(logs[0], Matches, `.*ignoring missing slot produceridididididididididididid:unknown`)
	c.Check(logs[1], Matches, `.* ignoring missing plug unknownididididididididididididi:plug`)
}

func (s *interfaceManagerSuite) TestAutoConnectGadgetHappyPolicyChecks(c *C) {
	// network-control does not auto-connect so this test also
	// checks that the right policy checker (for "*-connection"
	// rules) is used for gadget connections
	r1 := release.MockOnClassic(false)
	defer r1()

	s.MockModel(c, nil)

	s.mockSnap(c, coreSnapYaml)

	s.MockSnapDecl(c, "foo", "publisher1", nil)
	s.mockSnap(c, `name: foo
version: 1.0
plugs:
  network-control:
`)

	s.manager(c)

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

	err := os.WriteFile(filepath.Join(gadgetInfo.MountDir(), "meta", "gadget.yaml"), gadgetYaml, 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("setting-up", "...")
	t := s.state.NewTask("auto-connect", "gadget connections")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo", Revision: snap.R(1)},
	})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 3)
	c.Assert(tasks[0].Kind(), Equals, "auto-connect")
	c.Assert(tasks[1].Kind(), Equals, "connect")
	c.Assert(tasks[2].Kind(), Equals, "setup-profiles")

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

func (s *interfaceManagerSuite) testChangeConflict(c *C, kind string) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "producer", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "producer", SnapID: "producer-id", Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "consumer", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "consumer", SnapID: "consumer-id", Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := s.state.NewChange("another change", "...")
	t := s.state.NewTask(kind, "...")
	t.Set("slot", interfaces.SlotRef{Snap: "producer", Name: "slot"})
	t.Set("plug", interfaces.PlugRef{Snap: "consumer", Name: "plug"})
	chg.AddTask(t)

	_, err := snapstate.Disable(s.state, "producer")
	c.Assert(err, ErrorMatches, `snap "producer" has "another change" change in progress`)

	_, err = snapstate.Disable(s.state, "consumer")
	c.Assert(err, ErrorMatches, `snap "consumer" has "another change" change in progress`)
}

func (s *interfaceManagerSuite) TestSnapstateOpConflictWithConnect(c *C) {
	s.testChangeConflict(c, "connect")
}

func (s *interfaceManagerSuite) TestSnapstateOpConflictWithDisconnect(c *C) {
	s.testChangeConflict(c, "disconnect")
}

type udevMonitorMock struct {
	ConnectError, RunError            error
	ConnectCalls, RunCalls, StopCalls int
	AddDevice                         udevmonitor.DeviceAddedFunc
	RemoveDevice                      udevmonitor.DeviceRemovedFunc
	EnumerationDone                   udevmonitor.EnumerationDoneFunc
}

func (u *udevMonitorMock) Connect() error {
	u.ConnectCalls++
	return u.ConnectError
}

func (u *udevMonitorMock) Run() error {
	u.RunCalls++
	return u.RunError
}

func (u *udevMonitorMock) Stop() error {
	u.StopCalls++
	return nil
}

func (s *interfaceManagerSuite) TestUDevMonitorInit(c *C) {
	u := udevMonitorMock{}
	st := s.state
	st.Lock()
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	st.Unlock()
	s.mockSnap(c, coreSnapYaml)

	restoreTimeout := ifacestate.MockUDevInitRetryTimeout(0 * time.Second)
	defer restoreTimeout()

	restoreCreate := ifacestate.MockCreateUDevMonitor(func(udevmonitor.DeviceAddedFunc, udevmonitor.DeviceRemovedFunc, udevmonitor.EnumerationDoneFunc) udevmonitor.Interface {
		return &u
	})
	defer restoreCreate()

	mgr, err := ifacestate.Manager(s.state, nil, s.o.TaskRunner(), nil, nil)
	c.Assert(err, IsNil)
	s.o.AddManager(mgr)
	c.Assert(s.o.StartUp(), IsNil)

	// succesfull initialization should result in exactly 1 connect and run call
	for i := 0; i < 5; i++ {
		c.Assert(s.se.Ensure(), IsNil)
	}
	s.se.Stop()

	c.Assert(u.ConnectCalls, Equals, 1)
	c.Assert(u.RunCalls, Equals, 1)
	c.Assert(u.StopCalls, Equals, 1)
}

func (s *interfaceManagerSuite) TestUDevMonitorInitErrors(c *C) {
	u := udevMonitorMock{
		ConnectError: fmt.Errorf("Connect failed"),
	}
	st := s.state
	st.Lock()
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	st.Unlock()
	s.mockSnap(c, coreSnapYaml)

	restoreTimeout := ifacestate.MockUDevInitRetryTimeout(0 * time.Second)
	defer restoreTimeout()

	restoreCreate := ifacestate.MockCreateUDevMonitor(func(udevmonitor.DeviceAddedFunc, udevmonitor.DeviceRemovedFunc, udevmonitor.EnumerationDoneFunc) udevmonitor.Interface {
		return &u
	})
	defer restoreCreate()

	mgr, err := ifacestate.Manager(s.state, nil, s.o.TaskRunner(), nil, nil)
	c.Assert(err, IsNil)
	s.o.AddManager(mgr)
	c.Assert(s.o.StartUp(), IsNil)

	c.Assert(s.se.Ensure(), ErrorMatches, ".*Connect failed.*")
	c.Assert(u.ConnectCalls, Equals, 1)
	c.Assert(u.RunCalls, Equals, 0)
	c.Assert(u.StopCalls, Equals, 0)

	u.ConnectError = nil
	u.RunError = fmt.Errorf("Run failed")
	c.Assert(s.se.Ensure(), ErrorMatches, ".*Run failed.*")
	c.Assert(u.ConnectCalls, Equals, 2)
	c.Assert(u.RunCalls, Equals, 1)
	c.Assert(u.StopCalls, Equals, 0)

	u.RunError = nil
	c.Assert(s.se.Ensure(), IsNil)

	s.se.Stop()

	c.Assert(u.StopCalls, Equals, 1)
}

func (s *interfaceManagerSuite) TestUDevMonitorInitWaitsForCore(c *C) {
	restoreTimeout := ifacestate.MockUDevInitRetryTimeout(0 * time.Second)
	defer restoreTimeout()

	var udevMonitorCreated bool
	restoreCreate := ifacestate.MockCreateUDevMonitor(func(udevmonitor.DeviceAddedFunc, udevmonitor.DeviceRemovedFunc, udevmonitor.EnumerationDoneFunc) udevmonitor.Interface {
		udevMonitorCreated = true
		return &udevMonitorMock{}
	})
	defer restoreCreate()

	mgr, err := ifacestate.Manager(s.state, nil, s.o.TaskRunner(), nil, nil)
	c.Assert(err, IsNil)
	s.o.AddManager(mgr)
	c.Assert(s.o.StartUp(), IsNil)

	for i := 0; i < 5; i++ {
		c.Assert(s.se.Ensure(), IsNil)
		c.Assert(udevMonitorCreated, Equals, false)
	}

	// core snap appears in the system
	st := s.state
	st.Lock()
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	st.Unlock()

	// and udev monitor is now created
	c.Assert(s.se.Ensure(), IsNil)
	c.Assert(udevMonitorCreated, Equals, true)
}

func (s *interfaceManagerSuite) TestAttributesRestoredFromConns(c *C) {
	slotAppSet := s.mockAppSet(c, producer2Yaml)
	plugAppSet := s.mockAppSet(c, consumerYaml)

	slot := slotAppSet.Info().Slots["slot"]
	c.Assert(slot, NotNil)
	plug := plugAppSet.Info().Plugs["plug"]
	c.Assert(plug, NotNil)

	st := s.st
	st.Lock()
	defer st.Unlock()

	conns, err := ifacestate.GetConns(st)
	c.Assert(err, IsNil)

	// create connection in conns state
	dynamicAttrs := map[string]interface{}{"dynamic-number": 7}
	conn := &interfaces.Connection{
		Plug: interfaces.NewConnectedPlug(plug, slotAppSet, nil, nil),
		Slot: interfaces.NewConnectedSlot(slot, plugAppSet, nil, dynamicAttrs),
	}

	var number, dynnumber int64
	c.Check(conn.Slot.Attr("number", &number), IsNil)
	c.Check(number, Equals, int64(1))

	var isAuto, byGadget, isUndesired, hotplugGone bool
	ifacestate.UpdateConnectionInConnState(conns, conn, isAuto, byGadget, isUndesired, hotplugGone)
	ifacestate.SetConns(st, conns)

	// restore connection from conns state
	newConns, err := ifacestate.GetConns(st)
	c.Assert(err, IsNil)

	_, _, slotStaticAttrs, slotDynamicAttrs, ok := ifacestate.GetConnStateAttrs(newConns, "consumer:plug producer2:slot")
	c.Assert(ok, Equals, true)

	restoredSlot := interfaces.NewConnectedSlot(slot, slotAppSet, slotStaticAttrs, slotDynamicAttrs)
	c.Check(restoredSlot.Attr("number", &number), IsNil)
	c.Check(number, Equals, int64(1))
	c.Check(restoredSlot.Attr("dynamic-number", &dynnumber), IsNil)
}

func (s *interfaceManagerSuite) setupHotplugConnectTestData(c *C) *state.Change {
	s.state.Unlock()

	// mock hotplug slot in the repo and state
	coreAppSet := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()

	c.Assert(repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "test"}), IsNil)

	repo.AddSlot(&snap.SlotInfo{
		Snap:       coreAppSet.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
	})

	// mock the consumer
	testSnap := s.mockAppSet(c, consumerYaml)
	c.Assert(testSnap.Info().Plugs["plug"], NotNil)
	c.Assert(repo.AddAppSet(testSnap), IsNil)

	s.state.Lock()
	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1234",
		},
	})

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-connect", "")
	ifacestate.SetHotplugAttrs(t, "test", "1234")
	chg.AddTask(t)

	return chg
}

func (s *interfaceManagerSuite) TestHotplugConnect(c *C) {
	s.MockModel(c, nil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.setupHotplugConnectTestData(c)

	// simulate a device that was known and connected before
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":    "test",
			"hotplug-key":  "1234",
			"hotplug-gone": true,
		}})

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
			"plug-static": map[string]interface{}{"attr1": "value1"},
		}})
}

func (s *interfaceManagerSuite) TestHotplugConnectIgnoresUndesired(c *C) {
	s.MockModel(c, nil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.setupHotplugConnectTestData(c)

	// simulate a device that was known and connected before
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
			"undesired":   true,
		}})

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// no connect task created
	c.Check(chg.Tasks(), HasLen, 1)
	c.Assert(chg.Err(), IsNil)

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
			"undesired":   true,
		}})
}

func (s *interfaceManagerSuite) TestHotplugConnectSlotMissing(c *C) {
	s.MockModel(c, nil)

	coreAppSet := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	c.Assert(repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "test"}), IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreAppSet.Info(),
		Name:       "slot",
		Interface:  "test",
		HotplugKey: "1",
	}), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-connect", "")
	ifacestate.SetHotplugAttrs(t, "test", "2")
	chg.AddTask(t)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot find hotplug slot for interface test and hotplug key "2".*`)
}

func (s *interfaceManagerSuite) TestHotplugConnectNothingTodo(c *C) {
	s.MockModel(c, nil)

	coreAppSet := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()

	iface := &ifacetest.TestInterface{InterfaceName: "test", AutoConnectCallback: func(*snap.PlugInfo, *snap.SlotInfo) bool { return false }}
	c.Assert(repo.AddInterface(iface), IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreAppSet.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1",
	}), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1",
		}})

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-connect", "")
	ifacestate.SetHotplugAttrs(t, "test", "1")
	chg.AddTask(t)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// no connect tasks created
	c.Check(chg.Tasks(), HasLen, 1)
	c.Assert(chg.Err(), IsNil)
}

func (s *interfaceManagerSuite) TestHotplugConnectConflictRetry(c *C) {
	s.MockModel(c, nil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.setupHotplugConnectTestData(c)

	// simulate a device that was known and connected before
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":    "test",
			"hotplug-key":  "1234",
			"hotplug-gone": true,
		}})

	otherChg := s.state.NewChange("other-chg", "...")
	t := s.state.NewTask("link-snap", "...")
	t.Set("snap-setup", &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "core"}})
	otherChg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Check(chg.Status().Ready(), Equals, false)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)

	hotplugConnectTask := tasks[0]
	c.Check(hotplugConnectTask.Status(), Equals, state.DoingStatus)
	c.Check(hotplugConnectTask.Log()[0], Matches, `.*hotplug connect will be retried: conflicting snap core with task "link-snap"`)
}

func (s *interfaceManagerSuite) TestHotplugAutoconnect(c *C) {
	s.MockModel(c, nil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.setupHotplugConnectTestData(c)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
			"auto":        true,
			"plug-static": map[string]interface{}{"attr1": "value1"},
		}})
}

func (s *interfaceManagerSuite) TestHotplugAutoconnectConflictRetry(c *C) {
	s.MockModel(c, nil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.setupHotplugConnectTestData(c)

	otherChg := s.state.NewChange("other-chg", "...")
	t := s.state.NewTask("link-snap", "...")
	t.Set("snap-setup", &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "core"}})
	otherChg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Check(chg.Status().Ready(), Equals, false)
	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 1)

	hotplugConnectTask := tasks[0]
	c.Check(hotplugConnectTask.Status(), Equals, state.DoingStatus)
	c.Check(hotplugConnectTask.Log()[0], Matches, `.*hotplug connect will be retried: conflicting snap core with task "link-snap"`)
}

// mockConsumer mocks a consumer snap and its single plug in the repository
func mockConsumer(c *C, st *state.State, repo *interfaces.Repository, snapYaml, consumerSnapName, plugName string) {
	si := &snap.SideInfo{RealName: consumerSnapName, Revision: snap.R(1)}
	consumer := ifacetest.MockSnapAndAppSet(c, snapYaml, nil, si)
	c.Assert(consumer.Info().Plugs[plugName], NotNil)
	c.Assert(repo.AddAppSet(consumer), IsNil)
	snapstate.Set(st, consumerSnapName, &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
}

func (s *interfaceManagerSuite) TestHotplugConnectAndAutoconnect(c *C) {
	s.MockModel(c, nil)

	coreInfo := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	c.Assert(repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "test"}), IsNil)

	// mock hotplug slot in the repo and state
	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreInfo.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
	}), IsNil)

	s.state.Lock()
	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{"name": "hotplugslot", "interface": "test", "hotplug-key": "1234"},
	})

	mockConsumer(c, s.state, repo, consumerYaml, "consumer", "plug")
	mockConsumer(c, s.state, repo, consumer2Yaml, "consumer2", "plug")

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-connect", "")
	ifacestate.SetHotplugAttrs(t, "test", "1234")
	chg.AddTask(t)

	// simulate a device that was known and connected before to only one consumer, this connection will be restored
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":    "test",
			"hotplug-key":  "1234",
			"hotplug-gone": true,
		}})

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	// two connections now present (restored one for consumer, and new one for consumer2)
	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
			"plug-static": map[string]interface{}{"attr1": "value1"},
		},
		"consumer2:plug core:hotplugslot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
			"auto":        true,
			"plug-static": map[string]interface{}{"attr1": "value1"},
		}})
}

func (s *interfaceManagerSuite) TestHotplugDisconnect(c *C) {
	coreInfo := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test",
	})
	c.Assert(err, IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreInfo.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
	}), IsNil)

	// mock the consumer
	testSnap := s.mockAppSet(c, consumerYaml)
	c.Assert(testSnap.Info().Plugs["plug"], NotNil)
	c.Assert(repo.AddAppSet(testSnap), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1234",
		}})
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
		}})
	_, err = repo.Connect(&interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "hotplugslot"}},
		nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-disconnect", "")
	t.Set("hotplug-key", "1234")
	t.Set("interface", "test")
	chg.AddTask(t)

	s.state.Unlock()
	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()
	c.Assert(chg.Err(), IsNil)

	var byHotplug bool
	for _, t := range s.state.Tasks() {
		// the 'disconnect' task created by hotplug-disconnect should have by-hotplug flag set
		if t.Kind() == "disconnect" {
			c.Assert(t.Get("by-hotplug", &byHotplug), IsNil)
		}
	}
	c.Assert(byHotplug, Equals, true)

	// hotplug-gone flag on the connection is set
	var conns map[string]interface{}
	c.Assert(s.state.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":    "test",
			"hotplug-key":  "1234",
			"hotplug-gone": true,
		}})
}

func (s *interfaceManagerSuite) testHotplugDisconnectWaitsForCoreRefresh(c *C, taskKind string) {
	coreInfo := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test",
	})
	c.Assert(err, IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreInfo.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
	}), IsNil)

	// mock the consumer
	testSnap := s.mockAppSet(c, consumerYaml)
	c.Assert(testSnap.Info().Plugs["plug"], NotNil)
	c.Assert(repo.AddAppSet(testSnap), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1234",
		}})
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
		}})
	_, err = repo.Connect(&interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "hotplugslot"}},
		nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-disconnect", "")
	ifacestate.SetHotplugAttrs(t, "test", "1234")
	chg.AddTask(t)

	chg2 := s.state.NewChange("other-chg", "...")
	t2 := s.state.NewTask(taskKind, "...")
	t2.Set("snap-setup", &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "core"}})
	chg2.AddTask(t2)
	t3 := s.state.NewTask("other", "")
	t2.WaitFor(t3)
	t3.SetStatus(state.HoldStatus)
	chg2.AddTask(t3)

	s.state.Unlock()
	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()
	c.Assert(chg.Err(), IsNil)

	c.Assert(strings.Join(t.Log(), ""), Matches, `.*Waiting for conflicting change in progress:.*`)
	c.Assert(chg.Status(), Equals, state.DoingStatus)

	t2.SetStatus(state.DoneStatus)
	t3.SetStatus(state.DoneStatus)

	s.state.Unlock()
	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)
}

func (s *interfaceManagerSuite) TestHotplugDisconnectWaitsForCoreSetupProfiles(c *C) {
	s.testHotplugDisconnectWaitsForCoreRefresh(c, "setup-profiles")
}

func (s *interfaceManagerSuite) TestHotplugDisconnectWaitsForCoreLnkSnap(c *C) {
	s.testHotplugDisconnectWaitsForCoreRefresh(c, "link-snap")
}

func (s *interfaceManagerSuite) TestHotplugDisconnectWaitsForCoreUnlinkSnap(c *C) {
	s.testHotplugDisconnectWaitsForCoreRefresh(c, "unlink-snap")
}

func (s *interfaceManagerSuite) TestHotplugDisconnectWaitsForDisconnectPlug(c *C) {
	coreInfo := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test",
	})
	c.Assert(err, IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreInfo.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
	}), IsNil)

	testSnap := s.mockAppSet(c, consumerYaml)
	c.Assert(testSnap.Info().Plugs["plug"], NotNil)
	c.Assert(repo.AddAppSet(testSnap), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1234",
		}})
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":   "test",
			"hotplug-key": "1234",
		}})
	conn, err := repo.Connect(&interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "hotplugslot"}},
		nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	hotplugChg := s.state.NewChange("hotplug change", "")
	hotplugDisconnect := s.state.NewTask("hotplug-disconnect", "")
	ifacestate.SetHotplugAttrs(hotplugDisconnect, "test", "1234")
	hotplugChg.AddTask(hotplugDisconnect)

	disconnectChg := s.state.NewChange("disconnect change", "...")
	disconnectTs, err := ifacestate.Disconnect(s.state, conn)
	c.Assert(err, IsNil)
	disconnectChg.AddAll(disconnectTs)

	holdingTask := s.state.NewTask("other", "")
	disconnectTs.WaitFor(holdingTask)
	holdingTask.SetStatus(state.HoldStatus)
	disconnectChg.AddTask(holdingTask)

	s.state.Unlock()
	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()
	c.Assert(hotplugChg.Err(), IsNil)

	c.Assert(strings.Join(hotplugDisconnect.Log(), ""), Matches, `.*Waiting for conflicting change in progress: conflicting plug snap consumer.*`)
	c.Assert(hotplugChg.Status(), Equals, state.DoingStatus)

	for _, t := range disconnectTs.Tasks() {
		t.SetStatus(state.DoneStatus)
	}
	holdingTask.SetStatus(state.DoneStatus)

	s.state.Unlock()
	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()

	c.Assert(hotplugChg.Err(), IsNil)
	c.Assert(hotplugChg.Status(), Equals, state.DoneStatus)
}

func (s *interfaceManagerSuite) testHotplugAddNewSlot(c *C, devData map[string]string, specName, expectedName string) {
	_ = s.mockSnap(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-add-slot", "")
	t.Set("hotplug-key", "1234")
	t.Set("interface", "test")
	proposedSlot := hotplug.ProposedSlot{Name: specName, Attrs: map[string]interface{}{"foo": "bar"}}
	t.Set("proposed-slot", proposedSlot)
	devinfo, _ := hotplug.NewHotplugDeviceInfo(devData)
	t.Set("device-info", devinfo)
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	// hotplugslot is created in the repository
	slot := repo.Slot("core", expectedName)
	c.Assert(slot, NotNil)
	c.Check(slot.Attrs, DeepEquals, map[string]interface{}{"foo": "bar"})
	c.Check(slot.HotplugKey, Equals, snap.HotplugKey("1234"))

	var hotplugSlots map[string]interface{}
	c.Assert(s.state.Get("hotplug-slots", &hotplugSlots), IsNil)
	c.Assert(hotplugSlots, HasLen, 1)
	c.Check(hotplugSlots[expectedName], DeepEquals, map[string]interface{}{
		"name":         expectedName,
		"interface":    "test",
		"hotplug-key":  "1234",
		"static-attrs": map[string]interface{}{"foo": "bar"},
		"hotplug-gone": false,
	})
}

func (s *interfaceManagerSuite) TestHotplugAddNewSlotWithNameFromSpec(c *C) {
	s.testHotplugAddNewSlot(c, map[string]string{"DEVPATH": "/a", "NAME": "hdcamera"}, "hotplugslot", "hotplugslot")
}

func (s *interfaceManagerSuite) TestHotplugAddNewSlotWithNameFromDevice(c *C) {
	s.testHotplugAddNewSlot(c, map[string]string{"DEVPATH": "/a", "NAME": "hdcamera"}, "", "hdcamera")
}

func (s *interfaceManagerSuite) TestHotplugAddNewSlotWithNameFromInterface(c *C) {
	s.testHotplugAddNewSlot(c, map[string]string{"DEVPATH": "/a"}, "", "test")
}

func (s *interfaceManagerSuite) TestHotplugAddGoneSlot(c *C) {
	_ = s.mockSnap(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot-old-name": map[string]interface{}{
			"name":         "hotplugslot-old-name",
			"interface":    "test",
			"static-attrs": map[string]interface{}{"foo": "old"},
			"hotplug-key":  "1234",
			"hotplug-gone": true,
		}})

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-add-slot", "")
	t.Set("hotplug-key", "1234")
	t.Set("interface", "test")
	proposedSlot := hotplug.ProposedSlot{Name: "hotplugslot", Label: "", Attrs: map[string]interface{}{"foo": "bar"}}
	t.Set("proposed-slot", proposedSlot)
	t.Set("device-info", map[string]string{"DEVPATH": "/a", "NAME": "hdcamera"})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	// hotplugslot is re-created in the repository, reuses old name and has new attributes
	slot := repo.Slot("core", "hotplugslot-old-name")
	c.Assert(slot, NotNil)
	c.Check(slot.Attrs, DeepEquals, map[string]interface{}{"foo": "bar"})
	c.Check(slot.HotplugKey, DeepEquals, snap.HotplugKey("1234"))

	var hotplugSlots map[string]interface{}
	c.Assert(s.state.Get("hotplug-slots", &hotplugSlots), IsNil)
	c.Check(hotplugSlots, DeepEquals, map[string]interface{}{
		"hotplugslot-old-name": map[string]interface{}{
			"name":         "hotplugslot-old-name",
			"interface":    "test",
			"hotplug-key":  "1234",
			"static-attrs": map[string]interface{}{"foo": "bar"},
			"hotplug-gone": false,
		}})
}

func (s *interfaceManagerSuite) TestHotplugAddSlotWithChangedAttrs(c *C) {
	coreInfo := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "test"})
	c.Assert(err, IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreInfo.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
		Attrs:      map[string]interface{}{"foo": "oldfoo"},
	}), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":         "hotplugslot",
			"interface":    "test",
			"static-attrs": map[string]interface{}{"foo": "old"},
			"hotplug-key":  "1234",
		},
	})

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-add-slot", "")
	t.Set("hotplug-key", "1234")
	t.Set("interface", "test")
	proposedSlot := hotplug.ProposedSlot{Name: "hotplugslot", Label: "", Attrs: map[string]interface{}{"foo": "newfoo"}}
	t.Set("proposed-slot", proposedSlot)
	devinfo, _ := hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "/a"})
	t.Set("device-info", devinfo)
	chg.AddTask(t)

	s.state.Unlock()
	for i := 0; i < 5; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// hotplugslot is re-created in the repository
	slot := repo.Slot("core", "hotplugslot")
	c.Assert(slot, NotNil)
	c.Check(slot.Attrs, DeepEquals, map[string]interface{}{"foo": "newfoo"})
	c.Check(slot.HotplugKey, DeepEquals, snap.HotplugKey("1234"))

	var hotplugSlots map[string]interface{}
	c.Assert(s.state.Get("hotplug-slots", &hotplugSlots), IsNil)
	c.Check(hotplugSlots, DeepEquals, map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":         "hotplugslot",
			"interface":    "test",
			"hotplug-key":  "1234",
			"static-attrs": map[string]interface{}{"foo": "newfoo"},
			"hotplug-gone": false,
		}})
}

func (s *interfaceManagerSuite) TestHotplugUpdateSlot(c *C) {
	coreInfo := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test",
	})
	c.Assert(err, IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreInfo.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
	}), IsNil)

	// validity check
	c.Assert(repo.Slot("core", "hotplugslot"), NotNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1234",
		}})

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-update-slot", "")
	t.Set("hotplug-key", "1234")
	t.Set("interface", "test")
	t.Set("slot-attrs", map[string]interface{}{"foo": "bar"})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	// hotplugslot is updated in the repository
	slot := repo.Slot("core", "hotplugslot")
	c.Assert(slot, NotNil)
	c.Assert(slot.Attrs, DeepEquals, map[string]interface{}{"foo": "bar"})

	var hotplugSlots map[string]interface{}
	c.Assert(s.state.Get("hotplug-slots", &hotplugSlots), IsNil)
	c.Assert(hotplugSlots, DeepEquals, map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":         "hotplugslot",
			"interface":    "test",
			"hotplug-key":  "1234",
			"static-attrs": map[string]interface{}{"foo": "bar"},
			"hotplug-gone": false,
		}})
}

func (s *interfaceManagerSuite) TestHotplugUpdateSlotWhenConnected(c *C) {
	coreInfo := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test",
	})
	c.Assert(err, IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreInfo.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
	}), IsNil)

	consumer := s.mockAppSet(c, consumerYaml)
	err = repo.AddAppSet(consumer)
	c.Assert(err, IsNil)

	// validity check
	c.Assert(repo.Slot("core", "hotplugslot"), NotNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1234",
		}})
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":    "test",
			"hotplug-key":  "1234",
			"hotplug-gone": true,
		}})
	_, err = repo.Connect(&interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "hotplugslot"}},
		nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-update-slot", "")
	t.Set("hotplug-key", "1234")
	t.Set("interface", "test")
	t.Set("slot-attrs", map[string]interface{}{})
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*internal error: cannot update slot hotplugslot while connected.*`)

	// hotplugslot is not removed because of existing connection
	c.Assert(repo.Slot("core", "hotplugslot"), NotNil)

	var hotplugSlots map[string]interface{}
	c.Assert(s.state.Get("hotplug-slots", &hotplugSlots), IsNil)
	c.Assert(hotplugSlots, DeepEquals, map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1234",
		}})
}

func (s *interfaceManagerSuite) TestHotplugRemoveSlot(c *C) {
	coreInfo := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test",
	})
	c.Assert(err, IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreInfo.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
	}), IsNil)

	// validity check
	c.Assert(repo.Slot("core", "hotplugslot"), NotNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1234",
		},
		"otherslot": map[string]interface{}{
			"name":        "otherslot",
			"interface":   "test",
			"hotplug-key": "5678",
		}})

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-remove-slot", "")
	t.Set("hotplug-key", "1234")
	t.Set("interface", "test")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	// hotplugslot is removed from the repository and from the state
	c.Assert(repo.Slot("core", "hotplugslot"), IsNil)
	slot, err := repo.SlotForHotplugKey("test", "1234")
	c.Assert(err, IsNil)
	c.Assert(slot, IsNil)

	var hotplugSlots map[string]interface{}
	c.Assert(s.state.Get("hotplug-slots", &hotplugSlots), IsNil)
	c.Assert(hotplugSlots, DeepEquals, map[string]interface{}{
		"otherslot": map[string]interface{}{
			"name":         "otherslot",
			"interface":    "test",
			"hotplug-key":  "5678",
			"hotplug-gone": false,
		}})
}

func (s *interfaceManagerSuite) TestHotplugRemoveSlotWhenConnected(c *C) {
	coreInfo := s.mockAppSet(c, coreSnapYaml)
	repo := s.manager(c).Repository()
	err := repo.AddInterface(&ifacetest.TestInterface{
		InterfaceName: "test",
	})
	c.Assert(err, IsNil)

	c.Assert(repo.AddSlot(&snap.SlotInfo{
		Snap:       coreInfo.Info(),
		Name:       "hotplugslot",
		Interface:  "test",
		HotplugKey: "1234",
	}), IsNil)

	// validity check
	c.Assert(repo.Slot("core", "hotplugslot"), NotNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("hotplug-slots", map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":        "hotplugslot",
			"interface":   "test",
			"hotplug-key": "1234",
		}})
	s.state.Set("conns", map[string]interface{}{
		"consumer:plug core:hotplugslot": map[string]interface{}{
			"interface":    "test",
			"hotplug-key":  "1234",
			"hotplug-gone": true,
		}})

	chg := s.state.NewChange("hotplug change", "")
	t := s.state.NewTask("hotplug-remove-slot", "")
	t.Set("hotplug-key", "1234")
	t.Set("interface", "test")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	// hotplugslot is removed from the repository but not from the state, because of existing connection
	c.Assert(repo.Slot("core", "hotplugslot"), IsNil)
	slot, err := repo.SlotForHotplugKey("test", "1234")
	c.Assert(err, IsNil)
	c.Assert(slot, IsNil)

	var hotplugSlots map[string]interface{}
	c.Assert(s.state.Get("hotplug-slots", &hotplugSlots), IsNil)
	c.Assert(hotplugSlots, DeepEquals, map[string]interface{}{
		"hotplugslot": map[string]interface{}{
			"name":         "hotplugslot",
			"interface":    "test",
			"hotplug-key":  "1234",
			"hotplug-gone": true,
		}})
}

func (s *interfaceManagerSuite) TestHotplugSeqWaitTasks(c *C) {
	restore := ifacestate.MockHotplugRetryTimeout(5 * time.Millisecond)
	defer restore()

	var order []int
	_ = s.manager(c)
	s.o.TaskRunner().AddHandler("witness", func(task *state.Task, tomb *tomb.Tomb) error {
		task.State().Lock()
		defer task.State().Unlock()
		var seq int
		c.Assert(task.Get("seq", &seq), IsNil)
		order = append(order, seq)
		return nil
	}, nil)
	s.st.Lock()

	// create hotplug changes with witness task to track execution order
	for i := 10; i >= 1; i-- {
		chg := s.st.NewChange("hotplug-change", "")
		chg.Set("hotplug-key", "1234")
		chg.Set("hotplug-seq", i)
		t := s.st.NewTask("hotplug-seq-wait", "")
		witness := s.st.NewTask("witness", "")
		witness.Set("seq", i)
		witness.WaitFor(t)
		chg.AddTask(t)
		chg.AddTask(witness)
	}

	s.st.Unlock()

	s.settle(c)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(order, DeepEquals, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})

	for _, chg := range s.st.Changes() {
		c.Assert(chg.Status(), Equals, state.DoneStatus)
	}
}

func (s *interfaceManagerSuite) testConnectionStates(c *C, auto, byGadget, undesired, hotplugGone bool, expected map[string]ifacestate.ConnectionState) {
	slotAppSet := s.mockAppSet(c, producerYaml)
	plugAppSet := s.mockAppSet(c, consumerYaml)

	slotSnap := slotAppSet.Info()
	plugSnap := plugAppSet.Info()

	mgr := s.manager(c)

	conns, err := mgr.ConnectionStates()
	c.Assert(err, IsNil)
	c.Check(conns, HasLen, 0)

	st := s.state
	st.Lock()
	sc, err := ifacestate.GetConns(st)
	c.Assert(err, IsNil)

	slot := slotSnap.Slots["slot"]
	c.Assert(slot, NotNil)
	plug := plugSnap.Plugs["plug"]
	c.Assert(plug, NotNil)
	dynamicPlugAttrs := map[string]interface{}{"dynamic-number": 7}
	dynamicSlotAttrs := map[string]interface{}{"other-number": 9}
	// create connection in conns state
	conn := &interfaces.Connection{
		Plug: interfaces.NewConnectedPlug(plug, plugAppSet, nil, dynamicPlugAttrs),
		Slot: interfaces.NewConnectedSlot(slot, slotAppSet, nil, dynamicSlotAttrs),
	}
	ifacestate.UpdateConnectionInConnState(sc, conn, auto, byGadget, undesired, hotplugGone)
	ifacestate.SetConns(st, sc)
	st.Unlock()

	conns, err = mgr.ConnectionStates()
	c.Assert(err, IsNil)
	c.Assert(conns, HasLen, 1)
	c.Check(conns, DeepEquals, expected)
}

func (s *interfaceManagerSuite) TestConnectionStatesAutoManual(c *C) {
	var isAuto, byGadget, isUndesired, hotplugGone bool = true, false, false, false
	s.testConnectionStates(c, isAuto, byGadget, isUndesired, hotplugGone, map[string]ifacestate.ConnectionState{
		"consumer:plug producer:slot": {
			Interface: "test",
			Auto:      true,
			StaticPlugAttrs: map[string]interface{}{
				"attr1": "value1",
			},
			DynamicPlugAttrs: map[string]interface{}{
				"dynamic-number": int64(7),
			},
			StaticSlotAttrs: map[string]interface{}{
				"attr2": "value2",
			},
			DynamicSlotAttrs: map[string]interface{}{
				"other-number": int64(9),
			},
		}})
}

func (s *interfaceManagerSuite) TestConnectionStatesGadget(c *C) {
	var isAuto, byGadget, isUndesired, hotplugGone bool = true, true, false, false
	s.testConnectionStates(c, isAuto, byGadget, isUndesired, hotplugGone, map[string]ifacestate.ConnectionState{
		"consumer:plug producer:slot": {
			Interface: "test",
			Auto:      true,
			ByGadget:  true,
			StaticPlugAttrs: map[string]interface{}{
				"attr1": "value1",
			},
			DynamicPlugAttrs: map[string]interface{}{
				"dynamic-number": int64(7),
			},
			StaticSlotAttrs: map[string]interface{}{
				"attr2": "value2",
			},
			DynamicSlotAttrs: map[string]interface{}{
				"other-number": int64(9),
			},
		}})
}

func (s *interfaceManagerSuite) TestConnectionStatesUndesired(c *C) {
	var isAuto, byGadget, isUndesired, hotplugGone bool = true, false, true, false
	s.testConnectionStates(c, isAuto, byGadget, isUndesired, hotplugGone, map[string]ifacestate.ConnectionState{
		"consumer:plug producer:slot": {
			Interface: "test",
			Auto:      true,
			Undesired: true,
			StaticPlugAttrs: map[string]interface{}{
				"attr1": "value1",
			},
			DynamicPlugAttrs: map[string]interface{}{
				"dynamic-number": int64(7),
			},
			StaticSlotAttrs: map[string]interface{}{
				"attr2": "value2",
			},
			DynamicSlotAttrs: map[string]interface{}{
				"other-number": int64(9),
			},
		}})
}

func (s *interfaceManagerSuite) TestConnectionStatesHotplugGone(c *C) {
	var isAuto, byGadget, isUndesired, hotplugGone bool = false, false, false, true
	s.testConnectionStates(c, isAuto, byGadget, isUndesired, hotplugGone, map[string]ifacestate.ConnectionState{
		"consumer:plug producer:slot": {
			Interface:   "test",
			HotplugGone: true,
			StaticPlugAttrs: map[string]interface{}{
				"attr1": "value1",
			},
			DynamicPlugAttrs: map[string]interface{}{
				"dynamic-number": int64(7),
			},
			StaticSlotAttrs: map[string]interface{}{
				"attr2": "value2",
			},
			DynamicSlotAttrs: map[string]interface{}{
				"other-number": int64(9),
			},
		}})
}

func (s *interfaceManagerSuite) TestResolveDisconnectFromConns(c *C) {
	mgr := s.manager(c)

	st := s.st
	st.Lock()
	defer st.Unlock()

	st.Set("conns", map[string]interface{}{"some-snap:plug core:slot": map[string]interface{}{"interface": "foo"}})

	forget := true
	ref, err := mgr.ResolveDisconnect("some-snap", "plug", "core", "slot", forget)
	c.Check(err, IsNil)
	c.Check(ref, DeepEquals, []*interfaces.ConnRef{{
		PlugRef: interfaces.PlugRef{Snap: "some-snap", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "slot"}},
	})

	ref, err = mgr.ResolveDisconnect("some-snap", "plug", "", "slot", forget)
	c.Check(err, IsNil)
	c.Check(ref, DeepEquals, []*interfaces.ConnRef{
		{PlugRef: interfaces.PlugRef{Snap: "some-snap", Name: "plug"},
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "slot"}},
	})

	ref, err = mgr.ResolveDisconnect("some-snap", "plug", "", "slot", forget)
	c.Check(err, IsNil)
	c.Check(ref, DeepEquals, []*interfaces.ConnRef{
		{PlugRef: interfaces.PlugRef{Snap: "some-snap", Name: "plug"},
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "slot"}},
	})

	_, err = mgr.ResolveDisconnect("some-snap", "plug", "", "", forget)
	c.Check(err, IsNil)
	c.Check(ref, DeepEquals, []*interfaces.ConnRef{
		{PlugRef: interfaces.PlugRef{Snap: "some-snap", Name: "plug"},
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "slot"}},
	})

	ref, err = mgr.ResolveDisconnect("", "", "core", "slot", forget)
	c.Check(err, IsNil)
	c.Check(ref, DeepEquals, []*interfaces.ConnRef{
		{PlugRef: interfaces.PlugRef{Snap: "some-snap", Name: "plug"},
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "slot"}},
	})

	_, err = mgr.ResolveDisconnect("", "plug", "", "slot", forget)
	c.Check(err, ErrorMatches, `cannot forget connection core:plug from core:slot, it was not connected`)

	_, err = mgr.ResolveDisconnect("some-snap", "", "", "slot", forget)
	c.Check(err, ErrorMatches, `allowed forms are <snap>:<plug> <snap>:<slot> or <snap>:<plug or slot>`)

	_, err = mgr.ResolveDisconnect("other-snap", "plug", "", "slot", forget)
	c.Check(err, ErrorMatches, `cannot forget connection other-snap:plug from core:slot, it was not connected`)
}

func (s *interfaceManagerSuite) TestResolveDisconnectWithRepository(c *C) {
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})
	mgr := s.manager(c)

	consumerInfo := s.mockSnap(c, consumerYaml)
	producerInfo := s.mockSnap(c, producerYaml)

	consumerAppSet, err := interfaces.NewSnapAppSet(consumerInfo, nil)
	c.Assert(err, IsNil)

	producerAppSet, err := interfaces.NewSnapAppSet(producerInfo, nil)
	c.Assert(err, IsNil)

	repo := s.manager(c).Repository()
	c.Assert(repo.AddAppSet(consumerAppSet), IsNil)
	c.Assert(repo.AddAppSet(producerAppSet), IsNil)

	_, err = repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}, nil, nil, nil, nil, nil)

	c.Assert(err, IsNil)

	st := s.st
	st.Lock()
	defer st.Unlock()

	// resolve through interfaces repository because of forget=false
	forget := false
	ref, err := mgr.ResolveDisconnect("consumer", "plug", "producer", "slot", forget)
	c.Check(err, IsNil)
	c.Check(ref, DeepEquals, []*interfaces.ConnRef{
		{PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"}},
	})

	_, err = mgr.ResolveDisconnect("consumer", "foo", "producer", "slot", forget)
	c.Check(err, ErrorMatches, `snap "consumer" has no plug named "foo"`)
}

const someSnapYaml = `name: some-snap
version: 1
plugs:
  network:
`

const ubuntucoreSnapYaml = `name: ubuntu-core
version: 1.0
type: os
slots:
  network:

`
const coreSnapYaml2 = `name: core
version: 1.0
type: os
slots:
  network:
`

func (s *interfaceManagerSuite) TestTransitionConnectionsCoreMigration(c *C) {
	mgr := s.manager(c)

	st := s.st
	st.Lock()
	defer st.Unlock()

	repo := mgr.Repository()
	snapstate.Set(st, "core", nil)
	snapstate.Set(st, "ubuntu-core", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}}),
		Current:         snap.R(1),
		SnapType:        "os",
		TrackingChannel: "beta",
	})

	si := snap.SideInfo{RealName: "some-snap", Revision: snap.R(-42)}
	someSnap := ifacetest.MockInfoAndAppSet(c, someSnapYaml, nil, &si)
	ubuntuCore := ifacetest.MockInfoAndAppSet(c, ubuntucoreSnapYaml, nil, &snap.SideInfo{
		RealName: "ubuntu-core",
		Revision: snap.R(1),
	})
	core := ifacetest.MockInfoAndAppSet(c, coreSnapYaml2, nil, &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	})

	c.Assert(repo.AddAppSet(ubuntuCore), IsNil)
	c.Assert(repo.AddAppSet(core), IsNil)
	c.Assert(repo.AddAppSet(someSnap), IsNil)

	_, err := repo.Connect(&interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "some-snap", Name: "network"}, SlotRef: interfaces.SlotRef{Snap: "ubuntu-core", Name: "network"}}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	repoConns, err := repo.Connections("ubuntu-core")
	c.Assert(err, IsNil)
	c.Assert(repoConns, HasLen, 1)

	st.Set("conns", map[string]interface{}{"some-snap:network ubuntu-core:network": map[string]interface{}{"interface": "network", "auto": true}})

	c.Assert(mgr.TransitionConnectionsCoreMigration(st, "ubuntu-core", "core"), IsNil)

	// check connections
	var conns map[string]interface{}
	st.Get("conns", &conns)
	c.Assert(conns, DeepEquals, map[string]interface{}{"some-snap:network core:network": map[string]interface{}{"interface": "network", "auto": true}})

	repoConns, err = repo.Connections("ubuntu-core")
	c.Assert(err, IsNil)
	c.Assert(repoConns, HasLen, 0)
	repoConns, err = repo.Connections("core")
	c.Assert(err, IsNil)
	c.Assert(repoConns, HasLen, 1)

	// migrate back (i.e. undo)
	c.Assert(mgr.TransitionConnectionsCoreMigration(st, "core", "ubuntu-core"), IsNil)

	// check connections
	conns = nil
	st.Get("conns", &conns)
	c.Assert(conns, DeepEquals, map[string]interface{}{"some-snap:network ubuntu-core:network": map[string]interface{}{"interface": "network", "auto": true}})
	repoConns, err = repo.Connections("ubuntu-core")
	c.Assert(err, IsNil)
	c.Assert(repoConns, HasLen, 1)
	repoConns, err = repo.Connections("core")
	c.Assert(err, IsNil)
	c.Assert(repoConns, HasLen, 0)
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedAnySlotsPerPlugPlugSide(c *C) {
	s.MockModel(c, nil)

	// the producer snap
	s.MockSnapDecl(c, "theme1", "one-publisher", nil)

	// 2nd producer snap
	s.MockSnapDecl(c, "theme2", "one-publisher", nil)

	// the consumer
	s.MockSnapDecl(c, "theme-consumer", "one-publisher", map[string]interface{}{
		"format": "1",
		"plugs": map[string]interface{}{
			"content": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"slots-per-plug": "*",
				},
			},
		},
	})

	check := func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
		c.Check(repoConns, HasLen, 2)

		c.Check(conns, DeepEquals, map[string]interface{}{
			"theme-consumer:plug theme1:slot": map[string]interface{}{
				"auto":        true,
				"interface":   "content",
				"plug-static": map[string]interface{}{"content": "themes"},
				"slot-static": map[string]interface{}{"content": "themes"},
			},
			"theme-consumer:plug theme2:slot": map[string]interface{}{
				"auto":        true,
				"interface":   "content",
				"plug-static": map[string]interface{}{"content": "themes"},
				"slot-static": map[string]interface{}{"content": "themes"},
			},
		})
	}

	s.testDoSetupSnapSecurityAutoConnectsDeclBasedAnySlotsPerPlug(c, check)
}

func (s *interfaceManagerSuite) testDoSetupSnapSecurityAutoConnectsDeclBasedAnySlotsPerPlug(c *C, check func(map[string]interface{}, []*interfaces.ConnRef)) {
	const theme1Yaml = `
name: theme1
version: 1
slots:
  slot:
    interface: content
    content: themes
`
	s.mockSnap(c, theme1Yaml)
	const theme2Yaml = `
name: theme2
version: 1
slots:
  slot:
    interface: content
    content: themes
`
	s.mockSnap(c, theme2Yaml)

	mgr := s.manager(c)

	const themeConsumerYaml = `
name: theme-consumer
version: 1
plugs:
  plug:
    interface: content
    content: themes
`
	snapInfo := s.mockSnap(c, themeConsumerYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
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
	plug := repo.Plug("theme-consumer", "plug")
	c.Assert(plug, Not(IsNil))

	check(conns, repo.Interfaces().Connections)
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedAnySlotsPerPlugSlotSide(c *C) {
	s.MockModel(c, nil)

	// the producer snap
	s.MockSnapDecl(c, "theme1", "one-publisher", map[string]interface{}{
		"format": "1",
		"slots": map[string]interface{}{
			"content": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"slots-per-plug": "*",
				},
			},
		},
	})

	// 2nd producer snap
	s.MockSnapDecl(c, "theme2", "one-publisher", map[string]interface{}{
		"format": "1",
		"slots": map[string]interface{}{
			"content": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"slots-per-plug": "*",
				},
			},
		},
	})

	// the consumer
	s.MockSnapDecl(c, "theme-consumer", "one-publisher", nil)

	check := func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
		c.Check(repoConns, HasLen, 2)

		c.Check(conns, DeepEquals, map[string]interface{}{
			"theme-consumer:plug theme1:slot": map[string]interface{}{
				"auto":        true,
				"interface":   "content",
				"plug-static": map[string]interface{}{"content": "themes"},
				"slot-static": map[string]interface{}{"content": "themes"},
			},
			"theme-consumer:plug theme2:slot": map[string]interface{}{
				"auto":        true,
				"interface":   "content",
				"plug-static": map[string]interface{}{"content": "themes"},
				"slot-static": map[string]interface{}{"content": "themes"},
			},
		})
	}

	s.testDoSetupSnapSecurityAutoConnectsDeclBasedAnySlotsPerPlug(c, check)
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedAnySlotsPerPlugAmbiguity(c *C) {
	s.MockModel(c, nil)

	// the producer snap
	s.MockSnapDecl(c, "theme1", "one-publisher", map[string]interface{}{
		"format": "1",
		"slots": map[string]interface{}{
			"content": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"slots-per-plug": "*",
				},
			},
		},
	})

	// 2nd producer snap
	s.MockSnapDecl(c, "theme2", "one-publisher", map[string]interface{}{
		"format": "1",
		"slots": map[string]interface{}{
			"content": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"slots-per-plug": "1",
				},
			},
		},
	})

	// the consumer
	s.MockSnapDecl(c, "theme-consumer", "one-publisher", nil)

	check := func(conns map[string]interface{}, repoConns []*interfaces.ConnRef) {
		// slots-per-plug were ambigous, nothing was connected
		c.Check(repoConns, HasLen, 0)
		c.Check(conns, HasLen, 0)
	}

	s.testDoSetupSnapSecurityAutoConnectsDeclBasedAnySlotsPerPlug(c, check)
}

func (s *interfaceManagerSuite) TestDoSetupSnapSecurityAutoConnectsDeclBasedSlotNames(c *C) {
	s.MockModel(c, nil)

	restore := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
plugs:
  test:
    allow-auto-connection: false
`))
	defer restore()
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})

	s.MockSnapDecl(c, "gadget", "one-publisher", nil)

	const gadgetYaml = `
name: gadget
type: gadget
version: 1
slots:
  test1:
    interface: test
  test2:
    interface: test
`
	s.mockSnap(c, gadgetYaml)

	mgr := s.manager(c)

	s.MockSnapDecl(c, "consumer", "one-publisher", map[string]interface{}{
		"format": "4",
		"plugs": map[string]interface{}{
			"test": map[string]interface{}{
				"allow-auto-connection": map[string]interface{}{
					"slot-names": []interface{}{
						"test1",
					},
				},
			},
		}})

	const consumerYaml = `
name: consumer
version: 1
plugs:
  test:
`
	snapInfo := s.mockSnap(c, consumerYaml)

	// Run the setup-snap-security task and let it finish.
	change := s.addSetupSnapSecurityChange(c, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
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
	plug := repo.Plug("consumer", "test")
	c.Assert(plug, Not(IsNil))

	c.Check(conns, DeepEquals, map[string]interface{}{
		"consumer:test gadget:test1": map[string]interface{}{"auto": true, "interface": "test"},
	})
	c.Check(repo.Interfaces().Connections, HasLen, 1)
}

func (s *interfaceManagerSuite) autoconnectChangeForPreseeding(c *C, skipMarkPreseeded bool) (autoconnectTask, markPreseededTask *state.Task) {
	s.MockModel(c, nil)
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"}, &ifacetest.TestInterface{InterfaceName: "test2"})

	snapInfo := s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	// Initialize the manager. This registers the OS snap.
	_ = s.manager(c)

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapInfo.SnapName(),
			Revision: snapInfo.Revision,
		},
	}

	st := s.state
	st.Lock()
	defer st.Unlock()

	change := s.state.NewChange("test", "")
	autoconnectTask = s.state.NewTask("auto-connect", "")
	autoconnectTask.Set("snap-setup", snapsup)
	change.AddTask(autoconnectTask)
	if !skipMarkPreseeded {
		markPreseededTask = s.state.NewTask("mark-preseeded", "")
		markPreseededTask.WaitFor(autoconnectTask)
		change.AddTask(markPreseededTask)
	}
	installHook := s.state.NewTask("run-hook", "")
	hsup := &hookstate.HookSetup{
		Snap: snapInfo.InstanceName(),
		Hook: "install",
	}
	installHook.Set("hook-setup", &hsup)
	if markPreseededTask != nil {
		installHook.WaitFor(markPreseededTask)
	} else {
		installHook.WaitFor(autoconnectTask)
	}
	change.AddTask(installHook)
	return autoconnectTask, markPreseededTask
}

func (s *interfaceManagerSuite) TestPreseedAutoConnectWithInterfaceHooks(c *C) {
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	autoConnectTask, markPreseededTask := s.autoconnectChangeForPreseeding(c, false)

	st := s.state
	s.settle(c)
	st.Lock()
	defer st.Unlock()

	change := markPreseededTask.Change()
	c.Check(change.Status(), Equals, state.DoStatus)
	c.Check(autoConnectTask.Status(), Equals, state.DoneStatus)
	c.Check(markPreseededTask.Status(), Equals, state.DoStatus)

	checkWaitsForMarkPreseeded := func(t *state.Task) {
		var foundMarkPreseeded bool
		for _, wt := range t.WaitTasks() {
			if wt.Kind() == "mark-preseeded" {
				foundMarkPreseeded = true
				break
			}
		}
		c.Check(foundMarkPreseeded, Equals, true)
	}

	var setupProfilesCount, connectCount, hookCount, installHook int
	for _, t := range change.Tasks() {
		switch t.Kind() {
		case "setup-profiles":
			c.Check(ifacestate.InSameChangeWaitChain(markPreseededTask, t), Equals, true)
			checkWaitsForMarkPreseeded(t)
			setupProfilesCount++
		case "connect":
			c.Check(ifacestate.InSameChangeWaitChain(markPreseededTask, t), Equals, true)
			checkWaitsForMarkPreseeded(t)
			connectCount++
		case "run-hook":
			c.Check(ifacestate.InSameChangeWaitChain(markPreseededTask, t), Equals, true)
			var hsup hookstate.HookSetup
			c.Assert(t.Get("hook-setup", &hsup), IsNil)
			if hsup.Hook == "install" {
				installHook++
				checkWaitsForMarkPreseeded(t)
			}
			hookCount++
		case "auto-connect":
		case "mark-preseeded":
		default:
			c.Fatalf("unexpected task: %s", t.Kind())
		}
	}

	c.Check(setupProfilesCount, Equals, 1)
	c.Check(hookCount, Equals, 5)
	c.Check(connectCount, Equals, 1)
	c.Check(installHook, Equals, 1)
}

func (s *interfaceManagerSuite) TestPreseedAutoConnectInternalErrorOnMarkPreseededState(c *C) {
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	autoConnectTask, markPreseededTask := s.autoconnectChangeForPreseeding(c, false)

	st := s.state
	st.Lock()
	defer st.Unlock()

	markPreseededTask.SetStatus(state.DoingStatus)
	st.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Check(strings.Join(autoConnectTask.Log(), ""), Matches, `.* internal error: unexpected state of mark-preseeded task: Doing`)
}

func (s *interfaceManagerSuite) TestPreseedAutoConnectInternalErrorMarkPreseededMissing(c *C) {
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	skipMarkPreseeded := true
	autoConnectTask, markPreseededTask := s.autoconnectChangeForPreseeding(c, skipMarkPreseeded)
	c.Assert(markPreseededTask, IsNil)

	st := s.state
	st.Lock()
	defer st.Unlock()

	st.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Check(strings.Join(autoConnectTask.Log(), ""), Matches, `.* internal error: mark-preseeded task not found in preseeding mode`)
}

func (s *interfaceManagerSuite) TestFirstTaskAfterBootWhenPreseeding(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "")

	setupTask := st.NewTask("some-task", "")
	setupTask.Set("snap-setup", &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "test-snap"}})
	chg.AddTask(setupTask)

	markPreseeded := st.NewTask("fake-mark-preseeded", "")
	markPreseeded.WaitFor(setupTask)
	_, err := ifacestate.FirstTaskAfterBootWhenPreseeding("test-snap", markPreseeded)
	c.Check(err, ErrorMatches, `internal error: fake-mark-preseeded task not in change`)

	chg.AddTask(markPreseeded)

	_, err = ifacestate.FirstTaskAfterBootWhenPreseeding("test-snap", markPreseeded)
	c.Check(err, ErrorMatches, `internal error: cannot find install hook for snap "test-snap"`)

	// install hook of another snap
	task1 := st.NewTask("run-hook", "")
	hsup := hookstate.HookSetup{Hook: "install", Snap: "other-snap"}
	task1.Set("hook-setup", &hsup)
	task1.WaitFor(markPreseeded)
	chg.AddTask(task1)
	_, err = ifacestate.FirstTaskAfterBootWhenPreseeding("test-snap", markPreseeded)
	c.Check(err, ErrorMatches, `internal error: cannot find install hook for snap "test-snap"`)

	// add install hook for the correct snap
	task2 := st.NewTask("run-hook", "")
	hsup = hookstate.HookSetup{Hook: "install", Snap: "test-snap"}
	task2.Set("hook-setup", &hsup)
	task2.WaitFor(markPreseeded)
	chg.AddTask(task2)
	hooktask, err := ifacestate.FirstTaskAfterBootWhenPreseeding("test-snap", markPreseeded)
	c.Assert(err, IsNil)
	c.Check(hooktask.ID(), Equals, task2.ID())
}

// Tests for ResolveDisconnect()

// All the ways to resolve a 'snap disconnect' between two snaps.
// The actual snaps are not installed though.
func (s *interfaceManagerSuite) TestResolveDisconnectMatrixNoSnaps(c *C) {
	// Mock the interface that will be used by the test
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "interface"})
	mgr := s.manager(c)
	scenarios := []struct {
		plugSnapName, plugName, slotSnapName, slotName string
		errMsg                                         string
	}{
		// Case 0 (INVALID)
		// Nothing is provided
		{"", "", "", "", "allowed forms are .*"},
		// Case 1 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The snap name is implicit and refers to the core snap.
		{"", "", "", "slot", `snap "core" has no plug or slot named "slot"`},
		// Case 2 (INVALID)
		// The slot name is not provided.
		{"", "", "producer", "", "allowed forms are .*"},
		// Case 3 (FAILURE)
		// Disconnect anything connected to a specific plug or slot
		{"", "", "producer", "slot", `snap "producer" has no plug or slot named "slot"`},
		// Case 4 (FAILURE)
		// Disconnect everything from a specific plug or slot.
		// The plug side implicit refers to the core snap.
		{"", "plug", "", "", `snap "core" has no plug or slot named "plug"`},
		// Case 5 (FAILURE)
		// Disconnect a specific connection.
		// The plug and slot side implicit refers to the core snap.
		{"", "plug", "", "slot", `snap "core" has no plug named "plug"`},
		// Case 6 (INVALID)
		// Slot name is not provided.
		{"", "plug", "producer", "", "allowed forms are .*"},
		// Case 7 (FAILURE)
		// Disconnect a specific connection.
		// The plug side implicit refers to the core snap.
		{"", "plug", "producer", "slot", `snap "core" has no plug named "plug"`},
		// Case 8 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "", "allowed forms are .*"},
		// Case 9 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "slot", "allowed forms are .*"},
		// Case 10 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "", "allowed forms are .*"},
		// Case 11 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "slot", "allowed forms are .*"},
		// Case 12 (FAILURE)
		// Disconnect anything connected to a specific plug
		{"consumer", "plug", "", "", `snap "consumer" has no plug or slot named "plug"`},
		// Case 13 (FAILURE)
		// Disconnect a specific connection.
		// The snap name is implicit and refers to the core snap.
		{"consumer", "plug", "", "slot", `snap "consumer" has no plug named "plug"`},
		// Case 14 (INVALID)
		// The slot name was not provided.
		{"consumer", "plug", "producer", "", "allowed forms are .*"},
		// Case 15 (FAILURE)
		// Disconnect a specific connection.
		{"consumer", "plug", "producer", "slot", `snap "consumer" has no plug named "plug"`},
	}
	for i, scenario := range scenarios {
		c.Logf("checking scenario %d: %q", i, scenario)
		connRefList, err := mgr.ResolveDisconnect(
			scenario.plugSnapName, scenario.plugName, scenario.slotSnapName,
			scenario.slotName, false)
		c.Check(err, ErrorMatches, scenario.errMsg)
		c.Check(connRefList, HasLen, 0)
	}
}

// All the ways to resolve a 'snap disconnect' between two snaps.
// The actual snaps are not installed though but a snapd snap is.
func (s *interfaceManagerSuite) TestResolveDisconnectMatrixJustSnapdSnap(c *C) {
	restore := ifacestate.MockSnapMapper(&ifacestate.CoreSnapdSystemMapper{})
	defer restore()

	// Mock the interface that will be used by the test
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "interface"})
	mgr := s.manager(c)
	repo := mgr.Repository()
	// Rename the "slot" from the snapd snap so that it is not picked up below.
	c.Assert(snaptest.RenameSlot(s.snapdSnap.Info(), "slot", "unused"), IsNil)
	c.Assert(repo.AddAppSet(s.snapdSnap), IsNil)
	scenarios := []struct {
		plugSnapName, plugName, slotSnapName, slotName string
		errMsg                                         string
	}{
		// Case 0 (INVALID)
		// Nothing is provided
		{"", "", "", "", "allowed forms are .*"},
		// Case 1 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The snap name is implicit and refers to the snapd snap.
		{"", "", "", "slot", `snap "snapd" has no plug or slot named "slot"`},
		// Case 2 (INVALID)
		// The slot name is not provided.
		{"", "", "producer", "", "allowed forms are .*"},
		// Case 3 (FAILURE)
		// Disconnect anything connected to a specific plug or slot
		{"", "", "producer", "slot", `snap "producer" has no plug or slot named "slot"`},
		// Case 4 (FAILURE)
		// Disconnect anything connected to a specific plug or slot
		{"", "plug", "", "", `snap "snapd" has no plug or slot named "plug"`},
		// Case 5 (FAILURE)
		// Disconnect a specific connection.
		// The plug and slot side implicit refers to the snapd snap.
		{"", "plug", "", "slot", `snap "snapd" has no plug named "plug"`},
		// Case 6 (INVALID)
		// Slot name is not provided.
		{"", "plug", "producer", "", "allowed forms are .*"},
		// Case 7 (FAILURE)
		// Disconnect a specific connection.
		// The plug side implicit refers to the snapd snap.
		{"", "plug", "producer", "slot", `snap "snapd" has no plug named "plug"`},
		// Case 8 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "", "allowed forms are .*"},
		// Case 9 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "slot", "allowed forms are .*"},
		// Case 10 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "", "allowed forms are .*"},
		// Case 11 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "slot", "allowed forms are .*"},
		// Case 12 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		{"consumer", "plug", "", "", `snap "consumer" has no plug or slot named "plug"`},
		// Case 13 (FAILURE)
		// Disconnect a specific connection.
		// The snap name is implicit and refers to the snapd snap.
		{"consumer", "plug", "", "slot", `snap "consumer" has no plug named "plug"`},
		// Case 14 (INVALID)
		// The slot name was not provided.
		{"consumer", "plug", "producer", "", "allowed forms are .*"},
		// Case 15 (FAILURE)
		// Disconnect a specific connection.
		{"consumer", "plug", "producer", "slot", `snap "consumer" has no plug named "plug"`},
	}
	for i, scenario := range scenarios {
		c.Logf("checking scenario %d: %q", i, scenario)
		connRefList, err := mgr.ResolveDisconnect(
			scenario.plugSnapName, scenario.plugName, scenario.slotSnapName,
			scenario.slotName, false)
		c.Check(err, ErrorMatches, scenario.errMsg)
		c.Check(connRefList, HasLen, 0)
	}
}

// All the ways to resolve a 'snap disconnect' between two snaps.
// The actual snaps are not installed though but a core snap is.
func (s *interfaceManagerSuite) TestResolveDisconnectMatrixJustCoreSnap(c *C) {
	restore := ifacestate.MockSnapMapper(&ifacestate.CoreCoreSystemMapper{})
	defer restore()

	// Mock the interface that will be used by the test
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "interface"})
	mgr := s.manager(c)
	repo := mgr.Repository()
	// Rename the "slot" from the core snap so that it is not picked up below.
	c.Assert(snaptest.RenameSlot(s.coreSnap.Info(), "slot", "unused"), IsNil)
	c.Assert(repo.AddAppSet(s.coreSnap), IsNil)
	scenarios := []struct {
		plugSnapName, plugName, slotSnapName, slotName string
		errMsg                                         string
	}{
		// Case 0 (INVALID)
		// Nothing is provided
		{"", "", "", "", "allowed forms are .*"},
		// Case 1 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The snap name is implicit and refers to the core snap.
		{"", "", "", "slot", `snap "core" has no plug or slot named "slot"`},
		// Case 2 (INVALID)
		// The slot name is not provided.
		{"", "", "producer", "", "allowed forms are .*"},
		// Case 3 (FAILURE)
		// Disconnect anything connected to a specific plug or slot
		{"", "", "producer", "slot", `snap "producer" has no plug or slot named "slot"`},
		// Case 4 (FAILURE)
		// Disconnect anything connected to a specific plug or slot
		{"", "plug", "", "", `snap "core" has no plug or slot named "plug"`},
		// Case 5 (FAILURE)
		// Disconnect a specific connection.
		// The plug and slot side implicit refers to the core snap.
		{"", "plug", "", "slot", `snap "core" has no plug named "plug"`},
		// Case 6 (INVALID)
		// Slot name is not provided.
		{"", "plug", "producer", "", "allowed forms are .*"},
		// Case 7 (FAILURE)
		// Disconnect a specific connection.
		// The plug side implicit refers to the core snap.
		{"", "plug", "producer", "slot", `snap "core" has no plug named "plug"`},
		// Case 8 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "", "allowed forms are .*"},
		// Case 9 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "slot", "allowed forms are .*"},
		// Case 10 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "", "allowed forms are .*"},
		// Case 11 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "slot", "allowed forms are .*"},
		// Case 12 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		{"consumer", "plug", "", "", `snap "consumer" has no plug or slot named "plug"`},
		// Case 13 (FAILURE)
		// Disconnect a specific connection.
		// The snap name is implicit and refers to the core snap.
		{"consumer", "plug", "", "slot", `snap "consumer" has no plug named "plug"`},
		// Case 14 (INVALID)
		// The slot name was not provided.
		{"consumer", "plug", "producer", "", "allowed forms are .*"},
		// Case 15 (FAILURE)
		// Disconnect a specific connection.
		{"consumer", "plug", "producer", "slot", `snap "consumer" has no plug named "plug"`},
	}
	for i, scenario := range scenarios {
		c.Logf("checking scenario %d: %q", i, scenario)
		connRefList, err := mgr.ResolveDisconnect(
			scenario.plugSnapName, scenario.plugName, scenario.slotSnapName,
			scenario.slotName, false)
		c.Check(err, ErrorMatches, scenario.errMsg)
		c.Check(connRefList, HasLen, 0)
	}
}

// All the ways to resolve a 'snap disconnect' between two snaps.
// The actual snaps as well as the core snap are installed.
// The snaps are not connected.
func (s *interfaceManagerSuite) TestResolveDisconnectMatrixDisconnectedSnaps(c *C) {
	restore := ifacestate.MockSnapMapper(&ifacestate.CoreCoreSystemMapper{})
	defer restore()

	// Mock the interface that will be used by the test
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "interface"})
	mgr := s.manager(c)
	repo := mgr.Repository()
	// Rename the "slot" from the core snap so that it is not picked up below.
	c.Assert(snaptest.RenameSlot(s.coreSnap.Info(), "slot", "unused"), IsNil)
	c.Assert(repo.AddAppSet(s.coreSnap), IsNil)
	c.Assert(repo.AddAppSet(s.consumer), IsNil)
	c.Assert(repo.AddAppSet(s.producer), IsNil)
	scenarios := []struct {
		plugSnapName, plugName, slotSnapName, slotName string
		errMsg                                         string
	}{
		// Case 0 (INVALID)
		// Nothing is provided
		{"", "", "", "", "allowed forms are .*"},
		// Case 1 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The snap name is implicit and refers to the core snap.
		{"", "", "", "slot", `snap "core" has no plug or slot named "slot"`},
		// Case 2 (INVALID)
		// The slot name is not provided.
		{"", "", "producer", "", "allowed forms are .*"},
		// Case 3 (SUCCESS)
		// Disconnect anything connected to a specific plug or slot
		{"", "", "producer", "slot", ""},
		// Case 4 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The plug side implicit refers to the core snap.
		{"", "plug", "", "", `snap "core" has no plug or slot named "plug"`},
		// Case 5 (FAILURE)
		// Disconnect a specific connection.
		// The plug and slot side implicit refers to the core snap.
		{"", "plug", "", "slot", `snap "core" has no plug named "plug"`},
		// Case 6 (INVALID)
		// Slot name is not provided.
		{"", "plug", "producer", "", "allowed forms are .*"},
		// Case 7 (FAILURE)
		// Disconnect a specific connection.
		// The plug side implicit refers to the core snap.
		{"", "plug", "producer", "slot", `snap "core" has no plug named "plug"`},
		// Case 8 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "", "allowed forms are .*"},
		// Case 9 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "slot", "allowed forms are .*"},
		// Case 10 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "", "allowed forms are .*"},
		// Case 11 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "slot", "allowed forms are .*"},
		// Case 12 (SUCCESS)
		// Disconnect anything connected to a specific plug or slot.
		{"consumer", "plug", "", "", ""},
		// Case 13 (FAILURE)
		// Disconnect a specific connection.
		// The snap name is implicit and refers to the core snap.
		{"consumer", "plug", "", "slot", `snap "core" has no slot named "slot"`},
		// Case 14 (INVALID)
		// The slot name was not provided.
		{"consumer", "plug", "producer", "", "allowed forms are .*"},
		// Case 15 (FAILURE)
		// Disconnect a specific connection (but it is not connected).
		{"consumer", "plug", "producer", "slot", `cannot disconnect consumer:plug from producer:slot, it is not connected`},
	}
	for i, scenario := range scenarios {
		c.Logf("checking scenario %d: %q", i, scenario)
		connRefList, err := mgr.ResolveDisconnect(
			scenario.plugSnapName, scenario.plugName, scenario.slotSnapName,
			scenario.slotName, false)
		if scenario.errMsg != "" {
			c.Check(err, ErrorMatches, scenario.errMsg)
		} else {
			c.Check(err, IsNil)
		}
		c.Check(connRefList, HasLen, 0)
	}
}

// All the ways to resolve a 'snap disconnect' between two snaps.
// The actual snaps as well as the core snap are installed.
// The snaps are connected.
func (s *interfaceManagerSuite) TestResolveDisconnectMatrixTypical(c *C) {
	restore := ifacestate.MockSnapMapper(&ifacestate.CoreCoreSystemMapper{})
	defer restore()

	// Mock the interface that will be used by the test
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "interface"})
	mgr := s.manager(c)
	repo := mgr.Repository()

	// Rename the "slot" from the core snap so that it is not picked up below.
	c.Assert(snaptest.RenameSlot(s.coreSnap.Info(), "slot", "unused"), IsNil)
	c.Assert(repo.AddAppSet(s.coreSnap), IsNil)
	c.Assert(repo.AddAppSet(s.consumer), IsNil)
	c.Assert(repo.AddAppSet(s.producer), IsNil)
	connRef := interfaces.NewConnRef(s.consumerPlug, s.producerSlot)
	_, err := repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	scenarios := []struct {
		plugSnapName, plugName, slotSnapName, slotName string
		errMsg                                         string
	}{
		// Case 0 (INVALID)
		// Nothing is provided
		{"", "", "", "", "allowed forms are .*"},
		// Case 1 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The snap name is implicit and refers to the core snap.
		{"", "", "", "slot", `snap "core" has no plug or slot named "slot"`},
		// Case 2 (INVALID)
		// The slot name is not provided.
		{"", "", "producer", "", "allowed forms are .*"},
		// Case 3 (SUCCESS)
		// Disconnect anything connected to a specific plug or slot
		{"", "", "producer", "slot", ""},
		// Case 4 (FAILURE)
		// Disconnect anything connected to a specific plug or slot.
		// The plug side implicit refers to the core snap.
		{"", "plug", "", "", `snap "core" has no plug or slot named "plug"`},
		// Case 5 (FAILURE)
		// Disconnect a specific connection.
		// The plug and slot side implicit refers to the core snap.
		{"", "plug", "", "slot", `snap "core" has no plug named "plug"`},
		// Case 6 (INVALID)
		// Slot name is not provided.
		{"", "plug", "producer", "", "allowed forms are .*"},
		// Case 7 (FAILURE)
		// Disconnect a specific connection.
		// The plug side implicit refers to the core snap.
		{"", "plug", "producer", "slot", `snap "core" has no plug named "plug"`},
		// Case 8 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "", "allowed forms are .*"},
		// Case 9 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "", "slot", "allowed forms are .*"},
		// Case 10 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "", "allowed forms are .*"},
		// Case 11 (INVALID)
		// Plug name is not provided.
		{"consumer", "", "producer", "slot", "allowed forms are .*"},
		// Case 12 (SUCCESS)
		// Disconnect anything connected to a specific plug or slot.
		{"consumer", "plug", "", "", ""},
		// Case 13 (FAILURE)
		// Disconnect a specific connection.
		// The snap name is implicit and refers to the core snap.
		{"consumer", "plug", "", "slot", `snap "core" has no slot named "slot"`},
		// Case 14 (INVALID)
		// The slot name was not provided.
		{"consumer", "plug", "producer", "", "allowed forms are .*"},
		// Case 15 (SUCCESS)
		// Disconnect a specific connection.
		{"consumer", "plug", "producer", "slot", ""},
	}
	for i, scenario := range scenarios {
		c.Logf("checking scenario %d: %q", i, scenario)
		connRefList, err := mgr.ResolveDisconnect(
			scenario.plugSnapName, scenario.plugName, scenario.slotSnapName,
			scenario.slotName, false)
		if scenario.errMsg != "" {
			c.Check(err, ErrorMatches, scenario.errMsg)
			c.Check(connRefList, HasLen, 0)
		} else {
			c.Check(err, IsNil)
			c.Check(connRefList, DeepEquals, []*interfaces.ConnRef{connRef})
		}
	}
}

func (s *interfaceManagerSuite) TestConnectSetsUpSecurityFails(c *C) {
	s.MockModel(c, nil)
	s.mockIfaces(&ifacetest.TestInterface{InterfaceName: "test"})

	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	_ = s.manager(c)

	s.secBackend.SetupCallback = func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
		return fmt.Errorf("setup-callback failed")
	}

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
	c.Assert(change.Err(), ErrorMatches, `(?ms).*\(setup-callback failed\)`)
	c.Check(change.Status(), Equals, state.ErrorStatus)

	repo := s.manager(c).Repository()
	ifaces := repo.Interfaces()
	c.Check(ifaces.Connections, HasLen, 0)
}

func (s *interfaceManagerSuite) TestConnectionStateActive(c *C) {
	for i, cs := range []struct {
		undesired      bool
		hotplugGone    bool
		expectedActive bool
	}{
		{false, false, true},
		{true, false, false},
		{false, true, false},
		{true, true, false},
	} {
		connState := ifacestate.ConnectionState{Undesired: cs.undesired, HotplugGone: cs.hotplugGone}
		c.Assert(connState.Active(), Equals, cs.expectedActive, Commentf("#%d: %v", i, cs))
	}
}

func (s *interfaceManagerSuite) TestOnSnapLinkageChanged(c *C) {
	info := s.mockSnapInstance(c, "producer", producerYaml)
	c.Check(info, NotNil)

	s.state.Lock()
	defer s.state.Unlock()

	err := ifacestate.OnSnapLinkageChanged(s.state, &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "not-installed"}})
	c.Assert(err, IsNil)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "producer", &snapst)
	c.Assert(err, IsNil)

	// same as unlink-snap etc
	snapst.Active = false
	snapstate.Set(s.state, "producer", &snapst)

	err = ifacestate.OnSnapLinkageChanged(s.state, &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "producer"}})
	c.Assert(err, IsNil)

	err = snapstate.Get(s.state, "producer", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst, DeepEquals, snapstate.SnapState{
		SnapType: "app",
		Active:   false,
		Current:  snap.R(1),
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&info.SideInfo}),
		PendingSecurity: &snapstate.PendingSecurityState{
			SideInfo: &info.SideInfo,
		},
	})

	// same as link-snap etc
	snapst.Active = true
	snapstate.Set(s.state, "producer", &snapst)

	err = ifacestate.OnSnapLinkageChanged(s.state, &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "producer"}})
	c.Assert(err, IsNil)

	var snapst1 snapstate.SnapState
	err = snapstate.Get(s.state, "producer", &snapst1)
	c.Assert(err, IsNil)

	c.Check(snapst1, DeepEquals, snapstate.SnapState{
		SnapType: "app",
		Active:   true,
		Current:  snap.R(1),
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&info.SideInfo}),
	})
}
