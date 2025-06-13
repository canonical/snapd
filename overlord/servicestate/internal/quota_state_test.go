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

package internal_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/servicestate/internal"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

var testYaml = `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`

type quotaStateSuite struct {
	testutil.BaseTest
	state            *state.State
	testSnapState    *snapstate.SnapState
	testSnapSideInfo *snap.SideInfo
	testSnapInfo     *snap.Info
}

var _ = Suite(&quotaStateSuite{})

func (s *quotaStateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.state = state.New(nil)

	// set an empty sanitize method.
	snap.SanitizePlugsSlots = func(*snap.Info) {}

	// setup a test-snap with a service that can be easily injected into
	// snapstate to be setup as needed
	s.testSnapSideInfo = &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	s.testSnapState = &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{s.testSnapSideInfo}),
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	}

	// need lock for setting up test-snap
	s.state.Lock()
	defer s.state.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	s.testSnapInfo = snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
}

func (s *quotaStateSuite) TestQuotaStateUpdate(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// mock a boot id
	r := internal.MockOsutilBootID("mock-boot-id")
	defer r()

	task := st.NewTask("foo", "...")

	// check that the quota state is not already updated
	data, err := internal.GetQuotaState(task)
	c.Assert(data, IsNil)
	c.Assert(err, IsNil)

	c.Check(internal.SetQuotaState(task, &internal.QuotaStateItems{
		QuotaGroupName: "test-group",
		AppsToRestartBySnap: map[*snap.Info][]*snap.AppInfo{
			s.testSnapInfo: {s.testSnapInfo.Apps["svc1"]},
		},
		RefreshProfiles: true,
	}), IsNil)

	// manually verify the task data
	var updated internal.QuotaStateUpdated
	c.Assert(task.Get("state-updated", &updated), IsNil)
	c.Check(updated.BootID, Equals, "mock-boot-id")
	c.Check(updated.QuotaGroupName, Equals, "test-group")
	c.Check(updated.AppsToRestartBySnap, HasLen, 1)
	c.Check(updated.AppsToRestartBySnap[s.testSnapInfo.InstanceName()], DeepEquals, []string{"svc1"})

	data, err = internal.GetQuotaState(task)
	c.Assert(err, IsNil)
	c.Assert(data, NotNil)
}

func (s *quotaStateSuite) TestQuotaStateAlreadyUpdated(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("foo", "...")

	// check that the quota state is not already updated
	data, err := internal.GetQuotaState(task)
	c.Assert(data, IsNil)
	c.Assert(err, IsNil)

	// Manually set the task data to simulate a previous update
	task.Set("state-updated", internal.QuotaStateUpdated{
		BootID:              "mock-boot-id",
		QuotaGroupName:      "test-group",
		AppsToRestartBySnap: map[string][]string{"test-snap": {"svc1"}},
		RefreshProfiles:     true,
	})

	// mock a different boot id we set for the state, meaning
	// that a restart has happened, and thus no apps should be restarted
	r := internal.MockOsutilBootID("different-boot-id")
	data, err = internal.GetQuotaState(task)
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, &internal.QuotaStateItems{
		QuotaGroupName: "test-group",
	})
	r()

	// the boot id must match to get the below app list
	r = internal.MockOsutilBootID("mock-boot-id")
	defer r()

	data, err = internal.GetQuotaState(task)
	c.Assert(err, IsNil)
	c.Check(data.QuotaGroupName, Equals, "test-group")
	c.Assert(data.AppsToRestartBySnap, HasLen, 1)
	for snapName, apps := range data.AppsToRestartBySnap {
		c.Check(snapName.RealName, Equals, "test-snap")
		c.Check(apps, HasLen, 1)
		c.Check(apps[0].Name, Equals, "svc1")
	}
	c.Check(data.RefreshProfiles, Equals, true)
}

func (s *quotaStateSuite) TestQuotaStateSnaps(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("foo", "...")

	// Manually set the task data to simulate a previous update
	task.Set("state-updated", internal.QuotaStateUpdated{
		AppsToRestartBySnap: map[string][]string{"test-snap": {"svc1"}},
	})

	data, err := internal.GetQuotaStateSnaps(task)
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, []string{"test-snap"})
}
