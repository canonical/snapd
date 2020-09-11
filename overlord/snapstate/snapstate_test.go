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
	"context"
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

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox"
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
	se      *overlord.StateEngine
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend
	fakeStore   *fakeStore

	bl *bootloadertest.MockBootloader

	user  *auth.UserState
	user2 *auth.UserState
	user3 *auth.UserState
}

func (s *snapmgrTestSuite) settle(c *C) {
	err := s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
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

	restoreCheckFreeSpace := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error { return nil })
	s.AddCleanup(restoreCheckFreeSpace)

	s.fakeBackend = &fakeSnappyBackend{}
	s.fakeBackend.emptyContainer = emptyContainer(c)
	s.fakeStore = &fakeStore{
		fakeCurrentProgress: 75,
		fakeTotalProgress:   100,
		fakeBackend:         s.fakeBackend,
		state:               s.state,
	}

	// setup a bootloader for policy and boot
	s.bl = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.bl)
	s.AddCleanup(func() { bootloader.Force(nil) })

	oldSetupInstallHook := snapstate.SetupInstallHook
	oldSetupPreRefreshHook := snapstate.SetupPreRefreshHook
	oldSetupPostRefreshHook := snapstate.SetupPostRefreshHook
	oldSetupRemoveHook := snapstate.SetupRemoveHook
	snapstate.SetupInstallHook = hookstate.SetupInstallHook
	snapstate.SetupPreRefreshHook = hookstate.SetupPreRefreshHook
	snapstate.SetupPostRefreshHook = hookstate.SetupPostRefreshHook
	snapstate.SetupRemoveHook = hookstate.SetupRemoveHook

	var err error
	s.snapmgr, err = snapstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(err, IsNil)

	AddForeignTaskHandlers(s.o.TaskRunner(), s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	s.o.AddManager(s.snapmgr)
	s.o.AddManager(s.o.TaskRunner())
	s.se = s.o.StateEngine()
	c.Assert(s.o.StartUp(), IsNil)

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

	s.BaseTest.AddCleanup(snapstate.MockReRefreshRetryTimeout(time.Second / 200))
	s.BaseTest.AddCleanup(snapstate.MockReRefreshUpdateMany(func(context.Context, *state.State, []string, int, snapstate.UpdateFilter, *snapstate.Flags, string) ([]string, []*state.TaskSet, error) {
		return nil, nil, nil
	}))

	oldEstimateSnapshotSize := snapstate.EstimateSnapshotSize
	snapstate.EstimateSnapshotSize = func(st *state.State, instanceName string, users []string) (uint64, error) {
		return 1, nil
	}
	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []*snap.Info, userID int) (uint64, error) {
		return 0, nil
	})
	s.AddCleanup(restoreInstallSize)

	oldAutomaticSnapshot := snapstate.AutomaticSnapshot
	snapstate.AutomaticSnapshot = func(st *state.State, instanceName string) (ts *state.TaskSet, err error) {
		task := st.NewTask("save-snapshot", "...")
		ts = state.NewTaskSet(task)
		return ts, nil
	}

	oldAutomaticSnapshotExpiration := snapstate.AutomaticSnapshotExpiration
	snapstate.AutomaticSnapshotExpiration = func(st *state.State) (time.Duration, error) { return 1, nil }
	s.BaseTest.AddCleanup(func() {
		snapstate.EstimateSnapshotSize = oldEstimateSnapshotSize
		snapstate.AutomaticSnapshot = oldAutomaticSnapshot
		snapstate.AutomaticSnapshotExpiration = oldAutomaticSnapshotExpiration
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

	s.state.Set("seeded", true)
	s.state.Set("seed-time", time.Now())

	r := snapstatetest.MockDeviceModel(DefaultModel())
	s.BaseTest.AddCleanup(r)

	s.state.Set("refresh-privacy-key", "privacy-key")
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
}

func (s *snapmgrTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	snapstate.ValidateRefreshes = nil
	snapstate.AutoAliases = nil
	snapstate.CanAutoRefresh = nil
}

type ForeignTaskTracker interface {
	ForeignTask(kind string, status state.Status, snapsup *snapstate.SnapSetup)
}

func AddForeignTaskHandlers(runner *state.TaskRunner, tracker ForeignTaskTracker) {
	// Add fake handlers for tasks handled by interfaces manager
	fakeHandler := func(task *state.Task, _ *tomb.Tomb) error {
		task.State().Lock()
		kind := task.Kind()
		status := task.Status()
		snapsup, err := snapstate.TaskSnapSetup(task)
		task.State().Unlock()
		if err != nil {
			return err
		}

		tracker.ForeignTask(kind, status, snapsup)

		return nil
	}
	runner.AddHandler("setup-profiles", fakeHandler, fakeHandler)
	runner.AddHandler("auto-connect", fakeHandler, nil)
	runner.AddHandler("auto-disconnect", fakeHandler, nil)
	runner.AddHandler("remove-profiles", fakeHandler, fakeHandler)
	runner.AddHandler("discard-conns", fakeHandler, fakeHandler)
	runner.AddHandler("validate-snap", fakeHandler, nil)
	runner.AddHandler("transition-ubuntu-core", fakeHandler, nil)
	runner.AddHandler("transition-to-snapd-snap", fakeHandler, nil)

	// Add handler to test full aborting of changes
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	runner.AddHandler("error-trigger", erroringHandler, nil)

	runner.AddHandler("save-snapshot", func(task *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)
	runner.AddHandler("run-hook", func(task *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)
	runner.AddHandler("configure-snapd", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)

}

func (s *snapmgrTestSuite) TestCleanSnapStateGet(c *C) {
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		},
		Current:         snap.R(1),
		SnapType:        "os",
		TrackingChannel: "foo/stable",
		InstanceKey:     "bar",
	}

	s.state.Lock()

	defer s.state.Unlock()
	snapstate.Set(s.state, "no-instance-key", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	err := snapstate.Get(s.state, "bar", nil)
	c.Assert(err, ErrorMatches, "internal error: snapst is nil")

	err = snapstate.Get(s.state, "no-instance-key", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst, DeepEquals, snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})
}

func (s *snapmgrTestSuite) TestStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	sto := &store.Store{}
	snapstate.ReplaceStore(s.state, sto)
	store1 := snapstate.Store(s.state, nil)
	c.Check(store1, Equals, sto)

	// cached
	store2 := snapstate.Store(s.state, nil)
	c.Check(store2, Equals, sto)
}

func (s *snapmgrTestSuite) TestStoreWithDeviceContext(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	stoA := &store.Store{}
	snapstate.ReplaceStore(s.state, stoA)
	store1 := snapstate.Store(s.state, nil)
	c.Check(store1, Equals, stoA)

	stoB := &store.Store{}

	// cached
	store2 := snapstate.Store(s.state, &snapstatetest.TrivialDeviceContext{})
	c.Check(store2, Equals, stoA)

	// from context
	store3 := snapstate.Store(s.state, &snapstatetest.TrivialDeviceContext{CtxStore: stoB})
	c.Check(store3, Equals, stoB)
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
	doesReRefresh
	updatesGadget
	noConfigure
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

func verifyLastTasksetIsReRefresh(c *C, tts []*state.TaskSet) {
	ts := tts[len(tts)-1]
	c.Assert(ts.Tasks(), HasLen, 1)
	reRefresh := ts.Tasks()[0]
	c.Check(reRefresh.Kind(), Equals, "check-rerefresh")
	// nothing should wait on it
	c.Check(reRefresh.NumHaltTasks(), Equals, 0)
}

func verifyRemoveTasks(c *C, ts *state.TaskSet) {
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"run-hook[remove]",
		"auto-disconnect",
		"save-snapshot",
		"remove-aliases",
		"unlink-snap",
		"remove-profiles",
		"clear-snap",
		"discard-snap",
	})
	verifyStopReason(c, ts, "remove")
}

func verifyCoreRemoveTasks(c *C, ts *state.TaskSet) {
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"run-hook[remove]",
		"auto-disconnect",
		"remove-aliases",
		"unlink-snap",
		"remove-profiles",
		"clear-snap",
		"discard-snap",
	})
	verifyStopReason(c, ts, "remove")
}

func checkIsAutoRefresh(c *C, tasks []*state.Task, expected bool) {
	for _, t := range tasks {
		if t.Kind() == "download-snap" {
			var snapsup snapstate.SnapSetup
			err := t.Get("snap-setup", &snapsup)
			c.Assert(err, IsNil)
			c.Check(snapsup.IsAutoRefresh, Equals, expected)
			return
		}
	}
	c.Fatalf("cannot find download-snap task in %v", tasks)
}

func (s *snapmgrTestSuite) TestLastIndexFindsLast(c *C) {
	snapst := &snapstate.SnapState{Sequence: []*snap.SideInfo{
		{Revision: snap.R(7)},
		{Revision: snap.R(11)},
		{Revision: snap.R(11)},
	}}
	c.Check(snapst.LastIndex(snap.R(11)), Equals, 2)
}

func maybeMockClassicSupport(c *C) (restore func()) {
	if dirs.SupportsClassicConfinement() {
		return func() {}
	}

	d := filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/snap")
	err := os.MkdirAll(d, 0755)
	c.Assert(err, IsNil)
	snapSymlink := filepath.Join(dirs.GlobalRootDir, "snap")
	err = os.Symlink(d, snapSymlink)
	c.Assert(err, IsNil)

	return func() { os.Remove(snapSymlink) }
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
		"run-hook[check-health]",
	})
	// a revert is a special refresh
	verifyStopReason(c, ts, "refresh")

	snapsup, err := snapstate.TaskSnapSetup(tasks[0])
	c.Assert(err, IsNil)
	flags.setup.Revert = true
	c.Check(snapsup.Flags, Equals, flags.setup)
	c.Check(snapsup.Type, Equals, snap.TypeApp)

	chg := s.state.NewChange("revert", "revert snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
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
	restore := maybeMockClassicSupport(c)
	defer restore()

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
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.testRevertTasks(snapstate.Flags{Classic: true}, c)
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
		"run-hook[check-health]",
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

	ts, err := snapstate.Switch(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"})
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

	ts, err := snapstate.Switch(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("switch-snap", "...").AddAll(ts)

	_, err = snapstate.Switch(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "other-channel"})
	c.Check(err, ErrorMatches, `snap "some-snap" has "switch-snap" change in progress`)
}

func (s *snapmgrTestSuite) TestSwitchUnhappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	_, err := snapstate.Switch(s.state, "non-existing-snap", &snapstate.RevisionOptions{Channel: "some-channel"})
	c.Assert(err, ErrorMatches, `snap "non-existing-snap" is not installed`)
}

func (s *snapmgrTestSuite) TestSwitchRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
	})

	_, err := snapstate.Switch(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(42)})
	c.Assert(err, ErrorMatches, "cannot switch revision")
}

func (s *snapmgrTestSuite) TestSwitchKernelTrackForbidden(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

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

	_, err := snapstate.Switch(s.state, "kernel", &snapstate.RevisionOptions{Channel: "new-channel"})
	c.Assert(err, ErrorMatches, `cannot switch from kernel track "18" as specified for the \(device\) model to "new-channel"`)
}

func (s *snapmgrTestSuite) TestSwitchKernelTrackRiskOnlyIsOK(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

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

	_, err := snapstate.Switch(s.state, "kernel", &snapstate.RevisionOptions{Channel: "18/beta"})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestSwitchKernelTrackRiskOnlyDefaultTrackIsOK(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

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

	_, err := snapstate.Switch(s.state, "kernel", &snapstate.RevisionOptions{Channel: "beta"})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestSwitchGadgetTrackForbidden(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

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

	_, err := snapstate.Switch(s.state, "brand-gadget", &snapstate.RevisionOptions{Channel: "new-channel"})
	c.Assert(err, ErrorMatches, `cannot switch from gadget track "18" as specified for the \(device\) model to "new-channel"`)
}

func (s *snapmgrTestSuite) TestSwitchGadgetTrackRiskOnlyIsOK(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

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

	_, err := snapstate.Switch(s.state, "brand-gadget", &snapstate.RevisionOptions{Channel: "18/beta"})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestSwitchGadgetTrackRiskOnlyDefaultTrackIsOK(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

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

	_, err := snapstate.Switch(s.state, "brand-gadget", &snapstate.RevisionOptions{Channel: "beta"})
	c.Assert(err, IsNil)
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

func (s *snapmgrTestSuite) TestDoInstallWithSlots(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})

	ts, err := snapstate.Install(context.Background(), s.state, "snap-content-slot", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.PlugsOnly, Equals, false)
}

func (s *snapmgrTestSuite) TestDoUpdateHadSlots(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		if name != "some-snap" {
			return s.fakeBackend.ReadInfo(name, si)
		}

		info := &snap.Info{
			SideInfo: *si,
			SnapType: snap.TypeApp,
		}
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
		return info, nil
	})

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.PlugsOnly, Equals, false)
}

func (s *snapmgrTestSuite) TestRemoveTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(0), nil)
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	verifyRemoveTasks(c, ts)
}

func (s *snapmgrTestSuite) TestRemoveTasksAutoSnapshotDisabled(c *C) {
	snapstate.AutomaticSnapshot = func(st *state.State, instanceName string) (ts *state.TaskSet, err error) {
		return nil, snapstate.ErrNothingToDo
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(0), nil)
	c.Assert(err, IsNil)

	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"run-hook[remove]",
		"auto-disconnect",
		"remove-aliases",
		"unlink-snap",
		"remove-profiles",
		"clear-snap",
		"discard-snap",
	})
}

func (s *snapmgrTestSuite) TestRemoveTasksAutoSnapshotDisabledByPurgeFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(0), &snapstate.RemoveFlags{Purge: true})
	c.Assert(err, IsNil)

	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"run-hook[remove]",
		"auto-disconnect",
		"remove-aliases",
		"unlink-snap",
		"remove-profiles",
		"clear-snap",
		"discard-snap",
	})
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

	ts, err := snapstate.Remove(s.state, "foo", snap.R(11), nil)
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

	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("remove", "...").AddAll(ts)

	_, err = snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, ErrorMatches, `snap "some-snap" has "remove" change in progress`)
}

func (s *snapmgrTestSuite) testRemoveDiskSpaceCheck(c *C, featureFlag, automaticSnapshot bool) error {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error {
		// osutil.CheckFreeSpace shouldn't be hit if either featureFlag
		// or automaticSnapshot is false. If both are true then we return disk
		// space error which should result in snapstate.InsufficientSpaceError
		// on remove().
		return &osutil.NotEnoughDiskSpaceError{}
	})
	defer restore()

	var automaticSnapshotCalled bool
	snapstate.AutomaticSnapshot = func(st *state.State, instanceName string) (ts *state.TaskSet, err error) {
		automaticSnapshotCalled = true
		if automaticSnapshot {
			t := s.state.NewTask("foo", "")
			ts = state.NewTaskSet(t)
			return ts, nil
		}
		// ErrNothingToDo is returned if automatic snapshots are disabled
		return nil, snapstate.ErrNothingToDo
	}

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-remove", featureFlag)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", Revision: snap.R(11)}},
		Current:  snap.R(11),
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(automaticSnapshotCalled, Equals, true)
	return err
}

func (s *snapmgrTestSuite) TestRemoveDiskSpaceCheckDoesNothingWhenNoSnapshot(c *C) {
	featureFlag := true
	snapshot := false
	err := s.testRemoveDiskSpaceCheck(c, featureFlag, snapshot)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveDiskSpaceCheckDisabledByFeatureFlag(c *C) {
	featureFlag := false
	snapshot := true
	err := s.testRemoveDiskSpaceCheck(c, featureFlag, snapshot)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveDiskSpaceForSnapshotError(c *C) {
	featureFlag := true
	snapshot := true
	// both the snapshot and disk check feature are enabled, so we should hit
	// the disk check (which fails).
	err := s.testRemoveDiskSpaceCheck(c, featureFlag, snapshot)
	c.Assert(err, NotNil)

	diskSpaceErr := err.(*snapstate.InsufficientSpaceError)
	c.Assert(diskSpaceErr, ErrorMatches, `cannot create automatic snapshot when removing last revision of the snap: insufficient space.*`)
	c.Check(diskSpaceErr.Path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd"))
	c.Check(diskSpaceErr.Snaps, DeepEquals, []string{"some-snap"})
	c.Check(diskSpaceErr.ChangeKind, Equals, "remove")
}

func (s *snapmgrTestSuite) TestDisableSnapDisabledServicesSaved(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	ts, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(disableChg.Err(), IsNil)
	c.Assert(disableChg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})
}

func (s *snapmgrTestSuite) TestEnableSnapDisabledServicesPassedAroundHappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	disableTs, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(disableTs)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(disableChg.Err(), IsNil)
	c.Assert(disableChg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})

	enableChg := s.state.NewChange("enable", "disable a snap")
	enableTs, err := snapstate.Enable(s.state, "services-snap")
	c.Assert(err, IsNil)
	enableChg.AddAll(enableTs)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(enableChg.Err(), IsNil)
	c.Assert(enableChg.IsReady(), Equals, true)

	// check the ops that will be provided disabledServices
	svcStateOp := s.fakeBackend.ops.First("current-snap-service-states")
	c.Assert(svcStateOp, Not(IsNil))
	c.Assert(svcStateOp.disabledServices, DeepEquals, []string{"svc1", "svc2"})

	linkStateOp := s.fakeBackend.ops.First("link-snap")
	c.Assert(linkStateOp, Not(IsNil))
	c.Assert(linkStateOp.disabledServices, DeepEquals, []string{"svc1", "svc2"})
}

func (s *snapmgrTestSuite) TestEnableSnapDisabledServicesNotSaved(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	disableTs, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(disableTs)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(disableChg.Err(), IsNil)
	c.Assert(disableChg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})

	enableChg := s.state.NewChange("enable", "disable a snap")
	enableTs, err := snapstate.Enable(s.state, "services-snap")
	c.Assert(err, IsNil)
	enableChg.AddAll(enableTs)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(enableChg.Err(), IsNil)
	c.Assert(enableChg.IsReady(), Equals, true)

	// get the snap state again
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that there is nothing in the last active disabled services list
	// because we re-enabled the snap and there should be nothing we have to
	// keep track of in the state anymore
	c.Assert(snapst.LastActiveDisabledServices, HasLen, 0)
}

func (s *snapmgrTestSuite) TestEnableSnapMissingDisabledServicesMergedAndSaved(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
		// keep this to make gofmt 1.10 happy
		LastActiveDisabledServices: []string{"missing-svc3"},
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	disableTs, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(disableTs)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(disableChg.Err(), IsNil)
	c.Assert(disableChg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"missing-svc3", "svc1", "svc2"})

	enableChg := s.state.NewChange("enable", "disable a snap")
	enableTs, err := snapstate.Enable(s.state, "services-snap")
	c.Assert(err, IsNil)
	enableChg.AddAll(enableTs)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(enableChg.Err(), IsNil)
	c.Assert(enableChg.IsReady(), Equals, true)

	// get the snap state again
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that there is nothing in the last active disabled services list
	// because we re-enabled the snap and there should be nothing we have to
	// keep track of in the state anymore
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"missing-svc3"})
}

func (s *snapmgrTestSuite) TestEnableSnapMissingDisabledServicesSaved(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
		// keep this to make gofmt 1.10 happy
		LastActiveDisabledServices: []string{"missing-svc3"},
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	disableTs, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(disableTs)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(disableChg.Err(), IsNil)
	c.Assert(disableChg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"missing-svc3"})

	enableChg := s.state.NewChange("enable", "disable a snap")
	enableTs, err := snapstate.Enable(s.state, "services-snap")
	c.Assert(err, IsNil)
	enableChg.AddAll(enableTs)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(enableChg.Err(), IsNil)
	c.Assert(enableChg.IsReady(), Equals, true)

	// get the snap state again
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that there is nothing in the last active disabled services list
	// because we re-enabled the snap and there should be nothing we have to
	// keep track of in the state anymore
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"missing-svc3"})
}

func makeTestSnap(c *C, snapYamlContent string) (snapFilePath string) {
	return snaptest.MakeTestSnapWithFiles(c, snapYamlContent, nil)
}

func (s *snapmgrTestSuite) TestRemoveRunThrough(c *C) {
	c.Assert(snapstate.KeepAuxStoreInfo("some-snap-id", nil), IsNil)
	c.Check(snapstate.AuxStoreInfoFilename("some-snap-id"), testutil.FilePresent)
	si := snap.SideInfo{
		SnapID:   "some-snap-id",
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
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "remove-snap-data-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapDataDir, "some-snap"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "remove-snap-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap"),
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
		if t.Kind() == "save-snapshot" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		switch t.Kind() {
		case "discard-conns":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
			}
		case "clear-snap", "discard-snap":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					SnapID:   "some-snap-id",
					Revision: snap.R(7),
				},
			}
		default:
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					SnapID:   "some-snap-id",
					Revision: snap.R(7),
				},
				Type:      snap.TypeApp,
				PlugsOnly: true,
			}

		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
	c.Check(snapstate.AuxStoreInfoFilename("some-snap-id"), testutil.FileAbsent)

}

func (s *snapmgrTestSuite) TestParallelInstanceRemoveRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	// pretend we have both a regular snap and a parallel instance
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{&si},
		Current:     si.Revision,
		SnapType:    "app",
		InstanceKey: "instance",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap_instance", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap_instance",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:             "remove-snap-data-dir",
			name:           "some-snap_instance",
			path:           filepath.Join(dirs.SnapDataDir, "some-snap"),
			otherInstances: true,
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap_instance",
		},
		{
			op:             "remove-snap-dir",
			name:           "some-snap_instance",
			path:           filepath.Join(dirs.SnapMountDir, "some-snap"),
			otherInstances: true,
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
		if t.Kind() == "save-snapshot" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		switch t.Kind() {
		case "discard-conns":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
				InstanceKey: "instance",
			}
		case "clear-snap", "discard-snap":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: snap.R(7),
				},
				InstanceKey: "instance",
			}
		default:
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: snap.R(7),
				},
				Type:        snap.TypeApp,
				PlugsOnly:   true,
				InstanceKey: "instance",
			}

		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

	// the non-instance snap is still there
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestParallelInstanceRemoveRunThroughOtherInstances(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	// pretend we have both a regular snap and a parallel instance
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{&si},
		Current:     si.Revision,
		SnapType:    "app",
		InstanceKey: "instance",
	})
	snapstate.Set(s.state, "some-snap_other", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{&si},
		Current:     si.Revision,
		SnapType:    "app",
		InstanceKey: "other",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap_instance", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap_instance",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:             "remove-snap-data-dir",
			name:           "some-snap_instance",
			path:           filepath.Join(dirs.SnapDataDir, "some-snap"),
			otherInstances: true,
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap_instance",
		},
		{
			op:             "remove-snap-dir",
			name:           "some-snap_instance",
			path:           filepath.Join(dirs.SnapMountDir, "some-snap"),
			otherInstances: true,
		},
	}
	// start with an easier-to-read error if this fails:
	c.Check(len(s.fakeBackend.ops), Equals, len(expected))
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

	// the other instance is still there
	err = snapstate.Get(s.state, "some-snap_other", &snapst)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveWithManyRevisionsRunThrough(c *C) {
	si3 := snap.SideInfo{
		SnapID:   "some-snap-id",
		RealName: "some-snap",
		Revision: snap.R(3),
	}

	si5 := snap.SideInfo{
		SnapID:   "some-snap-id",
		RealName: "some-snap",
		Revision: snap.R(5),
	}

	si7 := snap.SideInfo{
		SnapID:   "some-snap-id",
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
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/3"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/3"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/5"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/5"),
		},
		{
			op:   "remove-snap-data-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapDataDir, "some-snap"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/5"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
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

	// verify snapSetup info
	tasks := ts.Tasks()
	revnos := []snap.Revision{{N: 7}, {N: 3}, {N: 5}}
	whichRevno := 0
	for _, t := range tasks {
		if t.Kind() == "run-hook" {
			continue
		}
		if t.Kind() == "save-snapshot" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		switch t.Kind() {
		case "discard-conns":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					SnapID:   "some-snap-id",
					RealName: "some-snap",
				},
			}
		case "clear-snap", "discard-snap":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					SnapID:   "some-snap-id",
					RealName: "some-snap",
					Revision: revnos[whichRevno],
				},
			}
		default:
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					SnapID:   "some-snap-id",
					RealName: "some-snap",
					Revision: snap.R(7),
				},
				Type:      snap.TypeApp,
				PlugsOnly: true,
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
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(3), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(len(s.fakeBackend.ops), Equals, 2)
	expected := fakeOps{
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/3"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/3"),
			stype: "app",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		if t.Kind() == "save-snapshot" {
			continue
		}
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
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(2), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(len(s.fakeBackend.ops), Equals, 7)
	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap",
			revno: snap.R(2),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:   "remove-snap-data-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapDataDir, "some-snap"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/2"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
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

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		if t.Kind() == "run-hook" {
			continue
		}
		if t.Kind() == "save-snapshot" {
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
		if t.Kind() == "auto-disconnect" {
			expSnapSetup.PlugsOnly = true
			expSnapSetup.Type = "app"
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

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(2), nil)

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

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(2), nil)
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

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(1), nil)

	c.Check(err, ErrorMatches, `revision 1 of snap "some-snap" is not installed`)
}

func (s *snapmgrTestSuite) TestRemoveRefused(c *C) {
	si := snap.SideInfo{
		RealName: "brand-gadget",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "gadget",
	})

	_, err := snapstate.Remove(s.state, "brand-gadget", snap.R(0), nil)

	c.Check(err, ErrorMatches, `snap "brand-gadget" is not removable: snap is used by the model`)
}

func (s *snapmgrTestSuite) TestRemoveRefusedLastRevision(c *C) {
	si := snap.SideInfo{
		RealName: "brand-gadget",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "gadget",
	})

	_, err := snapstate.Remove(s.state, "brand-gadget", snap.R(7), nil)

	c.Check(err, ErrorMatches, `snap "brand-gadget" is not removable: snap is used by the model`)
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
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
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
	ts, err := snapstate.Remove(s.state, "some-snap", si1.Revision, nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
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
	defer s.se.Stop()
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
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
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
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
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

func (s *snapmgrTestSuite) TestRevertWithBaseRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap-with-base",
		Revision: snap.R(7),
	}
	siOld := snap.SideInfo{
		RealName: "some-snap-with-base",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	// core18 with snapd, no core snap
	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "core18", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core18", SnapID: "core18-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "base",
	})
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// test snap to revert
	snapstate.Set(s.state, "some-snap-with-base", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&siOld, &si},
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap-with-base", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap-with-base",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap-with-base/7"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap-with-base",
			revno: snap.R(2),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap-with-base",
				Revision: snap.R(2),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap-with-base/2"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap-with-base",
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
	err = snapstate.Get(s.state, "some-snap-with-base", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(2))
}

func (s *snapmgrTestSuite) TestParallelInstanceRevertRunThrough(c *C) {
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

	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active:      true,
		SnapType:    "app",
		Sequence:    []*snap.SideInfo{&siOld, &si},
		Current:     si.Revision,
		InstanceKey: "instance",
	})

	// another snap withouth instance key
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{&siOld, &si},
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap_instance", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap_instance",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap_instance",
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
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/2"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap_instance",
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
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.InstanceKey, Equals, "instance")
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

	// non instance snap is not affected
	var nonInstanceSnapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &nonInstanceSnapst)
	c.Assert(err, IsNil)
	c.Assert(nonInstanceSnapst.Current, Equals, snap.R(7))

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
	defer s.se.Stop()
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
		Active:          true,
		SnapType:        "app",
		Sequence:        []*snap.SideInfo{&si, &siNew},
		Current:         snap.R(2),
		TrackingChannel: "latest/edge",
	})

	chg := s.state.NewChange("revert", "revert a snap forward")
	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R(7), snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
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
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
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
	c.Check(snapst.TrackingChannel, Equals, "latest/edge")
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
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
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
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
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
			op: "current-snap-service-states",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
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
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
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
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		// undo stuff here
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
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
		TrackingChannel:     "latest/edge",
		Flags:               flags,
		AliasesPending:      true,
		AutoAliasesDisabled: true,
	})

	chg := s.state.NewChange("enable", "enable a snap")
	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
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
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
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

	first := ts.Tasks()[0]
	snapsup, err := snapstate.TaskSnapSetup(first)
	c.Assert(err, IsNil)
	c.Check(snapsup, DeepEquals, &snapstate.SnapSetup{
		SideInfo:  &si,
		Flags:     flags,
		Type:      snap.TypeApp,
		PlugsOnly: true,
	})
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
		SnapType: "app",
	})

	chg := s.state.NewChange("disable", "disable a snap")
	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
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

	first := ts.Tasks()[0]
	snapsup, err := snapstate.TaskSnapSetup(first)
	c.Assert(err, IsNil)
	c.Check(snapsup, DeepEquals, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(7),
		},
		Type:      snap.TypeApp,
		PlugsOnly: true,
	})
}

func (s *snapmgrTestSuite) TestParallelInstanceEnableRunThrough(c *C) {
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
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Sequence:            []*snap.SideInfo{&si},
		Current:             si.Revision,
		Active:              false,
		TrackingChannel:     "latest/edge",
		Flags:               flags,
		AliasesPending:      true,
		AutoAliasesDisabled: true,
		InstanceKey:         "instance",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence:            []*snap.SideInfo{&si},
		Current:             si.Revision,
		Active:              false,
		TrackingChannel:     "latest/edge",
		Flags:               flags,
		AliasesPending:      true,
		AutoAliasesDisabled: true,
	})

	chg := s.state.NewChange("enable", "enable a snap")
	ts, err := snapstate.Enable(s.state, "some-snap_instance")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:    "candidate",
			sinfo: si,
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap_instance",
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
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Flags, DeepEquals, flags)

	c.Assert(snapst.InstanceKey, Equals, "instance")
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.AliasesPending, Equals, false)
	c.Assert(snapst.AutoAliasesDisabled, Equals, true)

	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Channel, Equals, "edge")
	c.Assert(info.SnapID, Equals, "foo")

	// the non-parallel instance is still disabled
	snapst = snapstate.SnapState{}
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.InstanceKey, Equals, "")
	c.Assert(snapst.Active, Equals, false)
}

func (s *snapmgrTestSuite) TestParallelInstanceDisableRunThrough(c *C) {
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
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Sequence:    []*snap.SideInfo{&si},
		Current:     si.Revision,
		Active:      true,
		InstanceKey: "instance",
	})

	chg := s.state.NewChange("disable", "disable a snap")
	ts, err := snapstate.Disable(s.state, "some-snap_instance")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap_instance",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.InstanceKey, Equals, "instance")
	c.Assert(snapst.Active, Equals, false)
	c.Assert(snapst.AliasesPending, Equals, true)

	// the non-parallel instance is still active
	snapst = snapstate.SnapState{}
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.InstanceKey, Equals, "")
	c.Assert(snapst.Active, Equals, true)
}

type switchScenario struct {
	chanFrom string
	chanTo   string
	cohFrom  string
	cohTo    string
	summary  string
}

var switchScenarios = map[string]switchScenario{
	"no cohort at all": {
		chanFrom: "latest/stable",
		chanTo:   "some-channel/stable",
		cohFrom:  "",
		cohTo:    "",
		summary:  `Switch snap "some-snap" from channel "latest/stable" to "some-channel/stable"`,
	},
	"no cohort, from empty channel": {
		chanFrom: "",
		chanTo:   "some-channel/stable",
		cohFrom:  "",
		cohTo:    "",
		summary:  `Switch snap "some-snap" to channel "some-channel/stable"`,
	},
	"no cohort change requested": {
		chanFrom: "latest/stable",
		chanTo:   "some-channel/stable",
		cohFrom:  "some-cohort",
		cohTo:    "some-cohort",
		summary:  `Switch snap "some-snap" from channel "latest/stable" to "some-channel/stable"`,
	},
	"leave cohort": {
		chanFrom: "latest/stable",
		chanTo:   "latest/stable",
		cohFrom:  "some-cohort",
		cohTo:    "",
		summary:  `Switch snap "some-snap" away from cohort "…me-cohort"`,
	},
	"leave cohort, change channel": {
		chanFrom: "latest/stable",
		chanTo:   "latest/edge",
		cohFrom:  "some-cohort",
		cohTo:    "",
		summary:  `Switch snap "some-snap" from channel "latest/stable" to "latest/edge" and away from cohort "…me-cohort"`,
	},
	"leave cohort, change from empty channel": {
		chanFrom: "",
		chanTo:   "latest/stable",
		cohFrom:  "some-cohort",
		cohTo:    "",
		summary:  `Switch snap "some-snap" to channel "latest/stable" and away from cohort "…me-cohort"`,
	},
	"no channel at all": {
		chanFrom: "",
		chanTo:   "",
		cohFrom:  "some-cohort",
		cohTo:    "some-other-cohort",
		summary:  `Switch snap "some-snap" from cohort "…me-cohort" to "…er-cohort"`,
	},
	"no channel change requested": {
		chanFrom: "latest/stable",
		chanTo:   "latest/stable",
		cohFrom:  "some-cohort",
		cohTo:    "some-other-cohort",
		summary:  `Switch snap "some-snap" from cohort "…me-cohort" to "…er-cohort"`,
	},
	"no channel change requested, from empty cohort": {
		chanFrom: "latest/stable",
		chanTo:   "latest/stable",
		cohFrom:  "",
		cohTo:    "some-cohort",
		summary:  `Switch snap "some-snap" from no cohort to "…me-cohort"`,
	},
	"all change": {
		chanFrom: "latest/stable",
		chanTo:   "latest/edge",
		cohFrom:  "some-cohort",
		cohTo:    "some-other-cohort",
		summary:  `Switch snap "some-snap" from channel "latest/stable" to "latest/edge" and from cohort "…me-cohort" to "…er-cohort"`,
	},
	"all change, from empty channel": {
		chanFrom: "",
		chanTo:   "latest/stable",
		cohFrom:  "some-cohort",
		cohTo:    "some-other-cohort",
		summary:  `Switch snap "some-snap" to channel "latest/stable" and from cohort "…me-cohort" to "…er-cohort"`,
	},
	"all change, from empty cohort": {
		chanFrom: "latest/stable",
		chanTo:   "latest/edge",
		cohFrom:  "",
		cohTo:    "some-cohort",
		summary:  `Switch snap "some-snap" from channel "latest/stable" to "latest/edge" and from no cohort to "…me-cohort"`,
	},
	"all change, from empty channel and cohort": {
		chanFrom: "",
		chanTo:   "latest/stable",
		cohFrom:  "",
		cohTo:    "some-cohort",
		summary:  `Switch snap "some-snap" to channel "latest/stable" and from no cohort to "…me-cohort"`,
	},
	"no change": {
		chanFrom: "latest/stable",
		chanTo:   "latest/stable",
		cohFrom:  "some-cohort",
		cohTo:    "some-cohort",
		summary:  `No change switch (no-op)`,
	},
}

func (s *snapmgrTestSuite) TestSwitchScenarios(c *C) {
	for k, t := range switchScenarios {
		s.testSwitchScenario(c, k, t)
	}
}

func (s *snapmgrTestSuite) testSwitchScenario(c *C, desc string, t switchScenario) {
	comment := Commentf("%q (%+v)", desc, t)
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		Channel:  t.chanFrom,
		SnapID:   "foo",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		TrackingChannel: t.chanFrom,
		CohortKey:       t.cohFrom,
	})

	summary := snapstate.SwitchSummary("some-snap", t.chanFrom, t.chanTo, t.cohFrom, t.cohTo)
	c.Check(summary, Equals, t.summary, comment)
	chg := s.state.NewChange("switch-snap", summary)
	ts, err := snapstate.Switch(s.state, "some-snap", &snapstate.RevisionOptions{
		Channel:     t.chanTo,
		CohortKey:   t.cohTo,
		LeaveCohort: t.cohFrom != "" && t.cohTo == "",
	})
	c.Assert(err, IsNil, comment)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// switch is not really really doing anything backend related
	c.Assert(s.fakeBackend.ops, HasLen, 0, comment)

	expectedChanTo := t.chanTo
	if t.chanTo == "" {
		expectedChanTo = t.chanFrom
	}
	expectedCohTo := t.cohTo

	// ensure the desired channel/cohort has changed
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil, comment)
	c.Assert(snapst.TrackingChannel, Equals, expectedChanTo, comment)
	c.Assert(snapst.CohortKey, Equals, expectedCohTo, comment)

	// ensure the current info has not changed
	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil, comment)
	c.Assert(info.Channel, Equals, t.chanFrom, comment)
}

func (s *snapmgrTestSuite) TestParallelInstallSwitchRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		Channel:  "edge",
		SnapID:   "foo",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		TrackingChannel: "latest/edge",
	})

	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		TrackingChannel: "latest/edge",
		InstanceKey:     "instance",
	})

	chg := s.state.NewChange("switch-snap", "switch snap to some-channel")
	ts, err := snapstate.Switch(s.state, "some-snap_instance", &snapstate.RevisionOptions{Channel: "some-channel"})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// switch is not really really doing anything backend related
	c.Assert(s.fakeBackend.ops, HasLen, 0)

	// ensure the desired channel has changed
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.TrackingChannel, Equals, "some-channel/stable")

	// ensure the current info has not changed
	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Channel, Equals, "edge")

	// Ensure that the non-instance snap is unchanged
	var nonInstanceSnapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &nonInstanceSnapst)
	c.Assert(err, IsNil)
	c.Assert(nonInstanceSnapst.TrackingChannel, Equals, "latest/edge")
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

func (s *snapmgrTestSuite) TestAbortCausesNoErrReport(c *C) {
	errReported := 0
	restore := snapstate.MockErrtrackerReport(func(aSnap, aErrMsg, aDupSig string, extra map[string]string) (string, error) {
		errReported++
		return "oops-id", nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	s.fakeBackend.linkSnapWaitCh = make(chan int)
	s.fakeBackend.linkSnapWaitTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/11")
	go func() {
		<-s.fakeBackend.linkSnapWaitCh
		chg.Abort()
		s.fakeBackend.linkSnapWaitCh <- 1
	}()

	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.UndoneStatus)
	c.Assert(errReported, Equals, 0)
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
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)
	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/11")

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// no failure report was generated
}

func (s *snapmgrTestSuite) TestEnsureRefreshesAtSeedPolicy(c *C) {
	// special policy only on classic
	r := release.MockOnClassic(true)
	defer r()
	// set at not seeded yet
	st := s.state
	st.Lock()
	st.Set("seeded", nil)
	st.Unlock()

	s.snapmgr.Ensure()

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

func (s *snapmgrTestSuite) TestEsnureCleansOldSideloads(c *C) {
	filenames := func() []string {
		filenames, _ := filepath.Glob(filepath.Join(dirs.SnapBlobDir, "*"))
		return filenames
	}

	defer snapstate.MockLocalInstallCleanupWait(200 * time.Millisecond)()
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0700), IsNil)
	// sanity check; note * in go glob matches .foo
	c.Assert(filenames(), HasLen, 0)

	s0 := filepath.Join(dirs.SnapBlobDir, "some.snap")
	s1 := filepath.Join(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix+"-12345")
	s2 := filepath.Join(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix+"-67890")

	c.Assert(ioutil.WriteFile(s0, nil, 0600), IsNil)
	c.Assert(ioutil.WriteFile(s1, nil, 0600), IsNil)
	c.Assert(ioutil.WriteFile(s2, nil, 0600), IsNil)

	t1 := time.Now()
	t0 := t1.Add(-time.Hour)

	c.Assert(os.Chtimes(s0, t0, t0), IsNil)
	c.Assert(os.Chtimes(s1, t0, t0), IsNil)
	c.Assert(os.Chtimes(s2, t1, t1), IsNil)

	// all there
	c.Assert(filenames(), DeepEquals, []string{s1, s2, s0})

	// set last cleanup in the future
	defer snapstate.MockLocalInstallLastCleanup(t1.Add(time.Minute))()
	s.snapmgr.Ensure()
	// all there ( -> cleanup not done)
	c.Assert(filenames(), DeepEquals, []string{s1, s2, s0})

	// set last cleanup to epoch
	snapstate.MockLocalInstallLastCleanup(time.Time{})

	s.snapmgr.Ensure()
	// oldest sideload gone
	c.Assert(filenames(), DeepEquals, []string{s2, s0})

	time.Sleep(200 * time.Millisecond)

	s.snapmgr.Ensure()
	// all sideloads gone
	c.Assert(filenames(), DeepEquals, []string{s0})

}

func (s *snapmgrTestSuite) verifyRefreshLast(c *C) {
	var lastRefresh time.Time

	s.state.Get("last-refresh", &lastRefresh)
	c.Check(time.Now().Year(), Equals, lastRefresh.Year())
}

func makeTestRefreshConfig(st *state.State) {
	// avoid special at seed policy
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
	s.se.Ensure()
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
	s.se.Ensure()
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
	s.se.Ensure()
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
	s.se.Ensure()
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
	s.se.Ensure()
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

	checkIsAutoRefresh(c, chg.Tasks(), true)
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

func (s *snapmgrTestSuite) testEnsureRefreshesDisabledViaSnapdControl(c *C, confSet func(*config.Transaction)) {
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
	confSet(tr)
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

func (s *snapmgrTestSuite) TestEnsureRefreshDisableLegacy(c *C) {
	f := func(tr *config.Transaction) {
		tr.Set("core", "refresh.timer", "")
		tr.Set("core", "refresh.schedule", "managed")
	}
	s.testEnsureRefreshesDisabledViaSnapdControl(c, f)
}

func (s *snapmgrTestSuite) TestEnsureRefreshDisableNew(c *C) {
	f := func(tr *config.Transaction) {
		tr.Set("core", "refresh.timer", "managed")
		tr.Set("core", "refresh.schedule", "")
	}
	s.testEnsureRefreshesDisabledViaSnapdControl(c, f)
}

func (s *snapmgrTestSuite) TestEnsureRefreshDisableNewTrumpsOld(c *C) {
	f := func(tr *config.Transaction) {
		tr.Set("core", "refresh.timer", "managed")
		tr.Set("core", "refresh.schedule", "00:00-12:00")
	}
	s.testEnsureRefreshesDisabledViaSnapdControl(c, f)
}

func (s *snapmgrTestSuite) TestDefaultRefreshScheduleParsing(c *C) {
	l, err := timeutil.ParseSchedule(snapstate.DefaultRefreshSchedule)
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
}

func (s *snapmgrTestSuite) TestWaitRestartBasics(c *C) {
	r := release.MockOnClassic(true)
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	// not restarting
	state.MockRestarting(st, state.RestartUnset)
	si := &snap.SideInfo{RealName: "some-app"}
	snaptest.MockSnap(c, "name: some-app\nversion: 1", si)
	snapsup := &snapstate.SnapSetup{SideInfo: si}
	err := snapstate.WaitRestart(task, snapsup)
	c.Check(err, IsNil)

	// restarting ... we always wait
	state.MockRestarting(st, state.RestartDaemon)
	err = snapstate.WaitRestart(task, snapsup)
	c.Check(err, FitsTypeOf, &state.Retry{})
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
	sideInfo11 := &snap.SideInfo{RealName: "name1", Revision: snap.R(11), EditedSummary: "s11", SnapID: "123123123"}
	sideInfo12 := &snap.SideInfo{RealName: "name1", Revision: snap.R(12), EditedSummary: "s12", SnapID: "123123123"}
	instanceSideInfo13 := &snap.SideInfo{RealName: "name1", Revision: snap.R(13), EditedSummary: "s13 instance", SnapID: "123123123"}
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
	snaptest.MockSnapInstance(c, "name1_instance", `
name: name0
version: 1.3
description: |
    Lots of text`, instanceSideInfo13)
	snapstate.Set(st, "name1", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo11, sideInfo12},
		Current:  sideInfo12.Revision,
		SnapType: "app",
	})
	snapstate.Set(st, "name1_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{instanceSideInfo13},
		Current:     instanceSideInfo13.Revision,
		SnapType:    "app",
		InstanceKey: "instance",
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

	c.Check(info.InstanceName(), Equals, "name1")
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

	c.Check(info.InstanceName(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(12))
	c.Check(info.Summary(), Equals, "s12")
	c.Check(info.Version, Equals, "1.2")
	c.Check(info.Description(), Equals, "Lots of text")
	c.Check(info.Media, IsNil)
	c.Check(info.Website, Equals, "")
}

func (s *snapmgrQuerySuite) TestSnapStateCurrentInfoLoadsAuxiliaryStoreInfo(c *C) {
	storeInfo := &snapstate.AuxStoreInfo{
		Media: snap.MediaInfos{{
			Type: "icon",
			URL:  "http://example.com/favicon.ico",
		}},
		Website: "http://example.com/",
	}

	c.Assert(snapstate.KeepAuxStoreInfo("123123123", storeInfo), IsNil)

	st := s.st
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "name1", &snapst)
	c.Assert(err, IsNil)

	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(12))
	c.Check(info.Summary(), Equals, "s12")
	c.Check(info.Version, Equals, "1.2")
	c.Check(info.Description(), Equals, "Lots of text")
	c.Check(info.Media, DeepEquals, storeInfo.Media)
	c.Check(info.Website, Equals, storeInfo.Website)
}

func (s *snapmgrQuerySuite) TestSnapStateCurrentInfoParallelInstall(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "name1_instance", &snapst)
	c.Assert(err, IsNil)

	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "name1_instance")
	c.Check(info.Revision, Equals, snap.R(13))
	c.Check(info.Summary(), Equals, "s13 instance")
	c.Check(info.Version, Equals, "1.3")
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

	c.Check(info.InstanceName(), Equals, "name1")
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

	c.Check(infos, HasLen, 2)

	instanceName := "name1_instance"
	if infos[0].InstanceName() != instanceName && infos[1].InstanceName() != instanceName {
		c.Fail()
	}
	// need stable ordering
	if infos[0].InstanceName() == instanceName {
		infos[1], infos[0] = infos[0], infos[1]
	}

	c.Check(infos[0].InstanceName(), Equals, "name1")
	c.Check(infos[0].Revision, Equals, snap.R(12))
	c.Check(infos[0].Summary(), Equals, "s12")
	c.Check(infos[0].Version, Equals, "1.2")
	c.Check(infos[0].Description(), Equals, "Lots of text")

	c.Check(infos[1].InstanceName(), Equals, "name1_instance")
	c.Check(infos[1].Revision, Equals, snap.R(13))
	c.Check(infos[1].Summary(), Equals, "s13 instance")
	c.Check(infos[1].Version, Equals, "1.3")
	c.Check(infos[1].Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestGadgetInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	deviceCtxNoGadget := deviceWithoutGadgetContext()
	deviceCtx := deviceWithGadgetContext("gadget")

	_, err := snapstate.GadgetInfo(st, deviceCtxNoGadget)
	c.Assert(err, Equals, state.ErrNoState)

	_, err = snapstate.GadgetInfo(st, deviceCtx)
	c.Assert(err, Equals, state.ErrNoState)

	sideInfo := &snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(2),
	}
	snaptest.MockSnap(c, `
name: gadget
type: gadget
version: v1
`, sideInfo)
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  sideInfo.Revision,
	})

	info, err := snapstate.GadgetInfo(st, deviceCtx)
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "gadget")
	c.Check(info.Revision, Equals, snap.R(2))
	c.Check(info.Version, Equals, "v1")
	c.Check(info.Type(), Equals, snap.TypeGadget)
}

func (s *snapmgrQuerySuite) TestKernelInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	deviceCtxNoKernel := &snapstatetest.TrivialDeviceContext{
		DeviceModel: ClassicModel(),
	}
	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: MakeModel(map[string]interface{}{
			"kernel": "pc-kernel",
		}),
	}

	_, err := snapstate.KernelInfo(st, deviceCtxNoKernel)
	c.Assert(err, Equals, state.ErrNoState)

	_, err = snapstate.KernelInfo(st, deviceCtx)
	c.Assert(err, Equals, state.ErrNoState)

	sideInfo := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(3),
	}
	snaptest.MockSnap(c, `
name: pc-kernel
type: kernel
version: v2
`, sideInfo)
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  sideInfo.Revision,
	})

	info, err := snapstate.KernelInfo(st, deviceCtx)
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "pc-kernel")
	c.Check(info.Revision, Equals, snap.R(3))
	c.Check(info.Version, Equals, "v2")
	c.Check(info.Type(), Equals, snap.TypeKernel)
}

func (s *snapmgrQuerySuite) TestBootBaseInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	deviceCtxNoBootBase := &snapstatetest.TrivialDeviceContext{
		DeviceModel: ClassicModel(),
	}
	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: MakeModel20("gadget", map[string]interface{}{
			"base": "core20",
		}),
	}

	// add core18 which is *not* used for booting
	si := &snap.SideInfo{RealName: "core18", Revision: snap.R(1)}
	snaptest.MockSnap(c, `
name: core18
type: base
version: v18
`, si)
	snapstate.Set(st, "core18", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	_, err := snapstate.BootBaseInfo(st, deviceCtxNoBootBase)
	c.Assert(err, Equals, state.ErrNoState)

	// no boot-base in the state so ErrNoState
	_, err = snapstate.BootBaseInfo(st, deviceCtx)
	c.Assert(err, Equals, state.ErrNoState)

	sideInfo := &snap.SideInfo{RealName: "core20", Revision: snap.R(4)}
	snaptest.MockSnap(c, `
name: core20
type: base
version: v20
`, sideInfo)
	snapstate.Set(st, "core20", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  sideInfo.Revision,
	})

	info, err := snapstate.BootBaseInfo(st, deviceCtx)
	c.Assert(err, IsNil)

	c.Check(info.InstanceName(), Equals, "core20")
	c.Check(info.Revision, Equals, snap.R(4))
	c.Check(info.Version, Equals, "v20")
	c.Check(info.Type(), Equals, snap.TypeBase)
}

func (s *snapmgrQuerySuite) TestCoreInfoInternal(c *C) {
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

		info, err := snapstate.CoreInfoInternal(st)
		if t.errMatcher != "" {
			c.Assert(err, ErrorMatches, t.errMatcher)
		} else {
			c.Assert(info, NotNil)
			c.Check(info.InstanceName(), Equals, t.expectedSnap, Commentf("(%d) test %q %v", testNr, t.expectedSnap, t.snapNames))
			c.Check(info.Type(), Equals, snap.TypeOS)
		}
	}
}

func (s *snapmgrQuerySuite) TestHasSnapOfType(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	// an app snap is already setup
	ok, err := snapstate.HasSnapOfType(st, snap.TypeApp)
	c.Assert(err, IsNil)
	c.Check(ok, Equals, true)

	for _, x := range []struct {
		snapName string
		snapType snap.Type
	}{
		{
			snapName: "gadget",
			snapType: snap.TypeGadget,
		},
		{
			snapName: "core",
			snapType: snap.TypeOS,
		},
		{
			snapName: "kernel",
			snapType: snap.TypeKernel,
		},
		{
			snapName: "base",
			snapType: snap.TypeBase,
		},
	} {
		ok, err := snapstate.HasSnapOfType(st, x.snapType)
		c.Assert(err, IsNil)
		c.Check(ok, Equals, false, Commentf("%q", x.snapType))

		sideInfo := &snap.SideInfo{
			RealName: x.snapName,
			Revision: snap.R(2),
		}
		snapstate.Set(st, x.snapName, &snapstate.SnapState{
			SnapType: string(x.snapType),
			Active:   true,
			Sequence: []*snap.SideInfo{sideInfo},
			Current:  sideInfo.Revision,
		})

		ok, err = snapstate.HasSnapOfType(st, x.snapType)
		c.Assert(err, IsNil)
		c.Check(ok, Equals, true)
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
	c.Assert(snapStates, HasLen, 2)

	n, err := snapstate.NumSnaps(st)
	c.Assert(err, IsNil)
	c.Check(n, Equals, 2)

	snapst := snapStates["name1"]
	c.Assert(snapst, NotNil)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.CurrentSideInfo(), NotNil)

	info12, err := snap.ReadInfo("name1", snapst.CurrentSideInfo())
	c.Assert(err, IsNil)

	c.Check(info12.InstanceName(), Equals, "name1")
	c.Check(info12.Revision, Equals, snap.R(12))
	c.Check(info12.Summary(), Equals, "s12")
	c.Check(info12.Version, Equals, "1.2")
	c.Check(info12.Description(), Equals, "Lots of text")

	info11, err := snap.ReadInfo("name1", snapst.Sequence[0])
	c.Assert(err, IsNil)

	c.Check(info11.InstanceName(), Equals, "name1")
	c.Check(info11.Revision, Equals, snap.R(11))
	c.Check(info11.Version, Equals, "1.1")

	instance := snapStates["name1_instance"]
	c.Assert(instance, NotNil)

	c.Check(instance.Active, Equals, true)
	c.Check(instance.CurrentSideInfo(), NotNil)

	info13, err := snap.ReadInfo("name1_instance", instance.CurrentSideInfo())
	c.Assert(err, IsNil)

	c.Check(info13.InstanceName(), Equals, "name1_instance")
	c.Check(info13.SnapName(), Equals, "name1")
	c.Check(info13.Revision, Equals, snap.R(13))
	c.Check(info13.Summary(), Equals, "s13 instance")
	c.Check(info13.Version, Equals, "1.3")
	c.Check(info13.Description(), Equals, "Lots of text")

	info13other, err := snap.ReadInfo("name1_instance", instance.Sequence[0])
	c.Assert(err, IsNil)
	c.Check(info13, DeepEquals, info13other)
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

func (snapStateSuite) TestDefaultContentPlugProviders(c *C) {
	info := &snap.Info{
		Plugs: map[string]*snap.PlugInfo{},
	}

	info.Plugs["foo"] = &snap.PlugInfo{
		Snap:      info,
		Name:      "sound-themes",
		Interface: "content",
		Attrs:     map[string]interface{}{"default-provider": "common-themes", "content": "foo"},
	}
	info.Plugs["bar"] = &snap.PlugInfo{
		Snap:      info,
		Name:      "visual-themes",
		Interface: "content",
		Attrs:     map[string]interface{}{"default-provider": "common-themes", "content": "bar"},
	}
	info.Plugs["baz"] = &snap.PlugInfo{
		Snap:      info,
		Name:      "not-themes",
		Interface: "content",
		Attrs:     map[string]interface{}{"default-provider": "some-snap", "content": "baz"},
	}
	info.Plugs["qux"] = &snap.PlugInfo{Snap: info, Interface: "not-content"}

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	repo := interfaces.NewRepository()
	ifacerepo.Replace(st, repo)

	providers := snapstate.DefaultContentPlugProviders(st, info)
	sort.Strings(providers)
	c.Check(providers, DeepEquals, []string{"common-themes", "some-snap"})
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
	for i, ts := range tts {
		c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
			"stop-snap-services",
			"run-hook[remove]",
			"auto-disconnect",
			"remove-aliases",
			"unlink-snap",
			"remove-profiles",
			"clear-snap",
			"discard-snap",
		})
		verifyStopReason(c, ts, "remove")
		// check that tasksets are in separate lanes
		for _, t := range ts.Tasks() {
			c.Assert(t.Lanes(), DeepEquals, []int{i + 1})
		}

	}
}

func (s *snapmgrTestSuite) testRemoveManyDiskSpaceCheck(c *C, featureFlag, automaticSnapshot, freeSpaceCheckFail bool) error {
	s.state.Lock()
	defer s.state.Unlock()

	var checkFreeSpaceCall, snapshotSizeCall int

	// restored by TearDownTest
	snapstate.EstimateSnapshotSize = func(st *state.State, instanceName string, users []string) (uint64, error) {
		snapshotSizeCall++
		// expect two snapshot size estimations
		switch instanceName {
		case "one":
			return 10, nil
		case "two":
			return 20, nil
		default:
			c.Fatalf("unexpected snap: %s", instanceName)
		}
		return 1, nil
	}

	restore := snapstate.MockOsutilCheckFreeSpace(func(path string, required uint64) error {
		checkFreeSpaceCall++
		// required size is the sum of snapshot sizes of test snaps
		c.Check(required, Equals, snapstate.SafetyMarginDiskSpace(30))
		if freeSpaceCheckFail {
			return &osutil.NotEnoughDiskSpaceError{}
		}
		return nil
	})
	defer restore()

	var automaticSnapshotCalled bool
	snapstate.AutomaticSnapshot = func(st *state.State, instanceName string) (ts *state.TaskSet, err error) {
		automaticSnapshotCalled = true
		if automaticSnapshot {
			t := s.state.NewTask("foo", "")
			ts = state.NewTaskSet(t)
			return ts, nil
		}
		// ErrNothingToDo is returned if automatic snapshots are disabled
		return nil, snapstate.ErrNothingToDo
	}

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-remove", featureFlag)
	tr.Commit()

	snapstate.Set(s.state, "one", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{
			{RealName: "one", SnapID: "one-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})
	snapstate.Set(s.state, "two", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{
			{RealName: "two", SnapID: "two-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})

	_, _, err := snapstate.RemoveMany(s.state, []string{"one", "two"})
	if featureFlag && automaticSnapshot {
		c.Check(snapshotSizeCall, Equals, 2)
		c.Check(checkFreeSpaceCall, Equals, 1)
	} else {
		c.Check(checkFreeSpaceCall, Equals, 0)
		c.Check(snapshotSizeCall, Equals, 0)
	}
	c.Check(automaticSnapshotCalled, Equals, true)

	return err
}

func (s *snapmgrTestSuite) TestRemoveManyDiskSpaceError(c *C) {
	featureFlag := true
	automaticSnapshot := true
	freeSpaceCheckFail := true
	err := s.testRemoveManyDiskSpaceCheck(c, featureFlag, automaticSnapshot, freeSpaceCheckFail)

	diskSpaceErr := err.(*snapstate.InsufficientSpaceError)
	c.Check(diskSpaceErr.Path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd"))
	c.Check(diskSpaceErr.Snaps, DeepEquals, []string{"one", "two"})
	c.Check(diskSpaceErr.ChangeKind, Equals, "remove")
}

func (s *snapmgrTestSuite) TestRemoveManyDiskSpaceCheckDisabled(c *C) {
	featureFlag := false
	automaticSnapshot := true
	freeSpaceCheckFail := true
	err := s.testRemoveManyDiskSpaceCheck(c, featureFlag, automaticSnapshot, freeSpaceCheckFail)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveManyDiskSpaceSnapshotDisabled(c *C) {
	featureFlag := true
	automaticSnapshot := false
	freeSpaceCheckFail := true
	err := s.testRemoveManyDiskSpaceCheck(c, featureFlag, automaticSnapshot, freeSpaceCheckFail)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveManyDiskSpaceCheckPasses(c *C) {
	featureFlag := true
	automaticSnapshot := true
	freeSpaceCheckFail := false
	err := s.testRemoveManyDiskSpaceCheck(c, featureFlag, automaticSnapshot, freeSpaceCheckFail)
	c.Check(err, IsNil)
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
    somesnapidididididididididididid:
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

func deviceWithGadgetContext(gadgetName string) snapstate.DeviceContext {
	return &snapstatetest.TrivialDeviceContext{
		DeviceModel: MakeModel(map[string]interface{}{
			"gadget": gadgetName,
		}),
	}
}

func deviceWithGadgetContext20(gadgetName string) snapstate.DeviceContext {
	return &snapstatetest.TrivialDeviceContext{
		DeviceModel: MakeModel20(gadgetName, nil),
	}
}

func deviceWithoutGadgetContext() snapstate.DeviceContext {
	return &snapstatetest.TrivialDeviceContext{
		DeviceModel: ClassicModel(),
	}
}

func (s *snapmgrTestSuite) TestConfigDefaults(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	s.prepareGadget(c)

	deviceCtx := deviceWithGadgetContext("the-gadget")

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11), SnapID: "somesnapidididididididididididid"},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})
	makeInstalledMockCoreSnap(c)

	defls, err := snapstate.ConfigDefaults(s.state, deviceCtx, "some-snap")
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
	_, err = snapstate.ConfigDefaults(s.state, deviceCtx, "local-snap")
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestConfigDefaultsSmokeUC20(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	// provide a uc20 gadget structure
	s.prepareGadget(c, `
        bootloader: grub
        structure:
        - name: ubuntu-seed
          role: system-seed
          filesystem: vfat
          type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
          size: 1200M
        - name: ubuntu-boot
          role: system-boot
          filesystem: ext4
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          # whats the appropriate size?
          size: 750M
        - name: ubuntu-data
          role: system-data
          filesystem: ext4
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          size: 1G
`)
	// use a UC20 model context
	deviceCtx := deviceWithGadgetContext20("the-gadget")

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11), SnapID: "somesnapidididididididididididid"},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})
	makeInstalledMockCoreSnap(c)

	defls, err := snapstate.ConfigDefaults(s.state, deviceCtx, "some-snap")
	c.Assert(err, IsNil)
	c.Assert(defls, DeepEquals, map[string]interface{}{"key": "value"})
}

func (s *snapmgrTestSuite) TestConfigDefaultsNoGadget(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	s.state.Lock()
	defer s.state.Unlock()

	deviceCtxNoGadget := deviceWithoutGadgetContext()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11), SnapID: "somesnapidididididididididididid"},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})
	makeInstalledMockCoreSnap(c)

	_, err := snapstate.ConfigDefaults(s.state, deviceCtxNoGadget, "some-snap")
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestConfigDefaultsSystemWithCore(c *C) {
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

	deviceCtx := deviceWithGadgetContext("the-gadget")

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11), SnapID: "the-core-ididididididididididid"},
		},
		Current:  snap.R(11),
		SnapType: "os",
	})

	makeInstalledMockCoreSnap(c)

	defls, err := snapstate.ConfigDefaults(s.state, deviceCtx, "core")
	c.Assert(err, IsNil)
	c.Assert(defls, DeepEquals, map[string]interface{}{"foo": "bar"})
}

var snapdSnapYaml = `name: snapd
version: 1.0
type: snapd
`

func (s *snapmgrTestSuite) TestConfigDefaultsSystemWithSnapdNoCore(c *C) {
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

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: MakeModel(map[string]interface{}{
			"gadget": "the-gadget",
			"base":   "the-base",
		}),
	}

	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "snapd", SnapID: "the-snapd-snapidididididididididi", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	snaptest.MockSnap(c, snapdSnapYaml, &snap.SideInfo{
		RealName: "snapd",
		Revision: snap.R(1),
	})

	defls, err := snapstate.ConfigDefaults(s.state, deviceCtx, "core")
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
    thecoresnapididididididididididi:
        foo: other-bar
        other-key: other-key-default
`)

	deviceCtx := deviceWithGadgetContext("the-gadget")

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", SnapID: "thecoresnapididididididididididi", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})

	makeInstalledMockCoreSnap(c)

	// 'system' key defaults take precedence over snap-id ones
	defls, err := snapstate.ConfigDefaults(s.state, deviceCtx, "core")
	c.Assert(err, IsNil)
	c.Assert(defls, DeepEquals, map[string]interface{}{"foo": "bar"})
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
	verifyCoreRemoveTasks(c, tsl[2])
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
	verifyCoreRemoveTasks(c, tsl[1])
}

func (s *snapmgrTestSuite) TestTransitionCoreRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:         snap.R(1),
		SnapType:        "os",
		TrackingChannel: "latest/beta",
	})

	chg := s.state.NewChange("transition-ubuntu-core", "...")
	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)
	for _, ts := range tsl {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		name: "core",
		// the transition has no user associcated with it
		macaroon: "",
		target:   filepath.Join(dirs.SnapBlobDir, "core_11.snap"),
	}})
	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{
				{
					InstanceName:    "ubuntu-core",
					SnapID:          "ubuntu-core-snap-id",
					Revision:        snap.R(1),
					TrackingChannel: "latest/beta",
					RefreshedDate:   fakeRevDateEpoch.AddDate(0, 0, 1),
					Epoch:           snap.E("1*"),
				},
			},
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "core",
				Channel:      "latest/beta",
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
			path: filepath.Join(dirs.SnapBlobDir, "core_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "core",
				SnapID:   "core-id",
				Channel:  "latest/beta",
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
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "core",
				SnapID:   "core-id",
				Channel:  "latest/beta",
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
		{
			op:   "transition-ubuntu-core:Doing",
			name: "ubuntu-core",
		},
		{
			op:    "auto-disconnect:Doing",
			name:  "ubuntu-core",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-aliases",
			name: "ubuntu-core",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "ubuntu-core",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:   "remove-snap-data-dir",
			name: "ubuntu-core",
			path: filepath.Join(dirs.SnapDataDir, "ubuntu-core"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
			stype: "os",
		},
		{
			op:   "discard-namespace",
			name: "ubuntu-core",
		},
		{
			op:   "remove-snap-dir",
			name: "ubuntu-core",
			path: filepath.Join(dirs.SnapMountDir, "ubuntu-core"),
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
		Active:          true,
		Sequence:        []*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}},
		Current:         snap.R(1),
		SnapType:        "os",
		TrackingChannel: "latest/stable",
	})
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:         snap.R(1),
		SnapType:        "os",
		TrackingChannel: "latest/stable",
	})

	chg := s.state.NewChange("transition-ubuntu-core", "...")
	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)
	for _, ts := range tsl {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	defer s.se.Stop()
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
			op:    "auto-disconnect:Doing",
			name:  "ubuntu-core",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-aliases",
			name: "ubuntu-core",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "ubuntu-core",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
		},
		{
			op:   "remove-snap-data-dir",
			name: "ubuntu-core",
			path: filepath.Join(dirs.SnapDataDir, "ubuntu-core"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "ubuntu-core/1"),
			stype: "os",
		},
		{
			op:   "discard-namespace",
			name: "ubuntu-core",
		},
		{
			op:   "remove-snap-dir",
			name: "ubuntu-core",
			path: filepath.Join(dirs.SnapMountDir, "ubuntu-core"),
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
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(s.state.Changes()[0].Kind(), Equals, "transition-ubuntu-core")
}

func (s *snapmgrTestSuite) TestTransitionCoreTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(nil)
	defer r()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1)}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 0)
	// not counted as a try
	var t time.Time
	err := s.state.Get("ubuntu-core-transition-last-retry-time", &t)
	c.Assert(err, Equals, state.ErrNoState)
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
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 0)

	// tried 7h ago, retry
	s.state.Set("ubuntu-core-transition-last-retry-time", time.Now().Add(-7*time.Hour))

	s.state.Unlock()
	defer s.se.Stop()
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
	defer s.se.Stop()
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
	opts := &snapstate.RevisionOptions{Channel: "stable"}
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Check(err, ErrorMatches, "ubuntu-core to core transition in progress, no other changes allowed until this is done")

	// and when the transition is done, other tasks run
	chg.SetStatus(state.DoneStatus)
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Check(err, IsNil)
	c.Check(ts, NotNil)
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapDoesNotRunWithoutSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	// no snaps installed on this system (e.g. fresh classic)
	snapstate.Set(s.state, "core", nil)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 0)
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapDoesRunWithAnySnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	// some snap installed on this system but no core
	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "foo", SnapID: "foo-id", Revision: snap.R(1), Channel: "beta"}},
		Current:  snap.R(1),
	})

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 1)
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapDoesNotRunWhenNotEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1), Channel: "beta"}},
		Current:  snap.R(1),
		SnapType: "os",
	})

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 0)
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapStartsAutomaticallyWhenEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1), Channel: "beta"}},
		Current:  snap.R(1),
		SnapType: "os",
	})
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Check(chg.Kind(), Equals, "transition-to-snapd-snap")
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// snapd snap is instaleld from the default channel
	var snapst snapstate.SnapState
	snapstate.Get(s.state, "snapd", &snapst)
	c.Assert(snapst.TrackingChannel, Equals, "latest/stable")
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapWithCoreRunthrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "corecore", SnapID: "core-snap-id", Revision: snap.R(1), Channel: "edge"}},
		Current:  snap.R(1),
		SnapType: "os",
		// TrackingChannel
		TrackingChannel: "latest/beta",
	})
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Assert(chg.Kind(), Equals, "transition-to-snapd-snap")
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, HasLen, 1)
	ts := state.NewTaskSet(chg.Tasks()...)
	verifyInstallTasks(c, noConfigure, 0, ts, s.state)

	// ensure preferences from the core snap got transferred over
	var snapst snapstate.SnapState
	snapstate.Get(s.state, "snapd", &snapst)
	c.Assert(snapst.TrackingChannel, Equals, "latest/beta")
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapTimeLimitWorks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	// tried 3h ago, no retry
	s.state.Set("snapd-transition-last-retry-time", time.Now().Add(-3*time.Hour))

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.state.Changes(), HasLen, 0)

	// tried 7h ago, retry
	s.state.Set("snapd-transition-last-retry-time", time.Now().Add(-7*time.Hour))

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 1)

	var t time.Time
	s.state.Get("snapd-transition-last-retry-time", &t)
	c.Assert(time.Now().Sub(t) < 2*time.Minute, Equals, true)
}

type unhappyStore struct {
	*fakeStore
}

func (s unhappyStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if assertQuery != nil {
		panic("no assertion query support")
	}

	return nil, nil, fmt.Errorf("a grumpy store")
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, unhappyStore{fakeStore: s.fakeStore})

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	s.state.Unlock()
	defer s.se.Stop()
	err := s.o.Settle(5 * time.Second)
	c.Assert(err, ErrorMatches, `state ensure errors: \[a grumpy store\]`)

	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 0)

	// all the attempts were recorded
	var t time.Time
	s.state.Get("snapd-transition-last-retry-time", &t)
	c.Assert(time.Now().Sub(t) < 2*time.Minute, Equals, true)

	var cnt int
	s.state.Get("snapd-transition-retry", &cnt)
	c.Assert(cnt, Equals, 1)

	// the transition is not tried again (because of retry time)
	s.state.Unlock()
	err = s.o.Settle(5 * time.Second)
	c.Assert(err, IsNil)
	s.state.Lock()

	s.state.Get("snapd-transition-retry", &cnt)
	c.Assert(cnt, Equals, 1)
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapBlocksOtherChanges(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// if we have a snapd transition
	chg := s.state.NewChange("transition-to-snapd-snap", "...")
	chg.SetStatus(state.DoStatus)

	// other tasks block until the transition is done
	_, err := snapstate.Install(context.Background(), s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Check(err, ErrorMatches, "transition to snapd snap in progress, no other changes allowed until this is done")

	// and when the transition is done, other tasks run
	chg.SetStatus(state.DoneStatus)
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
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
	r := sandbox.MockForceDevMode(true)
	defer r()
	c.Assert(sandbox.ForceDevMode(), Equals, true)

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
	defer s.se.Stop()
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
	r := sandbox.MockForceDevMode(true)
	defer r()
	c.Assert(sandbox.ForceDevMode(), Equals, true)

	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	var n int
	s.state.Get("fix-forced-devmode", &n)
	c.Check(n, Equals, 1)
}

func (s *snapmgrTestSuite) TestForceDevModeCleanupSkipsNonForcedOS(c *C) {
	r := sandbox.MockForceDevMode(false)
	defer r()
	c.Assert(sandbox.ForceDevMode(), Equals, false)

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
	defer s.se.Stop()
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
		switch info.InstanceName() {
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
				{Name: "alias1", Target: "alias-snap.cmd1"},
				{Name: "alias2", Target: "alias-snap.cmd2"},
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
		switch info.InstanceName() {
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

	for _, instanceName := range []string{"a-snap", "b-snap"} {
		snapstate.Set(s.state, instanceName, &snapstate.SnapState{
			Sequence: []*snap.SideInfo{
				{RealName: instanceName, Revision: snap.R(11)},
			},
			Current: snap.R(11),
			Active:  false,
		})

		ts, err := snapstate.Enable(s.state, instanceName)
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
		c.Check(snapstate.CheckChangeConflictMany(s.state, m, ""), IsNil)
	}

	// things that should not be ok:
	for _, m := range [][]string{
		{"a-snap"},
		{"a-snap", "b-snap"},
		{"a-snap", "c-snap"},
		{"b-snap", "c-snap"},
	} {
		err := snapstate.CheckChangeConflictMany(s.state, m, "")
		c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
		c.Check(err, ErrorMatches, `snap "[^"]*" has "enable" change in progress`)
	}
}

func (s *snapmgrTestSuite) TestConflictManyRemodeling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("remodel", "...")
	chg.SetStatus(state.DoingStatus)

	err := snapstate.CheckChangeConflictMany(s.state, []string{"a-snap"}, "")
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Check(err, ErrorMatches, `remodeling in progress, no other changes allowed until this is done`)
}

type contentStore struct {
	*fakeStore
	state *state.State
}

func (s contentStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	sars, _, err := s.fakeStore.SnapAction(ctx, currentSnaps, actions, assertQuery, user, opts)
	if err != nil {
		return nil, nil, err
	}
	if len(sars) != 1 {
		panic("expected to be queried for install of only one snap at a time")
	}
	info := sars[0].Info
	switch info.InstanceName() {
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

	return []store.SnapActionResult{{Info: info}}, nil, err
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

func (s *snapmgrTestSuite) TestParallelInstallValidateFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	info := &snap.Info{
		InstanceKey: "foo",
	}

	err := snapstate.ValidateFeatureFlags(s.state, info)
	c.Assert(err, ErrorMatches, `experimental feature disabled - test it by setting 'experimental.parallel-instances' to true`)

	// various forms of disabling
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", false)
	tr.Commit()

	err = snapstate.ValidateFeatureFlags(s.state, info)
	c.Assert(err, ErrorMatches, `experimental feature disabled - test it by setting 'experimental.parallel-instances' to true`)

	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", "")
	tr.Commit()

	err = snapstate.ValidateFeatureFlags(s.state, info)
	c.Assert(err, ErrorMatches, `experimental feature disabled - test it by setting 'experimental.parallel-instances' to true`)

	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", nil)
	tr.Commit()

	err = snapstate.ValidateFeatureFlags(s.state, info)
	c.Assert(err, ErrorMatches, `experimental feature disabled - test it by setting 'experimental.parallel-instances' to true`)

	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", "veryfalse")
	tr.Commit()

	err = snapstate.ValidateFeatureFlags(s.state, info)
	c.Assert(err, ErrorMatches, `parallel-instances can only be set to 'true' or 'false', got "veryfalse"`)

	// enable parallel instances
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	err = snapstate.ValidateFeatureFlags(s.state, info)
	c.Assert(err, IsNil)
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
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, true)

	// but bases do not for install
	ts, err = snapstate.Install(context.Background(), s.state, "some-base", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, false)

	// or for refresh
	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)}},
		Current:         snap.R(1),
		SnapType:        "base",
	})
	ts, err = snapstate.Update(s.state, "some-base", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, false)
}

func (s *snapmgrTestSuite) TestSnapdSnapOnCoreWithoutBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	r := release.MockOnClassic(false)
	defer r()

	// it is now possible to install snapd snap on a system with core
	_, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestSnapdSnapOnSystemsWithoutBaseOnUbuntuCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	r := release.MockOnClassic(false)
	defer r()

	// it is not possible to opt-into the snapd snap on core yet
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	// it is now possible to install snapd snap on a system with core, experimental option has no effect
	_, err := snapstate.Install(context.Background(), s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestNoSnapdSnapOnSystemsWithoutBaseButOption(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	_, err := snapstate.Install(context.Background(), s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestNoConfigureForSnapdSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// snapd cannot be installed unless the model uses a base snap
	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	// but snapd do not for install
	ts, err := snapstate.Install(context.Background(), s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, false)

	// or for refresh
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}},
		Current:         snap.R(1),
		SnapType:        "app",
	})
	ts, err = snapstate.Update(s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(hasConfigureTask(ts), Equals, false)

}

func (s *snapmgrTestSuite) TestCanLoadOldSnapSetupWithoutType(c *C) {
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

func (s *snapmgrTestSuite) TestHasOtherInstances(c *C) {
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
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
		},
		Current:     snap.R(3),
		SnapType:    "app",
		InstanceKey: "instance",
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	other, err := snapstate.HasOtherInstances(s.state, "some-snap")
	c.Assert(err, IsNil)
	c.Assert(other, Equals, true)
	other, err = snapstate.HasOtherInstances(s.state, "some-snap_instance")
	c.Assert(err, IsNil)
	c.Assert(other, Equals, true)
	other, err = snapstate.HasOtherInstances(s.state, "some-other-snap")
	c.Assert(err, IsNil)
	c.Assert(other, Equals, false)
	// other snaps like only looks at the name of the refence snap
	other, err = snapstate.HasOtherInstances(s.state, "some-other-snap_instance")
	c.Assert(err, IsNil)
	c.Assert(other, Equals, true)

	// remove the snap without instance key
	snapstate.Set(s.state, "some-snap", nil)
	// some-snap_instance is like some-snap
	other, err = snapstate.HasOtherInstances(s.state, "some-snap")
	c.Assert(err, IsNil)
	c.Assert(other, Equals, true)
	other, err = snapstate.HasOtherInstances(s.state, "some-snap_instance")
	c.Assert(err, IsNil)
	c.Assert(other, Equals, false)

	// add another snap with instance key
	snapstate.Set(s.state, "some-snap_other", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
		},
		Current:     snap.R(3),
		SnapType:    "app",
		InstanceKey: "other",
	})
	other, err = snapstate.HasOtherInstances(s.state, "some-snap")
	c.Assert(err, IsNil)
	c.Assert(other, Equals, true)
	other, err = snapstate.HasOtherInstances(s.state, "some-snap_instance")
	c.Assert(err, IsNil)
	c.Assert(other, Equals, true)
}

func (s *snapmgrTestSuite) TestRequestSalt(c *C) {
	si := snap.SideInfo{
		RealName: "other-snap",
		Revision: snap.R(7),
		SnapID:   "other-snap-id",
	}
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})
	snapstate.Set(s.state, "other-snap_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{&si},
		Current:     si.Revision,
		SnapType:    "app",
		InstanceKey: "instance",
	})

	// clear request-salt to have it generated
	s.state.Set("refresh-privacy-key", nil)

	_, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "internal error: request salt is unset")

	s.state.Set("refresh-privacy-key", "privacy-key")

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(len(s.fakeBackend.ops) >= 1, Equals, true)
	storeAction := s.fakeBackend.ops[0]
	c.Assert(storeAction.op, Equals, "storesvc-snap-action")
	c.Assert(storeAction.curSnaps, HasLen, 2)
	c.Assert(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true)
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
		info := &snap.Info{SnapType: tt.typ}
		c.Check(snapstate.CanDisable(info), Equals, tt.canDisable)
	}
}

func (s *snapmgrTestSuite) TestGadgetConnections(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	deviceCtxNoGadget := deviceWithoutGadgetContext()
	deviceCtx := deviceWithGadgetContext("the-gadget")

	s.state.Lock()
	defer s.state.Unlock()

	_, err := snapstate.GadgetConnections(s.state, deviceCtxNoGadget)
	c.Assert(err, Equals, state.ErrNoState)

	_, err = snapstate.GadgetConnections(s.state, deviceCtx)
	c.Assert(err, Equals, state.ErrNoState)

	s.prepareGadget(c, `
connections:
  - plug: snap1idididididididididididididi:plug
    slot: snap2idididididididididididididi:slot
`)

	conns, err := snapstate.GadgetConnections(s.state, deviceCtx)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []gadget.Connection{
		{Plug: gadget.ConnectionPlug{SnapID: "snap1idididididididididididididi", Plug: "plug"}, Slot: gadget.ConnectionSlot{SnapID: "snap2idididididididididididididi", Slot: "slot"}}})
}

func (s *snapmgrTestSuite) TestGadgetConnectionsUC20(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	// use a UC20 model context
	deviceCtx := deviceWithGadgetContext20("the-gadget")

	s.state.Lock()
	defer s.state.Unlock()

	// provide a uc20 gadget structure
	s.prepareGadget(c, `
        bootloader: grub
        structure:
        - name: ubuntu-seed
          role: system-seed
          filesystem: vfat
          type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
          size: 1200M
        - name: ubuntu-boot
          role: system-boot
          filesystem: ext4
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          # whats the appropriate size?
          size: 750M
        - name: ubuntu-data
          role: system-data
          filesystem: ext4
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          size: 1G
connections:
  - plug: snap1idididididididididididididi:plug
    slot: snap2idididididididididididididi:slot
`)

	conns, err := snapstate.GadgetConnections(s.state, deviceCtx)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, []gadget.Connection{
		{Plug: gadget.ConnectionPlug{SnapID: "snap1idididididididididididididi", Plug: "plug"}, Slot: gadget.ConnectionSlot{SnapID: "snap2idididididididididididididi", Slot: "slot"}}})
}

func (s *snapmgrTestSuite) TestSnapManagerCanStandby(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no snaps -> can standby
	s.state.Set("snaps", nil)
	c.Assert(s.snapmgr.CanStandby(), Equals, true)

	// snaps installed -> can *not* standby
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})
	c.Assert(s.snapmgr.CanStandby(), Equals, false)
}

func (s *snapmgrTestSuite) TestResolveChannelPinnedTrack(c *C) {
	type test struct {
		snap        string
		cur         string
		new         string
		exp         string
		kernelTrack string
		gadgetTrack string
		err         string
	}

	for i, tc := range []test{
		// neither kernel nor gadget
		{snap: "some-snap"},
		{snap: "some-snap", new: "stable", exp: "stable"},
		{snap: "some-snap", new: "foo/stable", exp: "foo/stable"},
		{snap: "some-snap", new: "stable/with-branch", exp: "stable/with-branch"},
		{snap: "some-snap", new: "supertrack/stable", exp: "supertrack/stable"},
		{snap: "some-snap", new: "supertrack/stable/with-branch", exp: "supertrack/stable/with-branch"},
		// kernel or gadget snap set, but unrelated snap
		{snap: "some-snap", new: "stable", exp: "stable", kernelTrack: "18"},
		{snap: "some-snap", new: "foo/stable", exp: "foo/stable", kernelTrack: "18"},
		{snap: "some-snap", new: "foo/stable", exp: "foo/stable", gadgetTrack: "18"},
		// no pinned track
		{snap: "kernel", new: "latest/stable", exp: "latest/stable"},
		{snap: "kernel", new: "stable", exp: "stable"},
		{snap: "brand-gadget", new: "stable", exp: "stable"},
		// not a risk only request
		{snap: "kernel", new: "", kernelTrack: "18"},
		{snap: "brand-gadget", new: "", gadgetTrack: "18"},
		{snap: "kernel", new: "latest/stable", kernelTrack: "18", err: "cannot switch from kernel track.*"},
		{snap: "kernel", new: "latest/stable/hotfix-123", kernelTrack: "18", err: "cannot switch from kernel track.*"},
		{snap: "kernel", new: "foo/stable", kernelTrack: "18", err: "cannot switch from kernel track.*"},
		{snap: "brand-gadget", new: "foo/stable", exp: "18/stable", gadgetTrack: "18", err: "cannot switch from gadget track.*"},
		{snap: "kernel", new: "18/stable", exp: "18/stable", kernelTrack: "18"},
		{snap: "kernel", new: "18/stable", exp: "18/stable"},
		{snap: "brand-gadget", new: "18/stable", exp: "18/stable", gadgetTrack: "18"},
		{snap: "brand-gadget", new: "18/stable", exp: "18/stable"},
		// risk/branch within a track
		{snap: "kernel", new: "stable/hotfix-123", exp: "18/stable/hotfix-123", kernelTrack: "18"},
		{snap: "kernel", new: "18/stable/hotfix-123", exp: "18/stable/hotfix-123", kernelTrack: "18"},
		// risk only defaults to pinned gadget track
		{snap: "brand-gadget", new: "stable", exp: "17/stable", gadgetTrack: "17"},
		{snap: "brand-gadget", new: "edge", exp: "17/edge", gadgetTrack: "17"},
		// risk only defaults to pinned kernel track
		{snap: "kernel", new: "stable", exp: "17/stable", kernelTrack: "17"},
		{snap: "kernel", new: "edge", exp: "17/edge", kernelTrack: "17"},
		// risk only defaults to current track
		{snap: "some-snap", new: "stable", cur: "stable", exp: "stable"},
		{snap: "some-snap", new: "stable", cur: "latest/stable", exp: "latest/stable"},
		{snap: "some-snap", new: "stable", cur: "sometrack/edge", exp: "sometrack/stable"},
	} {
		if tc.kernelTrack != "" && tc.gadgetTrack != "" {
			c.Fatalf("%d: setting both kernel and gadget tracks is not supported by the test", i)
		}
		var model *asserts.Model
		switch {
		case tc.kernelTrack != "":
			model = ModelWithKernelTrack(tc.kernelTrack)
		case tc.gadgetTrack != "":
			model = ModelWithGadgetTrack(tc.gadgetTrack)
		default:
			model = DefaultModel()
		}
		deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}
		s.state.Lock()
		ch, err := snapstate.ResolveChannel(s.state, tc.snap, tc.cur, tc.new, deviceCtx)
		s.state.Unlock()
		comment := Commentf("tc %d: %#v", i, tc)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err, comment)
		} else {
			c.Check(err, IsNil, comment)
			c.Check(ch, Equals, tc.exp, comment)
		}
	}
}

func (s *snapmgrTestSuite) TestGadgetUpdateTaskAddedOnInstall(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// task added on install
	ts, err := snapstate.Install(context.Background(), s.state, "brand-gadget", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	verifyInstallTasks(c, updatesGadget, 0, ts, s.state)
}

func (s *snapmgrTestSuite) TestGadgetUpdateTaskAddedOnRefresh(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "brand-gadget", SnapID: "brand-gadget-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "gadget",
	})

	// and on update
	ts, err := snapstate.Update(s.state, "brand-gadget", &snapstate.RevisionOptions{}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh|updatesGadget, 0, ts, s.state)

}

func (s *snapmgrTestSuite) TestForSnapSetupResetsFlags(c *C) {
	flags := snapstate.Flags{
		DevMode:          true,
		JailMode:         true,
		Classic:          true,
		TryMode:          true,
		Revert:           true,
		RemoveSnapPath:   true,
		IgnoreValidation: true,
		Required:         true,
		SkipConfigure:    true,
		Unaliased:        true,
		Amend:            true,
		IsAutoRefresh:    true,
		NoReRefresh:      true,
		RequireTypeBase:  true,
	}
	flags = flags.ForSnapSetup()

	// certain flags get reset, others are not touched
	c.Check(flags, DeepEquals, snapstate.Flags{
		DevMode:          true,
		JailMode:         true,
		Classic:          true,
		TryMode:          true,
		Revert:           true,
		RemoveSnapPath:   true,
		IgnoreValidation: true,
		Required:         true,
		SkipConfigure:    false,
		Unaliased:        true,
		Amend:            true,
		IsAutoRefresh:    true,
		NoReRefresh:      false,
		RequireTypeBase:  false,
	})
}
