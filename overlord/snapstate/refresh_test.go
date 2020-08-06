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
	"bytes"
	"fmt"
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

	// paths of the PID cgroup of each app or hook.
	daemonPath string
	appPath    string
	hookPath   string
}

var _ = Suite(&refreshSuite{})

func (s *refreshSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	mockPidsCgroupDir := c.MkDir()
	s.AddCleanup(snapstate.MockPidsCgroupDir(mockPidsCgroupDir))

	yamlText := `
name: foo
version: 1
apps:
  daemon:
    command: dummy
    daemon: simple
  app:
    command: dummy
hooks:
  configure:
`
	s.info = snaptest.MockInfo(c, yamlText, nil)
	s.daemonPath = filepath.Join(mockPidsCgroupDir, s.info.Apps["daemon"].SecurityTag())
	s.appPath = filepath.Join(mockPidsCgroupDir, s.info.Apps["app"].SecurityTag())
	s.hookPath = filepath.Join(mockPidsCgroupDir, s.info.Hooks["configure"].SecurityTag())

	s.state = state.New(nil)
}

func (s *refreshSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.BaseTest.TearDownTest(c)
}

func writePids(c *C, dir string, pids []int) {
	err := os.MkdirAll(dir, 0755)
	c.Assert(err, IsNil)
	var buf bytes.Buffer
	for _, pid := range pids {
		fmt.Fprintf(&buf, "%d\n", pid)
	}
	err = ioutil.WriteFile(filepath.Join(dir, "cgroup.procs"), buf.Bytes(), 0644)
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestSoftNothingRunningRefreshCheck(c *C) {
	// There are no errors when PID cgroup is absent.
	err := snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Check(err, IsNil)

	// Services are not blocking soft refresh check,
	// they will be stopped before refresh.
	writePids(c, s.daemonPath, []int{100})
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Check(err, IsNil)

	// Apps are blocking soft refresh check. They are not stopped by
	// snapd, unless the app is running for longer than the maximum
	// duration allowed for postponing refreshes.
	writePids(c, s.daemonPath, []int{100})
	writePids(c, s.appPath, []int{101})
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "foo" has running apps (app)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101})

	// Hooks behave just like apps. IDEA: perhaps hooks should not be
	// killed this way? They have their own life-cycle management.
	writePids(c, s.daemonPath, []int{})
	writePids(c, s.appPath, []int{})
	writePids(c, s.hookPath, []int{105})
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "foo" has running hooks (configure)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105})

	// Both apps and hooks can be running.
	writePids(c, s.daemonPath, []int{100})
	writePids(c, s.appPath, []int{101})
	writePids(c, s.hookPath, []int{105})
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "foo" has running apps (app) and hooks (configure)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101, 105})
}

func (s *refreshSuite) TestHardNothingRunningRefreshCheck(c *C) {
	// There are no errors when PID cgroup is absent.
	err := snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Check(err, IsNil)

	// Regular services are blocking hard refresh check.
	// We were expecting them to be gone by now.
	writePids(c, s.daemonPath, []int{100})
	writePids(c, s.appPath, []int{})
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "foo" has running apps (daemon)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{100})

	// When the service is supposed to endure refreshes it will not be
	// stopped. As such such services cannot block refresh.
	s.info.Apps["daemon"].RefreshMode = "endure"
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Check(err, IsNil)
	s.info.Apps["daemon"].RefreshMode = ""

	// Applications are also blocking hard refresh check.
	writePids(c, s.daemonPath, []int{})
	writePids(c, s.appPath, []int{101})
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "foo" has running apps (app)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101})

	// Hooks are equally blocking hard refresh check.
	writePids(c, s.daemonPath, []int{})
	writePids(c, s.appPath, []int{})
	writePids(c, s.hookPath, []int{105})
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "foo" has running hooks (configure)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105})
}

func (s *refreshSuite) addInstalledSnap(snapst *snapstate.SnapState) (*snapstate.SnapState, *snap.Info) {
	snapName := snapst.Sequence[0].RealName
	snapstate.Set(s.state, snapName, snapst)
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: snapName, Revision: snapst.Current}}
	return snapst, info
}

func (s *refreshSuite) addDummyInstalledSnap() (*snapstate.SnapState, *snap.Info) {
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
	snapst, info := s.addDummyInstalledSnap()

	// Pretend that snaps can refresh normally.
	restore := snapstate.MockGenericRefreshCheck(func(info *snap.Info, canAppRunDuringRefresh func(app *snap.AppInfo) bool) error {
		return nil
	})
	defer restore()

	// Soft refresh should not fail.
	err := snapstate.DoSoftRefreshCheck(s.state, snapst, info)
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
	snapst, info := s.addDummyInstalledSnap()

	// Pretend that snaps cannot refresh.
	restore := snapstate.MockGenericRefreshCheck(func(info *snap.Info, canAppRunDuringRefresh func(app *snap.AppInfo) bool) error {
		return &snapstate.BusySnapError{SnapName: info.InstanceName()}
	})
	defer restore()

	// Soft refresh should fail with a proper error.
	err := snapstate.DoSoftRefreshCheck(s.state, snapst, info)
	c.Assert(err, ErrorMatches, `snap "pkg" has running apps or hooks`)

	// Sanity check: the inhibition lock was not set.
	hint, err := runinhibit.IsLocked(info.InstanceName())
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
}

func (s *refreshSuite) TestDoHardRefreshCheckAllowed(c *C) {
	// Pretend we have a snap
	s.state.Lock()
	defer s.state.Unlock()
	snapst, info := s.addDummyInstalledSnap()

	// Pretend that snaps can refresh normally.
	restore := snapstate.MockGenericRefreshCheck(func(info *snap.Info, canAppRunDuringRefresh func(app *snap.AppInfo) bool) error {
		return nil
	})
	defer restore()

	// Hard refresh should not fail and return a valid lock.
	lock, err := snapstate.DoHardRefreshCheck(s.state, snapst, info)
	c.Assert(err, IsNil)
	defer lock.Close()

	// We should be able to unlock the lock without an error because
	// it was acquired in the same process by the tested logic.
	c.Assert(lock.Unlock(), IsNil)

	// In addition, there's now a run inhibit lock with a refresh hint.
	hint, err := runinhibit.IsLocked(info.InstanceName())
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintInhibitedForRefresh)
}

func (s *refreshSuite) TestDoHardRefreshCheckDisallowed(c *C) {
	// Pretend we have a snap
	s.state.Lock()
	defer s.state.Unlock()
	snapst, info := s.addDummyInstalledSnap()

	// Pretend that snaps cannot refresh.
	restore := snapstate.MockGenericRefreshCheck(func(info *snap.Info, canAppRunDuringRefresh func(app *snap.AppInfo) bool) error {
		return &snapstate.BusySnapError{SnapName: info.InstanceName()}
	})
	defer restore()

	// Hard refresh should fail and not return a lock.
	lock, err := snapstate.DoHardRefreshCheck(s.state, snapst, info)
	c.Assert(err, ErrorMatches, `snap "pkg" has running apps or hooks`)
	c.Assert(lock, IsNil)

	// Sanity check: the inhibition lock was not set.
	hint, err := runinhibit.IsLocked(info.InstanceName())
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
}
