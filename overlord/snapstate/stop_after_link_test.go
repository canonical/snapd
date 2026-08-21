// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func (s *snapmgrTestSuite) TestStopAfterLinkSkipsPostLinkSuffix(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si1 := &snap.SideInfo{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}
	si2 := &snap.SideInfo{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(2)}
	snapst := &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1, si2}),
		Current:         snap.R(2),
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
	}
	snapstate.Set(s.state, "snapd", snapst)

	snapsup := &snapstate.SnapSetup{
		Channel: "18/stable",
		SideInfo: &snap.SideInfo{
			RealName: "snapd",
			SnapID:   "snapd-snap-id",
			Revision: snap.R(3),
		},
		Type:    snap.TypeSnapd,
		Version: "snapdVer",
	}

	ts, err := snapstate.DoInstallOrPreDownload(s.state, snapst, snapsup, nil, snapstate.InstallContext{
		StopAfterLink:       true,
		NoRestartBoundaries: true,
	})
	c.Assert(err, IsNil)

	c.Check(taskKinds(ts.TaskSet().Tasks()), DeepEquals, []string{
		"prerequisites",
		"download-snap",
		"validate-snap",
		"prerequisites",
		"mount-snap",
		"run-hook[pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"clear-snap",
		"discard-snap",
		"cleanup",
	})

	link := ts.TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(link, NotNil)
	c.Check(link.Kind(), Equals, "link-snap")

	end := ts.TaskSet().MaybeEdge(snapstate.EndEdge)
	c.Assert(end, NotNil)
	c.Check(end.Kind(), Equals, "cleanup")

	setup, err := ts.TaskSet().Edge(snapstate.SnapSetupEdge)
	c.Assert(err, IsNil)
	c.Check(setup.Kind(), Equals, "download-snap")
	got, err := snapstate.TaskSnapSetup(setup)
	c.Assert(err, IsNil)
	c.Check(got.Channel, Equals, "18/stable")
	c.Check(got.Revision(), Equals, snap.R(3))

	var discarded []snap.Revision
	for _, t := range ts.TaskSet().Tasks() {
		if t.Kind() != "clear-snap" {
			continue
		}
		var discardedSetup snapstate.SnapSetup
		c.Assert(t.Get("snap-setup", &discardedSetup), IsNil)
		discarded = append(discarded, discardedSetup.Revision())
	}
	c.Check(discarded, DeepEquals, []snap.Revision{snap.R(1)})
}

func (s *snapmgrTestSuite) TestStopAfterLinkCleanupWaitsForLinkSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}
	snapst := &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "snapd",
	}
	snapstate.Set(s.state, "snapd", snapst)

	snapsup := &snapstate.SnapSetup{
		Channel: "18/stable",
		SideInfo: &snap.SideInfo{
			RealName: "snapd",
			SnapID:   "snapd-snap-id",
			Revision: snap.R(2),
		},
		Type:    snap.TypeSnapd,
		Version: "snapdVer",
	}

	ts, err := snapstate.DoInstallOrPreDownload(s.state, snapst, snapsup, nil, snapstate.InstallContext{
		StopAfterLink:       true,
		NoRestartBoundaries: true,
	})
	c.Assert(err, IsNil)

	link := ts.TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(link, NotNil)
	end := ts.TaskSet().MaybeEdge(snapstate.EndEdge)
	c.Assert(end, NotNil)
	c.Check(end.Kind(), Equals, "cleanup")
	c.Check(end.WaitTasks(), DeepEquals, []*state.Task{link})
}
