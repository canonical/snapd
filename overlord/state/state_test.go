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
	checkpoints [][]byte
	error       func() error
}

func (b *fakeStateBackend) Checkpoint(data []byte) error {
	b.checkpoints = append(b.checkpoints, data)
	if b.error != nil {
		return b.error()
	}
	return nil
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

func (ss *stateSuite) TestNewChangeAndCheckpoint(c *C) {
	b := new(fakeStateBackend)
	st := state.New(b)
	st.Lock()

	chg := st.NewChange("install", "...")
	c.Assert(chg, NotNil)
	chgID := chg.ID()

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
	c.Check(chgs[0].ID(), Equals, chgID)
}
