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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type discardSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&discardSnapSuite{})

func (s *discardSnapSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)

	s.state.Lock()
	defer s.state.Unlock()
	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)
	oldSnapStateEnsureSnapAbsentFromQuotaGroup := snapstate.EnsureSnapAbsentFromQuotaGroup
	snapstate.EnsureSnapAbsentFromQuotaGroup = servicestate.EnsureSnapAbsentFromQuota
	s.AddCleanup(func() {
		snapstate.EnsureSnapAbsentFromQuotaGroup = oldSnapStateEnsureSnapAbsentFromQuotaGroup
	})

	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
}

func (s *discardSnapSuite) TestDoDiscardSnapSuccess(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(3)},
			{RealName: "foo", Revision: snap.R(33)},
		}),
		Current:  snap.R(33),
		SnapType: "app",
	})
	t := s.state.NewTask("discard-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
	})
	s.state.NewChange("sample", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "foo", &snapst))


	c.Check(snapst.Sequence.Revisions, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(3))
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *discardSnapSuite) TestDoDiscardSnapInQuotaGroup(c *C) {
	s.state.Lock()

	old := snapstate.EnsureSnapAbsentFromQuotaGroup
	defer func() {
		snapstate.EnsureSnapAbsentFromQuotaGroup = old
	}()

	ensureSnapAbsentFromQuotaGroupCalls := 0
	snapstate.EnsureSnapAbsentFromQuotaGroup = func(st *state.State, snap string) error {
		ensureSnapAbsentFromQuotaGroupCalls++
		c.Assert(snap, Equals, "foo")
		return nil
	}
	defer func() { c.Assert(ensureSnapAbsentFromQuotaGroupCalls, Equals, 1) }()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(3)},
		}),
		Current:  snap.R(3),
		SnapType: "app",
	})
	t := s.state.NewTask("discard-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
	})
	s.state.NewChange("sample", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "foo", &snapst))
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *discardSnapSuite) TestDoDiscardSnapToEmpty(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(3)},
		}),
		Current:  snap.R(3),
		SnapType: "app",
	})
	t := s.state.NewTask("discard-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
	})
	s.state.NewChange("sample", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "foo", &snapst))
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *discardSnapSuite) TestDoDiscardSnapErrorsForActive(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(3)},
		}),
		Current:  snap.R(3),
		Active:   true,
		SnapType: "app",
	})
	t := s.state.NewTask("discard-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(3),
		},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*internal error: cannot discard snap "foo": still active.*`)
}

func (s *discardSnapSuite) TestDoDiscardSnapNoErrorsForActive(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(3)},
			{RealName: "foo", Revision: snap.R(33)},
		}),
		Current:  snap.R(33),
		Active:   true,
		SnapType: "app",
	})
	t := s.state.NewTask("discard-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(3),
		},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "foo", &snapst))


	c.Assert(chg.Err(), IsNil)
	c.Check(snapst.Sequence.Revisions, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(33))
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *discardSnapSuite) TestDoDiscardSnapdRemovesLate(c *C) {
	var removeLateCalledFor [][]string
	restore := snapstate.MockSecurityProfilesDiscardLate(func(snapName string, rev snap.Revision, typ snap.Type) error {
		removeLateCalledFor = append(removeLateCalledFor, []string{
			snapName, rev.String(), string(typ),
		})
		return nil
	})
	defer restore()

	s.state.Lock()

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", Revision: snap.R(3)},
			{RealName: "snapd", Revision: snap.R(33)},
		}),
		Current:  snap.R(33),
		SnapType: "snapd",
	})
	t := s.state.NewTask("discard-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "snapd",
			Revision: snap.R(33),
		},
		Type: snap.TypeSnapd,
	})
	s.state.NewChange("sample", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "snapd", &snapst))


	c.Check(snapst.Sequence.Revisions, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(3))
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(removeLateCalledFor, DeepEquals, [][]string{
		{"snapd", "33", "snapd"},
	})
}
