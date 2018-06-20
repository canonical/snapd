// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"

	// So it registers Configure.
	_ "github.com/snapcore/snapd/overlord/configstate"
)

func TestSnapManager(t *testing.T) { TestingT(t) }

type snapmgrTestSuite struct {
	testutil.BaseTest
	o       *overlord.Overlord
	state   *state.State
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend
	fakeStore   *fakeStore

	user  *auth.UserState
	user2 *auth.UserState
	user3 *auth.UserState
}

func (s *snapmgrTestSuite) settle(c *C) {
	err := s.o.Settle(5 * time.Second)
	c.Assert(err, IsNil)
}

var _ = Suite(&snapmgrTestSuite{})

var fakeRevDateEpoch = time.Date(2018, 1, 0, 0, 0, 0, 0, time.UTC)

func (s *snapmgrTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.o = overlord.Mock()
	s.state = s.o.State()

	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.fakeBackend = &fakeSnappyBackend{}
	s.fakeBackend.emptyContainer = emptyContainer(c)
	s.fakeStore = &fakeStore{
		fakeCurrentProgress: 75,
		fakeTotalProgress:   100,
		fakeBackend:         s.fakeBackend,
		state:               s.state,
	}

	oldSetupInstallHook := snapstate.SetupInstallHook
	oldSetupPreRefreshHook := snapstate.SetupPreRefreshHook
	oldSetupPostRefreshHook := snapstate.SetupPostRefreshHook
	oldSetupRemoveHook := snapstate.SetupRemoveHook
	snapstate.SetupInstallHook = hookstate.SetupInstallHook
	snapstate.SetupPreRefreshHook = hookstate.SetupPreRefreshHook
	snapstate.SetupPostRefreshHook = hookstate.SetupPostRefreshHook
	snapstate.SetupRemoveHook = hookstate.SetupRemoveHook

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers(s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	s.o.AddManager(s.snapmgr)

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(s.fakeBackend.ReadInfo))
	s.BaseTest.AddCleanup(snapstate.MockOpenSnapFile(s.fakeBackend.OpenSnapFile))
	revDate := func(info *snap.Info) time.Time {
		if info.Revision.Local() {
			panic("no local revision should reach revisionDate")
		}
		// for convenience a date derived from the revision
		return fakeRevDateEpoch.AddDate(0, 0, info.Revision.N)
	}
	s.BaseTest.AddCleanup(snapstate.MockRevisionDate(revDate))

	s.BaseTest.AddCleanup(func() {
		snapstate.SetupInstallHook = oldSetupInstallHook
		snapstate.SetupPreRefreshHook = oldSetupPreRefreshHook
		snapstate.SetupPostRefreshHook = oldSetupPostRefreshHook
		snapstate.SetupRemoveHook = oldSetupRemoveHook

		dirs.SetRootDir("/")
	})

	s.state.Lock()
	snapstate.ReplaceStore(s.state, s.fakeStore)
	s.user, err = auth.NewUser(s.state, "username", "email@test.com", "macaroon", []string{"discharge"})
	c.Assert(err, IsNil)
	s.user2, err = auth.NewUser(s.state, "username2", "email2@test.com", "macaroon2", []string{"discharge2"})
	c.Assert(err, IsNil)
	// 3 has no store auth
	s.user3, err = auth.NewUser(s.state, "username3", "email2@test.com", "", nil)
	c.Assert(err, IsNil)

	s.state.Set("seed-time", time.Now())
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})
	s.state.Unlock()

	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	snapstate.Model = func(*state.State) (*asserts.Model, error) {
		return nil, nil
	}
}

func (s *snapmgrTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	snapstate.ValidateRefreshes = nil
	snapstate.AutoAliases = nil
	snapstate.CanAutoRefresh = nil
}

func (s *snapmgrTestSuite) TestKnownTaskKinds(c *C) {
	kinds := s.snapmgr.KnownTaskKinds()
	sort.Strings(kinds)
	c.Assert(kinds, DeepEquals, []string{
		"alias",
		"auto-connect",
		"cleanup",
		"clear-aliases",
		"clear-snap",
		"configure-snapd",
		"copy-snap-data",
		"disable-aliases",
		"discard-conns",
		"discard-snap",
		"download-snap",
		"error-trigger",
		"link-snap",
		"mount-snap",
		"nop",
		"prefer-aliases",
		"prepare-snap",
		"prerequisites",
		"prune-auto-aliases",
		"refresh-aliases",
		"remove-aliases",
		"remove-profiles",
		"run-hook",
		"set-auto-aliases",
		"setup-aliases",
		"setup-profiles",
		"start-snap-services",
		"stop-snap-services",
		"switch-snap",
		"switch-snap-channel",
		"toggle-snap-flags",
		"transition-ubuntu-core",
		"unalias",
		"unlink-current-snap",
		"unlink-snap",
		"validate-snap"})
}

func (s *snapmgrTestSuite) TestStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	sto := &store.Store{}
	snapstate.ReplaceStore(s.state, sto)
	store1 := snapstate.Store(s.state)
	c.Check(store1, Equals, sto)

	// cached
	store2 := snapstate.Store(s.state)
	c.Check(store2, Equals, sto)
}

func (s *snapmgrTestSuite) TestUserFromUserID(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tests := []struct {
		ids     []int
		u       *auth.UserState
		invalid bool
	}{
		{[]int{0}, nil, false},
		{[]int{2}, s.user2, false},
		{[]int{99}, nil, true},
		{[]int{1, 99}, s.user, false},
		{[]int{99, 0}, nil, false},
		{[]int{99, 2}, s.user2, false},
		{[]int{99, 100}, nil, true},
	}

	for _, t := range tests {
		u, err := snapstate.UserFromUserID(s.state, t.ids...)
		c.Check(u, DeepEquals, t.u)
		if t.invalid {
			c.Check(err, Equals, auth.ErrInvalidUser)
		} else {
			c.Check(err, IsNil)
		}
	}
}

const (
	unlinkBefore = 1 << iota
	cleanupAfter
	maybeCore
	runCoreConfigure
)

func taskKinds(tasks []*state.Task) []string {
	kinds := make([]string, len(tasks))
	for i, task := range tasks {
		k := task.Kind()
		if k == "run-hook" {
			var hooksup hookstate.HookSetup
			if err := task.Get("hook-setup", &hooksup); err != nil {
				panic(err)
			}
			k = fmt.Sprintf("%s[%s]", k, hooksup.Hook)
		}
		kinds[i] = k
	}
	return kinds
}

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
	expected = append(expected,
		"copy-snap-data",
		"setup-profiles",
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
	expected = append(expected,
		"run-hook[configure]",
	)

	c.Assert(kinds, DeepEquals, expected)
}

func verifyUpdateTasks(c *C, opts, discards int, ts *state.TaskSet, st *state.State) {
	kinds := taskKinds(ts.Tasks())

	expected := []string{
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
	}
	expected = append(expected, "run-hook[pre-refresh]")
	if opts&unlinkBefore != 0 {
		expected = append(expected,
			"stop-snap-services",
		)
	}
	if opts&unlinkBefore != 0 {
		expected = append(expected,
			"remove-aliases",
			"unlink-current-snap",
		)
	}
	expected = append(expected,
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
	)
	if opts&maybeCore != 0 {
		expected = append(expected, "setup-profiles")
	}
	expected = append(expected,
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook[post-refresh]",
		"start-snap-services")

	c.Assert(ts.Tasks()[len(expected)-2].Summary(), Matches, `Run post-refresh hook of .*`)
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
	expected = append(expected,
		"run-hook[configure]",
	)

	c.Assert(kinds, DeepEquals, expected)
}

func verifyRemoveTasks(c *C, ts *state.TaskSet) {
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"run-hook[remove]",
		"remove-aliases",
		"unlink-snap",
		"remove-profiles",
		"clear-snap",
		"discard-snap",
		"discard-conns",
	})
	verifyStopReason(c, ts, "remove")
}

func (s *snapmgrTestSuite) TestLastIndexFindsLast(c *C) {
	snapst := &snapstate.SnapState{Sequence: []*snap.SideInfo{
		{Revision: snap.R(7)},
		{Revision: snap.R(11)},
		{Revision: snap.R(11)},
	}}
	c.Check(snapst.LastIndex(snap.R(11)), Equals, 2)
}

func (s *snapmgrTestSuite) TestInstallDevModeConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// if a snap is devmode, you can't install it without --devmode
	_, err := snapstate.Install(s.state, "some-snap", "channel-for-devmode", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires devmode or confinement override`)

	// if a snap is devmode, you *can* install it with --devmode
	_, err = snapstate.Install(s.state, "some-snap", "channel-for-devmode", snap.R(0), s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)

	// if a snap is *not* devmode, you can still install it with --devmode
	_, err = snapstate.Install(s.state, "some-snap", "channel-for-strict", snap.R(0), s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallClassicConfinementFiltering(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}

	s.state.Lock()
	defer s.state.Unlock()

	// if a snap is classic, you can't install it without --classic
	_, err := snapstate.Install(s.state, "some-snap", "channel-for-classic", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires classic confinement`)

	// if a snap is classic, you *can* install it with --classic
	_, err = snapstate.Install(s.state, "some-snap", "channel-for-classic", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)

	// if a snap is *not* classic, you can still install it with --classic
	_, err = snapstate.Install(s.state, "some-snap", "channel-for-strict", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
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

	_, err := snapstate.Install(s.state, "some-snap", "channel-for-classic", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, ErrorMatches, "classic confinement requires snaps under /snap or symlink from /snap to "+dirs.SnapMountDir)
}

func (s *snapmgrTestSuite) TestInstallTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
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
version: 1.0`)
	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(8)}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, IsNil)

	runHooks := tasksWithKind(ts, "run-hook")
	// hook tasks for refresh and for configure hook only; no install hook
	c.Assert(runHooks, HasLen, 3)
	c.Assert(runHooks[0].Summary(), Equals, `Run pre-refresh hook of "some-snap" snap if present`)
	c.Assert(runHooks[1].Summary(), Equals, `Run post-refresh hook of "some-snap" snap if present`)
	c.Assert(runHooks[2].Summary(), Equals, `Run configure hook of "some-snap" snap if present`)
}

type fullFlags struct{ before, change, after, setup snapstate.Flags }

func (s *snapmgrTestSuite) testRevertTasksFullFlags(flags fullFlags, c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(7)},
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Flags:    flags.before,
		Current:  snap.R(11),
		SnapType: "app",
	})

	ts, err := snapstate.Revert(s.state, "some-snap", flags.change)
	c.Assert(err, IsNil)

	tasks := ts.Tasks()
	c.Assert(s.state.TaskCount(), Equals, len(tasks))
	c.Assert(taskKinds(tasks), DeepEquals, []string{
		"prerequisites",
		"prepare-snap",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"start-snap-services",
		"run-hook[configure]",
	})
	// a revert is a special refresh
	verifyStopReason(c, ts, "refresh")

	snapsup, err := snapstate.TaskSnapSetup(tasks[0])
	c.Assert(err, IsNil)
	flags.setup.Revert = true
	c.Check(snapsup.Flags, Equals, flags.setup)

	chg := s.state.NewChange("revert", "revert snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Flags, Equals, flags.after)
}

func (s *snapmgrTestSuite) testRevertTasks(flags snapstate.Flags, c *C) {
	s.testRevertTasksFullFlags(fullFlags{before: flags, change: flags, after: flags, setup: flags}, c)
}

func (s *snapmgrTestSuite) TestRevertTasks(c *C) {
	s.testRevertTasks(snapstate.Flags{}, c)
}

func (s *snapmgrTestSuite) TestRevertTasksFromDevMode(c *C) {
	// the snap is installed in devmode, but the request to revert does not specify devmode
	s.testRevertTasksFullFlags(fullFlags{
		before: snapstate.Flags{DevMode: true}, // the snap is installed in devmode
		change: snapstate.Flags{},              // the request to revert does not specify devmode
		after:  snapstate.Flags{DevMode: true}, // the reverted snap is installed in devmode
		setup:  snapstate.Flags{DevMode: true}, // because setup said so
	}, c)
}

func (s *snapmgrTestSuite) TestRevertTasksFromJailMode(c *C) {
	// the snap is installed in jailmode, but the request to revert does not specify jailmode
	s.testRevertTasksFullFlags(fullFlags{
		before: snapstate.Flags{JailMode: true}, // the snap is installed in jailmode
		change: snapstate.Flags{},               // the request to revert does not specify jailmode
		after:  snapstate.Flags{JailMode: true}, // the reverted snap is installed in jailmode
		setup:  snapstate.Flags{JailMode: true}, // because setup said so
	}, c)
}

func (s *snapmgrTestSuite) TestRevertTasksFromClassic(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}

	// the snap is installed in classic, but the request to revert does not specify classic
	s.testRevertTasksFullFlags(fullFlags{
		before: snapstate.Flags{Classic: true}, // the snap is installed in classic
		change: snapstate.Flags{},              // the request to revert does not specify classic
		after:  snapstate.Flags{Classic: true}, // the reverted snap is installed in classic
		setup:  snapstate.Flags{Classic: true}, // because setup said so
	}, c)
}

func (s *snapmgrTestSuite) TestRevertTasksDevMode(c *C) {
	s.testRevertTasks(snapstate.Flags{DevMode: true}, c)
}

func (s *snapmgrTestSuite) TestRevertTasksJailMode(c *C) {
	s.testRevertTasks(snapstate.Flags{JailMode: true}, c)
}

func (s *snapmgrTestSuite) TestRevertTasksClassic(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}
	s.testRevertTasks(snapstate.Flags{Classic: true}, c)
}

func (s *snapmgrTestSuite) TestUpdateCreatesGCTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 2, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s snapmgrTestSuite) TestInstallFailsOnDisabledSnap(c *C) {
	snapst := &snapstate.SnapState{
		Active:   false,
		Channel:  "channel",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}},
		Current:  snap.R(2),
		SnapType: "app",
	}
	snapsup := &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}}
	_, err := snapstate.DoInstall(s.state, snapst, snapsup, 0)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot update disabled snap "some-snap"`)
}

func (s snapmgrTestSuite) TestInstallFailsOnSystem(c *C) {
	snapsup := &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "system", SnapID: "some-snap-id", Revision: snap.R(1)}}
	_, err := snapstate.DoInstall(s.state, nil, snapsup, 0)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot install reserved snap name 'system'`)
}

func (s *snapmgrTestSuite) TestUpdateCreatesDiscardAfterCurrentTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 3, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, tts, err := snapstate.UpdateMany(context.TODO(), s.state, nil, 0)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 1)
	c.Check(updates, DeepEquals, []string{"some-snap"})

	ts := tts[0]
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 3, ts, s.state)

	// check that the tasks are in non-default lane
	for _, t := range ts.Tasks() {
		c.Assert(t.Lanes(), DeepEquals, []int{1})
	}
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateManyDevModeConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-devmode",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// updated snap is devmode, updatemany doesn't update it
	_, tts, _ := snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap"}, s.user.ID)
	// FIXME: UpdateMany will not error out in this case (daemon catches this case, with a weird error)
	c.Assert(tts, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyClassicConfinementFiltering(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-classic",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// if a snap installed without --classic gets a classic update it isn't installed
	_, tts, _ := snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap"}, s.user.ID)
	// FIXME: UpdateMany will not error out in this case (daemon catches this case, with a weird error)
	c.Assert(tts, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyClassic(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-classic",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
		Flags:    snapstate.Flags{Classic: true},
	})

	// snap installed with classic: refresh gets classic
	_, tts, err := snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap"}, s.user.ID)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 1)
}

func (s *snapmgrTestSuite) TestUpdateManyDevMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Flags:  snapstate.Flags{DevMode: true},
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap"}, 0)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 1)
}

func (s *snapmgrTestSuite) TestUpdateAllDevMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Flags:  snapstate.Flags{DevMode: true},
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.TODO(), s.state, nil, 0)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 0)
}

func (s *snapmgrTestSuite) TestByKindOrder(c *C) {
	core := &snap.Info{Type: snap.TypeOS}
	base := &snap.Info{Type: snap.TypeBase}
	app := &snap.Info{Type: snap.TypeApp}

	c.Check(snapstate.ByKindOrder(base, core), DeepEquals, []*snap.Info{core, base})
	c.Check(snapstate.ByKindOrder(app, core), DeepEquals, []*snap.Info{core, app})
	c.Check(snapstate.ByKindOrder(app, base), DeepEquals, []*snap.Info{base, app})
	c.Check(snapstate.ByKindOrder(app, base, core), DeepEquals, []*snap.Info{core, base, app})
	c.Check(snapstate.ByKindOrder(app, core, base), DeepEquals, []*snap.Info{core, base, app})
}

func (s *snapmgrTestSuite) TestUpdateManyWaitForBases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})

	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
		Channel:  "channel-for-base",
	})

	updates, tts, err := snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap", "core", "some-base"}, 0)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 3)
	c.Check(updates, HasLen, 3)

	// to make TaskSnapSetup work
	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	prereqTotal := len(tts[0].Tasks()) + len(tts[1].Tasks())
	prereqs := map[string]bool{}
	for i, task := range tts[2].Tasks() {
		waitTasks := task.WaitTasks()
		if i == 0 {
			c.Check(len(waitTasks), Equals, prereqTotal)
		} else if task.Kind() == "link-snap" {
			c.Check(len(waitTasks), Equals, prereqTotal+1)
			for _, pre := range waitTasks {
				if pre.Kind() == "link-snap" {
					snapsup, err := snapstate.TaskSnapSetup(pre)
					c.Assert(err, IsNil)
					prereqs[snapsup.Name()] = true
				}
			}
		}
	}

	c.Check(prereqs, DeepEquals, map[string]bool{
		"core":      true,
		"some-base": true,
	})
}

func (s *snapmgrTestSuite) TestUpdateManyValidateRefreshes(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	validateCalled := false
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int) ([]*snap.Info, error) {
		validateCalled = true
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	updates, tts, err := snapstate.UpdateMany(context.TODO(), s.state, nil, 0)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 1)
	c.Check(updates, DeepEquals, []string{"some-snap"})
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 0, tts[0], s.state)

	c.Check(validateCalled, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateManyValidateRefreshesUnhappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	// refresh all => no error
	updates, tts, err := snapstate.UpdateMany(context.TODO(), s.state, nil, 0)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 0)
	c.Check(updates, HasLen, 0)

	// refresh some-snap => report error
	updates, tts, err = snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap"}, 0)
	c.Assert(err, Equals, validateErr)
	c.Check(tts, HasLen, 0)
	c.Check(updates, HasLen, 0)

}

func (s *snapmgrTestSuite) TestRevertCreatesNoGCTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(1)},
			{RealName: "some-snap", Revision: snap.R(2)},
			{RealName: "some-snap", Revision: snap.R(3)},
			{RealName: "some-snap", Revision: snap.R(4)},
		},
		Current: snap.R(2),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R(4), snapstate.Flags{})
	c.Assert(err, IsNil)

	// ensure that we do not run any form of garbage-collection
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prerequisites",
		"prepare-snap",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"start-snap-services",
		"run-hook[configure]",
	})
}

func (s *snapmgrTestSuite) TestEnableTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prepare-snap",
		"setup-profiles",
		"link-snap",
		"setup-aliases",
		"start-snap-services",
	})
}

func (s *snapmgrTestSuite) TestSwitchTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	ts, err := snapstate.Switch(s.state, "some-snap", "some-channel")
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{"switch-snap"})
}

func (s *snapmgrTestSuite) TestSwitchConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	ts, err := snapstate.Switch(s.state, "some-snap", "some-channel")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("switch-snap", "...").AddAll(ts)

	_, err = snapstate.Switch(s.state, "some-snap", "other-channel")
	c.Check(err, ErrorMatches, `snap "some-snap" has "switch-snap" change in progress`)
}

func (s *snapmgrTestSuite) TestSwitchUnhappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	_, err := snapstate.Switch(s.state, "non-existing-snap", "some-channel")
	c.Assert(err, ErrorMatches, `snap "non-existing-snap" is not installed`)
}

func (s *snapmgrTestSuite) TestDisableTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"remove-aliases",
		"unlink-snap",
		"remove-profiles",
	})
	verifyStopReason(c, ts, "disable")
}

func (s *snapmgrTestSuite) TestEnableConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("enable", "...").AddAll(ts)

	_, err = snapstate.Enable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" has "enable" change in progress`)
}

func (s *snapmgrTestSuite) TestDisableConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	_, err = snapstate.Disable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" has "install" change in progress`)
}

func (s *snapmgrTestSuite) TestDoInstallChannelDefault(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "stable")
}

func (s *snapmgrTestSuite) TestInstallRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "", snap.R(7), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Revision(), Equals, snap.R(7))
}

func (s *snapmgrTestSuite) TestInstallConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	_, err = snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
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

	_, err := snapstate.Install(s.state, "foo", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "foo" command namespace conflicts with alias "foo\.bar" for "otherfoosnap" snap`)
}

// A sneakyStore changes the state when called
type sneakyStore struct {
	*fakeStore
	state *state.State
}

func (s sneakyStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, error) {
	s.state.Lock()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "app",
	})
	s.state.Unlock()
	return s.fakeStore.SnapAction(ctx, currentSnaps, actions, user, opts)
}

func (s *snapmgrTestSuite) TestInstallStateConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, sneakyStore{fakeStore: s.fakeStore, state: s.state})

	_, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" state changed during install preparations`)
}

func (s *snapmgrTestSuite) TestInstallPathConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, err = snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "install" change in progress`)
}

func (s *snapmgrTestSuite) TestInstallPathMissingName(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, err := snapstate.InstallPath(s.state, &snap.SideInfo{}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`internal error: snap name to install %q not provided`, mockSnap))
}

func (s *snapmgrTestSuite) TestInstallPathSnapIDRevisionUnset(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "snapididid"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`internal error: snap id set to install %q but revision is unset`, mockSnap))
}

func (s *snapmgrTestSuite) TestUpdateTasksPropagatesErrors(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "fakestore-please-error-on-refresh", Revision: snap.R(7)}},
		Current:  snap.R(7),
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `failing as requested`)
}

func (s *snapmgrTestSuite) TestUpdateTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	validateCalled := false
	happyValidateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int) ([]*snap.Info, error) {
		validateCalled = true
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = happyValidateRefreshes

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))

	c.Check(validateCalled, Equals, true)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "some-channel")
}

func (s *snapmgrTestSuite) TestUpdateTasksCoreSetsIgnoreOnConfigure(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "os",
	})

	oldConfigure := snapstate.Configure
	defer func() { snapstate.Configure = oldConfigure }()

	var configureFlags int
	snapstate.Configure = func(st *state.State, snapName string, patch map[string]interface{}, flags int) *state.TaskSet {
		configureFlags = flags
		return state.NewTaskSet()
	}

	_, err := snapstate.Update(s.state, "core", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// ensure the core snap sets the "ignore-hook-error" flag
	c.Check(configureFlags&snapstate.IgnoreHookError, Equals, 1)
}

func (s *snapmgrTestSuite) TestUpdateDevModeConfinementFiltering(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-devmode",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// updated snap is devmode, refresh without --devmode, do nothing
	// TODO: better error message here
	_, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires devmode or confinement override`)

	// updated snap is devmode, refresh with --devmode
	_, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateClassicConfinementFiltering(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-classic",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// updated snap is classic, refresh without --classic, do nothing
	// TODO: better error message here
	_, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires classic confinement`)

	// updated snap is classic, refresh with --classic
	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snap is in classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Classic, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateClassicFromClassic(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel-for-classic",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
		Flags:    snapstate.Flags{Classic: true},
	})

	// snap installed with --classic, update needs classic, refresh with --classic works
	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	// devmode overrides the snapsetup classic flag
	ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, false)

	// jailmode overrides it too (you need to provide both)
	ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{JailMode: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, false)

	// jailmode and classic together gets you both
	ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{JailMode: true, Classic: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	// snap installed with --classic, update needs classic, refresh without --classic works
	ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snap is in classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Classic, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateStrictFromClassic(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "channel",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
		Flags:    snapstate.Flags{Classic: true},
	})

	// snap installed with --classic, update does not need classic, refresh works without --classic
	_, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// snap installed with --classic, update does not need classic, refresh works with --classic
	_, err = snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateChannelFallback(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "edge")
}

func (s *snapmgrTestSuite) TestUpdateConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("refresh", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "refresh" change in progress`)
}

func (s *snapmgrTestSuite) testChangeConflict(c *C, kind string) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "producer", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "producer", SnapID: "producer-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "consumer", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "consumer", SnapID: "consumer-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := s.state.NewChange("another change", "...")
	t := s.state.NewTask(kind, "...")
	t.Set("slot", interfaces.SlotRef{Snap: "producer", Name: "slot"})
	t.Set("plug", interfaces.PlugRef{Snap: "consumer", Name: "plug"})
	chg.AddTask(t)

	_, err := snapstate.Update(s.state, "producer", "some-channel", snap.R(2), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "producer" has "another change" change in progress`)

	_, err = snapstate.Update(s.state, "consumer", "some-channel", snap.R(2), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "consumer" has "another change" change in progress`)
}

func (s *snapmgrTestSuite) TestUpdateConflictWithConnect(c *C) {
	s.testChangeConflict(c, "connect")
}

func (s *snapmgrTestSuite) TestUpdateConflictWithDisconnect(c *C) {
	s.testChangeConflict(c, "disconnect")
}

func (s *snapmgrTestSuite) TestRemoveTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
		},
		Current: snap.R(11),
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(0))
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	verifyRemoveTasks(c, ts)
}

func (s *snapmgrTestSuite) TestRemoveHookNotExecutedIfNotLastRevison(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
			{RealName: "foo", Revision: snap.R(12)},
		},
		Current: snap.R(12),
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(11))
	c.Assert(err, IsNil)

	runHooks := tasksWithKind(ts, "run-hook")
	// no 'remove' hook task
	c.Assert(runHooks, HasLen, 0)
}

func (s *snapmgrTestSuite) TestRemoveConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", Revision: snap.R(11)}},
		Current:  snap.R(11),
	})

	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("remove", "...").AddAll(ts)

	_, err = snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, ErrorMatches, `snap "some-snap" has "remove" change in progress`)
}

func (s *snapmgrTestSuite) TestInstallRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(snapstate.Installing(s.state), Equals, false)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
	}})
	expected := fakeOps{
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:  "install",
				Name:    "some-snap",
				Channel: "some-channel",
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
			name: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
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
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
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

	// check link/start snap summary
	linkTask := ta[len(ta)-7]
	c.Check(linkTask.Summary(), Equals, `Make snap "some-snap" (11) available to the system`)
	startTask := ta[len(ta)-2]
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
		},
		SideInfo: snapsup.SideInfo,
		Type:     snap.TypeApp,
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
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "some-channel")
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "some-channel",
		Revision: snap.R(11),
	})
	c.Assert(snapst.Required, Equals, false)
}

func (s *snapmgrTestSuite) TestInstallWithRevisionRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(42), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(snapstate.Installing(s.state), Equals, false)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
	}})
	expected := fakeOps{
		{
			op:     "storesvc-snap-action",
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:   "install",
				Name:     "some-snap",
				Revision: snap.R(42),
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
			name: filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Revision: snap.R(42),
			},
		},
		{
			op:    "setup-snap",
			name:  filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
			revno: snap.R(42),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/42"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
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
			name: filepath.Join(dirs.SnapMountDir, "some-snap/42"),
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
	linkTask := ta[len(ta)-7]
	c.Check(linkTask.Summary(), Equals, `Make snap "some-snap" (42) available to the system`)
	startTask := ta[len(ta)-2]
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
		},
		SideInfo: snapsup.SideInfo,
		Type:     snap.TypeApp,
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
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "some-channel")
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(42),
	})
	c.Assert(snapst.Required, Equals, false)
}

func (s *snapmgrTestSuite) TestInstalling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	c.Check(snapstate.Installing(s.state), Equals, false)

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(42), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	c.Check(snapstate.Installing(s.state), Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateRunThrough(c *C) {
	// use services-snap here to make sure services would be stopped/started appropriately
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(7),
		SnapID:   "services-snap-id",
	}
	snaptest.MockSnap(c, `name: services-snap`, &si)
	fi, err := os.Stat(snap.MountFile("services-snap", si.Revision))
	c.Assert(err, IsNil)
	refreshedDate := fi.ModTime()
	// look at disk
	r := snapstate.MockRevisionDate(nil)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
		Channel:  "stable",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				Name:            "services-snap",
				SnapID:          "services-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "stable",
				RefreshedDate:   refreshedDate,
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:  "refresh",
				SnapID:  "services-snap-id",
				Channel: "some-channel",
				Flags:   store.SnapActionEnforceValidation,
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "services-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "services-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op:   "open-snap-file",
			name: filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "services-snap",
				SnapID:   "services-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "stop-snap-services:refresh",
			name: filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op:   "remove-snap-aliases",
			name: "services-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "services-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "services-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "services-snap",
				SnapID:   "services-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "services-snap/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "services-snap",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:   "start-snap-services",
			name: filepath.Join(dirs.SnapMountDir, "services-snap/11"),
		},
		{
			op:    "cleanup-trash",
			name:  "services-snap",
			revno: snap.R(11),
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "services-snap",
	}})
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	task := ts.Tasks()[1]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel: "some-channel",
		UserID:  s.user.ID,

		SnapPath: filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo: snapsup.SideInfo,
		Type:     snap.TypeApp,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(11),
		Channel:  "some-channel",
		SnapID:   "services-snap-id",
	})

	// verify services stop reason
	verifyStopReason(c, ts, "refresh")

	// check post-refresh hook
	task = ts.Tasks()[14]
	c.Assert(task.Kind(), Equals, "run-hook")
	c.Assert(task.Summary(), Matches, `Run post-refresh hook of "services-snap" snap if present`)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "services-snap",
		SnapID:   "services-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "services-snap",
		Channel:  "some-channel",
		SnapID:   "services-snap-id",
		Revision: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateWithNewBase(c *C) {
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(7),
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-base", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "some-base"},
		{macaroon: s.user.StoreMacaroon, name: "some-snap"},
	})
}

func (s *snapmgrTestSuite) TestUpdateWithAlreadyInstalledBase(c *C) {
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(7),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active:  true,
		Channel: "stable",
		Sequence: []*snap.SideInfo{{
			RealName: "some-base",
			SnapID:   "some-base-id",
			Revision: snap.R(1),
		}},
		Current:  snap.R(1),
		SnapType: "base",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-base", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "some-snap"},
	})
}

func (s *snapmgrTestSuite) TestUpdateWithNewDefaultProvider(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})
	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	si := &snap.SideInfo{
		RealName: "snap-content-plug",
		SnapID:   "snap-content-plug-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: snap-content-plug`, si)
	snapstate.Set(s.state, "snap-content-plug", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(7),
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "snap-content-plug", "stable", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "snap-content-plug"},
		{macaroon: s.user.StoreMacaroon, name: "snap-content-slot"},
	})
}

func (s *snapmgrTestSuite) TestUpdateWithInstalledDefaultProvider(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})
	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	si := &snap.SideInfo{
		RealName: "snap-content-plug",
		SnapID:   "snap-content-plug-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: snap-content-plug`, si)
	snapstate.Set(s.state, "snap-content-plug", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(7),
		SnapType: "app",
	})
	snapstate.Set(s.state, "snap-content-slot", &snapstate.SnapState{
		Active:  true,
		Channel: "stable",
		Sequence: []*snap.SideInfo{{
			RealName: "snap-content-slot",
			SnapID:   "snap-content-slot-id",
			Revision: snap.R(1),
		}},
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "snap-content-plug", "stable", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "snap-content-plug"},
	})
}

func (s *snapmgrTestSuite) TestUpdateRememberedUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action":
			c.Check(op.userID, Equals, 1)
		case "storesvc-download":
			snapName := op.name
			c.Check(s.fakeStore.downloads[0], DeepEquals, fakeDownload{
				macaroon: "macaroon",
				name:     "some-snap",
			}, Commentf(snapName))
		}
	}
}

func (s *snapmgrTestSuite) TestUpdateToRevisionRememberedUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(11), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action:action":
			c.Check(op.userID, Equals, 1)
		case "storesvc-download":
			snapName := op.name
			c.Check(s.fakeStore.downloads[0], DeepEquals, fakeDownload{
				macaroon: "macaroon",
				name:     "some-snap",
			}, Commentf(snapName))
		}
	}
}

func (s *snapmgrTestSuite) TestUpdateManyMultipleCredsNoUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(2), SnapID: "services-snap-id"},
		},
		Current:  snap.R(2),
		SnapType: "app",
		UserID:   2,
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	// no user is passed to use for UpdateMany
	updated, tts, err := snapstate.UpdateMany(context.TODO(), s.state, nil, 0)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	macaroonMap := map[string]string{
		"core":          "",
		"some-snap":     "macaroon",
		"services-snap": "macaroon2",
	}

	seen := make(map[string]int)
	ir := 0
	di := 0
	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action":
			ir++
			c.Check(op.curSnaps, DeepEquals, []store.CurrentSnap{
				{Name: "core", SnapID: "core-snap-id", Revision: snap.R(1), RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1)},
				{Name: "services-snap", SnapID: "services-snap-id", Revision: snap.R(2), RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 2)},
				{Name: "some-snap", SnapID: "some-snap-id", Revision: snap.R(5), RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 5)},
			})
		case "storesvc-snap-action:action":
			snapID := op.action.SnapID
			seen[snapID] = op.userID
		case "storesvc-download":
			snapName := op.name
			c.Check(s.fakeStore.downloads[di], DeepEquals, fakeDownload{
				macaroon: macaroonMap[snapName],
				name:     snapName,
			}, Commentf(snapName))
			di++
		}
	}
	c.Check(ir, Equals, 2)
	// we check all snaps with each user
	c.Check(seen["some-snap-id"], Equals, 1)
	c.Check(seen["services-snap-id"], Equals, 2)
	// coalesced with one of the others
	c.Check(seen["core-snap-id"] > 0, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateManyMultipleCredsUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(2), SnapID: "services-snap-id"},
		},
		Current:  snap.R(2),
		SnapType: "app",
		UserID:   2,
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	// do UpdateMany using user 2 as fallback
	updated, tts, err := snapstate.UpdateMany(context.TODO(), s.state, nil, 2)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	macaroonMap := map[string]string{
		"core":          "macaroon2",
		"some-snap":     "macaroon",
		"services-snap": "macaroon2",
	}

	type snapIDuserID struct {
		snapID string
		userID int
	}
	seen := make(map[snapIDuserID]bool)
	ir := 0
	di := 0
	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action":
			ir++
			c.Check(op.curSnaps, DeepEquals, []store.CurrentSnap{
				{Name: "core", SnapID: "core-snap-id", Revision: snap.R(1), RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1)},
				{Name: "services-snap", SnapID: "services-snap-id", Revision: snap.R(2), RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 2)},
				{Name: "some-snap", SnapID: "some-snap-id", Revision: snap.R(5), RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 5)},
			})
		case "storesvc-snap-action:action":
			snapID := op.action.SnapID
			seen[snapIDuserID{snapID: snapID, userID: op.userID}] = true
		case "storesvc-download":
			snapName := op.name
			c.Check(s.fakeStore.downloads[di], DeepEquals, fakeDownload{
				macaroon: macaroonMap[snapName],
				name:     snapName,
			}, Commentf(snapName))
			di++
		}
	}
	c.Check(ir, Equals, 2)
	// we check all snaps with each user
	c.Check(seen, DeepEquals, map[snapIDuserID]bool{
		{snapID: "core-snap-id", userID: 2}:     true,
		{snapID: "some-snap-id", userID: 1}:     true,
		{snapID: "services-snap-id", userID: 2}: true,
	})

	var coreState, snapState snapstate.SnapState
	// user in SnapState was preserved
	err = snapstate.Get(s.state, "some-snap", &snapState)
	c.Assert(err, IsNil)
	c.Check(snapState.UserID, Equals, 1)
	c.Check(snapState.Current, DeepEquals, snap.R(11))

	// user in SnapState was set
	err = snapstate.Get(s.state, "core", &coreState)
	c.Assert(err, IsNil)
	c.Check(coreState.UserID, Equals, 2)
	c.Check(coreState.Current, DeepEquals, snap.R(11))

}

func (s *snapmgrTestSuite) TestUpdateManyMultipleCredsUserWithNoStoreAuthRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(2), SnapID: "services-snap-id"},
		},
		Current:  snap.R(2),
		SnapType: "app",
		UserID:   3,
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	// no user is passed to use for UpdateMany
	updated, tts, err := snapstate.UpdateMany(context.TODO(), s.state, nil, 0)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	macaroonMap := map[string]string{
		"core":          "",
		"some-snap":     "macaroon",
		"services-snap": "",
	}

	seen := make(map[string]int)
	ir := 0
	di := 0
	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action":
			ir++
			c.Check(op.curSnaps, DeepEquals, []store.CurrentSnap{
				{Name: "core", SnapID: "core-snap-id", Revision: snap.R(1), RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1)},
				{Name: "services-snap", SnapID: "services-snap-id", Revision: snap.R(2), RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 2)},
				{Name: "some-snap", SnapID: "some-snap-id", Revision: snap.R(5), RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 5)},
			})
		case "storesvc-snap-action:action":
			snapID := op.action.SnapID
			seen[snapID] = op.userID
		case "storesvc-download":
			snapName := op.name
			c.Check(s.fakeStore.downloads[di], DeepEquals, fakeDownload{
				macaroon: macaroonMap[snapName],
				name:     snapName,
			}, Commentf(snapName))
			di++
		}
	}
	c.Check(ir, Equals, 1)
	// we check all snaps with each user
	c.Check(seen["some-snap-id"], Equals, 1)
	// coalesced with request for 1
	c.Check(seen["services-snap-id"], Equals, 1)
	c.Check(seen["core-snap-id"], Equals, 1)
}

func (s *snapmgrTestSuite) TestUpdateUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/some-snap/11")

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				Name:          "some-snap",
				SnapID:        "some-snap-id",
				Revision:      snap.R(7),
				RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:  "refresh",
				SnapID:  "some-snap-id",
				Channel: "some-channel",
				Flags:   store.SnapActionEnforceValidation,
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
			old: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "open-snap-file",
			name: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "setup-profiles:Doing",
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
			op:   "link-snap.failed",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "undo-setup-snap",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			stype: "app",
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
	}})
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateTotalUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "stable",
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
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
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				Name:            "some-snap",
				SnapID:          "some-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "stable",
				RefreshedDate:   fakeRevDateEpoch.AddDate(0, 0, 7),
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:  "refresh",
				SnapID:  "some-snap-id",
				Channel: "some-channel",
				Flags:   store.SnapActionEnforceValidation,
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
			old: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "open-snap-file",
			name: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "setup-profiles:Doing",
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
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		// undoing everything from here down...
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "undo-setup-snap",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			stype: "app",
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
	}})
	// friendlier failure first
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "stable")
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateSameRevision(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "channel-for-7",
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, store.ErrNoUpdateAvailable)
}

// A noResultsStore returns no results for install/refresh requests
type noResultsStore struct {
	*fakeStore
}

func (n noResultsStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, error) {
	return nil, &store.SnapActionError{NoResults: true}
}

func (s *snapmgrTestSuite) TestUpdateNoStoreResults(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, noResultsStore{fakeStore: s.fakeStore})

	// this is an atypical case in which the store didn't return
	// an error nor a result, we are defensive and return
	// a reasonable error
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "channel-for-7",
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, store.ErrNoUpdateAvailable)
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionSwitchesChannel(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "other-chanenl",
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "switch-snap-channel")
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionSwitchesChannelConflict(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "other-channel",
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// make it visible
	s.state.NewChange("refresh", "refresh a snap").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Check(err, ErrorMatches, `snap "some-snap" has "refresh" change in progress`)
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionSwitchChannelRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "other-channel",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "other-channel",
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		// we just expect the "storesvc-snap-action" ops, we
		// don't have a fakeOp for switchChannel because it has
		// not a backend method, it just manipulates the state
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				Name:            "some-snap",
				SnapID:          "some-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "other-channel",
				RefreshedDate:   fakeRevDateEpoch.AddDate(0, 0, 7),
			}},
			userID: 1,
		},

		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:  "refresh",
				SnapID:  "some-snap-id",
				Channel: "channel-for-7",
				Flags:   store.SnapActionEnforceValidation,
			},
			userID: 1,
		},
	}

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel:  "channel-for-7",
		SideInfo: snapsup.SideInfo,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "channel-for-7",
		Revision: snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionToggleIgnoreValidation(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "channel-for-7",
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)
	c.Check(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "toggle-snap-flags")
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionToggleIgnoreValidationConflict(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "channel-for-7",
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)
	// make it visible
	s.state.NewChange("refresh", "refresh a snap").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Check(err, ErrorMatches, `snap "some-snap" has "refresh" change in progress`)

}

func (s *snapmgrTestSuite) TestUpdateSameRevisionToggleIgnoreValidationRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "channel-for-7",
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup, DeepEquals, snapstate.SnapSetup{
		SideInfo: snapsup.SideInfo,
		Flags: snapstate.Flags{
			IgnoreValidation: true,
		},
	})
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "channel-for-7",
		Revision: snap.R(7),
	})
	c.Check(snapst.IgnoreValidation, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateValidateRefreshesSaysNo(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	_, err := snapstate.Update(s.state, "some-snap", "stable", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, validateErr)
}

func (s *snapmgrTestSuite) TestUpdateValidateRefreshesSaysNoButIgnoreValidationIsSet(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int) ([]*snap.Info, error) {
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	flags := snapstate.Flags{JailMode: true, IgnoreValidation: true}
	ts, err := snapstate.Update(s.state, "some-snap", "stable", snap.R(0), s.user.ID, flags)
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags, DeepEquals, flags.ForSnapSetup())
}

func (s *snapmgrTestSuite) TestUpdateIgnoreValidationSticky(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	validateErr := errors.New("refresh control error")
	validateRefreshesFail := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		if len(ignoreValidation) == 0 {
			return nil, validateErr
		}
		c.Check(ignoreValidation, DeepEquals, map[string]bool{
			"some-snap-id": true,
		})
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshesFail

	flags := snapstate.Flags{IgnoreValidation: true}
	ts, err := snapstate.Update(s.state, "some-snap", "stable", snap.R(0), s.user.ID, flags)
	c.Assert(err, IsNil)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			Name:             "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(7),
			IgnoreValidation: false,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 7),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(11),
		action: store.SnapAction{
			Action:  "refresh",
			SnapID:  "some-snap-id",
			Channel: "stable",
			Flags:   store.SnapActionIgnoreValidation,
		},
		userID: 1,
	})

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snap has IgnoreValidation set
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(11))

	s.fakeBackend.ops = nil
	s.fakeStore.refreshRevnos = map[string]snap.Revision{
		"some-snap-id": snap.R(12),
	}
	_, tts, err := snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap"}, s.user.ID)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 1)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			Name:             "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(11),
			TrackingChannel:  "stable",
			IgnoreValidation: true,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 11),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(12),
		action: store.SnapAction{
			Action: "refresh",
			SnapID: "some-snap-id",
			Flags:  0,
		},
		userID: 1,
	})

	chg = s.state.NewChange("refresh", "refresh snaps")
	chg.AddAll(tts[0])

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	snapst = snapstate.SnapState{}
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(12))

	// reset ignore validation
	s.fakeBackend.ops = nil
	s.fakeStore.refreshRevnos = map[string]snap.Revision{
		"some-snap-id": snap.R(11),
	}
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int) ([]*snap.Info, error) {
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes
	flags = snapstate.Flags{}
	ts, err = snapstate.Update(s.state, "some-snap", "stable", snap.R(0), s.user.ID, flags)
	c.Assert(err, IsNil)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			Name:             "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(12),
			TrackingChannel:  "stable",
			IgnoreValidation: true,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 12),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(11),
		action: store.SnapAction{
			Action:  "refresh",
			SnapID:  "some-snap-id",
			Channel: "stable",
			Flags:   store.SnapActionEnforceValidation,
		},
		userID: 1,
	})

	chg = s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	snapst = snapstate.SnapState{}
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, false)
	c.Check(snapst.Current, Equals, snap.R(11))
}

func (s *snapmgrTestSuite) TestUpdateFromLocal(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R("x1"),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "channel-for-7",
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, store.ErrLocalSnap)
}

func (s *snapmgrTestSuite) TestUpdateAmend(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R("x1"),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "channel-for-7",
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", snap.R(0), s.user.ID, snapstate.Flags{Amend: true})
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 0, ts, s.state)

	// ensure we go from local to store revision-7
	var snapsup snapstate.SnapSetup
	tasks := ts.Tasks()
	c.Check(tasks[1].Kind(), Equals, "download-snap")
	err = tasks[1].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, snap.R(7))
}

func (s *snapmgrTestSuite) TestUpdateAmendSnapNotFound(c *C) {
	si := snap.SideInfo{
		RealName: "snap-unknown",
		Revision: snap.R("x1"),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snap-unknown", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "stable",
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "snap-unknown", "stable", snap.R(0), s.user.ID, snapstate.Flags{Amend: true})
	c.Assert(err, Equals, store.ErrSnapNotFound)
}

func (s *snapmgrTestSuite) TestSingleUpdateBlockedRevision(c *C) {
	// single updates should *not* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			Name:          "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
		}},
		userID: 1,
	})
}

func (s *snapmgrTestSuite) TestMultiUpdateBlockedRevision(c *C) {
	// multi-updates should *not* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap"}, s.user.ID)
	c.Assert(err, IsNil)
	c.Check(updates, DeepEquals, []string{"some-snap"})

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			Name:          "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
		}},
		userID: 1,
	})
}

func (s *snapmgrTestSuite) TestAllUpdateBlockedRevision(c *C) {
	//  update-all *should* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
	})

	updates, _, err := snapstate.UpdateMany(context.TODO(), s.state, nil, s.user.ID)
	c.Check(err, IsNil)
	c.Check(updates, HasLen, 0)

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			Name:          "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
			Block:         []snap.Revision{snap.R(11)},
		}},
		userID: 1,
	})
}

var orthogonalAutoAliasesScenarios = []struct {
	aliasesBefore map[string][]string
	names         []string
	prune         []string
	update        bool
	new           bool
}{
	{nil, nil, nil, true, true},
	{nil, []string{"some-snap"}, nil, true, false},
	{nil, []string{"other-snap"}, nil, false, true},
	{map[string][]string{"some-snap": {"aliasA", "aliasC"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"other-snap": {"aliasB", "aliasC"}}, []string{"other-snap"}, []string{"other-snap"}, false, false},
	{map[string][]string{"other-snap": {"aliasB", "aliasC"}}, nil, []string{"other-snap"}, true, false},
	{map[string][]string{"other-snap": {"aliasB", "aliasC"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"other-snap": {"aliasC"}}, []string{"other-snap"}, []string{"other-snap"}, false, true},
	{map[string][]string{"other-snap": {"aliasC"}}, nil, []string{"other-snap"}, true, true},
	{map[string][]string{"other-snap": {"aliasC"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"some-snap": {"aliasB"}, "other-snap": {"aliasA"}}, []string{"some-snap"}, []string{"other-snap"}, true, false},
	{map[string][]string{"some-snap": {"aliasB"}, "other-snap": {"aliasA"}}, nil, []string{"other-snap", "some-snap"}, true, true},
	{map[string][]string{"some-snap": {"aliasB"}, "other-snap": {"aliasA"}}, []string{"other-snap"}, []string{"other-snap", "some-snap"}, false, true},
	{map[string][]string{"some-snap": {"aliasB"}}, nil, []string{"some-snap"}, true, true},
	{map[string][]string{"some-snap": {"aliasB"}}, []string{"other-snap"}, []string{"some-snap"}, false, true},
	{map[string][]string{"some-snap": {"aliasB"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"other-snap": {"aliasA"}}, nil, []string{"other-snap"}, true, true},
	{map[string][]string{"other-snap": {"aliasA"}}, []string{"other-snap"}, []string{"other-snap"}, false, true},
	{map[string][]string{"other-snap": {"aliasA"}}, []string{"some-snap"}, []string{"other-snap"}, true, false},
}

func (s *snapmgrTestSuite) TestUpdateManyAutoAliasesScenarios(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", SnapID: "other-snap-id", Revision: snap.R(2)},
		},
		Current:  snap.R(2),
		SnapType: "app",
	})

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		switch info.Name() {
		case "some-snap":
			return map[string]string{"aliasA": "cmdA"}, nil
		case "other-snap":
			return map[string]string{"aliasB": "cmdB"}, nil
		}
		return nil, nil
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	expectedSet := func(aliases []string) map[string]bool {
		res := make(map[string]bool, len(aliases))
		for _, alias := range aliases {
			res[alias] = true
		}
		return res
	}

	for _, scenario := range orthogonalAutoAliasesScenarios {
		for _, snapName := range []string{"some-snap", "other-snap"} {
			var snapst snapstate.SnapState
			err := snapstate.Get(s.state, snapName, &snapst)
			c.Assert(err, IsNil)
			snapst.Aliases = nil
			snapst.AutoAliasesDisabled = false
			if autoAliases := scenario.aliasesBefore[snapName]; autoAliases != nil {
				targets := make(map[string]*snapstate.AliasTarget)
				for _, alias := range autoAliases {
					targets[alias] = &snapstate.AliasTarget{Auto: "cmd" + alias[len(alias)-1:]}
				}

				snapst.Aliases = targets
			}
			snapstate.Set(s.state, snapName, &snapst)
		}

		updates, tts, err := snapstate.UpdateMany(context.TODO(), s.state, scenario.names, s.user.ID)
		c.Check(err, IsNil)

		_, dropped, err := snapstate.AutoAliasesDelta(s.state, []string{"some-snap", "other-snap"})
		c.Assert(err, IsNil)

		j := 0
		expectedUpdatesSet := make(map[string]bool)
		var expectedPruned map[string]map[string]bool
		var pruneTs *state.TaskSet
		if len(scenario.prune) != 0 {
			pruneTs = tts[0]
			j++
			taskAliases := make(map[string]map[string]bool)
			for _, aliasTask := range pruneTs.Tasks() {
				c.Check(aliasTask.Kind(), Equals, "prune-auto-aliases")
				var aliases []string
				err := aliasTask.Get("aliases", &aliases)
				c.Assert(err, IsNil)
				snapsup, err := snapstate.TaskSnapSetup(aliasTask)
				c.Assert(err, IsNil)
				taskAliases[snapsup.Name()] = expectedSet(aliases)
			}
			expectedPruned = make(map[string]map[string]bool)
			for _, snapName := range scenario.prune {
				expectedPruned[snapName] = expectedSet(dropped[snapName])
				if snapName == "other-snap" && !scenario.new && !scenario.update {
					expectedUpdatesSet["other-snap"] = true
				}
			}
			c.Check(taskAliases, DeepEquals, expectedPruned)
		}
		if scenario.update {
			updateTs := tts[j]
			j++
			expectedUpdatesSet["some-snap"] = true
			first := updateTs.Tasks()[0]
			c.Check(first.Kind(), Equals, "prerequisites")
			wait := false
			if expectedPruned["other-snap"]["aliasA"] {
				wait = true
			} else if expectedPruned["some-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(first.WaitTasks(), DeepEquals, pruneTs.Tasks())
			} else {
				c.Check(first.WaitTasks(), HasLen, 0)
			}
		}
		if scenario.new {
			newTs := tts[j]
			j++
			expectedUpdatesSet["other-snap"] = true
			tasks := newTs.Tasks()
			c.Check(tasks, HasLen, 1)
			aliasTask := tasks[0]
			c.Check(aliasTask.Kind(), Equals, "refresh-aliases")

			wait := false
			if expectedPruned["some-snap"]["aliasB"] {
				wait = true
			} else if expectedPruned["other-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(aliasTask.WaitTasks(), DeepEquals, pruneTs.Tasks())
			} else {
				c.Check(aliasTask.WaitTasks(), HasLen, 0)
			}
		}
		c.Assert(j, Equals, len(tts), Commentf("%#v", scenario))

		// check reported updated names
		c.Check(len(updates) > 0, Equals, true)
		sort.Strings(updates)
		expectedUpdates := make([]string, 0, len(expectedUpdatesSet))
		for x := range expectedUpdatesSet {
			expectedUpdates = append(expectedUpdates, x)
		}
		sort.Strings(expectedUpdates)
		c.Check(updates, DeepEquals, expectedUpdates)
	}
}

func (s *snapmgrTestSuite) TestUpdateOneAutoAliasesScenarios(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", SnapID: "other-snap-id", Revision: snap.R(2)},
		},
		Current:  snap.R(2),
		SnapType: "app",
	})

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		switch info.Name() {
		case "some-snap":
			return map[string]string{"aliasA": "cmdA"}, nil
		case "other-snap":
			return map[string]string{"aliasB": "cmdB"}, nil
		}
		return nil, nil
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	expectedSet := func(aliases []string) map[string]bool {
		res := make(map[string]bool, len(aliases))
		for _, alias := range aliases {
			res[alias] = true
		}
		return res
	}

	for _, scenario := range orthogonalAutoAliasesScenarios {
		if len(scenario.names) != 1 {
			continue
		}

		for _, snapName := range []string{"some-snap", "other-snap"} {
			var snapst snapstate.SnapState
			err := snapstate.Get(s.state, snapName, &snapst)
			c.Assert(err, IsNil)
			snapst.Aliases = nil
			snapst.AutoAliasesDisabled = false
			if autoAliases := scenario.aliasesBefore[snapName]; autoAliases != nil {
				targets := make(map[string]*snapstate.AliasTarget)
				for _, alias := range autoAliases {
					targets[alias] = &snapstate.AliasTarget{Auto: "cmd" + alias[len(alias)-1:]}
				}

				snapst.Aliases = targets
			}
			snapstate.Set(s.state, snapName, &snapst)
		}

		ts, err := snapstate.Update(s.state, scenario.names[0], "", snap.R(0), s.user.ID, snapstate.Flags{})
		c.Assert(err, IsNil)
		_, dropped, err := snapstate.AutoAliasesDelta(s.state, []string{"some-snap", "other-snap"})
		c.Assert(err, IsNil)

		j := 0
		tasks := ts.Tasks()
		var expectedPruned map[string]map[string]bool
		var pruneTasks []*state.Task
		if len(scenario.prune) != 0 {
			nprune := len(scenario.prune)
			pruneTasks = tasks[:nprune]
			j += nprune
			taskAliases := make(map[string]map[string]bool)
			for _, aliasTask := range pruneTasks {
				c.Check(aliasTask.Kind(), Equals, "prune-auto-aliases")
				var aliases []string
				err := aliasTask.Get("aliases", &aliases)
				c.Assert(err, IsNil)
				snapsup, err := snapstate.TaskSnapSetup(aliasTask)
				c.Assert(err, IsNil)
				taskAliases[snapsup.Name()] = expectedSet(aliases)
			}
			expectedPruned = make(map[string]map[string]bool)
			for _, snapName := range scenario.prune {
				expectedPruned[snapName] = expectedSet(dropped[snapName])
			}
			c.Check(taskAliases, DeepEquals, expectedPruned)
		}
		if scenario.update {
			first := tasks[j]
			j += 18
			c.Check(first.Kind(), Equals, "prerequisites")
			wait := false
			if expectedPruned["other-snap"]["aliasA"] {
				wait = true
			} else if expectedPruned["some-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(first.WaitTasks(), DeepEquals, pruneTasks)
			} else {
				c.Check(first.WaitTasks(), HasLen, 0)
			}
		}
		if scenario.new {
			aliasTask := tasks[j]
			j++
			c.Check(aliasTask.Kind(), Equals, "refresh-aliases")
			wait := false
			if expectedPruned["some-snap"]["aliasB"] {
				wait = true
			} else if expectedPruned["other-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(aliasTask.WaitTasks(), DeepEquals, pruneTasks)
			} else {
				c.Check(aliasTask.WaitTasks(), HasLen, 0)
			}
		}
		c.Assert(len(tasks), Equals, j, Commentf("%#v", scenario))

		// conflict checks are triggered
		chg := s.state.NewChange("update", "...")
		chg.AddAll(ts)
		err = snapstate.CheckChangeConflict(s.state, scenario.names[0], nil, nil)
		c.Check(err, ErrorMatches, `.* has "update" change in progress`)
		chg.SetStatus(state.DoneStatus)
	}
}

func (s *snapmgrTestSuite) TestUpdateLocalSnapFails(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, store.ErrLocalSnap)
}

func (s *snapmgrTestSuite) TestUpdateDisabledUnsupported(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `refreshing disabled snap "some-snap" not supported`)
}

func makeTestSnap(c *C, snapYamlContent string) (snapFilePath string) {
	return snaptest.MakeTestSnapWithFiles(c, snapYamlContent, nil)
}

func (s *snapmgrTestSuite) TestInstallFirstLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "mock"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
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
			name:  mockSnap,
			revno: snap.R("x1"),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "mock/x1"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
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
			name: filepath.Join(dirs.SnapMountDir, "mock/x1"),
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

	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[1]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		SnapPath: mockSnap,
		SideInfo: snapsup.SideInfo,
		Type:     snap.TypeApp,
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
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "mock"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	ops := s.fakeBackend.ops
	// ensure only local install was run, i.e. first action is pseudo-action current
	c.Assert(ops.Ops(), HasLen, 11)
	c.Check(ops[0].op, Equals, "current")
	c.Check(ops[0].old, Equals, filepath.Join(dirs.SnapMountDir, "mock/x2"))
	// and setup-snap
	c.Check(ops[1].op, Equals, "setup-snap")
	c.Check(ops[1].name, Matches, `.*/mock_1.0_all.snap`)
	c.Check(ops[1].revno, Equals, snap.R("x3"))
	// and cleanup
	c.Check(ops[len(ops)-1], DeepEquals, fakeOp{
		op:    "cleanup-trash",
		name:  "mock",
		revno: snap.R("x3"),
	})

	c.Check(ops[3].op, Equals, "unlink-snap")
	c.Check(ops[3].name, Equals, filepath.Join(dirs.SnapMountDir, "mock/x2"))

	c.Check(ops[4].op, Equals, "copy-data")
	c.Check(ops[4].name, Equals, filepath.Join(dirs.SnapMountDir, "mock/x3"))
	c.Check(ops[4].old, Equals, filepath.Join(dirs.SnapMountDir, "mock/x2"))

	c.Check(ops[5].op, Equals, "setup-profiles:Doing")
	c.Check(ops[5].name, Equals, "mock")
	c.Check(ops[5].revno, Equals, snap.R(-3))

	c.Check(ops[6].op, Equals, "candidate")
	c.Check(ops[6].sinfo, DeepEquals, snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-3),
	})
	c.Check(ops[7].op, Equals, "link-snap")
	c.Check(ops[7].name, Equals, filepath.Join(dirs.SnapMountDir, "mock/x3"))

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[1]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		SnapPath: mockSnap,
		SideInfo: snapsup.SideInfo,
		Type:     snap.TypeApp,
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
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "mock"}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
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
			name:  mockSnap,
			revno: snap.R("x1"),
		},
		{
			op:   "remove-snap-aliases",
			name: "mock",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "mock/100001"),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "mock/x1"),
			old:  filepath.Join(dirs.SnapMountDir, "mock/100001"),
		},
		{
			op:    "setup-profiles:Doing",
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
			name: filepath.Join(dirs.SnapMountDir, "mock/x1"),
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
	ts, err := snapstate.InstallPath(s.state, si, someSnap, "", snapstate.Flags{Required: true})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure only local install was run, i.e. first actions are pseudo-action current
	c.Assert(s.fakeBackend.ops.Ops(), HasLen, 9)
	c.Check(s.fakeBackend.ops[0].op, Equals, "current")
	c.Check(s.fakeBackend.ops[0].old, Equals, "<no-current>")
	// and setup-snap
	c.Check(s.fakeBackend.ops[1].op, Equals, "setup-snap")
	c.Check(s.fakeBackend.ops[1].name, Matches, `.*/orig-name_1.0_all.snap`)
	c.Check(s.fakeBackend.ops[1].revno, Equals, snap.R(42))

	c.Check(s.fakeBackend.ops[4].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[4].sinfo, DeepEquals, *si)
	c.Check(s.fakeBackend.ops[5].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[5].name, Equals, filepath.Join(dirs.SnapMountDir, "some-snap/42"))

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
		Type: snap.TypeApp,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, si)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "")
	c.Assert(snapst.Sequence[0], DeepEquals, si)
	c.Assert(snapst.LocalRevision().Unset(), Equals, true)
	c.Assert(snapst.Required, Equals, true)
}

func (s *snapmgrTestSuite) TestRemoveRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "remove-snap-common-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "discard-conns:Doing",
			name: "some-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Check(len(s.fakeBackend.ops), Equals, len(expected))
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		if t.Kind() == "run-hook" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		if t.Kind() == "discard-conns" {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
			}
		} else {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: snap.R(7),
				},
			}
		}
		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveWithManyRevisionsRunThrough(c *C) {
	si3 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(3),
	}

	si5 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(5),
	}

	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si5, &si3, &si7},
		Current:  si7.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/3"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/3"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/5"),
		},
		{
			op:   "remove-snap-common-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/5"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/5"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "discard-conns:Doing",
			name: "some-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	revnos := []snap.Revision{{N: 7}, {N: 3}, {N: 5}}
	whichRevno := 0
	for _, t := range tasks {
		if t.Kind() == "run-hook" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		if t.Kind() == "discard-conns" {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
			}
		} else {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: revnos[whichRevno],
				},
			}
		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))

		if t.Kind() == "discard-snap" {
			whichRevno++
		}
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveOneRevisionRunThrough(c *C) {
	si3 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(3),
	}

	si5 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(5),
	}

	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si5, &si3, &si7},
		Current:  si7.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(3))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(len(s.fakeBackend.ops), Equals, 2)
	expected := fakeOps{
		{
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/3"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/3"),
			stype: "app",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		expSnapSetup := &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(3),
			},
		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Sequence, HasLen, 2)
}

func (s *snapmgrTestSuite) TestRemoveLastRevisionRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(2))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(len(s.fakeBackend.ops), Equals, 5)
	expected := fakeOps{
		{
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:   "remove-snap-common-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/2"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "discard-conns:Doing",
			name: "some-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		if t.Kind() == "run-hook" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		expSnapSetup := &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: "some-snap",
			},
		}
		if t.Kind() != "discard-conns" {
			expSnapSetup.SideInfo.Revision = snap.R(2)
		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveCurrentActiveRevisionRefused(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(2))

	c.Check(err, ErrorMatches, `cannot remove active revision 2 of snap "some-snap"`)
}

func (s *snapmgrTestSuite) TestRemoveCurrentRevisionOfSeveralRefused(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(2))
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `cannot remove active revision 2 of snap "some-snap" (revert first?)`)
}

func (s *snapmgrTestSuite) TestRemoveMissingRevisionRefused(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(1))

	c.Check(err, ErrorMatches, `revision 1 of snap "some-snap" is not installed`)
}

func (s *snapmgrTestSuite) TestRemoveRefused(c *C) {
	si := snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "gadget", snap.R(0))

	c.Check(err, ErrorMatches, `snap "gadget" is not removable`)
}

func (s *snapmgrTestSuite) TestRemoveRefusedLastRevision(c *C) {
	si := snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "gadget", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "gadget", snap.R(7))

	c.Check(err, ErrorMatches, `snap "gadget" is not removable`)
}

func (s *snapmgrTestSuite) TestRemoveDeletesConfigOnLastRevision(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	snapstate.Set(s.state, "another-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	// a config for some other snap to verify its not accidentally destroyed
	tr = config.NewTransaction(s.state)
	tr.Set("another-snap", "bar", "baz")
	tr.Commit()

	var res string
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)
	c.Assert(tr.Get("another-snap", "bar", &res), IsNil)

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

	tr = config.NewTransaction(s.state)
	err = tr.Get("some-snap", "foo", &res)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `snap "some-snap" has no "foo" configuration option`)

	// and another snap has its config intact
	c.Assert(tr.Get("another-snap", "bar", &res), IsNil)
	c.Assert(res, Equals, "baz")
}

func (s *snapmgrTestSuite) TestRemoveDoesntDeleteConfigIfNotLastRevision(c *C) {
	si1 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(8),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si1, &si2},
		Current:  si2.Revision,
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	var res string
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", si1.Revision)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)
	c.Assert(res, Equals, "bar")
}

func (s *snapmgrTestSuite) TestUpdateMakesConfigSnapshot(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	var cfgs map[string]interface{}
	// we don't have config snapshots yet
	c.Assert(s.state.Get("revision-config", &cfgs), Equals, state.ErrNoState)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(2), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)

	s.state.Lock()
	cfgs = nil
	// config copy of rev. 1 has been made
	c.Assert(s.state.Get("revision-config", &cfgs), IsNil)
	c.Assert(cfgs["some-snap"], DeepEquals, map[string]interface{}{
		"1": map[string]interface{}{
			"foo": "bar",
		},
	})
}

func (s *snapmgrTestSuite) TestRevertRestoresConfigSnapshot(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", Revision: snap.R(2)},
		},
		Current:  snap.R(2),
		SnapType: "app",
	})

	// set configuration for current snap
	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "100")
	tr.Commit()

	// make config snapshot for rev.1
	config.SaveRevisionConfig(s.state, "some-snap", snap.R(1))

	// modify for rev. 2
	tr = config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "200")
	tr.Commit()

	chg := s.state.NewChange("revert", "revert snap")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)

	s.state.Lock()
	// config snapshot of rev. 2 has been made by 'revert'
	var cfgs map[string]interface{}
	c.Assert(s.state.Get("revision-config", &cfgs), IsNil)
	c.Assert(cfgs["some-snap"], DeepEquals, map[string]interface{}{
		"1": map[string]interface{}{"foo": "100"},
		"2": map[string]interface{}{"foo": "200"},
	})

	// current snap configuration has been restored from rev. 1 config snapshot
	tr = config.NewTransaction(s.state)
	var res string
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)
	c.Assert(res, Equals, "100")
}

func (s *snapmgrTestSuite) TestUpdateDoesGC(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure garbage collection runs as the last tasks
	expectedTail := fakeOps{
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
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
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/1"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/2"),
			stype: "app",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(11),
		},
	}

	opsTail := s.fakeBackend.ops[len(s.fakeBackend.ops)-len(expectedTail):]
	c.Assert(opsTail.Ops(), DeepEquals, expectedTail.Ops())
	c.Check(opsTail, DeepEquals, expectedTail)
}

func (s *snapmgrTestSuite) TestRevertNoRevertAgain(c *C) {
	siNew := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(77),
	}

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &siNew},
		Current:  snap.R(7),
	})

	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, ErrorMatches, "no revision to revert to")
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertNothingToRevertTo(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, ErrorMatches, "no revision to revert to")
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertToRevisionNoValidVersion(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(77),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  snap.R(77),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R("99"), snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot find revision 99 for snap "some-snap"`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertToRevisionAlreadyCurrent(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(77),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  snap.R(77),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R("77"), snapstate.Flags{})
	c.Assert(err, ErrorMatches, `already on requested revision`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	siOld := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&siOld, &si},
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(2),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(2),
			},
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(2),
		},
		{
			op: "update-aliases",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify that the R(2) version is active now and R(7) is still there
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(2),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Block(), DeepEquals, []snap.Revision{snap.R(7)})
}

func (s *snapmgrTestSuite) TestRevertWithLocalRevisionRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-7),
	}
	siOld := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&siOld, &si},
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(s.fakeBackend.ops.Ops(), HasLen, 7)

	// verify that LocalRevision is still -7
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.LocalRevision(), Equals, snap.R(-7))
}

func (s *snapmgrTestSuite) TestRevertToRevisionNewVersion(c *C) {
	siNew := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		SnapID:   "october",
	}

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
		SnapID:   "october",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&si, &siNew},
		Current:  snap.R(2),
		Channel:  "edge",
	})

	chg := s.state.NewChange("revert", "revert a snap forward")
	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R(7), snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:    "candidate",
			sinfo: siNew,
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op: "update-aliases",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify that the R(7) version is active now
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(7))
	c.Check(snapst.Sequence, HasLen, 2)
	c.Check(snapst.Channel, Equals, "edge")
	c.Check(snapst.CurrentSideInfo(), DeepEquals, &siNew)

	c.Check(snapst.Block(), HasLen, 0)
}

func (s *snapmgrTestSuite) TestRevertTotalUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  si2.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(1),
			},
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op: "update-aliases",
		},
		// undoing everything from here down...
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op: "update-aliases",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Current, Equals, si2.Revision)
}

func (s *snapmgrTestSuite) TestRevertUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  si2.Revision,
	})

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/1")

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(1),
			},
		},
		{
			op:   "link-snap.failed",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		// undo stuff here
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op: "update-aliases",
		},
	}

	// ensure all our tasks ran
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Current, Equals, snap.R(2))
}

func (s *snapmgrTestSuite) TestEnableDoesNotEnableAgain(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  snap.R(7),
		Active:   true,
	})

	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" already enabled`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestEnableRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		Channel:  "edge",
		SnapID:   "foo",
	}

	s.state.Lock()
	defer s.state.Unlock()

	flags := snapstate.Flags{
		DevMode:  true,
		JailMode: true,
		Classic:  true,
		TryMode:  true,
		Required: true,
	}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence:            []*snap.SideInfo{&si},
		Current:             si.Revision,
		Active:              false,
		Channel:             "edge",
		Flags:               flags,
		AliasesPending:      true,
		AutoAliasesDisabled: true,
	})

	chg := s.state.NewChange("enable", "enable a snap")
	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:    "candidate",
			sinfo: si,
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op: "update-aliases",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Flags, DeepEquals, flags)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.AliasesPending, Equals, false)
	c.Assert(snapst.AutoAliasesDisabled, Equals, true)

	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Channel, Equals, "edge")
	c.Assert(info.SnapID, Equals, "foo")
}

func (s *snapmgrTestSuite) TestDisableRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		Active:   true,
	})

	chg := s.state.NewChange("disable", "disable a snap")
	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, false)
	c.Assert(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestSwitchRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		Channel:  "edge",
		SnapID:   "foo",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		Channel:  "edge",
	})

	chg := s.state.NewChange("switch-snap", "switch snap to some-channel")
	ts, err := snapstate.Switch(s.state, "some-snap", "some-channel")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// switch is not really really doing anything backend related
	c.Assert(s.fakeBackend.ops, HasLen, 0)

	// ensure the desired channel has changed
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Channel, Equals, "some-channel")

	// ensure the current info has not changed
	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Channel, Equals, "edge")
}

func (s *snapmgrTestSuite) TestDisableDoesNotEnableAgain(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  snap.R(7),
		Active:   false,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" already disabled`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestUndoMountSnapFailsInCopyData(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.copySnapDataFailTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/11")

	s.state.Unlock()
	defer s.snapmgr.Stop()
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
				Action:  "install",
				Name:    "some-snap",
				Channel: "some-channel",
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
			name: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "copy-data.failed",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  "<no-old>",
		},
		{
			op:    "undo-setup-snap",
			name:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			stype: "app",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestRefreshFailureCausesErrorReport(c *C) {
	var errSnap, errMsg, errSig string
	var errExtra map[string]string
	var n int
	restore := snapstate.MockErrtrackerReport(func(aSnap, aErrMsg, aDupSig string, extra map[string]string) (string, error) {
		errSnap = aSnap
		errMsg = aErrMsg
		errSig = aDupSig
		errExtra = extra
		n += 1
		return "oopsid", nil
	})
	defer restore()

	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("ubuntu-core-transition-retry", 7)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/11")

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify we generated a failure report
	c.Check(n, Equals, 1)
	c.Check(errSnap, Equals, "some-snap")
	c.Check(errExtra, DeepEquals, map[string]string{
		"UbuntuCoreTransitionCount": "7",
		"Channel":                   "some-channel",
		"Revision":                  "11",
	})
	c.Check(errMsg, Matches, `(?sm)change "install": "install a snap"
prerequisites: Undo
 snap-setup: "some-snap" \(11\) "some-channel"
download-snap: Undoing
validate-snap: Done
.*
link-snap: Error
 INFO unlink
 ERROR fail
auto-connect: Hold
set-auto-aliases: Hold
setup-aliases: Hold
run-hook: Hold
start-snap-services: Hold
cleanup: Hold
run-hook: Hold`)
	c.Check(errSig, Matches, `(?sm)snap-install:
prerequisites: Undo
 snap-setup: "some-snap"
download-snap: Undoing
validate-snap: Done
.*
link-snap: Error
 INFO unlink
 ERROR fail
auto-connect: Hold
set-auto-aliases: Hold
setup-aliases: Hold
run-hook: Hold
start-snap-services: Hold
cleanup: Hold
run-hook: Hold`)

	// run again with empty "ubuntu-core-transition-retry"
	s.state.Set("ubuntu-core-transition-retry", 0)
	chg = s.state.NewChange("install", "install a snap")
	ts, err = snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)
	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()
	// verify that we excluded this field from the bugreport
	c.Check(n, Equals, 2)
	c.Check(errExtra, DeepEquals, map[string]string{
		"Channel":  "some-channel",
		"Revision": "11",
	})

}

func (s *snapmgrTestSuite) TestErrreportDisable(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "problem-reports.disabled", true)
	tr.Commit()

	restore := snapstate.MockErrtrackerReport(func(aSnap, aErrMsg, aDupSig string, extra map[string]string) (string, error) {
		c.Fatalf("this should not be reached")
		return "", nil
	})
	defer restore()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)
	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/11")

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// no failure report was generated
}

func (s *snapmgrTestSuite) TestEnsureRefreshesAtSeedPolicy(c *C) {
	// special policy only on classic
	r := release.MockOnClassic(true)
	defer r()

	s.snapmgr.Ensure()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// check that refresh policies have run in this case
	var t1 time.Time
	err := st.Get("last-refresh-hints", &t1)
	c.Check(err, IsNil)
	tr := config.NewTransaction(st)
	err = tr.Get("core", "refresh.hold", &t1)
	c.Check(err, IsNil)
}

func (s *snapmgrTestSuite) verifyRefreshLast(c *C) {
	var lastRefresh time.Time

	s.state.Get("last-refresh", &lastRefresh)
	c.Check(time.Now().Year(), Equals, lastRefresh.Year())
}

func makeTestRefreshConfig(st *state.State) {
	// avoid special at seed policy
	st.Set("seeded", true)
	now := time.Now()
	st.Set("last-refresh", time.Date(2009, 8, 13, 8, 0, 5, 0, now.Location()))

	tr := config.NewTransaction(st)
	tr.Set("core", "refresh.timer", "00:00-23:59")
	tr.Commit()
}

func (s *snapmgrTestSuite) TestEnsureRefreshRefusesLegacyWeekdaySchedules(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	logbuf, restore := logger.MockLogger()
	defer restore()

	s.state.Set("last-refresh", time.Date(2009, 8, 13, 8, 0, 5, 0, time.UTC))
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.timer", "")
	tr.Set("core", "refresh.schedule", "00:00-23:59/mon@12:00-14:00")
	tr.Commit()

	// Ensure() also runs ensureRefreshes()
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	c.Check(logbuf.String(), testutil.Contains, `cannot use refresh.schedule configuration: cannot parse "mon@12:00": not a valid time`)
	schedule, legacy, err := s.snapmgr.RefreshSchedule()
	c.Assert(err, IsNil)
	c.Check(schedule, Equals, "00:00~24:00/4")
	c.Check(legacy, Equals, false)

	tr = config.NewTransaction(s.state)
	refreshTimer := "canary"
	refreshSchedule := "canary"
	c.Assert(tr.Get("core", "refresh.timer", &refreshTimer), IsNil)
	c.Assert(tr.Get("core", "refresh.schedule", &refreshSchedule), IsNil)
	c.Check(refreshTimer, Equals, "")
	c.Check(refreshSchedule, Equals, "00:00-23:59/mon@12:00-14:00")
}

func (s *snapmgrTestSuite) TestEnsureRefreshLegacyScheduleIsLowerPriority(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	s.state.Set("last-refresh", time.Date(2009, 8, 13, 8, 0, 5, 0, time.UTC))
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.timer", "00:00-23:59,,mon,12:00-14:00")
	// legacy schedule is invalid
	tr.Set("core", "refresh.schedule", "00:00-23:59/mon@12:00-14:00")
	tr.Commit()

	// Ensure() also runs ensureRefreshes()
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// expecting new refresh.timer to have been used, fallback to legacy was
	// not attempted otherwise it would get reset to the default due to
	// refresh.schedule being garbage
	schedule, legacy, err := s.snapmgr.RefreshSchedule()
	c.Assert(err, IsNil)
	c.Check(schedule, Equals, "00:00-23:59,,mon,12:00-14:00")
	c.Check(legacy, Equals, false)
}

func (s *snapmgrTestSuite) TestEnsureRefreshFallbackToLegacySchedule(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.timer", "")
	tr.Set("core", "refresh.schedule", "00:00-23:59")
	tr.Commit()

	// Ensure() also runs ensureRefreshes()
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// refresh.timer is unset, triggering automatic fallback to legacy
	// schedule if that was set
	schedule, legacy, err := s.snapmgr.RefreshSchedule()
	c.Assert(err, IsNil)
	c.Check(schedule, Equals, "00:00-23:59")
	c.Check(legacy, Equals, true)
}

func (s *snapmgrTestSuite) TestEnsureRefreshFallbackToDefaultOnError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.timer", "garbage-in")
	tr.Set("core", "refresh.schedule", "00:00-23:59")
	tr.Commit()

	// Ensure() also runs ensureRefreshes()
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// automatic fallback to default schedule if refresh.timer is set but
	// cannot be parsed
	schedule, legacy, err := s.snapmgr.RefreshSchedule()
	c.Assert(err, IsNil)
	c.Check(schedule, Equals, "00:00~24:00/4")
	c.Check(legacy, Equals, false)

	tr = config.NewTransaction(s.state)
	refreshTimer := "canary"
	refreshSchedule := "canary"
	c.Assert(tr.Get("core", "refresh.timer", &refreshTimer), IsNil)
	c.Assert(tr.Get("core", "refresh.schedule", &refreshSchedule), IsNil)
	c.Check(refreshTimer, Equals, "garbage-in")
	c.Check(refreshSchedule, Equals, "00:00-23:59")
}

func (s *snapmgrTestSuite) TestEnsureRefreshFallbackOnEmptyToDefaultSchedule(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.timer", "")
	tr.Set("core", "refresh.schedule", "")
	tr.Commit()

	// Ensure() also runs ensureRefreshes()
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// automatic fallback to default schedule if neither refresh.timer nor
	// refresh.schedule was set
	schedule, legacy, err := s.snapmgr.RefreshSchedule()
	c.Assert(err, IsNil)
	c.Check(schedule, Equals, "00:00~24:00/4")
	c.Check(legacy, Equals, false)

	tr = config.NewTransaction(s.state)
	refreshTimer := "canary"
	refreshSchedule := "canary"
	c.Assert(tr.Get("core", "refresh.timer", &refreshTimer), IsNil)
	c.Assert(tr.Get("core", "refresh.schedule", &refreshSchedule), IsNil)
	c.Check(refreshTimer, Equals, "")
	c.Check(refreshSchedule, Equals, "")
}

func (s *snapmgrTestSuite) TestEnsureRefreshesNoUpdate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	makeTestRefreshConfig(s.state)

	// Ensure() also runs ensureRefreshes()
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// nothing needs to be done, but last-refresh got updated
	c.Check(s.state.Changes(), HasLen, 0)
	s.verifyRefreshLast(c)

	// ensure the next-refresh time is reset and re-calculated
	c.Check(s.snapmgr.NextRefresh().IsZero(), Equals, true)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesAlreadyRanInThisInterval(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) {
		return true, nil
	}
	nextRefresh := s.snapmgr.NextRefresh()
	c.Check(nextRefresh.IsZero(), Equals, true)

	now := time.Now()
	fakeLastRefresh := now.Add(-1 * time.Hour)
	s.state.Set("last-refresh", fakeLastRefresh)

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.timer", fmt.Sprintf("00:00-%02d:%02d", now.Hour(), now.Minute()))
	tr.Commit()

	// Ensure() also runs ensureRefreshes()
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// nothing needs to be done and no refresh was run
	c.Check(s.state.Changes(), HasLen, 0)

	var refreshLast time.Time
	s.state.Get("last-refresh", &refreshLast)
	c.Check(refreshLast.Equal(fakeLastRefresh), Equals, true)

	// but a nextRefresh time got calculated
	nextRefresh = s.snapmgr.NextRefresh()
	c.Check(nextRefresh.IsZero(), Equals, false)

	// run ensure again to test that nextRefresh again to ensure that
	// nextRefresh is not calculated again if nothing changes
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()
	c.Check(s.snapmgr.NextRefresh(), Equals, nextRefresh)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesWithUpdate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	makeTestRefreshConfig(s.state)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// Ensure() also runs ensureRefreshes() and our test setup has an
	// update for the "some-snap" in our fake store
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// verify we have an auto-refresh change scheduled now
	c.Assert(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Check(chg.Kind(), Equals, "auto-refresh")
	c.Check(chg.IsReady(), Equals, false)
	s.verifyRefreshLast(c)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesImmediateWithUpdate(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	// lastRefresh is unset/zero => immediate refresh try

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// Ensure() also runs ensureRefreshes() and our test setup has an
	// update for the "some-snap" in our fake store
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// verify we have an auto-refresh change scheduled now
	c.Assert(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Check(chg.Kind(), Equals, "auto-refresh")
	c.Check(chg.IsReady(), Equals, false)
	s.verifyRefreshLast(c)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesWithUpdateError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	makeTestRefreshConfig(s.state)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// Ensure() also runs ensureRefreshes() and our test setup has an
	// update for the "some-snap" in our fake store
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	terr := s.state.NewTask("error-trigger", "simulate an error")
	tasks := chg.Tasks()
	for _, t := range tasks[:len(tasks)-2] {
		terr.WaitFor(t)
	}
	chg.AddTask(terr)

	// run the changes
	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	s.verifyRefreshLast(c)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesInFlight(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	makeTestRefreshConfig(s.state)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// simulate an in-flight change
	chg := s.state.NewChange("auto-refresh", "...")
	chg.SetStatus(state.DoStatus)
	c.Check(s.state.Changes(), HasLen, 1)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	// verify no additional change got generated
	c.Check(s.state.Changes(), HasLen, 1)
}

func mockAutoRefreshAssertions(f func(st *state.State, userID int) error) func() {
	origAutoRefreshAssertions := snapstate.AutoRefreshAssertions
	snapstate.AutoRefreshAssertions = f
	return func() {
		snapstate.AutoRefreshAssertions = origAutoRefreshAssertions
	}
}

func (s *snapmgrTestSuite) TestEnsureRefreshesWithUpdateStoreError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	// avoid special at seed policy
	s.state.Set("seeded", true)
	s.state.Set("last-refresh", time.Time{})
	autoRefreshAssertionsCalled := 0
	restore := mockAutoRefreshAssertions(func(st *state.State, userID int) error {
		// simulate failure in snapstate.AutoRefresh()
		autoRefreshAssertionsCalled++
		return fmt.Errorf("simulate store error")
	})
	defer restore()

	// check that no change got created and that autoRefreshAssertins
	// got called once
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 0)
	c.Check(autoRefreshAssertionsCalled, Equals, 1)

	// run Ensure() again and check that AutoRefresh() did not run
	// again because to test that lastRefreshAttempt backoff is working
	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 0)
	c.Check(autoRefreshAssertionsCalled, Equals, 1)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesDisabledViaSnapdControl(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	makeTestRefreshConfig(st)

	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// snapstate.AutoRefresh is called from AutoRefresh()
	autoRefreshAssertionsCalled := 0
	restore := mockAutoRefreshAssertions(func(st *state.State, userID int) error {
		autoRefreshAssertionsCalled++
		return nil
	})
	defer restore()

	// pretend the device is refresh-control: managed
	oldCanManageRefreshes := snapstate.CanManageRefreshes
	snapstate.CanManageRefreshes = func(*state.State) bool {
		return true
	}
	defer func() { snapstate.CanManageRefreshes = oldCanManageRefreshes }()
	tr := config.NewTransaction(st)
	tr.Set("core", "refresh.schedule", "managed")
	tr.Commit()

	// Ensure() also runs ensureRefreshes()
	st.Unlock()
	s.snapmgr.Ensure()
	st.Lock()

	// no refresh was called (i.e. no update to last-refresh)
	var lastRefresh time.Time
	st.Get("last-refresh", &lastRefresh)
	c.Check(lastRefresh.Year(), Equals, 2009)

	// AutoRefresh was not called
	c.Check(autoRefreshAssertionsCalled, Equals, 0)

	// The last refresh hints got updated
	var lastRefreshHints time.Time
	st.Get("last-refresh-hints", &lastRefreshHints)
	c.Check(lastRefreshHints.Year(), Equals, time.Now().Year())
}

func (s *snapmgrTestSuite) TestDefaultRefreshScheduleParsing(c *C) {
	l, err := timeutil.ParseSchedule(snapstate.DefaultRefreshSchedule)
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
}

type snapmgrQuerySuite struct {
	st      *state.State
	restore func()
}

var _ = Suite(&snapmgrQuerySuite{})

func (s *snapmgrQuerySuite) SetUpTest(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	restoreSanitize := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.restore = func() {
		restoreSanitize()
	}

	s.st = st

	dirs.SetRootDir(c.MkDir())

	// Write a snap.yaml with fake name
	sideInfo11 := &snap.SideInfo{RealName: "name1", Revision: snap.R(11), EditedSummary: "s11"}
	sideInfo12 := &snap.SideInfo{RealName: "name1", Revision: snap.R(12), EditedSummary: "s12"}
	snaptest.MockSnap(c, `
name: name0
version: 1.1
description: |
    Lots of text`, sideInfo11)
	snaptest.MockSnap(c, `
name: name0
version: 1.2
description: |
    Lots of text`, sideInfo12)
	snapstate.Set(st, "name1", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo11, sideInfo12},
		Current:  sideInfo12.Revision,
		SnapType: "app",
	})

	// have also a snap being installed
	/*
		snapstate.Set(st, "installing", &snapstate.SnapState{
			Candidate: &snap.SideInfo{RealName: "installing", Revision: snap.R(1)},
		})
	*/
}

func (s *snapmgrQuerySuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.restore()
}

func (s *snapmgrQuerySuite) TestInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	info, err := snapstate.Info(st, "name1", snap.R(11))
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(11))
	c.Check(info.Summary(), Equals, "s11")
	c.Check(info.Version, Equals, "1.1")
	c.Check(info.Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestSnapStateCurrentInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "name1", &snapst)
	c.Assert(err, IsNil)

	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(12))
	c.Check(info.Summary(), Equals, "s12")
	c.Check(info.Version, Equals, "1.2")
	c.Check(info.Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestSnapStateCurrentInfoErrNoCurrent(c *C) {
	snapst := new(snapstate.SnapState)
	_, err := snapst.CurrentInfo()
	c.Assert(err, Equals, snapstate.ErrNoCurrent)

}

func (s *snapmgrQuerySuite) TestCurrentInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	info, err := snapstate.CurrentInfo(st, "name1")
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(12))
}

func (s *snapmgrQuerySuite) TestCurrentInfoAbsent(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	_, err := snapstate.CurrentInfo(st, "absent")
	c.Assert(err, ErrorMatches, `snap "absent" is not installed`)
}

func (s *snapmgrQuerySuite) TestActiveInfos(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	infos, err := snapstate.ActiveInfos(st)
	c.Assert(err, IsNil)

	c.Check(infos, HasLen, 1)

	c.Check(infos[0].Name(), Equals, "name1")
	c.Check(infos[0].Revision, Equals, snap.R(12))
	c.Check(infos[0].Summary(), Equals, "s12")
	c.Check(infos[0].Version, Equals, "1.2")
	c.Check(infos[0].Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestTypeInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	for _, x := range []struct {
		snapName string
		snapType snap.Type
		getInfo  func(*state.State) (*snap.Info, error)
	}{
		{
			snapName: "gadget",
			snapType: snap.TypeGadget,
			getInfo:  snapstate.GadgetInfo,
		},
		{
			snapName: "core",
			snapType: snap.TypeOS,
			getInfo:  snapstate.CoreInfo,
		},
		{
			snapName: "kernel",
			snapType: snap.TypeKernel,
			getInfo:  snapstate.KernelInfo,
		},
	} {
		_, err := x.getInfo(st)
		c.Assert(err, Equals, state.ErrNoState)

		sideInfo := &snap.SideInfo{
			RealName: x.snapName,
			Revision: snap.R(2),
		}
		snaptest.MockSnap(c, fmt.Sprintf("name: %q\ntype: %q\nversion: %q\n", x.snapName, x.snapType, x.snapName), sideInfo)
		snapstate.Set(st, x.snapName, &snapstate.SnapState{
			SnapType: string(x.snapType),
			Active:   true,
			Sequence: []*snap.SideInfo{sideInfo},
			Current:  sideInfo.Revision,
		})

		info, err := x.getInfo(st)
		c.Assert(err, IsNil)

		c.Check(info.Name(), Equals, x.snapName)
		c.Check(info.Revision, Equals, snap.R(2))
		c.Check(info.Version, Equals, x.snapName)
		c.Check(info.Type, Equals, x.snapType)
	}
}

func (s *snapmgrQuerySuite) TestTypeInfoCore(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	for testNr, t := range []struct {
		expectedSnap string
		snapNames    []string
		errMatcher   string
	}{
		// nothing
		{"", []string{}, state.ErrNoState.Error()},
		// single
		{"core", []string{"core"}, ""},
		{"ubuntu-core", []string{"ubuntu-core"}, ""},
		{"hard-core", []string{"hard-core"}, ""},
		// unrolled loop to ensure we don't pass because
		// the order is randomly right
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		{"core", []string{"core", "ubuntu-core"}, ""},
		// unknown combination
		{"", []string{"duo-core", "single-core"}, `unexpected cores.*`},
		// multi-core is not supported
		{"", []string{"core", "ubuntu-core", "multi-core"}, `unexpected number of cores, got 3`},
	} {
		// clear snapstate
		st.Set("snaps", map[string]*json.RawMessage{})

		for _, snapName := range t.snapNames {
			sideInfo := &snap.SideInfo{
				RealName: snapName,
				Revision: snap.R(1),
			}
			snaptest.MockSnap(c, fmt.Sprintf("name: %q\ntype: os\nversion: %q\n", snapName, snapName), sideInfo)
			snapstate.Set(st, snapName, &snapstate.SnapState{
				SnapType: string(snap.TypeOS),
				Active:   true,
				Sequence: []*snap.SideInfo{sideInfo},
				Current:  sideInfo.Revision,
			})
		}

		info, err := snapstate.CoreInfo(st)
		if t.errMatcher != "" {
			c.Assert(err, ErrorMatches, t.errMatcher)
		} else {
			c.Assert(info, NotNil)
			c.Check(info.Name(), Equals, t.expectedSnap, Commentf("(%d) test %q %v", testNr, t.expectedSnap, t.snapNames))
			c.Check(info.Type, Equals, snap.TypeOS)
		}
	}
}

func (s *snapmgrQuerySuite) TestPreviousSideInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "name1", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.CurrentSideInfo(), NotNil)
	c.Assert(snapst.CurrentSideInfo().Revision, Equals, snap.R(12))
	c.Assert(snapstate.PreviousSideInfo(&snapst), NotNil)
	c.Assert(snapstate.PreviousSideInfo(&snapst).Revision, Equals, snap.R(11))
}

func (s *snapmgrQuerySuite) TestPreviousSideInfoNoCurrent(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	snapst := &snapstate.SnapState{}
	c.Assert(snapstate.PreviousSideInfo(snapst), IsNil)
}

func (s *snapmgrQuerySuite) TestAll(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	c.Assert(err, IsNil)
	c.Assert(snapStates, HasLen, 1)

	n, err := snapstate.NumSnaps(st)
	c.Assert(err, IsNil)
	c.Check(n, Equals, 1)

	snapst := snapStates["name1"]
	c.Assert(snapst, NotNil)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.CurrentSideInfo(), NotNil)

	info12, err := snap.ReadInfo("name1", snapst.CurrentSideInfo())
	c.Assert(err, IsNil)

	c.Check(info12.Name(), Equals, "name1")
	c.Check(info12.Revision, Equals, snap.R(12))
	c.Check(info12.Summary(), Equals, "s12")
	c.Check(info12.Version, Equals, "1.2")
	c.Check(info12.Description(), Equals, "Lots of text")

	info11, err := snap.ReadInfo("name1", snapst.Sequence[0])
	c.Assert(err, IsNil)

	c.Check(info11.Name(), Equals, "name1")
	c.Check(info11.Revision, Equals, snap.R(11))
	c.Check(info11.Version, Equals, "1.1")
}

func (s *snapmgrQuerySuite) TestAllEmptyAndEmptyNormalisation(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(snapStates, HasLen, 0)

	n, err := snapstate.NumSnaps(st)
	c.Assert(err, IsNil)
	c.Check(n, Equals, 0)

	snapstate.Set(st, "foo", nil)

	snapStates, err = snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(snapStates, HasLen, 0)

	n, err = snapstate.NumSnaps(st)
	c.Assert(err, IsNil)
	c.Check(n, Equals, 0)

	snapstate.Set(st, "foo", &snapstate.SnapState{})

	snapStates, err = snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(snapStates, HasLen, 0)

	n, err = snapstate.NumSnaps(st)
	c.Assert(err, IsNil)
	c.Check(n, Equals, 0)
}

func (s *snapmgrTestSuite) TestTrySetsTryMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{}, c)
}

func (s *snapmgrTestSuite) TestTrySetsTryModeDevMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{DevMode: true}, c)
}
func (s *snapmgrTestSuite) TestTrySetsTryModeJailMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{JailMode: true}, c)
}
func (s *snapmgrTestSuite) TestTrySetsTryModeClassic(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}
	s.testTrySetsTryMode(snapstate.Flags{Classic: true}, c)
}

func (s *snapmgrTestSuite) testTrySetsTryMode(flags snapstate.Flags, c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// make mock try dir
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	tryYaml := filepath.Join(d, "meta", "snap.yaml")
	err := os.MkdirAll(filepath.Dir(tryYaml), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(tryYaml, []byte("name: foo\nversion: 1.0"), 0644)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("try", "try snap")
	ts, err := snapstate.TryPath(s.state, "foo", d, flags)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snap is in TryMode
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)

	flags.TryMode = true
	c.Check(snapst.Flags, DeepEquals, flags)

	c.Check(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Check(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prerequisites",
		"prepare-snap",
		"mount-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook[install]",
		"start-snap-services",
		"run-hook[configure]",
	})

}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlag(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}
	s.testTrySetsTryMode(snapstate.Flags{}, c)
}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesDevMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{DevMode: true}, c)
}
func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesJailMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{JailMode: true}, c)
}
func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesClassic(c *C) {
	if !dirs.SupportsClassicConfinement() {
		c.Skip("no support for classic")
	}
	s.testTrySetsTryMode(snapstate.Flags{Classic: true}, c)
}

func (s *snapmgrTestSuite) testTryUndoRemovesTryFlag(flags snapstate.Flags, c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// simulate existing state for foo
	var snapst snapstate.SnapState
	snapst.Sequence = []*snap.SideInfo{
		{
			RealName: "foo",
			Revision: snap.R(23),
		},
	}
	snapst.Flags = flags
	snapst.Current = snap.R(23)
	snapstate.Set(s.state, "foo", &snapst)
	c.Check(snapst.TryMode, Equals, false)

	chg := s.state.NewChange("try", "try snap")
	ts, err := snapstate.TryPath(s.state, "foo", c.MkDir(), flags)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	last := ts.Tasks()[len(ts.Tasks())-1]
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snap is not in try mode, the state got undone
	err = snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Flags, DeepEquals, flags)
}

type snapStateSuite struct{}

var _ = Suite(&snapStateSuite{})

func (s *snapStateSuite) TestSnapStateDevMode(c *C) {
	snapst := &snapstate.SnapState{}
	c.Check(snapst.DevMode, Equals, false)
	snapst.Flags.DevMode = true
	c.Check(snapst.DevMode, Equals, true)
}

func (s *snapStateSuite) TestSnapStateType(c *C) {
	snapst := &snapstate.SnapState{}
	_, err := snapst.Type()
	c.Check(err, ErrorMatches, "snap type unset")

	snapst.SetType(snap.TypeKernel)
	typ, err := snapst.Type()
	c.Assert(err, IsNil)
	c.Check(typ, Equals, snap.TypeKernel)
}

func (s *snapStateSuite) TestCurrentSideInfoEmpty(c *C) {
	var snapst snapstate.SnapState
	c.Check(snapst.CurrentSideInfo(), IsNil)
	c.Check(snapst.Current.Unset(), Equals, true)
}

func (s *snapStateSuite) TestCurrentSideInfoSimple(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  snap.R(1),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si1)
}

func (s *snapStateSuite) TestCurrentSideInfoInOrder(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	si2 := &snap.SideInfo{Revision: snap.R(2)}
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1, si2},
		Current:  snap.R(2),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si2)
}

func (s *snapStateSuite) TestCurrentSideInfoOutOfOrder(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	si2 := &snap.SideInfo{Revision: snap.R(2)}
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1, si2},
		Current:  snap.R(1),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si1)
}

func (s *snapStateSuite) TestCurrentSideInfoInconsistent(c *C) {
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{Revision: snap.R(1)},
		},
	}
	c.Check(func() { snapst.CurrentSideInfo() }, PanicMatches, `snapst.Current and snapst.Sequence out of sync:.*`)
}

func (s *snapStateSuite) TestCurrentSideInfoInconsistentWithCurrent(c *C) {
	snapst := snapstate.SnapState{Current: snap.R(17)}
	c.Check(func() { snapst.CurrentSideInfo() }, PanicMatches, `cannot find snapst.Current in the snapst.Sequence`)
}

type snapSetupSuite struct{}

var _ = Suite(&snapSetupSuite{})

type canRemoveSuite struct {
	st *state.State
}

var _ = Suite(&canRemoveSuite{})

func (s *canRemoveSuite) SetUpTest(c *C) {
	s.st = state.New(nil)
	snapstate.MockModel()
}

func (s *canRemoveSuite) TestAppAreAlwaysOKToRemove(c *C) {
	info := &snap.Info{
		Type: snap.TypeApp,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(s.st, info, &snapstate.SnapState{Active: true}, false), Equals, true)
	c.Check(snapstate.CanRemove(s.st, info, &snapstate.SnapState{Active: true}, true), Equals, true)
}

func (s *canRemoveSuite) TestLastGadgetsAreNotOK(c *C) {
	info := &snap.Info{
		Type: snap.TypeGadget,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(s.st, info, &snapstate.SnapState{}, true), Equals, false)
}

func (s *canRemoveSuite) TestLastOSAndKernelAreNotOK(c *C) {
	os := &snap.Info{
		Type: snap.TypeOS,
	}
	os.RealName = "os"
	kernel := &snap.Info{
		Type: snap.TypeKernel,
	}
	kernel.RealName = "krnl"

	c.Check(snapstate.CanRemove(s.st, os, &snapstate.SnapState{}, true), Equals, false)

	c.Check(snapstate.CanRemove(s.st, kernel, &snapstate.SnapState{}, true), Equals, false)
}

func (s *canRemoveSuite) TestLastOSWithModelBaseIsOk(c *C) {
	snapstate.MockModelWithBase("core18")
	os := &snap.Info{
		Type: snap.TypeOS,
	}
	os.RealName = "os"

	c.Check(snapstate.CanRemove(s.st, os, &snapstate.SnapState{}, true), Equals, true)
}

func (s *canRemoveSuite) TestOneRevisionIsOK(c *C) {
	info := &snap.Info{
		Type: snap.TypeGadget,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(s.st, info, &snapstate.SnapState{Active: true}, false), Equals, true)
}

func (s *canRemoveSuite) TestRequiredIsNotOK(c *C) {
	info := &snap.Info{
		Type: snap.TypeApp,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(s.st, info, &snapstate.SnapState{Active: false, Flags: snapstate.Flags{Required: true}}, true), Equals, false)
	c.Check(snapstate.CanRemove(s.st, info, &snapstate.SnapState{Active: true, Flags: snapstate.Flags{Required: true}}, true), Equals, false)
	c.Check(snapstate.CanRemove(s.st, info, &snapstate.SnapState{Active: true, Flags: snapstate.Flags{Required: true}}, false), Equals, true)
}

func revs(seq []*snap.SideInfo) []int {
	revs := make([]int, len(seq))
	for i, si := range seq {
		revs[i] = si.Revision.N
	}

	return revs
}

type opSeqOpts struct {
	revert  bool
	fail    bool
	before  []int
	current int
	via     int
	after   []int
}

// build a SnapState with a revision sequence given by `before` and a
// current revision of `current`. Then refresh --revision via. Then
// check the revision sequence is as in `after`.
func (s *snapmgrTestSuite) testOpSequence(c *C, opts *opSeqOpts) (*snapstate.SnapState, *state.TaskSet) {
	s.state.Lock()
	defer s.state.Unlock()

	seq := make([]*snap.SideInfo, len(opts.before))
	for i, n := range opts.before {
		seq[i] = &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(n)}
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: seq,
		Current:  snap.R(opts.current),
		SnapType: "app",
	})

	var chg *state.Change
	var ts *state.TaskSet
	var err error
	if opts.revert {
		chg = s.state.NewChange("revert", "revert a snap")
		ts, err = snapstate.RevertToRevision(s.state, "some-snap", snap.R(opts.via), snapstate.Flags{})
	} else {
		chg = s.state.NewChange("refresh", "refresh a snap")
		ts, err = snapstate.Update(s.state, "some-snap", "", snap.R(opts.via), s.user.ID, snapstate.Flags{})
	}
	c.Assert(err, IsNil)
	if opts.fail {
		tasks := ts.Tasks()
		last := tasks[len(tasks)-1]
		terr := s.state.NewTask("error-trigger", "provoking total undo")
		terr.WaitFor(last)
		if len(last.Lanes()) > 0 {
			lanes := last.Lanes()
			// sanity
			c.Assert(lanes, HasLen, 1)
			terr.JoinLane(lanes[0])
		}
		chg.AddTask(terr)
	}
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(revs(snapst.Sequence), DeepEquals, opts.after)

	return &snapst, ts
}

func (s *snapmgrTestSuite) testUpdateSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = false
	snapst, ts := s.testOpSequence(c, opts)
	// update always ends with current==seq[-1]==via:
	c.Check(snapst.Current.N, Equals, opts.after[len(opts.after)-1])
	c.Check(snapst.Current.N, Equals, opts.via)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 1)
	c.Check(s.fakeBackend.ops.First("copy-data"), DeepEquals, &fakeOp{
		op:   "copy-data",
		name: fmt.Sprintf(filepath.Join(dirs.SnapMountDir, "some-snap/%d"), opts.via),
		old:  fmt.Sprintf(filepath.Join(dirs.SnapMountDir, "some-snap/%d"), opts.current),
	})

	return ts
}

func (s *snapmgrTestSuite) testUpdateFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = false
	opts.after = opts.before
	s.fakeBackend.linkSnapFailTrigger = fmt.Sprintf(filepath.Join(dirs.SnapMountDir, "some-snap/%d"), opts.via)
	snapst, ts := s.testOpSequence(c, opts)
	// a failed update will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	ops := s.fakeBackend.ops
	c.Check(ops.Count("copy-data"), Equals, 1)
	do := ops.First("copy-data")

	c.Check(ops.Count("undo-copy-snap-data"), Equals, 1)
	undo := ops.First("undo-copy-snap-data")

	do.op = undo.op
	c.Check(do, DeepEquals, undo) // i.e. they only differed in the op

	return ts
}

// testTotal*Failure fails *after* link-snap
func (s *snapmgrTestSuite) testTotalUpdateFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = false
	opts.fail = true
	snapst, ts := s.testOpSequence(c, opts)
	// a failed update will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	ops := s.fakeBackend.ops
	c.Check(ops.Count("copy-data"), Equals, 1)
	do := ops.First("copy-data")

	c.Check(ops.Count("undo-copy-snap-data"), Equals, 1)
	undo := ops.First("undo-copy-snap-data")

	do.op = undo.op
	c.Check(do, DeepEquals, undo) // i.e. they only differed in the op

	return ts
}

func (s *snapmgrTestSuite) testRevertSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = true
	opts.after = opts.before
	snapst, ts := s.testOpSequence(c, opts)
	// successful revert leaves current == via
	c.Check(snapst.Current.N, Equals, opts.via)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 0)

	return ts
}

func (s *snapmgrTestSuite) testRevertFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = true
	opts.after = opts.before
	s.fakeBackend.linkSnapFailTrigger = fmt.Sprintf(filepath.Join(dirs.SnapMountDir, "some-snap/%d"), opts.via)
	snapst, ts := s.testOpSequence(c, opts)
	// a failed revert will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 0)
	c.Check(s.fakeBackend.ops.Count("undo-copy-snap-data"), Equals, 0)

	return ts
}

func (s *snapmgrTestSuite) testTotalRevertFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	opts.revert = true
	opts.fail = true
	opts.after = opts.before
	snapst, ts := s.testOpSequence(c, opts)
	// a failed revert will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 0)
	c.Check(s.fakeBackend.ops.Count("undo-copy-snap-data"), Equals, 0)

	return ts
}

// *** sequence tests ***

// 1. a boring update
// 1a. ... that works
func (s *snapmgrTestSuite) TestSeqNormal(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 4, after: []int{2, 3, 4}})
}

// 1b. that fails during link
func (s *snapmgrTestSuite) TestSeqNormalFailure(c *C) {
	s.testUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 4})
}

// 1c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalNormalFailure(c *C) {
	// total updates are failures after sequence trimming => we lose a rev
	s.testTotalUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 4, after: []int{2, 3}})
}

// 2. a boring revert
// 2a. that works
func (s *snapmgrTestSuite) TestSeqRevert(c *C) {
	s.testRevertSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2})
}

// 2b. that fails during link
func (s *snapmgrTestSuite) TestSeqRevertFailure(c *C) {
	s.testRevertFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2})
}

// 2c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalRevertFailure(c *C) {
	s.testTotalRevertFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2})
}

// 3. a post-revert update
// 3a. that works
func (s *snapmgrTestSuite) TestSeqPostRevert(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 4, after: []int{1, 2, 4}})
}

// 3b. that fails during link
func (s *snapmgrTestSuite) TestSeqPostRevertFailure(c *C) {
	s.testUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 4})
}

// 3c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalPostRevertFailure(c *C) {
	// lose a rev here as well
	s.testTotalUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 4, after: []int{1, 2}})
}

// 3d. manually requesting the one reverted away from
func (s *snapmgrTestSuite) TestSeqRefreshPostRevertSameRevno(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 3, after: []int{1, 2, 3}})
}

// 4. a post-revert revert
// 4a. that works
func (s *snapmgrTestSuite) TestSeqRevertPostRevert(c *C) {
	s.testRevertSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 1})
}

// 4b. that fails during link
func (s *snapmgrTestSuite) TestSeqRevertPostRevertFailure(c *C) {
	s.testRevertFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 1})
}

// 4c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalRevertPostRevertFailure(c *C) {
	s.testTotalRevertFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 2, via: 1})
}

// 5. an update that missed a rev
// 5a. that works
func (s *snapmgrTestSuite) TestSeqMissedOne(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2}, current: 2, via: 4, after: []int{1, 2, 4}})
}

// 5b. that fails during link
func (s *snapmgrTestSuite) TestSeqMissedOneFailure(c *C) {
	s.testUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2}, current: 2, via: 4})
}

// 5c. that fails after link
func (s *snapmgrTestSuite) TestSeqTotalMissedOneFailure(c *C) {
	// we don't lose a rev here because len(Seq) < 3 going in
	s.testTotalUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2}, current: 2, via: 4, after: []int{1, 2}})
}

// 6. an update that updates to a revision we already have ("ABA update")
// 6a. that works
func (s *snapmgrTestSuite) TestSeqABA(c *C) {
	s.testUpdateSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2, after: []int{1, 3, 2}})
	c.Check(s.fakeBackend.ops[len(s.fakeBackend.ops)-1], DeepEquals, fakeOp{
		op:    "cleanup-trash",
		name:  "some-snap",
		revno: snap.R(2),
	})
}

// 6b. that fails during link
func (s *snapmgrTestSuite) TestSeqABAFailure(c *C) {
	s.testUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2})
	c.Check(s.fakeBackend.ops.First("cleanup-trash"), IsNil)
}

// 6c that fails after link
func (s *snapmgrTestSuite) TestSeqTotalABAFailure(c *C) {
	// we don't lose a rev here because ABA
	s.testTotalUpdateFailureSequence(c, &opSeqOpts{before: []int{1, 2, 3}, current: 3, via: 2, after: []int{1, 2, 3}})
	// XXX: TODO: NOTE!! WARNING!! etc
	//
	// if this happens in real life, things will be weird. revno 2 will
	// have data that has been copied from 3, instead of old 2's data,
	// because the failure occurred *after* nuking the trash. This can
	// happen when things are chained. Because of this, if it were to
	// *actually* happen the correct end sequence would be [1, 3] and not
	// [1, 2, 3]. IRL this scenario can happen if an update that works is
	// chained to an update that fails. Detecting this case is rather hard,
	// and the end result is not nice, and we want to move cleanup to a
	// separate handler & status that will cope with this better (so trash
	// gets nuked after all tasks succeeded).
}

func (s *snapmgrTestSuite) TestSeqRetainConf(c *C) {
	revseq := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	for i := 2; i <= 10; i++ {
		// wot, me, hacky?
		s.TearDownTest(c)
		s.SetUpTest(c)
		s.state.Lock()
		tr := config.NewTransaction(s.state)
		tr.Set("core", "refresh.retain", i)
		tr.Commit()
		s.state.Unlock()

		s.testUpdateSequence(c, &opSeqOpts{before: revseq[:9], current: 9, via: 10, after: revseq[10-i:]})
	}
}

func (s *snapmgrTestSuite) TestUpdateTasksWithOldCurrent(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si1 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	si2 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}
	si3 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}
	si4 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{si1, si2, si3, si4},
		Current:  snap.R(2),
		SnapType: "app",
	})

	// run the update
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 2, ts, s.state)

	// and ensure that it will remove the revisions after "current"
	// (si3, si4)
	var snapsup snapstate.SnapSetup
	tasks := ts.Tasks()

	i := len(tasks) - 6
	c.Check(tasks[i].Kind(), Equals, "clear-snap")
	err = tasks[i].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, si3.Revision)

	i = len(tasks) - 4
	c.Check(tasks[i].Kind(), Equals, "clear-snap")
	err = tasks[i].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, si4.Revision)
}

func (s *snapmgrTestSuite) TestUpdateCanDoBackwards(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si11.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "", snap.R(7), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()
	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "",
				Revision: snap.R(7),
			},
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(7),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestSnapStateNoLocalRevision(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-11),
	}
	snapst := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
	}
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-11))
}

func (s *snapmgrTestSuite) TestSnapStateLocalRevision(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	snapst := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si7},
		Current:  si7.Revision,
	}
	c.Assert(snapst.LocalRevision().Unset(), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	installed, tts, err := snapstate.InstallMany(s.state, []string{"one", "two"}, 0)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	c.Check(installed, DeepEquals, []string{"one", "two"})

	for i, ts := range tts {
		verifyInstallTasks(c, 0, 0, ts, s.state)
		// check that tasksets are in separate lanes
		for _, t := range ts.Tasks() {
			c.Assert(t.Lanes(), DeepEquals, []int{i + 1})
		}
	}
}

func verifyStopReason(c *C, ts *state.TaskSet, reason string) {
	tl := tasksWithKind(ts, "stop-snap-services")
	c.Check(tl, HasLen, 1)

	var stopReason string
	err := tl[0].Get("stop-reason", &stopReason)
	c.Assert(err, IsNil)
	c.Check(stopReason, Equals, reason)

}

func (s *snapmgrTestSuite) TestRemoveMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "one", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "one", SnapID: "one-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})
	snapstate.Set(s.state, "two", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "two", SnapID: "two-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})

	removed, tts, err := snapstate.RemoveMany(s.state, []string{"one", "two"})
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	c.Check(removed, DeepEquals, []string{"one", "two"})

	c.Assert(s.state.TaskCount(), Equals, 8*2)
	for _, ts := range tts {
		c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
			"stop-snap-services",
			"run-hook[remove]",
			"remove-aliases",
			"unlink-snap",
			"remove-profiles",
			"clear-snap",
			"discard-snap",
			"discard-conns",
		})
		verifyStopReason(c, ts, "remove")
	}
}

func tasksWithKind(ts *state.TaskSet, kind string) []*state.Task {
	var tasks []*state.Task
	for _, task := range ts.Tasks() {
		if task.Kind() == kind {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

var gadgetYaml = `
defaults:
    some-snap-ididididididididididid:
        key: value

volumes:
    volume-id:
        bootloader: grub
`

func (s *snapmgrTestSuite) prepareGadget(c *C, extraGadgetYaml ...string) {
	gadgetSideInfo := &snap.SideInfo{RealName: "the-gadget", SnapID: "the-gadget-id", Revision: snap.R(1)}
	gadgetInfo := snaptest.MockSnap(c, `
name: the-gadget
type: gadget
version: 1.0
`, gadgetSideInfo)

	gadgetYamlWhole := strings.Join(append([]string{gadgetYaml}, extraGadgetYaml...), "")
	err := ioutil.WriteFile(filepath.Join(gadgetInfo.MountDir(), "meta/gadget.yaml"), []byte(gadgetYamlWhole), 0600)
	c.Assert(err, IsNil)

	snapstate.Set(s.state, "the-gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&gadgetInfo.SideInfo},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
}

func (s *snapmgrTestSuite) TestConfigDefaults(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11), SnapID: "some-snap-ididididididididididid"},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})
	makeInstalledMockCoreSnap(c)

	defls, err := snapstate.ConfigDefaults(s.state, "some-snap")
	c.Assert(err, IsNil)
	c.Assert(defls, DeepEquals, map[string]interface{}{"key": "value"})

	snapstate.Set(s.state, "local-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "local-snap", Revision: snap.R(5)},
		},
		Current:  snap.R(5),
		SnapType: "app",
	})
	_, err = snapstate.ConfigDefaults(s.state, "local-snap")
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestConfigDefaultsSystem(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnapReadInfo, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c, `
defaults:
    system:
        foo: bar
`)

	makeInstalledMockCoreSnap(c)

	defls, err := snapstate.ConfigDefaults(s.state, "core")
	c.Assert(err, IsNil)
	c.Assert(defls, DeepEquals, map[string]interface{}{"foo": "bar"})
}

func (s *snapmgrTestSuite) TestConfigDefaultsSystemConflictsCoreSnapId(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnapReadInfo, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c, `
defaults:
    system:
        foo: bar
    the-core-snapidididididididididi:
        foo: other-bar
        other-key: other-key-default
`)

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", SnapID: "the-core-snapidididididididididi", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})

	makeInstalledMockCoreSnap(c)

	// 'system' key defaults take precedence over snap-id ones
	defls, err := snapstate.ConfigDefaults(s.state, "core")
	c.Assert(err, IsNil)
	c.Assert(defls, DeepEquals, map[string]interface{}{"foo": "bar"})
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

	gi, err := snap.ReadGadgetInfo(info, false)
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

	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}, snapPath, "edge", snapstate.Flags{})
	c.Assert(err, IsNil)

	var m map[string]interface{}
	runHooks := tasksWithKind(ts, "run-hook")

	// two hooks expected - install and configure
	c.Assert(runHooks, HasLen, 2)
	c.Assert(runHooks[1].Kind(), Equals, "run-hook")
	err = runHooks[1].Get("hook-context", &m)
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]interface{}{"use-defaults": true})
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

	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}, snapPath, "edge", snapstate.Flags{SkipConfigure: true})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	// SkipConfigure is consumed and consulted when creating the taskset
	// but is not copied into SnapSetup
	c.Check(snapsup.Flags.SkipConfigure, Equals, false)
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

	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}, snapPath, "edge", snapstate.Flags{})
	c.Assert(err, IsNil)

	var m map[string]interface{}
	runHooks := tasksWithKind(ts, "run-hook")

	c.Assert(runHooks[0].Kind(), Equals, "run-hook")
	err = runHooks[0].Get("hook-context", &m)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestTransitionCoreTasksNoUbuntuCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	_, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, ErrorMatches, `cannot transition snap "ubuntu-core": not installed`)
}

func verifyTransitionConnectionsTasks(c *C, ts *state.TaskSet) {
	c.Check(taskKinds(ts.Tasks()), DeepEquals, []string{
		"transition-ubuntu-core",
	})

	transIf := ts.Tasks()[0]
	var oldName, newName string
	err := transIf.Get("old-name", &oldName)
	c.Assert(err, IsNil)
	c.Check(oldName, Equals, "ubuntu-core")

	err = transIf.Get("new-name", &newName)
	c.Assert(err, IsNil)
	c.Check(newName, Equals, "core")
}

func (s *snapmgrTestSuite) TestTransitionCoreTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)

	c.Assert(tsl, HasLen, 3)
	// 1. install core
	verifyInstallTasks(c, runCoreConfigure|maybeCore, 0, tsl[0], s.state)
	// 2 transition-connections
	verifyTransitionConnectionsTasks(c, tsl[1])
	// 3 remove-ubuntu-core
	verifyRemoveTasks(c, tsl[2])
}

func (s *snapmgrTestSuite) TestTransitionCoreTasksWithUbuntuCoreAndCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)

	c.Assert(tsl, HasLen, 2)
	// 1. transition connections
	verifyTransitionConnectionsTasks(c, tsl[0])
	// 2. remove ubuntu-core
	verifyRemoveTasks(c, tsl[1])
}

func (s *snapmgrTestSuite) TestTransitionCoreRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
		Channel:  "beta",
	})

	chg := s.state.NewChange("transition-ubuntu-core", "...")
	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)
	for _, ts := range tsl {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		name: "core",
		// the transition has no user associcated with it
		macaroon: "",
	}})
	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{
				{Name: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1), TrackingChannel: "beta", RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1)},
			},
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:  "install",
				Name:    "core",
				Channel: "beta",
			},
			revno: snap.R(11),
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
			name: filepath.Join(dirs.SnapBlobDir, "core_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "core",
				SnapID:   "core-id",
				Channel:  "beta",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  filepath.Join(dirs.SnapBlobDir, "core_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "core/11"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "core",
				SnapID:   "core-id",
				Channel:  "beta",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			name: filepath.Join(dirs.SnapMountDir, "core/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "core",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:   "transition-ubuntu-core:Doing",
			name: "ubuntu-core",
		},
		{
			op:   "remove-snap-aliases",
			name: "ubuntu-core",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "ubuntu-core",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:   "remove-snap-common-data",
			name: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
			stype: "os",
		},
		{
			op:   "discard-namespace",
			name: "ubuntu-core",
		},
		{
			op:   "discard-conns:Doing",
			name: "ubuntu-core",
		},
		{
			op:    "cleanup-trash",
			name:  "core",
			revno: snap.R(11),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestTransitionCoreRunThroughWithCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
		Channel:  "stable",
	})
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
		Channel:  "stable",
	})

	chg := s.state.NewChange("transition-ubuntu-core", "...")
	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)
	for _, ts := range tsl {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, HasLen, 0)
	expected := fakeOps{
		{
			op:   "transition-ubuntu-core:Doing",
			name: "ubuntu-core",
		},
		{
			op:   "remove-snap-aliases",
			name: "ubuntu-core",
		},
		{
			op:   "unlink-snap",
			name: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "ubuntu-core",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-data",
			name: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:   "remove-snap-common-data",
			name: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:    "remove-snap-files",
			name:  filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
			stype: "os",
		},
		{
			op:   "discard-namespace",
			name: "ubuntu-core",
		},
		{
			op:   "discard-conns:Doing",
			name: "ubuntu-core",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestTransitionCoreStartsAutomatically(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(s.state.Changes()[0].Kind(), Equals, "transition-ubuntu-core")
}

func (s *snapmgrTestSuite) TestTransitionCoreTimeLimitWorks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	// tried 3h ago, no retry
	s.state.Set("ubuntu-core-transition-last-retry-time", time.Now().Add(-3*time.Hour))

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 0)

	// tried 7h ago, retry
	s.state.Set("ubuntu-core-transition-last-retry-time", time.Now().Add(-7*time.Hour))

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 1)

	var t time.Time
	s.state.Get("ubuntu-core-transition-last-retry-time", &t)
	c.Assert(time.Now().Sub(t) < 2*time.Minute, Equals, true)
}

func (s *snapmgrTestSuite) TestTransitionCoreNoOtherChanges(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})
	chg := s.state.NewChange("unrelated-change", "unfinished change blocks core transition")
	chg.SetStatus(state.DoStatus)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(s.state.Changes()[0].Kind(), Equals, "unrelated-change")
}

func (s *snapmgrTestSuite) TestTransitionCoreBlocksOtherChanges(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// if we have a ubuntu-core -> core transition
	chg := s.state.NewChange("transition-ubuntu-core", "...")
	chg.SetStatus(state.DoStatus)

	// other tasks block until the transition is done
	_, err := snapstate.Install(s.state, "some-snap", "stable", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Check(err, ErrorMatches, "ubuntu-core to core transition in progress, no other changes allowed until this is done")

	// and when the transition is done, other tasks run
	chg.SetStatus(state.DoneStatus)
	ts, err := snapstate.Install(s.state, "some-snap", "stable", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Check(err, IsNil)
	c.Check(ts, NotNil)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupRunsForUbuntuCore(c *C) {
	s.checkForceDevModeCleanupRuns(c, "ubuntu-core", true)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupRunsForCore(c *C) {
	s.checkForceDevModeCleanupRuns(c, "core", true)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupSkipsRando(c *C) {
	s.checkForceDevModeCleanupRuns(c, "rando", false)
}

func (s *snapmgrTestSuite) checkForceDevModeCleanupRuns(c *C, name string, shouldBeReset bool) {
	r := release.MockForcedDevmode(true)
	defer r()
	c.Assert(release.ReleaseInfo.ForceDevMode(), Equals, true)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, name, &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{{
			RealName: name,
			SnapID:   "id-id-id",
			Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
		Flags:    snapstate.Flags{DevMode: true},
	})

	var snapst1 snapstate.SnapState
	// sanity check
	snapstate.Get(s.state, name, &snapst1)
	c.Assert(snapst1.DevMode, Equals, true)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	var snapst2 snapstate.SnapState
	snapstate.Get(s.state, name, &snapst2)

	c.Check(snapst2.DevMode, Equals, !shouldBeReset)

	var n int
	s.state.Get("fix-forced-devmode", &n)
	c.Check(n, Equals, 1)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupRunsNoSnaps(c *C) {
	r := release.MockForcedDevmode(true)
	defer r()
	c.Assert(release.ReleaseInfo.ForceDevMode(), Equals, true)

	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	var n int
	s.state.Get("fix-forced-devmode", &n)
	c.Check(n, Equals, 1)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupSkipsNonForcedOS(c *C) {
	r := release.MockForcedDevmode(false)
	defer r()
	c.Assert(release.ReleaseInfo.ForceDevMode(), Equals, false)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{{
			RealName: "core",
			SnapID:   "id-id-id",
			Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
		Flags:    snapstate.Flags{DevMode: true},
	})

	var snapst1 snapstate.SnapState
	// sanity check
	snapstate.Get(s.state, "core", &snapst1)
	c.Assert(snapst1.DevMode, Equals, true)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	var snapst2 snapstate.SnapState
	snapstate.Get(s.state, "core", &snapst2)

	// no change
	c.Check(snapst2.DevMode, Equals, true)

	// not really run at all in fact
	var n int
	s.state.Get("fix-forced-devmode", &n)
	c.Check(n, Equals, 0)
}

func (s *snapmgrTestSuite) TestEnsureAliasesV2(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		switch info.Name() {
		case "alias-snap":
			return map[string]string{
				"alias1": "cmd1",
				"alias2": "cmd2",
			}, nil
		}
		return nil, nil
	}

	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "auto",
		},
	})

	s.state.Unlock()
	err := s.snapmgr.Ensure()
	s.state.Lock()
	c.Assert(err, IsNil)

	var gone interface{}
	err = s.state.Get("aliases", &gone)
	c.Assert(err, Equals, state.ErrNoState)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
	})

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "alias-snap",
		},
		{
			op: "update-aliases",
			aliases: []*backend.Alias{
				{"alias1", "alias-snap.cmd1"},
				{"alias2", "alias-snap.cmd2"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestEnsureAliasesV2SnapDisabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		switch info.Name() {
		case "alias-snap":
			return map[string]string{
				"alias1": "cmd1",
				"alias2": "cmd2",
			}, nil
		}
		return nil, nil
	}

	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "auto",
		},
	})

	s.state.Unlock()
	err := s.snapmgr.Ensure()
	s.state.Lock()
	c.Assert(err, IsNil)

	var gone interface{}
	err = s.state.Get("aliases", &gone)
	c.Assert(err, Equals, state.ErrNoState)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
	})

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "alias-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestEnsureAliasesV2MarkAliasTasksInError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "auto",
		},
	})

	// pending old alias task
	t := s.state.NewTask("alias", "...")
	t.Set("aliases", map[string]string{})
	chg := s.state.NewChange("alias chg", "...")
	chg.AddTask(t)

	s.state.Unlock()
	err := s.snapmgr.Ensure()
	s.state.Lock()
	c.Assert(err, IsNil)

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.IsReady(), Equals, true)
	c.Check(t.Status(), Equals, state.ErrorStatus)
}

func (s *snapmgrTestSuite) TestConflictMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, snapName := range []string{"a-snap", "b-snap"} {
		snapstate.Set(s.state, snapName, &snapstate.SnapState{
			Sequence: []*snap.SideInfo{
				{RealName: snapName, Revision: snap.R(11)},
			},
			Current: snap.R(11),
			Active:  false,
		})

		ts, err := snapstate.Enable(s.state, snapName)
		c.Assert(err, IsNil)
		// need a change to make the tasks visible
		s.state.NewChange("enable", "...").AddAll(ts)
	}

	// things that should be ok:
	for _, m := range [][]string{
		{}, //nothing
		{"c-snap"},
		{"c-snap", "d-snap", "e-snap", "f-snap"},
	} {
		c.Check(snapstate.CheckChangeConflictMany(s.state, m, nil), IsNil)
	}

	// things that should not be ok:
	for _, m := range [][]string{
		{"a-snap"},
		{"a-snap", "b-snap"},
		{"a-snap", "c-snap"},
		{"b-snap", "c-snap"},
	} {
		c.Check(snapstate.CheckChangeConflictMany(s.state, m, nil), ErrorMatches, `snap "[^"]*" has "enable" change in progress`)
	}
}

func (s *snapmgrTestSuite) TestInstallWithoutCoreRunThrough1(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// pretend we don't have core
	snapstate.Set(s.state, "core", nil)

	chg := s.state.NewChange("install", "install a snap on a system without core")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(42), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{
			macaroon: s.user.StoreMacaroon,
			name:     "core",
		},
		{
			macaroon: s.user.StoreMacaroon,
			name:     "some-snap",
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
				Action:   "install",
				Name:     "some-snap",
				Revision: snap.R(42),
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
				Action:  "install",
				Name:    "core",
				Channel: "stable",
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
			name: filepath.Join(dirs.SnapBlobDir, "core_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "core",
				Channel:  "stable",
				SnapID:   "core-id",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  filepath.Join(dirs.SnapBlobDir, "core_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "core/11"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
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
			name: filepath.Join(dirs.SnapMountDir, "core/11"),
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
			name: filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Revision: snap.R(42),
			},
		},
		{
			op:    "setup-snap",
			name:  filepath.Join(dirs.SnapBlobDir, "some-snap_42.snap"),
			revno: snap.R(42),
		},
		{
			op:   "copy-data",
			name: filepath.Join(dirs.SnapMountDir, "some-snap/42"),
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
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
			name: filepath.Join(dirs.SnapMountDir, "some-snap/42"),
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
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "stable")
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
	ts1, err := snapstate.Install(s.state, "snap1", "some-channel", snap.R(42), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg1.AddAll(ts1)

	chg2 := s.state.NewChange("install", "install snap 2")
	ts2, err := snapstate.Install(s.state, "snap2", "some-other-channel", snap.R(21), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg2.AddAll(ts2)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran and core was only installed once
	c.Assert(chg1.Err(), IsNil)
	c.Assert(chg2.Err(), IsNil)

	c.Assert(chg1.IsReady(), Equals, true)
	c.Assert(chg2.IsReady(), Equals, true)

	// order in which the changes run is random
	len1 := len(chg1.Tasks())
	len2 := len(chg2.Tasks())
	if len1 > len2 {
		c.Assert(chg1.Tasks(), HasLen, 26)
		c.Assert(chg2.Tasks(), HasLen, 13)
	} else {
		c.Assert(chg1.Tasks(), HasLen, 13)
		c.Assert(chg2.Tasks(), HasLen, 26)
	}

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

	defer s.snapmgr.Stop()
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
		ts1, err := snapstate.Install(s.state, "snap1", "some-channel", snap.R(42), s.user.ID, snapstate.Flags{})
		c.Assert(err, IsNil)
		chg1.AddAll(ts1)

		tasks := ts1.Tasks()
		last := tasks[len(tasks)-1]
		terr := s.state.NewTask("error-trigger", "provoking total undo")
		terr.WaitFor(last)
		chg1.AddTask(terr)

		// chg2 is good
		chg2 := s.state.NewChange("install", "install snap 2")
		ts2, err := snapstate.Install(s.state, "snap2", "some-other-channel", snap.R(21), s.user.ID, snapstate.Flags{})
		c.Assert(err, IsNil)
		chg2.AddAll(ts2)

		// we use our own settle as we need a bigger timeout
		s.state.Unlock()
		err = s.o.Settle(15 * time.Second)
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

func (s behindYourBackStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, error) {
	if len(actions) == 1 && actions[0].Action == "install" && actions[0].Name == "core" {
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

	return s.fakeStore.SnapAction(ctx, currentSnaps, actions, user, opts)
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
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	prereq := ts.Tasks()[0]
	c.Assert(prereq.Kind(), Equals, "prerequisites")
	c.Check(prereq.AtTime().IsZero(), Equals, true)

	s.state.Unlock()
	defer s.snapmgr.Stop()

	// start running the change, this will trigger the
	// prerequisites task, which will trigger the install of core
	// and also call our mock store which will generate a parallel
	// change
	s.snapmgr.Ensure()
	s.snapmgr.Wait()

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
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	})

	snapst = snaps["some-snap"]
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "some-channel",
		Revision: snap.R(11),
	})
}

type contentStore struct {
	*fakeStore
	state *state.State
}

func (s contentStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, error) {
	snaps, err := s.fakeStore.SnapAction(ctx, currentSnaps, actions, user, opts)
	if len(snaps) != 1 {
		panic("expected to be queried for install of only one snap at a time")
	}
	info := snaps[0]
	switch info.Name() {
	case "snap-content-plug":
		info.Plugs = map[string]*snap.PlugInfo{
			"some-plug": {
				Snap:      info,
				Name:      "shared-content",
				Interface: "content",
				Attrs: map[string]interface{}{
					"default-provider": "snap-content-slot",
					"content":          "shared-content",
				},
			},
		}
	case "snap-content-plug-compat":
		info.Plugs = map[string]*snap.PlugInfo{
			"some-plug": {
				Snap:      info,
				Name:      "shared-content",
				Interface: "content",
				Attrs: map[string]interface{}{
					"default-provider": "snap-content-slot:some-slot",
					"content":          "shared-content",
				},
			},
		}
	case "snap-content-slot":
		info.Slots = map[string]*snap.SlotInfo{
			"some-slot": {
				Snap:      info,
				Name:      "shared-content",
				Interface: "content",
				Attrs: map[string]interface{}{
					"content": "shared-content",
				},
			},
		}
	case "snap-content-circular1":
		info.Plugs = map[string]*snap.PlugInfo{
			"circular-plug1": {
				Snap:      info,
				Name:      "circular-plug1",
				Interface: "content",
				Attrs: map[string]interface{}{
					"default-provider": "snap-content-circular2",
					"content":          "circular2",
				},
			},
		}
		info.Slots = map[string]*snap.SlotInfo{
			"circular-slot1": {
				Snap:      info,
				Name:      "circular-slot1",
				Interface: "content",
				Attrs: map[string]interface{}{
					"content": "circular1",
				},
			},
		}
	case "snap-content-circular2":
		info.Plugs = map[string]*snap.PlugInfo{
			"circular-plug2": {
				Snap:      info,
				Name:      "circular-plug2",
				Interface: "content",
				Attrs: map[string]interface{}{
					"default-provider": "snap-content-circular1",
					"content":          "circular2",
				},
			},
		}
		info.Slots = map[string]*snap.SlotInfo{
			"circular-slot2": {
				Snap:      info,
				Name:      "circular-slot2",
				Interface: "content",
				Attrs: map[string]interface{}{
					"content": "circular1",
				},
			},
		}
	}

	return []*snap.Info{info}, err
}

func (s *snapmgrTestSuite) TestInstallDefaultProviderRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "snap-content-plug", "stable", snap.R(42), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
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
			Action:   "install",
			Name:     "snap-content-plug",
			Revision: snap.R(42),
		},
		revno:  snap.R(42),
		userID: 1,
	}, {
		op:     "storesvc-snap-action",
		userID: 1,
	}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:  "install",
			Name:    "snap-content-slot",
			Channel: "stable",
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
		name: filepath.Join(dirs.SnapBlobDir, "snap-content-slot_11.snap"),
		sinfo: snap.SideInfo{
			RealName: "snap-content-slot",
			Channel:  "stable",
			SnapID:   "snap-content-slot-id",
			Revision: snap.R(11),
		},
	}, {
		op:    "setup-snap",
		name:  filepath.Join(dirs.SnapBlobDir, "snap-content-slot_11.snap"),
		revno: snap.R(11),
	}, {
		op:   "copy-data",
		name: filepath.Join(dirs.SnapMountDir, "snap-content-slot/11"),
		old:  "<no-old>",
	}, {
		op:    "setup-profiles:Doing",
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
		name: filepath.Join(dirs.SnapMountDir, "snap-content-slot/11"),
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
		name: filepath.Join(dirs.SnapBlobDir, "snap-content-plug_42.snap"),
		sinfo: snap.SideInfo{
			RealName: "snap-content-plug",
			SnapID:   "snap-content-plug-id",
			Revision: snap.R(42),
		},
	}, {
		op:    "setup-snap",
		name:  filepath.Join(dirs.SnapBlobDir, "snap-content-plug_42.snap"),
		revno: snap.R(42),
	}, {
		op:   "copy-data",
		name: filepath.Join(dirs.SnapMountDir, "snap-content-plug/42"),
		old:  "<no-old>",
	}, {
		op:    "setup-profiles:Doing",
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
		name: filepath.Join(dirs.SnapMountDir, "snap-content-plug/42"),
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
}

func (s *snapmgrTestSuite) TestInstallDefaultProviderCircular(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "snap-content-circular1", "some-channel", snap.R(42), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	// and both circular snaps got linked
	c.Check(s.fakeBackend.ops, testutil.DeepContains, fakeOp{
		op:   "link-snap",
		name: filepath.Join(dirs.SnapMountDir, "snap-content-circular1/42"),
	})
	c.Check(s.fakeBackend.ops, testutil.DeepContains, fakeOp{
		op:   "link-snap",
		name: filepath.Join(dirs.SnapMountDir, "snap-content-circular2/11"),
	})
}

func (s *snapmgrTestSuite) TestInstallDefaultProviderCompat(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "snap-content-plug-compat", "some-channel", snap.R(42), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	// and both circular snaps got linked
	c.Check(s.fakeBackend.ops, testutil.DeepContains, fakeOp{
		op:   "link-snap",
		name: filepath.Join(dirs.SnapMountDir, "snap-content-plug-compat/42"),
	})
	c.Check(s.fakeBackend.ops, testutil.DeepContains, fakeOp{
		op:   "link-snap",
		name: filepath.Join(dirs.SnapMountDir, "snap-content-slot/11"),
	})
}

func (s *snapmgrTestSuite) TestSnapManagerLegacyRefreshSchedule(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, t := range []struct {
		in     string
		out    string
		legacy bool
	}{
		{"", snapstate.DefaultRefreshSchedule, false},
		{"invalid schedule", snapstate.DefaultRefreshSchedule, false},
		{"8:00-12:00", "8:00-12:00", true},
		// using the legacy configuration option with a new-style
		// refresh.timer string is rejected (i.e. the legacy parser is
		// used for the parsing)
		{"0:00~24:00/24", snapstate.DefaultRefreshSchedule, false},
	} {
		if t.in != "" {
			tr := config.NewTransaction(s.state)
			tr.Set("core", "refresh.timer", "")
			tr.Set("core", "refresh.schedule", t.in)
			tr.Commit()
		}
		scheduleStr, legacy, err := s.snapmgr.RefreshSchedule()
		c.Check(err, IsNil)
		c.Check(scheduleStr, Equals, t.out)
		c.Check(legacy, Equals, t.legacy)
	}
}

func (s *snapmgrTestSuite) TestSnapManagerRefreshSchedule(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, t := range []struct {
		in  string
		out string
	}{
		{"", snapstate.DefaultRefreshSchedule},
		{"invalid schedule", snapstate.DefaultRefreshSchedule},
		{"8:00-12:00", "8:00-12:00"},
		// this is only valid under the new schedule parser
		{"9:00~15:00/2,,mon,20:00", "9:00~15:00/2,,mon,20:00"},
	} {
		if t.in != "" {
			tr := config.NewTransaction(s.state)
			tr.Set("core", "refresh.timer", t.in)
			tr.Commit()
		}
		scheduleStr, legacy, err := s.snapmgr.RefreshSchedule()
		c.Check(err, IsNil)
		c.Check(scheduleStr, Equals, t.out)
		c.Check(legacy, Equals, false)
	}
}

func (s *snapmgrTestSuite) TestSideInfoPaid(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	ts, err := snapstate.Install(s.state, "some-snap", "channel-for-paid", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "install paid snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
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
	ts, err := snapstate.Install(s.state, "some-snap", "channel-for-private", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "install private snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snap has private sideinfo
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.CurrentSideInfo().Private, Equals, true)
	c.Check(snapst.CurrentSideInfo().Paid, Equals, false)
}

func (s *snapmgrTestSuite) TestInstallPathWithLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
layout:
 /usr:
  bind: $SNAP/usr
`)
	_, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(8)}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, ErrorMatches, "cannot use experimental 'layouts' feature.*")

	// enable layouts
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, err = snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(8)}, mockSnap, "", snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	_, err := snapstate.Install(s.state, "some-snap", "channel-for-layout", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "cannot use experimental 'layouts' feature.*")

	// enable layouts
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, err = snapstate.Install(s.state, "some-snap", "channel-for-layout", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-snap", "channel-for-layout", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "cannot use experimental 'layouts' feature.*")

	// enable layouts
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, err = snapstate.Update(s.state, "some-snap", "channel-for-layout", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateManyExplicitLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:  true,
		Channel: "channel-for-layout",
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, _, err := snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap"}, s.user.ID)
	c.Assert(err, ErrorMatches, "cannot use experimental 'layouts' feature.*")

	// enable layouts
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, _, err = snapstate.UpdateMany(context.TODO(), s.state, []string{"some-snap"}, s.user.ID)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateManyLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:  true,
		Channel: "channel-for-layout",
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	refreshes, _, err := snapstate.UpdateMany(context.TODO(), s.state, nil, s.user.ID)
	c.Assert(err, IsNil)
	c.Assert(refreshes, HasLen, 0)

	// enable layouts
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	refreshes, _, err = snapstate.UpdateMany(context.TODO(), s.state, nil, s.user.ID)
	c.Assert(err, IsNil)
	c.Assert(refreshes, DeepEquals, []string{"some-snap"})
}

func (s *snapmgrTestSuite) TestInjectTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	lane := s.state.NewLane()

	// setup main task and two tasks waiting for it; all part of same change
	chg := s.state.NewChange("change", "")
	t0 := s.state.NewTask("task1", "")
	chg.AddTask(t0)
	t0.JoinLane(lane)
	t01 := s.state.NewTask("task1-1", "")
	t01.WaitFor(t0)
	chg.AddTask(t01)
	t02 := s.state.NewTask("task1-2", "")
	t02.WaitFor(t0)
	chg.AddTask(t02)

	// setup extra tasks
	t1 := s.state.NewTask("task2", "")
	t2 := s.state.NewTask("task3", "")
	ts := state.NewTaskSet(t1, t2)

	snapstate.InjectTasks(t0, ts)

	// verify that extra tasks are now part of same change
	c.Assert(t1.Change().ID(), Equals, t0.Change().ID())
	c.Assert(t2.Change().ID(), Equals, t0.Change().ID())
	c.Assert(t1.Change().ID(), Equals, chg.ID())

	c.Assert(t1.Lanes(), DeepEquals, []int{lane})

	// verify that halt tasks of the main task now wait for extra tasks
	c.Assert(t1.HaltTasks(), HasLen, 2)
	c.Assert(t2.HaltTasks(), HasLen, 2)
	c.Assert(t1.HaltTasks(), DeepEquals, t2.HaltTasks())

	ids := []string{t1.HaltTasks()[0].Kind(), t2.HaltTasks()[1].Kind()}
	sort.Strings(ids)
	c.Assert(ids, DeepEquals, []string{"task1-1", "task1-2"})

	// verify that extra tasks wait for the main task
	c.Assert(t1.WaitTasks(), HasLen, 1)
	c.Assert(t1.WaitTasks()[0].Kind(), Equals, "task1")
	c.Assert(t2.WaitTasks(), HasLen, 1)
	c.Assert(t2.WaitTasks()[0].Kind(), Equals, "task1")
}

func (s *snapmgrTestSuite) TestInjectTasksWithNullChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// setup main task
	t0 := s.state.NewTask("task1", "")
	t01 := s.state.NewTask("task1-1", "")
	t01.WaitFor(t0)

	// setup extra task
	t1 := s.state.NewTask("task2", "")
	ts := state.NewTaskSet(t1)

	snapstate.InjectTasks(t0, ts)

	c.Assert(t1.Lanes(), DeepEquals, []int{0})

	// verify that halt tasks of the main task now wait for extra tasks
	c.Assert(t1.HaltTasks(), HasLen, 1)
	c.Assert(t1.HaltTasks()[0].Kind(), Equals, "task1-1")
}

func hasConfigureTask(ts *state.TaskSet) bool {
	for _, tk := range taskKinds(ts.Tasks()) {
		if tk == "run-hook[configure]" {
			return true
		}
	}
	return false
}

func (s *snapmgrTestSuite) TestNoConfigureForBasesTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// normal snaps get a configure task
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, true)

	// but bases do not for install
	ts, err = snapstate.Install(s.state, "some-base", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, false)

	// or for refresh
	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "base",
	})
	ts, err = snapstate.Update(s.state, "some-base", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, false)
}

func (s *snapmgrTestSuite) TestNoConfigureForSnapdSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// but snapd do not for install
	ts, err := snapstate.Install(s.state, "snapd", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, false)

	// or for refresh
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "app",
	})
	ts, err = snapstate.Update(s.state, "snapd", "", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, false)

}

func (s snapmgrTestSuite) TestCanLoadOldSnapSetupWithoutType(c *C) {
	// ensure we don't crash when loading a SnapSetup json without
	// a type set
	oldSnapSetup := []byte(`{
 "snap-path":"/some/path",
 "side-info": {
    "channel": "edge",
    "name": "some-snap",
    "revision": "1",
    "snap-id": "some-snap-id"
 }
}`)
	var snapsup snapstate.SnapSetup
	err := json.Unmarshal(oldSnapSetup, &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.SnapPath, Equals, "/some/path")
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		Channel:  "edge",
		RealName: "some-snap",
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
	})
	c.Check(snapsup.Type, Equals, snap.Type(""))
}

type canDisableSuite struct{}

var _ = Suite(&canDisableSuite{})

func (s *canDisableSuite) TestCanDisable(c *C) {
	for _, tt := range []struct {
		typ        snap.Type
		canDisable bool
	}{
		{snap.TypeApp, true},
		{snap.TypeGadget, false},
		{snap.TypeKernel, false},
		{snap.TypeOS, false},
	} {
		info := &snap.Info{Type: tt.typ}
		c.Check(snapstate.CanDisable(info), Equals, tt.canDisable)
	}
}
