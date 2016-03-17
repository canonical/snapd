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

package state_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord/state"
)

func TestState(t *testing.T) { TestingT(t) }

type stateSuite struct{}

var _ = Suite(&stateSuite{})

type mgrState1 struct {
	A string
}

type Count2 struct {
	B int
}

type mgrState2 struct {
	C *Count2
}

func (ss *stateSuite) TestLockUnlock(c *C) {
	st := state.New(nil)
	st.Lock()
	st.Unlock()
}

func (ss *stateSuite) TestSetNeedsLocked(c *C) {
	st := state.New(nil)
	mSt1 := &mgrState1{A: "foo"}

	c.Assert(func() { st.Set("mgr1", mSt1) }, PanicMatches, "internal error: accessing state without lock")

	st.Lock()
	defer st.Unlock()
	// fine
	st.Set("mgr1", mSt1)
}

func (ss *stateSuite) TestGetNeedsLocked(c *C) {
	st := state.New(nil)

	var v int
	c.Assert(func() { st.Get("foo", &v) }, PanicMatches, "internal error: accessing state without lock")
}

func (ss *stateSuite) TestGetAndSet(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	mSt1 := &mgrState1{A: "foo"}
	st.Set("mgr1", mSt1)
	mSt2 := &mgrState2{C: &Count2{B: 42}}
	st.Set("mgr2", mSt2)

	var mSt1B mgrState1
	err := st.Get("mgr1", &mSt1B)
	c.Assert(err, IsNil)
	c.Check(&mSt1B, DeepEquals, mSt1)

	var mSt2B mgrState2
	err = st.Get("mgr2", &mSt2B)
	c.Assert(err, IsNil)
	c.Check(&mSt2B, DeepEquals, mSt2)
}

func (ss *stateSuite) TestSetPanic(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	unsupported := struct {
		Ch chan bool
	}{}
	c.Check(func() { st.Set("mgr9", unsupported) }, PanicMatches, `internal error: could not marshal value for state entry "mgr9": json: unsupported type:.*`)
}

func (ss *stateSuite) TestGetNoState(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	var mSt1B mgrState1
	err := st.Get("mgr9", &mSt1B)
	c.Check(err, Equals, state.ErrNoState)
}

func (ss *stateSuite) TestGetUnmarshalProblem(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	mismatched := struct {
		A int
	}{A: 22}
	st.Set("mgr9", &mismatched)

	var mSt1B mgrState1
	err := st.Get("mgr9", &mSt1B)
	c.Check(err, ErrorMatches, `internal error: could not unmarshal state entry "mgr9": json: cannot unmarshal .*`)
}

type fakeStateBackend struct {
	checkpoints  [][]byte
	error        func() error
	ensureBefore time.Duration
}

func (b *fakeStateBackend) Checkpoint(data []byte) error {
	b.checkpoints = append(b.checkpoints, data)
	if b.error != nil {
		return b.error()
	}
	return nil
}

func (b *fakeStateBackend) EnsureBefore(d time.Duration) {
	b.ensureBefore = d
}

func (ss *stateSuite) TestImplicitCheckpointAndRead(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	st.Set("v", 1)
	mSt1 := &mgrState1{A: "foo"}
	st.Set("mgr1", mSt1)
	mSt2 := &mgrState2{C: &Count2{B: 42}}
	st.Set("mgr2", mSt2)

	// implicit checkpoint
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 1)

	buf := bytes.NewBuffer(b.checkpoints[0])

	st2, err := state.ReadState(nil, buf)
	c.Assert(err, IsNil)
	c.Assert(st2, NotNil)

	st2.Lock()
	defer st2.Unlock()

	var v int
	err = st2.Get("v", &v)
	c.Assert(err, IsNil)
	c.Check(v, Equals, 1)

	var mSt1B mgrState1
	err = st2.Get("mgr1", &mSt1B)
	c.Assert(err, IsNil)
	c.Check(&mSt1B, DeepEquals, mSt1)

	var mSt2B mgrState2
	err = st2.Get("mgr2", &mSt2B)
	c.Assert(err, IsNil)
	c.Check(&mSt2B, DeepEquals, mSt2)
}

func (ss *stateSuite) TestImplicitCheckpointRetry(c *C) {
	prevInterval, prevMaxTime := state.ChangeUnlockCheckpointRetryParamsForTest(
		2*time.Millisecond,
		1*time.Second,
	)
	defer state.ChangeUnlockCheckpointRetryParamsForTest(prevInterval, prevMaxTime)

	retries := 0
	boom := errors.New("boom")
	error := func() error {
		retries++
		if retries == 2 {
			return nil
		}
		return boom
	}
	b := &fakeStateBackend{error: error}
	st := state.New(b)
	st.Lock()

	// implicit checkpoint will retry
	st.Unlock()

	c.Check(retries, Equals, 2)
}

func (ss *stateSuite) TestImplicitCheckpointPanicsAfterFailedRetries(c *C) {
	prevInterval, prevMaxTime := state.ChangeUnlockCheckpointRetryParamsForTest(
		2*time.Millisecond,
		10*time.Millisecond,
	)
	defer state.ChangeUnlockCheckpointRetryParamsForTest(prevInterval, prevMaxTime)

	boom := errors.New("boom")
	retries := 0
	error := func() error {
		retries++
		return boom
	}
	b := &fakeStateBackend{error: error}
	st := state.New(b)
	st.Lock()

	// implicit checkpoint will panic after all failed retries
	t0 := time.Now()
	c.Check(func() { st.Unlock() }, PanicMatches, "cannot checkpoint even after 10ms of retries every 2ms: boom")
	// we did at least a couple
	c.Check(retries > 2, Equals, true)
	c.Check(time.Since(t0) > 10*time.Millisecond, Equals, true)
}

func (ss *stateSuite) TestNewChangeAndChanges(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg1 := st.NewChange("install", "...")
	chg2 := st.NewChange("remove", "...")

	chgs := st.Changes()
	c.Check(chgs, HasLen, 2)

	expected := map[string]*state.Change{
		chg1.ID(): chg1,
		chg2.ID(): chg2,
	}

	for _, chg := range chgs {
		c.Check(chg, Equals, expected[chg.ID()])
	}
}

func (ss *stateSuite) TestNewChangeNeedsLocked(c *C) {
	st := state.New(nil)

	c.Assert(func() { st.NewChange("install", "...") }, PanicMatches, "internal error: accessing state without lock")
}

func (ss *stateSuite) TestChangesNeedsLocked(c *C) {
	st := state.New(nil)

	c.Assert(func() { st.Changes() }, PanicMatches, "internal error: accessing state without lock")
}

func (ss *stateSuite) TestNewChangeAndCheckpoint(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	chg := st.NewChange("install", "summary")
	c.Assert(chg, NotNil)
	chgID := chg.ID()
	chg.Set("a", 1)
	chg.SetStatus(state.ErrorStatus)

	// implicit checkpoint
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 1)

	buf := bytes.NewBuffer(b.checkpoints[0])

	st2, err := state.ReadState(nil, buf)
	c.Assert(err, IsNil)
	c.Assert(st2, NotNil)

	st2.Lock()
	defer st2.Unlock()

	chgs := st2.Changes()

	c.Assert(chgs, HasLen, 1)

	chg0 := chgs[0]
	c.Check(chg0.ID(), Equals, chgID)
	c.Check(chg0.Kind(), Equals, "install")
	c.Check(chg0.Summary(), Equals, "summary")

	var v int
	err = chg0.Get("a", &v)
	c.Check(v, Equals, 1)

	c.Check(chg0.Status(), Equals, state.ErrorStatus)
}

func (ss *stateSuite) TestNewTaskAndCheckpoint(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	chg := st.NewChange("install", "summary")
	c.Assert(chg, NotNil)

	t1 := chg.NewTask("download", "1...")
	t1ID := t1.ID()
	t1.Set("a", 1)
	t1.SetStatus(state.WaitingStatus)
	t1.SetProgress(5, 10)

	t2 := chg.NewTask("inst", "2...")
	t2ID := t2.ID()
	t2.WaitFor(t1)

	// implicit checkpoint
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 1)

	buf := bytes.NewBuffer(b.checkpoints[0])

	st2, err := state.ReadState(nil, buf)
	c.Assert(err, IsNil)
	c.Assert(st2, NotNil)

	st2.Lock()
	defer st2.Unlock()

	chgs := st2.Changes()
	c.Assert(chgs, HasLen, 1)
	chg0 := chgs[0]

	tasks0 := make(map[string]*state.Task)
	for _, t := range chg0.Tasks() {
		tasks0[t.ID()] = t
	}
	c.Assert(tasks0, HasLen, 2)

	task0_1 := tasks0[t1ID]
	c.Check(task0_1.ID(), Equals, t1ID)
	c.Check(task0_1.Kind(), Equals, "download")
	c.Check(task0_1.Summary(), Equals, "1...")

	var v int
	err = task0_1.Get("a", &v)
	c.Check(v, Equals, 1)

	c.Check(task0_1.Status(), Equals, state.WaitingStatus)

	cur, tot := task0_1.Progress()
	c.Check(cur, Equals, 5)
	c.Check(tot, Equals, 10)

	task0_2 := tasks0[t2ID]
	c.Check(task0_2.WaitTasks(), DeepEquals, []*state.Task{task0_1})
}

func (ss *stateSuite) TestEnsureBefore(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)

	st.EnsureBefore(10 * time.Second)

	c.Check(b.ensureBefore, Equals, 10*time.Second)
}

func (ss *stateSuite) TestTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg1 := st.NewChange("install", "...")
	t11 := chg1.NewTask("check", "...")
	t12 := chg1.NewTask("inst", "...")
	chg2 := st.NewChange("remove", "...")
	t21 := chg2.NewTask("check", "...")
	t22 := chg2.NewTask("rm", "...")

	tasks := st.Tasks()
	c.Check(tasks, HasLen, 4)

	expected := map[string]*state.Task{
		t11.ID(): t11,
		t12.ID(): t12,
		t21.ID(): t21,
		t22.ID(): t22,
	}

	for _, t := range tasks {
		c.Check(t, Equals, expected[t.ID()])
	}
}

func (ss *stateSuite) TestTasksNeedsLocked(c *C) {
	st := state.New(nil)

	c.Assert(func() { st.Tasks() }, PanicMatches, "internal error: accessing state without lock")
}
