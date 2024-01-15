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
	"errors"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type bootedSuite struct {
	testutil.BaseTest

	bootloader *boottest.Bootenv16

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

	bs.bootloader = boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bs.bootloader.SetBootKernel("canonical-pc-linux_2.snap")
	bs.bootloader.SetBootBase("core_2.snap")
	bootloader.Force(bs.bootloader)

	bs.fakeBackend = &fakeSnappyBackend{}
	bs.o = overlord.Mock()
	bs.state = bs.o.State()
	bs.state.Lock()
	_, err = restart.Manager(bs.state, "boot-id-0", nil)
	bs.state.Unlock()
	c.Assert(err, IsNil)
	bs.snapmgr, err = snapstate.Manager(bs.state, bs.o.TaskRunner())
	c.Assert(err, IsNil)

	AddForeignTaskHandlers(bs.o.TaskRunner(), bs.fakeBackend)

	bs.o.AddManager(bs.snapmgr)
	bs.o.AddManager(bs.o.TaskRunner())

	c.Assert(bs.o.StartUp(), IsNil)

	snapstate.SetSnapManagerBackend(bs.snapmgr, bs.fakeBackend)
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	bs.restore = snapstatetest.MockDeviceModel(DefaultModel())

	bs.state.Lock()
	repo := interfaces.NewRepository()
	ifacerepo.Replace(bs.state, repo)
	bs.state.Unlock()

	oldSnapServiceOptions := snapstate.SnapServiceOptions
	snapstate.SnapServiceOptions = servicestate.SnapServiceOptions
	bs.AddCleanup(func() {
		snapstate.SnapServiceOptions = oldSnapServiceOptions
	})
	bs.AddCleanup(osutil.MockMountInfo(""))
}

func (bs *bootedSuite) TearDownTest(c *C) {
	bs.BaseTest.TearDownTest(c)
	snapstate.AutoAliases = nil
	bs.restore()
	release.MockOnClassic(true)
	dirs.SetRootDir("")
	bootloader.Force(nil)
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
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{sequence.NewRevisionSideState(osSI1, nil),
			sequence.NewRevisionSideState(osSI2, nil)}),
		Current: snap.R(2),
	})

	snaptest.MockSnap(c, "name: canonical-pc-linux\ntype: os\nversion: 1", kernelSI1)
	snaptest.MockSnap(c, "name: canonical-pc-linux\ntype: os\nversion: 2", kernelSI2)
	snapstate.Set(st, "canonical-pc-linux", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{sequence.NewRevisionSideState(kernelSI1, nil),
			sequence.NewRevisionSideState(kernelSI2, nil)}),
		Current: snap.R(2),
	})

}

func (bs *bootedSuite) TestUpdateBootRevisionsOSSimple(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.SetBootBase("core_1.snap")
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

	bs.bootloader.SetBootKernel("canonical-pc-linux_1.snap")
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

func (bs *bootedSuite) TestUpdateBootRevisionsDeviceCtxErrors(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	bs.makeInstalledKernelOS(c, st)

	errBoom := errors.New("boom")

	r := snapstatetest.ReplaceDeviceCtxHook(func(*state.State, *state.Task, snapstate.DeviceContext) (snapstate.DeviceContext, error) {
		return nil, errBoom
	})
	defer r()

	err := snapstate.UpdateBootRevisions(st)
	c.Assert(err, Equals, errBoom)
}

func (bs *bootedSuite) TestUpdateBootRevisionsKernelErrorsEarly(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.SetBootKernel("canonical-pc-linux_99.snap")
	err := snapstate.UpdateBootRevisions(st)
	c.Assert(err, ErrorMatches, `cannot find revision 99 for snap "canonical-pc-linux"`)
}

func (bs *bootedSuite) TestUpdateBootRevisionsOSErrorsEarly(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.SetBootBase("core_99.snap")
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
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{sequence.NewRevisionSideState(kernelSI2, nil)}),
		Current:  snap.R(2),
	})

	snaptest.MockSnap(c, "name: core\ntype: os\nversion: 1", osSI1)
	snaptest.MockSnap(c, "name: core\ntype: os\nversion: 2", osSI2)
	snapstate.Set(st, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{sequence.NewRevisionSideState(osSI1, nil),
			sequence.NewRevisionSideState(osSI2, nil)}),
		Current: snap.R(2),
	})
	bs.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/core/1")

	bs.bootloader.SetBootBase("core_1.snap")
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

func (bs *bootedSuite) TestFinishRestartCore(c *C) {
	st := bs.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	// not core snap
	si := &snap.SideInfo{RealName: "some-app"}
	snaptest.MockSnap(c, "name: some-app\nversion: 1", si)
	err := snapstate.FinishRestart(task, &snapstate.SnapSetup{SideInfo: si})
	c.Check(err, IsNil)

	si = &snap.SideInfo{RealName: "core"}
	snapsup := &snapstate.SnapSetup{SideInfo: si, Type: snap.TypeOS}

	// core snap, restarting ... wait
	restart.MockPending(st, restart.RestartSystem)
	snaptest.MockSnap(c, "name: core\ntype: os\nversion: 1", si)
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, FitsTypeOf, &state.Retry{})

	// core snap, restarted, waiting for current core revision
	restart.MockPending(st, restart.RestartUnset)
	bs.bootloader.BootVars["snap_mode"] = boot.TryingStatus
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, DeepEquals, &state.Retry{After: 5 * time.Second})

	// core snap updated
	si.Revision = snap.R(2)
	snaptest.MockSnap(c, "name: core\ntype: os\nversion: 2", si)

	// core snap, restarted, right core revision, no rollback
	bs.bootloader.BootVars["snap_mode"] = ""
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, IsNil)

	// core snap, restarted, wrong core revision, rollback!
	bs.bootloader.SetBootBase("core_1.snap")
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, ErrorMatches, `cannot finish core installation, there was a rollback across reboot`)
}

func (bs *bootedSuite) TestFinishRestartBootableBase(c *C) {
	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	st := bs.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	// not core snap
	si := &snap.SideInfo{RealName: "some-app", Revision: snap.R(1)}
	snaptest.MockSnap(c, "name: some-app\nversion: 1", si)
	err := snapstate.FinishRestart(task, &snapstate.SnapSetup{SideInfo: si})
	c.Check(err, IsNil)

	// core snap but we are on a model with a different base
	si = &snap.SideInfo{RealName: "core"}
	snaptest.MockSnap(c, "name: core\ntype: os\nversion: 1", si)
	err = snapstate.FinishRestart(task, &snapstate.SnapSetup{SideInfo: si, Type: snap.TypeOS})
	c.Check(err, IsNil)

	si = &snap.SideInfo{RealName: "core18"}
	snapsup := &snapstate.SnapSetup{SideInfo: si, Type: snap.TypeBase}
	snaptest.MockSnap(c, "name: core18\ntype: base\nversion: 1", si)
	// core snap, restarting ... wait
	restart.MockPending(st, restart.RestartSystem)
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, FitsTypeOf, &state.Retry{})

	// core snap, restarted, waiting for current core revision
	restart.MockPending(st, restart.RestartUnset)
	bs.bootloader.BootVars["snap_mode"] = boot.TryingStatus
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, DeepEquals, &state.Retry{After: 5 * time.Second})

	// core18 snap updated
	si.Revision = snap.R(2)
	snaptest.MockSnap(c, "name: core18\ntype: base\nversion: 2", si)

	// core snap, restarted, right core revision, no rollback
	bs.bootloader.BootVars["snap_mode"] = ""
	bs.bootloader.SetBootBase("core18_2.snap")
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, IsNil)

	// core snap, restarted, wrong core revision, rollback!
	bs.bootloader.SetBootBase("core18_1.snap")
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, ErrorMatches, `cannot finish core18 installation, there was a rollback across reboot`)
}

func (bs *bootedSuite) TestFinishRestartKernel(c *C) {
	r := snapstatetest.MockDeviceModel(DefaultModel())
	defer r()

	st := bs.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	// not kernel snap
	si := &snap.SideInfo{RealName: "some-app", Revision: snap.R(1)}
	snaptest.MockSnap(c, "name: some-app\nversion: 1", si)
	err := snapstate.FinishRestart(task, &snapstate.SnapSetup{SideInfo: si})
	c.Check(err, IsNil)

	// different kernel (may happen with remodel)
	si = &snap.SideInfo{RealName: "other-kernel"}
	snaptest.MockSnap(c, "name: other-kernel\ntype: kernel\nversion: 1", si)
	err = snapstate.FinishRestart(task, &snapstate.SnapSetup{SideInfo: si, Type: snap.TypeKernel})
	c.Check(err, IsNil)

	si = &snap.SideInfo{RealName: "kernel"}
	snapsup := &snapstate.SnapSetup{SideInfo: si, Type: snap.TypeKernel}
	snaptest.MockSnap(c, "name: kernel\ntype: kernel\nversion: 1", si)
	// kernel snap, restarting ... wait
	restart.MockPending(st, restart.RestartSystem)
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, FitsTypeOf, &state.Retry{})

	// kernel snap, restarted, waiting for current core revision
	restart.MockPending(st, restart.RestartUnset)
	bs.bootloader.BootVars["snap_mode"] = boot.TryingStatus
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, DeepEquals, &state.Retry{After: 5 * time.Second})

	// kernel snap updated
	si.Revision = snap.R(2)
	snaptest.MockSnap(c, "name: kernel\ntype: kernel\nversion: 2", si)

	// kernel snap, restarted, right kernel revision, no rollback
	bs.bootloader.BootVars["snap_mode"] = ""
	bs.bootloader.SetBootKernel("kernel_2.snap")
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, IsNil)

	// kernel snap, restarted, wrong core revision, rollback!
	bs.bootloader.SetBootKernel("kernel_1.snap")
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, ErrorMatches, `cannot finish kernel installation, there was a rollback across reboot`)
}

func (bs *bootedSuite) TestFinishRestartKernelClassicWithModes(c *C) {
	r := release.MockOnClassic(true)
	defer r()
	r = snapstatetest.MockDeviceModel(MakeModelClassicWithModes("pc", nil))
	defer r()

	bl := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bl)
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("canonical-pc-linux_2.snap")
	c.Assert(err, IsNil)
	bl.SetEnabledKernel(kernel)

	st := bs.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	// not kernel snap
	si := &snap.SideInfo{RealName: "some-app", Revision: snap.R(1)}
	snaptest.MockSnap(c, "name: some-app\nversion: 1", si)
	err = snapstate.FinishRestart(task, &snapstate.SnapSetup{SideInfo: si})
	c.Check(err, IsNil)

	// different kernel (may happen with remodel)
	si = &snap.SideInfo{RealName: "other-kernel"}
	snaptest.MockSnap(c, "name: other-kernel\ntype: kernel\nversion: 1", si)
	err = snapstate.FinishRestart(task, &snapstate.SnapSetup{SideInfo: si, Type: snap.TypeKernel})
	c.Check(err, IsNil)

	si = &snap.SideInfo{RealName: "kernel"}
	snapsup := &snapstate.SnapSetup{SideInfo: si, Type: snap.TypeKernel}
	snaptest.MockSnap(c, "name: kernel\ntype: kernel\nversion: 1", si)
	// kernel snap, restarting ... wait
	restart.MockPending(st, restart.RestartSystem)
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, FitsTypeOf, &state.Retry{})

	// kernel snap, restarted, waiting for current core revision
	restart.MockPending(st, restart.RestartUnset)
	bl.BootVars["kernel_status"] = boot.TryingStatus
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, DeepEquals, &state.Retry{After: 5 * time.Second})

	// kernel snap updated
	si.Revision = snap.R(2)
	snaptest.MockSnap(c, "name: kernel\ntype: kernel\nversion: 2", si)

	// kernel snap, restarted, right kernel revision, no rollback
	bl.BootVars["kernel_status"] = ""
	kernel, err = snap.ParsePlaceInfoFromSnapFileName("kernel_2.snap")
	c.Assert(err, IsNil)
	bl.SetEnabledKernel(kernel)
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, IsNil)

	// kernel snap, restarted, wrong core revision, rollback!
	kernel, err = snap.ParsePlaceInfoFromSnapFileName("kernel_1.snap")
	c.Assert(err, IsNil)
	bl.SetEnabledKernel(kernel)
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, ErrorMatches, `cannot finish kernel installation, there was a rollback across reboot`)
}

func (bs *bootedSuite) TestFinishRestartEphemeralModeSkipsRollbackDetection(c *C) {
	r := snapstatetest.MockDeviceModel(DefaultModel())
	defer r()

	st := bs.state
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("auto-connect", "...")

	si := &snap.SideInfo{RealName: "kernel"}
	snapsup := &snapstate.SnapSetup{SideInfo: si, Type: snap.TypeKernel}
	snaptest.MockSnap(c, "name: kernel\ntype: kernel\nversion: 1", si)
	// kernel snap, restarted, wrong core revision, rollback detected!
	bs.bootloader.SetBootKernel("kernel_1.snap")
	err := snapstate.FinishRestart(task, snapsup)
	c.Check(err, ErrorMatches, `cannot finish kernel installation, there was a rollback across reboot`)

	// but *not* in an ephemeral mode like "recover" - we skip the rollback
	// detection here
	r = snapstatetest.MockDeviceModelAndMode(DefaultModel(), "install")
	defer r()
	err = snapstate.FinishRestart(task, snapsup)
	c.Check(err, IsNil)
}

func (bs *bootedSuite) TestFinishRestartClassicWithModesCoreIgnored(c *C) {
	r := release.MockOnClassic(true)
	defer r()
	r = snapstatetest.MockDeviceModel(MakeModelClassicWithModes("pc", nil))
	defer r()

	st := bs.state
	st.Lock()
	defer st.Unlock()

	// classic+modes has a kernel
	snaptest.MockSnap(c, "name: canonical-pc-linux\ntype: os\nversion: 2", kernelSI2)
	snapstate.Set(st, "canonical-pc-linux", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{sequence.NewRevisionSideState(kernelSI1, nil),
			sequence.NewRevisionSideState(kernelSI2, nil)}),
		Current: snap.R(2),
	})
	// we have core22 and current is r2
	osSI1 := &snap.SideInfo{RealName: "core22", Revision: snap.R(1)}
	osSI2 := &snap.SideInfo{RealName: "core22", Revision: snap.R(2)}
	snaptest.MockSnap(c, "name: core22\ntype: os\nversion: 1", osSI1)
	snaptest.MockSnap(c, "name: core22\ntype: os\nversion: 2", osSI2)
	snapstate.Set(st, "core22", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{sequence.NewRevisionSideState(osSI1, nil),
			sequence.NewRevisionSideState(osSI2, nil)}),
		Current: snap.R(2),
	})
	// now pretend that for whatever reason the modeenv reports that
	// r1 was booted (which is a bug as on a classic+modes
	// there is no boot base)
	bs.bootloader.SetBootBase("core22_1.snap")

	err := snapstate.UpdateBootRevisions(st)
	c.Assert(err, IsNil)

	st.Unlock()
	bs.settle()
	st.Lock()

	// and validate that this does not trigger a "Update kernel
	// and core snap revisions" change
	c.Assert(st.Changes(), HasLen, 0)
}
