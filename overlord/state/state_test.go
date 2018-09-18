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
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
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

func (ss *stateSuite) TestSetToNilDeletes(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.Set("a", map[string]int{"a": 1})
	var v map[string]int
	err := st.Get("a", &v)
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)

	st.Set("a", nil)

	var v1 map[string]int
	err = st.Get("a", &v1)
	c.Check(err, Equals, state.ErrNoState)
	c.Check(v1, HasLen, 0)
}

func (ss *stateSuite) TestNullMeansNoState(c *C) {
	buf := bytes.NewBufferString(`{"data": {"a": null}}`)
	st, err := state.ReadState(nil, buf)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var v1 map[string]int
	err = st.Get("a", &v1)
	c.Check(err, Equals, state.ErrNoState)
	c.Check(v1, HasLen, 0)
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

func (ss *stateSuite) TestCache(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	type key1 struct{}
	type key2 struct{}

	c.Assert(st.Cached(key1{}), Equals, nil)

	st.Cache(key1{}, "value1")
	st.Cache(key2{}, "value2")
	c.Assert(st.Cached(key1{}), Equals, "value1")
	c.Assert(st.Cached(key2{}), Equals, "value2")

	st.Cache(key1{}, nil)
	c.Assert(st.Cached(key1{}), Equals, nil)

	_, ok := st.Cached("key3").(string)
	c.Assert(ok, Equals, false)
}

type fakeStateBackend struct {
	checkpoints      [][]byte
	error            func() error
	ensureBefore     time.Duration
	restartRequested bool
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

func (b *fakeStateBackend) RequestRestart(t state.RestartType) {
	b.restartRequested = true
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
	c.Assert(st2.Modified(), Equals, false)

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
	restore := state.MockCheckpointRetryDelay(2*time.Millisecond, 1*time.Second)
	defer restore()

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
	restore := state.MockCheckpointRetryDelay(2*time.Millisecond, 80*time.Millisecond)
	defer restore()

	boom := errors.New("boom")
	retries := 0
	errFn := func() error {
		retries++
		return boom
	}
	b := &fakeStateBackend{error: errFn}
	st := state.New(b)
	st.Lock()

	// implicit checkpoint will panic after all failed retries
	t0 := time.Now()
	c.Check(func() { st.Unlock() }, PanicMatches, "cannot checkpoint even after 80ms of retries every 2ms: boom")
	// we did at least a couple
	c.Check(retries > 2, Equals, true, Commentf("expected more than 2 retries got %v", retries))
	c.Check(time.Since(t0) > 80*time.Millisecond, Equals, true)
}

func (ss *stateSuite) TestImplicitCheckpointModifiedOnly(c *C) {
	restore := state.MockCheckpointRetryDelay(2*time.Millisecond, 1*time.Second)
	defer restore()

	b := &fakeStateBackend{}
	st := state.New(b)
	st.Lock()
	st.Unlock()
	st.Lock()
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 1)

	st.Lock()
	st.Set("foo", "bar")
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 2)
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
		c.Check(st.Change(chg.ID()), Equals, chg)
	}

	c.Check(st.Change("no-such-id"), IsNil)
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

	spawnTime := chg.SpawnTime()
	readyTime := chg.ReadyTime()

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
	c.Check(chg0.SpawnTime().Equal(spawnTime), Equals, true)
	c.Check(chg0.ReadyTime().Equal(readyTime), Equals, true)

	var v int
	err = chg0.Get("a", &v)
	c.Check(err, IsNil)
	c.Check(v, Equals, 1)

	c.Check(chg0.Status(), Equals, state.ErrorStatus)

	select {
	case <-chg0.Ready():
	default:
		c.Errorf("Change didn't preserve Ready channel closed after deserialization")
	}
}

func (ss *stateSuite) TestNewChangeAndCheckpointTaskDerivedStatus(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	chg := st.NewChange("install", "summary")
	c.Assert(chg, NotNil)
	chgID := chg.ID()

	t1 := st.NewTask("download", "1...")
	t1.SetStatus(state.DoneStatus)
	chg.AddTask(t1)

	// implicit checkpoint
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 1)
	buf := bytes.NewBuffer(b.checkpoints[0])

	st2, err := state.ReadState(nil, buf)
	c.Assert(err, IsNil)

	st2.Lock()
	defer st2.Unlock()

	chgs := st2.Changes()

	c.Assert(chgs, HasLen, 1)

	chg0 := chgs[0]
	c.Check(chg0.ID(), Equals, chgID)
	c.Check(chg0.Status(), Equals, state.DoneStatus)

	select {
	case <-chg0.Ready():
	default:
		c.Errorf("Change didn't preserve Ready channel closed after deserialization")
	}
}

func (ss *stateSuite) TestNewTaskAndCheckpoint(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	chg := st.NewChange("install", "summary")
	c.Assert(chg, NotNil)

	t1 := st.NewTask("download", "1...")
	chg.AddTask(t1)
	t1ID := t1.ID()
	t1.Set("a", 1)
	t1.SetStatus(state.DoneStatus)
	t1.SetProgress("snap", 5, 10)
	t1.JoinLane(42)
	t1.JoinLane(43)

	t2 := st.NewTask("inst", "2...")
	chg.AddTask(t2)
	t2ID := t2.ID()
	t2.WaitFor(t1)
	schedule := time.Now().Add(time.Hour)
	t2.At(schedule)

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
	c.Check(task0_1.Change(), Equals, chg0)

	var v int
	err = task0_1.Get("a", &v)
	c.Check(err, IsNil)
	c.Check(v, Equals, 1)

	c.Check(task0_1.Status(), Equals, state.DoneStatus)

	_, cur, tot := task0_1.Progress()
	c.Check(cur, Equals, 5)
	c.Check(tot, Equals, 10)

	c.Assert(task0_1.Lanes(), DeepEquals, []int{42, 43})

	task0_2 := tasks0[t2ID]
	c.Check(task0_2.WaitTasks(), DeepEquals, []*state.Task{task0_1})

	c.Check(task0_1.HaltTasks(), DeepEquals, []*state.Task{task0_2})

	tasks2 := make(map[string]*state.Task)
	for _, t := range st2.Tasks() {
		tasks2[t.ID()] = t
	}
	c.Assert(tasks2, HasLen, 2)

	c.Check(task0_1.AtTime().IsZero(), Equals, true)
	c.Check(task0_2.AtTime().Equal(schedule), Equals, true)
}

func (ss *stateSuite) TestEmptyStateDataAndCheckpointReadAndSet(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	chg := st.NewChange("install", "summary")
	c.Assert(chg, NotNil)

	// implicit checkpoint
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 1)

	buf := bytes.NewBuffer(b.checkpoints[0])

	st2, err := state.ReadState(nil, buf)
	c.Assert(err, IsNil)
	c.Assert(st2, NotNil)

	st2.Lock()
	defer st2.Unlock()

	// no crash
	st2.Set("a", 1)
}

func (ss *stateSuite) TestEmptyTaskAndChangeDataAndCheckpointReadAndSet(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	t1 := st.NewTask("1...", "...")
	t1ID := t1.ID()
	chg := st.NewChange("chg", "...")
	chgID := chg.ID()
	chg.AddTask(t1)

	// implicit checkpoint
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 1)

	buf := bytes.NewBuffer(b.checkpoints[0])

	st2, err := state.ReadState(nil, buf)
	c.Assert(err, IsNil)
	c.Assert(st2, NotNil)

	st2.Lock()
	defer st2.Unlock()

	chg2 := st2.Change(chgID)
	t1_2 := st2.Task(t1ID)
	c.Assert(t1_2, NotNil)

	// no crash
	chg2.Set("c", 1)
	// no crash either
	t1_2.Set("t", 1)
}

func (ss *stateSuite) TestEnsureBefore(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)

	st.EnsureBefore(10 * time.Second)

	c.Check(b.ensureBefore, Equals, 10*time.Second)
}

func (ss *stateSuite) TestCheckpointPreserveLastIds(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	st.NewChange("install", "...")
	st.NewTask("download", "...")
	st.NewTask("download", "...")

	c.Assert(st.NewLane(), Equals, 1)

	// implicit checkpoint
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 1)

	buf := bytes.NewBuffer(b.checkpoints[0])

	st2, err := state.ReadState(nil, buf)
	c.Assert(err, IsNil)

	st2.Lock()
	defer st2.Unlock()

	c.Assert(st2.NewTask("download", "...").ID(), Equals, "3")
	c.Assert(st2.NewChange("install", "...").ID(), Equals, "2")

	c.Assert(st2.NewLane(), Equals, 2)

}

func (ss *stateSuite) TestCheckpointPreserveCleanStatus(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	chg := st.NewChange("install", "...")
	t := st.NewTask("download", "...")
	chg.AddTask(t)
	t.SetStatus(state.DoneStatus)
	t.SetClean()

	// implicit checkpoint
	st.Unlock()

	c.Assert(b.checkpoints, HasLen, 1)

	buf := bytes.NewBuffer(b.checkpoints[0])

	st2, err := state.ReadState(nil, buf)
	c.Assert(err, IsNil)

	st2.Lock()
	defer st2.Unlock()

	chg2 := st2.Change(chg.ID())
	t2 := st2.Task(t.ID())

	c.Assert(chg2.IsClean(), Equals, true)
	c.Assert(t2.IsClean(), Equals, true)
}

func (ss *stateSuite) TestNewTaskAndTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg1 := st.NewChange("install", "...")
	t11 := st.NewTask("check", "...")
	chg1.AddTask(t11)
	t12 := st.NewTask("inst", "...")
	chg1.AddTask(t12)

	chg2 := st.NewChange("remove", "...")
	t21 := st.NewTask("check", "...")
	t22 := st.NewTask("rm", "...")
	chg2.AddTask(t21)
	chg2.AddTask(t22)

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

func (ss *stateSuite) TestTaskNoTask(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	c.Check(st.Task("1"), IsNil)
}

func (ss *stateSuite) TestNewTaskHiddenUntilLinked(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("check", "...")

	tasks := st.Tasks()
	c.Check(tasks, HasLen, 0)

	c.Check(st.Task(t1.ID()), IsNil)
}

func (ss *stateSuite) TestMethodEntrance(c *C) {
	st := state.New(&fakeStateBackend{})

	// Reset modified flag.
	st.Lock()
	st.Unlock()

	writes := []func(){
		func() { st.Set("foo", 1) },
		func() { st.NewChange("install", "...") },
		func() { st.NewTask("download", "...") },
		func() { st.UnmarshalJSON(nil) },
		func() { st.NewLane() },
	}

	reads := []func(){
		func() { st.Get("foo", nil) },
		func() { st.Cached("foo") },
		func() { st.Cache("foo", 1) },
		func() { st.Changes() },
		func() { st.Change("foo") },
		func() { st.Tasks() },
		func() { st.Task("foo") },
		func() { st.MarshalJSON() },
		func() { st.Prune(time.Hour, time.Hour, 100) },
		func() { st.TaskCount() },
	}

	for i, f := range reads {
		c.Logf("Testing read function #%d", i)
		c.Assert(f, PanicMatches, "internal error: accessing state without lock")
		c.Assert(st.Modified(), Equals, false)
	}

	for i, f := range writes {
		st.Lock()
		st.Unlock()
		c.Assert(st.Modified(), Equals, false)

		c.Logf("Testing write function #%d", i)
		c.Assert(f, PanicMatches, "internal error: accessing state without lock")
		c.Assert(st.Modified(), Equals, true)
	}
}

func (ss *stateSuite) TestPrune(c *C) {
	st := state.New(&fakeStateBackend{})
	st.Lock()
	defer st.Unlock()

	now := time.Now()
	pruneWait := 1 * time.Hour
	abortWait := 3 * time.Hour

	unset := time.Time{}

	t1 := st.NewTask("foo", "...")
	t2 := st.NewTask("foo", "...")
	t3 := st.NewTask("foo", "...")
	t4 := st.NewTask("foo", "...")

	chg1 := st.NewChange("abort", "...")
	chg1.AddTask(t1)
	state.MockChangeTimes(chg1, now.Add(-abortWait), unset)

	chg2 := st.NewChange("prune", "...")
	chg2.AddTask(t2)
	c.Assert(chg2.Status(), Equals, state.DoStatus)
	state.MockChangeTimes(chg2, now.Add(-pruneWait), now.Add(-pruneWait))

	chg3 := st.NewChange("ready-but-recent", "...")
	chg3.AddTask(t3)
	state.MockChangeTimes(chg3, now.Add(-pruneWait), now.Add(-pruneWait/2))

	chg4 := st.NewChange("old-but-not-ready", "...")
	chg4.AddTask(t4)
	state.MockChangeTimes(chg4, now.Add(-pruneWait/2), unset)

	// unlinked task
	t5 := st.NewTask("unliked", "...")
	c.Check(st.Task(t5.ID()), IsNil)
	state.MockTaskTimes(t5, now.Add(-pruneWait), now.Add(-pruneWait))

	st.Prune(pruneWait, abortWait, 100)

	c.Assert(st.Change(chg1.ID()), Equals, chg1)
	c.Assert(st.Change(chg2.ID()), IsNil)
	c.Assert(st.Change(chg3.ID()), Equals, chg3)
	c.Assert(st.Change(chg4.ID()), Equals, chg4)

	c.Assert(st.Task(t1.ID()), Equals, t1)
	c.Assert(st.Task(t2.ID()), IsNil)
	c.Assert(st.Task(t3.ID()), Equals, t3)
	c.Assert(st.Task(t4.ID()), Equals, t4)

	c.Assert(chg1.Status(), Equals, state.HoldStatus)
	c.Assert(chg3.Status(), Equals, state.DoStatus)
	c.Assert(chg4.Status(), Equals, state.DoStatus)

	c.Assert(t1.Status(), Equals, state.HoldStatus)
	c.Assert(t3.Status(), Equals, state.DoStatus)
	c.Assert(t4.Status(), Equals, state.DoStatus)

	c.Check(st.TaskCount(), Equals, 3)
}

func (ss *stateSuite) TestPruneEmptyChange(c *C) {
	// Empty changes are a bit special because they start out on Hold
	// which is a Ready status, but the change itself is not considered Ready
	// explicitly because that's how every change that will have tasks added
	// to it starts their life.
	st := state.New(&fakeStateBackend{})
	st.Lock()
	defer st.Unlock()

	now := time.Now()
	pruneWait := 1 * time.Hour
	abortWait := 3 * time.Hour

	chg := st.NewChange("abort", "...")
	state.MockChangeTimes(chg, now.Add(-pruneWait), time.Time{})

	st.Prune(pruneWait, abortWait, 100)
	c.Assert(st.Change(chg.ID()), IsNil)
}

func (ss *stateSuite) TestPruneMaxChangesHappy(c *C) {
	st := state.New(&fakeStateBackend{})
	st.Lock()
	defer st.Unlock()

	now := time.Now()
	pruneWait := 1 * time.Hour
	abortWait := 3 * time.Hour

	// create 10 changes, chg0 is freshest, chg9 is oldest, but
	// all changes are not old enough for pruneWait
	for i := 0; i < 10; i++ {
		chg := st.NewChange(fmt.Sprintf("chg%d", i), "...")
		t := st.NewTask("foo", "...")
		chg.AddTask(t)
		t.SetStatus(state.DoneStatus)

		when := time.Duration(i) * time.Second
		state.MockChangeTimes(chg, now.Add(-when), now.Add(-when))
	}
	c.Assert(st.Changes(), HasLen, 10)

	// and 5 more, all not ready
	for i := 10; i < 15; i++ {
		chg := st.NewChange(fmt.Sprintf("chg%d", i), "...")
		t := st.NewTask("foo", "...")
		chg.AddTask(t)
	}

	// test that nothing is done when we are within pruneWait and
	// maxReadyChanges
	maxReadyChanges := 100
	st.Prune(pruneWait, abortWait, maxReadyChanges)
	c.Assert(st.Changes(), HasLen, 15)

	// but with maxReadyChanges we remove the ready ones
	maxReadyChanges = 5
	st.Prune(pruneWait, abortWait, maxReadyChanges)
	c.Assert(st.Changes(), HasLen, 10)
	remaining := map[string]bool{}
	for _, chg := range st.Changes() {
		remaining[chg.Kind()] = true
	}
	c.Check(remaining, DeepEquals, map[string]bool{
		// ready and fresh
		"chg0": true,
		"chg1": true,
		"chg2": true,
		"chg3": true,
		"chg4": true,
		// not ready
		"chg10": true,
		"chg11": true,
		"chg12": true,
		"chg13": true,
		"chg14": true,
	})
}

func (ss *stateSuite) TestPruneMaxChangesSomeNotReady(c *C) {
	st := state.New(&fakeStateBackend{})
	st.Lock()
	defer st.Unlock()

	// 10 changes, none ready
	for i := 0; i < 10; i++ {
		chg := st.NewChange(fmt.Sprintf("chg%d", i), "...")
		t := st.NewTask("foo", "...")
		chg.AddTask(t)
	}
	c.Assert(st.Changes(), HasLen, 10)

	// nothing can be pruned
	maxChanges := 5
	st.Prune(1*time.Hour, 3*time.Hour, maxChanges)
	c.Assert(st.Changes(), HasLen, 10)
}

func (ss *stateSuite) TestPruneMaxChangesHonored(c *C) {
	st := state.New(&fakeStateBackend{})
	st.Lock()
	defer st.Unlock()

	// 10 changes, none ready
	for i := 0; i < 10; i++ {
		chg := st.NewChange(fmt.Sprintf("chg%d", i), "not-ready")
		t := st.NewTask("foo", "not-readly")
		chg.AddTask(t)
	}
	c.Assert(st.Changes(), HasLen, 10)

	// one extra change that just now entered ready state
	chg := st.NewChange(fmt.Sprintf("chg99"), "so-ready")
	t := st.NewTask("foo", "so-ready")
	when := 1 * time.Second
	state.MockChangeTimes(chg, time.Now().Add(-when), time.Now().Add(-when))
	t.SetStatus(state.DoneStatus)
	chg.AddTask(t)

	// we have 11 changes in total, 10 not-ready, 1 ready
	//
	// this test we do not purge the freshly ready change
	maxChanges := 10
	st.Prune(1*time.Hour, 3*time.Hour, maxChanges)
	c.Assert(st.Changes(), HasLen, 11)
}

func (ss *stateSuite) TestRequestRestart(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)

	ok, t := st.Restarting()
	c.Check(ok, Equals, false)
	c.Check(t, Equals, state.RestartUnset)

	st.RequestRestart(state.RestartDaemon)

	c.Check(b.restartRequested, Equals, true)

	ok, t = st.Restarting()
	c.Check(ok, Equals, true)
	c.Check(t, Equals, state.RestartDaemon)
}

func (ss *stateSuite) TestReadStateInitsCache(c *C) {
	st, err := state.ReadState(nil, bytes.NewBufferString("{}"))
	c.Assert(err, IsNil)
	st.Lock()
	defer st.Unlock()

	st.Cache("key", "value")
	c.Assert(st.Cached("key"), Equals, "value")
}
