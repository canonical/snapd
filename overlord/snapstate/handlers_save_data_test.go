// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type createSnapSaveDataSuite struct {
	baseHandlerSuite
}

var _ = Suite(&createSnapSaveDataSuite{})

func (s *createSnapSaveDataSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)

	rootDir := c.MkDir()
	dirs.SetRootDir(rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *createSnapSaveDataSuite) TestDoCreateSnapSave(c *C) {
	// Mock the mount point of ubuntu-save partition to /var/lib/snapd/save
	restoreMountInfo := osutil.MockMountInfo(fmt.Sprintf(`1084 75 253:1 / %s rw,relatime shared:57 - ext4 /dev/mapper/ubuntu-save-15dc36e8-9841-4cf0-ab86-2b3f88bcad7d rw
1085 149 253:1 / /writable/system-data/var/lib/snapd/save rw,relatime shared:57 - ext4 /dev/mapper/ubuntu-save-15dc36e8-9841-4cf0-ab86-2b3f88bcad7d rw
`, dirs.SnapSaveDir))
	defer restoreMountInfo()

	s.state.Lock()
	defer s.state.Unlock()

	// With a snap "pkg" at revision 42
	si := &snap.SideInfo{RealName: "pkg", Revision: snap.R(42)}
	snapstate.Set(s.state, "pkg", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		Active:   true,
	})

	// We can unlink the current revision of that snap, by setting IgnoreRunning flag.
	task := s.state.NewTask("create-snap-save", "test")
	task.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "pkg",
			Revision: snap.R(42),
		},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(task)

	// Run the task we created
	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	// And observe the results.
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "pkg", &snapst)
	c.Assert(err, IsNil)
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)
	c.Check(osutil.IsDirectory(snap.CommonSaveDir(snapst.InstanceName())), Equals, true)
}

func (s *createSnapSaveDataSuite) TestDoCreateSnapSaveNoPartition(c *C) {
	restoreMountInfo := osutil.MockMountInfo("")
	defer restoreMountInfo()

	s.state.Lock()
	defer s.state.Unlock()

	// With a snap "pkg" at revision 42
	si := &snap.SideInfo{RealName: "pkg", Revision: snap.R(42)}
	snapstate.Set(s.state, "pkg", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		Active:   true,
	})

	// We can unlink the current revision of that snap, by setting IgnoreRunning flag.
	task := s.state.NewTask("create-snap-save", "test")
	task.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "pkg",
			Revision: snap.R(42),
		},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(task)

	// Run the task we created
	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	// And observe the results.
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "pkg", &snapst)
	c.Assert(err, IsNil)
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)
	c.Check(osutil.IsDirectory(snap.CommonSaveDir(snapst.InstanceName())), Equals, false)
}
