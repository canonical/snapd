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
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type baseHandlerSuite struct {
	testutil.BaseTest

	state   *state.State
	runner  *state.TaskRunner
	se      *overlord.StateEngine
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend
}

func (s *baseHandlerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())

	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(nil)
	s.runner = state.NewTaskRunner(s.state)

	s.snapmgr = mylog.Check2(snapstate.Manager(s.state, s.runner))


	s.se = overlord.NewStateEngine(s.state)
	s.se.AddManager(s.snapmgr)
	s.se.AddManager(s.runner)
	c.Assert(s.se.StartUp(), IsNil)

	AddForeignTaskHandlers(s.runner, s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	reset1 := snapstate.MockSnapReadInfo(s.fakeBackend.ReadInfo)
	reset2 := snapstate.MockReRefreshRetryTimeout(time.Second / 200)

	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.AddCleanup(reset1)
	s.AddCleanup(reset2)
	s.AddCleanup(snapstatetest.MockDeviceModel(nil))

	restoreCheckFreeSpace := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error { return nil })
	s.AddCleanup(restoreCheckFreeSpace)

	restoreSecurityProfilesDiscardLate := snapstate.MockSecurityProfilesDiscardLate(func(snapName string, rev snap.Revision, typ snap.Type) error {
		return nil
	})
	s.AddCleanup(restoreSecurityProfilesDiscardLate)
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
	s.state.NewChange("sample", "...").AddTask(t)

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
