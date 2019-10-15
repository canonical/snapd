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
