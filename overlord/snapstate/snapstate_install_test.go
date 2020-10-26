// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"

	// So it registers Configure.
	_ "github.com/snapcore/snapd/overlord/configstate"
)

func verifyInstallTasks(c *C, opts, discards int, ts *state.TaskSet, st *state.State) {
	kinds := taskKinds(ts.Tasks())

	expected := []string{
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
	}
	if opts&unlinkBefore != 0 {
		expected = append(expected,
			"stop-snap-services",
			"remove-aliases",
			"unlink-current-snap",
		)
	}
	if opts&updatesGadget != 0 {
		expected = append(expected, "update-gadget-assets")
	}
	expected = append(expected,
		"copy-snap-data",
		"setup-profiles",
		"export-content",
		"link-snap",
	)
	expected = append(expected,
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook[install]",
		"start-snap-services")
	for i := 0; i < discards; i++ {
		expected = append(expected,
			"clear-snap",
			"discard-snap",
		)
	}
	if opts&cleanupAfter != 0 {
		expected = append(expected,
			"cleanup",
		)
	}
	if opts&noConfigure == 0 {
		expected = append(expected,
			"run-hook[configure]",
		)
	}
	expected = append(expected,
		"run-hook[check-health]",
	)

	c.Assert(kinds, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestInstallDevModeConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// if a snap is devmode, you can't install it without --devmode
	opts := &snapstate.RevisionOptions{Channel: "channel-for-devmode"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires devmode or confinement override`)

	// if a snap is devmode, you *can* install it with --devmode
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)

	// if a snap is *not* devmode, you can still install it with --devmode
	opts.Channel = "channel-for-strict"
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallClassicConfinementFiltering(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// if a snap is classic, you can't install it without --classic
	opts := &snapstate.RevisionOptions{Channel: "channel-for-classic"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires classic confinement`)

	// if a snap is classic, you *can* install it with --classic
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)

	// if a snap is *not* classic, but can install it with --classic which gets ignored
	opts.Channel = "channel-for-strict"
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyInstallTasks(c, 0, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestInstallTaskEdgesForPreseeding(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
`)

	for _, skipConfig := range []bool{false, true} {
		ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(8)}, mockSnap, "", "", snapstate.Flags{SkipConfigure: skipConfig})
		c.Assert(err, IsNil)

		te, err := ts.Edge(snapstate.BeginEdge)
		c.Assert(err, IsNil)
		c.Check(te.Kind(), Equals, "prerequisites")

		te, err = ts.Edge(snapstate.BeforeHooksEdge)
		c.Assert(err, IsNil)
		c.Check(te.Kind(), Equals, "setup-aliases")

		te, err = ts.Edge(snapstate.HooksEdge)
		c.Assert(err, IsNil)
		c.Assert(te.Kind(), Equals, "run-hook")

		var hsup *hookstate.HookSetup
		c.Assert(te.Get("hook-setup", &hsup), IsNil)
		c.Check(hsup.Hook, Equals, "install")
		c.Check(hsup.Snap, Equals, "some-snap")
	}
}

func (s *snapmgrTestSuite) TestInstallSnapdSnapType(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "snapd", opts, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyInstallTasks(c, noConfigure, 0, ts, s.state)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Type, Equals, snap.TypeSnapd)
}

func (s *snapmgrTestSuite) TestInstallCohortTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	opts := &snapstate.RevisionOptions{Channel: "some-channel", CohortKey: "what"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.CohortKey, Equals, "what")

	verifyInstallTasks(c, 0, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestInstallWithDeviceContext(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{CtxStore: s.fakeStore}

	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.InstallWithDeviceContext(context.Background(), s.state, "some-snap", opts, 0, snapstate.Flags{}, deviceCtx, "")
	c.Assert(err, IsNil)

	verifyInstallTasks(c, 0, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestInstallHookNotRunForInstalledSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(7)},
		},
		Current:  snap.R(7),
		SnapType: "app",
	})

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
epoch: 1*
`)
	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(8)}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)

	runHooks := tasksWithKind(ts, "run-hook")
	// no install hook task
	c.Assert(taskKinds(runHooks), DeepEquals, []string{
		"run-hook[pre-refresh]",
		"run-hook[post-refresh]",
		"run-hook[configure]",
		"run-hook[check-health]",
	})
}

func (s *snapmgrTestSuite) TestInstallFailsOnDisabledSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapst := &snapstate.SnapState{
		Active:          false,
		TrackingChannel: "channel/stable",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}},
		Current:         snap.R(2),
		SnapType:        "app",
	}
	snapsup := &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}}
	_, err := snapstate.DoInstall(s.state, snapst, snapsup, 0, "", nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot update disabled snap "some-snap"`)
}

func dummyInUseCheck(snap.Type) (boot.InUseFunc, error) {
	return func(string, snap.Revision) bool {
		return false
	}, nil
}

func (s *snapmgrTestSuite) TestInstallFailsOnBusySnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// With the refresh-app-awareness feature enabled.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness", true)
	tr.Commit()

	// With a snap state indicating a snap is already installed.
	snapst := &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	}
	snapstate.Set(s.state, "some-snap", snapst)

	// With a snap info indicating it has an application called "app"
	snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		if name != "some-snap" {
			return s.fakeBackend.ReadInfo(name, si)
		}
		info := &snap.Info{SuggestedName: name, SideInfo: *si, SnapType: snap.TypeApp}
		info.Apps = map[string]*snap.AppInfo{
			"app": {Snap: info, Name: "app"},
		}
		return info, nil
	})

	// mock that "some-snap" has an app and that this app has pids running
	restore := snapstate.MockPidsOfSnap(func(instanceName string) (map[string][]int, error) {
		c.Assert(instanceName, Equals, "some-snap")
		return map[string][]int{
			"snap.some-snap.app": {1234},
		}, nil
	})
	defer restore()

	// Attempt to install revision 2 of the snap.
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
	}

	// And observe that we cannot refresh because the snap is busy.
	_, err := snapstate.DoInstall(s.state, snapst, snapsup, 0, "", dummyInUseCheck)
	c.Assert(err, ErrorMatches, `snap "some-snap" has running apps \(app\)`)

	// The state records the time of the failed refresh operation.
	err = snapstate.Get(s.state, "some-snap", snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.RefreshInhibitedTime, NotNil)
}

func (s *snapmgrTestSuite) TestInstallWithIgnoreValidationProceedsOnBusySnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// With the refresh-app-awareness feature enabled.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness", true)
	tr.Commit()

	// With a snap state indicating a snap is already installed.
	snapst := &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "pkg", SnapID: "pkg-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	}
	snapstate.Set(s.state, "pkg", snapst)

	// With a snap info indicating it has an application called "app"
	snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		if name != "pkg" {
			return s.fakeBackend.ReadInfo(name, si)
		}
		info := &snap.Info{SuggestedName: name, SideInfo: *si, SnapType: snap.TypeApp}
		info.Apps = map[string]*snap.AppInfo{
			"app": {Snap: info, Name: "app"},
		}
		return info, nil
	})

	// With an app belonging to the snap that is apparently running.
	restore := snapstate.MockPidsOfSnap(func(instanceName string) (map[string][]int, error) {
		c.Assert(instanceName, Equals, "pkg")
		return map[string][]int{
			"snap.pkg.app": {1234},
		}, nil
	})
	defer restore()

	// Attempt to install revision 2 of the snap, with the IgnoreRunning flag set.
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "pkg", SnapID: "pkg-id", Revision: snap.R(2)},
		Flags:    snapstate.Flags{IgnoreRunning: true},
	}

	// And observe that we do so despite the running app.
	_, err := snapstate.DoInstall(s.state, snapst, snapsup, 0, "", dummyInUseCheck)
	c.Assert(err, IsNil)

	// The state confirms that the refresh operation was not postponed.
	err = snapstate.Get(s.state, "pkg", snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.RefreshInhibitedTime, IsNil)
}

func (s *snapmgrTestSuite) TestInstallDespiteBusySnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// With the refresh-app-awareness feature enabled.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness", true)
	tr.Commit()

	// With a snap state indicating a snap is already installed and it failed
	// to refresh over a week ago. Use UTC and Round to have predictable
	// behaviour across time-zones and with enough precision loss to be
	// compatible with the serialization format.
	var longAgo = time.Now().UTC().Round(time.Second).Add(-time.Hour * 24 * 8)
	snapst := &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:              snap.R(1),
		SnapType:             "app",
		RefreshInhibitedTime: &longAgo,
	}
	snapstate.Set(s.state, "some-snap", snapst)

	// With a snap info indicating it has an application called "app"
	snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		if name != "some-snap" {
			return s.fakeBackend.ReadInfo(name, si)
		}
		info := &snap.Info{SuggestedName: name, SideInfo: *si, SnapType: snap.TypeApp}
		info.Apps = map[string]*snap.AppInfo{
			"app": {Snap: info, Name: "app"},
		}
		return info, nil
	})
	// And with cgroup information indicating the app has a process with pid 1234.
	restore := snapstate.MockPidsOfSnap(func(instanceName string) (map[string][]int, error) {
		c.Assert(instanceName, Equals, "some-snap")
		return map[string][]int{
			"snap.some-snap.some-app": {1234},
		}, nil
	})
	defer restore()

	// Attempt to install revision 2 of the snap.
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
	}

	// And observe that refresh occurred regardless of the running process.
	_, err := snapstate.DoInstall(s.state, snapst, snapsup, 0, "", dummyInUseCheck)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallFailsOnSystem(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapsup := &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "system", SnapID: "some-snap-id", Revision: snap.R(1)}}
	_, err := snapstate.DoInstall(s.state, nil, snapsup, 0, "", nil)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot install reserved snap name 'system'`)
}

func (s *snapmgrTestSuite) TestDoInstallChannelDefault(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "stable")
}

func (s *snapmgrTestSuite) TestInstallRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	opts := &snapstate.RevisionOptions{Revision: snap.R(7)}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Revision(), Equals, snap.R(7))
}

func (s *snapmgrTestSuite) TestInstallTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", nil)

	_, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `too early for operation, device not yet seeded or device model not acknowledged`)
}

func (s *snapmgrTestSuite) TestInstallConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	_, err = snapstate.Install(context.Background(), s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "install" change in progress`)
}

func (s *snapmgrTestSuite) TestInstallAliasConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "otherfoosnap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "otherfoosnap", Revision: snap.R(30)},
		},
		Current: snap.R(30),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"foo.bar": {Manual: "bar"},
		},
		SnapType: "app",
	})

	_, err := snapstate.Install(context.Background(), s.state, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "foo" command namespace conflicts with alias "foo\.bar" for "otherfoosnap" snap`)
}

func (s *snapmgrTestSuite) TestInstallStrictIgnoresClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	opts := &snapstate.RevisionOptions{Channel: "channel-for-strict"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)

	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "install snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap is *not* classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.TrackingChannel, Equals, "channel-for-strict/stable")
	c.Check(snapst.Classic, Equals, false)
}

func (s *snapmgrTestSuite) TestInstallSnapWithDefaultTrack(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	opts := &snapstate.RevisionOptions{Channel: "candidate"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap-with-default-track", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "install snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap is in the 2.0 track
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap-with-default-track", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.TrackingChannel, Equals, "2.0/candidate")
}

func (s *snapmgrTestSuite) TestInstallManySnapOneWithDefaultTrack(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapNames := []string{"some-snap", "some-snap-with-default-track"}
	installed, tss, err := snapstate.InstallMany(s.state, snapNames, s.user.ID)
	c.Assert(err, IsNil)
	c.Assert(installed, DeepEquals, snapNames)

	chg := s.state.NewChange("install", "install two snaps")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap is in the 2.0 track
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap-with-default-track", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.TrackingChannel, Equals, "2.0/stable")

	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.TrackingChannel, Equals, "latest/stable")
}

// A sneakyStore changes the state when called
type sneakyStore struct {
	*fakeStore
	state *state.State
}

func (s sneakyStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	s.state.Lock()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}},
		Current:         snap.R(1),
		SnapType:        "app",
	})
	s.state.Unlock()
	return s.fakeStore.SnapAction(ctx, currentSnaps, actions, assertQuery, user, opts)
}

func (s *snapmgrTestSuite) TestInstallStateConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, sneakyStore{fakeStore: s.fakeStore, state: s.state})

	_, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestInstallPathTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(nil)
	defer r()

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap"}, mockSnap, "", "", snapstate.Flags{})
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `too early for operation, device model not yet acknowledged`)

}

func (s *snapmgrTestSuite) TestInstallPathConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, _, err = snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap"}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "install" change in progress`)
}

func (s *snapmgrTestSuite) TestInstallPathMissingName(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`internal error: snap name to install %q not provided`, mockSnap))
}

func (s *snapmgrTestSuite) TestInstallPathSnapIDRevisionUnset(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "snapididid"}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`internal error: snap id set to install %q but revision is unset`, mockSnap))
}

func (s *snapmgrTestSuite) TestInstallPathValidateFlags(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
confinement: devmode
`)
	_, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap"}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires devmode or confinement override`)
}

func (s *snapmgrTestSuite) TestInstallPathStrictIgnoresClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
confinement: strict
`)

	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap"}, mockSnap, "", "", snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)

	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "install snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap is *not* classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Classic, Equals, false)
}

func (s *snapmgrTestSuite) TestInstallPathAsRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Flags:  snapstate.Flags{DevMode: true},
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "wibbly/stable",
	})

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
epoch: 1
`)

	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap"}, mockSnap, "", "edge", snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "install snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap is *not* classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.TrackingChannel, Equals, "wibbly/edge")
}

func (s *snapmgrTestSuite) TestParallelInstanceInstallNotAllowed(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, sneakyStore{fakeStore: s.fakeStore, state: s.state})

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	_, err := snapstate.Install(context.Background(), s.state, "core_foo", nil, 0, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install snap of type os as "core_foo"`)

	_, err = snapstate.Install(context.Background(), s.state, "some-base_foo", nil, 0, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install snap of type base as "some-base_foo"`)

	_, err = snapstate.Install(context.Background(), s.state, "some-gadget_foo", nil, 0, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install snap of type gadget as "some-gadget_foo"`)

	_, err = snapstate.Install(context.Background(), s.state, "some-kernel_foo", nil, 0, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install snap of type kernel as "some-kernel_foo"`)

	_, err = snapstate.Install(context.Background(), s.state, "some-snapd_foo", nil, 0, snapstate.Flags{})
	c.Check(err, ErrorMatches, `cannot install snap of type snapd as "some-snapd_foo"`)
}

func (s *snapmgrTestSuite) TestInstallPathFailsEarlyOnEpochMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have epoch 1* installed
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", Revision: snap.R(7)}},
		Current:         snap.R(7),
	})

	// try to install epoch 42
	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0\nepoch: 42\n")
	_, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap"}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot refresh "some-snap" to local snap with epoch 42, because it can't read the current epoch of 1\*`)
}

func (s *snapmgrTestSuite) TestInstallRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// we start without the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("some-snap-id"), testutil.FileAbsent)

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(snapstate.Installing(s.state), Equals, false)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
	}})
	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true, Commentf("salts seen: %v", s.fakeStore.seenPrivacyKeys))
	expected := fakeOps{
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "some-snap",
				Channel:      "some-channel",
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:    "export-content:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(11),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	ta := ts.Tasks()
	task := ta[1]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)
	c.Check(task.Summary(), Equals, `Download snap "some-snap" (11) from channel "some-channel"`)

	// check install-record present
	mountTask := ta[len(ta)-12]
	c.Check(mountTask.Kind(), Equals, "mount-snap")
	var installRecord backend.InstallRecord
	c.Assert(mountTask.Get("install-record", &installRecord), IsNil)
	c.Check(installRecord.TargetSnapExisted, Equals, false)

	// check link/start snap summary
	linkTask := ta[len(ta)-8]
	c.Check(linkTask.Summary(), Equals, `Make snap "some-snap" (11) available to the system`)
	startTask := ta[len(ta)-3]
	c.Check(startTask.Summary(), Equals, `Start snap "some-snap" (11) services`)

	// verify snap-setup in the task state
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel:  "some-channel",
		UserID:   s.user.ID,
		SnapPath: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
			Size:        5,
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		PlugsOnly: true,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "some-channel",
		Revision: snap.R(11),
		SnapID:   "some-snap-id",
	})

	// verify snaps in the system state
	var snaps map[string]*snapstate.SnapState
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["some-snap"]
	c.Assert(snapst, NotNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "some-channel/stable")
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "some-channel",
		Revision: snap.R(11),
	})
	c.Assert(snapst.Required, Equals, false)

	// we end with the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("some-snap-id"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestParallelInstanceInstallRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap_instance", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(snapstate.Installing(s.state), Equals, false)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_instance_11.snap"),
	}})
	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true, Commentf("salts seen: %v", s.fakeStore.seenPrivacyKeys))
	expected := fakeOps{
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "some-snap_instance",
				Channel:      "some-channel",
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap_instance",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "some-snap_instance_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "some-snap_instance",
			path:  filepath.Join(dirs.SnapBlobDir, "some-snap_instance_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/11"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap_instance",
			revno: snap.R(11),
		},
		{
			op:    "export-content:Doing",
			name:  "some-snap_instance",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap_instance",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap_instance",
			revno: snap.R(11),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	ta := ts.Tasks()
	task := ta[1]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)
	c.Check(task.Summary(), Equals, `Download snap "some-snap_instance" (11) from channel "some-channel"`)

	// check link/start snap summary
	linkTask := ta[len(ta)-8]
	c.Check(linkTask.Summary(), Equals, `Make snap "some-snap_instance" (11) available to the system`)
	startTask := ta[len(ta)-3]
	c.Check(startTask.Summary(), Equals, `Start snap "some-snap_instance" (11) services`)

	// verify snap-setup in the task state
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel:  "some-channel",
		UserID:   s.user.ID,
		SnapPath: filepath.Join(dirs.SnapBlobDir, "some-snap_instance_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
			Size:        5,
		},
		SideInfo:    snapsup.SideInfo,
		Type:        snap.TypeApp,
		PlugsOnly:   true,
		InstanceKey: "instance",
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "some-channel",
		Revision: snap.R(11),
		SnapID:   "some-snap-id",
	})

	// verify snaps in the system state
	var snaps map[string]*snapstate.SnapState
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["some-snap_instance"]
	c.Assert(snapst, NotNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "some-channel/stable")
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "some-channel",
		Revision: snap.R(11),
	})
	c.Assert(snapst.Required, Equals, false)
	c.Assert(snapst.InstanceKey, Equals, "instance")

	runHooks := tasksWithKind(ts, "run-hook")
	c.Assert(taskKinds(runHooks), DeepEquals, []string{"run-hook[install]", "run-hook[configure]", "run-hook[check-health]"})
	for _, hookTask := range runHooks {
		c.Assert(hookTask.Kind(), Equals, "run-hook")
		var hooksup hookstate.HookSetup
		err = hookTask.Get("hook-setup", &hooksup)
		c.Assert(err, IsNil)
		c.Assert(hooksup.Snap, Equals, "some-snap_instance")
	}
}

func (s *snapmgrTestSuite) TestInstallUndoRunThroughJustOneSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]
	// sanity
	c.Assert(last.Lanes(), HasLen, 1)
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	terr.JoinLane(last.Lanes()[0])
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	mountTask := tasks[len(tasks)-12]
	c.Assert(mountTask.Kind(), Equals, "mount-snap")
	var installRecord backend.InstallRecord
	c.Assert(mountTask.Get("install-record", &installRecord), IsNil)
	c.Check(installRecord.TargetSnapExisted, Equals, false)

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
	}})
	expected := fakeOps{
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "some-snap",
				Channel:      "some-channel",
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:    "export-content:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),

			unlinkFirstInstallUndo: true,
		},
		{
			op:    "export-content:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  "<no-old>",
		},
		{
			op:   "remove-snap-data-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapDataDir, "some-snap"),
		},
		{
			op:    "undo-setup-snap",
			name:  "some-snap",
			stype: "app",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:   "remove-snap-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap"),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestInstallWithCohortRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel", CohortKey: "scurries"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(snapstate.Installing(s.state), Equals, false)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_666.snap"),
	}})
	expected := fakeOps{
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "some-snap",
				CohortKey:    "scurries",
				Channel:      "some-channel",
			},
			revno:  snap.R(666),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(666),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "some-snap_666.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Revision: snap.R(666),
				Channel:  "some-channel",
			},
		},
		{
			op:    "setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "some-snap_666.snap"),
			revno: snap.R(666),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/666"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(666),
		},
		{
			op:    "export-content:Doing",
			name:  "some-snap",
			revno: snap.R(666),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Revision: snap.R(666),
				Channel:  "some-channel",
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/666"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(666),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(666),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	ta := ts.Tasks()
	task := ta[1]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)
	c.Check(task.Summary(), Equals, `Download snap "some-snap" (666) from channel "some-channel"`)

	// check link/start snap summary
	linkTask := ta[len(ta)-8]
	c.Check(linkTask.Summary(), Equals, `Make snap "some-snap" (666) available to the system`)
	startTask := ta[len(ta)-3]
	c.Check(startTask.Summary(), Equals, `Start snap "some-snap" (666) services`)

	// verify snap-setup in the task state
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel:  "some-channel",
		UserID:   s.user.ID,
		SnapPath: filepath.Join(dirs.SnapBlobDir, "some-snap_666.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
			Size:        5,
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		PlugsOnly: true,
		CohortKey: "scurries",
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(666),
		SnapID:   "some-snap-id",
		Channel:  "some-channel",
	})

	// verify snaps in the system state
	var snaps map[string]*snapstate.SnapState
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["some-snap"]
	c.Assert(snapst, NotNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "some-channel/stable")
	c.Assert(snapst.CohortKey, Equals, "scurries")
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(666),
		Channel:  "some-channel",
	})
	c.Assert(snapst.Required, Equals, false)
}

func (s *snapmgrTestSuite) TestInstallWithRevisionRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(42)}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(snapstate.Installing(s.state), Equals, false)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
	}})
	expected := fakeOps{
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "some-snap",
				Revision:     snap.R(42),
			},
			revno:  snap.R(42),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Revision: snap.R(42),
			},
		},
		{
			op:    "setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
			revno: snap.R(42),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/42"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op:    "export-content:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Revision: snap.R(42),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/42"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(42),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	ta := ts.Tasks()
	task := ta[1]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)
	c.Check(task.Summary(), Equals, `Download snap "some-snap" (42) from channel "some-channel"`)

	// check link/start snap summary
	linkTask := ta[len(ta)-8]
	c.Check(linkTask.Summary(), Equals, `Make snap "some-snap" (42) available to the system`)
	startTask := ta[len(ta)-3]
	c.Check(startTask.Summary(), Equals, `Start snap "some-snap" (42) services`)

	// verify snap-setup in the task state
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel:  "some-channel",
		UserID:   s.user.ID,
		SnapPath: filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
			Size:        5,
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		PlugsOnly: true,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(42),
		SnapID:   "some-snap-id",
	})

	// verify snaps in the system state
	var snaps map[string]*snapstate.SnapState
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["some-snap"]
	c.Assert(snapst, NotNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "some-channel/stable")
	c.Assert(snapst.CohortKey, Equals, "")
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(42),
	})
	c.Assert(snapst.Required, Equals, false)
}

func (s *snapmgrTestSuite) TestInstallStartOrder(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "services-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(snapstate.Installing(s.state), Equals, false)
	op := s.fakeBackend.ops.First("start-snap-services")
	c.Assert(op, NotNil)
	c.Assert(op, DeepEquals, &fakeOp{
		op:   "start-snap-services",
		path: filepath.Join(dirs.SnapMountDir, "services-snap/11"),
		// ordered to preserve after/before relation
		services: []string{"svc1", "svc3", "svc2"},
	})
}

func (s *snapmgrTestSuite) TestInstalling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	c.Check(snapstate.Installing(s.state), Equals, false)

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(42)}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	c.Check(snapstate.Installing(s.state), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallFirstLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []*snap.Info, userID int) (uint64, error) {
		c.Fatalf("installSize shouldn't be hit with local install")
		return 0, nil
	})
	defer restoreInstallSize()

	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, info, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "mock"}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// ensure the returned info is correct
	c.Check(info.SideInfo.RealName, Equals, "mock")
	c.Check(info.Version, Equals, "1.0")

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			// only local install was run, i.e. first actions are pseudo-action current
			op:  "current",
			old: "<no-current>",
		},
		{
			// and setup-snap
			op:    "setup-snap",
			name:  "mock",
			path:  mockSnap,
			revno: snap.R("x1"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "mock/x1"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "mock",
			revno: snap.R("x1"),
		},
		{
			op:    "export-content:Doing",
			name:  "mock",
			revno: snap.R("x1"),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "mock",
				Revision: snap.R("x1"),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "mock/x1"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "mock",
			revno: snap.R("x1"),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  "mock",
			revno: snap.R("x1"),
		},
	}

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[1]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		SnapPath:  mockSnap,
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		PlugsOnly: true,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-1),
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Channel:  "",
		Revision: snap.R(-1),
	})
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-1))
}

func (s *snapmgrTestSuite) TestInstallSubsequentLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "mock", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "mock", Revision: snap.R(-2)},
		},
		Current:  snap.R(-2),
		SnapType: "app",
	})

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0
epoch: 1*
`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "mock"}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "mock/x2"),
		},
		{
			op:    "setup-snap",
			name:  "mock",
			path:  mockSnap,
			revno: snap.R("x3"),
		},
		{
			op:   "remove-snap-aliases",
			name: "mock",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "mock/x2"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "mock/x3"),
			old:  filepath.Join(dirs.SnapMountDir, "mock/x2"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "mock",
			revno: snap.R(-3),
		},
		{
			op:    "export-content:Doing",
			name:  "mock",
			revno: snap.R(-3),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "mock",
				Revision: snap.R(-3),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "mock/x3"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "mock",
			revno: snap.R("x3"),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  "mock",
			revno: snap.R("x3"),
		},
	}

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[1]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		SnapPath:  mockSnap,
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		PlugsOnly: true,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-3),
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.CurrentSideInfo(), DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Channel:  "",
		Revision: snap.R(-3),
	})
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-3))
}

func (s *snapmgrTestSuite) TestInstallOldSubsequentLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "mock", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "mock", Revision: snap.R(100001)},
		},
		Current:  snap.R(100001),
		SnapType: "app",
	})

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0
epoch: 1*
`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "mock"}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			// ensure only local install was run, i.e. first action is pseudo-action current
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "mock/100001"),
		},
		{
			// and setup-snap
			op:    "setup-snap",
			name:  "mock",
			path:  mockSnap,
			revno: snap.R("x1"),
		},
		{
			op:   "remove-snap-aliases",
			name: "mock",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "mock/100001"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "mock/x1"),
			old:  filepath.Join(dirs.SnapMountDir, "mock/100001"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "mock",
			revno: snap.R("x1"),
		},
		{
			op:    "export-content:Doing",
			name:  "mock",
			revno: snap.R("x1"),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "mock",
				Revision: snap.R("x1"),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "mock/x1"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "mock",
			revno: snap.R("x1"),
		},
		{
			op: "update-aliases",
		},
		{
			// and cleanup
			op:    "cleanup-trash",
			name:  "mock",
			revno: snap.R("x1"),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.CurrentSideInfo(), DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Channel:  "",
		Revision: snap.R(-1),
	})
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-1))
}

func (s *snapmgrTestSuite) TestInstallPathWithMetadataRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	someSnap := makeTestSnap(c, `name: orig-name
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")

	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(42),
	}
	ts, _, err := snapstate.InstallPath(s.state, si, someSnap, "", "", snapstate.Flags{Required: true})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure only local install was run, i.e. first actions are pseudo-action current
	c.Assert(s.fakeBackend.ops.Ops(), HasLen, 10)
	c.Check(s.fakeBackend.ops[0].op, Equals, "current")
	c.Check(s.fakeBackend.ops[0].old, Equals, "<no-current>")
	// and setup-snap
	c.Check(s.fakeBackend.ops[1].op, Equals, "setup-snap")
	c.Check(s.fakeBackend.ops[1].name, Equals, "some-snap")
	c.Check(s.fakeBackend.ops[1].path, Matches, `.*/orig-name_1.0_all.snap`)
	c.Check(s.fakeBackend.ops[1].revno, Equals, snap.R(42))

	c.Check(s.fakeBackend.ops[5].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[5].sinfo, DeepEquals, *si)
	c.Check(s.fakeBackend.ops[6].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[6].path, Equals, filepath.Join(dirs.SnapMountDir, "some-snap/42"))

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		SnapPath: someSnap,
		SideInfo: snapsup.SideInfo,
		Flags: snapstate.Flags{
			Required: true,
		},
		Type:      snap.TypeApp,
		PlugsOnly: true,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, si)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "")
	c.Assert(snapst.Sequence[0], DeepEquals, si)
	c.Assert(snapst.LocalRevision().Unset(), Equals, true)
	c.Assert(snapst.Required, Equals, true)
}

func (s *snapmgrTestSuite) TestInstallPathSkipConfigure(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	makeInstalledMockCoreSnap(c)

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c)

	snapPath := makeTestSnap(c, "name: some-snap\nversion: 1.0")

	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}, snapPath, "", "edge", snapstate.Flags{SkipConfigure: true})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	// SkipConfigure is consumed and consulted when creating the taskset
	// but is not copied into SnapSetup
	c.Check(snapsup.Flags.SkipConfigure, Equals, false)
}

func (s *snapmgrTestSuite) TestInstallWithoutCoreRunThrough1(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// pretend we don't have core
	snapstate.Set(s.state, "core", nil)

	chg := s.state.NewChange("install", "install a snap on a system without core")
	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(42)}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{
			macaroon: s.user.StoreMacaroon,
			name:     "core",
			target:   filepath.Join(dirs.SnapBlobDir, "core_11.snap"),
		},
		{
			macaroon: s.user.StoreMacaroon,
			name:     "some-snap",
			target:   filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
		}})
	expected := fakeOps{
		// we check the snap
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "some-snap",
				Revision:     snap.R(42),
			},
			revno:  snap.R(42),
			userID: 1,
		},
		// then we check core because its not installed already
		// and continue with that
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "core",
				Channel:      "stable",
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "core",
		},
		{
			op:    "validate-snap:Doing",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "core_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "core",
				Channel:  "stable",
				SnapID:   "core-id",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "core",
			path:  filepath.Join(dirs.SnapBlobDir, "core_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "core/11"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op:    "export-content:Doing",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "core",
				Channel:  "stable",
				SnapID:   "core-id",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "core/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		// after core is in place continue with the snap
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Revision: snap.R(42),
			},
		},
		{
			op:    "setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
			revno: snap.R(42),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/42"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op:    "export-content:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Revision: snap.R(42),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/42"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(42),
		},
		{
			op: "update-aliases",
		},
		// cleanups order is random
		{
			op:    "cleanup-trash",
			name:  "core",
			revno: snap.R(42),
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(42),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	// compare the details without the cleanup tasks, the order is random
	// as they run in parallel
	opsLenWithoutCleanups := len(s.fakeBackend.ops) - 2
	c.Assert(s.fakeBackend.ops[:opsLenWithoutCleanups], DeepEquals, expected[:opsLenWithoutCleanups])

	// verify core in the system state
	var snaps map[string]*snapstate.SnapState
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["core"]
	c.Assert(snapst, NotNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "latest/stable")
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "core",
		Channel:  "stable",
		SnapID:   "core-id",
		Revision: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestInstallWithoutCoreTwoSnapsRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockPrerequisitesRetryTimeout(10 * time.Millisecond)
	defer restore()

	// pretend we don't have core
	snapstate.Set(s.state, "core", nil)

	chg1 := s.state.NewChange("install", "install snap 1")
	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(42)}
	ts1, err := snapstate.Install(context.Background(), s.state, "snap1", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg1.AddAll(ts1)

	chg2 := s.state.NewChange("install", "install snap 2")
	opts = &snapstate.RevisionOptions{Channel: "some-other-channel", Revision: snap.R(21)}
	ts2, err := snapstate.Install(context.Background(), s.state, "snap2", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg2.AddAll(ts2)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran and core was only installed once
	c.Assert(chg1.Err(), IsNil)
	c.Assert(chg2.Err(), IsNil)

	c.Assert(chg1.IsReady(), Equals, true)
	c.Assert(chg2.IsReady(), Equals, true)

	// order in which the changes run is random
	if len(chg1.Tasks()) < len(chg2.Tasks()) {
		chg1, chg2 = chg2, chg1
	}
	c.Assert(taskKinds(chg1.Tasks()), HasLen, 30)
	c.Assert(taskKinds(chg2.Tasks()), HasLen, 15)

	// FIXME: add helpers and do a DeepEquals here for the operations
}

func (s *snapmgrTestSuite) TestInstallWithoutCoreTwoSnapsWithFailureRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// slightly longer retry timeout to avoid deadlock when we
	// trigger a retry quickly that the link snap for core does
	// not have a chance to run
	restore := snapstate.MockPrerequisitesRetryTimeout(40 * time.Millisecond)
	defer restore()

	defer s.se.Stop()
	// Two changes are created, the first will fails, the second will
	// be fine. The order of what change runs first is random, the
	// first change will also install core in its own lane. This test
	// ensures that core gets installed and there are no conflicts
	// even if core already got installed from the first change.
	//
	// It runs multiple times so that both possible cases get a chance
	// to run
	for i := 0; i < 5; i++ {
		// start clean
		snapstate.Set(s.state, "core", nil)
		snapstate.Set(s.state, "snap2", nil)

		// chg1 has an error
		chg1 := s.state.NewChange("install", "install snap 1")
		opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(42)}
		ts1, err := snapstate.Install(context.Background(), s.state, "snap1", opts, s.user.ID, snapstate.Flags{})
		c.Assert(err, IsNil)
		chg1.AddAll(ts1)

		tasks := ts1.Tasks()
		last := tasks[len(tasks)-1]
		terr := s.state.NewTask("error-trigger", "provoking total undo")
		terr.WaitFor(last)
		chg1.AddTask(terr)

		// chg2 is good
		chg2 := s.state.NewChange("install", "install snap 2")
		opts = &snapstate.RevisionOptions{Channel: "some-other-channel", Revision: snap.R(21)}
		ts2, err := snapstate.Install(context.Background(), s.state, "snap2", opts, s.user.ID, snapstate.Flags{})
		c.Assert(err, IsNil)
		chg2.AddAll(ts2)

		// we use our own settle as we need a bigger timeout
		s.state.Unlock()
		err = s.o.Settle(testutil.HostScaledTimeout(15 * time.Second))
		s.state.Lock()
		c.Assert(err, IsNil)

		// ensure expected change states
		c.Check(chg1.Status(), Equals, state.ErrorStatus)
		c.Check(chg2.Status(), Equals, state.DoneStatus)

		// ensure we have both core and snap2
		var snapst snapstate.SnapState
		err = snapstate.Get(s.state, "core", &snapst)
		c.Assert(err, IsNil)
		c.Assert(snapst.Active, Equals, true)
		c.Assert(snapst.Sequence, HasLen, 1)
		c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
			RealName: "core",
			SnapID:   "core-id",
			Channel:  "stable",
			Revision: snap.R(11),
		})

		var snapst2 snapstate.SnapState
		err = snapstate.Get(s.state, "snap2", &snapst2)
		c.Assert(err, IsNil)
		c.Assert(snapst2.Active, Equals, true)
		c.Assert(snapst2.Sequence, HasLen, 1)
		c.Assert(snapst2.Sequence[0], DeepEquals, &snap.SideInfo{
			RealName: "snap2",
			SnapID:   "snap2-id",
			Channel:  "",
			Revision: snap.R(21),
		})

	}
}

type behindYourBackStore struct {
	*fakeStore
	state *state.State

	coreInstallRequested bool
	coreInstalled        bool
	chg                  *state.Change
}

func (s behindYourBackStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if assertQuery != nil {
		panic("no assertion query support")
	}

	if len(actions) == 1 && actions[0].Action == "install" && actions[0].InstanceName == "core" {
		s.state.Lock()
		if !s.coreInstallRequested {
			s.coreInstallRequested = true
			snapsup := &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "core",
				},
			}
			t := s.state.NewTask("prepare", "prepare core")
			t.Set("snap-setup", snapsup)
			s.chg = s.state.NewChange("install", "install core")
			s.chg.AddAll(state.NewTaskSet(t))
		}
		if s.chg != nil && !s.coreInstalled {
			// marks change ready but also
			// tasks need to also be marked cleaned
			for _, t := range s.chg.Tasks() {
				t.SetStatus(state.DoneStatus)
				t.SetClean()
			}
			snapstate.Set(s.state, "core", &snapstate.SnapState{
				Active: true,
				Sequence: []*snap.SideInfo{
					{RealName: "core", Revision: snap.R(1)},
				},
				Current:  snap.R(1),
				SnapType: "os",
			})
			s.coreInstalled = true
		}
		s.state.Unlock()
	}

	return s.fakeStore.SnapAction(ctx, currentSnaps, actions, nil, user, opts)
}

// this test the scenario that some-snap gets installed and during the
// install (when unlocking for the store info call for core) an
// explicit "snap install core" happens. In this case the snapstate
// will return a change conflict. we handle this via a retry, ensure
// this is actually what happens.
func (s *snapmgrTestSuite) TestInstallWithoutCoreConflictingInstall(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockPrerequisitesRetryTimeout(10 * time.Millisecond)
	defer restore()

	snapstate.ReplaceStore(s.state, behindYourBackStore{fakeStore: s.fakeStore, state: s.state})

	// pretend we don't have core
	snapstate.Set(s.state, "core", nil)

	// now install a snap that will pull in core
	chg := s.state.NewChange("install", "install a snap on a system without core")
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	prereq := ts.Tasks()[0]
	c.Assert(prereq.Kind(), Equals, "prerequisites")
	c.Check(prereq.AtTime().IsZero(), Equals, true)

	s.state.Unlock()
	defer s.se.Stop()

	// start running the change, this will trigger the
	// prerequisites task, which will trigger the install of core
	// and also call our mock store which will generate a parallel
	// change
	s.se.Ensure()
	s.se.Wait()

	// change is not ready yet, because the prerequists triggered
	// a state.Retry{} because of the conflicting change
	c.Assert(chg.IsReady(), Equals, false)
	s.state.Lock()
	// marked for retry
	c.Check(prereq.AtTime().IsZero(), Equals, false)
	c.Check(prereq.Status().Ready(), Equals, false)
	s.state.Unlock()

	// retry interval is 10ms so 20ms should be plenty of time
	time.Sleep(20 * time.Millisecond)
	s.settle(c)
	// chg got retried, core is now installed, things are good
	c.Assert(chg.IsReady(), Equals, true)

	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify core in the system state
	var snaps map[string]*snapstate.SnapState
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["core"]
	c.Assert(snapst, NotNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	})

	snapst = snaps["some-snap"]
	c.Assert(snapst, NotNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "some-channel",
		Revision: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestInstallDefaultProviderRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "stable", Revision: snap.R(42)}
	ts, err := snapstate.Install(context.Background(), s.state, "snap-content-plug", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	expected := fakeOps{{
		op:     "storesvc-snap-action",
		userID: 1,
	}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:       "install",
			InstanceName: "snap-content-plug",
			Revision:     snap.R(42),
		},
		revno:  snap.R(42),
		userID: 1,
	}, {
		op:     "storesvc-snap-action",
		userID: 1,
	}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:       "install",
			InstanceName: "snap-content-slot",
			Channel:      "stable",
		},
		revno:  snap.R(11),
		userID: 1,
	}, {
		op:   "storesvc-download",
		name: "snap-content-slot",
	}, {
		op:    "validate-snap:Doing",
		name:  "snap-content-slot",
		revno: snap.R(11),
	}, {
		op:  "current",
		old: "<no-current>",
	}, {
		op:   "open-snap-file",
		path: filepath.Join(dirs.SnapBlobDir, "snap-content-slot_11.snap"),
		sinfo: snap.SideInfo{
			RealName: "snap-content-slot",
			Channel:  "stable",
			SnapID:   "snap-content-slot-id",
			Revision: snap.R(11),
		},
	}, {
		op:    "setup-snap",
		name:  "snap-content-slot",
		path:  filepath.Join(dirs.SnapBlobDir, "snap-content-slot_11.snap"),
		revno: snap.R(11),
	}, {
		op:   "copy-data",
		path: filepath.Join(dirs.SnapMountDir, "snap-content-slot/11"),
		old:  "<no-old>",
	}, {
		op:    "setup-profiles:Doing",
		name:  "snap-content-slot",
		revno: snap.R(11),
	}, {
		op:    "export-content:Doing",
		name:  "snap-content-slot",
		revno: snap.R(11),
	}, {
		op: "candidate",
		sinfo: snap.SideInfo{
			RealName: "snap-content-slot",
			Channel:  "stable",
			SnapID:   "snap-content-slot-id",
			Revision: snap.R(11),
		},
	}, {
		op:   "link-snap",
		path: filepath.Join(dirs.SnapMountDir, "snap-content-slot/11"),
	}, {
		op:    "auto-connect:Doing",
		name:  "snap-content-slot",
		revno: snap.R(11),
	}, {
		op: "update-aliases",
	}, {
		op:   "storesvc-download",
		name: "snap-content-plug",
	}, {
		op:    "validate-snap:Doing",
		name:  "snap-content-plug",
		revno: snap.R(42),
	}, {
		op:  "current",
		old: "<no-current>",
	}, {
		op:   "open-snap-file",
		path: filepath.Join(dirs.SnapBlobDir, "snap-content-plug_42.snap"),
		sinfo: snap.SideInfo{
			RealName: "snap-content-plug",
			SnapID:   "snap-content-plug-id",
			Revision: snap.R(42),
		},
	}, {
		op:    "setup-snap",
		name:  "snap-content-plug",
		path:  filepath.Join(dirs.SnapBlobDir, "snap-content-plug_42.snap"),
		revno: snap.R(42),
	}, {
		op:   "copy-data",
		path: filepath.Join(dirs.SnapMountDir, "snap-content-plug/42"),
		old:  "<no-old>",
	}, {
		op:    "setup-profiles:Doing",
		name:  "snap-content-plug",
		revno: snap.R(42),
	}, {
		op:    "export-content:Doing",
		name:  "snap-content-plug",
		revno: snap.R(42),
	}, {
		op: "candidate",
		sinfo: snap.SideInfo{
			RealName: "snap-content-plug",
			SnapID:   "snap-content-plug-id",
			Revision: snap.R(42),
		},
	}, {
		op:   "link-snap",
		path: filepath.Join(dirs.SnapMountDir, "snap-content-plug/42"),
	}, {
		op:    "auto-connect:Doing",
		name:  "snap-content-plug",
		revno: snap.R(42),
	}, {
		op: "update-aliases",
	}, {
		op:    "cleanup-trash",
		name:  "snap-content-plug",
		revno: snap.R(42),
	}, {
		op:    "cleanup-trash",
		name:  "snap-content-slot",
		revno: snap.R(11),
	},
	}
	// snap and default provider are installed in parallel so we can't
	// do a simple c.Check(ops, DeepEquals, fakeOps{...})
	c.Check(len(s.fakeBackend.ops), Equals, len(expected))
	for _, op := range expected {
		c.Assert(s.fakeBackend.ops, testutil.DeepContains, op)
	}
	for _, op := range s.fakeBackend.ops {
		c.Assert(expected, testutil.DeepContains, op)
	}
}

func (s *snapmgrTestSuite) TestInstallDefaultProviderCompat(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(42)}
	ts, err := snapstate.Install(context.Background(), s.state, "snap-content-plug-compat", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	// and both circular snaps got linked
	c.Check(s.fakeBackend.ops, testutil.DeepContains, fakeOp{
		op:   "link-snap",
		path: filepath.Join(dirs.SnapMountDir, "snap-content-plug-compat/42"),
	})
	c.Check(s.fakeBackend.ops, testutil.DeepContains, fakeOp{
		op:   "link-snap",
		path: filepath.Join(dirs.SnapMountDir, "snap-content-slot/11"),
	})
}

func (s *snapmgrTestSuite) TestInstallDiskSpaceError(c *C) {
	restore := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error { return &osutil.NotEnoughDiskSpaceError{} })
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-install", true)
	tr.Commit()

	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	diskSpaceErr := err.(*snapstate.InsufficientSpaceError)
	c.Assert(diskSpaceErr, ErrorMatches, `insufficient space in .* to perform "install" change for the following snaps: some-snap`)
	c.Check(diskSpaceErr.Path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd"))
	c.Check(diskSpaceErr.Snaps, DeepEquals, []string{"some-snap"})
}

func (s *snapmgrTestSuite) TestInstallSizeError(c *C) {
	restore := snapstate.MockInstallSize(func(st *state.State, snaps []*snap.Info, userID int) (uint64, error) {
		return 0, fmt.Errorf("boom")
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-install", true)
	tr.Commit()

	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Check(err, ErrorMatches, `boom`)
}

func (s *snapmgrTestSuite) TestInstallPathWithLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// When layouts are disabled we cannot install a local snap depending on the feature.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", false)
	tr.Commit()

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
layout:
 /usr:
  bind: $SNAP/usr
`)
	_, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(8)}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.layouts' to true")

	// When layouts are enabled we can install a local snap depending on the feature.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, _, err = snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(8)}, mockSnap, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallPathWithMetadataChannelSwitchKernel(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	// snapd cannot be installed unless the model uses a base snap
	r := snapstatetest.MockDeviceModel(ModelWithKernelTrack("18"))
	defer r()
	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "kernel", Revision: snap.R(11)},
		},
		TrackingChannel: "18/stable",
		Current:         snap.R(11),
		Active:          true,
	})

	someSnap := makeTestSnap(c, `name: kernel
version: 1.0`)
	si := &snap.SideInfo{
		RealName: "kernel",
		SnapID:   "kernel-id",
		Revision: snap.R(42),
		Channel:  "some-channel",
	}
	_, _, err := snapstate.InstallPath(s.state, si, someSnap, "", "some-channel", snapstate.Flags{Required: true})
	c.Assert(err, ErrorMatches, `cannot switch from kernel track "18" as specified for the \(device\) model to "some-channel"`)
}

func (s *snapmgrTestSuite) TestInstallPathWithMetadataChannelSwitchGadget(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	// snapd cannot be installed unless the model uses a base snap
	r := snapstatetest.MockDeviceModel(ModelWithGadgetTrack("18"))
	defer r()
	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "brand-gadget", Revision: snap.R(11)},
		},
		TrackingChannel: "18/stable",
		Current:         snap.R(11),
		Active:          true,
	})

	someSnap := makeTestSnap(c, `name: brand-gadget
version: 1.0`)
	si := &snap.SideInfo{
		RealName: "brand-gadget",
		SnapID:   "brand-gadget-id",
		Revision: snap.R(42),
		Channel:  "some-channel",
	}
	_, _, err := snapstate.InstallPath(s.state, si, someSnap, "", "some-channel", snapstate.Flags{Required: true})
	c.Assert(err, ErrorMatches, `cannot switch from gadget track "18" as specified for the \(device\) model to "some-channel"`)
}

func (s *snapmgrTestSuite) TestInstallLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Layouts are now enabled by default.
	opts := &snapstate.RevisionOptions{Channel: "channel-for-layout"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// Layouts can be explicitly disabled.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", false)
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.layouts' to true")

	// Layouts can be explicitly enabled.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// The default empty value now means "enabled".
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", "")
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// Layouts are enabled when the controlling flag is reset to nil.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", nil)
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallUserDaemonsChecksFeatureFlag(c *C) {
	if release.ReleaseInfo.ID == "ubuntu" && release.ReleaseInfo.VersionID == "14.04" {
		c.Skip("Ubuntu 14.04 does not support user daemons")
	}

	s.state.Lock()
	defer s.state.Unlock()

	// User daemons are disabled by default.
	opts := &snapstate.RevisionOptions{Channel: "channel-for-user-daemon"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.user-daemons' to true")

	// User daemons can be explicitly enabled.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.user-daemons", true)
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// User daemons can be explicitly disabled.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.user-daemons", false)
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.user-daemons' to true")

	// The default empty value means "disabled"".
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.user-daemons", "")
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.user-daemons' to true")

	// User daemons are disabled when the controlling flag is reset to nil.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.user-daemons", nil)
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.user-daemons' to true")
}

func (s *snapmgrTestSuite) TestInstallUserDaemonsUsupportedOnTrusty(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer restore()
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.user-daemons", true)
	tr.Commit()

	// Even with the experimental.user-daemons flag set, user
	// daemons are not supported on Trusty
	opts := &snapstate.RevisionOptions{Channel: "channel-for-user-daemon"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "user session daemons are not supported on this release")
}

func (s *snapmgrTestSuite) TestInstallDbusActivationChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// D-Bus activation is disabled by default.
	opts := &snapstate.RevisionOptions{Channel: "channel-for-dbus-activation"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.dbus-activation' to true")

	// D-Bus activation can be explicitly enabled.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.dbus-activation", true)
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// D-Bus activation can be explicitly disabled.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.dbus-activation", false)
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.dbus-activation' to true")

	// The default empty value means "disabled"
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.dbus-activation", "")
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.dbus-activation' to true")

	// D-Bus activation is disabled when the controlling flag is reset to nil.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.dbus-activation", nil)
	tr.Commit()
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.dbus-activation' to true")
}

func (s *snapmgrTestSuite) TestInstallValidatesInstanceNames(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	_, err := snapstate.Install(context.Background(), s.state, "foo--invalid", nil, 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `invalid instance name: invalid snap name: "foo--invalid"`)

	_, err = snapstate.Install(context.Background(), s.state, "foo_123_456", nil, 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `invalid instance name: invalid instance key: "123_456"`)

	_, _, err = snapstate.InstallMany(s.state, []string{"foo--invalid"}, 0)
	c.Assert(err, ErrorMatches, `invalid instance name: invalid snap name: "foo--invalid"`)

	_, _, err = snapstate.InstallMany(s.state, []string{"foo_123_456"}, 0)
	c.Assert(err, ErrorMatches, `invalid instance name: invalid instance key: "123_456"`)

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
epoch: 1*
`)
	si := snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(8)}
	_, _, err = snapstate.InstallPath(s.state, &si, mockSnap, "some-snap_123_456", "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, `invalid instance name: invalid instance key: "123_456"`)
}

func (s *snapmgrTestSuite) TestInstallFailsWhenClassicSnapsAreNotSupported(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	reset := release.MockReleaseInfo(&release.OS{
		ID: "fedora",
	})
	defer reset()

	// this needs doing because dirs depends on the release info
	dirs.SetRootDir(dirs.GlobalRootDir)

	opts := &snapstate.RevisionOptions{Channel: "channel-for-classic"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, ErrorMatches, "classic confinement requires snaps under /snap or symlink from /snap to "+dirs.SnapMountDir)
}

func (s *snapmgrTestSuite) TestInstallUndoRunThroughUndoContextOptional(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap-no-install-record", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]
	// sanity
	c.Assert(last.Lanes(), HasLen, 1)
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	terr.JoinLane(last.Lanes()[0])
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	mountTask := tasks[len(tasks)-12]
	c.Assert(mountTask.Kind(), Equals, "mount-snap")
	var installRecord backend.InstallRecord
	c.Assert(mountTask.Get("install-record", &installRecord), Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestInstallDefaultProviderCircular(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(42)}
	ts, err := snapstate.Install(context.Background(), s.state, "snap-content-circular1", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	// and both circular snaps got linked
	c.Check(s.fakeBackend.ops, testutil.DeepContains, fakeOp{
		op:   "link-snap",
		path: filepath.Join(dirs.SnapMountDir, "snap-content-circular1/42"),
	})
	c.Check(s.fakeBackend.ops, testutil.DeepContains, fakeOp{
		op:   "link-snap",
		path: filepath.Join(dirs.SnapMountDir, "snap-content-circular2/11"),
	})
}

func (s *snapmgrTestSuite) TestParallelInstallInstallPathExperimentalSwitch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
`)
	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(8)}
	_, _, err := snapstate.InstallPath(s.state, si, mockSnap, "some-snap_foo", "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.parallel-instances' to true")

	// enable parallel instances
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	_, _, err = snapstate.InstallPath(s.state, si, mockSnap, "some-snap_foo", "", snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	installed, tts, err := snapstate.InstallMany(s.state, []string{"one", "two"}, 0)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	c.Check(installed, DeepEquals, []string{"one", "two"})

	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true)

	for i, ts := range tts {
		verifyInstallTasks(c, 0, 0, ts, s.state)
		// check that tasksets are in separate lanes
		for _, t := range ts.Tasks() {
			c.Assert(t.Lanes(), DeepEquals, []int{i + 1})
		}
	}
}

func (s *snapmgrTestSuite) TestInstallManyDiskSpaceError(c *C) {
	restore := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error { return &osutil.NotEnoughDiskSpaceError{} })
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-install", true)
	tr.Commit()

	_, _, err := snapstate.InstallMany(s.state, []string{"one", "two"}, 0)
	diskSpaceErr := err.(*snapstate.InsufficientSpaceError)
	c.Assert(diskSpaceErr, ErrorMatches, `insufficient space in .* to perform "install" change for the following snaps: one, two`)
	c.Check(diskSpaceErr.Path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd"))
	c.Check(diskSpaceErr.Snaps, DeepEquals, []string{"one", "two"})
	c.Check(diskSpaceErr.ChangeKind, Equals, "install")
}

func (s *snapmgrTestSuite) TestInstallManyDiskCheckDisabled(c *C) {
	restore := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error { return &osutil.NotEnoughDiskSpaceError{} })
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-install", false)
	tr.Commit()

	_, _, err := snapstate.InstallMany(s.state, []string{"one", "two"}, 0)
	c.Check(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallManyTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", nil)

	_, _, err := snapstate.InstallMany(s.state, []string{"one", "two"}, 0)
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `too early for operation, device not yet seeded or device model not acknowledged`)
}

func (s *snapmgrTestSuite) TestInstallManyChecksPreconditions(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	_, _, err := snapstate.InstallMany(s.state, []string{"some-snap-now-classic"}, 0)
	c.Assert(err, NotNil)
	c.Check(err, DeepEquals, &snapstate.SnapNeedsClassicError{Snap: "some-snap-now-classic"})

	_, _, err = snapstate.InstallMany(s.state, []string{"some-snap_foo"}, 0)
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.parallel-instances' to true")
}

func verifyStopReason(c *C, ts *state.TaskSet, reason string) {
	tl := tasksWithKind(ts, "stop-snap-services")
	c.Check(tl, HasLen, 1)

	var stopReason string
	err := tl[0].Get("stop-reason", &stopReason)
	c.Assert(err, IsNil)
	c.Check(stopReason, Equals, reason)

}

func (s *snapmgrTestSuite) TestUndoMountSnapFailsInCopyData(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.copySnapDataFailTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/11")

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "some-snap",
				Channel:      "some-channel",
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "copy-data.failed",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  "<no-old>",
		},
		{
			op:   "remove-snap-data-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapDataDir, "some-snap"),
		},
		{
			op:    "undo-setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			stype: "app",
		},
		{
			op:   "remove-snap-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap"),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestSideInfoPaid(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	opts := &snapstate.RevisionOptions{Channel: "channel-for-paid"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "install paid snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snap has paid sideinfo
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.CurrentSideInfo().Paid, Equals, true)
	c.Check(snapst.CurrentSideInfo().Private, Equals, false)
}

func (s *snapmgrTestSuite) TestSideInfoPrivate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	opts := &snapstate.RevisionOptions{Channel: "channel-for-private"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "install private snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snap has private sideinfo
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.CurrentSideInfo().Private, Equals, true)
	c.Check(snapst.CurrentSideInfo().Paid, Equals, false)
}

func (s *snapmgrTestSuite) TestGadgetDefaultsInstalled(c *C) {
	makeInstalledMockCoreSnap(c)

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "app",
	})

	snapPath := makeTestSnap(c, "name: some-snap\nversion: 1.0")

	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}, snapPath, "", "edge", snapstate.Flags{})
	c.Assert(err, IsNil)

	var m map[string]interface{}
	runHooks := tasksWithKind(ts, "run-hook")

	c.Assert(runHooks[0].Kind(), Equals, "run-hook")
	err = runHooks[0].Get("hook-context", &m)
	c.Assert(err, Equals, state.ErrNoState)
}

func makeInstalledMockCoreSnap(c *C) {
	coreSnapYaml := `name: core
version: 1.0
type: os
`
	snaptest.MockSnap(c, coreSnapYaml, &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	})
}

func (s *snapmgrTestSuite) TestGadgetDefaults(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	makeInstalledMockCoreSnap(c)

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c)

	snapPath := makeTestSnap(c, "name: some-snap\nversion: 1.0")

	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}, snapPath, "", "edge", snapstate.Flags{})
	c.Assert(err, IsNil)

	var m map[string]interface{}
	runHooks := tasksWithKind(ts, "run-hook")

	c.Assert(taskKinds(runHooks), DeepEquals, []string{
		"run-hook[install]",
		"run-hook[configure]",
		"run-hook[check-health]",
	})
	err = runHooks[1].Get("hook-context", &m)
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]interface{}{"use-defaults": true})
}

func (s *snapmgrTestSuite) TestGadgetDefaultsNotForOS(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", nil)

	s.prepareGadget(c)

	const coreSnapYaml = `
name: core
type: os
version: 1.0
`
	snapPath := makeTestSnap(c, coreSnapYaml)

	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "core", SnapID: "core-id", Revision: snap.R(1)}, snapPath, "", "edge", snapstate.Flags{})
	c.Assert(err, IsNil)

	var m map[string]interface{}
	runHooks := tasksWithKind(ts, "run-hook")

	c.Assert(taskKinds(runHooks), DeepEquals, []string{
		"run-hook[install]",
		"run-hook[configure]",
		"run-hook[check-health]",
	})
	// use-defaults flag is part of hook-context which isn't set
	err = runHooks[1].Get("hook-context", &m)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestGadgetDefaultsAreNormalizedForConfigHook(c *C) {
	var mockGadgetSnapYaml = `
name: canonical-pc
type: gadget
`
	var mockGadgetYaml = []byte(`
defaults:
  otheridididididididididididididi:
    foo:
      bar: baz
      num: 1.305

volumes:
    volume-id:
        bootloader: grub
`)

	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(2)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYaml, 0644)
	c.Assert(err, IsNil)

	gi, err := gadget.ReadInfo(info.MountDir(), nil)
	c.Assert(err, IsNil)
	c.Assert(gi, NotNil)

	snapName := "some-snap"
	hooksup := &hookstate.HookSetup{
		Snap:        snapName,
		Hook:        "configure",
		Optional:    true,
		IgnoreError: false,
		TrackError:  false,
	}

	var contextData map[string]interface{}
	contextData = map[string]interface{}{"patch": gi.Defaults}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(hookstate.HookTask(s.state, "", hooksup, contextData), NotNil)
}
