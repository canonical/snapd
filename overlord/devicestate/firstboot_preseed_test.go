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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/testutil"
)

type firstbootPreseed16Suite struct {
	firstBootBaseTest
	firstBoot16BaseTest
}

var _ = Suite(&firstbootPreseed16Suite{})

func checkPreseedTasks(c *C, tsAll []*state.TaskSet) {
	// the tasks of the last taskset must be mark-preseeded, mark-seeded
	lastTasks := tsAll[len(tsAll)-1].Tasks()
	c.Check(lastTasks, HasLen, 2)
	preseedTask := lastTasks[0]
	markSeededTask := lastTasks[1]
	c.Assert(preseedTask.Kind(), Equals, "mark-preseeded")
	c.Check(markSeededTask.Kind(), Equals, "mark-seeded")

	// mark-seeded waits for mark-preseeded
	var waitsForPreseeded bool
	for _, wt := range markSeededTask.WaitTasks() {
		if wt.Kind() == "mark-preseeded" {
			waitsForPreseeded = true
		}
	}
	c.Check(waitsForPreseeded, Equals, true)
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
		"auto-connect":         true,
	}
	doTasks := map[string]bool{
		"run-hook":            true,
		"mark-seeded":         true,
		"start-snap-services": true,
	}
	seenDone := make(map[string]bool)
	for _, t := range st.Tasks() {
		if t.Status() == state.DoneStatus {
			seenDone[t.Kind()] = true
		}
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
	// sanity: check that doneTasks is not declaring more tasks than
	// actually expected.
	c.Check(doneTasks, DeepEquals, seenDone)
}

func markPreseededInWaitChain(t *state.Task) bool {
	for _, wt := range t.WaitTasks() {
		if wt.Kind() == "mark-preseeded" {
			return true
		}
		if markPreseededInWaitChain(wt) {
			return true
		}
	}
	return false
}

func checkPreseedOrder(c *C, tsAll []*state.TaskSet, snaps ...string) {
	matched := 0
	markSeeded := 0
	markPreseeded := 0
	markPreseededWaitingForAliases := 0

	for _, ts := range tsAll {
		for _, t := range ts.Tasks() {
			switch t.Kind() {
			case "run-hook":
				// ensure that hooks are run after mark-preseeded
				c.Check(markPreseededInWaitChain(t), Equals, true)
			case "mark-seeded":
				// nothing waits for mark-seeded
				c.Check(t.HaltTasks(), HasLen, 0)
				markSeeded++
				c.Check(markPreseededInWaitChain(t), Equals, true)
			case "mark-preseeded":
				for _, wt := range t.WaitTasks() {
					if wt.Kind() == "setup-aliases" {
						markPreseededWaitingForAliases++
					}
				}
				markPreseeded++
			}
		}
	}

	c.Check(markSeeded, Equals, 1)
	c.Check(markPreseeded, Equals, 1)
	c.Check(markPreseededWaitingForAliases, Equals, len(snaps))

	// check that prerequisites tasks for all snaps are present and
	// are chained properly.
	for i, ts := range tsAll {
		task0 := ts.Tasks()[0]
		waitTasks := task0.WaitTasks()
		if task0.Kind() != "prerequisites" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(task0)
		c.Assert(err, IsNil, Commentf("%#v", task0))
		c.Check(snapsup.InstanceName(), Equals, snaps[matched])
		matched++
		if i == 0 {
			c.Check(waitTasks, HasLen, 0)
		} else {
			c.Assert(waitTasks, HasLen, 1)
			// prerequisites task waits for setup-aliases of previous snap
			c.Check(waitTasks[0].Kind(), Equals, "setup-aliases")
		}
	}

	c.Check(matched, Equals, len(snaps))
}

func (s *firstbootPreseed16Suite) SetUpTest(c *C) {
	s.TestingSeed16 = &seedtest.TestingSeed16{}
	s.setup16BaseTest(c, &s.firstBootBaseTest)

	s.SeedDir = dirs.SnapSeedDir

	err := os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "assertions"), 0755)
	c.Assert(err, IsNil)
}

func (s *firstbootPreseed16Suite) TestPreseedHappy(c *C) {
	restore := release.MockPreseedMode(func() bool { return true })
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	s.startOverlord(c)
	st := s.overlord.State()
	opts := &devicestate.PopulateStateFromSeedOptions{Preseed: true}
	chg := s.makeSeedChange(c, st, opts, checkPreseedTasks, checkPreseedOrder)
	err := s.overlord.Settle(settleTimeout)

	st.Lock()
	defer st.Unlock()

	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	checkPressedTaskStates(c, st)
}
