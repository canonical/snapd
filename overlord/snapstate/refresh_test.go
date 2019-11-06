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
)

type refreshSuite struct {
	state *state.State
	info  *snap.Info

	// paths of the PID cgroup of each app or hook.
	daemonPath string
	appPath    string
	hookPath   string
}

var _ = Suite(&refreshSuite{})

func (s *refreshSuite) SetUpTest(c *C) {
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
	dirs.SetRootDir(c.MkDir())
	s.daemonPath = filepath.Join(dirs.CgroupDir, "intermediate", s.info.Apps["daemon"].SecurityTag())
	s.appPath = filepath.Join(dirs.CgroupDir, "intermediate", s.info.Apps["app"].SecurityTag())
	s.hookPath = filepath.Join(dirs.CgroupDir, "intermediate", s.info.Hooks["configure"].SecurityTag())
}

func (s *refreshSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
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

func (s *refreshSuite) TestPidsOfSnapEmpty(c *C) {
	// For context,the snap is called "foo"
	c.Assert(s.info.SnapName(), Equals, "foo")

	// Not having any cgroup directories is not an error.
	pids, err := snapstate.PidsOfSnap(s.info)
	c.Assert(err, IsNil)
	c.Check(pids, HasLen, 0)
}

func (s *refreshSuite) TestPidsOfSnapUnrelatedStuff(c *C) {
	// For context,the snap is called "foo"
	c.Assert(s.info.SnapName(), Equals, "foo")

	// Things that are not related to the snap are not being picked up.
	path := filepath.Join(dirs.CgroupDir, "system.slice", "udisks2.service")
	writePids(c, path, []int{100})
	pids, err := snapstate.PidsOfSnap(s.info)
	c.Assert(err, IsNil)
	c.Check(pids, HasLen, 0)
}

func (s *refreshSuite) TestPidsOfSnapSecurityTags(c *C) {
	// For context,the snap is called "foo"
	c.Assert(s.info.SnapName(), Equals, "foo")

	// Pids are collected and assigned to bins by security tag
	path := filepath.Join(dirs.CgroupDir, "system.slice", "snap.foo.service", "snap.foo.hook.configure")
	writePids(c, path, []int{1})
	path = filepath.Join(dirs.CgroupDir, "system.slice", "snap.foo.service", "snap.foo.foo")
	writePids(c, path, []int{2})

	pids, err := snapstate.PidsOfSnap(s.info)
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.foo.hook.configure": {1},
		"snap.foo.foo":            {2},
	})
}

func (s *refreshSuite) TestPidsOfInstances(c *C) {
	// For context,the snap is called "foo"
	c.Assert(s.info.SnapName(), Equals, "foo")

	// Instances are not confused between themselves and between the non-instance version.
	path := filepath.Join(dirs.CgroupDir, "system.slice", "snap.foo_prod.foo.service", "snap.foo_prod.foo")
	writePids(c, path, []int{1})
	path = filepath.Join(dirs.CgroupDir, "system.slice", "snap.foo_devel.foo.service", "snap.foo_devel.foo")
	writePids(c, path, []int{2})
	path = filepath.Join(dirs.CgroupDir, "system.slice", "snap.foo.foo.service", "snap.foo.foo")
	writePids(c, path, []int{3})

	// The main one
	pids, err := snapstate.PidsOfSnap(s.info)
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.foo.foo": {3},
	})

	// The development one
	s.info.InstanceKey = "devel"
	pids, err = snapstate.PidsOfSnap(s.info)
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.foo_devel.foo": {2},
	})

	// The production one
	s.info.InstanceKey = "prod"
	pids, err = snapstate.PidsOfSnap(s.info)
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.foo_prod.foo": {1},
	})
}

func (s *refreshSuite) TestPidsOfAggregation(c *C) {
	// For context,the snap is called "foo"
	c.Assert(s.info.SnapName(), Equals, "foo")

	// A single snap may be invoked by multiple users in different sessions.
	// All of their PIDs are collected though.
	path := filepath.Join(dirs.CgroupDir, "user.slice", "user-1000.slice", "user@1000.service", "gnome-shell-wayland.service", "snap.foo.foo")
	writePids(c, path, []int{1})
	path = filepath.Join(dirs.CgroupDir, "user.slice", "user-1001.slice", "user@1001.service", "gnome-shell-wayland.service", "snap.foo.foo")
	writePids(c, path, []int{2})

	pids, err := snapstate.PidsOfSnap(s.info)
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.foo.foo": {1, 2},
	})
}

func (s *refreshSuite) TestPidsOfUnrelated(c *C) {
	// For context,the snap is called "foo"
	c.Assert(s.info.SnapName(), Equals, "foo")

	// We are not confusing snaps with other snaps and with non-snap hierarchies.
	path := filepath.Join(dirs.CgroupDir, "user.slice", "...", "snap.foo.foo")
	writePids(c, path, []int{1})
	path = filepath.Join(dirs.CgroupDir, "user.slice", "...", "snap.bar.bar")
	writePids(c, path, []int{2})
	path = filepath.Join(dirs.CgroupDir, "user.slice", "...", "foo.service")
	writePids(c, path, []int{3})

	pids, err := snapstate.PidsOfSnap(s.info)
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.foo.foo": {1},
	})
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
