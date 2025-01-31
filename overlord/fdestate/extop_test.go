// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2024 Canonical Ltd
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

package fdestate_test

import (
	"encoding/json"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type extopSuite struct {
	testutil.BaseTest

	state *state.State
}

var _ = Suite(&extopSuite{})

func (s *extopSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.state = state.New(nil)
}

func (s *extopSuite) TestExternalOperationInState(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()
	s.state.Set("fde", fdestate.FdeState{})

	opA := &fdestate.ExternalOperation{
		Kind:     "fde-efi-secureboot-db-update",
		ChangeID: "123",
		Context:  json.RawMessage(`{"foo":"bar"}`),
		Status:   fdestate.PreparingStatus,
	}

	opB := &fdestate.ExternalOperation{
		Kind:     "other-op",
		ChangeID: "999",
		Context:  json.RawMessage(`{"no":"yes"}`),
		Status:   fdestate.DoneStatus,
	}

	opC := &fdestate.ExternalOperation{
		Kind:     "fde-efi-secureboot-db-update",
		ChangeID: "444",
		Context:  json.RawMessage(`{"data":"b"}`),
		Status:   fdestate.DoingStatus,
	}

	c.Assert(fdestate.AddExternalOperation(st, opA), IsNil)
	c.Assert(fdestate.AddExternalOperation(st, opB), IsNil)
	c.Assert(fdestate.AddExternalOperation(st, opC), IsNil)

	op, err := fdestate.FindFirstExternalOperationByChangeID(st, "111")
	c.Assert(err, IsNil)
	c.Check(op, IsNil)

	op, err = fdestate.FindFirstExternalOperationByChangeID(st, "123")
	c.Assert(err, IsNil)
	c.Check(op, DeepEquals, opA)

	op, err = fdestate.FindFirstExternalOperationByChangeID(st, "999")
	c.Assert(err, IsNil)
	c.Check(op, DeepEquals, opB)

	// finds the first operation, which is opA
	op, err = fdestate.FindFirstPendingExternalOperationByKind(st, "fde-efi-secureboot-db-update")
	c.Assert(err, IsNil)
	c.Check(op, DeepEquals, opA)

	// opB has DoneStatus, no it's no longer 'pending'
	op, err = fdestate.FindFirstPendingExternalOperationByKind(st, "other-op")
	c.Assert(err, IsNil)
	c.Check(op, IsNil)

	opA_UpdateBad := &fdestate.ExternalOperation{
		Kind: "fde-efi-secureboot-db-update",
		// change ID which does not match opA
		ChangeID: "111",
		Context:  json.RawMessage(`{"no":"yes"}`),
		Status:   fdestate.DoneStatus,
	}
	// accidental update when no matching operation exists raises an error
	c.Assert(fdestate.UpdateExternalOperation(st, opA_UpdateBad), ErrorMatches, "no matching operation")

	opA_UpdateGood := &fdestate.ExternalOperation{
		Kind: "fde-efi-secureboot-db-update",
		// change ID which does not match opA
		ChangeID: "123",
		Context:  json.RawMessage(`{"no":"yes"}`),
		Status:   fdestate.DoneStatus,
	}
	c.Check(opA.Equal(opA_UpdateGood), Equals, true)
	c.Assert(fdestate.UpdateExternalOperation(st, opA_UpdateGood), IsNil)

	// finds the first pending operation, which at this point is opC, as opA has
	// DoneStatus
	op, err = fdestate.FindFirstPendingExternalOperationByKind(st, "fde-efi-secureboot-db-update")
	c.Assert(err, IsNil)
	c.Check(op, DeepEquals, opC)
}

func (s *extopSuite) TestStringifiedStatus(c *C) {
	for _, tc := range []struct {
		status fdestate.ExternalOperationStatus
		desc   string
	}{
		{status: fdestate.DefaultStatus, desc: "default"},
		{status: fdestate.DoingStatus, desc: "doing"},
		{status: fdestate.PreparingStatus, desc: "preparing"},
		{status: fdestate.CompletingStatus, desc: "completing"},
		{status: fdestate.AbortingStatus, desc: "aborting"},
		{status: fdestate.DoneStatus, desc: "done"},
		{status: fdestate.ErrorStatus, desc: "error"},
	} {
		c.Check(tc.status.String(), Equals, tc.desc)
	}
}

func (s *extopSuite) TestExternalOperationStatus(c *C) {
	for _, tc := range []struct {
		status fdestate.ExternalOperationStatus
		ready  bool
		desc   string
	}{
		{status: fdestate.DefaultStatus},
		{status: fdestate.DoingStatus},
		{status: fdestate.PreparingStatus},
		{status: fdestate.CompletingStatus},
		{status: fdestate.AbortingStatus},
		{status: fdestate.DoneStatus, ready: true},
		{status: fdestate.ErrorStatus, ready: true},
	} {

		op := fdestate.ExternalOperation{}
		if tc.status != fdestate.DefaultStatus {
			op.SetStatus(tc.status)
		}
		c.Check(op.Status, Equals, tc.status)
		c.Check(op.IsReady(), Equals, tc.ready)
	}

	op := fdestate.ExternalOperation{
		Status: fdestate.PreparingStatus,
	}
	op.SetFailed("this is an error")
	c.Check(op.Status, Equals, fdestate.ErrorStatus)
	c.Check(op.Err, Equals, "this is an error")
	c.Check(op.IsReady(), Equals, true)
}

func (s *extopSuite) TestExternalOperationEqual(c *C) {
	opA := fdestate.ExternalOperation{
		Kind:     "fde-efi-secureboot-db-update",
		ChangeID: "123",
		Context:  json.RawMessage(`{"foo":"bar"}`),
		Status:   fdestate.PreparingStatus,
	}

	opB := fdestate.ExternalOperation{
		Kind:     "fde-efi-secureboot-db-update",
		ChangeID: "123",
		Context:  json.RawMessage(`{"different":"content"}`),
		Status:   fdestate.DoneStatus,
	}

	opC_OtherKind := fdestate.ExternalOperation{
		Kind:     "nope",
		ChangeID: "123",
		Context:  json.RawMessage(`{"different":"content"}`),
		Status:   fdestate.PreparingStatus,
	}

	opC_OtherChange := fdestate.ExternalOperation{
		Kind:     "fde-efi-secureboot-db-update",
		ChangeID: "100",
		Status:   fdestate.PreparingStatus,
	}

	c.Check(opA.Equal(&opB), Equals, true)
	c.Check(opB.Equal(&opA), Equals, true)

	c.Check(opA.Equal(&opC_OtherKind), Equals, false)
	c.Check(opA.Equal(&opC_OtherChange), Equals, false)
	c.Check(opB.Equal(&opC_OtherKind), Equals, false)
	c.Check(opB.Equal(&opC_OtherChange), Equals, false)
}
