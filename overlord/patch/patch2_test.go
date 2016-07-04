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

package patch_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type patch2Suite struct{}

var _ = Suite(&patch2Suite{})

// makeState creates a state with SnapSetup with Name that can
// then be migrated to SideInfo.RealName
func (s *patch2Suite) makeState() *state.State {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	oldSS := patch.OldSnapSetup{
		Name: "foo",
	}
	chg := st.NewChange("something", "some change")
	t := st.NewTask("some-task", "some task")
	t.Set("snap-setup", &oldSS)
	chg.AddTask(t)

	return st
}

func (s *patch2Suite) TestPatch2(c *C) {
	st := s.makeState()

	err := patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(st.Tasks(), HasLen, 1)
	t := st.Tasks()[0]

	var ss snapstate.SnapSetup
	err = t.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "foo",
	})
}
