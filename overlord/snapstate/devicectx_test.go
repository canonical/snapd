// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2019 Canonical Ltd
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

package snapstate_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
)

type deviceCtxSuite struct {
	st *state.State
}

var _ = Suite(&deviceCtxSuite{})

func (s *deviceCtxSuite) SetUpTest(c *C) {
	s.st = state.New(nil)
}

func (s *deviceCtxSuite) TestDevicePastSeedingTooEarly(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	r := snapstatetest.MockDeviceModel(nil)
	defer r()

	expectedErr := &snapstate.ChangeConflictError{
		Message: "too early for operation, device not yet seeded or" +
			" device model not acknowledged",
		ChangeKind: "seed",
	}

	// not seeded, no model assertion
	_, err := snapstate.DevicePastSeeding(s.st, nil)
	c.Assert(err, DeepEquals, expectedErr)

	// seeded, no model assertion
	s.st.Set("seeded", true)
	_, err = snapstate.DevicePastSeeding(s.st, nil)
	c.Assert(err, DeepEquals, expectedErr)
}

func (s *deviceCtxSuite) TestDevicePastSeedingProvided(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	r := snapstatetest.MockDeviceContext(nil)
	defer r()

	expectedErr := &snapstate.ChangeConflictError{
		Message: "too early for operation, device not yet seeded or" +
			" device model not acknowledged",
		ChangeKind: "seed",
	}

	deviceCtx1 := &snapstatetest.TrivialDeviceContext{DeviceModel: MakeModel(nil)}

	// not seeded
	_, err := snapstate.DevicePastSeeding(s.st, deviceCtx1)
	c.Assert(err, DeepEquals, expectedErr)

	// seeded
	s.st.Set("seeded", true)
	deviceCtx, err := snapstate.DevicePastSeeding(s.st, deviceCtx1)
	c.Assert(err, IsNil)
	c.Assert(deviceCtx, Equals, deviceCtx1)

	// remodeling is also ok
	chg := s.st.NewChange("remodel", "test remodeling")
	deviceCtx2 := &snapstatetest.TrivialDeviceContext{DeviceModel: MakeModel(nil), Remodeling: true}
	defer snapstatetest.ReplaceRemodelingHook(func(*state.State) *state.Change {
		return chg
	})()
	deviceCtx, err = snapstate.DevicePastSeeding(s.st, deviceCtx2)
	c.Assert(err, IsNil)
	c.Assert(deviceCtx, Equals, deviceCtx2)

	expectedErr = &snapstate.ChangeConflictError{
		Message: "remodeling in progress, no other " +
			"changes allowed until this is done",
		ChangeKind: "remodel",
		ChangeID:   chg.ID(),
	}

	// should not happen in practice but correct
	deviceCtx, err = snapstate.DevicePastSeeding(s.st, deviceCtx1)
	c.Assert(err, DeepEquals, expectedErr)
	c.Check(deviceCtx, IsNil)
}

func (s *deviceCtxSuite) TestDevicePastSeedingReady(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// seeded and model assertion
	s.st.Set("seeded", true)

	r := snapstatetest.MockDeviceModel(DefaultModel())
	defer r()

	deviceCtx, err := snapstate.DevicePastSeeding(s.st, nil)
	c.Assert(err, IsNil)
	c.Check(deviceCtx.Model().Model(), Equals, "baz-3000")
	c.Check(deviceCtx.SystemMode(), Equals, "run")
}

func (s *deviceCtxSuite) TestDevicePastSeedingReadyInstallMode(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// seeded and model assertion
	s.st.Set("seeded", true)

	r := snapstatetest.MockDeviceModelAndMode(DefaultModel(), "install")
	defer r()

	deviceCtx, err := snapstate.DevicePastSeeding(s.st, nil)
	c.Assert(err, IsNil)
	c.Check(deviceCtx.Model().Model(), Equals, "baz-3000")
	c.Check(deviceCtx.SystemMode(), Equals, "install")
}

func (s *deviceCtxSuite) TestDevicePastSeedingButRemodeling(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// seeded and model assertion
	s.st.Set("seeded", true)

	r := snapstatetest.MockDeviceModel(DefaultModel())
	defer r()

	chg := s.st.NewChange("remodel", "test remodeling")
	defer snapstatetest.ReplaceRemodelingHook(func(*state.State) *state.Change {
		return chg
	})()

	expectedErr := &snapstate.ChangeConflictError{
		Message: "remodeling in progress, no other " +
			"changes allowed until this is done",
		ChangeKind: "remodel",
		ChangeID:   chg.ID(),
	}

	_, err := snapstate.DevicePastSeeding(s.st, nil)
	c.Assert(err, DeepEquals, expectedErr)
}

func (s *deviceCtxSuite) TestDeviceCtxFromStateReady(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// model assertion but not seeded yet
	r := snapstatetest.MockDeviceModel(DefaultModel())
	defer r()

	deviceCtx, err := snapstate.DeviceCtxFromState(s.st, nil)
	c.Assert(err, IsNil)
	c.Check(deviceCtx.Model().Model(), Equals, "baz-3000")
}

func (s *deviceCtxSuite) TestDeviceCtxFromStateProvided(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	r := snapstatetest.MockDeviceContext(nil)
	defer r()

	deviceCtx1 := &snapstatetest.TrivialDeviceContext{DeviceModel: MakeModel(nil)}

	// not seeded
	deviceCtx, err := snapstate.DeviceCtxFromState(s.st, deviceCtx1)
	c.Assert(err, IsNil)
	c.Assert(deviceCtx, Equals, deviceCtx1)

	// seeded
	s.st.Set("seeded", true)
	deviceCtx, err = snapstate.DeviceCtxFromState(s.st, deviceCtx1)
	c.Assert(err, IsNil)
	c.Assert(deviceCtx, Equals, deviceCtx1)
}

func (s *deviceCtxSuite) TestDeviceCtxFromStateTooEarly(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	r := snapstatetest.MockDeviceModel(nil)
	defer r()

	expectedErr := &snapstate.ChangeConflictError{
		Message: "too early for operation, device model " +
			"not yet acknowledged",
		ChangeKind: "seed",
	}

	// not seeded, no model assertion
	_, err := snapstate.DeviceCtxFromState(s.st, nil)
	c.Assert(err, DeepEquals, expectedErr)

	// seeded, no model assertion
	s.st.Set("seeded", true)
	_, err = snapstate.DeviceCtxFromState(s.st, nil)
	c.Assert(err, DeepEquals, expectedErr)
}

func (s *deviceCtxSuite) TestSystemSeeded(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	seeded, err := snapstate.SystemSeeded(s.st)
	c.Assert(err, IsNil)
	c.Check(seeded, Equals, false)

	s.st.Set("seeded", true)
	seeded, err = snapstate.SystemSeeded(s.st)
	c.Assert(err, IsNil)
	c.Check(seeded, Equals, true)
}

func (s *deviceCtxSuite) TestDeviceCtxForEnsureUsesAcknowledgedContext(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	acknowledged := &snapstatetest.TrivialDeviceContext{DeviceModel: DefaultModel()}
	early := &snapstatetest.TrivialDeviceContext{DeviceModel: MakeModel(nil)}
	defer snapstatetest.MockDeviceContext(acknowledged)()
	defer snapstatetest.ReplaceEarlyDeviceCtxForEnsureHook(func(*state.State, bool) (snapstate.DeviceContext, error) {
		return early, nil
	})()

	deviceCtx, err := snapstate.DeviceCtxForEnsure(s.st)
	c.Assert(err, IsNil)
	c.Check(deviceCtx, Equals, acknowledged)
}

func (s *deviceCtxSuite) TestDeviceCtxForEnsureUsesEarlyContextBeforeSeeding(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	early := &snapstatetest.TrivialDeviceContext{DeviceModel: DefaultModel()}
	defer snapstatetest.MockDeviceContext(nil)()
	defer snapstatetest.ReplaceEarlyDeviceCtxForEnsureHook(func(_ *state.State, noModel bool) (snapstate.DeviceContext, error) {
		c.Check(noModel, Equals, true)
		return early, nil
	})()

	deviceCtx, err := snapstate.DeviceCtxForEnsure(s.st)
	c.Assert(err, IsNil)
	c.Check(deviceCtx, Equals, early)
}

func (s *deviceCtxSuite) TestDeviceCtxForEnsureDoesNotUseEarlyContextAfterSeeding(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.st.Set("seeded", true)
	defer snapstatetest.MockDeviceContext(nil)()
	defer snapstatetest.ReplaceEarlyDeviceCtxForEnsureHook(func(*state.State, bool) (snapstate.DeviceContext, error) {
		c.Error("unexpected early context lookup")
		return nil, nil
	})()

	_, err := snapstate.DeviceCtxForEnsure(s.st)
	c.Assert(errors.Is(err, state.ErrNoState), Equals, true)
}

func (s *deviceCtxSuite) TestDeviceCtxForEnsurePropagatesEarlyContextError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	earlyErr := errors.New("cannot load early seed")
	defer snapstatetest.MockDeviceContext(nil)()
	defer snapstatetest.ReplaceEarlyDeviceCtxForEnsureHook(func(*state.State, bool) (snapstate.DeviceContext, error) {
		return nil, earlyErr
	})()

	_, err := snapstate.DeviceCtxForEnsure(s.st)
	c.Check(err, Equals, earlyErr)
}
