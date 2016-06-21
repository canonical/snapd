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

package overlord_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"

	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/state"
)

func TestOverlord(t *testing.T) { TestingT(t) }

type overlordSuite struct{}

var _ = Suite(&overlordSuite{})

func (ovs *overlordSuite) SetUpTest(c *C) {
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	dirs.SnapStateFile = filepath.Join(tmpdir, "test.json")
}

func (ovs *overlordSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (ovs *overlordSuite) TestNew(c *C) {
	restore := overlord.MockPatches(1, nil)
	defer restore()

	o, err := overlord.New()
	c.Assert(err, IsNil)
	c.Check(o, NotNil)

	c.Check(o.SnapManager(), NotNil)
	c.Check(o.AssertManager(), NotNil)
	c.Check(o.InterfaceManager(), NotNil)

	s := o.State()
	c.Check(s, NotNil)
	c.Check(o.Engine().State(), Equals, s)

	s.Lock()
	defer s.Unlock()
	var patchLevel int
	s.Get("patch-level", &patchLevel)
	c.Check(patchLevel, Equals, 1)
}

func (ovs *overlordSuite) TestNewWithGoodState(c *C) {
	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"some":"data"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0}`, overlord.PatchLevel()))
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

func (ovs *overlordSuite) TestNewWithInvalidState(c *C) {
	fakeState := []byte(``)
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	_, err = overlord.New()
	c.Assert(err, ErrorMatches, "EOF")
}

func (ovs *overlordSuite) TestNewNoDowngrade(c *C) {
	overlord.MockPatches(2, nil)

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"some":"data"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0}`, 3))
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	_, err = overlord.New()
	c.Assert(err, ErrorMatches, `cannot downgrade: snapd is too old for the current state patch level 3`)
}

func (ovs *overlordSuite) TestNewWithMigrations(c *C) {
	m12 := func(s *state.State, commit func()) error {
		s.Lock()
		s.Set("m12", true)
		commit()
		return nil
	}
	m23 := func(s *state.State, commit func()) error {
		s.Lock()
		s.Set("m23", true)
		commit()
		return nil
	}
	overlord.MockPatches(3, map[int]func(*state.State, func()) error{
		1: m12,
		2: m23,
	})

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"some":"data"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0}`, 1))
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	o, err := overlord.New()
	c.Assert(err, IsNil)

	state := o.State()
	c.Assert(err, IsNil)
	state.Lock()
	defer state.Unlock()

	var level int
	var m12f, m23f bool
	err = state.Get("patch-level", &level)
	c.Assert(err, IsNil)
	c.Check(level, Equals, 3)

	err = state.Get("m12", &m12f)
	c.Assert(err, IsNil)
	c.Check(m12f, Equals, true)

	err = state.Get("m23", &m23f)
	c.Assert(err, IsNil)
	c.Check(m12f, Equals, true)
}

func (ovs *overlordSuite) TestNewWithMissingMigrations(c *C) {
	m23 := func(s *state.State, commit func()) error {
		s.Lock()
		s.Set("m23", true)
		commit()
		return nil
	}
	overlord.MockPatches(3, map[int]func(*state.State, func()) error{
		2: m23,
	})

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"some":"data"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0}`, 1))
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	_, err = overlord.New()
	c.Assert(err, ErrorMatches, `cannot migrate from state patch level 1 to 2: no supported migration`)
}

func (ovs *overlordSuite) TestNewWithMigrationError(c *C) {
	m12 := func(s *state.State, commit func()) error {
		return fmt.Errorf("m12 failed")
	}
	m23 := func(s *state.State, commit func()) error {
		s.Set("m23", true)
		return nil
	}
	overlord.MockPatches(3, map[int]func(*state.State, func()) error{
		1: m12,
		2: m23,
	})

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"some":"data"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0}`, 1))
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	_, err = overlord.New()
	c.Assert(err, ErrorMatches, `cannot migrate from state patch level 1 to 2: m12 failed`)

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()

	s, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	s.Lock()
	defer s.Unlock()

	var level int
	var m12f, m23f bool
	err = s.Get("patch-level", &level)
	c.Assert(err, IsNil)
	c.Check(level, Equals, 1)

	err = s.Get("m12", &m12f)
	c.Assert(err, Equals, state.ErrNoState)

	err = s.Get("m23", &m23f)
	c.Assert(err, Equals, state.ErrNoState)
}

func (ovs *overlordSuite) TestMigrationsSanity(c *C) {
	if overlord.PatchLevel() == 0 {
		c.Assert(len(overlord.Migrations), Equals, 0)
		c.Skip("patch level still at 0, no migrations")
	}
	from := make([]int, 0, len(overlord.Migrations))
	for l, _ := range overlord.Migrations {
		from = append(from, l)
	}
	sort.Ints(from)
	// all steps present
	for i := 1; i < len(from); i++ {
		c.Check(from[i], Equals, from[i-1]+1)
	}
	// ends at previous of implemented patch level
	c.Check(from[len(from)-1], Equals, overlord.PatchLevel()-1)
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

func (wm *witnessManager) Stop() {
}

func (wm *witnessManager) Wait() {
}

func (ovs *overlordSuite) TestTrivialRunAndStop(c *C) {
	o, err := overlord.New()
	c.Assert(err, IsNil)

	o.Loop()

	err = o.Stop()
	c.Assert(err, IsNil)
}

func (ovs *overlordSuite) TestEnsureLoopRunAndStop(c *C) {
	restoreIntv := overlord.MockEnsureInterval(10 * time.Millisecond)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	witness := &witnessManager{
		state:          o.State(),
		expectedEnsure: 3,
		ensureCalled:   make(chan struct{}),
	}
	o.Engine().AddManager(witness)

	o.Loop()
	defer o.Stop()

	t0 := time.Now()
	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
	c.Check(time.Since(t0) >= 10*time.Millisecond, Equals, true)

	err = o.Stop()
	c.Assert(err, IsNil)
}

func (ovs *overlordSuite) TestEnsureLoopMediatedEnsureBeforeImmediate(c *C) {
	restoreIntv := overlord.MockEnsureInterval(10 * time.Minute)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

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
	se := o.Engine()
	se.AddManager(witness)

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
	o, err := overlord.New()
	c.Assert(err, IsNil)

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
	se := o.Engine()
	se.AddManager(witness)

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

	o, err := overlord.New()
	c.Assert(err, IsNil)

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
	se := o.Engine()
	se.AddManager(witness)

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
	o, err := overlord.New()
	c.Assert(err, IsNil)

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
	se := o.Engine()
	se.AddManager(witness)

	o.Loop()
	defer o.Stop()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}

	se.State().EnsureBefore(0)

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (ovs *overlordSuite) TestEnsureLoopPrune(c *C) {
	restoreIntv := overlord.MockPruneInterval(10*time.Millisecond, 5*time.Millisecond, 5*time.Millisecond)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	st := o.State()
	st.Lock()
	t1 := st.NewTask("foo", "...")
	chg1 := st.NewChange("abort", "...")
	chg1.AddTask(t1)
	chg2 := st.NewChange("prune", "...")
	chg2.SetStatus(state.DoneStatus)
	st.Unlock()

	o.Loop()
	time.Sleep(50 * time.Millisecond)
	err = o.Stop()
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(st.Change(chg1.ID()), Equals, chg1)
	c.Assert(st.Change(chg2.ID()), IsNil)

	c.Assert(t1.Status(), Equals, state.HoldStatus)
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

	content, err := ioutil.ReadFile(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	c.Check(string(content), testutil.Contains, `"mark":1`)
}

type runnerManager struct {
	runner         *state.TaskRunner
	ensureCallback func()
}

func newRunnerManager(s *state.State) *runnerManager {
	rm := &runnerManager{
		runner: state.NewTaskRunner(s),
	}

	rm.runner.AddHandler("runMgr1", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgr1Mark", 1)
		return nil
	}, nil)
	rm.runner.AddHandler("runMgr2", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgr2Mark", 1)
		return nil
	}, nil)
	rm.runner.AddHandler("runMgrEnsureBefore", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.EnsureBefore(20 * time.Millisecond)
		return nil
	}, nil)

	return rm
}

func (rm *runnerManager) Ensure() error {
	if rm.ensureCallback != nil {
		rm.ensureCallback()
	}
	rm.runner.Ensure()
	return nil
}

func (rm *runnerManager) Stop() {
	rm.runner.Stop()
}

func (rm *runnerManager) Wait() {
	rm.runner.Wait()
}

func (ovs *overlordSuite) TestTrivialSettle(c *C) {
	restoreIntv := overlord.MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	se := o.Engine()
	s := se.State()
	rm1 := newRunnerManager(s)
	se.AddManager(rm1)

	defer o.Engine().Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t1 := s.NewTask("runMgr1", "1...")
	chg.AddTask(t1)

	s.Unlock()

	o.Settle()

	s.Lock()
	c.Check(t1.Status(), Equals, state.DoneStatus)

	var v int
	err = s.Get("runMgr1Mark", &v)
	c.Check(err, IsNil)
}

func (ovs *overlordSuite) TestSettleChain(c *C) {
	restoreIntv := overlord.MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	se := o.Engine()
	s := se.State()
	rm1 := newRunnerManager(s)
	se.AddManager(rm1)

	defer o.Engine().Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t1 := s.NewTask("runMgr1", "1...")
	t2 := s.NewTask("runMgr2", "2...")
	t2.WaitFor(t1)
	chg.AddAll(state.NewTaskSet(t1, t2))

	s.Unlock()

	o.Settle()

	s.Lock()
	c.Check(t1.Status(), Equals, state.DoneStatus)
	c.Check(t2.Status(), Equals, state.DoneStatus)

	var v int
	err = s.Get("runMgr1Mark", &v)
	c.Check(err, IsNil)
	err = s.Get("runMgr2Mark", &v)
	c.Check(err, IsNil)
}

func (ovs *overlordSuite) TestSettleExplicitEnsureBefore(c *C) {
	restoreIntv := overlord.MockEnsureInterval(1 * time.Minute)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	se := o.Engine()
	s := se.State()
	rm1 := newRunnerManager(s)
	rm1.ensureCallback = func() {
		s.Lock()
		defer s.Unlock()
		v := 0
		s.Get("ensureCount", &v)
		s.Set("ensureCount", v+1)
	}

	se.AddManager(rm1)

	defer o.Engine().Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t := s.NewTask("runMgrEnsureBefore", "...")
	chg.AddTask(t)
	s.Unlock()

	o.Settle()

	s.Lock()
	c.Check(t.Status(), Equals, state.DoneStatus)

	var v int
	err = s.Get("ensureCount", &v)
	c.Check(err, IsNil)
	c.Check(v, Equals, 2)
}

func (ovs *overlordSuite) TestRequestRestartNoHandler(c *C) {
	o, err := overlord.New()
	c.Assert(err, IsNil)

	o.State().RequestRestart()
}

func (ovs *overlordSuite) TestRequestRestartHandler(c *C) {
	o, err := overlord.New()
	c.Assert(err, IsNil)

	restartRequested := false

	o.SetRestartHandler(func() {
		restartRequested = true
	})

	o.State().RequestRestart()

	c.Check(restartRequested, Equals, true)
}
