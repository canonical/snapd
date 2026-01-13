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

func checkExtraSnapdAppends(c *C, st *state.State, expected map[string]string) {
	var cmdlineAppends map[string]string
	err := st.Get("kcmdline-extra-snapd-appends", &cmdlineAppends)
	if !errors.Is(err, state.ErrNoState) {
		c.Assert(err, IsNil)
	}
	c.Check(cmdlineAppends, DeepEquals, expected)
}

func checkPendingExtraSnapdAppends(c *C, st *state.State, expected bool) {
	var pending bool
	err := st.Get("kcmdline-pending-extra-snapd-appends", &pending)
	if !errors.Is(err, state.ErrNoState) {
		c.Assert(err, IsNil)
	}
	c.Check(pending, Equals, expected)
}

func (s *deviceMgrBootconfigSuite) TestSetExtraSnapdKernelCommandLineAppend(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// kcmdline-extra-snapd-appends doesn't exist yet
	checkExtraSnapdAppends(c, s.state, nil)

	argName := devicestate.ExtraSnapdKernelCmdlineAppendType("xkb")

	updated, err := devicestate.SetExtraSnapdKernelCommandLineAppend(s.state, argName, `arg1="val-1" arg1="val-2" arg2`)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	checkPendingExtraSnapdAppends(c, s.state, true)
	checkExtraSnapdAppends(c, s.state, map[string]string{"xkb": `arg1="val-1" arg1="val-2" arg2`})

	// Set the same value
	updated, err = devicestate.SetExtraSnapdKernelCommandLineAppend(s.state, argName, `arg1="val-1" arg1="val-2" arg2`)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	// But pending flag was not explicitly reset so it stays from
	// last run.
	checkPendingExtraSnapdAppends(c, s.state, true)
	checkExtraSnapdAppends(c, s.state, map[string]string{"xkb": `arg1="val-1" arg1="val-2" arg2`})

	// Try again with pending flag reset
	s.state.Set("kcmdline-pending-extra-snapd-appends", false)
	updated, err = devicestate.SetExtraSnapdKernelCommandLineAppend(s.state, argName, `arg1="val-1" arg1="val-2" arg2`)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	checkPendingExtraSnapdAppends(c, s.state, false)
	checkExtraSnapdAppends(c, s.state, map[string]string{"xkb": `arg1="val-1" arg1="val-2" arg2`})

	// Set a different value
	updated, err = devicestate.SetExtraSnapdKernelCommandLineAppend(s.state, argName, `arg1="val-1"`)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	checkPendingExtraSnapdAppends(c, s.state, true)
	checkExtraSnapdAppends(c, s.state, map[string]string{"xkb": `arg1="val-1"`})

	// Unset value
	updated, err = devicestate.SetExtraSnapdKernelCommandLineAppend(s.state, argName, "")
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	checkPendingExtraSnapdAppends(c, s.state, true)
	checkExtraSnapdAppends(c, s.state, map[string]string{})
}

func (s *deviceMgrBootconfigSuite) TestSetExtraSnapdKernelCommandLineAppendErrors(c *C) {
	argName := devicestate.ExtraSnapdKernelCmdlineAppendType("bad-type")
	_, err := devicestate.SetExtraSnapdKernelCommandLineAppend(s.state, argName, "some-val")
	c.Assert(err, ErrorMatches, `internal error: unexpected extra snapd kernel command line append type: "bad-type"`)

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", false)
	argName = devicestate.ExtraSnapdKernelCmdlineAppendType("xkb")
	_, err = devicestate.SetExtraSnapdKernelCommandLineAppend(s.state, argName, "some-val")
	c.Assert(err, ErrorMatches, "cannot set extra snapd kernel command line arguments until fully seeded")
}
