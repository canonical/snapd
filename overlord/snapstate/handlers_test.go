// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
)

type handlersSuite struct {
	baseHandlerSuite

	stateBackend *witnessRestartReqStateBackend
}

var _ = Suite(&handlersSuite{})

func (s *handlersSuite) SetUpTest(c *C) {
	s.setup(c, s.stateBackend)

	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
	s.AddCleanup(snap.MockSnapdSnapID("snapd-snap-id"))
}

func (s *handlersSuite) TestSetTaskSnapSetupFirstTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// make a new task which will be the snap-setup-task for other tasks and
	// write a SnapSetup to it
	t := s.state.NewTask("link-snap", "test")
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
			SnapID:   "foo-id",
		},
		Channel: "beta",
		UserID:  2,
	}
	t.Set("snap-setup", snapsup)
	s.state.NewChange("dummy", "...").AddTask(t)

	// mutate it and rewrite it with the helper
	snapsup.Channel = "edge"
	err := snapstate.SetTaskSnapSetup(t, snapsup)
	c.Assert(err, IsNil)

	// get a fresh version of this task from state to check that the task's
	/// snap-setup has the changes in it now
	newT := s.state.Task(t.ID())
	c.Assert(newT, Not(IsNil))
	var newsnapsup snapstate.SnapSetup
	err = newT.Get("snap-setup", &newsnapsup)
	c.Assert(err, IsNil)
	c.Assert(newsnapsup.Channel, Equals, snapsup.Channel)
}

func (s *handlersSuite) TestSetTaskSnapSetupLaterTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	t := s.state.NewTask("link-snap", "test")

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
			SnapID:   "foo-id",
		},
		Channel: "beta",
		UserID:  2,
	}
	// setup snap-setup for the first task
	t.Set("snap-setup", snapsup)

	// make a new task and reference the first one in snap-setup-task
	t2 := s.state.NewTask("next-task-snap", "test2")
	t2.Set("snap-setup-task", t.ID())

	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	chg.AddTask(t2)

	// mutate it and rewrite it with the helper
	snapsup.Channel = "edge"
	err := snapstate.SetTaskSnapSetup(t2, snapsup)
	c.Assert(err, IsNil)

	// check that the original task's snap-setup is different now
	newT := s.state.Task(t.ID())
	c.Assert(newT, Not(IsNil))
	var newsnapsup snapstate.SnapSetup
	err = newT.Get("snap-setup", &newsnapsup)
	c.Assert(err, IsNil)
	c.Assert(newsnapsup.Channel, Equals, snapsup.Channel)
}

func (s *handlersSuite) TestComputeMissingDisabledServices(c *C) {
	for _, tt := range []struct {
		// inputs
		stDisabledSvcsList []string
		apps               map[string]*snap.AppInfo
		// outputs
		missing []string
		found   []string
		err     error
		comment string
	}{
		// no apps
		{
			[]string{},
			nil,
			[]string{},
			[]string{},
			nil,
			"no apps",
		},
		// only apps, no services
		{
			[]string{},
			map[string]*snap.AppInfo{
				"app": {
					Daemon: "",
				},
			},
			[]string{},
			[]string{},
			nil,
			"no services",
		},
		// services in snap, but not disabled
		{
			[]string{},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon: "simple",
				},
			},
			[]string{},
			[]string{},
			nil,
			"no disabled services",
		},
		// all disabled services, but not present in snap
		{
			[]string{"svc1"},
			nil,
			[]string{"svc1"},
			[]string{},
			nil,
			"all missing disabled services",
		},
		// all disabled services, and found in snap
		{
			[]string{"svc1"},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon: "simple",
				},
			},
			[]string{},
			[]string{"svc1"},
			nil,
			"all found disabled services",
		},
		// some disabled services, some missing, some present in snap
		{
			[]string{"svc1", "svc2"},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon: "simple",
				},
			},
			[]string{"svc2"},
			[]string{"svc1"},
			nil,
			"some missing, some found disabled services",
		},
		// some disabled services, but app is not service
		{
			[]string{"svc1"},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon: "",
				},
			},
			[]string{"svc1"},
			[]string{},
			nil,
			"some disabled services that are now apps",
		},
	} {
		info := &snap.Info{Apps: tt.apps}

		found, missing, err := snapstate.MissingDisabledServices(tt.stDisabledSvcsList, info)
		c.Assert(missing, DeepEquals, tt.missing, Commentf(tt.comment))
		c.Assert(found, DeepEquals, tt.found, Commentf(tt.comment))
		c.Assert(err, Equals, tt.err, Commentf(tt.comment))
	}
}
