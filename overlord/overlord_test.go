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
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/testutil"

	"github.com/ubuntu-core/snappy/overlord"
	"github.com/ubuntu-core/snappy/overlord/state"
)

func TestOverlord(t *testing.T) { TestingT(t) }

type overlordSuite struct{}

var _ = Suite(&overlordSuite{})

func (ovs *overlordSuite) SetUpTest(c *C) {
	dirs.SnapStateFile = filepath.Join(c.MkDir(), "test.json")
}

func (ovs *overlordSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (ovs *overlordSuite) TestNew(c *C) {
	o, err := overlord.New()
	c.Assert(err, IsNil)
	c.Check(o, NotNil)

	c.Check(o.SnapManager(), NotNil)
	c.Check(o.AssertManager(), NotNil)
	c.Check(o.InterfaceManager(), NotNil)

	c.Check(o.StateEngine(), NotNil)

	c.Check(o.StateEngine().State(), NotNil)
}

func (ovs *overlordSuite) TestNewWithGoodState(c *C) {
	fakeState := []byte(`{"data":{"some":"data"},"changes":null,"tasks":null}`)
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	o, err := overlord.New()

	c.Assert(err, IsNil)
	state := o.StateEngine().State()
	c.Assert(err, IsNil)
	state.Lock()
	defer state.Unlock()

	d, err := state.MarshalJSON()
	c.Assert(err, IsNil)
	c.Assert(string(d), DeepEquals, string(fakeState))
}

func (ovs *overlordSuite) TestNewWithInvalidState(c *C) {
	fakeState := []byte(``)
	err := ioutil.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, IsNil)

	_, err = overlord.New()
	c.Assert(err, ErrorMatches, "EOF")
}

type witnessManager struct {
	state          *state.State
	expectedEnsure int
	ensureCalled   chan struct{}
	ensureCallack  func(s *state.State) error
}

func (wm *witnessManager) Ensure() error {
	if wm.expectedEnsure--; wm.expectedEnsure == 0 {
		close(wm.ensureCalled)
		return nil
	}
	if wm.ensureCallack != nil {
		return wm.ensureCallack(wm.state)
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

	o.Run()

	err = o.Stop()
	c.Assert(err, IsNil)
}

func (ovs *overlordSuite) TestEnsureLoopRunAndStop(c *C) {
	restoreIntv := overlord.SetEnsureIntervalForTest(10 * time.Millisecond)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	witness := &witnessManager{
		state:          o.StateEngine().State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
	}
	o.StateEngine().AddManager(witness)

	o.Run()
	defer o.Stop()

	t0 := time.Now()
	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
	c.Check(time.Since(t0) >= 20*time.Millisecond, Equals, true)

	err = o.Stop()
	c.Assert(err, IsNil)
}

func (ovs *overlordSuite) TestEnsureLoopMediatedEnsureBefore(c *C) {
	restoreIntv := overlord.SetEnsureIntervalForTest(10 * time.Minute)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	witness := &witnessManager{
		state:          o.StateEngine().State(),
		expectedEnsure: 1,
		ensureCalled:   make(chan struct{}),
	}
	se := o.StateEngine()
	se.AddManager(witness)

	o.Run()
	defer o.Stop()

	se.State().EnsureBefore(10 * time.Millisecond)

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (ovs *overlordSuite) TestEnsureLoopMediatedEnsureBeforeInEnsure(c *C) {
	restoreIntv := overlord.SetEnsureIntervalForTest(10 * time.Minute)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	ensure := func(s *state.State) error {
		s.EnsureBefore(0)
		return nil
	}

	witness := &witnessManager{
		state:          o.StateEngine().State(),
		expectedEnsure: 2,
		ensureCalled:   make(chan struct{}),
		ensureCallack:  ensure,
	}
	se := o.StateEngine()
	se.AddManager(witness)

	o.Run()
	defer o.Stop()

	se.State().EnsureBefore(0)

	select {
	case <-witness.ensureCalled:
	case <-time.After(2 * time.Second):
		c.Fatal("Ensure calls not happening")
	}
}

func (ovs *overlordSuite) TestCheckpoint(c *C) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	o, err := overlord.New()
	c.Assert(err, IsNil)

	_, err = os.Stat(dirs.SnapStateFile)
	c.Check(os.IsNotExist(err), Equals, true)

	s := o.StateEngine().State()
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
	})
	rm.runner.AddHandler("runMgr2", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.Set("runMgr2Mark", 1)
		return nil
	})
	rm.runner.AddHandler("runMgrEnsureBefore", func(t *state.Task, _ *tomb.Tomb) error {
		s := t.State()
		s.Lock()
		defer s.Unlock()
		s.EnsureBefore(20 * time.Millisecond)
		return nil
	})

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
	restoreIntv := overlord.SetEnsureIntervalForTest(1 * time.Minute)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	se := o.StateEngine()
	s := se.State()
	rm1 := newRunnerManager(s)
	se.AddManager(rm1)

	o.Run()
	defer o.Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t1 := chg.NewTask("runMgr1", "1...")

	s.Unlock()

	o.Settle()

	s.Lock()
	c.Check(t1.Status(), Equals, state.DoneStatus)

	var v int
	err = s.Get("runMgr1Mark", &v)
	c.Check(err, IsNil)
}

func (ovs *overlordSuite) TestSettleChain(c *C) {
	restoreIntv := overlord.SetEnsureIntervalForTest(1 * time.Minute)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	se := o.StateEngine()
	s := se.State()
	rm1 := newRunnerManager(s)
	se.AddManager(rm1)

	o.Run()
	defer o.Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t1 := chg.NewTask("runMgr1", "1...")
	t2 := chg.NewTask("runMgr2", "2...")
	t2.WaitFor(t1)

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

func (ovs *overlordSuite) TestExplicitEnsureBefore(c *C) {
	restoreIntv := overlord.SetEnsureIntervalForTest(1 * time.Minute)
	defer restoreIntv()
	o, err := overlord.New()
	c.Assert(err, IsNil)

	se := o.StateEngine()
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

	o.Run()
	defer o.Stop()

	s.Lock()
	defer s.Unlock()

	chg := s.NewChange("chg", "...")
	t := chg.NewTask("runMgrEnsureBefore", "...")
	s.Unlock()

	o.Settle()

	s.Lock()
	c.Check(t.Status(), Equals, state.DoneStatus)

	var v int
	err = s.Get("ensureCount", &v)
	c.Check(err, IsNil)
	c.Check(v, Equals, 2)
}
