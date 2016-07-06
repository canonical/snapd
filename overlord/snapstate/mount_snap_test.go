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

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type mountSnapSuite struct {
	state   *state.State
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend

	reset func()
}

var _ = Suite(&mountSnapSuite{})

func (s *mountSnapSuite) SetUpTest(c *C) {
	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(nil)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers(s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	s.reset = snapstate.MockReadInfo(s.fakeBackend.ReadInfo)
}

func (s *mountSnapSuite) TearDownTest(c *C) {
	s.reset()
}

func (s *mountSnapSuite) TestDoMountSnapRemovesSnaps(c *C) {
	v1 := "name: mock\nversion: 1.0\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Candidate: &snap.SideInfo{
			RealName: "foo", Revision: snap.R(33),
		},
		SnapType: "app",
	})

	t := s.state.NewTask("mount-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Name:         "foo",
		SnapPath:     testSnap,
		DownloadInfo: &snap.DownloadInfo{DownloadURL: "https://some"},
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	c.Assert(osutil.FileExists(testSnap), Equals, false)
}
