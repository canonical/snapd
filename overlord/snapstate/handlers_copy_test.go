// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type copySnapDataSuite struct {
	baseHandlerSuite
}

var _ = Suite(&copySnapDataSuite{})

func (s *copySnapDataSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
}

func (s *copySnapDataSuite) TestDoCopySnapDataFailedRead(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// With a snap "pkg" at revision 42
	si := &snap.SideInfo{RealName: "pkg", Revision: snap.R(42)}
	snapstate.Set(s.state, "pkg", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	})

	// With an app belonging to the snap that is apparently running.
	snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		return nil, errors.New("some error")
	})

	// We can unlink the current revision of that snap, by setting IgnoreRunning flag.
	task := s.state.NewTask("copy-snap-data", "test")
	task.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "pkg",
			Revision: snap.R(42),
		},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(task)

	// Run the task we created
	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	// And observe the results.
	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "pkg", &snapst))

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*\(some error\)`)
}
