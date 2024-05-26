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
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
)

type firstbootPreseedingClassic16Suite struct {
	firstBootBaseTest
	firstBoot16BaseTest
}

var _ = Suite(&firstbootPreseedingClassic16Suite{})

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
		doneTasks["update-gadget-cmdline"] = true
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

	// validity: check that doneTasks is not declaring more tasks than
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
		hookEdgeTask := mylog.Check2(ts.Edge(snapstate.HooksEdge))

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
					snapsup := mylog.Check2(snapstate.TaskSnapSetup(wt))
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

		snapsup := mylog.Check2(snapstate.TaskSnapSetup(task0))
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

func (s *firstbootPreseedingClassic16Suite) SetUpTest(c *C) {
	s.TestingSeed16 = &seedtest.TestingSeed16{}
	s.setup16BaseTest(c, &s.firstBootBaseTest)

	s.SeedDir = dirs.SnapSeedDir
	mylog.Check(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "assertions"), 0755))


	s.AddCleanup(interfaces.MockSystemKey(`{"core": "123"}`))
	c.Assert(interfaces.WriteSystemKey(), IsNil)

	restoreRelease := release.MockOnClassic(true)
	s.AddCleanup(restoreRelease)

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	s.AddCleanup(mockMountCmd.Restore)

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	s.AddCleanup(mockUmountCmd.Restore)
}

func (s *firstbootPreseedingClassic16Suite) TestPreseedOnClassicHappy(c *C) {
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	// precondition
	c.Assert(release.OnClassic, Equals, true)

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
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))


	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	checkPreseedOrder(c, tsAll, "core", "foo", "bar")

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()


	c.Assert(chg.Err(), IsNil)

	checkPreseedTaskStates(c, st)
	c.Check(chg.Status(), Equals, state.DoingStatus)

	// verify
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	diskState := mylog.Check2(state.ReadState(nil, r))


	diskState.Lock()
	defer diskState.Unlock()

	// seeded snaps are installed
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "core"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "foo"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "bar"))
	c.Check(err, IsNil)

	// but we're not considered seeded
	var seeded bool
	mylog.Check(diskState.Get("seeded", &seeded))
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *firstbootPreseedingClassic16Suite) TestPreseedClassicWithSnapdOnlyHappy(c *C) {
	restorePreseedMode := snapdenv.MockPreseeding(true)
	defer restorePreseedMode()

	// precondition
	c.Assert(release.OnClassic, Equals, true)

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
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))


	// now run the change and check the result
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)
	c.Assert(chg.Err(), IsNil)

	checkPreseedOrder(c, tsAll, "snapd", "core18", "foo")

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()


	checkPreseedTaskStates(c, st)
	c.Check(chg.Status(), Equals, state.DoingStatus)

	// verify
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	diskState := mylog.Check2(state.ReadState(nil, r))


	diskState.Lock()
	defer diskState.Unlock()

	// seeded snaps are installed
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "snapd"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "core18"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "foo"))
	c.Check(err, IsNil)

	// but we're not considered seeded
	var seeded bool
	mylog.Check(diskState.Get("seeded", &seeded))
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *firstbootPreseedingClassic16Suite) TestPopulatePreseedWithConnectHook(c *C) {
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	// precondition
	c.Assert(release.OnClassic, Equals, true)

	hooksCalled := []*hookstate.Context{}
	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()

		hooksCalled = append(hooksCalled, ctx)
		return nil, nil
	})
	defer restore()

	core18Fname, snapdFname, _, _ := s.makeCore18Snaps(c, &core18SnapsOpts{
		classic: true,
	})

	snapFilesWithHook := [][]string{
		{"bin/bar", ``},
		{"meta/hooks/connect-plug-network", ``},
	}

	// put a firstboot snap into the SnapBlobDir
	snapYaml = `name: foo
base: core18
version: 1.0
plugs:
 shared-data-plug:
  interface: content
  target: import
  content: mylib
apps:
 bar:
  command: bin/bar
  plugs: [network]
`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, snapFilesWithHook, snap.R(128), "developerid")
	s.WriteAssertions("foo.asserts", s.devAcct, fooRev, fooDecl)

	// put a 2nd firstboot snap into the SnapBlobDir
	snapYaml = `name: bar
base: core18
version: 1.0
slots:
 shared-data-slot:
  interface: content
  content: mylib
  read:
   - /
apps:
 bar:
  command: bin/bar
`
	snapFiles := [][]string{
		{"bin/bar", ``},
	}
	barFname, barDecl, barRev := s.MakeAssertedSnap(c, snapYaml, snapFiles, snap.R(65), "developerid")
	s.WriteAssertions("bar.asserts", s.devAcct, barDecl, barRev)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: core18
   file: %s
 - name: foo
   file: %s
 - name: bar
   file: %s
`, snapdFname, core18Fname, fooFname, barFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))

	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	checkPreseedOrder(c, tsAll, "snapd", "core18", "foo", "bar")

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoingStatus)
	st.Unlock()

	// we are done with this instance of the overlord, stop it here. Otherwise
	// it will interfere with our second overlord instance
	c.Assert(s.overlord.Stop(), IsNil)

	// Verify state between the two change runs
	st.Lock()
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	diskState := mylog.Check2(state.ReadState(nil, r))


	diskState.Lock()
	defer diskState.Unlock()

	// seeded snaps are installed
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "snapd"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "core18"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "foo"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(diskState, "bar"))
	c.Check(err, IsNil)

	// but we're not considered seeded
	var seeded bool
	mylog.Check(diskState.Get("seeded", &seeded))
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	// For the next step of the test, we want to turn off pre-seeding so
	// we can run the change all the way through, and also see the hooks go
	// off.
	restore = snapdenv.MockPreseeding(false)
	defer restore()

	// Create a new overlord after turning pre-seeding off to run the
	// change fully through, which we cannot do in pre-seed mode. To actually
	// invoke the hooks we have to restart the overlord.
	s.startOverlord(c)
	st = s.overlord.State()
	st.Lock()
	defer st.Unlock()

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()


	restart.MockPending(st, restart.RestartUnset)
	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))

	c.Assert(s.overlord.Stop(), IsNil)

	st.Lock()

	// Update the change pointer to the change in the new state
	// otherwise we will be referring to the old one.
	chg = nil
	for _, c := range st.Changes() {
		if c.Kind() == "seed" {
			chg = c
			break
		}
	}
	c.Assert(chg, NotNil)
	c.Assert(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	c.Assert(hooksCalled, HasLen, 1)
	c.Check(hooksCalled[0].HookName(), Equals, "connect-plug-network")
	mylog.

		// and ensure state is now considered seeded
		Check(st.Get("seeded", &seeded))

	c.Check(seeded, Equals, true)

	// check we set seed-time
	var seedTime time.Time
	mylog.Check(st.Get("seed-time", &seedTime))

	c.Check(seedTime.IsZero(), Equals, false)

	// verify that connections was made
	var conns map[string]interface{}
	c.Assert(st.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"foo:network core:network": map[string]interface{}{
			"auto": true, "interface": "network",
		},
		"foo:shared-data-plug bar:shared-data-slot": map[string]interface{}{
			"auto": true, "interface": "content",
			"plug-static": map[string]interface{}{
				"content": "mylib", "target": "import",
			},
			"slot-static": map[string]interface{}{
				"content": "mylib",
				"read": []interface{}{
					"/",
				},
			},
		},
	})
}
