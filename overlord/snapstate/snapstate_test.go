// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	userclient "github.com/snapcore/snapd/usersession/client"

	// So it registers Configure.
	_ "github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/wrappers"
)

func TestSnapManager(t *testing.T) { TestingT(t) }

type snapmgrBaseTest struct {
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

	restarts map[string]int
}

// state must be locked by caller
func (s *snapmgrBaseTest) settle(c *C) {
	s.state.Unlock()
	defer s.state.Lock()

	err := s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	if err != nil {
		s.state.Lock()
		defer s.state.Unlock()
		c.Error(err)
		s.logTasks(c)
		c.FailNow()
	}
}

func (s *snapmgrBaseTest) logTasks(c *C) {
	for _, chg := range s.state.Changes() {
		c.Logf("\nChange %q (%s):", chg.Summary(), chg.Status())

		for _, t := range chg.Tasks() {
			c.Logf("  %s - %s", t.Summary(), t.Status())

			if t.Status() == state.ErrorStatus {
				c.Logf("    %s", strings.Join(t.Log(), "    \n"))
			}
		}
	}
}

var fakeRevDateEpoch = time.Date(2018, 1, 0, 0, 0, 0, 0, time.UTC)

func (s *snapmgrBaseTest) mockSystemctlCallsUpdateMounts(c *C) (restore func()) {
	r := systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "daemon-reload" {
			return []byte(""), nil
		}
		if len(args) == 3 && args[0] == "--no-reload" && args[1] == "enable" {
			return []byte(""), nil
		}
		if len(args) == 2 && args[0] == "restart" {
			value, ok := s.restarts[args[1]]
			if ok {
				s.restarts[args[1]] = value + 1
			}
			return []byte(""), nil
		}
		c.Errorf("unexpected and unhandled systemctl command: %+v", args)
		return nil, fmt.Errorf("broken test")
	})

	return r
}

func (s *snapmgrBaseTest) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.o = overlord.Mock()
	s.state = s.o.State()
	s.state.Lock()
	_, err := restart.Manager(s.state, "boot-id-0", nil)
	s.state.Unlock()
	c.Assert(err, IsNil)

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
		downloadError:       make(map[string]error),
	}

	// make tests work consistently also in containers
	s.AddCleanup(squashfs.MockNeedsFuse(false))

	// setup a bootloader for policy and boot
	s.bl = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.bl)
	s.AddCleanup(func() { bootloader.Force(nil) })

	oldSetupInstallHook := snapstate.SetupInstallHook
	oldSetupPreRefreshHook := snapstate.SetupPreRefreshHook
	oldSetupPostRefreshHook := snapstate.SetupPostRefreshHook
	oldSetupRemoveHook := snapstate.SetupRemoveHook
	oldSnapServiceOptions := snapstate.SnapServiceOptions
	oldEnsureSnapAbsentFromQuotaGroup := snapstate.EnsureSnapAbsentFromQuotaGroup
	snapstate.SetupInstallHook = hookstate.SetupInstallHook
	snapstate.SetupPreRefreshHook = hookstate.SetupPreRefreshHook
	snapstate.SetupPostRefreshHook = hookstate.SetupPostRefreshHook
	snapstate.SetupRemoveHook = hookstate.SetupRemoveHook
	snapstate.SnapServiceOptions = servicestate.SnapServiceOptions
	snapstate.EnsureSnapAbsentFromQuotaGroup = servicestate.EnsureSnapAbsentFromQuota

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return nil, nil
	})
	s.AddCleanup(restore)

	s.snapmgr, err = snapstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(err, IsNil)

	AddForeignTaskHandlers(s.o.TaskRunner(), s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	s.o.AddManager(s.snapmgr)
	s.o.AddManager(s.o.TaskRunner())
	s.se = s.o.StateEngine()
	c.Assert(s.o.StartUp(), IsNil)
	s.BaseTest.AddCleanup(func() { s.o.Stop() })

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
		snapstate.SnapServiceOptions = oldSnapServiceOptions
		snapstate.EnsureSnapAbsentFromQuotaGroup = oldEnsureSnapAbsentFromQuotaGroup

		dirs.SetRootDir("/")
	})

	s.BaseTest.AddCleanup(snapstate.MockReRefreshRetryTimeout(time.Second / 200))
	s.BaseTest.AddCleanup(snapstate.MockReRefreshUpdateMany(func(context.Context, *state.State, []string, []*snapstate.RevisionOptions, int, snapstate.UpdateFilter, *snapstate.Flags, string) ([]string, *snapstate.UpdateTaskSets, error) {
		return nil, nil, nil
	}))

	oldEstimateSnapshotSize := snapstate.EstimateSnapshotSize
	snapstate.EstimateSnapshotSize = func(st *state.State, instanceName string, users []string) (uint64, error) {
		return 1, nil
	}
	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []snapstate.MinimalInstallInfo, userID int, prqt snapstate.PrereqTracker) (uint64, error) {
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
	s.user, err = auth.NewUser(s.state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	c.Assert(err, IsNil)
	s.user2, err = auth.NewUser(s.state, auth.NewUserParams{
		Username:   "username2",
		Email:      "email2@test.com",
		Macaroon:   "macaroon2",
		Discharges: []string{"discharge2"},
	})
	c.Assert(err, IsNil)
	// 3 has no store auth
	s.user3, err = auth.NewUser(s.state, auth.NewUserParams{
		Username:   "username3",
		Email:      "email2@test.com",
		Macaroon:   "",
		Discharges: nil,
	})
	c.Assert(err, IsNil)

	s.state.Set("seeded", true)
	s.state.Set("seed-time", time.Now())

	r := snapstatetest.MockDeviceModel(DefaultModel())
	s.BaseTest.AddCleanup(r)

	s.state.Set("refresh-privacy-key", "privacy-key")
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	// commonly used revisions in tests
	defaultInfoFile := `
VERSION=2.54.3+git1.g479e745-dirty
SNAPD_APPARMOR_REEXEC=1
`
	for _, snapName := range []string{"snapd", "core"} {
		for _, rev := range []string{"1", "11"} {
			infoFile := filepath.Join(dirs.SnapMountDir, snapName, rev, dirs.CoreLibExecDir, "info")
			err = os.MkdirAll(filepath.Dir(infoFile), 0755)
			c.Assert(err, IsNil)
			err = os.WriteFile(infoFile, []byte(defaultInfoFile), 0644)
			c.Assert(err, IsNil)
		}
	}

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)
	s.state.Unlock()

	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}

	s.AddCleanup(snapstate.MockSecurityProfilesDiscardLate(func(snapName string, rev snap.Revision, typ snap.Type) error {
		return nil
	}))
	s.AddCleanup(osutil.MockMountInfo(""))

	s.restarts = make(map[string]int)
	s.AddCleanup(s.mockSystemctlCallsUpdateMounts(c))

	// mock so the actual notification code isn't called. It races with the SetRootDir
	// call in the TearDown function. It's harmless but triggers go test -race
	s.AddCleanup(snapstate.MockAsyncPendingRefreshNotification(func(context.Context, *userclient.PendingSnapRefreshInfo) {}))
}

func (s *snapmgrBaseTest) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	snapstate.ValidateRefreshes = nil
	snapstate.AutoAliases = nil
	snapstate.CanAutoRefresh = nil
}

type ForeignTaskTracker interface {
	ForeignTask(kind string, status state.Status, snapsup *snapstate.SnapSetup) error
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

		return tracker.ForeignTask(kind, status, snapsup)
	}
	runner.AddHandler("setup-profiles", fakeHandler, fakeHandler)
	runner.AddHandler("auto-connect", fakeHandler, fakeHandler)
	runner.AddHandler("auto-disconnect", fakeHandler, nil)
	runner.AddHandler("remove-profiles", fakeHandler, fakeHandler)
	runner.AddHandler("discard-conns", fakeHandler, fakeHandler)
	runner.AddHandler("validate-snap", fakeHandler, nil)
	runner.AddHandler("transition-ubuntu-core", fakeHandler, nil)
	runner.AddHandler("transition-to-snapd-snap", fakeHandler, nil)
	runner.AddHandler("update-gadget-assets", fakeHandler, nil)
	runner.AddHandler("update-managed-boot-config", fakeHandler, nil)

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

type snapmgrTestSuite struct {
	snapmgrBaseTest
}

var _ = Suite(&snapmgrTestSuite{})

func (s *snapmgrTestSuite) TestCleanSnapStateGet(c *C) {
	snapst := snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		}),
		Current:         snap.R(1),
		SnapType:        "os",
		TrackingChannel: "foo/stable",
		InstanceKey:     "bar",
	}

	s.state.Lock()

	defer s.state.Unlock()
	snapstate.Set(s.state, "no-instance-key", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	err := snapstate.Get(s.state, "bar", nil)
	c.Assert(err, ErrorMatches, "internal error: snapst is nil")

	err = snapstate.Get(s.state, "no-instance-key", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst, DeepEquals, snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		}),
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
	runCoreConfigure
	doesReRefresh
	updatesGadget
	updatesGadgetAssets
	updatesBootConfig
	noConfigure
	noLastBeforeModificationsEdge
	preferInstalled
	localSnap
	needsKernelSetup
	isHybrid
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

func checkIsContinuedAutoRefresh(c *C, tasks []*state.Task, expected bool) {
	for _, t := range tasks {
		if t.Kind() == "download-snap" {
			var snapsup snapstate.SnapSetup
			err := t.Get("snap-setup", &snapsup)
			c.Assert(err, IsNil)
			c.Check(snapsup.IsContinuedAutoRefresh, Equals, expected)
			return
		}
	}
	c.Fatalf("cannot find download-snap task in %v", tasks)
}

func (s *snapmgrTestSuite) TestLastIndexFindsLast(c *C) {
	snapst := &snapstate.SnapState{Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
		{Revision: snap.R(7)},
		{Revision: snap.R(11)},
		{Revision: snap.R(11)},
	})}
	c.Check(snapst.LastIndex(snap.R(11)), Equals, 2)
}

func (s *snapmgrTestSuite) TestSequenceSerialize(c *C) {
	si1 := &snap.SideInfo{RealName: "mysnap", SnapID: "snapid", Revision: snap.R(7)}
	si2 := &snap.SideInfo{RealName: "othersnap", SnapID: "otherid", Revision: snap.R(11)}

	// Without components
	snapst := &snapstate.SnapState{Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
		si1, si2,
	})}
	marshaled, err := json.Marshal(snapst)
	c.Assert(err, IsNil)
	c.Check(string(marshaled), Equals, `{"type":"","sequence":[{"name":"mysnap","snap-id":"snapid","revision":"7"},{"name":"othersnap","snap-id":"otherid","revision":"11"}],"current":"unset"}`)

	// With components
	snapst = &snapstate.SnapState{Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{
		sequence.NewRevisionSideState(si1, []*sequence.ComponentState{
			sequence.NewComponentState(snap.NewComponentSideInfo(naming.NewComponentRef("mysnap", "mycomp"), snap.R(7)), snap.TestComponent),
		}),
		sequence.NewRevisionSideState(si2, []*sequence.ComponentState{
			sequence.NewComponentState(snap.NewComponentSideInfo(naming.NewComponentRef("othersnap", "othercomp1"), snap.R(11)), snap.TestComponent),
			sequence.NewComponentState(snap.NewComponentSideInfo(naming.NewComponentRef("othersnap", "othercomp2"), snap.R(14)), snap.TestComponent),
		}),
	})}
	marshaled, err = json.Marshal(snapst)
	c.Assert(err, IsNil)
	c.Check(string(marshaled), Equals, `{"type":"","sequence":[{"name":"mysnap","snap-id":"snapid","revision":"7","components":[{"side-info":{"component":{"snap-name":"mysnap","component-name":"mycomp"},"revision":"7"},"type":"test"}]},{"name":"othersnap","snap-id":"otherid","revision":"11","components":[{"side-info":{"component":{"snap-name":"othersnap","component-name":"othercomp1"},"revision":"11"},"type":"test"},{"side-info":{"component":{"snap-name":"othersnap","component-name":"othercomp2"},"revision":"14"},"type":"test"}]}],"current":"unset"}`)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(7)},
			{RealName: "some-snap", Revision: snap.R(11)},
		}),
		Flags:    flags.before,
		Current:  snap.R(11),
		SnapType: "app",
	})

	ts, err := snapstate.Revert(s.state, "some-snap", flags.change, "")
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
	c.Check(snapsup.Version, Equals, "some-snapVer")

	chg := s.state.NewChange("revert", "revert snap")
	chg.AddAll(ts)

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(1)},
			{RealName: "some-snap", Revision: snap.R(2)},
			{RealName: "some-snap", Revision: snap.R(3)},
			{RealName: "some-snap", Revision: snap.R(4)},
		}),
		Current: snap.R(2),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R(4), snapstate.Flags{}, "")
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "kernel", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "kernel", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "kernel", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "brand-gadget", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "brand-gadget", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "brand-gadget", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	ts, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(ts)

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	disableTs, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(disableTs)

	s.settle(c)

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

	s.settle(c)

	c.Assert(enableChg.Err(), IsNil)
	c.Assert(enableChg.IsReady(), Equals, true)

	// check the ops that will be provided disabledServices
	svcStateOp := s.fakeBackend.ops.First("current-snap-service-states")
	c.Assert(svcStateOp, Not(IsNil))
	c.Assert(svcStateOp.disabledServices, DeepEquals, []string{"svc1", "svc2"})

	linkStateOp := s.fakeBackend.ops.First("start-snap-services")
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	disableTs, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(disableTs)

	s.settle(c)

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

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
		// keep this to make gofmt 1.10 happy
		LastActiveDisabledServices: []string{"missing-svc3"},
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	disableTs, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(disableTs)

	s.settle(c)

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

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
		// keep this to make gofmt 1.10 happy
		LastActiveDisabledServices: []string{"missing-svc3"},
	})

	disableChg := s.state.NewChange("disable", "disable a snap")
	disableTs, err := snapstate.Disable(s.state, "services-snap")
	c.Assert(err, IsNil)
	disableChg.AddAll(disableTs)

	s.settle(c)

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

	s.settle(c)

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

func (s *snapmgrTestSuite) TestRevertRestoresConfigSnapshot(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", Revision: snap.R(2)},
		}),
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
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &siNew}),
		Current:  snap.R(7),
	})

	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
	})

	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:  snap.R(77),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R("99"), snapstate.Flags{}, "")
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:  snap.R(77),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R("77"), snapstate.Flags{}, "")
	c.Assert(err, ErrorMatches, `already on requested revision`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) testRevertRunThrough(c *C, refreshAppAwarenessUX bool) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&siOld, &si}),
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "some-snap",
			inhibitHint: "refresh",
		},
		{
			op:                 "unlink-snap",
			path:               filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			unlinkSkipBinaries: refreshAppAwarenessUX,
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
	// aliases removal is skipped when refresh-app-awareness-ux is enabled
	if refreshAppAwarenessUX {
		// remove "remove-snap-aliases" operation
		expected = expected[1:]
	}

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify that the R(2) version is active now and R(7) is still there
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	// last refresh time shouldn't be modified on revert.
	c.Check(snapst.LastRefreshTime, IsNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(2),
	}, nil))
	c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(7),
	}, nil))
	c.Check(snapst.RevertStatus, HasLen, 0)
	c.Assert(snapst.Block(), DeepEquals, []snap.Revision{snap.R(7)})
}

func (s *snapmgrTestSuite) TestRevertRunThrough(c *C) {
	s.testRevertRunThrough(c, false)
}

func (s *snapmgrTestSuite) TestRevertRunThroughSkipBinaries(c *C) {
	s.enableRefreshAppAwarenessUX()
	s.testRevertRunThrough(c, true)
}

func (s *snapmgrTestSuite) TestRevertRevisionNotBlocked(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&siOld, &si}),
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{RevertStatus: snapstate.NotBlocked}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	// verify that the R(2) version is active now and R(7) is still there
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	// last refresh time shouldn't be modified on revert.
	c.Check(snapst.LastRefreshTime, IsNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(2),
	}, nil))
	c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(7),
	}, nil))
	// we have reverted from rev 7 to rev 2, but rev 7 is marked as not blocked
	// due to revert.
	c.Check(snapst.RevertStatus, DeepEquals, map[int]snapstate.RevertStatus{
		7: snapstate.NotBlocked,
	})
	c.Assert(snapst.Block(), HasLen, 0)
}

func (s *snapmgrTestSuite) TestRevertRevisionNotBlockedUndo(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:  si2.Revision,
		RevertStatus: map[int]snapstate.RevertStatus{
			3: snapstate.NotBlocked,
		},
	})

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{RevertStatus: snapstate.NotBlocked}, "")
	c.Assert(err, IsNil)
	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]
	terr := s.state.NewTask("error-trigger", "provoking undo")
	terr.WaitFor(last)
	chg.AddAll(ts)
	chg.AddTask(terr)

	s.settle(c)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Check(snapst.RevertStatus, DeepEquals, map[int]snapstate.RevertStatus{
		3: snapstate.NotBlocked,
	})
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core18", SnapID: "core18-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "base",
	})
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	// test snap to revert
	snapstate.Set(s.state, "some-snap-with-base", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&siOld, &si}),
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap-with-base", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap-with-base",
		},
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "some-snap-with-base",
			inhibitHint: "refresh",
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
		Sequence:    snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&siOld, &si}),
		Current:     si.Revision,
		InstanceKey: "instance",
	})

	// another snap withouth instance key
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&siOld, &si}),
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap_instance", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap_instance",
		},
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "some-snap_instance",
			inhibitHint: "refresh",
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
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(2),
	}, nil))
	c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(7),
	}, nil))
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&siOld, &si}),
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Assert(s.fakeBackend.ops.Ops(), HasLen, 8)

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
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &siNew}),
		Current:         snap.R(2),
		TrackingChannel: "latest/edge",
	})

	chg := s.state.NewChange("revert", "revert a snap forward")
	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R(7), snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "some-snap",
			inhibitHint: "refresh",
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
	c.Check(snapst.Sequence.Revisions, HasLen, 2)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:  si2.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	chg.AddTask(terr)

	s.settle(c)

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "some-snap",
			inhibitHint: "refresh",
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
			op:    "auto-connect:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
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
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:  si2.Revision,
	})

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/1")

	s.settle(c)

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "some-snap",
			inhibitHint: "refresh",
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
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Current, Equals, snap.R(2))
}

func (s *snapmgrTestSuite) TestRevertUndoMigrationAfterUnsetFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	snapst := &snapstate.SnapState{
		Active:         true,
		SnapType:       "app",
		Sequence:       snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:        si2.Revision,
		MigratedHidden: true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	assertMigrationState(c, s.state, "some-snap", nil)
}

func (s *snapmgrTestSuite) TestRevertKeepMigrationWithSetFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	snapst := &snapstate.SnapState{
		Active:         true,
		SnapType:       "app",
		Sequence:       snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:        si2.Revision,
		MigratedHidden: true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	assertMigrationState(c, s.state, "some-snap", &dirs.SnapDirOptions{HiddenSnapDataDir: true})
}

func (s *snapmgrTestSuite) TestRevertDoHiddenMigration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	snapst := &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:  si.Revision,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.RevertToRevision(s.state, "some-snap", si2.Revision, snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	s.fakeBackend.ops.MustFindOp(c, "hide-snap-data")
	assertMigrationState(c, s.state, "some-snap", &dirs.SnapDirOptions{HiddenSnapDataDir: true})

	snapst = &snapstate.SnapState{}
	snapstate.Get(s.state, "some-snap", snapst)
	c.Assert(snapst.Current, Equals, si2.Revision)
}

func (s *snapmgrTestSuite) TestUndoRevertDoHiddenMigration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	snapst := &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:  si.Revision,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.RevertToRevision(s.state, "some-snap", si2.Revision, snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	expectedErr := failAfterLinkSnap(s.o, chg)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, fmt.Sprintf(`(.|\s)*%s\)?`, expectedErr.Error()))

	containsInOrder(c, s.fakeBackend.ops, []string{"hide-snap-data", "undo-hide-snap-data"})
	assertMigrationState(c, s.state, "some-snap", nil)

	snapst = &snapstate.SnapState{}
	snapstate.Get(s.state, "some-snap", snapst)
	c.Assert(snapst.Current, Equals, si.Revision)
}

func (s *snapmgrTestSuite) TestRevertFromCore22WithSetFlagKeepMigration(c *C) {
	c.Skip("TODO:Snap-folder: no automatic migration for core22 snaps to ~/Snap folder for now")

	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	restore := snapstate.MockSnapReadInfo(func(_ string, si *snap.SideInfo) (*snap.Info, error) {
		info := &snap.Info{
			SideInfo: *si,
		}
		if si.Revision.N == 1 {
			info.Base = "core20"
		} else if si.Revision.N == 2 {
			info.Base = "core22"
		} else {
			panic(fmt.Sprintf("mocked readInfo: expecting revision 1 or 2 but got %d instead", si.Revision.N))
		}

		return info, nil
	})
	defer restore()

	snapst := &snapstate.SnapState{
		Active:                true,
		SnapType:              "app",
		Sequence:              snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:               si2.Revision,
		MigratedHidden:        true,
		MigratedToExposedHome: true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		Active:   true,
		SnapType: "base",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{Revision: snap.R(1)}}),
		Current:  snap.R(1),
	})

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)
	assertMigrationState(c, s.state, "some-snap", &dirs.SnapDirOptions{HiddenSnapDataDir: true})
}

func (s *snapmgrTestSuite) TestRevertToCore22WithoutFlagSet(c *C) {
	c.Skip("TODO:Snap-folder: no automatic migration for core22 snaps to ~/Snap folder for now")

	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	restore := snapstate.MockSnapReadInfo(func(_ string, si *snap.SideInfo) (*snap.Info, error) {
		return &snap.Info{
			SideInfo: *si,
			Base:     "core22",
		}, nil
	})
	defer restore()

	snapst := &snapstate.SnapState{
		Active:                true,
		SnapType:              "app",
		Sequence:              snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:               si2.Revision,
		MigratedHidden:        true,
		MigratedToExposedHome: true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	snapstate.Set(s.state, "core22", &snapstate.SnapState{
		Active:   true,
		SnapType: "base",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{Revision: snap.R(1)}}),
		Current:  snap.R(1),
	})

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)
	assertMigrationState(c, s.state, "some-snap", &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: true})
}

func (s *snapmgrTestSuite) TestRevertToCore22AfterRevertedHomeMigration(c *C) {
	// test reverting back to a core22 revision after reverting from it partially
	// (the HOME migration was disabled but the hidden one remained)
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: false}
	s.testRevertToCore22AfterRevertedMigration(c, opts)
}

func (s *snapmgrTestSuite) TestRevertToCore22AfterRevertedFullMigration(c *C) {
	// test reverting back to a core22 revision after reverting from it fully (after
	// the first revert, both the hidden and HOME migration were reverted or disabled)
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: false, MigratedToExposedHome: false}
	s.testRevertToCore22AfterRevertedMigration(c, opts)
}

func (s *snapmgrTestSuite) testRevertToCore22AfterRevertedMigration(c *C, migrationState *dirs.SnapDirOptions) {
	c.Skip("TODO:Snap-folder: no automatic migration for core22 snaps to ~/Snap folder for now")

	s.state.Lock()
	defer s.state.Unlock()

	si1 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	restore := snapstate.MockSnapReadInfo(func(_ string, si *snap.SideInfo) (*snap.Info, error) {
		if si.Revision == si1.Revision {
			return &snap.Info{
				SideInfo: *si,
				Base:     "core20",
			}, nil
		} else if si.Revision == si2.Revision {
			return &snap.Info{
				SideInfo: *si,
				Base:     "core22",
			}, nil
		}
		panic("unknown sideinfo")
	})
	defer restore()

	snapst := &snapstate.SnapState{
		Active:                true,
		SnapType:              "app",
		Sequence:              snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si1, &si2}),
		Current:               si1.Revision,
		MigratedHidden:        migrationState.HiddenSnapDataDir,
		MigratedToExposedHome: migrationState.MigratedToExposedHome,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	snapstate.Set(s.state, "core22", &snapstate.SnapState{
		Active:   true,
		SnapType: "base",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{Revision: snap.R(1)}}),
		Current:  snap.R(1),
	})

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.RevertToRevision(s.state, "some-snap", si2.Revision, snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)
	assertMigrationState(c, s.state, "some-snap", &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: true})

	snapst = &snapstate.SnapState{}
	c.Assert(snapstate.Get(s.state, "some-snap", snapst), IsNil)
	c.Check(snapst.Current, Equals, si2.Revision)
}

func (s *snapmgrTestSuite) TestUndoRevertToCore22AfterRevertedHomeMigration(c *C) {
	// test reverting back to a core22 revision after reverting from it partially
	// (the HOME migration was disabled but the hidden one remained)
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: false}
	s.testUndoRevertToCore22AfterRevertedMigration(c, opts)
}

func (s *snapmgrTestSuite) TestUndoRevertToCore22AfterRevertedFullMigration(c *C) {
	// test reverting back to a core22 revision after reverting from it partially
	// (the HOME migration was disabled but the hidden one remained)
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: false, MigratedToExposedHome: false}
	s.testUndoRevertToCore22AfterRevertedMigration(c, opts)
}

func (s *snapmgrTestSuite) testUndoRevertToCore22AfterRevertedMigration(c *C, migrationState *dirs.SnapDirOptions) {
	s.state.Lock()
	defer s.state.Unlock()

	si1 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	restore := snapstate.MockSnapReadInfo(func(_ string, si *snap.SideInfo) (*snap.Info, error) {
		if si.Revision == si1.Revision {
			return &snap.Info{
				SideInfo: *si,
				Base:     "core20",
			}, nil
		} else if si.Revision == si2.Revision {
			return &snap.Info{
				SideInfo: *si,
				Base:     "core22",
			}, nil
		}
		panic("unknown sideinfo")

	})
	defer restore()

	snapst := &snapstate.SnapState{
		Active:                true,
		SnapType:              "app",
		Sequence:              snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si1, &si2}),
		Current:               si1.Revision,
		MigratedHidden:        migrationState.HiddenSnapDataDir,
		MigratedToExposedHome: migrationState.MigratedToExposedHome,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	snapstate.Set(s.state, "core22", &snapstate.SnapState{
		Active:   true,
		SnapType: "base",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{Revision: snap.R(1)}}),
		Current:  snap.R(1),
	})

	chg := s.state.NewChange("revert", "install a revert")
	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/some-snap/2")

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", si2.Revision, snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.ErrorStatus)

	assertMigrationState(c, s.state, "some-snap", migrationState)

	snapst = &snapstate.SnapState{}
	c.Assert(snapstate.Get(s.state, "some-snap", snapst), IsNil)
	c.Check(snapst.Current, Equals, si1.Revision)
}

func (s *snapmgrTestSuite) TestRevertUndoHiddenMigrationFails(c *C) {
	s.testRevertUndoHiddenMigrationFails(c, func(_ *overlord.Overlord, _ *state.Change) error {
		err := errors.New("boom")
		s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
			if op.op == "undo-hide-snap-data" {
				return err
			}

			return nil
		}

		return err
	})
}

func (s *snapmgrTestSuite) TestRevertUndoHiddenMigrationFailsAfterWritingState(c *C) {
	s.testRevertUndoHiddenMigrationFails(c, failAfterLinkSnap)
}

func (s *snapmgrTestSuite) testRevertUndoHiddenMigrationFails(c *C, prepFail prepFailFunc) {
	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	snapst := &snapstate.SnapState{
		Active:         true,
		SnapType:       "app",
		Sequence:       snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:        si2.Revision,
		MigratedHidden: true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	expectedErr := prepFail(s.o, chg)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, fmt.Sprintf(`(.|\s)*%s\)?`, expectedErr.Error()))

	assertMigrationState(c, s.state, "some-snap", &dirs.SnapDirOptions{HiddenSnapDataDir: true})
}

func (s *snapmgrTestSuite) TestRevertUndoExposedMigrationFailsAfterWritingState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "snap-core18-to-core22",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "snap-core18-to-core22",
		Revision: snap.R(2),
	}

	snapst := &snapstate.SnapState{
		Active:                true,
		SnapType:              "app",
		Sequence:              snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:               si2.Revision,
		MigratedHidden:        true,
		MigratedToExposedHome: true,
	}
	snapstate.Set(s.state, "snap-core18-to-core22", snapst)
	c.Assert(snapstate.WriteSeqFile("snap-core18-to-core22", snapst), IsNil)

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "snap-core18-to-core22", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	expectedErr := failAfterLinkSnap(s.o, chg)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, fmt.Sprintf(`(.|\s)*%s\)?`, expectedErr.Error()))

	// check migration is reverted and then re-done
	assertMigrationState(c, s.state, "snap-core18-to-core22", &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: true})
}

func (s *snapmgrTestSuite) TestRevertUndoFullMigrationFails(c *C) {
	s.testRevertUndoFullMigrationFails(c, func(_ *overlord.Overlord, _ *state.Change) error {
		err := errors.New("boom")
		s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
			if op.op == "undo-hide-snap-data" {
				return err
			}

			return nil
		}

		return err
	})
}

func (s *snapmgrTestSuite) TestRevertUndoFullMigrationFailsAfterWritingState(c *C) {
	s.testRevertUndoFullMigrationFails(c, failAfterLinkSnap)
}

func (s *snapmgrTestSuite) testRevertUndoFullMigrationFails(c *C, prepFail prepFailFunc) {
	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "snap-core18-to-core22",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "snap-core18-to-core22",
		Revision: snap.R(2),
	}

	snapst := &snapstate.SnapState{
		Active:                true,
		SnapType:              "app",
		Sequence:              snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:               si2.Revision,
		MigratedHidden:        true,
		MigratedToExposedHome: true,
	}
	snapstate.Set(s.state, "snap-core18-to-core22", snapst)
	c.Assert(snapstate.WriteSeqFile("snap-core18-to-core22", snapst), IsNil)

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "snap-core18-to-core22", snapstate.Flags{}, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	expectedErr := prepFail(s.o, chg)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, fmt.Sprintf(`(.|\s)*%s\)?`, expectedErr.Error()))

	assertMigrationState(c, s.state, "snap-core18-to-core22", &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: true})
}

func (s *snapmgrTestSuite) TestEnableDoesNotEnableAgain(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
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
		Sequence:            snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
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

	s.settle(c)

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
		Version:   "some-snapVer",
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
		Active:   true,
		SnapType: "app",
	})

	chg := s.state.NewChange("disable", "disable a snap")
	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

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
		Version:   "some-snapVer",
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
		Sequence:            snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:             si.Revision,
		Active:              false,
		TrackingChannel:     "latest/edge",
		Flags:               flags,
		AliasesPending:      true,
		AutoAliasesDisabled: true,
		InstanceKey:         "instance",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence:            snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
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

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
		Active:   true,
	})
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Sequence:    snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:     si.Revision,
		Active:      true,
		InstanceKey: "instance",
	})

	chg := s.state.NewChange("disable", "disable a snap")
	ts, err := snapstate.Disable(s.state, "some-snap_instance")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

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
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
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

	s.settle(c)

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
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		TrackingChannel: "latest/edge",
	})

	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		TrackingChannel: "latest/edge",
		InstanceKey:     "instance",
	})

	chg := s.state.NewChange("switch-snap", "switch snap to some-channel")
	ts, err := snapstate.Switch(s.state, "some-snap_instance", &snapstate.RevisionOptions{Channel: "some-channel"})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  snap.R(7),
		Active:   false,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" already disabled`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestEnsureRemovesVulnerableCoreSnap(c *C) {
	s.testEnsureRemovesVulnerableSnap(c, "core")
}

func (s *snapmgrTestSuite) TestEnsureRemovesVulnerableSnapdSnap(c *C) {
	s.testEnsureRemovesVulnerableSnap(c, "snapd")
}

func (s *snapmgrTestSuite) testEnsureRemovesVulnerableSnap(c *C, snapName string) {
	// make the currently installed snap info file fixed but an old version
	// vulnerable
	fixedInfoFile := `
VERSION=2.57.6+git1.g479e745-dirty
SNAPD_APPARMOR_REEXEC=1
`
	vulnInfoFile := `
VERSION=2.57.5+git1.g479e745-dirty
SNAPD_APPARMOR_REEXEC=1
`

	// revision 1 vulnerable
	infoFile := filepath.Join(dirs.SnapMountDir, snapName, "1", dirs.CoreLibExecDir, "info")
	err := os.MkdirAll(filepath.Dir(infoFile), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(infoFile, []byte(vulnInfoFile), 0644)
	c.Assert(err, IsNil)

	// revision 2 fixed
	infoFile2 := filepath.Join(dirs.SnapMountDir, snapName, "2", dirs.CoreLibExecDir, "info")
	err = os.MkdirAll(filepath.Dir(infoFile2), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(infoFile2, []byte(fixedInfoFile), 0644)
	c.Assert(err, IsNil)

	// revision 11 fixed
	infoFile11 := filepath.Join(dirs.SnapMountDir, snapName, "11", dirs.CoreLibExecDir, "info")
	err = os.MkdirAll(filepath.Dir(infoFile11), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(infoFile11, []byte(fixedInfoFile), 0644)
	c.Assert(err, IsNil)

	// use generic classic model
	r := snapstatetest.UseFallbackDeviceModel()
	defer r()

	st := s.state
	st.Lock()
	// ensure that only this specific snap is installed
	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "snapd", nil)

	snapSt := &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: snapName, Revision: snap.R(1)},
			{RealName: snapName, Revision: snap.R(2)},
			{RealName: snapName, Revision: snap.R(11)},
		}),
		Current:  snap.R(11),
		SnapType: "os",
	}
	if snapName == "snapd" {
		snapSt.SnapType = "snapd"
	}
	snapstate.Set(s.state, snapName, snapSt)
	st.Unlock()

	// special policy only on classic
	r = release.MockOnClassic(true)
	defer r()
	ensureErr := s.snapmgr.Ensure()
	c.Assert(ensureErr, IsNil)

	// we should have created a single remove change for revision 1, revision 2
	// should have been left alone
	st.Lock()
	defer st.Unlock()

	allChgs := st.Changes()
	c.Assert(allChgs, HasLen, 1)
	removeChg := allChgs[0]
	c.Assert(removeChg.Status(), Equals, state.DoStatus)
	c.Assert(removeChg.Kind(), Equals, "remove-snap")
	c.Assert(removeChg.Summary(), Equals, fmt.Sprintf(`Remove inactive vulnerable %q snap (1)`, snapName))

	c.Assert(removeChg.Tasks(), HasLen, 2)
	clearSnap := removeChg.Tasks()[0]
	discardSnap := removeChg.Tasks()[1]
	c.Assert(clearSnap.Kind(), Equals, "clear-snap")
	c.Assert(discardSnap.Kind(), Equals, "discard-snap")
	var snapsup snapstate.SnapSetup
	err = clearSnap.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup.SideInfo.Revision, Equals, snap.R(1))

	// and we set the appropriate key in the state
	var removeDone bool
	st.Get(snapName+"-snap-cve-2022-3328-vuln-removed", &removeDone)
	c.Assert(removeDone, Equals, true)
}

func (s *snapmgrTestSuite) TestEnsureChecksSnapdInfoFileOnClassicOnly(c *C) {
	// delete the core/snapd snap info files - they should always exist in real
	// devices, but deleting them here makes it so we can see the failure
	// trying to read the files easily

	infoFile := filepath.Join(dirs.SnapMountDir, "core", "1", dirs.CoreLibExecDir, "info")
	err := os.Remove(infoFile)
	c.Assert(err, IsNil)

	// special policy only on classic
	r := release.MockOnClassic(true)
	defer r()
	ensureErr := s.snapmgr.Ensure()
	c.Assert(ensureErr, ErrorMatches, "cannot open snapd info file.*")

	// if we are not on classic nothing happens
	r = release.MockOnClassic(false)
	defer r()

	ensureErr = s.snapmgr.Ensure()
	c.Assert(ensureErr, IsNil)
}

func (s *snapmgrTestSuite) TestEnsureSkipsCheckingSnapdSnapInfoFileWhenStateSet(c *C) {
	// we default from SetUp to having the core snap installed, remove it so we
	// only have the snapd snap available
	s.state.Lock()
	snapstate.Set(s.state, "core", nil)
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})
	s.state.Unlock()

	s.testEnsureSkipsCheckingSnapdInfoFileWhenStateSet(c, "snapd")
}

func (s *snapmgrTestSuite) TestEnsureSkipsCheckingCoreSnapInfoFileWhenStateSet(c *C) {
	s.testEnsureSkipsCheckingSnapdInfoFileWhenStateSet(c, "core")
}

func (s *snapmgrTestSuite) TestEnsureSkipsCheckingBothCoreAndSnapdSnapsInfoFileWhenStateSet(c *C) {
	// special policy only on classic
	r := release.MockOnClassic(true)
	defer r()

	st := s.state
	// also set snapd snapd as installed
	st.Lock()
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})
	st.Unlock()

	infoFileFor := func(snapName string) string {
		return filepath.Join(dirs.SnapMountDir, snapName, "1", dirs.CoreLibExecDir, "info")
	}

	// delete both snapd and core snap info files
	for _, snapName := range []string{"core", "snapd"} {
		err := os.Remove(infoFileFor(snapName))
		c.Assert(err, IsNil)
	}

	// make sure Ensure makes a whole hearted attempt to read both files - snapd
	// is tried first
	ensureErr := s.snapmgr.Ensure()
	c.Assert(ensureErr, ErrorMatches, fmt.Sprintf(`cannot open snapd info file "%s".*`, infoFileFor("snapd")))

	st.Lock()
	st.Set("snapd-snap-cve-2022-3328-vuln-removed", true)
	st.Unlock()

	// still unhappy about core file missing
	ensureErr = s.snapmgr.Ensure()
	c.Assert(ensureErr, ErrorMatches, fmt.Sprintf(`cannot open snapd info file "%s".*`, infoFileFor("core")))

	// but with core state flag set too, we are now happy
	st.Lock()
	st.Set("core-snap-cve-2022-3328-vuln-removed", true)
	st.Unlock()

	ensureErr = s.snapmgr.Ensure()
	c.Assert(ensureErr, IsNil)
}

func (s *snapmgrTestSuite) testEnsureSkipsCheckingSnapdInfoFileWhenStateSet(c *C, snapName string) {
	// special policy only on classic
	r := release.MockOnClassic(true)
	defer r()

	// delete the snap info file for this snap - they should always exist in
	// real devices, but deleting them here makes it so we can see the failure
	// trying to read the files easily
	infoFile := filepath.Join(dirs.SnapMountDir, snapName, "1", dirs.CoreLibExecDir, "info")
	err := os.Remove(infoFile)
	c.Assert(err, IsNil)

	// make sure it makes a whole hearted attempt to read it
	ensureErr := s.snapmgr.Ensure()
	c.Assert(ensureErr, ErrorMatches, "cannot open snapd info file.*")

	// now it should stop trying to check if state says so
	st := s.state
	st.Lock()
	st.Set(snapName+"-snap-cve-2022-3328-vuln-removed", true)
	st.Unlock()

	ensureErr = s.snapmgr.Ensure()
	c.Assert(ensureErr, IsNil)
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

func (s *snapmgrTestSuite) TestEnsureRefreshesAtSeedPolicyNopAtPreseed(c *C) {
	// special policy only on classic
	r := release.MockOnClassic(true)
	defer r()
	// set at not seeded yet
	st := s.state
	st.Lock()
	st.Set("seeded", nil)
	st.Unlock()
	// preseed time
	snapstate.SetPreseed(s.snapmgr, true)

	s.snapmgr.Ensure()

	st.Lock()
	defer st.Unlock()

	// check that refresh policies have *NOT* run in this case
	var t1 time.Time
	err := st.Get("last-refresh-hints", &t1)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
	tr := config.NewTransaction(st)
	err = tr.Get("core", "refresh.hold", &t1)
	c.Check(config.IsNoOption(err), Equals, true)
}

func (s *snapmgrTestSuite) TestEsnureCleansOldSideloads(c *C) {
	filenames := func() []string {
		filenames, _ := filepath.Glob(filepath.Join(dirs.SnapBlobDir, "*"))
		return filenames
	}

	// prevent removing snap file
	defer snapstate.MockEnsuredDownloadsCleaned(s.snapmgr, true)()

	defer snapstate.MockLocalInstallCleanupWait(200 * time.Millisecond)()
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0700), IsNil)
	// validity check; note * in go glob matches .foo
	c.Assert(filenames(), HasLen, 0)

	s0 := filepath.Join(dirs.SnapBlobDir, "some.snap")
	s1 := filepath.Join(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix+"-12345.snap")
	s2 := filepath.Join(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix+"-67890.snap")

	c.Assert(os.WriteFile(s0, nil, 0600), IsNil)
	c.Assert(os.WriteFile(s1, nil, 0600), IsNil)
	c.Assert(os.WriteFile(s2, nil, 0600), IsNil)

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

	now := t1
	restore := snapstate.MockTimeNow(func() time.Time {
		return now
	})
	defer restore()

	s.snapmgr.Ensure()
	// oldest sideload gone
	c.Assert(filenames(), DeepEquals, []string{s2, s0})

	now = t1.Add(200 * time.Millisecond)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
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
	checkIsContinuedAutoRefresh(c, chg.Tasks(), false)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
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
	s.settle(c)

	s.verifyRefreshLast(c)
}

func (s *snapmgrTestSuite) TestEnsureRefreshesInFlight(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }

	makeTestRefreshConfig(s.state)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
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

func (s *snapmgrTestSuite) TestFinishRestartBasics(c *C) {
	r := release.MockOnClassic(true)
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	// not restarting
	restart.MockPending(st, restart.RestartUnset)
	si := &snap.SideInfo{RealName: "some-app"}
	snaptest.MockSnap(c, "name: some-app\nversion: 1", si)
	snapsup := &snapstate.SnapSetup{SideInfo: si}
	err := snapstate.FinishRestart(task, snapsup)
	c.Check(err, IsNil)

	// restarting ... we always wait
	restart.MockPending(st, restart.RestartDaemon)
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, FitsTypeOf, &state.Retry{})
}

func (s *snapmgrTestSuite) TestFinishRestartNoopWhenPreseeding(c *C) {
	r := release.MockOnClassic(true)
	defer r()

	restorePreseeding := snapdenv.MockPreseeding(true)
	defer restorePreseeding()

	st := s.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	// not restarting
	si := &snap.SideInfo{RealName: "some-app"}
	snaptest.MockSnap(c, "name: some-app\nversion: 1", si)
	snapsup := &snapstate.SnapSetup{SideInfo: si}
	err := snapstate.FinishRestart(task, snapsup)
	c.Check(err, IsNil)

	restart.MockPending(st, restart.RestartDaemon)
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, IsNil)

	// verification: retry when not preseeding
	snapdenv.MockPreseeding(false)
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, FitsTypeOf, &state.Retry{})
}

func (s *snapmgrTestSuite) TestFinishRestartGeneratesSnapdWrappersOnCore(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	var generateWrappersCalled bool
	restore := snapstate.MockGenerateSnapdWrappers(func(snapInfo *snap.Info, opts *backend.GenerateSnapdWrappersOptions) (wrappers.SnapdRestart, error) {
		c.Assert(snapInfo.SnapName(), Equals, "snapd")
		c.Assert(opts, IsNil)
		generateWrappersCalled = true
		return nil, nil
	})
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	for i, tc := range []struct {
		onClassic            bool
		expectedWrappersCall bool
		snapName             string
		snapYaml             string
	}{
		{
			onClassic: false,
			snapName:  "snapd",
			snapYaml: `name: snapd
type: snapd
`,
			expectedWrappersCall: true,
		},
		{
			onClassic: true,
			snapName:  "snapd",
			snapYaml: `name: snapd
type: snapd
`,
			expectedWrappersCall: false,
		},
		{
			onClassic:            false,
			snapName:             "some-snap",
			snapYaml:             `name: some-snap`,
			expectedWrappersCall: false,
		},
	} {
		generateWrappersCalled = false
		release.MockOnClassic(tc.onClassic)

		task := st.NewTask("auto-connect", "...")
		si := &snap.SideInfo{Revision: snap.R("x2"), RealName: tc.snapName}
		snapInfo := snaptest.MockSnapCurrent(c, string(tc.snapYaml), si)
		snapsup := &snapstate.SnapSetup{SideInfo: si, Type: snapInfo.SnapType}
		task.Set("snap-setup", snapsup)

		// restarting
		restart.MockPending(st, restart.RestartUnset)
		c.Assert(snapstate.FinishRestart(task, snapsup), IsNil)
		c.Check(generateWrappersCalled, Equals, tc.expectedWrappersCall, Commentf("#%d: %v", i, tc))

		c.Assert(os.RemoveAll(filepath.Join(snap.BaseDir(snapInfo.SnapName()), "current")), IsNil)
	}
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo11, sideInfo12}),
		Current:  sideInfo12.Revision,
		SnapType: "app",
	})
	snapstate.Set(st, "name1_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{instanceSideInfo13}),
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
	c.Check(info.Website(), Equals, "")
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
	c.Check(info.Website(), Equals, storeInfo.Website)
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
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	_, err = snapstate.GadgetInfo(st, deviceCtx)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
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
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	_, err = snapstate.KernelInfo(st, deviceCtx)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	_, err := snapstate.BootBaseInfo(st, deviceCtxNoBootBase)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	// no boot-base in the state so ErrNoState
	_, err = snapstate.BootBaseInfo(st, deviceCtx)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	sideInfo := &snap.SideInfo{RealName: "core20", Revision: snap.R(4)}
	snaptest.MockSnap(c, `
name: core20
type: base
version: v20
`, sideInfo)
	snapstate.Set(st, "core20", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
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
				Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
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
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
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

	info11, err := snap.ReadInfo("name1", snapst.Sequence.Revisions[0].Snap)
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

	info13other, err := snap.ReadInfo("name1_instance", instance.Sequence.Revisions[0].Snap)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  snap.R(1),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si1)
}

func (s *snapStateSuite) TestCurrentSideInfoInOrder(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	si2 := &snap.SideInfo{Revision: snap.R(2)}
	snapst := snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1, si2}),
		Current:  snap.R(2),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si2)
}

func (s *snapStateSuite) TestCurrentSideInfoOutOfOrder(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	si2 := &snap.SideInfo{Revision: snap.R(2)}
	snapst := snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1, si2}),
		Current:  snap.R(1),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si1)
}

func (s *snapStateSuite) TestCurrentSideInfoInconsistent(c *C) {
	snapst := snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{Revision: snap.R(1)},
		}),
	}
	c.Check(func() { snapst.CurrentSideInfo() }, PanicMatches, `snapst.Current and snapst.Sequence.Revisions out of sync:.*`)
}

func (s *snapStateSuite) TestCurrentSideInfoInconsistentWithCurrent(c *C) {
	snapst := snapstate.SnapState{Current: snap.R(17)}
	c.Check(func() { snapst.CurrentSideInfo() }, PanicMatches, `cannot find snapst.Current in the snapst.Sequence.Revisions`)
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

func (s *snapmgrTestSuite) TestRefreshRetain(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	restore := release.MockOnClassic(true)
	defer restore()

	// default value for classic
	c.Assert(snapstate.RefreshRetain(st), Equals, 2)

	release.MockOnClassic(false)
	// default value for core
	c.Assert(snapstate.RefreshRetain(st), Equals, 3)

	buf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	for i, val := range []struct {
		input    interface{}
		expected int
		msg      string
	}{
		{1, 1, "^$"},
		{json.Number("2"), 2, "^$"},
		{"6", 6, "^$"},
		// invalid => default value for core
		{map[string]interface{}{"foo": "bar"}, 3, `.*internal error: refresh.retain system option has unexpected type: map\[string\]interface {}\n`},
	} {
		tr := config.NewTransaction(s.state)
		tr.Set("core", "refresh.retain", val.input)
		tr.Commit()
		c.Assert(snapstate.RefreshRetain(st), Equals, val.expected, Commentf("#%d", i))
		c.Assert(buf.String(), Matches, val.msg, Commentf("#%d", i))
		buf.Reset()
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si7, &si11}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si7}),
		Current:  si7.Revision,
	}
	c.Assert(snapst.LocalRevision().Unset(), Equals, true)
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
	err := os.WriteFile(filepath.Join(gadgetInfo.MountDir(), "meta/gadget.yaml"), []byte(gadgetYamlWhole), 0600)
	c.Assert(err, IsNil)

	snapstate.Set(s.state, "the-gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&gadgetInfo.SideInfo}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	tsl, err := snapstate.TransitionCore(s.state, "ubuntu-core", "core")
	c.Assert(err, IsNil)

	c.Assert(tsl, HasLen, 3)
	// 1. install core
	verifyInstallTasks(c, snap.TypeOS, runCoreConfigure, 0, tsl[0])
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}}),
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
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}}),
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

	s.settle(c)

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
			op:   "setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "core"),
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
			op:   "remove-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "ubuntu-core"),
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
			op:   "remove-snap-mount-units",
			name: "ubuntu-core",
		},
		{
			op:   "discard-namespace",
			name: "ubuntu-core",
		},
		{
			op:   "remove-inhibit-lock",
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
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "ubuntu-core-snap-id", Revision: snap.R(1)}}),
		Current:         snap.R(1),
		SnapType:        "os",
		TrackingChannel: "latest/stable",
	})
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1)}}),
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

	s.settle(c)

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
			op:   "remove-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "ubuntu-core"),
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
			op:   "remove-snap-mount-units",
			name: "ubuntu-core",
		},
		{
			op:   "discard-namespace",
			name: "ubuntu-core",
		},
		{
			op:   "remove-inhibit-lock",
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "core-snap-id", Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "core-snap-id", Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	s.settle(c)

	c.Check(s.state.Changes(), HasLen, 0)
	// not counted as a try
	var t time.Time
	err := s.state.Get("ubuntu-core-transition-last-retry-time", &t)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestTransitionCoreTimeLimitWorks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "core-snap-id", Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	// tried 3h ago, no retry
	s.state.Set("ubuntu-core-transition-last-retry-time", time.Now().Add(-3*time.Hour))

	s.settle(c)

	c.Check(s.state.Changes(), HasLen, 0)

	// tried 7h ago, retry
	s.state.Set("ubuntu-core-transition-last-retry-time", time.Now().Add(-7*time.Hour))

	s.settle(c)

	c.Check(s.state.Changes(), HasLen, 1)

	var t time.Time
	s.state.Get("ubuntu-core-transition-last-retry-time", &t)
	c.Assert(time.Since(t) < 2*time.Minute, Equals, true)
}

func (s *snapmgrTestSuite) TestTransitionCoreNoOtherChanges(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "ubuntu-core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "ubuntu-core", SnapID: "core-snap-id", Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	chg := s.state.NewChange("unrelated-change", "unfinished change blocks core transition")
	chg.SetStatus(state.DoStatus)

	s.settle(c)

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

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "foo", SnapID: "foo-id", Revision: snap.R(1), Channel: "beta"}}),
		Current:  snap.R(1),
	})

	s.settle(c)

	c.Check(s.state.Changes(), HasLen, 1)
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapDoesNotRunWhenNotEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1), Channel: "beta"}}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	s.settle(c)

	c.Check(s.state.Changes(), HasLen, 0)
}

func (s *snapmgrTestSuite) TestTransitionSnapdSnapStartsAutomaticallyWhenEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1), Channel: "beta"}}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	s.settle(c)

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

	// setup a classic model so the device context says we are on classic
	defer snapstatetest.MockDeviceModel(ClassicModel())()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1), Channel: "edge"}}),
		Current:  snap.R(1),
		SnapType: "os",
		// TrackingChannel
		TrackingChannel: "latest/beta",
	})
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.snapd-snap", true)
	tr.Commit()

	s.settle(c)

	c.Assert(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Assert(chg.Kind(), Equals, "transition-to-snapd-snap")
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(s.fakeStore.downloads, HasLen, 1)
	ts := state.NewTaskSet(chg.Tasks()...)
	// task set was reconstituted from change tasks, so edges information is
	// lost
	verifyInstallTasks(c, snap.TypeSnapd, noConfigure|noLastBeforeModificationsEdge, 0, ts)

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

	s.settle(c)

	c.Check(s.state.Changes(), HasLen, 0)

	// tried 7h ago, retry
	s.state.Set("snapd-transition-last-retry-time", time.Now().Add(-7*time.Hour))

	s.settle(c)

	c.Check(s.state.Changes(), HasLen, 1)

	var t time.Time
	s.state.Get("snapd-transition-last-retry-time", &t)
	c.Assert(time.Since(t) < 2*time.Minute, Equals, true)
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

	err := s.o.Settle(5 * time.Second)
	c.Assert(err, ErrorMatches, `state ensure errors: \[a grumpy store\]`)

	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 0)

	// all the attempts were recorded
	var t time.Time
	s.state.Get("snapd-transition-last-retry-time", &t)
	c.Assert(time.Since(t) < 2*time.Minute, Equals, true)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: name,
			SnapID:   "id-id-id",
			Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "os",
		Flags:    snapstate.Flags{DevMode: true},
	})

	var snapst1 snapstate.SnapState
	// validity check
	snapstate.Get(s.state, name, &snapst1)
	c.Assert(snapst1.DevMode, Equals, true)

	s.settle(c)

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

	s.state.Lock()
	defer s.state.Unlock()

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "core",
			SnapID:   "id-id-id",
			Revision: snap.R(1)}}),
		Current:  snap.R(1),
		SnapType: "os",
		Flags:    snapstate.Flags{DevMode: true},
	})

	var snapst1 snapstate.SnapState
	// validity check
	snapstate.Get(s.state, "core", &snapst1)
	c.Assert(snapst1.DevMode, Equals, true)

	s.settle(c)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

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
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
				{RealName: instanceName, Revision: snap.R(11)},
			}),
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

func (s *snapmgrTestSuite) TestConflictChangeId(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snaps := []string{"a", "b", "c"}
	changes := make([]*state.Change, len(snaps))

	for i, name := range snaps {
		snapstate.Set(s.state, name, &snapstate.SnapState{
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
				{RealName: name, Revision: snap.R(11)},
			}),
			Current: snap.R(11),
		})

		ts, err := snapstate.Enable(s.state, name)
		c.Assert(err, IsNil)

		changes[i] = s.state.NewChange("enable", "...")
		changes[i].AddAll(ts)
	}

	for i, name := range snaps {
		err := snapstate.CheckChangeConflictMany(s.state, []string{name}, "")
		c.Assert(err, FitsTypeOf, &snapstate.ChangeConflictError{})

		conflictErr := err.(*snapstate.ChangeConflictError)
		c.Assert(conflictErr.ChangeID, Equals, changes[i].ID())
	}
}

func (s *snapmgrTestSuite) TestConflictRemodeling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("remodel", "...")
	chg.SetStatus(state.DoingStatus)

	err := snapstate.CheckChangeConflictMany(s.state, []string{"a-snap"}, "")
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Check(err, ErrorMatches, `remodeling in progress, no other changes allowed until this is done`)

	// a remodel conflicts with another remodel
	err = snapstate.CheckChangeConflictRunExclusively(s.state, "remodel")
	c.Check(err, ErrorMatches, `remodeling in progress, no other changes allowed until this is done`)

	// we have a remodel change in state, a remodel change triggers are conflict
	err = snapstate.CheckChangeConflictRunExclusively(s.state, "create-recovery-system")
	c.Check(err, ErrorMatches, `remodeling in progress, no other changes allowed until this is done`)
}

func (s *snapmgrTestSuite) TestConflictCreateRecovery(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("create-recovery-system", "...")
	c.Check(chg.IsReady(), Equals, false)
	chg.SetStatus(state.DoingStatus)

	err := snapstate.CheckChangeConflictMany(s.state, []string{"a-snap"}, "")
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Check(err, ErrorMatches, `creating recovery system in progress, no other changes allowed until this is done`)

	// remodeling conflicts with a change that creates a recovery system
	err = snapstate.CheckChangeConflictRunExclusively(s.state, "remodel")
	c.Check(err, ErrorMatches, `creating recovery system in progress, no other changes allowed until this is done`)

	// so does another another create recovery system change
	err = snapstate.CheckChangeConflictRunExclusively(s.state, "create-recovery-system")
	c.Check(err, ErrorMatches, `creating recovery system in progress, no other changes allowed until this is done`)
}

func (s *snapmgrTestSuite) TestConflictExclusive(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install-snap-a", "...")
	chg.SetStatus(state.DoingStatus)

	// a remodel conflicts with any other change
	err := snapstate.CheckChangeConflictRunExclusively(s.state, "remodel")
	c.Check(err, ErrorMatches, `other changes in progress \(conflicting change "install-snap-a"\), change "remodel" not allowed until they are done`)
	c.Assert(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	conflictErr := err.(*snapstate.ChangeConflictError)
	c.Assert(conflictErr.ChangeID, Equals, chg.ID())

	// and so does the  remodel conflicts with any other change
	err = snapstate.CheckChangeConflictRunExclusively(s.state, "create-recovery-system")
	c.Check(err, ErrorMatches, `other changes in progress \(conflicting change "install-snap-a"\), change "create-recovery-system" not allowed until they are done`)
	c.Assert(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	conflictErr = err.(*snapstate.ChangeConflictError)
	c.Assert(conflictErr.ChangeID, Equals, chg.ID())
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
	if len(sars) < 1 || len(sars) > 2 {
		panic("expected to be queried for install of 1 or 2 snaps at a time")
	}

	var res []store.SnapActionResult
	for _, s := range sars {
		info := s.Info
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
		res = append(res, store.SnapActionResult{Info: info})
	}
	return res, nil, err
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
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)}}),
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
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
		}),
		Current:     snap.R(3),
		SnapType:    "app",
		InstanceKey: "instance",
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
		SnapType: "app",
	})
	snapstate.Set(s.state, "other-snap_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
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

	s.settle(c)

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
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	_, err = snapstate.GadgetConnections(s.state, deviceCtx)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		}),
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
		ch, err := snapstate.ResolveChannel(tc.snap, tc.cur, tc.new, deviceCtx)
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
	verifyInstallTasks(c, snap.TypeGadget, 0, 0, ts)
}

func (s *snapmgrTestSuite) TestGadgetUpdateTaskAddedOnRefresh(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "brand-gadget", SnapID: "brand-gadget-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "gadget",
	})

	// and on update
	ts, err := snapstate.Update(s.state, "brand-gadget", &snapstate.RevisionOptions{}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	verifyUpdateTasks(c, snap.TypeGadget, doesReRefresh, 0, ts)

}

func (s *snapmgrTestSuite) TestGadgetUpdateTaskAddedOnKernelRefresh(c *C) {
	s.testGadgetUpdateTaskAddedOnUCKernelRefresh(c, DefaultModel(), doesReRefresh)
}

func (s *snapmgrTestSuite) TestGadgetUpdateTaskAddedOnUC20KernelRefresh(c *C) {
	s.testGadgetUpdateTaskAddedOnUCKernelRefresh(c, MakeModel20("brand-gadget", nil), doesReRefresh)
}

func (s *snapmgrTestSuite) TestGadgetUpdateTaskAddedOnUC24KernelRefresh(c *C) {
	s.testGadgetUpdateTaskAddedOnUCKernelRefresh(c,
		MakeModel20("brand-gadget", map[string]interface{}{"base": "core24"}),
		doesReRefresh|needsKernelSetup)
}

func (s *snapmgrTestSuite) testGadgetUpdateTaskAddedOnUCKernelRefresh(c *C, model *asserts.Model, opts int) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	defer snapstatetest.MockDeviceModel(model)()

	snapstate.Set(s.state, "brand-kernel", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "brand-kernel", SnapID: "brand-kernel-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "kernel",
	})

	// and on update
	ts, err := snapstate.Update(s.state, "brand-kernel", &snapstate.RevisionOptions{}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	verifyUpdateTasks(c, snap.TypeKernel, opts, 0, ts)
}

func (s *snapmgrTestSuite) TestGadgetUpdateTaskAddedOnUCKernelRefreshHybrid(c *C) {
	s.testGadgetUpdateTaskAddedOnUCKernelRefreshHybrid(c, "core24",
		doesReRefresh|needsKernelSetup|isHybrid)
}

func (s *snapmgrTestSuite) TestGadgetUpdateTaskAddedOnUCKernelRefreshHybridOldBase(c *C) {
	s.testGadgetUpdateTaskAddedOnUCKernelRefreshHybrid(c, "core22",
		doesReRefresh|isHybrid)
}

func (s *snapmgrTestSuite) testGadgetUpdateTaskAddedOnUCKernelRefreshHybrid(c *C, base string, opts int) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	defer snapstatetest.MockDeviceModel(MakeModelClassicWithModes(
		"brand-gadget", map[string]interface{}{"base": base}))()

	snapstate.Set(s.state, "brand-kernel", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "brand-kernel", SnapID: "brand-kernel-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "kernel",
	})

	// and on update
	ts, err := snapstate.Update(s.state, "brand-kernel",
		&snapstate.RevisionOptions{}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	verifyUpdateTasks(c, snap.TypeKernel, opts, 0, ts)
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

const servicesSnap = `name: hello-snap
version: 1
apps:
 hello:
   command: bin/hello
 svc1:
  command: bin/hello
  daemon: forking
  before: [svc2]
 svc2:
  command: bin/hello
  daemon: forking
  after: [svc1]
`

func (s *snapmgrTestSuite) runStartSnapServicesWithDisabledServices(c *C, disabled ...string) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "hello-snap", SnapID: "hello-snap-id", Revision: snap.R(1)}
	snaptest.MockSnap(c, servicesSnap, si)

	snapstate.Set(s.state, "hello-snap", &snapstate.SnapState{
		Active:                     true,
		Sequence:                   snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:                    si.Revision,
		SnapType:                   "app",
		LastActiveDisabledServices: disabled,
	})

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	chg := s.state.NewChange("services..", "")
	t := s.state.NewTask("start-snap-services", "")
	sup := &snapstate.SnapSetup{SideInfo: si}
	t.Set("snap-setup", sup)
	chg.AddTask(t)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)

	expected := fakeOps{
		{
			op:       "start-snap-services",
			path:     filepath.Join(dirs.SnapMountDir, "hello-snap/1"),
			services: []string{"svc1", "svc2"},
		},
	}
	c.Check(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestStartSnapServicesWithDisabledServicesNowApp(c *C) {
	// mock the logger
	buf, loggerRestore := logger.MockLogger()
	defer loggerRestore()

	s.runStartSnapServicesWithDisabledServices(c, "hello")

	// check the log for the notice
	c.Assert(buf.String(), Matches, `(?s).*previously disabled service hello is now an app and not a service\n.*`)
}

func (s *snapmgrTestSuite) TestStartSnapServicesWithDisabledServicesMissing(c *C) {
	// mock the logger
	buf, loggerRestore := logger.MockLogger()
	defer loggerRestore()

	s.runStartSnapServicesWithDisabledServices(c, "old-disabled-svc")

	// check the log for the notice
	c.Assert(buf.String(), Matches, `(?s).*previously disabled service old-disabled-svc no longer exists\n.*`)
}

func (s *snapmgrTestSuite) TestStartSnapServicesUndo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "hello-snap", SnapID: "hello-snap-id", Revision: snap.R(1)}
	snaptest.MockSnap(c, servicesSnap, si)

	snapstate.Set(s.state, "hello-snap", &snapstate.SnapState{
		Active:                     true,
		Sequence:                   snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:                    si.Revision,
		SnapType:                   "app",
		LastActiveDisabledServices: []string{"old-svc"},
	})

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	chg := s.state.NewChange("services..", "")
	t := s.state.NewTask("start-snap-services", "")
	sup := &snapstate.SnapSetup{SideInfo: si}
	t.Set("snap-setup", sup)
	chg.AddTask(t)
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	terr.JoinLane(t.Lanes()[0])
	chg.AddTask(terr)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(t.Status(), Equals, state.UndoneStatus)

	expected := fakeOps{
		{
			op:       "start-snap-services",
			path:     filepath.Join(dirs.SnapMountDir, "hello-snap/1"),
			services: []string{"svc1", "svc2"},
		},
		{
			op:   "stop-snap-services:",
			path: filepath.Join(dirs.SnapMountDir, "hello-snap/1"),
		},
	}
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	var oldDisabledSvcs []string
	c.Check(t.Get("old-last-active-disabled-services", &oldDisabledSvcs), IsNil)
	c.Check(oldDisabledSvcs, DeepEquals, []string{"old-svc"})
}

func (s *snapmgrTestSuite) TestStopSnapServicesUndo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	si := &snap.SideInfo{RealName: "hello-snap", SnapID: "hello-snap-id", Revision: snap.R(1)}
	snaptest.MockSnap(c, servicesSnap, si)

	snapstate.Set(s.state, "hello-snap", &snapstate.SnapState{
		Active:                     true,
		Sequence:                   snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:                    si.Revision,
		SnapType:                   "app",
		LastActiveDisabledServices: []string{"old-svc"},
	})

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	chg := s.state.NewChange("services..", "")
	t := s.state.NewTask("stop-snap-services", "")
	sup := &snapstate.SnapSetup{SideInfo: si}
	t.Set("snap-setup", sup)
	chg.AddTask(t)
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	terr.JoinLane(t.Lanes()[0])
	chg.AddTask(terr)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(t.Status(), Equals, state.UndoneStatus)

	expected := fakeOps{
		{
			op:   "stop-snap-services:",
			path: filepath.Join(dirs.SnapMountDir, "hello-snap/1"),
		},
		{
			op:               "current-snap-service-states",
			disabledServices: []string{"svc1"},
		},
		{
			op:               "start-snap-services",
			services:         []string{"svc1", "svc2"},
			disabledServices: []string{"svc1"},
			path:             filepath.Join(dirs.SnapMountDir, "hello-snap/1"),
		},
	}
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	var oldDisabledSvcs []string
	c.Check(t.Get("old-last-active-disabled-services", &oldDisabledSvcs), IsNil)
	c.Check(oldDisabledSvcs, DeepEquals, []string{"old-svc"})

	var disabled wrappers.DisabledServices
	c.Check(t.Get("disabled-services", &disabled), IsNil)
	c.Check(disabled, DeepEquals, wrappers.DisabledServices{
		SystemServices: []string{"svc1"},
	})
}

func (s *snapmgrTestSuite) TestStopSnapServicesErrInUndo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "hello-snap", SnapID: "hello-snap-id", Revision: snap.R(1)}
	snaptest.MockSnap(c, servicesSnap, si)

	snapstate.Set(s.state, "hello-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})

	// using MockSnap, we want to read the bits on disk
	snapstate.MockSnapReadInfo(snap.ReadInfo)

	chg := s.state.NewChange("services..", "")
	t := s.state.NewTask("stop-snap-services", "")
	sup := &snapstate.SnapSetup{SideInfo: si}
	t.Set("snap-setup", sup)
	chg.AddTask(t)
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	terr.JoinLane(t.Lanes()[0])
	chg.AddTask(terr)

	s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
		if op.op == "start-snap-services" {
			return fmt.Errorf("start-snap-services mock error")
		}
		return nil
	}

	s.settle(c)

	c.Assert(chg.IsReady(), Equals, true)
	c.Assert(chg.Err(), ErrorMatches, `(?s)cannot perform the following tasks:.*- +\(start-snap-services mock error\).*`)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(t.Status(), Equals, state.ErrorStatus)

	expected := fakeOps{
		{
			op:   "stop-snap-services:",
			path: filepath.Join(dirs.SnapMountDir, "hello-snap/1"),
		},
		{
			op: "current-snap-service-states",
		},
		{
			// failed after this op
			op:       "start-snap-services",
			services: []string{"svc1", "svc2"},
			path:     filepath.Join(dirs.SnapMountDir, "hello-snap/1"),
		},
	}
	c.Check(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestEnsureAutoRefreshesAreDelayed(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t0 := time.Now()
	// with no changes in flight still works and we set the auto-refresh time as
	// at least one minute past the start of the test
	chgs, err := s.snapmgr.EnsureAutoRefreshesAreDelayed(time.Minute)
	c.Assert(err, IsNil)
	c.Assert(chgs, HasLen, 0)

	var holdTime time.Time
	tr := config.NewTransaction(s.state)
	err = tr.Get("core", "refresh.hold", &holdTime)
	c.Assert(err, IsNil)
	// use After() == false in case holdTime is _exactly_ one minute later than
	// t0, in which case both After() and Before() will be false
	c.Assert(t0.Add(time.Minute).After(holdTime), Equals, false)

	// now make some auto-refresh changes to make sure we get those figured out
	chg0 := s.state.NewChange("auto-refresh", "auto-refresh-the-things")
	chg0.AddTask(s.state.NewTask("nop", "do nothing"))

	// make it in doing state
	chg0.SetStatus(state.DoingStatus)

	// this one will be picked up too
	chg1 := s.state.NewChange("auto-refresh", "auto-refresh-the-things")
	chg1.AddTask(s.state.NewTask("nop", "do nothing"))
	chg1.SetStatus(state.DoStatus)

	// this one won't, it's Done
	chg2 := s.state.NewChange("auto-refresh", "auto-refresh-the-things")
	chg2.AddTask(s.state.NewTask("nop", "do nothing"))
	chg2.SetStatus(state.DoneStatus)

	// nor this one, it's Undone
	chg3 := s.state.NewChange("auto-refresh", "auto-refresh-the-things")
	chg3.AddTask(s.state.NewTask("nop", "do nothing"))
	chg3.SetStatus(state.UndoneStatus)

	// now we get our change ID returned when calling EnsureAutoRefreshesAreDelayed
	chgs, err = s.snapmgr.EnsureAutoRefreshesAreDelayed(time.Minute)
	c.Assert(err, IsNil)
	// more helpful error message if we first compare the change ID's
	expids := []string{chg0.ID(), chg1.ID()}
	sort.Strings(expids)
	c.Assert(chgs, HasLen, len(expids))
	gotids := []string{chgs[0].ID(), chgs[1].ID()}
	sort.Strings(gotids)
	c.Assert(expids, DeepEquals, gotids)

	sort.SliceStable(chgs, func(i, j int) bool {
		return chgs[i].ID() < chgs[j].ID()
	})

	c.Assert(chgs, DeepEquals, []*state.Change{chg0, chg1})
}

func (s *snapmgrTestSuite) TestInstallModeDisableFreshInstall(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	oldServicesSnapYaml := servicesSnapYaml
	servicesSnapYaml += `
  svcInstallModeDisable:
    daemon: simple
    install-mode: disable
`
	defer func() { servicesSnapYaml = oldServicesSnapYaml }()

	installChg := s.state.NewChange("install", "...")
	installTs, err := snapstate.Install(context.Background(), s.state, "services-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	installChg.AddAll(installTs)

	s.settle(c)

	c.Assert(installChg.Err(), IsNil)
	c.Assert(installChg.IsReady(), Equals, true)

	op := s.fakeBackend.ops.First("start-snap-services")
	c.Assert(op, Not(IsNil))
	c.Check(op.disabledServices, DeepEquals, []string{"svcInstallModeDisable"})
}

func (s *snapmgrTestSuite) TestInstallModeDisableUpdateServiceNotDisabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	oldServicesSnapYaml := servicesSnapYaml
	servicesSnapYaml += `
  svcInstallModeDisable:
    daemon: simple
    install-mode: disable
`
	defer func() { servicesSnapYaml = oldServicesSnapYaml }()

	// pretent services-snap is installed and no service is disabled in
	// this install (i.e. svcInstallModeDisable is active)
	si := &snap.SideInfo{
		RealName: "services-snap", SnapID: "services-snap-id", Revision: snap.R(7),
	}
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	})
	snaptest.MockSnap(c, string(servicesSnapYaml), si)

	updateChg := s.state.NewChange("refresh", "...")
	updateTs, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	updateChg.AddAll(updateTs)

	s.settle(c)

	c.Assert(updateChg.Err(), IsNil)
	c.Assert(updateChg.IsReady(), Equals, true)

	op := s.fakeBackend.ops.First("start-snap-services")
	c.Assert(op, Not(IsNil))
	c.Check(op.disabledServices, HasLen, 0)
}

func (s *snapmgrTestSuite) TestInstallModeDisableFreshInstallEnabledByHook(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	oldServicesSnapYaml := servicesSnapYaml
	servicesSnapYaml += `
  svcInstallModeDisable:
    daemon: simple
    install-mode: disable
`
	defer func() { servicesSnapYaml = oldServicesSnapYaml }()

	// XXX: should this become part of managers_test.go ?
	// pretent we have a hook that enables the service on install
	runner := s.o.TaskRunner()
	runner.AddHandler("run-hook", func(t *state.Task, _ *tomb.Tomb) error {
		var snapst snapstate.SnapState
		st.Lock()
		err := snapstate.Get(st, "services-snap", &snapst)
		st.Unlock()
		c.Assert(err, IsNil)
		snapst.ServicesEnabledByHooks = []string{"svcInstallModeDisable"}
		st.Lock()
		snapstate.Set(st, "services-snap", &snapst)
		st.Unlock()
		return nil
	}, nil)

	installChg := s.state.NewChange("install", "...")
	installTs, err := snapstate.Install(context.Background(), s.state, "services-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	installChg.AddAll(installTs)

	s.settle(c)

	c.Assert(installChg.Err(), IsNil)
	c.Assert(installChg.IsReady(), Equals, true)

	op := s.fakeBackend.ops.First("start-snap-services")
	c.Assert(op, Not(IsNil))
	c.Check(op.disabledServices, HasLen, 0)
}

func (s *snapmgrTestSuite) TestSnapdRefreshTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	// setup a classic model so the device context says we are on classic
	defer snapstatetest.MockDeviceModel(ClassicModel())()

	chg := s.state.NewChange("snapd-refresh", "refresh snapd")
	ts, err := snapstate.Update(s.state, "snapd", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	// various backend operations, but no unlink-current-snap
	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{
				{
					InstanceName:  "snapd",
					SnapID:        "snapd-snap-id",
					Revision:      snap.R(1),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1),
					Epoch:         snap.E("1*"),
				},
			},
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				SnapID:       "snapd-snap-id",
				InstanceName: "snapd",
				Flags:        store.SnapActionEnforceValidation,
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "snapd",
		},
		{
			op:    "validate-snap:Doing",
			name:  "snapd",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "snapd/1"),
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "snapd_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "snapd",
				SnapID:   "snapd-snap-id",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "snapd",
			path:  filepath.Join(dirs.SnapBlobDir, "snapd_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "remove-snap-aliases",
			name: "snapd",
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "snapd/11"),
			old:  filepath.Join(dirs.SnapMountDir, "snapd/1"),
		},
		{
			op:   "setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "snapd"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "snapd",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "snapd",
				SnapID:   "snapd-snap-id",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "snapd/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "snapd",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  "snapd",
			revno: snap.R(11),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify that the R(2) version is active now and R(7) is still there
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "snapd", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(11))
}

type installTestType struct {
	t snap.Type
}

func (t *installTestType) InstanceName() string {
	panic("not expected")
}

func (t *installTestType) Type() snap.Type {
	return t.t
}

func (t *installTestType) SnapBase() string {
	panic("not expected")
}

func (t *installTestType) DownloadSize() int64 {
	panic("not expected")
}

func (t *installTestType) Prereq(st *state.State, prqt snapstate.PrereqTracker) []string {
	panic("not expected")
}

func (s *snapmgrTestSuite) TestMinimalInstallInfoSortByType(c *C) {
	snaps := []snapstate.MinimalInstallInfo{
		&installTestType{snap.TypeApp},
		&installTestType{snap.TypeBase},
		&installTestType{snap.TypeApp},
		&installTestType{snap.TypeSnapd},
		&installTestType{snap.TypeKernel},
		&installTestType{snap.TypeGadget},
	}

	sort.Sort(snapstate.ByType(snaps))
	c.Check(snaps, DeepEquals, []snapstate.MinimalInstallInfo{
		&installTestType{snap.TypeSnapd},
		&installTestType{snap.TypeKernel},
		&installTestType{snap.TypeBase},
		&installTestType{snap.TypeGadget},
		&installTestType{snap.TypeApp},
		&installTestType{snap.TypeApp}})
}

func (s *snapmgrTestSuite) TestInstalledSnaps(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snaps, ignoreValidation, err := snapstate.InstalledSnaps(st)
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 0)
	c.Check(ignoreValidation, HasLen, 0)

	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "foo", Revision: snap.R(23), SnapID: "foo-id"}}),
		Current:  snap.R(23),
	})
	snaptest.MockSnap(c, string(`name: foo
version: 1`), &snap.SideInfo{Revision: snap.R("13")})

	snapstate.Set(st, "bar", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "bar", Revision: snap.R(5), SnapID: "bar-id"}}),
		Current:  snap.R(5),
		Flags:    snapstate.Flags{IgnoreValidation: true},
	})
	snaptest.MockSnap(c, string(`name: bar
version: 1`), &snap.SideInfo{Revision: snap.R("5")})

	snaps, ignoreValidation, err = snapstate.InstalledSnaps(st)
	c.Assert(err, IsNil)
	c.Check(snaps, testutil.DeepUnsortedMatches, []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "foo-id", snap.R("23")),
		snapasserts.NewInstalledSnap("bar", "bar-id", snap.R("5"))})

	c.Check(ignoreValidation, DeepEquals, map[string]bool{"bar": true})
}

func (s *snapmgrTestSuite) addSnapsForRemodel(c *C) {
	si := &snap.SideInfo{
		RealName: "some-base", Revision: snap.R(1),
	}
	snaptest.MockSnapCurrent(c, "name: some-base\nversion: 1.0\ntype: base\n", si)
	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "base",
	})

	si = &snap.SideInfo{
		RealName: "some-kernel", Revision: snap.R(2),
	}
	snaptest.MockSnapCurrent(c, "name: some-kernel\nversion: 1.0\ntype: kernel\n", si)
	snapstate.Set(s.state, "some-kernel", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "kernel",
	})
	si = &snap.SideInfo{
		RealName: "some-gadget", Revision: snap.R(3),
	}
	snaptest.MockSnapCurrent(c, "name: some-gadget\nversion: 1.0\ntype: gadget\n", si)
	snapstate.Set(s.state, "some-gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "gadget",
	})
}

var nonReLinkKinds = []string{
	"copy-snap-data",
	"setup-profiles",
	"auto-connect",
	"set-auto-aliases",
	"setup-aliases",
	"run-hook[install]",
	"run-hook[default-configure]",
	"start-snap-services",
	"run-hook[configure]",
	"run-hook[check-health]",
	"discard-old-kernel-snap-setup",
}

func kindsToSet(kinds []string) map[string]bool {
	s := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		s[k] = true
	}
	return s
}

func (s *snapmgrTestSuite) TestRemodelLinkNewBaseOrKernelHappy(c *C) {
	s.testRemodelLinkNewBaseOrKernelHappy(c, DefaultModel(), 0)
}

func (s *snapmgrTestSuite) TestRemodelLinkNewBaseOrUC20KernelHappy(c *C) {
	s.testRemodelLinkNewBaseOrKernelHappy(c, MakeModel20("brand-gadget", nil), 0)
}

func (s *snapmgrTestSuite) TestRemodelLinkNewBaseOrUC24KernelHappy(c *C) {
	// UC24 model has additional tasks for the kernel
	s.testRemodelLinkNewBaseOrKernelHappy(c,
		MakeModel20("brand-gadget", map[string]interface{}{"base": "core24"}),
		needsKernelSetup)
}

func (s *snapmgrTestSuite) testRemodelLinkNewBaseOrKernelHappy(c *C, model *asserts.Model, opts int) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()

	defer snapstatetest.MockDeviceModel(model)()

	s.addSnapsForRemodel(c)

	ts, err := snapstate.LinkNewBaseOrKernel(s.state, "some-kernel", "")
	c.Assert(err, IsNil)
	tasks := ts.Tasks()
	c.Check(taskKinds(tasks), DeepEquals, expectedDoInstallTasks(snap.TypeKernel, opts, 0, []string{"prepare-snap"}, kindsToSet(nonReLinkKinds)))
	tPrepare := tasks[0]
	var tLink, tUpdateGadgetAssets *state.Task
	if opts&needsKernelSetup != 0 {
		c.Assert(tasks, HasLen, 4)
		tSetupKernelSnap := tasks[1]
		c.Assert(tSetupKernelSnap.Kind(), Equals, "prepare-kernel-snap")
		c.Assert(tSetupKernelSnap.Summary(), Equals, `Prepare kernel driver tree for "some-kernel" (2) for remodel`)
		c.Assert(tSetupKernelSnap.WaitTasks(), DeepEquals, []*state.Task{tPrepare})
		tUpdateGadgetAssets = tasks[2]
		tLink = tasks[3]
	} else {
		c.Assert(tasks, HasLen, 3)
		tUpdateGadgetAssets = tasks[1]
		tLink = tasks[2]
	}
	c.Assert(tPrepare.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepare.Summary(), Equals, `Prepare snap "some-kernel" (2) for remodel`)
	c.Assert(tPrepare.Has("snap-setup"), Equals, true)
	c.Assert(tUpdateGadgetAssets.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateGadgetAssets.Summary(), Equals, `Update assets from kernel "some-kernel" (2) for remodel`)
	c.Assert(tLink.Kind(), Equals, "link-snap")
	c.Assert(tLink.Summary(), Equals, `Make snap "some-kernel" (2) available to the system during remodel`)
	c.Assert(tLink.WaitTasks(), DeepEquals, []*state.Task{tUpdateGadgetAssets})

	ts, err = snapstate.LinkNewBaseOrKernel(s.state, "some-base", "")
	c.Assert(err, IsNil)
	tasks = ts.Tasks()
	c.Check(taskKinds(tasks), DeepEquals, expectedDoInstallTasks(snap.TypeBase, 0, 0, []string{"prepare-snap"}, kindsToSet(nonReLinkKinds)))
	c.Assert(tasks, HasLen, 2)
	tPrepare = tasks[0]
	tLink = tasks[1]
	c.Assert(tPrepare.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepare.Summary(), Equals, `Prepare snap "some-base" (1) for remodel`)
	c.Assert(tPrepare.Has("snap-setup"), Equals, true)
	c.Assert(tLink.Kind(), Equals, "link-snap")
	c.Assert(tLink.Summary(), Equals, `Make snap "some-base" (1) available to the system during remodel`)
}

func (s *snapmgrTestSuite) TestRemodelLinkNewBaseOrKernelBadType(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()
	s.addSnapsForRemodel(c)

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(3)}
	snaptest.MockSnapCurrent(c, "name: snap-gadget\nversion: 1.0\n", si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})
	ts, err := snapstate.LinkNewBaseOrKernel(s.state, "some-snap", "")
	c.Assert(err, ErrorMatches, `internal error: cannot link type app`)
	c.Assert(ts, IsNil)

	ts, err = snapstate.LinkNewBaseOrKernel(s.state, "some-gadget", "")
	c.Assert(err, ErrorMatches, `internal error: cannot link type gadget`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRemodelLinkNewBaseOrKernelNoRemodelConflict(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()
	s.addSnapsForRemodel(c)

	tugc := s.state.NewTask("update-managed-boot-config", "update managed boot config")
	chg := s.state.NewChange("remodel", "remodel")
	chg.AddTask(tugc)

	_, err := snapstate.LinkNewBaseOrKernel(s.state, "some-base", chg.ID())
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemodelAddLinkNewBaseOrKernel(c *C) {
	s.testRemodelAddLinkNewBaseOrKernel(c, DefaultModel(), 0)
}

func (s *snapmgrTestSuite) TestRemodelAddLinkNewBaseOrUC20Kernel(c *C) {
	s.testRemodelAddLinkNewBaseOrKernel(c, MakeModel20("brand-gadget", nil), 0)
}

func (s *snapmgrTestSuite) TestRemodelAddLinkNewBaseOrUC24Kernel(c *C) {
	// UC24 model has additional tasks for the kernel
	s.testRemodelAddLinkNewBaseOrKernel(c,
		MakeModel20("brand-gadget", map[string]interface{}{"base": "core24"}),
		needsKernelSetup)
}

func (s *snapmgrTestSuite) testRemodelAddLinkNewBaseOrKernel(c *C, model *asserts.Model, opts int) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()

	defer snapstatetest.MockDeviceModel(model)()

	// try a kernel snap first
	si := &snap.SideInfo{RealName: "some-kernel", Revision: snap.R(2)}
	tPrepare := s.state.NewTask("prepare-snap", "test task")
	snapsup := &snapstate.SnapSetup{
		SideInfo: si,
		Type:     "kernel",
	}
	tPrepare.Set("snap-setup", snapsup)
	testTask := s.state.NewTask("test-task", "test task")
	ts := state.NewTaskSet(tPrepare, testTask)

	tsNew, err := snapstate.AddLinkNewBaseOrKernel(s.state, ts)
	c.Assert(err, IsNil)
	c.Assert(tsNew, NotNil)
	tasks := tsNew.Tasks()
	c.Check(taskKinds(tasks), DeepEquals, expectedDoInstallTasks(snap.TypeKernel, opts, 0, []string{"prepare-snap", "test-task"}, kindsToSet(nonReLinkKinds)))
	// since this is the kernel, we have our task + test task + update-gadget-assets + link-snap
	var tLink, tUpdateGadgetAssets *state.Task
	if opts&needsKernelSetup != 0 {
		c.Assert(tasks, HasLen, 5)
		tSetupKernelSnap := tasks[2]
		c.Assert(tSetupKernelSnap.Kind(), Equals, "prepare-kernel-snap")
		c.Assert(tSetupKernelSnap.Summary(), Equals, `Prepare kernel driver tree for "some-kernel" (2) for remodel`)
		c.Assert(tSetupKernelSnap.WaitTasks(), DeepEquals, []*state.Task{
			testTask,
		})
		tUpdateGadgetAssets = tasks[3]
		tLink = tasks[4]
	} else {
		c.Assert(tasks, HasLen, 4)
		tUpdateGadgetAssets = tasks[2]
		tLink = tasks[3]
	}
	c.Assert(tUpdateGadgetAssets.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateGadgetAssets.Summary(), Equals, `Update assets from kernel "some-kernel" (2) for remodel`)
	c.Assert(tLink.Kind(), Equals, "link-snap")
	c.Assert(tLink.Summary(), Equals, `Make snap "some-kernel" (2) available to the system during remodel`)
	c.Assert(tLink.WaitTasks(), DeepEquals, []*state.Task{
		// waits for last task in the set
		tUpdateGadgetAssets,
	})
	for _, tsk := range []*state.Task{tLink, tUpdateGadgetAssets} {
		var ssID string
		c.Assert(tsk.Get("snap-setup-task", &ssID), IsNil)
		c.Assert(ssID, Equals, tPrepare.ID())
	}

	// try with base snap
	si = &snap.SideInfo{RealName: "some-base", Revision: snap.R(1)}
	tPrepare = s.state.NewTask("prepare-snap", "test task")
	tPrepare.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     "base",
	})
	ts = state.NewTaskSet(tPrepare)
	tsNew, err = snapstate.AddLinkNewBaseOrKernel(s.state, ts)
	c.Assert(err, IsNil)
	c.Assert(tsNew, NotNil)
	tasks = tsNew.Tasks()
	c.Check(taskKinds(tasks), DeepEquals, expectedDoInstallTasks(snap.TypeBase, 0, 0, []string{"prepare-snap"}, kindsToSet(nonReLinkKinds)))
	// since this is the base, we have our task + link-snap only
	c.Assert(tasks, HasLen, 2)
	tLink = tasks[1]
	c.Assert(tLink.Kind(), Equals, "link-snap")
	c.Assert(tLink.Summary(), Equals, `Make snap "some-base" (1) available to the system during remodel`)
	var ssID string
	c.Assert(tLink.Get("snap-setup-task", &ssID), IsNil)
	c.Assert(ssID, Equals, tPrepare.ID())

	// but bails when there is no task with snap setup
	ts = state.NewTaskSet()
	tsNew, err = snapstate.AddLinkNewBaseOrKernel(s.state, ts)
	c.Assert(err, ErrorMatches, `internal error: cannot identify task with snap-setup`)
	c.Assert(tsNew, IsNil)
}

func (s *snapmgrTestSuite) TestRemodelSwitchNewGadget(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()
	s.addSnapsForRemodel(c)

	ts, err := snapstate.SwitchToNewGadget(s.state, "some-gadget", "")
	c.Assert(err, IsNil)
	tasks := ts.Tasks()
	c.Check(taskKinds(tasks), DeepEquals, expectedDoInstallTasks(snap.TypeGadget, 0, 0, []string{"prepare-snap"}, kindsToSet(append(nonReLinkKinds, "link-snap"))))
	c.Assert(tasks, HasLen, 3)
	tPrepare := tasks[0]
	tUpdateGadgetAssets := tasks[1]
	tUpdateGadgetCmdline := tasks[2]
	c.Assert(tPrepare.Kind(), Equals, "prepare-snap")
	c.Assert(tPrepare.Summary(), Equals, `Prepare snap "some-gadget" (3) for remodel`)
	c.Assert(tPrepare.Has("snap-setup"), Equals, true)
	c.Assert(tUpdateGadgetAssets.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateGadgetAssets.Summary(), Equals, `Update assets from gadget "some-gadget" (3) for remodel`)
	c.Assert(tUpdateGadgetAssets.WaitTasks(), DeepEquals, []*state.Task{tPrepare})
	c.Assert(tUpdateGadgetCmdline.Kind(), Equals, "update-gadget-cmdline")
	c.Assert(tUpdateGadgetCmdline.Summary(), Equals, `Update kernel command line from gadget "some-gadget" (3) for remodel`)
	c.Assert(tUpdateGadgetCmdline.WaitTasks(), DeepEquals, []*state.Task{tUpdateGadgetAssets})
}

func (s *snapmgrTestSuite) TestRemodelSwitchNewGadgetNoRemodelConflict(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()
	s.addSnapsForRemodel(c)

	tugc := s.state.NewTask("update-managed-boot-config", "update managed boot config")
	chg := s.state.NewChange("remodel", "remodel")
	chg.AddTask(tugc)

	_, err := snapstate.SwitchToNewGadget(s.state, "some-gadget", chg.ID())
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemodelSwitchNewGadgetBadType(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()
	s.addSnapsForRemodel(c)

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(3)}
	snaptest.MockSnapCurrent(c, "name: snap-gadget\nversion: 1.0\n", si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})
	ts, err := snapstate.SwitchToNewGadget(s.state, "some-snap", "")
	c.Assert(err, ErrorMatches, `internal error: cannot link type app`)
	c.Assert(ts, IsNil)
	ts, err = snapstate.SwitchToNewGadget(s.state, "some-kernel", "")
	c.Assert(err, ErrorMatches, `internal error: cannot link type kernel`)
	c.Assert(ts, IsNil)
	ts, err = snapstate.SwitchToNewGadget(s.state, "some-base", "")
	c.Assert(err, ErrorMatches, `internal error: cannot link type base`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRemodelSwitchNewGadgetConflict(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()
	s.addSnapsForRemodel(c)

	tugc := s.state.NewTask("update-gadget-cmdline", "update gadget cmdline")
	chg := s.state.NewChange("optional-kernel-cmdline", "optional kernel cmdline")
	chg.AddTask(tugc)

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(3)}
	snaptest.MockSnapCurrent(c, "name: snap-gadget\nversion: 1.0\n", si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})
	ts, err := snapstate.SwitchToNewGadget(s.state, "some-snap", "")
	c.Assert(err, ErrorMatches, "kernel command line already being updated, no additional changes for it allowed meanwhile")
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRemodelSwitchNewGadgetConflictExclusiveKind(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()
	s.addSnapsForRemodel(c)

	tugc := s.state.NewTask("some-random-task", "...")
	chg := s.state.NewChange("transition-to-snapd-snap", "...")
	chg.AddTask(tugc)

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(3)}
	snaptest.MockSnapCurrent(c, "name: snap-gadget\nversion: 1.0\n", si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})
	ts, err := snapstate.SwitchToNewGadget(s.state, "some-snap", "")
	c.Assert(err, ErrorMatches, "transition to snapd snap in progress, no other changes allowed until this is done")
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRemodelAddGadgetAssetTasks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "some-gadget", Revision: snap.R(3)}
	tPrepare := s.state.NewTask("prepare-snap", "test task")
	snapsup := &snapstate.SnapSetup{
		SideInfo: si,
		Type:     "gadget",
	}
	tPrepare.Set("snap-setup", snapsup)
	testTask := s.state.NewTask("test-task", "test task")
	ts := state.NewTaskSet(tPrepare, testTask)

	tsNew, err := snapstate.AddGadgetAssetsTasks(s.state, ts)
	c.Assert(err, IsNil)
	c.Assert(tsNew, NotNil)
	tasks := tsNew.Tasks()
	c.Check(taskKinds(tasks), DeepEquals, expectedDoInstallTasks(snap.TypeGadget, 0, 0, []string{"prepare-snap", "test-task"}, kindsToSet(append(nonReLinkKinds, "link-snap"))))
	// since this is the gadget, we have our task + test task + update assets + update cmdline
	c.Assert(tasks, HasLen, 4)
	tUpdateGadgetAssets := tasks[2]
	tUpdateGadgetCmdline := tasks[3]
	c.Assert(tUpdateGadgetAssets.Kind(), Equals, "update-gadget-assets")
	c.Assert(tUpdateGadgetAssets.Summary(), Equals, `Update assets from gadget "some-gadget" (3) for remodel`)
	c.Assert(tUpdateGadgetAssets.WaitTasks(), DeepEquals, []*state.Task{
		// waits for the last task in the set
		testTask,
	})
	c.Assert(tUpdateGadgetCmdline.Kind(), Equals, "update-gadget-cmdline")
	c.Assert(tUpdateGadgetCmdline.Summary(), Equals, `Update kernel command line from gadget "some-gadget" (3) for remodel`)
	c.Assert(tUpdateGadgetCmdline.WaitTasks(), DeepEquals, []*state.Task{
		tUpdateGadgetAssets,
	})
	for _, tsk := range []*state.Task{tUpdateGadgetAssets, tUpdateGadgetCmdline} {
		var ssID string
		c.Assert(tsk.Get("snap-setup-task", &ssID), IsNil)
		c.Assert(ssID, Equals, tPrepare.ID())
	}

	// but bails when there is no task with snap setup
	ts = state.NewTaskSet()
	tsNew, err = snapstate.AddGadgetAssetsTasks(s.state, ts)
	c.Assert(err, ErrorMatches, `internal error: cannot identify task with snap-setup`)
	c.Assert(tsNew, IsNil)
}

func (s *snapmgrTestSuite) TestRemodelAddGadgetAssetNoRemodelConflict(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.BaseTest.AddCleanup(snapstate.MockSnapReadInfo(snap.ReadInfo))
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "some-gadget", Revision: snap.R(3)}
	tPrepare := s.state.NewTask("prepare-snap", "test task")
	snapsup := &snapstate.SnapSetup{
		SideInfo: si,
		Type:     "gadget",
	}
	tPrepare.Set("snap-setup", snapsup)

	tugc := s.state.NewTask("update-managed-boot-config", "update managed boot config")
	chg := s.state.NewChange("remodel", "remodel")
	ts := state.NewTaskSet(tPrepare, tugc)
	chg.AddTask(tugc)

	tsNew, err := snapstate.AddGadgetAssetsTasks(s.state, ts)
	c.Assert(err, IsNil)
	c.Assert(tsNew, NotNil)
}

func (s *snapmgrTestSuite) TestMigrateHome(c *C) {
	s.enableRefreshAppAwarenessUX()
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.move-snap-home-dir", true), IsNil)
	tr.Commit()

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(3)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("migrate-home", "...")
	tss, err := snapstate.MigrateHome(s.state, []string{"some-snap"})
	c.Assert(err, IsNil)
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	c.Assert(tss, HasLen, 1)
	c.Assert(taskNames(tss[0].Tasks()), DeepEquals, []string{
		`prepare-snap`,
		`stop-snap-services`,
		`unlink-current-snap`,
		`migrate-snap-home`,
		`link-snap`,
		`start-snap-services`,
	})

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	var undo backend.UndoInfo
	migrateTask := findLastTask(chg, "migrate-snap-home")
	c.Assert(migrateTask.Get("undo-exposed-home-init", &undo), IsNil)
	c.Assert(undo.Created, HasLen, 1)

	s.fakeBackend.ops.MustFindOp(c, "init-exposed-snap-home")

	// check unlink-reason
	unlinkTask := findLastTask(chg, "unlink-current-snap")
	c.Assert(unlinkTask, NotNil)
	var unlinkReason string
	unlinkTask.Get("unlink-reason", &unlinkReason)
	c.Check(unlinkReason, Equals, "home-migration")

	// binaries removal should not be skipped when unlink-reason is home-migration
	unlinkSnapOp := s.fakeBackend.ops.MustFindOp(c, "unlink-snap")
	c.Check(unlinkSnapOp.unlinkSkipBinaries, Equals, false)

	// check migration is off in state and seq file
	assertMigrationState(c, s.state, "some-snap", &dirs.SnapDirOptions{MigratedToExposedHome: true})
}

func (s *snapmgrTestSuite) TestMigrateHomeUndo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.move-snap-home-dir", true), IsNil)
	tr.Commit()

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(3)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	chg := s.state.NewChange("migrate-home", "...")
	tss, err := snapstate.MigrateHome(s.state, []string{"some-snap"})
	c.Assert(err, IsNil)

	c.Assert(tss, HasLen, 1)
	c.Assert(taskNames(tss[0].Tasks()), DeepEquals, []string{
		`prepare-snap`,
		`stop-snap-services`,
		`unlink-current-snap`,
		`migrate-snap-home`,
		`link-snap`,
		`start-snap-services`,
	})

	for _, ts := range tss {
		chg.AddAll(ts)
	}

	// fail the change after the link-snap task (after state is saved)
	s.o.TaskRunner().AddHandler("fail", func(*state.Task, *tomb.Tomb) error {
		return errors.New("boom")
	}, nil)

	failingTask := s.state.NewTask("fail", "expected failure")
	chg.AddTask(failingTask)
	linkTask := findLastTask(chg, "link-snap")
	failingTask.WaitFor(linkTask)
	for _, lane := range linkTask.Lanes() {
		failingTask.JoinLane(lane)
	}

	s.settle(c)

	c.Assert(chg.Err(), ErrorMatches, `(.|\s)* expected failure \(boom\)`)
	c.Assert(chg.IsReady(), Equals, true)

	s.fakeBackend.ops.MustFindOp(c, "init-exposed-snap-home")
	s.fakeBackend.ops.MustFindOp(c, "undo-init-exposed-snap-home")

	// check migration is off in state and seq file
	assertMigrationState(c, s.state, "some-snap", nil)
}

func (s *snapmgrTestSuite) TestMigrateHomeFailIfUnsetFeature(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tss, err := snapstate.MigrateHome(s.state, []string{"some-snap"})
	c.Check(tss, IsNil)
	c.Assert(err, ErrorMatches, `cannot migrate to ~/Snap: flag "experimental.move-snap-home-dir" is not set`)
}

func (s *snapmgrTestSuite) TestMigrateHomeFailIfSnapNotInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.move-snap-home-dir", true), IsNil)
	tr.Commit()

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(3)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})

	tss, err := snapstate.MigrateHome(s.state, []string{"some-snap", "other-snap"})
	c.Check(tss, IsNil)
	c.Assert(err, ErrorMatches, `snap "other-snap" is not installed`)
}

func (s *snapmgrTestSuite) TestMigrateHomeFailIfAlreadyMigrated(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.move-snap-home-dir", true), IsNil)
	tr.Commit()

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(3)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:                true,
		Sequence:              snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:               si.Revision,
		SnapType:              "app",
		MigratedToExposedHome: true,
	})

	tss, err := snapstate.MigrateHome(s.state, []string{"some-snap"})
	c.Check(tss, IsNil)
	c.Assert(err, ErrorMatches, `cannot migrate "some-snap" to ~/Snap: already migrated`)
}

func taskNames(tasks []*state.Task) []string {
	var names []string

	for _, t := range tasks {
		names = append(names, t.Kind())
	}

	return names
}

func (s *snapmgrTestSuite) TestMigrationTriggers(c *C) {
	c.Skip("TODO:Snap-folder: no automatic migration for core22 snaps to ~/Snap folder for now")

	testCases := []struct {
		oldBase  string
		newBase  string
		opts     snapstate.DirMigrationOptions
		expected snapstate.Migration
	}{
		// refreshing to core22 or later
		{
			oldBase:  "core20",
			newBase:  "core22",
			expected: snapstate.Full,
		},
		{
			oldBase:  "core20",
			newBase:  "core22",
			opts:     snapstate.DirMigrationOptions{MigratedToHidden: true},
			expected: snapstate.Home,
		},
		{
			oldBase:  "core20",
			newBase:  "core22",
			opts:     snapstate.DirMigrationOptions{MigratedToHidden: true, UseHidden: true},
			expected: snapstate.Home,
		},
		{
			oldBase:  "core20",
			newBase:  "core24",
			opts:     snapstate.DirMigrationOptions{MigratedToHidden: true, UseHidden: true},
			expected: snapstate.Home,
		},
		{
			oldBase:  "core20",
			newBase:  "core24",
			opts:     snapstate.DirMigrationOptions{MigratedToHidden: true, MigratedToExposedHome: true},
			expected: snapstate.None,
		},
		// reverting to another core22
		{
			oldBase:  "core24",
			newBase:  "core22",
			opts:     snapstate.DirMigrationOptions{MigratedToHidden: true, MigratedToExposedHome: true},
			expected: snapstate.None,
		},
		{
			oldBase:  "core20",
			newBase:  "core20",
			opts:     snapstate.DirMigrationOptions{MigratedToHidden: true},
			expected: snapstate.RevertHidden,
		},
		{
			oldBase:  "core22",
			newBase:  "core20",
			opts:     snapstate.DirMigrationOptions{MigratedToHidden: true, MigratedToExposedHome: true},
			expected: snapstate.RevertFull,
		},
		{
			oldBase:  "core22",
			newBase:  "core20",
			opts:     snapstate.DirMigrationOptions{MigratedToHidden: true, MigratedToExposedHome: true, UseHidden: true},
			expected: snapstate.DisableHome,
		},
	}

	for _, t := range testCases {
		action := snapstate.TriggeredMigration(t.oldBase, t.newBase, &t.opts)
		if action != t.expected {
			c.Errorf("expected install from %q to %q w/ %+v to result in %q but got %q", t.oldBase, t.newBase, t.opts, t.expected, action)
		}
	}
}

func (s *snapmgrTestSuite) TestExcludeFromRefreshAppAwareness(c *C) {
	c.Check(snapstate.ExcludeFromRefreshAppAwareness(snap.TypeApp), Equals, false)
	c.Check(snapstate.ExcludeFromRefreshAppAwareness(snap.TypeGadget), Equals, false)
	c.Check(snapstate.ExcludeFromRefreshAppAwareness(snap.TypeSnapd), Equals, true)
	c.Check(snapstate.ExcludeFromRefreshAppAwareness(snap.TypeOS), Equals, true)
}

func (s *snapmgrTestSuite) TestResolveValidationSetsEnforcementError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	info := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-other-snap-id",
		RealName: "some-other-snap",
	}
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{info}),
		Current:  info.Revision,
		Active:   true,
	})

	headers := map[string]interface{}{
		"type":         "validation-set",
		"timestamp":    time.Now().Format(time.RFC3339),
		"authority-id": "foo",
		"series":       "16",
		"account-id":   "foo",
		"name":         "bar",
		"sequence":     "3",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "some-snap",
				"id":       "mysnapdddddddddddddddddddddddddd",
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "some-other-snap",
				"id":       "mysnapcccccccccccccccccccccccccc",
				"presence": "required",
				"revision": "2",
			},
		},
	}

	storeSigning := assertstest.NewStoreStack("can0nical", nil)
	a, err := storeSigning.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)
	vs := a.(*asserts.ValidationSet)

	valErr := &snapasserts.ValidationSetsValidationError{
		MissingSnaps:       map[string]map[snap.Revision][]string{"some-snap": {snap.R(1): {"foo/bar"}}},
		WrongRevisionSnaps: map[string]map[snap.Revision][]string{"some-other-snap": {snap.R(2): {"foo/bar"}}},
		Sets:               map[string]*asserts.ValidationSet{"foo/bar": vs},
	}
	pinnedSeqs := map[string]int{"foo/bar": 3}

	var calledEnforce bool
	restore := snapstate.MockEnforceValidationSets(func(_ *state.State, usrKeysToVss map[string]*asserts.ValidationSet, pinned map[string]int, snaps []*snapasserts.InstalledSnap, snapsToIgnore map[string]bool, _ int) error {
		calledEnforce = true
		c.Check(pinned, DeepEquals, pinnedSeqs)
		c.Check(snaps, testutil.DeepUnsortedMatches, []*snapasserts.InstalledSnap{
			{SnapRef: naming.NewSnapRef("core", ""), Revision: snap.R(1)},
			{SnapRef: naming.NewSnapRef("some-other-snap", "some-other-snap-id"), Revision: snap.R(2)},
			{SnapRef: naming.NewSnapRef("some-snap", "some-snap-id"), Revision: snap.R(1)}})
		c.Check(snapsToIgnore, HasLen, 0)
		return nil
	})
	defer restore()

	tss, affected, err := snapstate.ResolveValidationSetsEnforcementError(context.Background(), s.state, valErr, pinnedSeqs, s.user.ID)
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"some-other-snap", "some-snap"})

	chg := s.state.NewChange("refresh-to-enforce", "")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)
	c.Assert(chg.Err(), IsNil)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-other-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Current, Equals, snap.R(2))

	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Current, Equals, snap.R(1))

	c.Assert(calledEnforce, Equals, true)
}

func (s *snapmgrTestSuite) TestResolveValidationSetsEnforcementErrorReverse(c *C) {
	// fail to enforce the validation set at the end to trigger an undo
	expectedErr := errors.New("expected")
	restore := snapstate.MockEnforceValidationSets(func(*state.State, map[string]*asserts.ValidationSet, map[string]int, []*snapasserts.InstalledSnap, map[string]bool, int) error {
		return expectedErr
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	info := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-other-snap-id",
		RealName: "some-other-snap",
	}
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{info}),
		Current:  info.Revision,
		Active:   true,
	})

	headers := map[string]interface{}{
		"type":         "validation-set",
		"timestamp":    time.Now().Format(time.RFC3339),
		"authority-id": "foo",
		"series":       "16",
		"account-id":   "foo",
		"name":         "bar",
		"sequence":     "3",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "some-snap",
				"id":       "mysnapdddddddddddddddddddddddddd",
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "some-other-snap",
				"id":       "mysnapcccccccccccccccccccccccccc",
				"presence": "required",
				"revision": "2",
			},
		},
	}

	storeSigning := assertstest.NewStoreStack("can0nical", nil)
	a, err := storeSigning.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)
	vs := a.(*asserts.ValidationSet)

	valErr := &snapasserts.ValidationSetsValidationError{
		MissingSnaps:       map[string]map[snap.Revision][]string{"some-snap": {snap.R(1): {"foo/bar"}}},
		WrongRevisionSnaps: map[string]map[snap.Revision][]string{"some-other-snap": {snap.R(2): {"foo/bar"}}},
		Sets:               map[string]*asserts.ValidationSet{"foo/bar": vs},
	}
	pinnedSeqs := map[string]int{"foo/bar": 3}

	tss, affected, err := snapstate.ResolveValidationSetsEnforcementError(context.TODO(), s.state, valErr, pinnedSeqs, s.user.ID)
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"some-other-snap", "some-snap"})

	chg := s.state.NewChange("refresh-to-enforce", "")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)
	c.Assert(chg.Err(), ErrorMatches, fmt.Sprintf(`(.|\s)*%s\)?`, expectedErr))
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-other-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Current, Equals, snap.R(1))

	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestResolveValidationSetsEnforcementErrorWithInvalidSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	valErr := &snapasserts.ValidationSetsValidationError{
		InvalidSnaps:       map[string][]string{"snap-a": {"foo/bar"}},
		MissingSnaps:       map[string]map[snap.Revision][]string{"snap-b": {snap.R(1): []string{"foo/bar"}}},
		WrongRevisionSnaps: map[string]map[snap.Revision][]string{"snap-c": {snap.R(2): []string{"foo/bar"}}},
	}

	_, _, err := snapstate.ResolveValidationSetsEnforcementError(context.TODO(), s.state, valErr, nil, s.user.ID)
	c.Assert(err, ErrorMatches, "cannot auto-resolve validation set constraints that require removing snaps: \"snap-a\"")
}

func (s *snapmgrTestSuite) TestEnsureSnapStateRewriteMounts(c *C) {
	s.testEnsureSnapStateRewriteMounts(c, "app")
}

func (s *snapmgrTestSuite) TestEnsureSnapStateRewriteMountsSnapdSnap(c *C) {
	s.testEnsureSnapStateRewriteMounts(c, "snapd")
}

func (s *snapmgrTestSuite) testEnsureSnapStateRewriteMounts(c *C, snapType string) {
	restore := snapstate.MockEnsuredMountsUpdated(s.snapmgr, false)
	defer restore()

	testSnapSideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	testSnapState := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{testSnapSideInfo}),
		Current:  snap.R(42),
		Active:   true,
		SnapType: snapType,
	}
	testYaml := `name: test-snap
version: v1
apps:
  test-snap:
    command: bin.sh
`

	s.state.Lock()
	snapstate.Set(s.state, "test-snap", testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, testSnapSideInfo)
	s.state.Unlock()

	what := fmt.Sprintf("%s/%s_%s.snap", "/var/lib/snapd/snaps", "test-snap", "42")
	unitName := systemd.EscapeUnitNamePath(dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "test-snap", fmt.Sprintf("%s.mount", "42"))))
	mountFile := filepath.Join(dirs.SnapServicesDir, unitName)
	mountContent := fmt.Sprintf(`
[Unit]
Description=Mount unit for test-snap, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=local-fs.target

[Mount]
What=%s
Where=%s/test-snap/42
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide,otheroptions
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], what, dirs.SnapMountDir)
	os.MkdirAll(dirs.SnapServicesDir, 0755)
	err := os.WriteFile(mountFile, []byte(mountContent), 0644)
	c.Assert(err, IsNil)

	s.restarts[unitName] = 0

	err = s.snapmgr.Ensure()
	c.Assert(err, IsNil)

	// no restarts of mount unit expected even if changed
	c.Assert(s.restarts[unitName], Equals, 0)

	expectedContent := fmt.Sprintf(`
[Unit]
Description=Mount unit for test-snap, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target

[Mount]
What=%s
Where=%s/test-snap/42
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], what, dirs.StripRootDir(dirs.SnapMountDir))

	c.Assert(mountFile, testutil.FileEquals, expectedContent)
}

func (s *snapmgrTestSuite) TestEnsureSnapStateRewriteMountsNoChange(c *C) {
	restore := snapstate.MockEnsuredMountsUpdated(s.snapmgr, false)
	defer restore()

	testSnapSideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	testSnapState := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{testSnapSideInfo}),
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	}
	testYaml := `name: test-snap
version: v1
apps:
  test-snap:
    command: bin.sh
`

	s.state.Lock()
	snapstate.Set(s.state, "test-snap", testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, testSnapSideInfo)
	s.state.Unlock()

	what := fmt.Sprintf("%s/%s_%s.snap", "/var/lib/snapd/snaps", "test-snap", "42")
	unitName := systemd.EscapeUnitNamePath(dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "test-snap", fmt.Sprintf("%s.mount", "42"))))
	mountFile := filepath.Join(dirs.SnapServicesDir, unitName)
	mountContent := fmt.Sprintf(`
[Unit]
Description=Mount unit for test-snap, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target

[Mount]
What=%s
Where=%s/test-snap/42
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], what, dirs.StripRootDir(dirs.SnapMountDir))
	os.MkdirAll(dirs.SnapServicesDir, 0755)
	err := os.WriteFile(mountFile, []byte(mountContent), 0644)
	c.Assert(err, IsNil)

	s.restarts[unitName] = 0

	err = s.snapmgr.Ensure()
	c.Assert(err, IsNil)

	c.Assert(s.restarts[unitName], Equals, 0)

	c.Assert(mountFile, testutil.FileEquals, mountContent)
}

func (s *snapmgrTestSuite) TestEnsureSnapStateRewriteMountsCreated(c *C) {
	testSnapSideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	testSnapState := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{testSnapSideInfo}),
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	}
	testYaml := `name: test-snap
version: v1
apps:
  test-snap:
    command: bin.sh
`

	s.state.Lock()
	snapstate.Set(s.state, "test-snap", testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, testSnapSideInfo)
	s.state.Unlock()

	what := fmt.Sprintf("%s/%s_%s.snap", "/var/lib/snapd/snaps", "test-snap", "42")
	unitName := systemd.EscapeUnitNamePath(dirs.StripRootDir(filepath.Join(dirs.SnapMountDir, "test-snap", fmt.Sprintf("%s.mount", "42"))))
	mountFile := filepath.Join(dirs.SnapServicesDir, unitName)
	if osutil.FileExists(mountFile) {
		c.Assert(os.Remove(mountFile), IsNil)
	}

	restore := snapstate.MockEnsuredMountsUpdated(s.snapmgr, false)
	defer restore()

	s.restarts[unitName] = 0

	err := s.snapmgr.Ensure()
	c.Assert(err, IsNil)

	c.Assert(s.restarts[unitName], Equals, 1)

	expectedContent := fmt.Sprintf(`
[Unit]
Description=Mount unit for test-snap, revision 42
After=snapd.mounts-pre.target
Before=snapd.mounts.target

[Mount]
What=%s
Where=%s/test-snap/42
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`[1:], what, dirs.StripRootDir(dirs.SnapMountDir))

	c.Assert(mountFile, testutil.FileEquals, expectedContent)
}

func (s *snapmgrTestSuite) TestEnsureSnapStateRewriteDesktopFiles(c *C) {
	restore := snapstate.MockEnsuredDesktopFilesUpdated(s.snapmgr, false)
	defer restore()

	testSnapSideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	testSnapState := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{testSnapSideInfo}),
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	}
	testYaml := `name: test-snap
version: v1
apps:
  test-snap:
    command: bin.sh
`

	s.state.Lock()
	snapstate.Set(s.state, "test-snap", testSnapState)
	info := snaptest.MockSnapCurrent(c, testYaml, testSnapSideInfo)
	s.fakeBackend.addSnapApp("test-snap", "test-snap")
	s.state.Unlock()

	guiDir := filepath.Join(info.MountDir(), "meta", "gui")
	c.Assert(os.MkdirAll(guiDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(guiDir, "test-snap.desktop"), []byte(`
[Desktop Entry]
Name=test
Exec=test-snap
`[1:]), 0o644), IsNil)

	desktopFile := filepath.Join(dirs.SnapDesktopFilesDir, "test-snap_test-snap.desktop")
	otherDesktopFile := filepath.Join(dirs.SnapDesktopFilesDir, "test-snap_other.desktop")
	c.Assert(os.MkdirAll(dirs.SnapDesktopFilesDir, 0o755), IsNil)
	c.Assert(os.WriteFile(desktopFile, []byte("old content"), 0o644), IsNil)
	c.Assert(os.WriteFile(otherDesktopFile, []byte("other old content"), 0o644), IsNil)

	err := s.snapmgr.Ensure()
	c.Assert(err, IsNil)

	expectedContent := fmt.Sprintf(`
[Desktop Entry]
X-SnapInstanceName=test-snap
Name=test
Exec=env BAMF_DESKTOP_FILE_HINT=%s/test-snap_test-snap.desktop %s/test-snap
`[1:], dirs.SnapDesktopFilesDir, dirs.SnapBinariesDir)

	c.Assert(desktopFile, testutil.FileEquals, expectedContent)
	c.Assert(otherDesktopFile, testutil.FileAbsent)
}

func (s *snapmgrTestSuite) TestEnsureSnapStateDownloadsCleanedBlockedOnSeeding(c *C) {
	restore := snapstate.MockEnsuredDownloadsCleaned(s.snapmgr, false)
	defer restore()

	called := 0
	restore = snapstate.MockCleanDownloads(func(st *state.State) error {
		called++
		return nil
	})
	defer restore()

	s.state.Lock()
	s.state.Set("seeded", false)
	s.state.Unlock()

	c.Check(s.snapmgr.Ensure(), Equals, nil)
	c.Check(s.snapmgr.Ensure(), Equals, nil)

	// never attempt removing snaps during seeding because
	// state is not yet initialized with seeded snaps
	c.Check(called, Equals, 0)
}

func (s *snapmgrTestSuite) TestEnsureSnapStateDownloadsCleaned(c *C) {
	restore := snapstate.MockEnsuredDownloadsCleaned(s.snapmgr, false)
	defer restore()

	called := 0
	restore = snapstate.MockCleanDownloads(func(st *state.State) error {
		called++
		return nil
	})
	defer restore()

	// simulate ensure called many times
	for i := 0; i < 5; i++ {
		c.Check(s.snapmgr.Ensure(), Equals, nil)
	}

	// system-wide snap downloads cleaning should only run once
	c.Check(called, Equals, 1)
}

func (s *snapmgrTestSuite) TestSaveRefreshCandidatesOnAutoRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	// precondition check
	var cands map[string]*snapstate.RefreshCandidate
	err := s.state.Get("refresh-candidates", &cands)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})

	names, tss, err := snapstate.AutoRefresh(context.Background(), s.state)
	c.Assert(err, IsNil)
	c.Assert(tss, NotNil)
	c.Check(names, DeepEquals, []string{"some-other-snap", "some-snap"})

	// check that refresh-candidates in the state were updated
	err = s.state.Get("refresh-candidates", &cands)
	c.Assert(err, IsNil)

	c.Assert(cands, HasLen, 2)
	c.Check(cands["some-snap"], NotNil)
	c.Check(cands["some-other-snap"], NotNil)
}

type customStore struct {
	*fakeStore

	customSnapAction func(context.Context, []*store.CurrentSnap, []*store.SnapAction, store.AssertionQuery, *auth.UserState, *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error)
}

func (s customStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	return s.customSnapAction(ctx, currentSnaps, actions, assertQuery, user, opts)
}

func (s *snapmgrTestSuite) TestSaveMonitoredRefreshCandidatesOnAutoRefreshThrottled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "snap-c", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snap-c", SnapID: "snap-c-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	// simulate existing refresh hint from older refresh with
	// some-other-snap being monitored
	cands := map[string]*snapstate.RefreshCandidate{
		"some-other-snap": {Monitored: true},
		"snap-c":          {},
	}
	s.state.Set("refresh-candidates", &cands)

	// simulate store throttling some snaps' during auto-refresh
	isThrottled := map[string]bool{
		"some-other-snap-id": true,
		"snap-c-id":          true,
	}
	type requestRecord struct {
		opts    store.RefreshOptions
		snapIDs map[string]bool
	}
	var requests []requestRecord
	sto := customStore{fakeStore: s.fakeStore}
	sto.customSnapAction = func(ctx context.Context, cs []*store.CurrentSnap, sa []*store.SnapAction, aq store.AssertionQuery, us *auth.UserState, ro *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
		var actionResult []store.SnapActionResult

		snapIDs := map[string]bool{}
		for _, action := range sa {
			snapIDs[action.SnapID] = true
			// throttle refresh requests if this is an auto-refresh
			if isThrottled[action.SnapID] && ro.Scheduled {
				continue
			}
			info, err := s.fakeStore.lookupRefresh(refreshCand{snapID: action.SnapID})
			c.Assert(err, IsNil)
			actionResult = append(actionResult, store.SnapActionResult{Info: info})
		}

		requests = append(requests, requestRecord{
			opts:    *ro,
			snapIDs: snapIDs,
		})

		return actionResult, nil, nil
	}
	snapstate.ReplaceStore(s.state, &sto)

	names, tss, err := snapstate.AutoRefresh(context.Background(), s.state)
	c.Assert(err, IsNil)
	c.Assert(tss, NotNil)
	c.Check(names, DeepEquals, []string{"some-other-snap", "some-snap"})

	// check store request was retried
	c.Check(requests, HasLen, 2)

	// first time, all snaps were requested
	c.Check(requests[0].snapIDs["some-snap-id"], Equals, true)
	c.Check(requests[0].snapIDs["some-other-snap-id"], Equals, true)
	c.Check(requests[0].snapIDs["snap-c-id"], Equals, true)
	// auto-refresh -> scheduled
	c.Check(requests[0].opts.Scheduled, Equals, true)

	// second time, only some-other-snap was retried because it
	// was in old refresh-candidates and was monitored as well
	c.Check(requests[1].snapIDs["some-snap-id"], Equals, false)
	c.Check(requests[1].snapIDs["some-other-snap-id"], Equals, true)
	c.Check(requests[1].snapIDs["snap-c-id"], Equals, false)
	// retry mimicking manual refresh
	c.Check(requests[1].opts.Scheduled, Equals, false)

	// check that refresh-candidates in the state were updated
	var newCands map[string]*snapstate.RefreshCandidate
	err = s.state.Get("refresh-candidates", &newCands)
	c.Assert(err, IsNil)

	c.Assert(newCands, HasLen, 2)
	c.Check(newCands["some-snap"], NotNil)
	c.Check(newCands["some-other-snap"], NotNil)
}

func (s *snapmgrTestSuite) TestRefreshCandidatesMergeFlags(c *C) {
	si := &snap.SideInfo{
		RealName: "some-snap",
	}
	cand := &snapstate.RefreshCandidate{
		SnapSetup: snapstate.SnapSetup{
			SideInfo: si,
			Flags: snapstate.Flags{
				Classic:     true,
				NoReRefresh: true,
			},
		},
	}

	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si})})

	globalFlags := &snapstate.Flags{IsAutoRefresh: true, IsContinuedAutoRefresh: true}
	snapsup, _, err := cand.SnapSetupForUpdate(s.state, nil, 0, globalFlags, nil)
	c.Assert(err, IsNil)
	c.Assert(snapsup, NotNil)
	c.Assert(*snapsup, DeepEquals, snapstate.SnapSetup{
		SideInfo: si,
		Flags: snapstate.Flags{
			Classic:                true,
			NoReRefresh:            true,
			IsAutoRefresh:          true,
			IsContinuedAutoRefresh: true,
		},
	})
}

func (s *snapmgrTestSuite) TestReRefreshSummary(c *C) {
	type testcase struct {
		snaps         []string
		isAutoRefresh bool
		summary       string
	}

	cases := []testcase{
		{},
		{
			snaps:         []string{"one"},
			isAutoRefresh: true,
			summary:       `Monitoring snap "one" to determine whether extra refresh steps are required`,
		},
		{
			snaps:         []string{"one", "two"},
			isAutoRefresh: true,
			summary:       `Monitoring snaps "one", "two" to determine whether extra refresh steps are required`,
		},
		{
			snaps:         []string{"one", "two", "three"},
			isAutoRefresh: true,
			summary:       `Monitoring snaps "one", "two", "three" to determine whether extra refresh steps are required`,
		},
		{
			snaps:         []string{"one", "two", "three", "four"},
			isAutoRefresh: true,
			summary:       `Monitoring 4 snaps to determine whether extra refresh steps are required`,
		},
		{
			snaps:         []string{"one", "two", "three"},
			isAutoRefresh: false,
			summary:       `Monitoring snaps "one", "two", "three" to determine whether extra refresh steps are required`,
		},
		{
			snaps:         []string{"one", "two", "three", "four"},
			isAutoRefresh: false,
			summary:       `Monitoring snaps "one", "two", "three", "four" to determine whether extra refresh steps are required`,
		},
	}

	for _, tc := range cases {
		summary := snapstate.ReRefreshSummary(tc.snaps, &snapstate.Flags{IsAutoRefresh: tc.isAutoRefresh})
		cmt := Commentf("unexpected re-refresh summary for %d snaps (auto-refresh: %t)", len(tc.snaps), tc.isAutoRefresh)
		c.Check(summary, Equals, tc.summary, cmt)
	}
}

func (s *snapmgrTestSuite) TestEndEdgeSetCorrectlyHealthCheck(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Under normal circumstances the end-edge is set on the run-hook task
	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// find the correct task, and ensure it has the correct edge set
	var t *state.Task
	for _, task := range ts.Tasks() {
		if task.Kind() == "run-hook" {
			var hooksup hookstate.HookSetup
			if err := task.Get("hook-setup", &hooksup); err != nil {
				panic(err)
			}
			if hooksup.Hook == "check-health" {
				t = task
				break
			}
		}
	}
	c.Assert(t, NotNil)
	c.Check(ts.MaybeEdge(snapstate.EndEdge).ID(), Equals, t.ID())
}

func (s *snapmgrTestSuite) TestEndEdgeSetCorrectlyNoConfigure(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
`)

	// For snaps that skip configure, we expect the end-edge to be set to either of
	// cleanup task, or start snap services
	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(8),
	}, mockSnap, "", "", snapstate.Flags{SkipConfigure: true}, nil)
	c.Assert(err, IsNil)

	var t *state.Task
	for _, task := range ts.Tasks() {
		if task.Kind() == "start-snap-services" {
			t = task
			break
		}
	}
	c.Assert(t, NotNil)
	c.Check(ts.MaybeEdge(snapstate.EndEdge).ID(), Equals, t.ID())
}

func (s *snapmgrTestSuite) TestEndEdgeSetCorrectlyNoConfigureRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Flags:  snapstate.Flags{DevMode: true},
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "wibbly/stable",
	})

	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
epoch: 1
`)

	// For snaps that skip configure, we expect the end-edge to be set to either of
	// cleanup task, or start snap services
	ts, _, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "some-snap"}, mockSnap, "", "edge", snapstate.Flags{SkipConfigure: true}, nil)
	c.Assert(err, IsNil)

	var t *state.Task
	for _, task := range ts.Tasks() {
		if task.Kind() == "cleanup" {
			t = task
			break
		}
	}
	c.Assert(t, NotNil)
	c.Check(ts.MaybeEdge(snapstate.EndEdge).ID(), Equals, t.ID())
}

func (s *snapmgrTestSuite) TestDownload(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, info, err := snapstate.Download(context.Background(), s.state, "foo", "", nil, 0, snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	c.Check(info.SideInfo, DeepEquals, snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(11),
		SnapID:   "foo-id",
		Channel:  "stable",
	})

	c.Check(ts.Tasks(), HasLen, 2)

	downloadSnap := ts.MaybeEdge(snapstate.BeginEdge)
	c.Assert(downloadSnap, NotNil)
	c.Check(downloadSnap.Kind(), Equals, "download-snap")

	var snapsup snapstate.SnapSetup
	err = downloadSnap.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	validateSnap := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(validateSnap, NotNil)
	c.Check(validateSnap.Kind(), Equals, "validate-snap")

	var snapsupTaskID string
	err = validateSnap.Get("snap-setup-task", &snapsupTaskID)
	c.Assert(err, IsNil)
	c.Check(snapsupTaskID, Equals, downloadSnap.ID())
}

func (s *snapmgrTestSuite) TestDownloadSpecifyRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, info, err := snapstate.Download(context.Background(), s.state, "foo", "", &snapstate.RevisionOptions{
		Revision: snap.R(2),
	}, 0, snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	c.Check(ts.Tasks(), HasLen, 2)

	c.Check(info.SideInfo, DeepEquals, snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(2),
		SnapID:   "foo-id",
	})

	downloadSnap := ts.MaybeEdge(snapstate.BeginEdge)
	c.Assert(downloadSnap, NotNil)
	c.Check(downloadSnap.Kind(), Equals, "download-snap")

	var snapsup snapstate.SnapSetup
	err = downloadSnap.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, snap.R(2))

	validateSnap := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(validateSnap, NotNil)
	c.Check(validateSnap.Kind(), Equals, "validate-snap")

	var snapsupTaskID string
	err = validateSnap.Get("snap-setup-task", &snapsupTaskID)
	c.Assert(err, IsNil)
	c.Check(snapsupTaskID, Equals, downloadSnap.ID())
}

func (s *snapmgrTestSuite) TestDownloadSpecifyDownloadDir(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	downloadDir := c.MkDir()

	ts, info, err := snapstate.Download(context.Background(), s.state, "foo", downloadDir, &snapstate.RevisionOptions{
		Revision: snap.R(1),
	}, 0, snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	c.Check(info.SideInfo, DeepEquals, snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(1),
		SnapID:   "foo-id",
	})

	c.Check(ts.Tasks(), HasLen, 2)

	downloadSnap := ts.MaybeEdge(snapstate.BeginEdge)
	c.Assert(downloadSnap, NotNil)
	c.Check(downloadSnap.Kind(), Equals, "download-snap")

	var snapsup snapstate.SnapSetup
	err = downloadSnap.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.MountFile(), Equals, filepath.Join(downloadDir, "foo_1.snap"))

	validateSnap := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(validateSnap, NotNil)
	c.Check(validateSnap.Kind(), Equals, "validate-snap")

	var snapsupTaskID string
	err = validateSnap.Get("snap-setup-task", &snapsupTaskID)
	c.Assert(err, IsNil)
	c.Check(snapsupTaskID, Equals, downloadSnap.ID())
}

func (s *snapmgrTestSuite) TestDownloadOutOfSpace(c *C) {
	restore := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error {
		return &osutil.NotEnoughDiskSpaceError{}
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	_, _, err := snapstate.Download(context.Background(), s.state, "foo", "", &snapstate.RevisionOptions{
		Revision: snap.R(2),
	}, 0, snapstate.Flags{}, nil)
	c.Assert(err, NotNil)

	diskSpaceErr, ok := err.(*snapstate.InsufficientSpaceError)
	c.Assert(ok, Equals, true)
	c.Check(diskSpaceErr, ErrorMatches, `insufficient space in .* to perform "download" change for the following snaps: foo`)
	c.Check(diskSpaceErr.Path, Equals, dirs.SnapBlobDir)
	c.Check(diskSpaceErr.Snaps, DeepEquals, []string{"foo"})
}

func (s *snapmgrTestSuite) TestDownloadAlreadyInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Current: snap.R(11),
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", SnapID: snaptest.AssertedSnapID("foo"), Revision: snap.R(11)},
		}),
		Active:   true,
		SnapType: "app",
	})

	const downloadDir = ""
	_, _, err := snapstate.Download(context.Background(), s.state, "foo", downloadDir, nil, 0, snapstate.Flags{}, nil)
	c.Assert(err, NotNil)

	alreadyInstalledErr, ok := err.(*snap.AlreadyInstalledError)
	c.Assert(ok, Equals, true)
	c.Check(alreadyInstalledErr.Snap, Equals, "foo")
}

func (s *snapmgrTestSuite) TestDownloadSpecifyCohort(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	opts := &snapstate.RevisionOptions{Channel: "some-channel", CohortKey: "cohort-key"}
	ts, info, err := snapstate.Download(context.Background(), s.state, "foo", "", opts, 0, snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	c.Check(ts.Tasks(), HasLen, 2)

	c.Check(info.SideInfo, DeepEquals, snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(666),
		SnapID:   "foo-id",
		Channel:  "some-channel",
	})

	downloadSnap := ts.MaybeEdge(snapstate.BeginEdge)
	c.Assert(downloadSnap, NotNil)
	c.Check(downloadSnap.Kind(), Equals, "download-snap")

	var snapsup snapstate.SnapSetup
	err = downloadSnap.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, snap.R(666))

	c.Check(snapsup.CohortKey, Equals, "cohort-key")
	c.Check(snapsup.Channel, Equals, "some-channel")

	validateSnap := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(validateSnap, NotNil)
	c.Check(validateSnap.Kind(), Equals, "validate-snap")

	var snapsupTaskID string
	err = validateSnap.Get("snap-setup-task", &snapsupTaskID)
	c.Assert(err, IsNil)
	c.Check(snapsupTaskID, Equals, downloadSnap.ID())
}

func initSnapDownloads(c *C, revisions []snap.Revision) {
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), IsNil)
	for _, rev := range revisions {
		fileName := fmt.Sprintf("some-snap_%s.snap", rev)
		c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, fileName), nil, 0644), IsNil)
	}
}

func (s *snapmgrTestSuite) TestCleanSnapDownloadsSequences(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockMaxUnusedDownloadRetention(0)
	defer restore()

	initSnapDownloads(c, []snap.Revision{snap.R(1), snap.R(1111), snap.R(2), snap.R(3)})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}},
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}},
			},
		},
		Current:  snap.R(3),
		SnapType: "app",
	})

	err := snapstate.CleanSnapDownloads(s.state, "some-snap")
	c.Check(err, IsNil)
	// revision not in sequence should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1111.snap"), testutil.FileAbsent)
	// revisions in sequence should be kept
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestCleanSnapDownloadsRefreshHint(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockMaxUnusedDownloadRetention(0)
	defer restore()

	initSnapDownloads(c, []snap.Revision{snap.R(1), snap.R(11111), snap.R(2), snap.R(3), snap.R(4)})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}},
			},
		},
		Current:  snap.R(3),
		SnapType: "app",
	})
	refreshHints := map[string]*snapstate.RefreshCandidate{
		"some-snap": {
			SnapSetup: snapstate.SnapSetup{
				Type: "app",
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: snap.R(4),
				},
			},
		},
	}
	s.state.Set("refresh-candidates", refreshHints)

	err := snapstate.CleanSnapDownloads(s.state, "some-snap")
	c.Check(err, IsNil)
	// revisions not in refresh hint or sequence should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_11111.snap"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), testutil.FileAbsent)
	// revisions in sequence should be kept
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), testutil.FilePresent)
	// revisions in refresh hint should be kept
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_4.snap"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestCleanSnapDownloadsOngoingChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockMaxUnusedDownloadRetention(0)
	defer restore()

	initSnapDownloads(c, []snap.Revision{snap.R(1), snap.R(2), snap.R(3), snap.R(4)})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}},
			},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})
	mkSnapSetup := func(revision snap.Revision) *snapstate.SnapSetup {
		return &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: "some-snap",
				Revision: revision,
			},
		}
	}
	chg1 := s.state.NewChange("chg-1", "")
	chg2 := s.state.NewChange("chg-2", "")
	tsk1 := s.state.NewTask("download-snap", "")
	tsk2 := s.state.NewTask("pre-download-snap", "")
	tsk3 := s.state.NewTask("download-snap", "")
	tsk1.Set("snap-setup", mkSnapSetup(snap.R(2)))
	tsk2.Set("snap-setup", mkSnapSetup(snap.R(3)))
	tsk3.Set("snap-setup", mkSnapSetup(snap.R(4)))
	chg1.AddTask(tsk1)
	chg2.AddTask(tsk2)
	chg2.AddTask(tsk3)
	// mark ready
	chg1.SetStatus(state.DoneStatus)

	err := snapstate.CleanSnapDownloads(s.state, "some-snap")
	c.Check(err, IsNil)
	// revisions in a finished change should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), testutil.FileAbsent)
	// revisions pointed to by a pre-download task don't count and should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), testutil.FileAbsent)
	// revisions in sequence should be kept
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), testutil.FilePresent)
	// revisions pointed to by a download task in an ongoing change should be kept
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_4.snap"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestCleanSnapDownloadsLocalRevisions(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockMaxUnusedDownloadRetention(0)
	defer restore()

	initSnapDownloads(c, []snap.Revision{snap.R("x1"), snap.R("x2"), snap.R("x3")})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R("x2")}},
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R("x3")}},
			},
		},
		Current:  snap.R("x3"),
		SnapType: "app",
	})

	err := snapstate.CleanSnapDownloads(s.state, "some-snap")
	c.Check(err, IsNil)
	// revision not in sequence should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_x1.snap"), testutil.FileAbsent)
	// revisions in sequence should be kept
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_x2.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_x3.snap"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestCleanSnapDownloadsParallelInstalls(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockMaxUnusedDownloadRetention(0)
	defer restore()

	// parallel install downloads
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-snap_1_1.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-snap_2_2.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-snap_3_x3.snap"), nil, 0644), IsNil)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)}},
			},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	err := snapstate.CleanSnapDownloads(s.state, "some-snap")
	c.Check(err, IsNil)
	// parallel installs should not be affected
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1_1.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_2_2.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_3_x3.snap"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestCleanSnapDownloadsKeepsNewDownloads(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	initSnapDownloads(c, []snap.Revision{snap.R(1), snap.R(1111), snap.R(2), snap.R(3)})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}},
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}},
			},
		},
		Current:  snap.R(3),
		SnapType: "app",
	})

	restore := snapstate.MockMaxUnusedDownloadRetention(10 * time.Second)
	defer restore()

	err := snapstate.CleanSnapDownloads(s.state, "some-snap")
	c.Check(err, IsNil)
	// all snaps will be kept because retention period is still going
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1111.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), testutil.FilePresent)

	// simulate retention period passed
	restore = snapstate.MockMaxUnusedDownloadRetention(0)
	defer restore()

	err = snapstate.CleanSnapDownloads(s.state, "some-snap")
	c.Check(err, IsNil)
	// revision not in sequence should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1111.snap"), testutil.FileAbsent)
	// revisions in sequence should be kept
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestCleanDownloads(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockMaxUnusedDownloadRetention(0)
	defer restore()

	// check that we delete leftovers of non-existing snaps
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-other-snap_1.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-other-other-snap_1.snap"), nil, 0644), IsNil)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}},
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}},
			},
		},
		Current:  snap.R(3),
		SnapType: "app",
	})

	err := snapstate.CleanDownloads(s.state)
	c.Check(err, IsNil)
	// leftovers from non-existing snaps should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-other-snap_1.snap"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-other-other-snap_1.snap"), testutil.FileAbsent)
	// revision not in sequence should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), testutil.FileAbsent)
	// revisions in sequence should be kept
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestCleanDownloadsKeepsNewDownloads(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// check that we delete leftovers of non-existing snaps
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-other-snap_1.snap"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBlobDir, "some-other-other-snap_1.snap"), nil, 0644), IsNil)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}},
				{Snap: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}},
			},
		},
		Current:  snap.R(3),
		SnapType: "app",
	})

	restore := snapstate.MockMaxUnusedDownloadRetention(10 * time.Second)
	defer restore()

	err := snapstate.CleanDownloads(s.state)
	c.Check(err, IsNil)
	// all snaps will be kept because retention period is still going
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-other-snap_1.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-other-other-snap_1.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), testutil.FilePresent)

	// simulate retention period passed
	restore = snapstate.MockMaxUnusedDownloadRetention(0)
	defer restore()

	err = snapstate.CleanDownloads(s.state)
	c.Check(err, IsNil)
	// leftovers from non-existing snaps should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-other-snap_1.snap"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-other-other-snap_1.snap"), testutil.FileAbsent)
	// revision not in sequence should be removed
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_1.snap"), testutil.FileAbsent)
	// revisions in sequence should be kept
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_2.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDir, "some-snap_3.snap"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestRefreshInhibitProceedTime(c *C) {
	snapst := snapstate.SnapState{}
	// No pending refresh
	c.Check(snapst.RefreshInhibitProceedTime(s.state).IsZero(), Equals, true)

	// Refresh inhibited
	refreshInhibitedTime := time.Date(2024, 2, 12, 18, 36, 56, 0, time.UTC)
	snapst.RefreshInhibitedTime = &refreshInhibitedTime
	expectedRefreshInhibitProceedTime := refreshInhibitedTime.Add(snapstate.MaxInhibition)
	c.Check(snapst.RefreshInhibitProceedTime(s.state), Equals, expectedRefreshInhibitProceedTime)
}

func (s *snapmgrTestSuite) TestChangeStatusRecordsChangeUpdateNotice(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("refresh", "")

	s.o.TaskRunner().AddHandler("fake-task", func(task *state.Task, tomb *tomb.Tomb) error { return nil }, nil)

	var prev *state.Task
	addTask := func(name string) {
		t := st.NewTask(name, "")
		chg.AddTask(t)
		if prev != nil {
			t.WaitFor(prev)
		}
		prev = t
	}

	for i := 0; i < 5; i++ {
		addTask("fake-task")
	}

	s.settle(c)

	// Check notice is recorded on change status updates
	notices := s.state.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, chg.ID())
	c.Check(n["last-data"], DeepEquals, map[string]any{"kind": "refresh"})
	// Default -> Doing -> Done
	c.Check(n["occurrences"], Equals, 3.0)
}

func (s *snapmgrTestSuite) TestChangeStatusUndoRecordsChangeUpdateNotice(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("refresh", "")

	s.o.TaskRunner().AddHandler("fake-task", func(task *state.Task, tomb *tomb.Tomb) error { return nil }, nil)

	var prev *state.Task
	addTask := func(name string) {
		t := st.NewTask(name, "")
		chg.AddTask(t)
		if prev != nil {
			t.WaitFor(prev)
		}
		prev = t
	}

	for i := 0; i < 5; i++ {
		addTask("fake-task")
	}
	addTask("error-trigger")
	for i := 0; i < 5; i++ {
		addTask("fake-task")
	}

	s.settle(c)

	// Check notice is recorded on change status updates
	notices := s.state.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, chg.ID())
	c.Check(n["last-data"], DeepEquals, map[string]any{"kind": "refresh"})
	// Default -> Doing -> Undo -> Abort -> Undo -> Error
	c.Check(n["occurrences"], Equals, 6.0)
}

// noticeToMap converts a Notice to a map using a JSON marshal-unmarshal round trip.
func noticeToMap(c *C, notice *state.Notice) map[string]any {
	buf, err := json.Marshal(notice)
	c.Assert(err, IsNil)
	var n map[string]any
	err = json.Unmarshal(buf, &n)
	c.Assert(err, IsNil)
	return n
}

func (s *snapmgrTestSuite) TestCheckExpectedRestartNoEnv(c *C) {
	os.Unsetenv("SNAPD_REVERT_TO_REV")

	st := s.state
	st.Lock()
	defer st.Unlock()

	// no snapd related change in the state
	err := snapstate.CheckExpectedRestart(st)
	c.Assert(err, IsNil)

	// procure a non-ready change for the snapd snap in the state
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", Revision: snap.R(1), SnapID: "snapd-snap-id"},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	chg := s.state.NewChange("refresh-snap", "snapd refresh")
	ts, err := snapstate.Update(s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.Set("snap-names", []string{"snapd"})
	chg.AddAll(ts)

	// but since the env variable is still unset, we just proceed with execution
	err = snapstate.CheckExpectedRestart(st)
	c.Assert(err, IsNil)

	// pretend everything up to auto-connect is done, as if daemon restart
	// was requested
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "auto-connect" {
			break
		}
		tsk.SetStatus(state.DoneStatus)
	}

	// but even then we just proceed with execution
	err = snapstate.CheckExpectedRestart(st)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestCheckExpectedRestartFromSnapFailure(c *C) {
	os.Setenv("SNAPD_REVERT_TO_REV", "1")
	defer os.Unsetenv("SNAPD_REVERT_TO_REV")

	st := s.state
	st.Lock()
	defer st.Unlock()

	// no snapd related change in the state
	err := snapstate.CheckExpectedRestart(st)
	// indicating we should exit
	c.Assert(err, Equals, snapstate.ErrUnexpectedRuntimeRestart)

	// procure a non-ready change for the snapd snap in the state
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", Revision: snap.R(1), SnapID: "snapd-snap-id"},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	chg := s.state.NewChange("refresh-snap", "snapd refresh")
	tss, err := snapstate.Update(s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.Set("snap-names", []string{"snapd"})
	chg.AddAll(tss)

	err = snapstate.CheckExpectedRestart(st)
	// snapd should proceed with execution (possibly rolling back)
	c.Assert(err, Equals, snapstate.ErrUnexpectedRuntimeRestart)

	// pretend everything up to auto-connect is done
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "auto-connect" {
			break
		}
		tsk.SetStatus(state.DoneStatus)
	}

	// if snap-failure was to call snapd now, the restart would not be
	// unexpected
	err = snapstate.CheckExpectedRestart(st)
	// now a restart is not unexpected
	c.Assert(err, IsNil)

	// now mark each task as ready
	for _, ts := range chg.Tasks() {
		ts.SetStatus(state.DoneStatus)
	}
	c.Assert(chg.IsReady(), Equals, true)

	// now there are no non-ready changes related to the snapd snap, which
	// means restart with the env varialbe set would indicate a failure at
	// runtime
	err = snapstate.CheckExpectedRestart(st)
	// snapd should proceed with execution (possibly rolling back)
	c.Assert(err, Equals, snapstate.ErrUnexpectedRuntimeRestart)
}

func (s *snapmgrTestSuite) TestCheckExpectedRestartFromStartUpRequestsStop(c *C) {
	os.Setenv("SNAPD_REVERT_TO_REV", "1")
	defer os.Unsetenv("SNAPD_REVERT_TO_REV")

	s.state.Lock()
	// make sure we have an expected state
	err := snapstate.CheckExpectedRestart(s.state)
	c.Assert(err, Equals, snapstate.ErrUnexpectedRuntimeRestart)
	s.state.Unlock()

	// startup asserts the runtime failure state
	err = s.snapmgr.StartUp()
	c.Check(err, Equals, snapstate.ErrUnexpectedRuntimeRestart)
}
