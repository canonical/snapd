// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
)

type regenerateKernelDriversTreeSuite struct {
	baseHandlerSuite
}

var _ = Suite(&regenerateKernelDriversTreeSuite{})

func (s *regenerateKernelDriversTreeSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(MakeModel20("gadget", map[string]any{"base": "core24"})))
	s.AddCleanup(osutil.MockMountInfo(""))
	// ensureMountsUpdated (a real, unfaked SnapManager.Ensure() step) walks
	// every installed snap and talks to systemd directly; these tests
	// install real SnapState entries and some of them call the real
	// Ensure(), so systemctl needs to be mocked to avoid touching the
	// real system (this was previously triggering a real
	// "systemctl daemon-reload" attempt).
	s.AddCleanup(systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		return []byte(""), nil
	}))
}

// setUpKernel installs a minimal kernel snap "kernel" rev 3 in state.
func (s *regenerateKernelDriversTreeSuite) setUpKernel(c *C) *snap.Info {
	sideInfo := &snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(3),
	}
	info := snaptest.MockSnap(c, `
name: kernel
type: kernel
version: v1
`, sideInfo)

	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  sideInfo.Revision,
	})
	return info
}

func changesOfKind(st *state.State, kind string) []*state.Change {
	var found []*state.Change
	for _, chg := range st.Changes() {
		if chg.Kind() == kind {
			found = append(found, chg)
		}
	}
	return found
}

func (s *regenerateKernelDriversTreeSuite) TestDoRegenerateKernelDriversTree(c *C) {
	s.state.Lock()

	sideInfo := &snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(3),
	}
	snaptest.MockSnap(c, `
name: kernel
type: kernel
version: v1
`, sideInfo)

	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  sideInfo.Revision,
	})

	t := s.state.NewTask("regenerate-kernel-drivers-tree", "test check kernel drivers tree")
	chg := s.state.NewChange("regenerate-kernel-drivers-tree", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)

	// The handler re-derives the current kernel + its (empty, here)
	// active kernel-modules components live at execution time and calls
	// the backend to check/regenerate the drivers tree.
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "prepare-kernel-snap",
		},
	})
}

// 1. Not seeded yet: no change, no error.
func (s *regenerateKernelDriversTreeSuite) TestEnsureNotSeededNoChange(c *C) {
	s.state.Lock()
	info := s.setUpKernel(c)
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	c.Assert(os.MkdirAll(destDir, 0755), IsNil)
	// deliberately not setting "seeded"
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 0)
}

// 2. Model unknown yet: no change, no error.
func (s *regenerateKernelDriversTreeSuite) TestEnsureModelUnknownNoChange(c *C) {
	s.AddCleanup(snapstatetest.MockDeviceModel(nil))

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 0)
}

// 3. kernel.NeedsKernelDriversTree(model) == false (e.g. a classic model):
// no change created, even with a stale/missing marker.
func (s *regenerateKernelDriversTreeSuite) TestEnsureNeedsKernelDriversTreeFalseNoChange(c *C) {
	s.AddCleanup(snapstatetest.MockDeviceModel(ClassicModel()))

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 0)
}

// 4. No kernel snap installed: no change, no error.
func (s *regenerateKernelDriversTreeSuite) TestEnsureNoKernelInstalledNoChange(c *C) {
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 0)
}

// 5. Kernel installed, but its drivers tree directory doesn't exist at all:
// no change created (nothing to check against).
func (s *regenerateKernelDriversTreeSuite) TestEnsureDestDirMissingNoChange(c *C) {
	s.state.Lock()
	s.setUpKernel(c)
	s.state.Set("seeded", true)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 0)
}

// 6. Kernel installed, tree exists, marker present and current: no change.
func (s *regenerateKernelDriversTreeSuite) TestEnsureMarkerUpToDateNoChange(c *C) {
	s.state.Lock()
	info := s.setUpKernel(c)
	s.state.Set("seeded", true)
	s.state.Unlock()

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	mountDir := c.MkDir()
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{Current: mountDir, Target: mountDir},
		nil, destDir, &kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 0)
}

// 7. Kernel installed, tree exists, marker missing entirely (simulates a
// pre-existing tree from before this feature shipped): exactly one change
// created, and after running it to completion the marker ends up written
// and current.
func (s *regenerateKernelDriversTreeSuite) TestEnsureMarkerMissingCreatesChangeAndRegenerates(c *C) {
	s.state.Lock()
	info := s.setUpKernel(c)
	s.state.Set("seeded", true)
	s.state.Unlock()

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	c.Assert(os.MkdirAll(destDir, 0755), IsNil)

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	found := changesOfKind(s.state, "regenerate-kernel-drivers-tree")
	c.Check(found, HasLen, 1)
	s.state.Unlock()

	// Let the runner actually execute the task. The fake backend does not
	// perform real filesystem work, so we can only assert the state
	// machine converged (change completed without error); real marker
	// persistence is covered by the kernel package's own tests plus
	// backend's SetupKernelSnap tests.
	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(found[0].Err(), IsNil)
	c.Check(found[0].Status(), Equals, state.DoneStatus)
}

// 8. Same as 7, but the marker is present with an older generator version
// rather than fully missing.
func (s *regenerateKernelDriversTreeSuite) TestEnsureMarkerOlderVersionCreatesChangeAndRegenerates(c *C) {
	s.state.Lock()
	info := s.setUpKernel(c)
	s.state.Set("seeded", true)
	s.state.Unlock()

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	c.Assert(os.MkdirAll(destDir, 0755), IsNil)
	c.Assert(os.WriteFile(destDir+"/kernel.json", []byte(`{"generator-version":0}`), 0644), IsNil)

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	found := changesOfKind(s.state, "regenerate-kernel-drivers-tree")
	c.Check(found, HasLen, 1)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(found[0].Err(), IsNil)
	c.Check(found[0].Status(), Equals, state.DoneStatus)
}

// 9. changeInFlight guard: an unrelated change in progress and not yet
// Ready() defers the check entirely; once it completes, the check runs
// (assuming the marker is still stale).
func (s *regenerateKernelDriversTreeSuite) TestEnsureChangeInFlightGuard(c *C) {
	s.state.Lock()
	info := s.setUpKernel(c)
	s.state.Set("seeded", true)
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	c.Assert(os.MkdirAll(destDir, 0755), IsNil)

	// An unrelated change, unrelated to the kernel snap, still in flight.
	t := s.state.NewTask("nop", "unrelated task")
	unrelatedChg := s.state.NewChange("unrelated-change", "unrelated")
	unrelatedChg.AddTask(t)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 0)

	// Let the unrelated change complete.
	t.SetStatus(state.DoneStatus)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 1)
}

// 10. CheckChangeConflict guard: a real in-flight change already touches
// the kernel snap itself (e.g. a pending kernel-modules-component
// operation): no change created while it's in flight; once it completes,
// the check does get created. Note this scenario is also covered by the
// coarser changeInFlight guard (9) in practice, since a not-yet-Ready
// change of any kind blocks both; this test specifically targets the
// kernel-snap-scoped CheckChangeConflict mechanism to document it as an
// independent, defense-in-depth layer.
func (s *regenerateKernelDriversTreeSuite) TestEnsureCheckChangeConflictGuard(c *C) {
	s.state.Lock()
	info := s.setUpKernel(c)
	s.state.Set("seeded", true)
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	c.Assert(os.MkdirAll(destDir, 0755), IsNil)

	// A conflicting change that touches the kernel snap itself (e.g. a
	// pending kernel-modules-component install/remove).
	t := s.state.NewTask("nop", "conflicting kernel-affecting task")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &info.SideInfo,
		Type:     snap.TypeKernel,
	})
	conflictingChg := s.state.NewChange("kernel-modules-setup", "conflicting")
	conflictingChg.AddTask(t)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 0)

	// Let the conflicting change complete.
	t.SetStatus(state.DoneStatus)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 1)
}

// 11. Self-conflict / no duplicate launches: calling the Ensure() logic
// again while our own regenerate-kernel-drivers-tree change is still in
// flight (task not yet run) must not create a second one. Note: with the
// ensureKernelRegenerateDone per-process cache (set as soon as a change is
// successfully launched, or as soon as the tree is found to already be
// up to date), the second call below is actually short-circuited by that
// cache before it would even reach CheckChangeConflict again - see
// TestEnsureKernelRegenerateRunsOnlyOncePerManagerLifetime below for a test
// that isolates the cache's effect specifically, independent of
// CheckChangeConflict/changeInFlight. The observable guarantee this test
// checks (no duplicate launches) still holds either way.
func (s *regenerateKernelDriversTreeSuite) TestEnsureNoDuplicateSelfConflict(c *C) {
	s.state.Lock()
	info := s.setUpKernel(c)
	s.state.Set("seeded", true)
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	c.Assert(os.MkdirAll(destDir, 0755), IsNil)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)
	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 1)
}

// 11b. ensureKernelRegenerateDone per-process cache, isolated from
// CheckChangeConflict/changeInFlight: once a change has been launched (or
// the tree found to already be up to date), a SnapManager instance never
// attempts the check again for the rest of its lifetime (i.e. until
// snapd itself restarts and a fresh SnapManager is created) - even if the
// on-disk marker would otherwise still look stale and nothing else is in
// flight to block a relaunch via the other guards.
func (s *regenerateKernelDriversTreeSuite) TestEnsureKernelRegenerateRunsOnlyOncePerManagerLifetime(c *C) {
	s.state.Lock()
	info := s.setUpKernel(c)
	s.state.Set("seeded", true)
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	c.Assert(os.MkdirAll(destDir, 0755), IsNil)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	found := changesOfKind(s.state, "regenerate-kernel-drivers-tree")
	c.Check(found, HasLen, 1)

	// Mark the launched change's task as already done, without actually
	// running it (so the on-disk marker stays missing/stale, exactly as
	// if the real task had silently failed to persist it). This removes
	// the change from changeInFlight/CheckChangeConflict's view entirely,
	// so a second call below would, if the per-process cache did not
	// exist, incorrectly launch a duplicate change.
	for _, t := range found[0].Tasks() {
		t.SetStatus(state.DoneStatus)
	}
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 1)
}

// 12. Forward-only / revert safety: marker version is higher than the
// current generator version (simulating a snapd revert): no change
// created, so a reverted (older) snapd cannot regress an already-fixed
// tree.
func (s *regenerateKernelDriversTreeSuite) TestEnsureForwardOnlyRevertSafetyNoChange(c *C) {
	s.state.Lock()
	info := s.setUpKernel(c)
	s.state.Set("seeded", true)
	s.state.Unlock()

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	mountDir := c.MkDir()
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{Current: mountDir, Target: mountDir},
		nil, destDir, &kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// Simulate a tree built by a newer generator than what is currently
	// running (as if snapd had been reverted to an older build after a
	// fix shipped). Using an arbitrarily high version number avoids
	// needing to know the exact current generator-version constant.
	c.Assert(os.WriteFile(destDir+"/kernel.json",
		[]byte(`{"generator-version":999999}`), 0644), IsNil)

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 0)
}

// 13. A real doRegenerateKernelDriversTree task failure (backend.SetupKernelSnap
// returns an error): the task and change end up in an error state, and -
// this is the concrete regression test for the ensureKernelRegenerateDone
// one-shot-per-process behavior documented on that field and exercised only
// indirectly by TestEnsureKernelRegenerateRunsOnlyOncePerManagerLifetime above -
// a subsequent EnsureKernelDriversTreeRegenerated() call in the same process
// does not launch a second change, even though the on-disk marker is still
// stale (the failed task never got to write it).
func (s *regenerateKernelDriversTreeSuite) TestEnsureRealTaskFailureNoRelaunch(c *C) {
	s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
		if op.op == "prepare-kernel-snap" {
			return fmt.Errorf("boom: cannot set up kernel snap")
		}
		return nil
	}

	s.state.Lock()
	info := s.setUpKernel(c)
	s.state.Set("seeded", true)
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, info.InstanceName(), info.Revision)
	c.Assert(os.MkdirAll(destDir, 0755), IsNil)
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	found := changesOfKind(s.state, "regenerate-kernel-drivers-tree")
	c.Check(found, HasLen, 1)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(found[0].Err(), ErrorMatches, "(?s).*boom: cannot set up kernel snap.*")
	c.Check(found[0].Status(), Equals, state.ErrorStatus)
	for _, t := range found[0].Tasks() {
		c.Check(t.Status(), Equals, state.ErrorStatus)
	}
	s.state.Unlock()

	// A subsequent Ensure()-driven check must not relaunch, even though the
	// marker is still stale, because ensureKernelRegenerateDone was already set
	// when the (now-failed) change was launched.
	c.Assert(s.snapmgr.EnsureKernelDriversTreeRegenerated(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(changesOfKind(s.state, "regenerate-kernel-drivers-tree"), HasLen, 1)
}
