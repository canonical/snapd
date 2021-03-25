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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

type discardSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&discardSnapSuite{})

func (s *discardSnapSuite) SetUpTest(c *C) {
	s.setup(c, nil)

	s.state.Lock()
	defer s.state.Unlock()
	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
}

func (s *discardSnapSuite) TestDoDiscardSnapSuccess(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(3)},
			{RealName: "foo", Revision: snap.R(33)},
		},
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
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(3))
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *discardSnapSuite) TestDoDiscardSnapToEmpty(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(3)},
		},
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
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *discardSnapSuite) TestDoDiscardSnapErrorsForActive(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(3)},
		},
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
	chg := s.state.NewChange("dummy", "...")
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
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(3)},
			{RealName: "foo", Revision: snap.R(33)},
		},
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
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(33))
	c.Check(t.Status(), Equals, state.DoneStatus)
}

type MockSecurityBackend struct {
	removeCalls     int
	removeLateCalls int
	calls           [][]string
}

func (m *MockSecurityBackend) Initialize(*interfaces.SecurityBackendOptions) error {
	return fmt.Errorf("not called")
}
func (m *MockSecurityBackend) Name() interfaces.SecuritySystem { return "mock" }
func (m *MockSecurityBackend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	return fmt.Errorf("not called")
}

func (m *MockSecurityBackend) Remove(_ string) error {
	return fmt.Errorf("not called")
}

func (m *MockSecurityBackend) NewSpecification() interfaces.Specification { return nil }

func (m *MockSecurityBackend) SandboxFeatures() []string { return nil }

func (m *MockSecurityBackend) RemoveLate(snapName string, rev snap.Revision, typ snap.Type) error {
	m.calls = append(m.calls, []string{snapName, rev.String(), string(typ)})
	m.removeLateCalls++
	return nil
}

func (s *discardSnapSuite) TestDoDiscardSnapdRemovesLate(c *C) {
	s.state.Lock()

	repo := ifacerepo.Get(s.state)
	m := &MockSecurityBackend{}
	repo.AddBackend(m)

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "snapd", Revision: snap.R(3)},
			{RealName: "snapd", Revision: snap.R(33)},
		},
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
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "snapd", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(3))
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(m.removeCalls, Equals, 0)
	c.Check(m.removeLateCalls, Equals, 1)
	c.Check(m.calls, DeepEquals, [][]string{
		{"snapd", "33", "snapd"},
	})
}
