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

package boot_test

// test the boot releated code

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/boot"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func TestBoot(t *testing.T) { TestingT(t) }

type bootedSuite struct {
	bootloader *boottest.MockBootloader
	overlord   *overlord.Overlord
}

var _ = Suite(&bootedSuite{})

func (bs *bootedSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)

	// booted is not running on classic
	release.MockOnClassic(false)

	bs.bootloader = boottest.NewMockBootloader("mock", c.MkDir())
	bs.bootloader.BootVars["snappy_os"] = "ubuntu-core_2.snap"
	bs.bootloader.BootVars["snappy_kernel"] = "canonical-pc-linux_2.snap"
	partition.ForceBootloader(bs.bootloader)

	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	bs.overlord = ovld
}

func (bs *bootedSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	partition.ForceBootloader(nil)
}

var osSI1 = &snap.SideInfo{RealName: "ubuntu-core", Revision: snap.R(1)}
var osSI2 = &snap.SideInfo{RealName: "ubuntu-core", Revision: snap.R(2)}
var kernelSI1 = &snap.SideInfo{RealName: "canonical-pc-linux", Revision: snap.R(1)}
var kernelSI2 = &snap.SideInfo{RealName: "canonical-pc-linux", Revision: snap.R(2)}

func (bs *bootedSuite) makeInstalledKernelOS(c *C, st *state.State) {
	st.Lock()
	defer st.Unlock()

	snaptest.MockSnap(c, "name: ubuntu-core\ntype: os\nversion: 1", osSI1)
	snaptest.MockSnap(c, "name: ubuntu-core\ntype: os\nversion: 2", osSI2)
	snapstate.Set(st, "ubuntu-core", &snapstate.SnapState{
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

func (bs *bootedSuite) TestUpdateRevisionsOSSimple(c *C) {
	st := bs.overlord.State()
	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.BootVars["snappy_os"] = "ubuntu-core_1.snap"
	err := boot.UpdateRevisions(bs.overlord)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	// ubuntu-core "current" got reverted but canonical-pc-linux did not
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "ubuntu-core", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(1))
	c.Assert(snapst.Active, Equals, true)

	err = snapstate.Get(st, "canonical-pc-linux", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.Active, Equals, true)
}

func (bs *bootedSuite) TestUpdateRevisionsKernelSimple(c *C) {
	st := bs.overlord.State()
	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.BootVars["snappy_kernel"] = "canonical-pc-linux_1.snap"
	err := boot.UpdateRevisions(bs.overlord)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	// canonical-pc-linux "current" got reverted but ubuntu-core did not
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "canonical-pc-linux", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(1))
	c.Assert(snapst.Active, Equals, true)

	err = snapstate.Get(st, "ubuntu-core", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.Active, Equals, true)
}

func (bs *bootedSuite) TestUpdateRevisionsKernelErrorsEarly(c *C) {
	st := bs.overlord.State()
	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.BootVars["snappy_kernel"] = "canonical-pc-linux_99.snap"
	err := boot.UpdateRevisions(bs.overlord)
	c.Assert(err, ErrorMatches, `cannot find revision 99 for snap "canonical-pc-linux"`)
}

func (bs *bootedSuite) TestUpdateRevisionsOSErrorsEarly(c *C) {
	st := bs.overlord.State()
	bs.makeInstalledKernelOS(c, st)

	bs.bootloader.BootVars["snappy_os"] = "ubuntu-core_99.snap"
	err := boot.UpdateRevisions(bs.overlord)
	c.Assert(err, ErrorMatches, `cannot find revision 99 for snap "ubuntu-core"`)
}

func (bs *bootedSuite) TestUpdateRevisionsOSErrorsLate(c *C) {
	st := bs.overlord.State()

	st.Lock()
	// put ubuntu-core into the state but add no files on disk
	// will break in the tasks
	snapstate.Set(st, "ubuntu-core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{osSI1, osSI2},
		Current:  snap.R(2),
	})
	st.Unlock()

	bs.bootloader.BootVars["snappy_kernel"] = "ubuntu-core_1.snap"
	err := boot.UpdateRevisions(bs.overlord)
	c.Assert(err, ErrorMatches, `(?ms)cannot update revisions after boot changes:.*`)
}

func (bs *bootedSuite) TestNameAndRevnoFromSnapValid(c *C) {
	name, revno, err := boot.NameAndRevnoFromSnap("foo_2.snap")
	c.Assert(err, IsNil)
	c.Assert(name, Equals, "foo")
	c.Assert(revno, Equals, snap.R(2))
}

func (bs *bootedSuite) TestNameAndRevnoFromSnapInvalidFormat(c *C) {
	_, _, err := boot.NameAndRevnoFromSnap("invalid")
	c.Assert(err, ErrorMatches, `input "invalid" has invalid format \(not enough '_'\)`)
}
