// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
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

package devicestate_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type firstbootPreseed16Suite struct {
	firstBootBaseTest
	firstBoot16BaseTest
}

var _ = Suite(&firstbootPreseed16Suite{})

func checkPreseedTasks(c *C, tsAll []*state.TaskSet) {
	// the tasks of the last taskset must be gadget-connect, mark-preseeded, mark-seeded
	lastTasks := tsAll[len(tsAll)-1].Tasks()
	c.Check(lastTasks, HasLen, 3)
	gadgetConnectTask := lastTasks[0]
	preseedTask := lastTasks[1]
	markSeededTask := lastTasks[2]
	c.Check(gadgetConnectTask.Kind(), Equals, "gadget-connect")
	c.Check(preseedTask.Kind(), Equals, "mark-preseeded")
	c.Check(markSeededTask.Kind(), Equals, "mark-seeded")

	// mark-seeded waits for mark-preseeded and gadget-connect
	c.Check(markSeededTask.WaitTasks(), DeepEquals, []*state.Task{gadgetConnectTask, preseedTask})
}

func checkPressedTaskStates(c *C, st *state.State) {
	doneTasks := map[string]bool{
		"prerequisites":        true,
		"prepare-snap":         true,
		"link-snap":            true,
		"mount-snap":           true,
		"setup-profiles":       true,
		"update-gadget-assets": true,
		"copy-snap-data":       true,
		"set-auto-aliases":     true,
		"setup-aliases":        true,
		"gadget-connect":       true,
		"auto-connect":         true,
	}
	doTasks := map[string]bool{
		"run-hook":            true,
		"mark-seeded":         true,
		"start-snap-services": true,
	}
	for _, t := range st.Tasks() {
		switch {
		case doneTasks[t.Kind()]:
			c.Check(t.Status(), Equals, state.DoneStatus, Commentf("task: %s", t.Kind()))
		case t.Kind() == "mark-preseeded":
			c.Check(t.Status(), Equals, state.DoingStatus, Commentf("task: %s", t.Kind()))
		case doTasks[t.Kind()]:
			c.Check(t.Status(), Equals, state.DoStatus, Commentf("task: %s", t.Kind()))
		default:
			c.Fatalf("unhandled task kind %s", t.Kind())
		}
	}
}

func checkPreseedOrder(c *C, tsAll []*state.TaskSet, snaps ...string) {
}

func (s *firstbootPreseed16Suite) SetUpTest(c *C) {
	s.TestingSeed16 = &seedtest.TestingSeed16{}

	s.setupBaseTest(c, &s.TestingSeed16.SeedSnaps, false)

	s.SeedDir = dirs.SnapSeedDir

	err := os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "assertions"), 0755)
	c.Assert(err, IsNil)
}

func (s *firstbootPreseed16Suite) TestPreseedHappy(c *C) {
	restore := release.MockPreseedMode(true)
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	systemd.MockOsSymlink(func(string, string) error { return nil })

	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	o := s.createOverlord(c)
	st := o.State()
	opts := &devicestate.PopulateStateFromSeedOptions{Preseed: true}
	chg := s.makeSeedChange(c, st, opts, s.devAcct, checkPreseedTasks, checkPreseedOrder)
	err := o.Settle(settleTimeout)

	st.Lock()
	defer st.Unlock()

	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	checkPressedTaskStates(c, st)
}
