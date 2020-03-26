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
	"bytes"
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
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
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

	oldAutomaticSnapshot := snapstate.AutomaticSnapshot
	snapstate.AutomaticSnapshot = func(st *state.State, instanceName string) (ts *state.TaskSet, err error) {
		task := st.NewTask("save-snapshot", "...")
		ts = state.NewTaskSet(task)
		return ts, nil
	}

	oldAutomaticSnapshotExpiration := snapstate.AutomaticSnapshotExpiration
	snapstate.AutomaticSnapshotExpiration = func(st *state.State) (time.Duration, error) { return 1, nil }
	s.BaseTest.AddCleanup(func() {
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
	if opts&updatesGadget != 0 {
		expected = append(expected, "update-gadget-assets")
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
		"run-hook[check-health]",
	)
	if opts&doesReRefresh != 0 {
		expected = append(expected, "check-rerefresh")
	}

	c.Assert(kinds, DeepEquals, expected)
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

func (s *snapmgrTestSuite) TestUpdateCreatesGCTasks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.testUpdateCreatesGCTasks(c, 2)
}

func (s *snapmgrTestSuite) TestUpdateCreatesGCTasksOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.testUpdateCreatesGCTasks(c, 3)
}

func (s *snapmgrTestSuite) testUpdateCreatesGCTasks(c *C, expectedDiscards int) {
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

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	// ensure edges information is still there
	te, err := ts.Edge(snapstate.DownloadAndChecksDoneEdge)
	c.Assert(te, NotNil)
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, expectedDiscards, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s snapmgrTestSuite) TestInstallFailsOnDisabledSnap(c *C) {
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

func (s snapmgrTestSuite) TestInstallFailsOnBusySnap(c *C) {
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
	mockPidsCgroupDir := c.MkDir()
	restore := snapstate.MockPidsCgroupDir(mockPidsCgroupDir)
	defer restore()

	// And with cgroup v1 information indicating the app has a process with pid 1234.
	writePids(c, filepath.Join(mockPidsCgroupDir, "snap.some-snap.app"), []int{1234})

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

func (s snapmgrTestSuite) TestInstallDespiteBusySnap(c *C) {
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
	// And with cgroup v1 information indicating the app has a process with pid 1234.
	writePids(c, filepath.Join(dirs.PidsCgroupDir, "snap.some-snap.app"), []int{1234})

	// Attempt to install revision 2 of the snap.
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
	}

	// And observe that refresh occurred regardless of the running process.
	_, err := snapstate.DoInstall(s.state, snapst, snapsup, 0, "", dummyInUseCheck)
	c.Assert(err, IsNil)
}

func (s snapmgrTestSuite) TestInstallFailsOnSystem(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapsup := &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "system", SnapID: "some-snap-id", Revision: snap.R(1)}}
	_, err := snapstate.DoInstall(s.state, nil, snapsup, 0, "", nil)
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

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 3, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateManyTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", nil)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	_, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `too early for operation, device not yet seeded or device model not acknowledged`)
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

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, DeepEquals, []string{"some-snap"})

	ts := tts[0]
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 3, ts, s.state)

	// check that the tasks are in non-default lane
	for _, t := range ts.Tasks() {
		c.Assert(t.Lanes(), DeepEquals, []int{1})
	}
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks())+1) // 1==rerefresh

	// ensure edges information is still there
	te, err := ts.Edge(snapstate.DownloadAndChecksDoneEdge)
	c.Assert(te, NotNil)
	c.Assert(err, IsNil)

	checkIsAutoRefresh(c, ts.Tasks(), false)
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateMany(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

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
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
		},
		Current:     snap.R(3),
		SnapType:    "app",
		InstanceKey: "instance",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 3)
	verifyLastTasksetIsReRefresh(c, tts)
	// ensure stable ordering of updates list
	if updates[0] != "some-snap" {
		updates[1], updates[0] = updates[0], updates[1]
	}

	c.Check(updates, DeepEquals, []string{"some-snap", "some-snap_instance"})

	var snapsup, snapsupInstance *snapstate.SnapSetup

	// ensure stable ordering of task sets list
	snapsup, err = snapstate.TaskSnapSetup(tts[0].Tasks()[0])
	c.Assert(err, IsNil)
	if snapsup.InstanceName() != "some-snap" {
		tts[0], tts[1] = tts[1], tts[0]
		snapsup, err = snapstate.TaskSnapSetup(tts[0].Tasks()[0])
		c.Assert(err, IsNil)
	}
	snapsupInstance, err = snapstate.TaskSnapSetup(tts[1].Tasks()[0])
	c.Assert(err, IsNil)

	c.Assert(snapsup.InstanceName(), Equals, "some-snap")
	c.Assert(snapsupInstance.InstanceName(), Equals, "some-snap_instance")

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 3, tts[0], s.state)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 1, tts[1], s.state)
}

func (s *snapmgrTestSuite) TestUpdateManyDevModeConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-devmode/stable",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	// updated snap is devmode, updatemany doesn't update it
	_, tts, _ := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
	// FIXME: UpdateMany will not error out in this case (daemon catches this case, with a weird error)
	c.Assert(tts, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyClassicConfinementFiltering(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-classic/stable",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	// if a snap installed without --classic gets a classic update it isn't installed
	_, tts, _ := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
	// FIXME: UpdateMany will not error out in this case (daemon catches this case, with a weird error)
	c.Assert(tts, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-classic/stable",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
		Flags:           snapstate.Flags{Classic: true},
	})

	// snap installed with classic: refresh gets classic
	_, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)
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

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, 0, nil)
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

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyWaitForBasesUC16(c *C) {
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
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "channel-for-base/stable",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "core", "some-base"}, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 4)
	verifyLastTasksetIsReRefresh(c, tts)
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
					prereqs[snapsup.InstanceName()] = true
				}
			}
		}
	}

	c.Check(prereqs, DeepEquals, map[string]bool{
		"core":      true,
		"some-base": true,
	})
}

func (s *snapmgrTestSuite) TestUpdateManyWaitForBasesUC18(c *C) {
	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core18", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core18", SnapID: "core18-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)},
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

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "channel-for-base/stable",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "core18", "some-base", "snapd"}, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 5)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, HasLen, 4)

	// to make TaskSnapSetup work
	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	// Note that some-app only waits for snapd+some-base. The core18
	// base is not special to this snap and not waited for
	prereqTotal := len(tts[0].Tasks()) + len(tts[1].Tasks())
	prereqs := map[string]bool{}
	for i, task := range tts[3].Tasks() {
		waitTasks := task.WaitTasks()
		if i == 0 {
			c.Check(len(waitTasks), Equals, prereqTotal)
		} else if task.Kind() == "link-snap" {
			c.Check(len(waitTasks), Equals, prereqTotal+1)
			for _, pre := range waitTasks {
				if pre.Kind() == "link-snap" {
					snapsup, err := snapstate.TaskSnapSetup(pre)
					c.Assert(err, IsNil)
					prereqs[snapsup.InstanceName()] = true
				}
			}
		}
	}

	// Note that "core18" is not part of the prereqs for some-app
	// as it does not use this base.
	c.Check(prereqs, DeepEquals, map[string]bool{
		"some-base": true,
		"snapd":     true,
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
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		validateCalled = true
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].InstanceName(), Equals, "some-snap")
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, DeepEquals, []string{"some-snap"})
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 0, tts[0], s.state)

	c.Check(validateCalled, Equals, true)
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateManyValidateRefreshes(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

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
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:     snap.R(1),
		SnapType:    "app",
		InstanceKey: "instance",
	})

	validateCalled := false
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		validateCalled = true
		c.Check(refreshes, HasLen, 2)
		instanceIdx := 0
		someIdx := 1
		if refreshes[0].InstanceName() != "some-snap_instance" {
			instanceIdx = 1
			someIdx = 0
		}
		c.Check(refreshes[someIdx].InstanceName(), Equals, "some-snap")
		c.Check(refreshes[instanceIdx].InstanceName(), Equals, "some-snap_instance")
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(refreshes[1].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[1].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 3)
	verifyLastTasksetIsReRefresh(c, tts)
	sort.Strings(updates)
	c.Check(updates, DeepEquals, []string{"some-snap", "some-snap_instance"})
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 0, tts[0], s.state)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 0, tts[1], s.state)

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
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	// refresh all => no error
	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 0)
	c.Check(updates, HasLen, 0)

	// refresh some-snap => report error
	updates, tts, err = snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, 0, nil)
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
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap-with-default-track", opts, s.user.ID, snapstate.Flags{Classic: true})
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

func (s sneakyStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, error) {
	s.state.Lock()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}},
		Current:         snap.R(1),
		SnapType:        "app",
	})
	s.state.Unlock()
	return s.fakeStore.SnapAction(ctx, currentSnaps, actions, user, opts)
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

func (s *snapmgrTestSuite) TestUpdateTasksPropagatesErrors(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "fakestore-please-error-on-refresh", Revision: snap.R(7)}},
		Current:         snap.R(7),
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `failing as requested`)
}

func (s *snapmgrTestSuite) TestUpdateTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	validateCalled := false
	happyValidateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		validateCalled = true
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = happyValidateRefreshes

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))

	c.Check(validateCalled, Equals, true)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "some-channel")
}

func (s *snapmgrTestSuite) TestUpdateWithDeviceContext(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: DefaultModel(),
		CtxStore:    s.fakeStore,
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	validateCalled := false
	happyValidateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx1 snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(deviceCtx1, Equals, deviceCtx)
		validateCalled = true
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = happyValidateRefreshes

	ts, err := snapstate.UpdateWithDeviceContext(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{}, deviceCtx, "")
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))

	c.Check(validateCalled, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateWithDeviceContextToRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: DefaultModel(),
		CtxStore:    s.fakeStore,
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(11)}
	ts, err := snapstate.UpdateWithDeviceContext(s.state, "some-snap", opts, 0, snapstate.Flags{}, deviceCtx, "")
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateTasksCoreSetsIgnoreOnConfigure(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "os",
	})

	oldConfigure := snapstate.Configure
	defer func() { snapstate.Configure = oldConfigure }()

	var configureFlags int
	snapstate.Configure = func(st *state.State, snapName string, patch map[string]interface{}, flags int) *state.TaskSet {
		configureFlags = flags
		return state.NewTaskSet()
	}

	_, err := snapstate.Update(s.state, "core", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// ensure the core snap sets the "ignore-hook-error" flag
	c.Check(configureFlags&snapstate.IgnoreHookError, Equals, 1)
}

func (s *snapmgrTestSuite) TestUpdateDevModeConfinementFiltering(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-devmode/stable",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	// updated snap is devmode, refresh without --devmode, do nothing
	// TODO: better error message here
	_, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires devmode or confinement override`)

	// updated snap is devmode, refresh with --devmode
	_, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateClassicConfinementFiltering(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap-now-classic", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap-now-classic", SnapID: "some-snap-now-classic-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	// updated snap is classic, refresh without --classic, do nothing
	// TODO: better error message here
	_, err := snapstate.Update(s.state, "some-snap-now-classic", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires classic confinement`)

	// updated snap is classic, refresh with --classic
	ts, err := snapstate.Update(s.state, "some-snap-now-classic", nil, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap is in classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap-now-classic", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Classic, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateClassicFromClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-classic/stable",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
		Flags:           snapstate.Flags{Classic: true},
	})

	// snap installed with --classic, update needs classic, refresh with --classic works
	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	// devmode overrides the snapsetup classic flag
	ts, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, false)

	// jailmode overrides it too (you need to provide both)
	ts, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{JailMode: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, false)

	// jailmode and classic together gets you both
	ts, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{JailMode: true, Classic: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	// snap installed with --classic, update needs classic, refresh without --classic works
	ts, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snap is in classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Classic, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateStrictFromClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap-was-classic", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel/stable",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap-was-classic", SnapID: "some-snap-was-classic-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
		Flags:           snapstate.Flags{Classic: true},
	})

	// snap installed with --classic, update does not need classic, refresh works without --classic
	_, err := snapstate.Update(s.state, "some-snap-was-classic", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// snap installed with --classic, update does not need classic, refresh works with --classic
	_, err = snapstate.Update(s.state, "some-snap-was-classic", nil, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateChannelFallback(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "latest/edge")
}

func (s *snapmgrTestSuite) TestUpdateTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", nil)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `too early for operation, device not yet seeded or device model not acknowledged`)
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

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("refresh", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "refresh" change in progress`)
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
	mountTask := ta[len(ta)-11]
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

	mountTask := tasks[len(tasks)-11]
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

	mountTask := tasks[len(tasks)-11]
	c.Assert(mountTask.Kind(), Equals, "mount-snap")
	var installRecord backend.InstallRecord
	c.Assert(mountTask.Get("install-record", &installRecord), Equals, state.ErrNoState)
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

func (s *snapmgrTestSuite) TestUnlinkCurrentSnapLastActiveDisabledServicesSet(c *C) {
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(-42),
	}
	snaptest.MockSnap(c, `name: services-snap`, &si)

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:                     true,
		Sequence:                   []*snap.SideInfo{&si},
		Current:                    si.Revision,
		SnapType:                   "app",
		TrackingChannel:            "stable",
		LastActiveDisabledServices: []string{},
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{Amend: true})

	c.Assert(err, IsNil)
	// only add up to unlink-current-snap task
	for _, t := range ts.Tasks() {
		chg.AddTask(t)
		if t.Kind() == "unlink-current-snap" {
			// don't add any more from this point on
			break
		}
	}

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})
}

func (s *snapmgrTestSuite) TestUnlinkCurrentSnapMergedLastActiveDisabledServicesSet(c *C) {
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(-42),
	}
	snaptest.MockSnap(c, `name: services-snap`, &si)

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:                     true,
		Sequence:                   []*snap.SideInfo{&si},
		Current:                    si.Revision,
		SnapType:                   "app",
		TrackingChannel:            "stable",
		LastActiveDisabledServices: []string{"missing-svc3"},
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{Amend: true})

	c.Assert(err, IsNil)
	// only add up to unlink-current-snap task
	for _, t := range ts.Tasks() {
		chg.AddTask(t)
		if t.Kind() == "unlink-current-snap" {
			// don't add any more from this point on
			break
		}
	}

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"missing-svc3", "svc1", "svc2"})
}

func (s *snapmgrTestSuite) TestUnlinkCurrentSnapPassthroughLastActiveDisabledServicesSet(c *C) {
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(-42),
	}
	snaptest.MockSnap(c, `name: services-snap`, &si)

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:                     true,
		Sequence:                   []*snap.SideInfo{&si},
		Current:                    si.Revision,
		SnapType:                   "app",
		TrackingChannel:            "stable",
		LastActiveDisabledServices: []string{"missing-svc3"},
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{Amend: true})

	c.Assert(err, IsNil)
	// only add up to unlink-current-snap task
	for _, t := range ts.Tasks() {
		chg.AddTask(t)
		if t.Kind() == "unlink-current-snap" {
			// don't add any more from this point on
			break
		}
	}

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"missing-svc3"})
}

func (s *snapmgrTestSuite) TestStopSnapServicesSavesSnapSetupLastActiveDisabledServices(c *C) {
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

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "services-snap",
			Revision: snap.R(11),
			SnapID:   "services-snap-id",
		},
	}

	chg := s.state.NewChange("stop-services", "stop the services")
	t1 := s.state.NewTask("prerequisites", "...")
	t1.Set("snap-setup", snapsup)
	t2 := s.state.NewTask("stop-snap-services", "...")
	t2.Set("stop-reason", snap.StopReasonDisable)
	t2.Set("snap-setup-task", t1.ID())
	t2.WaitFor(t1)
	chg.AddTask(t1)
	chg.AddTask(t2)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap setup from the task from state
	endT := s.state.Task(t1.ID())
	finalsnapsup := &snapstate.SnapSetup{}
	endT.Get("snap-setup", finalsnapsup)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(finalsnapsup.LastActiveDisabledServices)
	c.Assert(finalsnapsup.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})

}

func (s *snapmgrTestSuite) TestStopSnapServicesFirstSavesSnapSetupLastActiveDisabledServices(c *C) {
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

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "services-snap",
			Revision: snap.R(11),
			SnapID:   "services-snap-id",
		},
	}

	chg := s.state.NewChange("stop-services", "stop the services")
	t := s.state.NewTask("stop-snap-services", "...")
	t.Set("stop-reason", snap.StopReasonDisable)
	t.Set("snap-setup", snapsup)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap setup from the task from state
	endT := s.state.Task(t.ID())
	finalsnapsup := &snapstate.SnapSetup{}
	endT.Get("snap-setup", finalsnapsup)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(finalsnapsup.LastActiveDisabledServices)
	c.Assert(finalsnapsup.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})
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

func (s *snapmgrTestSuite) TestUpdateAmendRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-42),
	}
	snaptest.MockSnap(c, `name: some-snap`, &si)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{Amend: true})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
	}})
	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true, Commentf("salts seen: %v", s.fakeStore.seenPrivacyKeys))
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, []string{
		"storesvc-snap-action",
		"storesvc-snap-action:action",
		"storesvc-download",
		"validate-snap:Doing",
		"current",
		"open-snap-file",
		"setup-snap",
		"remove-snap-aliases",
		"unlink-snap",
		"copy-data",
		"setup-profiles:Doing",
		"candidate",
		"link-snap",
		"auto-connect:Doing",
		"update-aliases",
		"cleanup-trash",
	})
	// just check the interesting op
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:       "install", // we asked for an Update, but an amend is actually an Install
			InstanceName: "some-snap",
			Channel:      "some-channel",
			Epoch:        snap.E("1*"), // in amend, epoch in the action is not nil!
			Flags:        store.SnapActionEnforceValidation,
		},
		revno:  snap.R(11),
		userID: 1,
	})

	task := ts.Tasks()[1]
	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel: "some-channel",
		UserID:  s.user.ID,

		SnapPath: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		PlugsOnly: true,
		Flags:     snapstate.Flags{Amend: true},
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(11),
		Channel:  "some-channel",
		SnapID:   "some-snap-id",
	})

	// verify services stop reason
	verifyStopReason(c, ts, "refresh")

	// check post-refresh hook
	task = ts.Tasks()[14]
	c.Assert(task.Kind(), Equals, "run-hook")
	c.Assert(task.Summary(), Matches, `Run post-refresh hook of "some-snap" snap if present`)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(-42),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "some-channel",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateRunThrough(c *C) {
	// we start without the auxiliary store info (or with an older one)
	c.Check(snapstate.AuxStoreInfoFilename("services-snap-id"), testutil.FileAbsent)

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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
		CohortKey:       "embattled",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{
		Channel:   "some-channel",
		CohortKey: "some-cohort",
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "services-snap",
				SnapID:          "services-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "latest/stable",
				RefreshedDate:   refreshedDate,
				Epoch:           snap.E("0"),
				CohortKey:       "embattled",
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "services-snap",
				SnapID:       "services-snap-id",
				Channel:      "some-channel",
				CohortKey:    "some-cohort",
				Flags:        store.SnapActionEnforceValidation,
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
			path: filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "services-snap",
				SnapID:   "services-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "services-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "stop-snap-services:refresh",
			path: filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op: "current-snap-service-states",
		},
		{
			op:   "remove-snap-aliases",
			name: "services-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "services-snap/11"),
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
			path: filepath.Join(dirs.SnapMountDir, "services-snap/11"),
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
			op:       "start-snap-services",
			path:     filepath.Join(dirs.SnapMountDir, "services-snap/11"),
			services: []string{"svc1", "svc3", "svc2"},
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
		target:   filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
	}})
	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true, Commentf("salts seen: %v", s.fakeStore.seenPrivacyKeys))
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
		Channel:   "some-channel",
		CohortKey: "some-cohort",
		UserID:    s.user.ID,

		SnapPath: filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		PlugsOnly: true,
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
	c.Check(snapst.CohortKey, Equals, "some-cohort")

	// we end up with the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("services-snap-id"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateRunThrough(c *C) {
	// use services-snap here to make sure services would be stopped/started appropriately
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(7),
		SnapID:   "services-snap-id",
	}
	snaptest.MockSnapInstance(c, "services-snap_instance", `name: services-snap`, &si)
	fi, err := os.Stat(snap.MountFile("services-snap_instance", si.Revision))
	c.Assert(err, IsNil)
	refreshedDate := fi.ModTime()
	// look at disk
	r := snapstate.MockRevisionDate(nil)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	snapstate.Set(s.state, "services-snap_instance", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
		InstanceKey:     "instance",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap_instance", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "services-snap_instance",
				SnapID:          "services-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "latest/stable",
				RefreshedDate:   refreshedDate,
				Epoch:           snap.E("0"),
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				SnapID:       "services-snap-id",
				InstanceName: "services-snap_instance",
				Channel:      "some-channel",
				Flags:        store.SnapActionEnforceValidation,
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
			name:  "services-snap_instance",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "services-snap_instance_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "services-snap",
				SnapID:   "services-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "services-snap_instance",
			path:  filepath.Join(dirs.SnapBlobDir, "services-snap_instance_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "stop-snap-services:refresh",
			path: filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
		},
		{
			op: "current-snap-service-states",
		},
		{
			op:   "remove-snap-aliases",
			name: "services-snap_instance",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "services-snap_instance/11"),
			old:  filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "services-snap_instance",
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
			path: filepath.Join(dirs.SnapMountDir, "services-snap_instance/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "services-snap_instance",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:       "start-snap-services",
			path:     filepath.Join(dirs.SnapMountDir, "services-snap_instance/11"),
			services: []string{"svc1", "svc3", "svc2"},
		},
		{
			op:    "cleanup-trash",
			name:  "services-snap_instance",
			revno: snap.R(11),
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "services-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "services-snap_instance_11.snap"),
	}})
	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true, Commentf("salts seen: %v", s.fakeStore.seenPrivacyKeys))
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

		SnapPath: filepath.Join(dirs.SnapBlobDir, "services-snap_instance_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo:    snapsup.SideInfo,
		Type:        snap.TypeApp,
		PlugsOnly:   true,
		InstanceKey: "instance",
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
	c.Assert(task.Summary(), Matches, `Run post-refresh hook of "services-snap_instance" snap if present`)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap_instance", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.InstanceKey, Equals, "instance")
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
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{si},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-base/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "some-base", target: filepath.Join(dirs.SnapBlobDir, "some-base_11.snap")},
		{macaroon: s.user.StoreMacaroon, name: "some-snap", target: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap")},
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
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{si},
		Current:         snap.R(7),
		SnapType:        "app",
	})
	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: []*snap.SideInfo{{
			RealName: "some-base",
			SnapID:   "some-base-id",
			Revision: snap.R(1),
		}},
		Current:  snap.R(1),
		SnapType: "base",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-base"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "some-snap", target: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap")},
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
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{si},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "snap-content-plug", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "snap-content-plug", target: filepath.Join(dirs.SnapBlobDir, "snap-content-plug_11.snap")},
		{macaroon: s.user.StoreMacaroon, name: "snap-content-slot", target: filepath.Join(dirs.SnapBlobDir, "snap-content-slot_11.snap")},
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
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{si},
		Current:         snap.R(7),
		SnapType:        "app",
	})
	snapstate.Set(s.state, "snap-content-slot", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: []*snap.SideInfo{{
			RealName: "snap-content-slot",
			SnapID:   "snap-content-slot-id",
			Revision: snap.R(1),
		}},
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "snap-content-plug", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "snap-content-plug", target: filepath.Join(dirs.SnapBlobDir, "snap-content-plug_11.snap")},
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
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
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
				target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
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
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(11)}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
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
				target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			}, Commentf(snapName))
		}
	}
}

func (s *snapmgrTestSuite) TestUpdateModelKernelSwitchTrackRunThrough(c *C) {
	// use services-snap here to make sure services would be stopped/started appropriately
	si := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(7),
		SnapID:   "kernel-id",
	}
	snaptest.MockSnap(c, `name: kernel`, &si)
	fi, err := os.Stat(snap.MountFile("kernel", si.Revision))
	c.Assert(err, IsNil)
	refreshedDate := fi.ModTime()
	// look at disk
	r := snapstate.MockRevisionDate(nil)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	r1 := snapstatetest.MockDeviceModel(ModelWithKernelTrack("18"))
	defer r1()
	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		TrackingChannel: "18/stable",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "kernel", &snapstate.RevisionOptions{Channel: "edge"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoneStatus)

	c.Assert(len(s.fakeBackend.ops) > 2, Equals, true)
	c.Assert(s.fakeBackend.ops[:2], DeepEquals, fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "kernel",
				SnapID:          "kernel-id",
				Revision:        snap.R(7),
				TrackingChannel: "18/stable",
				RefreshedDate:   refreshedDate,
				Epoch:           snap.E("1*"),
			}},
			userID: 1,
		}, {
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "kernel",
				SnapID:       "kernel-id",
				Channel:      "18/edge",
				Flags:        store.SnapActionEnforceValidation,
			},
			revno:  snap.R(11),
			userID: 1,
		},
	})

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
		Channel: "18/edge",
		UserID:  s.user.ID,

		SnapPath: filepath.Join(dirs.SnapBlobDir, "kernel_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeKernel,
		PlugsOnly: true,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(11),
		Channel:  "18/edge",
		SnapID:   "kernel-id",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "kernel", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "18/edge")
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "kernel",
		SnapID:   "kernel-id",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "kernel",
		Channel:  "18/edge",
		SnapID:   "kernel-id",
		Revision: snap.R(11),
	})
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
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.state.Unlock()
	defer s.se.Stop()
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
				{
					InstanceName:  "core",
					SnapID:        "core-snap-id",
					Revision:      snap.R(1),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1),
					Epoch:         snap.E("1*"),
				},
				{
					InstanceName:  "services-snap",
					SnapID:        "services-snap-id",
					Revision:      snap.R(2),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 2),
					Epoch:         snap.E("0"),
				},
				{
					InstanceName:  "some-snap",
					SnapID:        "some-snap-id",
					Revision:      snap.R(5),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 5),
					Epoch:         snap.E("1*"),
				},
			})
		case "storesvc-snap-action:action":
			snapID := op.action.SnapID
			seen[snapID] = op.userID
		case "storesvc-download":
			snapName := op.name
			fakeDl := s.fakeStore.downloads[di]
			// check target path separately and clear it
			c.Check(fakeDl.target, Matches, filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_[0-9]+.snap", snapName)))
			fakeDl.target = ""
			c.Check(fakeDl, DeepEquals, fakeDownload{
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
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 2, nil)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.state.Unlock()
	defer s.se.Stop()
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
				{
					InstanceName:  "core",
					SnapID:        "core-snap-id",
					Revision:      snap.R(1),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1),
					Epoch:         snap.E("1*"),
				},
				{
					InstanceName:  "services-snap",
					SnapID:        "services-snap-id",
					Revision:      snap.R(2),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 2),
					Epoch:         snap.E("0"),
				},
				{
					InstanceName:  "some-snap",
					SnapID:        "some-snap-id",
					Revision:      snap.R(5),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 5),
					Epoch:         snap.E("1*"),
				},
			})
		case "storesvc-snap-action:action":
			snapID := op.action.SnapID
			seen[snapIDuserID{snapID: snapID, userID: op.userID}] = true
		case "storesvc-download":
			snapName := op.name
			fakeDl := s.fakeStore.downloads[di]
			// check target path separately and clear it
			c.Check(fakeDl.target, Matches, filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_[0-9]+.snap", snapName)))
			fakeDl.target = ""
			c.Check(fakeDl, DeepEquals, fakeDownload{
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
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.state.Unlock()
	defer s.se.Stop()
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
				{
					InstanceName:  "core",
					SnapID:        "core-snap-id",
					Revision:      snap.R(1),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1),
					Epoch:         snap.E("1*"),
				},
				{
					InstanceName:  "services-snap",
					SnapID:        "services-snap-id",
					Revision:      snap.R(2),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 2),
					Epoch:         snap.E("0"),
				},
				{
					InstanceName:  "some-snap",
					SnapID:        "some-snap-id",
					Revision:      snap.R(5),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 5),
					Epoch:         snap.E("1*"),
				},
			})
		case "storesvc-snap-action:action":
			snapID := op.action.SnapID
			if _, ok := seen[snapID]; !ok {
				seen[snapID] = op.userID
			}
		case "storesvc-download":
			snapName := op.name
			fakeDl := s.fakeStore.downloads[di]
			// check target path separately and clear it
			c.Check(fakeDl.target, Matches, filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_[0-9]+.snap", snapName)))
			fakeDl.target = ""
			c.Check(fakeDl, DeepEquals, fakeDownload{
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
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/some-snap/11")

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:  "some-snap",
				SnapID:        "some-snap-id",
				Revision:      snap.R(7),
				RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
				Epoch:         snap.E("1*"),
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "some-snap",
				SnapID:       "some-snap-id",
				Channel:      "some-channel",
				Flags:        store.SnapActionEnforceValidation,
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
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
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
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op: "update-aliases",
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

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
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

func lastWithLane(tasks []*state.Task) *state.Task {
	for i := len(tasks) - 1; i >= 0; i-- {
		if lanes := tasks[i].Lanes(); len(lanes) == 1 && lanes[0] != 0 {
			return tasks[i]
		}
	}
	return nil
}

func (s *snapmgrTestSuite) TestUpdateUndoRestoresRevisionConfig(c *C) {
	var errorTaskExecuted bool

	// overwrite error-trigger task handler with custom one for this test
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		st := task.State()
		st.Lock()
		defer st.Unlock()

		// modify current config of some-snap
		tr := config.NewTransaction(st)
		tr.Set("some-snap", "foo", "canary")
		tr.Commit()

		errorTaskExecuted = true
		return errors.New("error out")
	}
	s.o.TaskRunner().AddHandler("error-trigger", erroringHandler, nil)

	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(6),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{&si2, &si},
		TrackingChannel: "latest/stable",
		Current:         si.Revision,
		SnapType:        "app",
	})

	// set some configuration
	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "revision 7 value")
	tr.Commit()
	config.SaveRevisionConfig(s.state, "some-snap", snap.R(7))

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	last := lastWithLane(ts.Tasks())
	c.Assert(last, NotNil)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	terr.JoinLane(last.Lanes()[0])
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(errorTaskExecuted, Equals, true)

	// after undoing the update some-snap config should be restored to that of rev.7
	var val string
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &val), IsNil)
	c.Check(val, Equals, "revision 7 value")
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "latest/stable",
		Current:         si.Revision,
		SnapType:        "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// We need to make it not be rerefresh, and we could do just
	// that but instead we do the 'right' thing and attach it to
	// the last task that's on a lane.
	last := lastWithLane(ts.Tasks())
	c.Assert(last, NotNil)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	terr.JoinLane(last.Lanes()[0])
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "some-snap",
				SnapID:          "some-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "latest/stable",
				RefreshedDate:   fakeRevDateEpoch.AddDate(0, 0, 7),
				Epoch:           snap.E("1*"),
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "some-snap",
				SnapID:       "some-snap-id",
				Channel:      "some-channel",
				Flags:        store.SnapActionEnforceValidation,
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
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
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
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op: "update-aliases",
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

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
	}})
	// friendlier failure first
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "latest/stable")
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, store.ErrNoUpdateAvailable)
}

// A noResultsStore returns no results for install/refresh requests
type noResultsStore struct {
	*fakeStore
}

func (n noResultsStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, error) {
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, snapstate.ErrMissingExpectedResult)
}

func (s *snapmgrTestSuite) TestUpdateNoStoreResultsWithChannelChange(c *C) {
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-9/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, snapstate.ErrMissingExpectedResult)
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "other-chanenl/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{})
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "other-channel/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// make it visible
	s.state.NewChange("refresh", "refresh a snap").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "other-channel/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		// we just expect the "storesvc-snap-action" ops, we
		// don't have a fakeOp for switchChannel because it has
		// not a backend method, it just manipulates the state
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "some-snap",
				SnapID:          "some-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "other-channel/stable",
				RefreshedDate:   fakeRevDateEpoch.AddDate(0, 0, 7),
				Epoch:           snap.E("1*"),
			}},
			userID: 1,
		},

		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "some-snap",
				SnapID:       "some-snap-id",
				Channel:      "channel-for-7/stable",
				Flags:        store.SnapActionEnforceValidation,
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
		Channel:  "channel-for-7/stable",
		SideInfo: snapsup.SideInfo,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7/stable",
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
		Channel:  "channel-for-7/stable",
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{IgnoreValidation: true})
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)
	// make it visible
	s.state.NewChange("refresh", "refresh a snap").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{IgnoreValidation: true})
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
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
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
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
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	flags := snapstate.Flags{JailMode: true, IgnoreValidation: true}
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, flags)
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
	validateRefreshesFail := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		if len(ignoreValidation) == 0 {
			return nil, validateErr
		}
		c.Check(ignoreValidation, DeepEquals, map[string]bool{
			"some-snap": true,
		})
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshesFail

	flags := snapstate.Flags{IgnoreValidation: true}
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, flags)
	c.Assert(err, IsNil)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(7),
			IgnoreValidation: false,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:            snap.E("1*"),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(11),
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap",
			SnapID:       "some-snap-id",
			Channel:      "stable",
			Flags:        store.SnapActionIgnoreValidation,
		},
		userID: 1,
	})

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
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
	_, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(11),
			TrackingChannel:  "latest/stable",
			IgnoreValidation: true,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 11),
			Epoch:            snap.E("1*"),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(12),
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap",
			SnapID:       "some-snap-id",
			Flags:        0,
		},
		userID: 1,
	})

	chg = s.state.NewChange("refresh", "refresh snaps")
	chg.AddAll(tts[0])

	s.state.Unlock()
	defer s.se.Stop()
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
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes
	flags = snapstate.Flags{}
	ts, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, flags)
	c.Assert(err, IsNil)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(12),
			TrackingChannel:  "latest/stable",
			IgnoreValidation: true,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 12),
			Epoch:            snap.E("1*"),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(11),
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap",
			SnapID:       "some-snap-id",
			Channel:      "latest/stable",
			Flags:        store.SnapActionEnforceValidation,
		},
		userID: 1,
	})

	chg = s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	snapst = snapstate.SnapState{}
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, false)
	c.Check(snapst.Current, Equals, snap.R(11))
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateIgnoreValidationSticky(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{&si},
		Current:     si.Revision,
		SnapType:    "app",
		InstanceKey: "instance",
	})

	validateErr := errors.New("refresh control error")
	validateRefreshesFail := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 2)
		if len(ignoreValidation) == 0 {
			return nil, validateErr
		}
		c.Check(ignoreValidation, DeepEquals, map[string]bool{
			"some-snap_instance": true,
		})
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshesFail

	flags := snapstate.Flags{IgnoreValidation: true}
	ts, err := snapstate.Update(s.state, "some-snap_instance", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, flags)
	c.Assert(err, IsNil)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(7),
			IgnoreValidation: false,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:            snap.E("1*"),
		}, {
			InstanceName:     "some-snap_instance",
			SnapID:           "some-snap-id",
			Revision:         snap.R(7),
			IgnoreValidation: false,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:            snap.E("1*"),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(11),
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap_instance",
			SnapID:       "some-snap-id",
			Channel:      "stable",
			Flags:        store.SnapActionIgnoreValidation,
		},
		userID: 1,
	})

	chg := s.state.NewChange("refresh", "refresh snaps")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap 'instance' has IgnoreValidation set and the snap was
	// updated
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(11))
	// and the other snap does not
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Current, Equals, snap.R(7))
	c.Check(snapst.IgnoreValidation, Equals, false)

	s.fakeBackend.ops = nil
	s.fakeStore.refreshRevnos = map[string]snap.Revision{
		"some-snap-id": snap.R(12),
	}
	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "some-snap_instance"}, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 3)
	verifyLastTasksetIsReRefresh(c, tts)
	sort.Strings(updates)
	c.Check(updates, DeepEquals, []string{"some-snap", "some-snap_instance"})

	chg = s.state.NewChange("refresh", "refresh snaps")
	for _, ts := range tts[:len(tts)-1] {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, false)
	c.Check(snapst.Current, Equals, snap.R(12))

	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(12))

	for i := 0; i < 2; i++ {
		op := s.fakeBackend.ops[i]
		switch op.op {
		case "storesvc-snap-action":
			c.Check(op, DeepEquals, fakeOp{
				op: "storesvc-snap-action",
				curSnaps: []store.CurrentSnap{{
					InstanceName:     "some-snap",
					SnapID:           "some-snap-id",
					Revision:         snap.R(7),
					IgnoreValidation: false,
					RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 7),
					Epoch:            snap.E("1*"),
				}, {
					InstanceName:     "some-snap_instance",
					SnapID:           "some-snap-id",
					Revision:         snap.R(11),
					TrackingChannel:  "latest/stable",
					IgnoreValidation: true,
					RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 11),
					Epoch:            snap.E("1*"),
				}},
				userID: 1,
			})
		case "storesvc-snap-action:action":
			switch op.action.InstanceName {
			case "some-snap":
				c.Check(op, DeepEquals, fakeOp{
					op:    "storesvc-snap-action:action",
					revno: snap.R(12),
					action: store.SnapAction{
						Action:       "refresh",
						InstanceName: "some-snap",
						SnapID:       "some-snap-id",
						Flags:        0,
					},
					userID: 1,
				})
			case "some-snap_instance":
				c.Check(op, DeepEquals, fakeOp{
					op:    "storesvc-snap-action:action",
					revno: snap.R(12),
					action: store.SnapAction{
						Action:       "refresh",
						InstanceName: "some-snap_instance",
						SnapID:       "some-snap-id",
						Flags:        0,
					},
					userID: 1,
				})
			default:
				c.Fatalf("unexpected instance name %q", op.action.InstanceName)
			}
		default:
			c.Fatalf("unexpected action %q", op.op)
		}
	}

}

func (s *snapmgrTestSuite) TestUpdateFromLocal(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R("x1"),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{Amend: true})
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 0, ts, s.state)

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
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "latest/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "snap-unknown", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{Amend: true})
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

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:         snap.E("1*"),
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

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Check(updates, DeepEquals, []string{"some-snap"})

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:         snap.E("1*"),
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

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, s.user.ID, nil)
	c.Check(err, IsNil)
	c.Check(updates, HasLen, 0)

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
			Block:         []snap.Revision{snap.R(11)},
			Epoch:         snap.E("1*"),
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
		switch info.InstanceName() {
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
		for _, instanceName := range []string{"some-snap", "other-snap"} {
			var snapst snapstate.SnapState
			err := snapstate.Get(s.state, instanceName, &snapst)
			c.Assert(err, IsNil)
			snapst.Aliases = nil
			snapst.AutoAliasesDisabled = false
			if autoAliases := scenario.aliasesBefore[instanceName]; autoAliases != nil {
				targets := make(map[string]*snapstate.AliasTarget)
				for _, alias := range autoAliases {
					targets[alias] = &snapstate.AliasTarget{Auto: "cmd" + alias[len(alias)-1:]}
				}

				snapst.Aliases = targets
			}
			snapstate.Set(s.state, instanceName, &snapst)
		}

		updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, scenario.names, s.user.ID, nil)
		c.Check(err, IsNil)
		if scenario.update {
			verifyLastTasksetIsReRefresh(c, tts)
		}

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
				taskAliases[snapsup.InstanceName()] = expectedSet(aliases)
			}
			expectedPruned = make(map[string]map[string]bool)
			for _, instanceName := range scenario.prune {
				expectedPruned[instanceName] = expectedSet(dropped[instanceName])
				if instanceName == "other-snap" && !scenario.new && !scenario.update {
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
		l := len(tts)
		if scenario.update {
			l--
		}
		c.Assert(j, Equals, l, Commentf("%#v", scenario))

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
		switch info.InstanceName() {
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

		for _, instanceName := range []string{"some-snap", "other-snap"} {
			var snapst snapstate.SnapState
			err := snapstate.Get(s.state, instanceName, &snapst)
			c.Assert(err, IsNil)
			snapst.Aliases = nil
			snapst.AutoAliasesDisabled = false
			if autoAliases := scenario.aliasesBefore[instanceName]; autoAliases != nil {
				targets := make(map[string]*snapstate.AliasTarget)
				for _, alias := range autoAliases {
					targets[alias] = &snapstate.AliasTarget{Auto: "cmd" + alias[len(alias)-1:]}
				}

				snapst.Aliases = targets
			}
			snapstate.Set(s.state, instanceName, &snapst)
		}

		ts, err := snapstate.Update(s.state, scenario.names[0], nil, s.user.ID, snapstate.Flags{})
		c.Assert(err, IsNil)
		_, dropped, err := snapstate.AutoAliasesDelta(s.state, []string{"some-snap", "other-snap"})
		c.Assert(err, IsNil)

		j := 0

		tasks := ts.Tasks()
		// make sure the last task from Update is the rerefresh
		if scenario.update {
			reRefresh := tasks[len(tasks)-1]
			c.Check(reRefresh.Kind(), Equals, "check-rerefresh")
			// nothing should wait on it
			c.Check(reRefresh.NumHaltTasks(), Equals, 0)
			tasks = tasks[:len(tasks)-1] // and now forget about it
		}

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
				taskAliases[snapsup.InstanceName()] = expectedSet(aliases)
			}
			expectedPruned = make(map[string]map[string]bool)
			for _, instanceName := range scenario.prune {
				expectedPruned[instanceName] = expectedSet(dropped[instanceName])
			}
			c.Check(taskAliases, DeepEquals, expectedPruned)
		}
		if scenario.update {
			first := tasks[j]
			j += 19
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
		err = snapstate.CheckChangeConflict(s.state, scenario.names[0], nil)
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

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
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

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `refreshing disabled snap "some-snap" not supported`)
}

func (s *snapmgrTestSuite) TestUpdateKernelTrackChecksSwitchingTracks(c *C) {
	si := snap.SideInfo{
		RealName: "kernel",
		SnapID:   "kernel-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(ModelWithKernelTrack("18"))
	defer r()
	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		TrackingChannel: "18/stable",
	})

	// switching tracks is not ok
	_, err := snapstate.Update(s.state, "kernel", &snapstate.RevisionOptions{Channel: "new-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot switch from kernel track "18" as specified for the \(device\) model to "new-channel"`)

	// no change to the channel is ok
	_, err = snapstate.Update(s.state, "kernel", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// switching risk level is ok
	_, err = snapstate.Update(s.state, "kernel", &snapstate.RevisionOptions{Channel: "18/beta"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// switching just risk within the pinned track is ok
	_, err = snapstate.Update(s.state, "kernel", &snapstate.RevisionOptions{Channel: "beta"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateGadgetTrackChecksSwitchingTracks(c *C) {
	si := snap.SideInfo{
		RealName: "brand-gadget",
		SnapID:   "brand-gadget-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(ModelWithGadgetTrack("18"))
	defer r()
	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		TrackingChannel: "18/stable",
	})

	// switching tracks is not ok
	_, err := snapstate.Update(s.state, "brand-gadget", &snapstate.RevisionOptions{Channel: "new-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot switch from gadget track "18" as specified for the \(device\) model to "new-channel"`)

	// no change to the channel is ok
	_, err = snapstate.Update(s.state, "brand-gadget", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// switching risk level is ok
	_, err = snapstate.Update(s.state, "brand-gadget", &snapstate.RevisionOptions{Channel: "18/beta"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// switching just risk within the pinned track is ok
	_, err = snapstate.Update(s.state, "brand-gadget", &snapstate.RevisionOptions{Channel: "beta"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

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
	c.Assert(s.fakeBackend.ops.Ops(), HasLen, 9)
	c.Check(s.fakeBackend.ops[0].op, Equals, "current")
	c.Check(s.fakeBackend.ops[0].old, Equals, "<no-current>")
	// and setup-snap
	c.Check(s.fakeBackend.ops[1].op, Equals, "setup-snap")
	c.Check(s.fakeBackend.ops[1].name, Equals, "some-snap")
	c.Check(s.fakeBackend.ops[1].path, Matches, `.*/orig-name_1.0_all.snap`)
	c.Check(s.fakeBackend.ops[1].revno, Equals, snap.R(42))

	c.Check(s.fakeBackend.ops[4].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[4].sinfo, DeepEquals, *si)
	c.Check(s.fakeBackend.ops[5].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[5].path, Equals, filepath.Join(dirs.SnapMountDir, "some-snap/42"))

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
					Revision: snap.R(7),
					SnapID:   "some-snap-id",
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
	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(2)}
	ts, err := snapstate.Update(s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
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

func (s *snapmgrTestSuite) TestRefreshDoesntRestoreRevisionConfig(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

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

	// set global configuration (affecting current snap)
	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "100")
	tr.Commit()

	// set per-revision config for the upcoming rev. 2, we don't expect it restored though
	// since only revert restores revision configs.
	s.state.Set("revision-config", map[string]interface{}{
		"some-snap": map[string]interface{}{
			"2": map[string]interface{}{"foo": "200"},
		},
	})

	// simulate a refresh to rev. 2
	chg := s.state.NewChange("update", "update some-snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(2)}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)

	s.state.Lock()
	// config of rev. 1 has been stored in per-revision map
	var cfgs map[string]interface{}
	c.Assert(s.state.Get("revision-config", &cfgs), IsNil)
	c.Assert(cfgs["some-snap"], DeepEquals, map[string]interface{}{
		"1": map[string]interface{}{"foo": "100"},
		"2": map[string]interface{}{"foo": "200"},
	})

	// config of rev. 2 hasn't been restored by refresh, old value returned
	tr = config.NewTransaction(s.state)
	var res string
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)
	c.Assert(res, Equals, "100")
}

func (s *snapmgrTestSuite) TestUpdateDoesGC(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	restore := release.MockOnClassic(false)
	defer restore()

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
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// ensure garbage collection runs as the last tasks
	expectedTail := fakeOps{
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
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/1"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/2"),
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

func (s *snapmgrTestSuite) TestUpdateScenarios(c *C) {
	// TODO: also use channel-for-7 or equiv to check updates that are switches
	for k, t := range switchScenarios {
		s.testUpdateScenario(c, k, t)
	}
}

func (s *snapmgrTestSuite) testUpdateScenario(c *C, desc string, t switchScenario) {
	// reset
	s.fakeBackend.ops = nil

	comment := Commentf("%q (%+v)", desc, t)
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		Channel:  t.chanFrom,
		SnapID:   "some-snap-id",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence:        []*snap.SideInfo{&si},
		Active:          true,
		Current:         si.Revision,
		TrackingChannel: t.chanFrom,
		CohortKey:       t.cohFrom,
	})

	chg := s.state.NewChange("update-snap", t.summary)
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{
		Channel:     t.chanTo,
		CohortKey:   t.cohTo,
		LeaveCohort: t.cohFrom != "" && t.cohTo == "",
	}, 0, snapstate.Flags{})
	c.Assert(err, IsNil, comment)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// switch is not really really doing anything backend related
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, []string{
		"storesvc-snap-action",
		"storesvc-snap-action:action",
		"storesvc-download",
		"validate-snap:Doing",
		"current",
		"open-snap-file",
		"setup-snap",
		"remove-snap-aliases",
		"unlink-snap",
		"copy-data",
		"setup-profiles:Doing",
		"candidate",
		"link-snap",
		"auto-connect:Doing",
		"update-aliases",
		"cleanup-trash",
	}, comment)

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

	// ensure the current info *has* changed
	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil, comment)
	c.Assert(info.Channel, Equals, expectedChanTo, comment)
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

	// Ensure that the non-intance snap is unchanged
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
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/11")

	s.state.Unlock()
	defer s.se.Stop()
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
	ts, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)
	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()
	// verify that we excluded this field from the bugreport
	c.Check(n, Equals, 2)
	c.Check(errExtra, DeepEquals, map[string]string{
		"Channel":  "some-channel",
		"Revision": "11",
	})

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
	c.Check(info.GetType(), Equals, snap.TypeGadget)
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
	c.Check(info.GetType(), Equals, snap.TypeKernel)
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
	c.Check(info.GetType(), Equals, snap.TypeBase)
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
			c.Check(info.GetType(), Equals, snap.TypeOS)
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
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.testTrySetsTryMode(snapstate.Flags{Classic: true}, c, "confinement: classic\n")
}

func (s *snapmgrTestSuite) testTrySetsTryMode(flags snapstate.Flags, c *C, extraYaml ...string) {
	s.state.Lock()
	defer s.state.Unlock()

	// make mock try dir
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), IsNil)
	tryYaml := filepath.Join(d, "meta", "snap.yaml")
	err := os.MkdirAll(filepath.Dir(tryYaml), 0755)
	c.Assert(err, IsNil)
	buf := bytes.Buffer{}
	buf.WriteString("name: foo\nversion: 1.0\n")
	if len(extraYaml) > 0 {
		for _, extra := range extraYaml {
			buf.WriteString(extra)
		}
	}
	err = ioutil.WriteFile(tryYaml, buf.Bytes(), 0644)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("try", "try snap")
	ts, err := snapstate.TryPath(s.state, "foo", d, flags)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

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
		"run-hook[check-health]",
	})

}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlag(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()
	s.testTrySetsTryMode(snapstate.Flags{}, c)
}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesDevMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{DevMode: true}, c)
}
func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesJailMode(c *C) {
	s.testTrySetsTryMode(snapstate.Flags{JailMode: true}, c)
}
func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlagLeavesClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()
	s.testTrySetsTryMode(snapstate.Flags{Classic: true}, c, "confinement: classic\n")
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
	defer s.se.Stop()
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
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        seq,
		Current:         snap.R(opts.current),
		SnapType:        "app",
	})

	var chg *state.Change
	var ts *state.TaskSet
	var err error
	if opts.revert {
		chg = s.state.NewChange("revert", "revert a snap")
		ts, err = snapstate.RevertToRevision(s.state, "some-snap", snap.R(opts.via), snapstate.Flags{})
	} else {
		chg = s.state.NewChange("refresh", "refresh a snap")
		ts, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(opts.via)}, s.user.ID, snapstate.Flags{})
	}
	c.Assert(err, IsNil)
	if opts.fail {
		tasks := ts.Tasks()
		var last *state.Task
		// don't make a task wait on rerefresh, that's bad
		for i := len(tasks) - 1; i > 0; i-- {
			last = tasks[i]
			if last.Kind() != "check-rerefresh" {
				break
			}
		}
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
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(revs(snapst.Sequence), DeepEquals, opts.after)

	return &snapst, ts
}

func (s *snapmgrTestSuite) testUpdateSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	restore := release.MockOnClassic(false)
	defer restore()

	opts.revert = false
	snapst, ts := s.testOpSequence(c, opts)
	// update always ends with current==seq[-1]==via:
	c.Check(snapst.Current.N, Equals, opts.after[len(opts.after)-1])
	c.Check(snapst.Current.N, Equals, opts.via)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 1)
	c.Check(s.fakeBackend.ops.First("copy-data"), DeepEquals, &fakeOp{
		op:   "copy-data",
		path: fmt.Sprintf(filepath.Join(dirs.SnapMountDir, "some-snap/%d"), opts.via),
		old:  fmt.Sprintf(filepath.Join(dirs.SnapMountDir, "some-snap/%d"), opts.current),
	})

	return ts
}

func (s *snapmgrTestSuite) testUpdateFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	restore := release.MockOnClassic(false)
	defer restore()

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
	restore := release.MockOnClassic(false)
	defer restore()

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
	restore := release.MockOnClassic(false)
	defer restore()

	si1 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	si2 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}
	si3 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}
	si4 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{si1, si2, si3, si4},
		Current:         snap.R(2),
		SnapType:        "app",
	})

	// run the update
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 2, ts, s.state)

	// and ensure that it will remove the revisions after "current"
	// (si3, si4)
	var snapsup snapstate.SnapSetup
	tasks := ts.Tasks()

	i := len(tasks) - 8
	c.Check(tasks[i].Kind(), Equals, "clear-snap")
	err = tasks[i].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, si3.Revision)

	i = len(tasks) - 6
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
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(7)}, s.user.ID, snapstate.Flags{})
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
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
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

	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true)

	for i, ts := range tts {
		verifyInstallTasks(c, 0, 0, ts, s.state)
		// check that tasksets are in separate lanes
		for _, t := range ts.Tasks() {
			c.Assert(t.Lanes(), DeepEquals, []int{i + 1})
		}
	}
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

func (s *snapmgrTestSuite) TestNoReRefreshInUpdate(c *C) {
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

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{NoReRefresh: true})
	c.Assert(err, IsNil)

	// ensure we have no re-refresh task
	for _, t := range ts.Tasks() {
		c.Assert(t.Kind(), Not(Equals), "check-rerefresh")
	}

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	// NoReRefresh is consumed and consulted when creating the taskset
	// but is not copied into SnapSetup
	c.Check(snapsup.Flags.NoReRefresh, Equals, false)
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

func (s unhappyStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, error) {

	return nil, fmt.Errorf("a grumpy store")
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
	r := release.MockForcedDevmode(true)
	defer r()
	c.Assert(release.ReleaseInfo.ForceDevMode(), Equals, true)

	defer s.se.Stop()
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

func (s *snapmgrTestSuite) TestEmptyUpdateWithChannelChangeAndAutoAlias(c *C) {
	// this reproduces the cause behind lp:1860324,
	// namely an empty refresh with a channel change on a snap
	// with changed aliases

	s.state.Lock()
	defer s.state.Unlock()

	n := 0
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		if info.InstanceName() == "alias-snap" {
			if n > 0 {
				return map[string]string{
					"alias1": "cmd1",
					"alias2": "cmd2",
				}, nil
			}
			n++
		}
		return nil, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		TrackingChannel: "latest/stable",
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11), SnapID: "alias-snap-id"},
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

	ts, err := snapstate.Update(s.state, "alias-snap", &snapstate.RevisionOptions{Channel: "latest/candidate"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
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
	c.Assert(taskKinds(chg1.Tasks()), HasLen, 28)
	c.Assert(taskKinds(chg2.Tasks()), HasLen, 14)

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

func (s behindYourBackStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, error) {
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

type contentStore struct {
	*fakeStore
	state *state.State
}

func (s contentStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, error) {
	sars, err := s.fakeStore.SnapAction(ctx, currentSnaps, actions, user, opts)
	if err != nil {
		return nil, err
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

	return []store.SnapActionResult{{Info: info}}, err
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

func (s *snapmgrTestSuite) TestUpdateLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// When layouts are disabled we cannot refresh to a snap depending on the feature.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", false)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-layout/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.layouts' to true")

	// When layouts are enabled we can refresh to a snap depending on the feature.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-layout/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateManyExplicitLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// When layouts are disabled we cannot refresh multiple snaps if one of them depends on the feature.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", false)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-layout/stable",
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.layouts' to true")

	// When layouts are enabled we can refresh multiple snaps if one of them depends on the feature.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, _, err = snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateManyLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// When layouts are disabled we cannot refresh multiple snaps if one of them depends on the feature.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", false)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-layout/stable",
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	refreshes, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(refreshes, HasLen, 0)

	// When layouts are enabled we can refresh multiple snaps if one of them depends on the feature.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	refreshes, _, err = snapstate.UpdateMany(context.Background(), s.state, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(refreshes, DeepEquals, []string{"some-snap"})
}

func (s *snapmgrTestSuite) TestUpdateFailsEarlyOnEpochMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-epoch-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-epoch-snap", SnapID: "some-epoch-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-epoch-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot refresh "some-epoch-snap" to new revision 11 with epoch 42, because it can't read the current epoch of 13`)
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

func (s snapmgrTestSuite) TestHasOtherInstances(c *C) {
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
