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

package snapstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type baseHandlerSuite struct {
	state   *state.State
	runner  *state.TaskRunner
	se      *overlord.StateEngine
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend

	reset func()
}

func (s *baseHandlerSuite) setup(c *C, b state.Backend) {
	dirs.SetRootDir(c.MkDir())

	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(b)
	s.runner = state.NewTaskRunner(s.state)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state, s.runner)
	c.Assert(err, IsNil)

	s.se = overlord.NewStateEngine(s.state)
	s.se.AddManager(s.snapmgr)
	s.se.AddManager(s.runner)

	AddForeignTaskHandlers(s.runner, s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	reset1 := snapstate.MockSnapReadInfo(s.fakeBackend.ReadInfo)
	s.reset = func() {
		dirs.SetRootDir("/")
		reset1()
	}
}

func (s *baseHandlerSuite) SetUpTest(c *C) {
	s.setup(c, nil)
}

func (s *baseHandlerSuite) TearDownTest(c *C) {
	s.reset()
}

type prepareSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&prepareSnapSuite{})

func (s *prepareSnapSuite) TestDoPrepareSnapSimple(c *C) {
	s.state.Lock()
	t := s.state.NewTask("prepare-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
		},
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(-1),
	})
	c.Check(t.Status(), Equals, state.DoneStatus)
}
