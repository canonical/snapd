// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

// set up gocheck
func TestBoot(t *testing.T) { TestingT(t) }

// baseBootSuite is used to setup the common test environment
type baseBootSetSuite struct {
	testutil.BaseTest

	bootdir string
}

func (s *baseBootSetSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.AddCleanup(restore)

	s.bootdir = filepath.Join(dirs.GlobalRootDir, "boot")
}

func (s *baseBootSetSuite) forceBootloader(bloader bootloader.Bootloader) {
	bootloader.Force(bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

// bootSetSuite tests the abstract BootSet interface, and tools that
// don't depend on a specific BootSet implementation
type bootSetSuite struct {
	baseBootSetSuite

	bootloader *bootloadertest.MockBootloader
}

var _ = Suite(&bootSetSuite{})

func (s *bootSetSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	s.forceBootloader(s.bootloader)
}

func (s *bootSetSuite) TestInUseClassic(c *C) {
	classicDev := boottest.MockDevice("")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))

	inUse, err := boot.InUse(snap.TypeBase, classicDev)
	c.Assert(err, IsNil)
	c.Check(inUse("core18", snap.R(41)), Equals, false)
}

func (s *bootSetSuite) TestInUseIrrelevantTypes(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))

	inUse, err := boot.InUse(snap.TypeGadget, coreDev)
	c.Assert(err, IsNil)
	c.Check(inUse("gadget", snap.R(41)), Equals, false)
}

func (s *bootSetSuite) TestInUse(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	for _, t := range []struct {
		bootVarKey   string
		bootVarValue string

		snapName string
		snapRev  snap.Revision

		inUse bool
	}{
		// in use
		{"snap_kernel", "kernel_41.snap", "kernel", snap.R(41), true},
		{"snap_try_kernel", "kernel_82.snap", "kernel", snap.R(82), true},
		{"snap_core", "core_21.snap", "core", snap.R(21), true},
		{"snap_try_core", "core_42.snap", "core", snap.R(42), true},
		// not in use
		{"snap_core", "core_111.snap", "core", snap.R(21), false},
		{"snap_try_core", "core_111.snap", "core", snap.R(21), false},
		{"snap_kernel", "kernel_111.snap", "kernel", snap.R(1), false},
		{"snap_try_kernel", "kernel_111.snap", "kernel", snap.R(1), false},
	} {
		typ := snap.TypeBase
		if t.snapName == "kernel" {
			typ = snap.TypeKernel
		}
		s.bootloader.BootVars[t.bootVarKey] = t.bootVarValue
		inUse, err := boot.InUse(typ, coreDev)
		c.Assert(err, IsNil)
		c.Assert(inUse(t.snapName, t.snapRev), Equals, t.inUse, Commentf("unexpected result: %s %s %v", t.snapName, t.snapRev, t.inUse))
	}
}

func (s *bootSetSuite) TestInUseEphemeral(c *C) {
	coreDev := boottest.MockDevice("some-snap@install")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))

	inUse, err := boot.InUse(snap.TypeBase, coreDev)
	c.Assert(err, IsNil)
	c.Check(inUse("whatever", snap.R(0)), Equals, true)
}

func (s *bootSetSuite) TestInUseUnhappy(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// make GetVars fail
	s.bootloader.GetErr = errors.New("zap")
	_, err := boot.InUse(snap.TypeKernel, coreDev)
	c.Check(err, ErrorMatches, `cannot get boot variables: zap`)

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	_, err = boot.InUse(snap.TypeKernel, coreDev)
	c.Check(err, ErrorMatches, `cannot get boot settings: broken bootloader`)
}

func (s *bootSetSuite) TestCurrentBootNameAndRevision(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	s.bootloader.BootVars["snap_core"] = "core_2.snap"
	s.bootloader.BootVars["snap_kernel"] = "canonical-pc-linux_2.snap"

	current, err := boot.GetCurrentBoot(snap.TypeOS, coreDev)
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, "core")
	c.Check(current.SnapRevision(), Equals, snap.R(2))

	current, err = boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, "canonical-pc-linux")
	c.Check(current.SnapRevision(), Equals, snap.R(2))

	s.bootloader.BootVars["snap_mode"] = boot.TryingStatus
	_, err = boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, Equals, boot.ErrBootNameAndRevisionNotReady)
}

func (s *bootSetSuite) TestCurrentBoot20NameAndRevision(c *C) {
	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := boottest.ForceModeenv(dirs.GlobalRootDir, &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	})
	defer r()

	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r = s.bootloader.SetRunKernelImageEnabledKernel(kernel)
	defer r()

	current, err := boot.GetCurrentBoot(snap.TypeBase, coreDev)
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, "core20")
	c.Check(current.SnapRevision(), Equals, snap.R(1))

	current, err = boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, "pc-kernel")
	c.Check(current.SnapRevision(), Equals, snap.R(1))

	s.bootloader.BootVars["kernel_status"] = boot.TryingStatus
	_, err = boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, Equals, boot.ErrBootNameAndRevisionNotReady)
}

func (s *bootSetSuite) TestCurrentBootNameAndRevisionUnhappy(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	_, err := boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, ErrorMatches, `cannot get name and revision of kernel \(snap_kernel\): boot variable unset`)

	_, err = boot.GetCurrentBoot(snap.TypeOS, coreDev)
	c.Check(err, ErrorMatches, `cannot get name and revision of boot base \(snap_core\): boot variable unset`)

	_, err = boot.GetCurrentBoot(snap.TypeBase, coreDev)
	c.Check(err, ErrorMatches, `cannot get name and revision of boot base \(snap_core\): boot variable unset`)

	_, err = boot.GetCurrentBoot(snap.TypeApp, coreDev)
	c.Check(err, ErrorMatches, `internal error: no boot state handling for snap type "app"`)

	// sanity check
	s.bootloader.BootVars["snap_kernel"] = "kernel_41.snap"
	current, err := boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, "kernel")
	c.Check(current.SnapRevision(), Equals, snap.R(41))

	// make GetVars fail
	s.bootloader.GetErr = errors.New("zap")
	_, err = boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, ErrorMatches, "cannot get boot variables: zap")
	s.bootloader.GetErr = nil

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	_, err = boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, ErrorMatches, "cannot get boot settings: broken bootloader")
}

func (s *bootSetSuite) TestParticipant(c *C) {
	info := &snap.Info{}
	info.RealName = "some-snap"

	coreDev := boottest.MockDevice("some-snap")
	classicDev := boottest.MockDevice("")

	bp := boot.Participant(info, snap.TypeApp, coreDev)
	c.Check(bp.IsTrivial(), Equals, true)

	for _, typ := range []snap.Type{
		snap.TypeKernel,
		snap.TypeOS,
		snap.TypeBase,
	} {
		bp = boot.Participant(info, typ, classicDev)
		c.Check(bp.IsTrivial(), Equals, true)

		bp = boot.Participant(info, typ, coreDev)
		c.Check(bp.IsTrivial(), Equals, false)

		c.Check(bp, DeepEquals, boot.NewCoreBootParticipant(info, typ, coreDev))
	}
}

func (s *bootSetSuite) TestParticipantBaseWithModel(c *C) {
	core := &snap.Info{SideInfo: snap.SideInfo{RealName: "core"}, SnapType: snap.TypeOS}
	core18 := &snap.Info{SideInfo: snap.SideInfo{RealName: "core18"}, SnapType: snap.TypeBase}

	type tableT struct {
		with  *snap.Info
		model string
		nop   bool
	}

	table := []tableT{
		{
			with:  core,
			model: "",
			nop:   true,
		}, {
			with:  core,
			model: "core",
			nop:   false,
		}, {
			with:  core,
			model: "core18",
			nop:   true,
		},
		{
			with:  core18,
			model: "",
			nop:   true,
		},
		{
			with:  core18,
			model: "core",
			nop:   true,
		},
		{
			with:  core18,
			model: "core18",
			nop:   false,
		},
		{
			with:  core18,
			model: "core18@install",
			nop:   true,
		},
		{
			with:  core,
			model: "core@install",
			nop:   true,
		},
	}

	for i, t := range table {
		dev := boottest.MockDevice(t.model)
		bp := boot.Participant(t.with, t.with.GetType(), dev)
		c.Check(bp.IsTrivial(), Equals, t.nop, Commentf("%d", i))
		if !t.nop {
			c.Check(bp, DeepEquals, boot.NewCoreBootParticipant(t.with, t.with.GetType(), dev))
		}
	}
}

func (s *bootSetSuite) TestKernelWithModel(c *C) {
	info := &snap.Info{}
	info.RealName = "kernel"

	type tableT struct {
		model string
		nop   bool
		krn   boot.BootKernel
	}

	table := []tableT{
		{
			model: "other-kernel",
			nop:   true,
			krn:   boot.Trivial{},
		}, {
			model: "kernel",
			nop:   false,
			krn:   boot.NewCoreKernel(info, boottest.MockDevice("kernel")),
		}, {
			model: "",
			nop:   true,
			krn:   boot.Trivial{},
		}, {
			model: "kernel@install",
			nop:   true,
			krn:   boot.Trivial{},
		},
	}

	for _, t := range table {
		dev := boottest.MockDevice(t.model)
		krn := boot.Kernel(info, snap.TypeKernel, dev)
		c.Check(krn.IsTrivial(), Equals, t.nop)
		c.Check(krn, DeepEquals, t.krn)
	}
}

func (s *bootSetSuite) TestCoreKernel20(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)

	// get the boot kernel from our kernel snap
	bootKern := boot.Kernel(kernel, snap.TypeKernel, coreDev)
	// can't use FitsTypeOf with coreKernel here, cause that causes an import
	// loop as boottest imports boot and coreKernel is unexported
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// extract the kernel assets from the coreKernel
	// the container here doesn't really matter since it's just being passed
	// to the mock bootloader method anyways
	kernelContainer := snaptest.MockContainer(c, nil)
	err = bootKern.ExtractKernelAssets(kernelContainer)
	c.Assert(err, IsNil)

	// make sure that the bootloader was told to extract some assets
	c.Assert(s.bootloader.ExtractKernelAssetsCalls, DeepEquals, []snap.PlaceInfo{kernel})

	// now remove the kernel assets and ensure that we get those calls
	err = bootKern.RemoveKernelAssets()
	c.Assert(err, IsNil)

	// make sure that the bootloader was told to remove assets
	c.Assert(s.bootloader.RemoveKernelAssetsCalls, DeepEquals, []snap.PlaceInfo{kernel})
}

func (s *bootSetSuite) TestCoreParticipant20SetNextSameKernelSnap(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// set the current kernel
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := s.bootloader.SetRunKernelImageEnabledKernel(kernel)
	defer r()

	// default state
	s.bootloader.BootVars["kernel_status"] = boot.DefaultStatus

	// get the boot kernel participant from our kernel snap
	bootKern := boot.Participant(kernel, snap.TypeKernel, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

	nSetBootVarCalls := s.bootloader.SetBootVarsCalls

	// make the kernel used on next boot
	rebootRequired, err := bootKern.SetNextBoot()
	c.Assert(err, IsNil)
	c.Assert(rebootRequired, Equals, false)

	// make sure that the bootloader was asked for the current kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)

	// ensure that kernel_status is still empty
	c.Assert(s.bootloader.BootVars["kernel_status"], Equals, boot.DefaultStatus)

	// there was no attempt to enable a kernel
	_, enableKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(enableKernelCalls, Equals, 0)

	// finally we didn't call SetBootVars on the bootloader because nothing
	// changed
	c.Assert(nSetBootVarCalls, Equals, s.bootloader.SetBootVarsCalls)
}

func (s *bootSetSuite) TestCoreParticipant20SetNextNewKernelSnap(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// set the current kernel
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := s.bootloader.SetRunKernelImageEnabledKernel(kernel)
	defer r()

	// make a new kernel
	kernel2, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)

	// default state
	s.bootloader.BootVars["kernel_status"] = boot.DefaultStatus

	// get the boot kernel participant from our new kernel snap
	bootKern := boot.Participant(kernel2, snap.TypeKernel, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// make the kernel used on next boot
	rebootRequired, err := bootKern.SetNextBoot()
	c.Assert(err, IsNil)
	c.Assert(rebootRequired, Equals, true)

	// make sure that the bootloader was asked for the current kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)

	// ensure that kernel_status is now try
	c.Assert(s.bootloader.BootVars["kernel_status"], Equals, boot.TryStatus)

	// and we were asked to enable kernel2 as the try kernel
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{kernel2})
}

func (s *bootSetSuite) TestMarkBootSuccessful20KernelStatusTryingNoKernelSnapCleansUp(c *C) {
	r := boottest.ForceModeenv(dirs.GlobalRootDir, &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	})
	defer r()

	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// set kernel_status as trying, but don't set a kernel snap
	s.bootloader.BootVars["kernel_status"] = boot.TryingStatus

	// set the current Kernel
	kernel1, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r = s.bootloader.SetRunKernelImageEnabledKernel(kernel1)
	defer r()

	// mark successful
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check that the bootloader variable was cleaned
	expected := map[string]string{"kernel_status": boot.DefaultStatus}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that MarkBootSuccessful didn't enable a kernel (since there was no
	// try kernel)
	_, nEnableCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(nEnableCalls, Equals, 0)

	// we also didn't disable a try kernel (because it didn't exist)
	_, nDisableTryCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 0)

	// do it again, verify it's still okay
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// no new bootloader calls
	_, nEnableCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(nEnableCalls, Equals, 0)
	_, nDisableTryCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 0)
}

func (s *bootSetSuite) TestMarkBootSuccessful20BaseStatusTryingNoBaseSnapCleansUp(c *C) {
	m := &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
		BaseStatus:     boot.TryingStatus,
	}
	err := m.Write("")
	c.Assert(err, IsNil)
	defer os.Remove(dirs.SnapModeenvFileUnder(dirs.GlobalRootDir))

	coreDev := boottest.MockUC20Device("core20")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// mark successful
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check that the modeenv base_status was re-written to default
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.BaseStatus, Equals, boot.DefaultStatus)
	c.Assert(m2.Base, Equals, m.Base)
	c.Assert(m2.TryBase, Equals, m.TryBase)

	// do it again, verify it's still okay
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	m3, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m3.BaseStatus, Equals, boot.DefaultStatus)
	c.Assert(m3.Base, Equals, m.Base)
	c.Assert(m3.TryBase, Equals, m.TryBase)
}

func (s *bootSetSuite) TestCoreParticipant20SetNextSameBaseSnap(c *C) {
	coreDev := boottest.MockUC20Device("core20")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// make a new base snap
	base, err := snap.ParsePlaceInfoFromSnapFileName("core20_1.snap")
	c.Assert(err, IsNil)

	// default state
	m := &boot.Modeenv{
		Base: "core20_1.snap",
	}
	err = m.Write("")
	c.Assert(err, IsNil)
	defer os.Remove(dirs.SnapModeenvFileUnder(dirs.GlobalRootDir))

	// get the boot base participant from our base snap
	bootBase := boot.Participant(base, snap.TypeBase, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootBase.IsTrivial(), Equals, false)

	// make the base used on next boot
	rebootRequired, err := bootBase.SetNextBoot()
	c.Assert(err, IsNil)

	// we don't need to reboot because it's the same base snap
	c.Assert(rebootRequired, Equals, false)

	// make sure the modeenv wasn't changed
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.Base, Equals, m.Base)
	c.Assert(m2.BaseStatus, Equals, m.BaseStatus)
	c.Assert(m2.TryBase, Equals, m.TryBase)
}

func (s *bootSetSuite) TestCoreParticipant20SetNextNewBaseSnap(c *C) {
	coreDev := boottest.MockUC20Device("core20")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// make a new base snap to update to
	base2, err := snap.ParsePlaceInfoFromSnapFileName("core20_2.snap")
	c.Assert(err, IsNil)

	// default state
	m := &boot.Modeenv{
		Base: "core20_1.snap",
	}
	err = m.Write("")
	c.Assert(err, IsNil)
	defer os.Remove(dirs.SnapModeenvFileUnder(dirs.GlobalRootDir))

	// get the boot base participant from our new base snap
	bootBase := boot.Participant(base2, snap.TypeBase, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootBase.IsTrivial(), Equals, false)

	// make the base used on next boot
	rebootRequired, err := bootBase.SetNextBoot()
	c.Assert(err, IsNil)
	c.Assert(rebootRequired, Equals, true)

	// make sure the modeenv was updated
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.Base, Equals, m.Base)
	c.Assert(m2.BaseStatus, Equals, boot.TryStatus)
	c.Assert(m2.TryBase, Equals, "core20_2.snap")
}

func (s *bootSetSuite) TestMarkBootSuccessfulAllSnap(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	s.bootloader.BootVars["snap_mode"] = boot.TryingStatus
	s.bootloader.BootVars["snap_try_core"] = "os1"
	s.bootloader.BootVars["snap_try_kernel"] = "k1"
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	expected := map[string]string{
		// cleared
		"snap_mode":       boot.DefaultStatus,
		"snap_try_kernel": "",
		"snap_try_core":   "",
		// updated
		"snap_kernel": "k1",
		"snap_core":   "os1",
	}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)
}

func (s *bootSetSuite) TestMarkBootSuccessful20AllSnap(c *C) {
	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// we were trying a base snap
	m := &boot.Modeenv{
		Base:       "core20_1.snap",
		TryBase:    "core20_2.snap",
		BaseStatus: boot.TryingStatus,
	}
	err := m.Write("")
	c.Assert(err, IsNil)
	defer os.Remove(dirs.SnapModeenvFileUnder(dirs.GlobalRootDir))

	// set the current kernel
	kernel1, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := s.bootloader.SetRunKernelImageEnabledKernel(kernel1)
	defer r()

	// set the current try kernel
	kernel2, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)
	r = s.bootloader.SetRunKernelImageEnabledTryKernel(kernel2)
	defer r()

	s.bootloader.BootVars["kernel_status"] = boot.TryingStatus

	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the bootloader variables
	expected := map[string]string{
		// cleared
		"kernel_status": boot.DefaultStatus,
	}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that we called EnableKernel() on the try-kernel
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{kernel2})

	// and that we disabled a try kernel
	_, nDisableTryCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 1)

	// also check that the modeenv was updated
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.Base, Equals, "core20_2.snap")
	c.Assert(m2.TryBase, Equals, "")
	c.Assert(m2.BaseStatus, Equals, boot.DefaultStatus)

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// no new bootloader calls
	actual, _ = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{kernel2})
	_, nDisableTryCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 1)
}

func (s *bootSetSuite) TestMarkBootSuccessfulKernelUpdate(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	s.bootloader.BootVars["snap_mode"] = boot.TryingStatus
	s.bootloader.BootVars["snap_core"] = "os1"
	s.bootloader.BootVars["snap_kernel"] = "k1"
	s.bootloader.BootVars["snap_try_core"] = ""
	s.bootloader.BootVars["snap_try_kernel"] = "k2"
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, map[string]string{
		// cleared
		"snap_mode":       boot.DefaultStatus,
		"snap_try_kernel": "",
		"snap_try_core":   "",
		// unchanged
		"snap_core": "os1",
		// updated
		"snap_kernel": "k2",
	})
}

func (s *bootSetSuite) TestMarkBootSuccessfulBaseUpdate(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	s.bootloader.BootVars["snap_mode"] = boot.TryingStatus
	s.bootloader.BootVars["snap_core"] = "os1"
	s.bootloader.BootVars["snap_kernel"] = "k1"
	s.bootloader.BootVars["snap_try_core"] = "os2"
	s.bootloader.BootVars["snap_try_kernel"] = ""
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, map[string]string{
		// cleared
		"snap_mode":     boot.DefaultStatus,
		"snap_try_core": "",
		// unchanged
		"snap_kernel":     "k1",
		"snap_try_kernel": "",
		// updated
		"snap_core": "os2",
	})
}

func (s *bootSetSuite) TestMarkBootSuccessful20KernelUpdate(c *C) {
	r := boottest.ForceModeenv(dirs.GlobalRootDir, &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	})
	defer r()

	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// set bootloader variables
	s.bootloader.BootVars["kernel_status"] = boot.TryingStatus

	// set the current Kernel
	kernel1, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r = s.bootloader.SetRunKernelImageEnabledKernel(kernel1)
	defer r()

	// set the current try kernel
	kernel2, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)
	r = s.bootloader.SetRunKernelImageEnabledTryKernel(kernel2)
	defer r()

	// mark successful
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the bootloader variables
	expected := map[string]string{"kernel_status": boot.DefaultStatus}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that MarkBootSuccessful enabled the try kernel
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{kernel2})

	// and that we disabled a try kernel
	_, nDisableTryCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 1)

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// no new bootloader calls
	actual, _ = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{kernel2})
	_, nDisableTryCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 1)
}

func (s *bootSetSuite) TestMarkBootSuccessful20BaseUpdate(c *C) {
	// we were trying a base snap
	m := &boot.Modeenv{
		Base:       "core20_1.snap",
		TryBase:    "core20_2.snap",
		BaseStatus: boot.TryingStatus,
	}
	err := m.Write("")
	c.Assert(err, IsNil)
	defer os.Remove(dirs.SnapModeenvFileUnder(dirs.GlobalRootDir))

	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// mark successful
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.Base, Equals, "core20_2.snap")
	c.Assert(m2.TryBase, Equals, "")
	c.Assert(m2.BaseStatus, Equals, "")

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv again
	m3, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m3.Base, Equals, "core20_2.snap")
	c.Assert(m3.TryBase, Equals, "")
	c.Assert(m3.BaseStatus, Equals, "")
}
