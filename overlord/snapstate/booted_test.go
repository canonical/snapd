// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// test the boot related code

import (
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type bootedSuite struct {
	testutil.BaseTest
	bootloader *boottest.MockBootloader

	o           *overlord.Overlord
	state       *state.State
	snapmgr     *snapstate.SnapManager
	fakeBackend *fakeSnappyBackend
	restore     func()
}

var _ = Suite(&bootedSuite{})

func (bs *bootedSuite) SetUpTest(c *C) {
	bs.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)

	bs.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	// booted is not running on classic
	release.MockOnClassic(false)

	bs.bootloader = boottest.NewMockBootloader("mock", c.MkDir())
	bs.bootloader.BootVars["snap_core"] = "core_2.snap"
	bs.bootloader.BootVars["snap_kernel"] = "canonical-pc-linux_2.snap"
	partition.ForceBootloader(bs.bootloader)

	bs.fakeBackend = &fakeSnappyBackend{}
	bs.o = overlord.Mock()
	bs.state = bs.o.State()
	bs.snapmgr, err = snapstate.Manager(bs.state)
	c.Assert(err, IsNil)
	bs.snapmgr.AddForeignTaskHandlers(bs.fakeBackend)

	bs.o.AddManager(bs.snapmgr)

	snapstate.SetSnapManagerBackend(bs.snapmgr, bs.fakeBackend)
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	snapstate.MockModel()
}

func (bs *bootedSuite) TearDownTest(c *C) {
	bs.BaseTest.TearDownTest(c)
	snapstate.AutoAliases = nil
	release.MockOnClassic(true)
	dirs.SetRootDir("")
	partition.ForceBootloader(nil)
}

var osSI1 = &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
var osSI2 = &snap.SideInfo{RealName: "core", Revision: snap.R(2)}
var kernelSI1 = &snap.SideInfo{RealName: "canonical-pc-linux", Revision: snap.R(1)}
var kernelSI2 = &snap.SideInfo{RealName: "canonical-pc-linux", Revision: snap.R(2)}

func (bs *bootedSuite) settle() {
	bs.o.Settle(5 * time.Second)
}

func (bs *bootedSuite) makeInstalledKernelOS(c *C, st *state.State) {
	snaptest.MockSnap(c, "name: core\ntype: os\nversion: 1", osSI1)
	snaptest.MockSnap(c, "name: core\ntype: os\nversion: 2", osSI2)
	snapstate.Set(st, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{osSI1, osSI2},
		Current:  snap.R(2),
	})

	snaptest.MockSnap(c, "name: canonical-pc-linux\ntype: os\nversion: 1", kernelSI1)
	snaptest.MockSnap(c, "name: canonical-pc-linux\ntype: os\nversion: 2", kernelSI2)
	snapstate.Set(st, "canonical-pc-linux", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{kernelSI1, kernelSI2},
		Current:  snap.R(2),
	})

}

func (bs *bootedSuite) TestUpdateBootRevisionsOSSimple(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.BootVars["snap_core"] = "core_1.snap"
	err := snapstate.UpdateBootRevisions(st)
	c.Assert(err, IsNil)

	st.Unlock()
	bs.settle()
	st.Lock()

	c.Assert(st.Changes(), HasLen, 1)
	chg := st.Changes()[0]
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Kind(), Equals, "update-revisions")
	c.Assert(chg.IsReady(), Equals, true)

	// core "current" got reverted but canonical-pc-linux did not
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "core", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(1))
	c.Assert(snapst.Active, Equals, true)

	err = snapstate.Get(st, "canonical-pc-linux", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.Active, Equals, true)
}

func (bs *bootedSuite) TestUpdateBootRevisionsKernelSimple(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.BootVars["snap_kernel"] = "canonical-pc-linux_1.snap"
	err := snapstate.UpdateBootRevisions(st)
	c.Assert(err, IsNil)

	st.Unlock()
	bs.settle()
	st.Lock()

	c.Assert(st.Changes(), HasLen, 1)
	chg := st.Changes()[0]
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Kind(), Equals, "update-revisions")
	c.Assert(chg.IsReady(), Equals, true)

	// canonical-pc-linux "current" got reverted but core did not
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "canonical-pc-linux", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(1))
	c.Assert(snapst.Active, Equals, true)

	err = snapstate.Get(st, "core", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.Active, Equals, true)
}

func (bs *bootedSuite) TestUpdateBootRevisionsKernelErrorsEarly(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.BootVars["snap_kernel"] = "canonical-pc-linux_99.snap"
	err := snapstate.UpdateBootRevisions(st)
	c.Assert(err, ErrorMatches, `cannot find revision 99 for snap "canonical-pc-linux"`)
}

func (bs *bootedSuite) TestUpdateBootRevisionsOSErrorsEarly(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.BootVars["snap_core"] = "core_99.snap"
	err := snapstate.UpdateBootRevisions(st)
	c.Assert(err, ErrorMatches, `cannot find revision 99 for snap "core"`)
}

func (bs *bootedSuite) TestUpdateBootRevisionsOSErrorsLate(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	// have a kernel
	snaptest.MockSnap(c, "name: canonical-pc-linux\ntype: os\nversion: 2", kernelSI2)
	snapstate.Set(st, "canonical-pc-linux", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{kernelSI2},
		Current:  snap.R(2),
	})

	// put core into the state but add no files on disk
	// will break in the tasks
	snapstate.Set(st, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{osSI1, osSI2},
		Current:  snap.R(2),
	})
	bs.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/core/1")

	bs.bootloader.BootVars["snap_core"] = "core_1.snap"
	err := snapstate.UpdateBootRevisions(st)
	c.Assert(err, IsNil)

	st.Unlock()
	bs.settle()
	st.Lock()

	c.Assert(st.Changes(), HasLen, 1)
	chg := st.Changes()[0]
	c.Assert(chg.Kind(), Equals, "update-revisions")
	c.Assert(chg.IsReady(), Equals, true)
	c.Assert(chg.Err(), ErrorMatches, `(?ms).*Make snap "core" \(1\) available to the system \(fail\).*`)
}

func (bs *bootedSuite) TestNameAndRevnoFromSnapValid(c *C) {
	name, revno, err := snapstate.NameAndRevnoFromSnap("foo_2.snap")
	c.Assert(err, IsNil)
	c.Assert(name, Equals, "foo")
	c.Assert(revno, Equals, snap.R(2))
}

func (bs *bootedSuite) TestNameAndRevnoFromSnapInvalidFormat(c *C) {
	_, _, err := snapstate.NameAndRevnoFromSnap("invalid")
	c.Assert(err, ErrorMatches, `input "invalid" has invalid format \(not enough '_'\)`)
}

func (bs *bootedSuite) TestCurrentBootNameAndRevision(c *C) {
	name, revision, err := snapstate.CurrentBootNameAndRevision(snap.TypeOS)
	c.Check(err, IsNil)
	c.Check(name, Equals, "core")
	c.Check(revision, Equals, snap.R(2))

	name, revision, err = snapstate.CurrentBootNameAndRevision(snap.TypeKernel)
	c.Check(err, IsNil)
	c.Check(name, Equals, "canonical-pc-linux")
	c.Check(revision, Equals, snap.R(2))

	bs.bootloader.BootVars["snap_mode"] = "trying"
	_, _, err = snapstate.CurrentBootNameAndRevision(snap.TypeKernel)
	c.Check(err, Equals, snapstate.ErrBootNameAndRevisionAgain)
}

func (bs *bootedSuite) TestCurrentBootNameAndRevisionUnhappy(c *C) {
	delete(bs.bootloader.BootVars, "snap_kernel")
	_, _, err := snapstate.CurrentBootNameAndRevision(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot retrieve boot revision for kernel: unset")

	delete(bs.bootloader.BootVars, "snap_core")
	_, _, err = snapstate.CurrentBootNameAndRevision(snap.TypeOS)
	c.Check(err, ErrorMatches, "cannot retrieve boot revision for core: unset")

	delete(bs.bootloader.BootVars, "snap_core")
	_, _, err = snapstate.CurrentBootNameAndRevision(snap.TypeBase)
	c.Check(err, ErrorMatches, "cannot retrieve boot revision for base: unset")

}

func (bs *bootedSuite) TestWaitRestartCore(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	// not core snap
	err := snapstate.WaitRestart(task, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "some-app",
		},
	}, &snap.Info{Type: snap.TypeApp})
	c.Check(err, IsNil)

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "core",
		},
	}

	// core snap, restarting ... wait
	state.MockRestarting(st, state.RestartSystem)
	err = snapstate.WaitRestart(task, snapsup, &snap.Info{Type: snap.TypeOS})
	c.Check(err, FitsTypeOf, &state.Retry{})

	// core snap, restarted, waiting for current core revision
	state.MockRestarting(st, state.RestartUnset)
	bs.bootloader.BootVars["snap_mode"] = "trying"
	err = snapstate.WaitRestart(task, snapsup, &snap.Info{Type: snap.TypeOS})
	c.Check(err, DeepEquals, &state.Retry{After: 5 * time.Second})

	// core snap, restarted, right core revision, no rollback
	bs.bootloader.BootVars["snap_mode"] = ""
	err = snapstate.WaitRestart(task, snapsup, &snap.Info{Type: snap.TypeOS, SideInfo: snap.SideInfo{Revision: snap.R(2)}})
	c.Check(err, IsNil)

	// core snap, restarted, wrong core revision, rollback!
	bs.bootloader.BootVars["snap_core"] = "core_1.snap"
	err = snapstate.WaitRestart(task, snapsup, &snap.Info{Type: snap.TypeOS, SideInfo: snap.SideInfo{Revision: snap.R(2)}})
	c.Check(err, ErrorMatches, `cannot finish core installation, there was a rollback across reboot`)
}

func (bs *bootedSuite) TestWaitRestartBootableBase(c *C) {
	restore := snapstate.MockModelWithBase("core18")
	defer restore()

	st := bs.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	// not core snap
	err := snapstate.WaitRestart(task, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "some-app",
		},
	}, &snap.Info{Type: snap.TypeApp})
	c.Check(err, IsNil)

	// core snap but we are on a model with a different base
	err = snapstate.WaitRestart(task, &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "core",
		},
	}, &snap.Info{Type: snap.TypeOS})
	c.Check(err, IsNil)

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "core18",
		},
	}

	// core snap, restarting ... wait
	state.MockRestarting(st, state.RestartSystem)
	err = snapstate.WaitRestart(task, snapsup, &snap.Info{Type: snap.TypeApp})
	c.Check(err, FitsTypeOf, &state.Retry{})

	// core snap, restarted, waiting for current core revision
	state.MockRestarting(st, state.RestartUnset)
	bs.bootloader.BootVars["snap_mode"] = "trying"
	err = snapstate.WaitRestart(task, snapsup, &snap.Info{Type: snap.TypeApp})
	c.Check(err, DeepEquals, &state.Retry{After: 5 * time.Second})

	// core snap, restarted, right core revision, no rollback
	bs.bootloader.BootVars["snap_mode"] = ""
	bs.bootloader.BootVars["snap_core"] = "core18_2.snap"
	err = snapstate.WaitRestart(task, snapsup, &snap.Info{Type: snap.TypeApp, SideInfo: snap.SideInfo{Revision: snap.R(2)}})
	c.Check(err, IsNil)

	// core snap, restarted, wrong core revision, rollback!
	bs.bootloader.BootVars["snap_core"] = "core18_1.snap"
	err = snapstate.WaitRestart(task, snapsup, &snap.Info{Type: snap.TypeApp, SideInfo: snap.SideInfo{Revision: snap.R(2)}})
	c.Check(err, ErrorMatches, `cannot finish core18 installation, there was a rollback across reboot`)
}
