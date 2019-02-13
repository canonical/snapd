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
	"github.com/snapcore/snapd/snap/snaptest"
)

type refreshSuite struct {
	state *state.State
}

var _ = Suite(&refreshSuite{})

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

func (s *refreshSuite) TestSoftRefreshCheck(c *C) {
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
	info := snaptest.MockInfo(c, yamlText, nil)

	// Mock directory locations.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// There are no errors when cgroups are absent.
	err := snapstate.SoftRefreshCheck(info)
	c.Check(err, IsNil)

	snapPath := filepath.Join(dirs.FreezerCgroupDir, "snap.foo")
	daemonPath := filepath.Join(dirs.PidsCgroupDir, "snap.foo.daemon")
	appPath := filepath.Join(dirs.PidsCgroupDir, "snap.foo.app")
	hookPath := filepath.Join(dirs.PidsCgroupDir, "snap.foo.hook.configure")

	// Processes not traced to a service block refresh.
	writePids(c, snapPath, []int{100})
	err = snapstate.SoftRefreshCheck(info)
	c.Check(err, ErrorMatches, `snap "foo" has running apps or hooks`)

	// Services are excluded from the check.
	writePids(c, snapPath, []int{100})
	writePids(c, daemonPath, []int{100})
	err = snapstate.SoftRefreshCheck(info)
	c.Check(err, IsNil)

	// Apps are not excluded.
	writePids(c, snapPath, []int{100, 101})
	writePids(c, daemonPath, []int{100})
	writePids(c, appPath, []int{101})
	err = snapstate.SoftRefreshCheck(info)
	c.Check(err, ErrorMatches, `snap "foo" has running apps \(app\)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101})

	// Hooks are not excluded.
	writePids(c, snapPath, []int{105})
	writePids(c, daemonPath, []int{})
	writePids(c, appPath, []int{})
	writePids(c, hookPath, []int{105})
	err = snapstate.SoftRefreshCheck(info)
	c.Check(err, ErrorMatches, `snap "foo" has running hooks \(configure\)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105})

	// Both apps and hooks can be present.
	writePids(c, snapPath, []int{100, 101, 105})
	writePids(c, daemonPath, []int{100})
	writePids(c, appPath, []int{101})
	writePids(c, hookPath, []int{105})
	err = snapstate.SoftRefreshCheck(info)
	c.Check(err, ErrorMatches, `snap "foo" has running apps \(app\) and hooks \(configure\)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101, 105})
}

func (s *refreshSuite) TestHardRefreshCheck(c *C) {
	// Mock directory locations.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// There are no errors when cgroups are absent.
	err := snapstate.HardRefreshCheck("foo")
	c.Check(err, IsNil)

	snapPath := filepath.Join(dirs.FreezerCgroupDir, "snap.foo")
	daemonPath := filepath.Join(dirs.PidsCgroupDir, "snap.foo.daemon")
	appPath := filepath.Join(dirs.PidsCgroupDir, "snap.foo.app")
	hookPath := filepath.Join(dirs.PidsCgroupDir, "snap.foo.hooks.configure")

	// Presence of any processes blocks refresh.
	writePids(c, snapPath, []int{100})
	err = snapstate.HardRefreshCheck("foo")
	c.Check(err, ErrorMatches, `snap "foo" has running apps or hooks`)

	// Services are not excluded from the check.
	writePids(c, snapPath, []int{100})
	writePids(c, daemonPath, []int{100})
	err = snapstate.HardRefreshCheck("foo")
	c.Check(err, ErrorMatches, `snap "foo" has running apps or hooks`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{100})

	// Apps are not excluded from the check.
	writePids(c, snapPath, []int{101})
	writePids(c, appPath, []int{101})
	err = snapstate.HardRefreshCheck("foo")
	c.Check(err, ErrorMatches, `snap "foo" has running apps or hooks`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101})

	// Hooks are not excluded from the check.
	writePids(c, snapPath, []int{105})
	writePids(c, daemonPath, []int{})
	writePids(c, appPath, []int{})
	writePids(c, hookPath, []int{105})
	err = snapstate.HardRefreshCheck("foo")
	c.Check(err, ErrorMatches, `snap "foo" has running apps or hooks`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105})
}
