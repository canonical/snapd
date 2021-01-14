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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
)

type firstbootPreseed16Suite struct {
	firstBootBaseTest
	firstBoot16BaseTest
}

var _ = Suite(&firstbootPreseed16Suite{})

func checkPreseedTasks(c *C, tsAll []*state.TaskSet) {
	// the tasks of the last taskset must be mark-preseeded, mark-seeded, in that order
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

func checkPreseedTaskStates(c *C, st *state.State) {
	doneTasks := map[string]bool{
		"prerequisites":    true,
		"prepare-snap":     true,
		"link-snap":        true,
		"mount-snap":       true,
		"setup-profiles":   true,
		"copy-snap-data":   true,
		"set-auto-aliases": true,
		"setup-aliases":    true,
		"auto-connect":     true,
	}
	if !release.OnClassic {
		doneTasks["update-gadget-assets"] = true
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
	var prevTask *state.Task
	for i, ts := range tsAll {
		task0 := ts.Tasks()[0]
		waitTasks := task0.WaitTasks()
		// all tasksets start with prerequisites task, except for
		// tasksets with just the configure hook of special snaps,
		// or last taskset.
		if task0.Kind() != "prerequisites" {
			if i == len(tsAll)-1 {
				c.Check(task0.Kind(), Equals, "mark-preseeded")
				c.Check(ts.Tasks()[1].Kind(), Equals, "mark-seeded")
				c.Check(ts.Tasks(), HasLen, 2)
			} else {
				c.Check(task0.Kind(), Equals, "run-hook")
				var hsup hookstate.HookSetup
				c.Assert(task0.Get("hook-setup", &hsup), IsNil)
				c.Check(hsup.Hook, Equals, "configure")
				c.Check(ts.Tasks(), HasLen, 1)
			}
			continue
		}

		if i == 0 {
			c.Check(waitTasks, HasLen, 0)
		} else {
			c.Assert(waitTasks, HasLen, 1)
			c.Assert(waitTasks[0].Kind(), Equals, prevTask.Kind())
			c.Check(waitTasks[0], Equals, prevTask)
		}

		// make sure that install-hooks wait for the previous snap, and for
		// mark-preseeded.
		hookEdgeTask, err := ts.Edge(snapstate.HooksEdge)
		c.Assert(err, IsNil)
		c.Assert(hookEdgeTask.Kind(), Equals, "run-hook")
		var hsup hookstate.HookSetup
		c.Assert(hookEdgeTask.Get("hook-setup", &hsup), IsNil)
		c.Check(hsup.Hook, Equals, "install")
		switch hsup.Snap {
		case "core", "core18", "snapd":
			// ignore
		default:
			// snaps other than core/core18/snapd
			var waitsForMarkPreseeded, waitsForPreviousSnapHook, waitsForPreviousSnap bool
			for _, wt := range hookEdgeTask.WaitTasks() {
				switch wt.Kind() {
				case "setup-aliases":
					continue
				case "run-hook":
					var wtsup hookstate.HookSetup
					c.Assert(wt.Get("hook-setup", &wtsup), IsNil)
					c.Check(wtsup.Snap, Equals, snaps[matched-1])
					waitsForPreviousSnapHook = true
				case "mark-preseeded":
					waitsForMarkPreseeded = true
				case "prerequisites":
				default:
					snapsup, err := snapstate.TaskSnapSetup(wt)
					c.Assert(err, IsNil, Commentf("%#v", wt))
					c.Check(snapsup.SnapName(), Equals, snaps[matched-1], Commentf("%s: %#v", hsup.Snap, wt))
					waitsForPreviousSnap = true
				}
			}
			c.Assert(waitsForMarkPreseeded, Equals, true)
			c.Assert(waitsForPreviousSnapHook, Equals, true)
			if snaps[matched-1] != "core" && snaps[matched-1] != "core18" && snaps[matched-1] != "pc" {
				c.Check(waitsForPreviousSnap, Equals, true, Commentf("%s", snaps[matched-1]))
			}
		}

		snapsup, err := snapstate.TaskSnapSetup(task0)
		c.Assert(err, IsNil, Commentf("%#v", task0))
		c.Check(snapsup.InstanceName(), Equals, snaps[matched])
		matched++

		// find setup-aliases task in current taskset; its position
		// is not fixed due to e.g. optional update-gadget-assets task.
		var aliasesTask *state.Task
		for _, t := range ts.Tasks() {
			if t.Kind() == "setup-aliases" {
				aliasesTask = t
				break
			}
		}
		c.Assert(aliasesTask, NotNil)
		prevTask = aliasesTask
	}

	c.Check(matched, Equals, len(snaps))
}

func (s *firstbootPreseed16Suite) SetUpTest(c *C) {
	s.TestingSeed16 = &seedtest.TestingSeed16{}
	s.setup16BaseTest(c, &s.firstBootBaseTest)

	s.SeedDir = dirs.SnapSeedDir

	err := os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "assertions"), 0755)
	c.Assert(err, IsNil)

	s.AddCleanup(interfaces.MockSystemKey(`{"core": "123"}`))
	c.Assert(interfaces.WriteSystemKey(), IsNil)
}

func (s *firstbootPreseed16Suite) TestPreseedHappy(c *C) {
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	s.startOverlord(c)
	st := s.overlord.State()
	opts := &devicestate.PopulateStateFromSeedOptions{Preseed: true}
	chg, _ := s.makeSeedChange(c, st, opts, checkPreseedTasks, checkPreseedOrder)
	err := s.overlord.Settle(settleTimeout)

	st.Lock()
	defer st.Unlock()

	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	checkPreseedTaskStates(c, st)
}

func (s *firstbootPreseed16Suite) TestPreseedOnClassicHappy(c *C) {
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	restoreRelease := release.MockOnClassic(true)
	defer restoreRelease()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	coreFname, _, _ := s.makeCoreSnaps(c, "")

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0
`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("foo.asserts", s.devAcct, fooRev, fooDecl)

	// put a firstboot snap into the SnapBlobDir
	snapYaml2 := `name: bar
version: 1.0
`
	barFname, barDecl, barRev := s.MakeAssertedSnap(c, snapYaml2, nil, snap.R(33), "developerid")
	s.WriteAssertions("bar.asserts", s.devAcct, barRev, barDecl)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: foo
   file: %s
 - name: bar
   file: %s
 - name: core
   file: %s
`, fooFname, barFname, coreFname))
	err := ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	opts := &devicestate.PopulateStateFromSeedOptions{Preseed: true}
	tsAll, err := devicestate.PopulateStateFromSeedImpl(st, opts, s.perfTimings)
	c.Assert(err, IsNil)

	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	checkPreseedOrder(c, tsAll, "core", "foo", "bar")

	st.Unlock()
	err = s.overlord.Settle(settleTimeout)
	st.Lock()

	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	checkPreseedTaskStates(c, st)
	c.Check(chg.Status(), Equals, state.DoingStatus)

	// verify
	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	diskState, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	diskState.Lock()
	defer diskState.Unlock()

	// seeded snaps are installed
	_, err = snapstate.CurrentInfo(diskState, "core")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(diskState, "foo")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(diskState, "bar")
	c.Check(err, IsNil)

	// but we're not considered seeded
	var seeded bool
	err = diskState.Get("seeded", &seeded)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *firstbootPreseed16Suite) TestPreseedClassicWithSnapdOnlyHappy(c *C) {
	restorePreseedMode := snapdenv.MockPreseeding(true)
	defer restorePreseedMode()

	restore := release.MockOnClassic(true)
	defer restore()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	core18Fname, snapdFname, _, _ := s.makeCore18Snaps(c, &core18SnapsOpts{
		classic: true,
	})

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0
base: core18
`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("foo.asserts", s.devAcct, fooRev, fooDecl)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: foo
   file: %s
 - name: core18
   file: %s
`, snapdFname, fooFname, core18Fname))
	err := ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	opts := &devicestate.PopulateStateFromSeedOptions{Preseed: true}
	tsAll, err := devicestate.PopulateStateFromSeedImpl(st, opts, s.perfTimings)
	c.Assert(err, IsNil)

	// now run the change and check the result
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)
	c.Assert(chg.Err(), IsNil)

	checkPreseedOrder(c, tsAll, "snapd", "core18", "foo")

	st.Unlock()
	err = s.overlord.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	checkPreseedTaskStates(c, st)
	c.Check(chg.Status(), Equals, state.DoingStatus)

	// verify
	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	diskState, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	diskState.Lock()
	defer diskState.Unlock()

	// seeded snaps are installed
	_, err = snapstate.CurrentInfo(diskState, "snapd")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(diskState, "core18")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(diskState, "foo")
	c.Check(err, IsNil)

	// but we're not considered seeded
	var seeded bool
	err = diskState.Get("seeded", &seeded)
	c.Assert(err, Equals, state.ErrNoState)
}
