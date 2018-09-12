// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package overlord_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

func TestOverlord(t *testing.T) { TestingT(t) }

type overlordSuite struct{}

var _ = Suite(&overlordSuite{})

func (ovs *overlordSuite) SetUpTest(c *C) {
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	dirs.SnapStateFile = filepath.Join(tmpdir, "test.json")
	snapstate.CanAutoRefresh = nil
}

func (ovs *overlordSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (ovs *overlordSuite) TestNew(c *C) {
	restore := patch.Mock(42, 2, nil)
	defer restore()

	var configstateInitCalled bool
	overlord.MockConfigstateInit(func(*hookstate.HookManager) {
		configstateInitCalled = true
	})

	o, err := overlord.New()
	c.Assert(err, IsNil)
	c.Check(o, NotNil)

	c.Check(o.StateEngine(), NotNil)
	c.Check(o.TaskRunner(), NotNil)
	c.Check(o.SnapManager(), NotNil)
	c.Check(o.AssertManager(), NotNil)
	c.Check(o.InterfaceManager(), NotNil)
	c.Check(o.HookManager(), NotNil)
	c.Check(o.DeviceManager(), NotNil)
	c.Check(o.CommandManager(), NotNil)
	c.Check(o.SnapshotManager(), NotNil)
	c.Check(configstateInitCalled, Equals, true)

	o.InterfaceManager().DisableUDevMonitor()

	s := o.State()
	c.Check(s, NotNil)
	c.Check(o.Engine().State(), Equals, s)

	s.Lock()
	defer s.Unlock()
	var patchLevel, patchSublevel int
	s.Get("patch-level", &patchLevel)
	c.Check(patchLevel, Equals, 42)
	s.Get("patch-sublevel", &patchSublevel)
	c.Check(patchSublevel, Equals, 2)
	var refreshPrivacyKey string
	s.Get("refresh-privacy-key", &refreshPrivacyKey)
	c.Check(refreshPrivacyKey, HasLen, 16)

	// store is setup
	sto := snapstate.Store(s)
	c.Check(sto, FitsTypeOf, &store.Store{})
	c.Check(sto.(*store.Store).CacheDownloads(), Equals, 5)
}

func (ovs *overlordSuite) TestNewWithGoodState(c *C) {
	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"some":"data","refresh-privacy-key":"0123456789ABCDEF"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level))
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	o, err := overlord.New()
	c.Assert(err, IsNil)

	state := o.State()
	c.Assert(err, IsNil)
	state.Lock()
	defer state.Unlock()

	d, err := state.MarshalJSON()
	c.Assert(err, IsNil)

	var got, expected map[string]interface{}
	err = json.Unmarshal(d, &got)
	c.Assert(err, IsNil)
	err = json.Unmarshal(fakeState, &expected)
	c.Assert(err, IsNil)

	c.Check(got, DeepEquals, expected)
}

func (ovs *overlordSuite) TestNewWithStateSnapmgrUpdate(c *C) {
	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"some":"data"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level))
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	o, err := overlord.New()
	c.Assert(err, IsNil)

	state := o.State()
	c.Assert(err, IsNil)
	state.Lock()
	defer state.Unlock()

	var refreshPrivacyKey string
	state.Get("refresh-privacy-key", &refreshPrivacyKey)
	c.Check(refreshPrivacyKey, HasLen, 16)
}

func (ovs *overlordSuite) TestNewWithInvalidState(c *C) {
	fakeState := []byte(``)
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	_, err = overlord.New()
	c.Assert(err, ErrorMatches, "EOF")
}

func (ovs *overlordSuite) TestNewWithPatches(c *C) {
	p := func(s *state.State) error {
		s.Set("patched", true)
		return nil
	}
	sp := func(s *state.State) error {
		s.Set("patched2", true)
		return nil
	}
	patch.Mock(1, 1, map[int][]patch.PatchFunc{1: {p, sp}})

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":0, "patch-sublevel":0}}`))
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	o, err := overlord.New()
	c.Assert(err, IsNil)

	state := o.State()
	c.Assert(err, IsNil)
	state.Lock()
	defer state.Unlock()

	var level int
	err = state.Get("patch-level", &level)
	c.Assert(err, IsNil)
	c.Check(level, Equals, 1)

	var sublevel int
	c.Assert(state.Get("patch-sublevel", &sublevel), IsNil)
	c.Check(sublevel, Equals, 1)

	var b bool
	err = state.Get("patched", &b)
	c.Assert(err, IsNil)
	c.Check(b, Equals, true)

	c.Assert(state.Get("patched2", &b), IsNil)
	c.Check(b, Equals, true)
}

type witnessManager struct {
	state          *state.State
	expectedEnsure int
	ensureCalled   chan struct{}
	ensureCallback func(s *state.State) error
}

func (wm *witnessManager) Ensure() error {
	if wm.expectedEnsure--; wm.expectedEnsure == 0 {
		close(wm.ensureCalled)
		return nil
	}
	if wm.ensureCallback != nil {
		return wm.ensureCallback(wm.state)
	}
	return nil
}

// markSeeded flags the state under the overlord as seeded to avoid running the seeding code in these tests
func markSeeded(o *overlord.Overlord) {
	st := o.State()
	st.Lock()
	st.Set("seeded", true)
	auth.SetDevice(st, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "serialserial",
	})
	st.Unlock()
}

func (ovs *overlordSuite) TestTrivialRunAndStop(c *C) {
	o, err := overlord.New()
	c.Assert(err, IsNil)

	markSeeded(o)
	// make sure we don't try to talk to the store
	snapstate.CanAutoRefresh = nil

	o.Loop()

	err = o.Stop()
	c.Assert(err, IsNil)
}

func (ovs *overlordSuite) TestUnknownTasks(c *C) {
	o, err := overlord.New()
	c.Assert(err, IsNil)
	o.InterfaceManager().DisableUDevMonitor()

	markSeeded(o)
	// make sure we don't try to talk to the store
	snapstate.CanAutoRefresh = nil

	// unknown tasks are ignored and succeed
	st := o.State()
	st.Lock()
	defer st.Unlock()
	t := st.NewTask("unknown", "...")
	chg := st.NewChange("change-w-unknown", "...")
	chg.AddTask(t)

	st.Unlock()
	err = o.Settle(1 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (ovs *overlordSuite) TestEnsureLoopRunAndStop(c *C) {
	restoreIntv := overlord.MockEnsureInterval(10 * time.Millisecond)
	defer restoreIntv()
	o := overlord.Mock()

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 3,
		ensureCalled:   make(chan struct{}),
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	t0 := time.Now()
	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
	c.Check(time.Since(t0) >= 10*time.Millisecond, Equals, true)

	err := o.Stop()
	c.Assert(err, IsNil)
}

func (ovs *overlordSuite) TestEnsureLoopMediatedEnsureBeforeImmediate(c *C) {
	restoreIntv := overlord.MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o := overlord.Mock()

	ensure := func(s *state.State) error {
		s.EnsureBefore(0)
		return nil
	}

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallback: ensure,
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (ovs *overlordSuite) TestEnsureLoopMediatedEnsureBefore(c *C) {
	restoreIntv := overlord.MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o := overlord.Mock()

	ensure := func(s *state.State) error {
		s.EnsureBefore(10 * time.Millisecond)
		return nil
	}

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallback: ensure,
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (ovs *overlordSuite) TestEnsureBeforeSleepy(c *C) {
	restoreIntv := overlord.MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o := overlord.Mock()

	ensure := func(s *state.State) error {
		overlord.MockEnsureNext(o, time.Now().Add(-10*time.Hour))
		s.EnsureBefore(0)
		return nil
	}

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallback: ensure,
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (ovs *overlordSuite) TestEnsureLoopMediatedEnsureBeforeOutsideEnsure(c *C) {
	restoreIntv := overlord.MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o := overlord.Mock()

	ch := make(chan struct{})
	ensure := func(s *state.State) error {
		close(ch)
		return nil
	}

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallback: ensure,
	}
	o.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}

	o.State().EnsureBefore(0)

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (ovs *overlordSuite) TestEnsureLoopPrune(c *C) {
	restoreIntv := overlord.MockPruneInterval(200*time.Millisecond, 1000*time.Millisecond, 1000*time.Millisecond)
	defer restoreIntv()
	o := overlord.Mock()

	st := o.State()
	st.Lock()
	t1 := st.NewTask("foo", "...")
	chg1 := st.NewChange("abort", "...")
	chg1.AddTask(t1)
	chg2 := st.NewChange("prune", "...")
	chg2.SetStatus(state.DoneStatus)
	t0 := chg2.ReadyTime()
	st.Unlock()

	// observe the loop cycles to detect when prune should have happened
	pruneHappened := make(chan struct{})
	cycles := -1
	waitForPrune := func(_ *state.State) error {
		if cycles == -1 {
			if time.Since(t0) > 1000*time.Millisecond {
				cycles = 2 // wait a couple more loop cycles
			}
			return nil
		}
		if cycles > 0 {
			cycles--
			if cycles == 0 {
				close(pruneHappened)
			}
		}
		return nil
	}
	witness := &witnessManager{
		ensureCallback: waitForPrune,
	}
	o.AddManager(witness)

	o.Loop()

	select {
	case <-pruneHappened:
	case <-time.After(2 * time.Second):
		c.Fatal("Pruning should have happened by now")
	}

	err := o.Stop()
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(st.Change(chg1.ID()), Equals, chg1)
	c.Assert(st.Change(chg2.ID()), IsNil)

	c.Assert(t1.Status(), Equals, state.HoldStatus)
}

func (ovs *overlordSuite) TestEnsureLoopPruneRunsMultipleTimes(c *C) {
	restoreIntv := overlord.MockPruneInterval(100*time.Millisecond, 1000*time.Millisecond, 1*time.Hour)
	defer restoreIntv()
	o := overlord.Mock()

	// create two changes, one that can be pruned now, one in progress
	st := o.State()
	st.Lock()
	t1 := st.NewTask("foo", "...")
	chg1 := st.NewChange("pruneNow", "...")
	chg1.AddTask(t1)
	t1.SetStatus(state.DoneStatus)
	t2 := st.NewTask("foo", "...")
	chg2 := st.NewChange("pruneNext", "...")
	chg2.AddTask(t2)
	t2.SetStatus(state.DoStatus)
	c.Check(st.Changes(), HasLen, 2)
	st.Unlock()

	// start the loop that runs the prune ticker
	o.Loop()

	// ensure the first change is pruned
	time.Sleep(1500 * time.Millisecond)
	st.Lock()
	c.Check(st.Changes(), HasLen, 1)
	st.Unlock()

	// ensure the second is also purged after it is ready
	st.Lock()
	chg2.SetStatus(state.DoneStatus)
	st.Unlock()
	time.Sleep(1500 * time.Millisecond)
	st.Lock()
	c.Check(st.Changes(), HasLen, 0)
	st.Unlock()

	// cleanup loop ticker
	err := o.Stop()
	c.Assert(err, IsNil)
}

func (ovs *overlordSuite) TestCheckpoint(c *C) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	o, err := overlord.New()
	c.Assert(err, IsNil)

	s := o.State()
	s.Lock()
	s.Set("mark", 1)
	s.Unlock()

	st, err := os.Stat(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	c.Assert(st.Mode(), Equals, os.FileMode(0600))

	c.Check(dirs.SnapStateFile, testutil.FileContains, `"mark":1`)
}

type sampleManager struct {
	ensureCallback func()
}

func newSampleManager(s *state.State, runner *state.TaskRunner) *sampleManager {
	sm := &sampleManager{}

	runner.AddHandler("runMgr1", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgr1Mark", 1)
		return nil
	}, nil)
	runner.AddHandler("runMgr2", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgr2Mark", 1)
		return nil
	}, nil)
	runner.AddHandler("runMgrEnsureBefore", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.EnsureBefore(20 * time.Millisecond)
		return nil
	}, nil)
	runner.AddHandler("runMgrForever", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.EnsureBefore(20 * time.Millisecond)
		return &state.Retry{}
	}, nil)
	runner.AddHandler("runMgrWCleanup", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgrWCleanupMark", 1)
		return nil
	}, nil)
	runner.AddCleanup("runMgrWCleanup", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgrWCleanupCleanedUp", 1)
		return nil
	})

	return sm
}

func (sm *sampleManager) Ensure() error {
	if sm.ensureCallback != nil {
		sm.ensureCallback()
	}
	return nil
}

func (ovs *overlordSuite) TestTrivialSettle(c *C) {
	restoreIntv := overlord.MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := overlord.Mock()

	s := o.State()
	sm1 := newSampleManager(s, o.TaskRunner())
	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.Engine().Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t1 := s.NewTask("runMgr1", "1...")
	chg.AddTask(t1)

	s.Unlock()
	o.Settle(5 * time.Second)
	s.Lock()
	c.Check(t1.Status(), Equals, state.DoneStatus)

	var v int
	err := s.Get("runMgr1Mark", &v)
	c.Check(err, IsNil)
}

func (ovs *overlordSuite) TestSettleNotConverging(c *C) {
	restoreIntv := overlord.MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := overlord.Mock()

	s := o.State()
	sm1 := newSampleManager(s, o.TaskRunner())
	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.Engine().Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t1 := s.NewTask("runMgrForever", "1...")
	chg.AddTask(t1)

	s.Unlock()
	err := o.Settle(250 * time.Millisecond)
	s.Lock()

	c.Check(err, ErrorMatches, `Settle is not converging`)

}

func (ovs *overlordSuite) TestSettleChain(c *C) {
	restoreIntv := overlord.MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := overlord.Mock()

	s := o.State()
	sm1 := newSampleManager(s, o.TaskRunner())
	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.Engine().Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t1 := s.NewTask("runMgr1", "1...")
	t2 := s.NewTask("runMgr2", "2...")
	t2.WaitFor(t1)
	chg.AddAll(state.NewTaskSet(t1, t2))

	s.Unlock()
	o.Settle(5 * time.Second)
	s.Lock()
	c.Check(t1.Status(), Equals, state.DoneStatus)
	c.Check(t2.Status(), Equals, state.DoneStatus)

	var v int
	err := s.Get("runMgr1Mark", &v)
	c.Check(err, IsNil)
	err = s.Get("runMgr2Mark", &v)
	c.Check(err, IsNil)
}

func (ovs *overlordSuite) TestSettleChainWCleanup(c *C) {
	restoreIntv := overlord.MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := overlord.Mock()

	s := o.State()
	sm1 := newSampleManager(s, o.TaskRunner())
	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.Engine().Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t1 := s.NewTask("runMgrWCleanup", "1...")
	t2 := s.NewTask("runMgr2", "2...")
	t2.WaitFor(t1)
	chg.AddAll(state.NewTaskSet(t1, t2))

	s.Unlock()
	o.Settle(5 * time.Second)
	s.Lock()
	c.Check(t1.Status(), Equals, state.DoneStatus)
	c.Check(t2.Status(), Equals, state.DoneStatus)

	var v int
	err := s.Get("runMgrWCleanupMark", &v)
	c.Check(err, IsNil)
	err = s.Get("runMgr2Mark", &v)
	c.Check(err, IsNil)

	err = s.Get("runMgrWCleanupCleanedUp", &v)
	c.Check(err, IsNil)
}

func (ovs *overlordSuite) TestSettleExplicitEnsureBefore(c *C) {
	restoreIntv := overlord.MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o := overlord.Mock()

	s := o.State()
	sm1 := newSampleManager(s, o.TaskRunner())
	sm1.ensureCallback = func() {
		s.Lock()
		defer s.Unlock()
		v := 0
		s.Get("ensureCount", &v)
		s.Set("ensureCount", v+1)
	}

	o.AddManager(sm1)
	o.AddManager(o.TaskRunner())

	defer o.Engine().Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t := s.NewTask("runMgrEnsureBefore", "...")
	chg.AddTask(t)

	s.Unlock()
	o.Settle(5 * time.Second)
	s.Lock()
	c.Check(t.Status(), Equals, state.DoneStatus)

	var v int
	err := s.Get("ensureCount", &v)
	c.Check(err, IsNil)
	c.Check(v, Equals, 2)
}

func (ovs *overlordSuite) TestRequestRestartNoHandler(c *C) {
	o, err := overlord.New()
	c.Assert(err, IsNil)

	o.State().RequestRestart(state.RestartDaemon)
}

func (ovs *overlordSuite) TestRequestRestartHandler(c *C) {
	o, err := overlord.New()
	c.Assert(err, IsNil)

	restartRequested := false

	o.SetRestartHandler(func(t state.RestartType) {
		restartRequested = true
	})

	o.State().RequestRestart(state.RestartDaemon)

	c.Check(restartRequested, Equals, true)
}
