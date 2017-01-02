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

// test the boot releated code

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type bootedSuite struct {
	bootloader *boottest.MockBootloader

	state       *state.State
	snapmgr     *snapstate.SnapManager
	fakeBackend *fakeSnappyBackend
}

var _ = Suite(&bootedSuite{})

func (bs *bootedSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)

	// booted is not running on classic
	release.MockOnClassic(false)

	bs.bootloader = boottest.NewMockBootloader("mock", c.MkDir())
	bs.bootloader.BootVars["snap_core"] = "core_2.snap"
	bs.bootloader.BootVars["snap_kernel"] = "canonical-pc-linux_2.snap"
	partition.ForceBootloader(bs.bootloader)

	bs.fakeBackend = &fakeSnappyBackend{}
	bs.state = state.New(nil)
	bs.snapmgr, err = snapstate.Manager(bs.state)
	c.Assert(err, IsNil)
	bs.snapmgr.AddForeignTaskHandlers(bs.fakeBackend)

	snapstate.SetSnapManagerBackend(bs.snapmgr, bs.fakeBackend)
	snapstate.AutoAliases = func(*state.State, *snap.Info) ([]string, error) {
		return nil, nil
	}
}

func (bs *bootedSuite) TearDownTest(c *C) {
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
	for i := 0; i < 50; i++ {
		bs.snapmgr.Ensure()
		bs.snapmgr.Wait()
	}
}

func (bs *bootedSuite) makeInstalledKernelOS(c *C, st *state.State) {
	snaptest.MockSnap(c, "name: core\ntype: os\nversion: 1", "", osSI1)
	snaptest.MockSnap(c, "name: core\ntype: os\nversion: 2", "", osSI2)
	snapstate.Set(st, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{osSI1, osSI2},
		Current:  snap.R(2),
	})

	snaptest.MockSnap(c, "name: canonical-pc-linux\ntype: os\nversion: 1", "", kernelSI1)
	snaptest.MockSnap(c, "name: canonical-pc-linux\ntype: os\nversion: 2", "", kernelSI2)
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

	// put core into the state but add no files on disk
	// will break in the tasks
	snapstate.Set(st, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{osSI1, osSI2},
		Current:  snap.R(2),
	})
	bs.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/core/1")

	bs.bootloader.BootVars["snap_kernel"] = "core_1.snap"
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
