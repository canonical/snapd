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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type refreshSuite struct {
	state   *state.State
	info    *snap.Info
	pids    map[string][]int
	restore func()
}

var _ = Suite(&refreshSuite{})

func (s *refreshSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	yamlText := `
name: pkg
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
	s.pids = nil
	s.restore = snapstate.MockPidsOfSnap(func(instanceName string) (map[string][]int, error) {
		c.Assert(instanceName, Equals, s.info.InstanceName())
		return s.pids, nil
	})
}

func (s *refreshSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.restore()
}

func (s *refreshSuite) TestSoftNothingRunningRefreshCheck(c *C) {
	// Services are not blocking soft refresh check,
	// they will be stopped before refresh.
	s.pids = map[string][]int{
		"snap.pkg.daemon": []int{100},
	}
	err := snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Check(err, IsNil)

	// Apps are blocking soft refresh check. They are not stopped by
	// snapd, unless the app is running for longer than the maximum
	// duration allowed for postponing refreshes.
	s.pids = map[string][]int{
		"snap.pkg.daemon": []int{100},
		"snap.pkg.app":    []int{101},
	}
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running apps (app)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101})

	// Hooks behave just like apps. IDEA: perhaps hooks should not be
	// killed this way? They have their own life-cycle management.
	s.pids = map[string][]int{
		"snap.pkg.hook.configure": []int{105},
	}
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running hooks (configure)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105})

	// Both apps and hooks can be running.
	s.pids = map[string][]int{
		"snap.pkg.hook.configure": []int{105},
		"snap.pkg.app":            []int{106},
	}
	err = snapstate.SoftNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running apps (app) and hooks (configure)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105, 106})
}

func (s *refreshSuite) TestHardNothingRunningRefreshCheck(c *C) {
	// Regular services are blocking hard refresh check.
	// We were expecting them to be gone by now.
	s.pids = map[string][]int{
		"snap.pkg.daemon": []int{100},
	}
	err := snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running apps (daemon)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{100})

	// When the service is supposed to endure refreshes it will not be
	// stopped. As such such services cannot block refresh.
	s.info.Apps["daemon"].RefreshMode = "endure"
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Check(err, IsNil)
	s.info.Apps["daemon"].RefreshMode = ""

	// Applications are also blocking hard refresh check.
	s.pids = map[string][]int{
		"snap.pkg.app": []int{101},
	}
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running apps (app)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{101})

	// Hooks are equally blocking hard refresh check.
	s.pids = map[string][]int{
		"snap.pkg.hook.configure": []int{105},
	}
	err = snapstate.HardNothingRunningRefreshCheck(s.info)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `snap "pkg" has running hooks (configure)`)
	c.Check(err.(*snapstate.BusySnapError).Pids(), DeepEquals, []int{105})
}
