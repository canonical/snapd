// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"fmt"
	"os"

	. "gopkg.in/check.v1"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

type activateStateSuite struct {
	testutil.BaseTest
}

var _ = Suite(&activateStateSuite{})

type serializesToChecker struct {
	*CheckerInfo
}

var SerializesTo = &serializesToChecker{
	CheckerInfo: &CheckerInfo{Name: "SerializesTo", Params: []string{"obtained", "expected"}},
}

func (checker *serializesToChecker) Check(params []any, names []string) (bool, string) {
	serialized, err := json.Marshal(params[0])
	if err != nil {
		return false, err.Error()
	}

	var genericValue any
	err = json.Unmarshal(serialized, &genericValue)
	if err != nil {
		return false, err.Error()
	}

	return DeepEquals.Check([]any{genericValue, params[1]}, names)
}

func (s *activateStateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *activateStateSuite) TestActivateStateHappy(c *C) {
	calls := 0
	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		calls += 1
		c.Check(name, Equals, "unlocked.json")

		s := &secboot.ActivateState{}
		s.Activations = map[string]*sb.ContainerActivateState{
			"data-cred-id": {
				Status: sb.ActivationSucceededWithPlatformKey,
			},
			"save-cred-id": {
				Status: sb.ActivationSucceededWithPlatformKey,
			},
		}
		return &boot.DiskUnlockState{
			State: s,
		}, nil
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	systemState, err := fdestate.SystemState(st)
	c.Assert(err, IsNil)

	c.Check(systemState, SerializesTo, map[string]any{
		"status": "active",
	})

	// Let's check the file is cached
	_, err = fdestate.SystemState(st)
	c.Assert(err, IsNil)
	c.Check(calls, Equals, 1)
}

func (s *activateStateSuite) TestActivateStateInactive(c *C) {
	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		c.Check(name, Equals, "unlocked.json")

		s := &secboot.ActivateState{}
		s.Activations = map[string]*sb.ContainerActivateState{}
		return &boot.DiskUnlockState{
			State: s,
		}, nil
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	systemState, err := fdestate.SystemState(st)
	c.Assert(err, IsNil)

	c.Check(systemState, SerializesTo, map[string]any{
		"status": "inactive",
	})
}

func (s *activateStateSuite) TestActivateStateRecovery(c *C) {
	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		c.Check(name, Equals, "unlocked.json")

		s := &secboot.ActivateState{}
		s.Activations = map[string]*sb.ContainerActivateState{
			"data-cred-id": {
				Status: sb.ActivationSucceededWithRecoveryKey,
			},
			"save-cred-id": {
				Status: sb.ActivationSucceededWithRecoveryKey,
			},
		}
		return &boot.DiskUnlockState{
			State: s,
		}, nil
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	systemState, err := fdestate.SystemState(st)
	c.Assert(err, IsNil)

	c.Check(systemState, SerializesTo, map[string]any{
		"status": "recovery",
	})
}

func (s *activateStateSuite) TestActivateStateNoActivateState(c *C) {
	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		c.Check(name, Equals, "unlocked.json")
		return &boot.DiskUnlockState{}, nil
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	systemState, err := fdestate.SystemState(st)
	c.Assert(err, IsNil)

	c.Check(systemState, SerializesTo, map[string]any{
		"status": "indeterminate",
	})
}

func (s *activateStateSuite) TestActivateStateNoUnlockedJSON(c *C) {
	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		c.Check(name, Equals, "unlocked.json")
		return nil, os.ErrNotExist
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	systemState, err := fdestate.SystemState(st)
	c.Assert(err, IsNil)

	c.Check(systemState, SerializesTo, map[string]any{
		"status": "indeterminate",
	})
}

func (s *activateStateSuite) TestActivateStateErrorUnlockedJSON(c *C) {
	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		c.Check(name, Equals, "unlocked.json")
		return nil, fmt.Errorf("some error")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := fdestate.SystemState(st)
	c.Assert(err, ErrorMatches, `some error`)
}
