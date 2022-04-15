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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type refreshSuite struct {
	testutil.BaseTest
	state *state.State
	info  *snap.Info
	pids  map[string][]int
}

var _ = Suite(&refreshSuite{})

func (s *refreshSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	yamlText := `
name: pkg
version: 1
apps:
  daemon:
    command: test
    daemon: simple
  app:
    command: test
hooks:
  configure:
`
	s.info = snaptest.MockInfo(c, yamlText, nil)
	s.pids = nil
	restore := snapstate.MockPidsOfSnap(func(instanceName string) (map[string][]int, error) {
		c.Assert(instanceName, Equals, s.info.InstanceName())
		return s.pids, nil
	})
	s.AddCleanup(restore)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	s.state = state.New(nil)
}

func (s *refreshSuite) TestSoftNothingRunningRefreshCheck(c *C) {
	// Services are not blocking soft refresh check,
	// they will be stopped before refresh.
	s.pids = map[string][]int{
		"snap.pkg.daemon": {100},
	}
	err := snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Check(err, IsNil)

	// Apps are blocking soft refresh check. They are not stopped by
	// snapd, unless the app is running for longer than the maximum
	// duration allowed for postponing refreshes.
	s.pids = map[string][]int{
		"snap.pkg.daemon": {100},
		"snap.pkg.app":    {101},
	}
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running apps (app), pids: 101`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101})

	// Hooks behave just like apps. IDEA: perhaps hooks should not be
	// killed this way? They have their own life-cycle management.
	s.pids = map[string][]int{
		"snap.pkg.hook.configure": {105},
	}
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running hooks (configure), pids: 105`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105})

	// Both apps and hooks can be running.
	s.pids = map[string][]int{
		"snap.pkg.hook.configure": {105},
		"snap.pkg.app":            {106},
	}
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running apps (app) and hooks (configure), pids: 105,106`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105, 106})
}

func (s *refreshSuite) TestHardNothingRunningRefreshCheck(c *C) {
	// Regular services are blocking hard refresh check.
	// We were expecting them to be gone by now.
	s.pids = map[string][]int{
		"snap.pkg.daemon": {100},
	}
	err := snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running apps (daemon), pids: 100`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{100})

	// When the service is supposed to endure refreshes it will not be
	// stopped. As such such services cannot block refresh.
	s.info.Apps["daemon"].RefreshMode = "endure"
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Check(err, IsNil)
	s.info.Apps["daemon"].RefreshMode = ""

	// Applications are also blocking hard refresh check.
	s.pids = map[string][]int{
		"snap.pkg.app": {101},
	}
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running apps (app), pids: 101`)
	c.Assert(err, FitsTypeOf, &snapstate.BusySnapError{})
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101})

	// Hooks are equally blocking hard refresh check.
	s.pids = map[string][]int{
		"snap.pkg.hook.configure": {105},
	}
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running hooks (configure), pids: 105`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105})
}

func (s *refreshSuite) TestPendingSnapRefreshInfo(c *C) {
	err := snapstate.NewBusySnapError(s.info, nil, nil, nil)
	refreshInfo := err.PendingSnapRefreshInfo()
	c.Check(refreshInfo.InstanceName, Equals, s.info.InstanceName())
	// The information about a busy app is not populated because
	// the original error did not have the corresponding information.
	c.Check(refreshInfo.BusyAppName, Equals, "")
	c.Check(refreshInfo.BusyAppDesktopEntry, Equals, "")

	// If we create a matching desktop entry then relevant meta-data is added.
	err = snapstate.NewBusySnapError(s.info, nil, []string{"app"}, nil)
	desktopFile := s.info.Apps["app"].DesktopFile()
	c.Assert(os.MkdirAll(filepath.Dir(desktopFile), 0755), IsNil)
	c.Assert(ioutil.WriteFile(desktopFile, nil, 0644), IsNil)
	refreshInfo = err.PendingSnapRefreshInfo()
	c.Check(refreshInfo.InstanceName, Equals, s.info.InstanceName())
	c.Check(refreshInfo.BusyAppName, Equals, "app")
	c.Check(refreshInfo.BusyAppDesktopEntry, Equals, "pkg_app")
}

func (s *refreshSuite) addInstalledSnap(snapst *snapstate.SnapState) (*snapstate.SnapState, *snap.Info) {
	snapName := snapst.Sequence[0].RealName
	snapstate.Set(s.state, snapName, snapst)
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: snapName, Revision: snapst.Current}}
	return snapst, info
}

func (s *refreshSuite) addFakeInstalledSnap() (*snapstate.SnapState, *snap.Info) {
	return s.addInstalledSnap(&snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "pkg", Revision: snap.R(5), SnapID: "pkg-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
}

func (s *refreshSuite) TestDoSoftRefreshCheckAllowed(c *C) {
	// Pretend we have a snap
	s.state.Lock()
	defer s.state.Unlock()
	snapst, info := s.addFakeInstalledSnap()

	// Pretend that snaps can refresh normally.
	restore := snapstate.MockGenericRefreshCheck(func(info *snap.Info, canAppRunDuringRefresh func(app *snap.AppInfo) bool) error {
		return nil
	})
	defer restore()

	// Soft refresh should not fail.
	err := snapstate.SoftCheckNothingRunningForRefresh(s.state, snapst, info)
	c.Assert(err, IsNil)

	// In addition, the inhibition lock is not set.
	hint, err := runinhibit.IsLocked(info.InstanceName())
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
}

func (s *refreshSuite) TestDoSoftRefreshCheckDisallowed(c *C) {
	// Pretend we have a snap
	s.state.Lock()
	defer s.state.Unlock()
	snapst, info := s.addFakeInstalledSnap()

	// Pretend that snaps cannot refresh.
	restore := snapstate.MockGenericRefreshCheck(func(info *snap.Info, canAppRunDuringRefresh func(app *snap.AppInfo) bool) error {
		return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
	})
	defer restore()

	// Soft refresh should fail with a proper error.
	err := snapstate.SoftCheckNothingRunningForRefresh(s.state, snapst, info)
	c.Assert(err, ErrorMatches, `snap "pkg" has running apps or hooks, pids: 123`)

	// Validity check: the inhibition lock was not set.
	hint, err := runinhibit.IsLocked(info.InstanceName())
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
}

func (s *refreshSuite) TestDoHardRefreshFlowRefreshAllowed(c *C) {
	backend := &fakeSnappyBackend{}
	// Pretend we have a snap
	s.state.Lock()
	defer s.state.Unlock()
	snapst, info := s.addFakeInstalledSnap()

	// Pretend that snaps can refresh normally.
	restore := snapstate.MockGenericRefreshCheck(func(info *snap.Info, canAppRunDuringRefresh func(app *snap.AppInfo) bool) error {
		return nil
	})
	defer restore()

	// Hard refresh should not fail and return a valid lock.
	lock, err := snapstate.HardEnsureNothingRunningDuringRefresh(backend, s.state, snapst, info)
	c.Assert(err, IsNil)
	c.Assert(lock, NotNil)
	defer lock.Close()

	// We should be able to unlock the lock without an error because
	// it was acquired in the same process by the tested logic.
	c.Assert(lock.Unlock(), IsNil)

	// In addition, the fake backend recorded that a lock was established.
	op := backend.ops.MustFindOp(c, "run-inhibit-snap-for-unlink")
	c.Check(op.inhibitHint, Equals, runinhibit.Hint("refresh"))
}

func (s *refreshSuite) TestDoHardRefreshFlowRefreshDisallowed(c *C) {
	backend := &fakeSnappyBackend{}
	// Pretend we have a snap
	s.state.Lock()
	defer s.state.Unlock()
	snapst, info := s.addFakeInstalledSnap()

	// Pretend that snaps cannot refresh.
	restore := snapstate.MockGenericRefreshCheck(func(info *snap.Info, canAppRunDuringRefresh func(app *snap.AppInfo) bool) error {
		return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
	})
	defer restore()

	// Hard refresh should fail and not return a lock.
	lock, err := snapstate.HardEnsureNothingRunningDuringRefresh(backend, s.state, snapst, info)
	c.Assert(err, ErrorMatches, `snap "pkg" has running apps or hooks, pids: 123`)
	c.Assert(lock, IsNil)

	// Validity check: the inhibition lock was not set.
	op := backend.ops.MustFindOp(c, "run-inhibit-snap-for-unlink")
	c.Check(op.inhibitHint, Equals, runinhibit.Hint("refresh"))
}
