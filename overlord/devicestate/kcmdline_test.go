// -*- Mode: Go; indent-tabs-mode: t -*-

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

package devicestate_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
)

func checkExtraSnapdArgs(c *C, st *state.State, expectedArgs map[string]string) {
	var args map[string]string
	err := st.Get("kcmdline-extra-snapd-args", &args)
	if !errors.Is(err, state.ErrNoState) {
		c.Assert(err, IsNil)
	}
	c.Check(args, DeepEquals, expectedArgs)
}

func checkPendingExtraSnapdArgs(c *C, st *state.State, expected bool) {
	var pending bool
	err := st.Get("kcmdline-pending-extra-snapd-args", &pending)
	if !errors.Is(err, state.ErrNoState) {
		c.Assert(err, IsNil)
	}
	c.Check(pending, Equals, expected)
}

func (s *deviceMgrBootconfigSuite) TestSetExtraSnapdKernelCommandLineArg(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// kcmdline-extra-snapd-args doesn't exist yet
	checkExtraSnapdArgs(c, s.state, nil)

	argName := devicestate.ExtraSnapdKernelCmdlineArg("snapd.xkb")

	updated, err := devicestate.SetExtraSnapdKernelCommandLineArg(s.state, argName, "some-val")
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	checkPendingExtraSnapdArgs(c, s.state, true)
	checkExtraSnapdArgs(c, s.state, map[string]string{"snapd.xkb": "some-val"})

	// Set the same value
	updated, err = devicestate.SetExtraSnapdKernelCommandLineArg(s.state, argName, "some-val")
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	// But pending flag was not explicitly reset so it stays from
	// last run.
	checkPendingExtraSnapdArgs(c, s.state, true)
	checkExtraSnapdArgs(c, s.state, map[string]string{"snapd.xkb": "some-val"})

	// Try again with pending flag reset
	s.state.Set("kcmdline-pending-extra-snapd-args", false)
	updated, err = devicestate.SetExtraSnapdKernelCommandLineArg(s.state, argName, "some-val")
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	checkPendingExtraSnapdArgs(c, s.state, false)
	checkExtraSnapdArgs(c, s.state, map[string]string{"snapd.xkb": "some-val"})

	// Set a different value
	updated, err = devicestate.SetExtraSnapdKernelCommandLineArg(s.state, argName, "some-other-val")
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	checkPendingExtraSnapdArgs(c, s.state, true)
	checkExtraSnapdArgs(c, s.state, map[string]string{"snapd.xkb": "some-other-val"})

	// Unset value
	updated, err = devicestate.SetExtraSnapdKernelCommandLineArg(s.state, argName, "")
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	checkPendingExtraSnapdArgs(c, s.state, true)
	checkExtraSnapdArgs(c, s.state, map[string]string{})
}

func (s *deviceMgrBootconfigSuite) TestSetExtraSnapdKernelCommandLineArgErrors(c *C) {
	argName := devicestate.ExtraSnapdKernelCmdlineArg("bad-arg")
	_, err := devicestate.SetExtraSnapdKernelCommandLineArg(s.state, argName, "some-val")
	c.Assert(err, ErrorMatches, `internal error: unexpected extra snapd kcmdline arg: "bad-arg"`)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", false)
	argName = devicestate.ExtraSnapdKernelCmdlineArg("snapd.xkb")
	_, err = devicestate.SetExtraSnapdKernelCommandLineArg(s.state, argName, "some-val")
	c.Assert(err, ErrorMatches, "cannot set extra snapd kernel cmdline arguments until fully seeded")
}
