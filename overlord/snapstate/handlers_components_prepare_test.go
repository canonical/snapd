// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

type prepareCompSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&prepareCompSnapSuite{})

func (s *prepareSnapSuite) TestDoPrepareComponentSimple(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	// Unset component revision
	compRev := snap.R(0)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	t := s.state.NewTask("prepare-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, "path-to-component"))
	t.Set("snap-setup", ssu)

	s.state.NewChange("test change", "change desc").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var csup snapstate.ComponentSetup
	t.Get("component-setup", &csup)
	// Revision should have been set to x1 (-1)
	c.Check(csup.CompSideInfo, DeepEquals, snap.NewComponentSideInfo(
		cref, snap.R(-1),
	))
	c.Check(t.Status(), Equals, state.DoneStatus)
}
