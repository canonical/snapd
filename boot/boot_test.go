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
	"io/ioutil"
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

func TestBoot(t *testing.T) { TestingT(t) }

type baseBootenvSuite struct {
	testutil.BaseTest

	rootdir string
	bootdir string
}

func (s *baseBootenvSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.AddCleanup(restore)

	s.bootdir = filepath.Join(s.rootdir, "boot")
}

func (s *baseBootenvSuite) forceBootloader(bloader bootloader.Bootloader) {
	bootloader.Force(bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

type bootenvSuite struct {
	baseBootenvSuite

	bootloader *bootloadertest.MockBootloader
}

var _ = Suite(&bootenvSuite{})

func (s *bootenvSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	s.forceBootloader(s.bootloader)
}

type baseBootenv20Suite struct {
	baseBootenvSuite

	kern1 snap.PlaceInfo
	kern2 snap.PlaceInfo
	base1 snap.PlaceInfo
	base2 snap.PlaceInfo

	normalDefaultState      *bootenv20Setup
	normalTryingKernelState *bootenv20Setup
}

func (s *baseBootenv20Suite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	var err error
	s.kern1, err = snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	s.kern2, err = snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)

	s.base1, err = snap.ParsePlaceInfoFromSnapFileName("core20_1.snap")
	c.Assert(err, IsNil)
	s.base2, err = snap.ParsePlaceInfoFromSnapFileName("core20_2.snap")
	c.Assert(err, IsNil)

	// default boot state for robustness tests, etc.
	s.normalDefaultState = &bootenv20Setup{
		modeenv: &boot.Modeenv{
			// base is base1
			Base: s.base1.Filename(),
			// no try base
			TryBase: "",
			// base status is default
			BaseStatus: boot.DefaultStatus,
			// current kernels is just kern1
			CurrentKernels: []string{s.kern1.Filename()},
			// operating mode is run
			Mode: "run",
			// RecoverySystem is unset, as it should be during run mode
			RecoverySystem: "",
		},
		// enabled kernel is kern1
		kern: s.kern1,
		// no try kernel enabled
		tryKern: nil,
		// kernel status is default
		kernStatus: boot.DefaultStatus,
	}

	// state for after trying a new kernel for robustness tests, etc.
	s.normalTryingKernelState = &bootenv20Setup{
		modeenv: &boot.Modeenv{
			// operating mode is run
			Mode: "run",
			// base is base1
			Base: s.base1.Filename(),
			// no try base
			TryBase: "",
			// base status is default
			BaseStatus: boot.DefaultStatus,
			// current kernels is kern1 + kern2
			CurrentKernels: []string{s.kern1.Filename(), s.kern2.Filename()},
		},
		// enabled kernel is kern1
		kern: s.kern1,
		// try kernel is kern2
		tryKern: s.kern2,
		// kernel status is trying
		kernStatus: boot.TryingStatus,
	}
}

type bootenv20Suite struct {
	baseBootenv20Suite

	bootloader *bootloadertest.MockExtractedRunKernelImageBootloader
}

type bootenv20EnvRefKernelSuite struct {
	baseBootenv20Suite

	bootloader *bootloadertest.MockBootloader
}

var defaultUC20BootEnv = map[string]string{"kernel_status": boot.DefaultStatus}

var _ = Suite(&bootenv20Suite{})
var _ = Suite(&bootenv20EnvRefKernelSuite{})

func (s *bootenv20Suite) SetUpTest(c *C) {
	s.baseBootenv20Suite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir()).WithExtractedRunKernelImage()
	s.forceBootloader(s.bootloader)
}

func (s *bootenv20EnvRefKernelSuite) SetUpTest(c *C) {
	s.baseBootenv20Suite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	s.forceBootloader(s.bootloader)
}

type bootenv20Setup struct {
	modeenv    *boot.Modeenv
	kern       snap.PlaceInfo
	tryKern    snap.PlaceInfo
	kernStatus string
}

func setupUC20Bootenv(c *C, bl bootloader.Bootloader, opts *bootenv20Setup) (restore func()) {
	var cleanups []func()

	// write the modeenv
	if opts.modeenv != nil {
		c.Assert(opts.modeenv.WriteTo(""), IsNil)
		// this isn't strictly necessary since the modeenv will be written to
		// the test's private dir anyways, but it's nice to have so we can write
		// multiple modeenvs from a single test and just call the restore
		// function in between the parts of the test that use different modeenvs
		r := func() {
			defaultModeenv := &boot.Modeenv{Mode: "run"}
			c.Assert(defaultModeenv.WriteTo(""), IsNil)
		}
		cleanups = append(cleanups, r)
	}

	// set the status
	origEnv, err := bl.GetBootVars("kernel_status")
	c.Assert(err, IsNil)

	err = bl.SetBootVars(map[string]string{"kernel_status": opts.kernStatus})
	c.Assert(err, IsNil)
	cleanups = append(cleanups, func() {
		err := bl.SetBootVars(origEnv)
		c.Assert(err, IsNil)
	})

	// check what kind of real mock bootloader we have to use different methods
	// to set the kernel snaps are if they're non-nil
	switch vbl := bl.(type) {
	case *bootloadertest.MockExtractedRunKernelImageBootloader:
		// then we can use the advanced methods on it
		if opts.kern != nil {
			r := vbl.SetEnabledKernel(opts.kern)
			cleanups = append(cleanups, r)
		}

		if opts.tryKern != nil {
			r := vbl.SetEnabledTryKernel(opts.tryKern)
			cleanups = append(cleanups, r)
		}

		// don't count any calls to SetBootVars made thus far
		vbl.SetBootVarsCalls = 0

	case *bootloadertest.MockBootloader:
		// then we need to use the bootenv to set the current kernels
		origEnv, err := vbl.GetBootVars("snap_kernel", "snap_try_kernel")
		c.Assert(err, IsNil)
		m := make(map[string]string, 2)
		if opts.kern != nil {
			m["snap_kernel"] = opts.kern.Filename()
		} else {
			m["snap_kernel"] = ""
		}

		if opts.tryKern != nil {
			m["snap_try_kernel"] = opts.tryKern.Filename()
		} else {
			m["snap_try_kernel"] = ""
		}

		err = vbl.SetBootVars(m)
		c.Assert(err, IsNil)

		// don't count any calls to SetBootVars made thus far
		vbl.SetBootVarsCalls = 0

		cleanups = append(cleanups, func() {
			err := bl.SetBootVars(origEnv)
			c.Assert(err, IsNil)
		})
	}

	return func() {
		for _, r := range cleanups {
			r()
		}
	}
}

func (s *bootenvSuite) TestInUseClassic(c *C) {
	classicDev := boottest.MockDevice("")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))

	inUse, err := boot.InUse(snap.TypeBase, classicDev)
	c.Assert(err, IsNil)
	c.Check(inUse("core18", snap.R(41)), Equals, false)
}

func (s *bootenvSuite) TestInUseIrrelevantTypes(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))

	inUse, err := boot.InUse(snap.TypeGadget, coreDev)
	c.Assert(err, IsNil)
	c.Check(inUse("gadget", snap.R(41)), Equals, false)
}

func (s *bootenvSuite) TestInUse(c *C) {
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

func (s *bootenvSuite) TestInUseEphemeral(c *C) {
	coreDev := boottest.MockDevice("some-snap@install")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))

	inUse, err := boot.InUse(snap.TypeBase, coreDev)
	c.Assert(err, IsNil)
	c.Check(inUse("whatever", snap.R(0)), Equals, true)
}

func (s *bootenvSuite) TestInUseUnhappy(c *C) {
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

func (s *bootenvSuite) TestCurrentBootNameAndRevision(c *C) {
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

func (s *bootenv20Suite) TestCurrentBoot20NameAndRevision(c *C) {
	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()

	current, err := boot.GetCurrentBoot(snap.TypeBase, coreDev)
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, s.base1.SnapName())
	c.Check(current.SnapRevision(), Equals, snap.R(1))

	current, err = boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, s.kern1.SnapName())
	c.Check(current.SnapRevision(), Equals, snap.R(1))

	s.bootloader.BootVars["kernel_status"] = boot.TryingStatus
	_, err = boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Check(err, Equals, boot.ErrBootNameAndRevisionNotReady)
}

// only difference between this test and TestCurrentBoot20NameAndRevision is the
// base bootloader which doesn't support ExtractedRunKernelImageBootloader.
func (s *bootenv20EnvRefKernelSuite) TestCurrentBoot20NameAndRevision(c *C) {
	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()

	current, err := boot.GetCurrentBoot(snap.TypeKernel, coreDev)
	c.Assert(err, IsNil)
	c.Assert(current.SnapName(), Equals, s.kern1.SnapName())
	c.Assert(current.SnapRevision(), Equals, snap.R(1))
}

func (s *bootenvSuite) TestCurrentBootNameAndRevisionUnhappy(c *C) {
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

func (s *bootenvSuite) TestParticipant(c *C) {
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

func (s *bootenvSuite) TestParticipantBaseWithModel(c *C) {
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
		bp := boot.Participant(t.with, t.with.Type(), dev)
		c.Check(bp.IsTrivial(), Equals, t.nop, Commentf("%d", i))
		if !t.nop {
			c.Check(bp, DeepEquals, boot.NewCoreBootParticipant(t.with, t.with.Type(), dev))
		}
	}
}

func (s *bootenvSuite) TestKernelWithModel(c *C) {
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

func (s *bootenvSuite) TestMarkBootSuccessfulKernelStatusTryingNoTryKernelSnapCleansUp(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// set all the same vars as if we were doing trying, except don't set a try
	// kernel

	err := s.bootloader.SetBootVars(map[string]string{
		"snap_kernel": "kernel_41.snap",
		"snap_mode":   boot.TryingStatus,
	})
	c.Assert(err, IsNil)

	// mark successful
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check that the bootloader variables were cleaned
	expected := map[string]string{
		"snap_mode":       boot.DefaultStatus,
		"snap_kernel":     "kernel_41.snap",
		"snap_try_kernel": "",
	}
	m, err := s.bootloader.GetBootVars("snap_mode", "snap_try_kernel", "snap_kernel")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, expected)

	// do it again, verify it's still okay
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	m2, err := s.bootloader.GetBootVars("snap_mode", "snap_try_kernel", "snap_kernel")
	c.Assert(err, IsNil)
	c.Assert(m2, DeepEquals, expected)
}

func (s *bootenvSuite) TestMarkBootSuccessfulTryKernelKernelStatusDefaultCleansUp(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// set an errant snap_try_kernel
	err := s.bootloader.SetBootVars(map[string]string{
		"snap_kernel":     "kernel_41.snap",
		"snap_try_kernel": "kernel_42.snap",
		"snap_mode":       boot.DefaultStatus,
	})
	c.Assert(err, IsNil)

	// mark successful
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check that the bootloader variables were cleaned
	expected := map[string]string{
		"snap_mode":       boot.DefaultStatus,
		"snap_kernel":     "kernel_41.snap",
		"snap_try_kernel": "",
	}
	m, err := s.bootloader.GetBootVars("snap_mode", "snap_try_kernel", "snap_kernel")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, expected)

	// do it again, verify it's still okay
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	m2, err := s.bootloader.GetBootVars("snap_mode", "snap_try_kernel", "snap_kernel")
	c.Assert(err, IsNil)
	c.Assert(m2, DeepEquals, expected)
}

func (s *bootenv20Suite) TestCoreKernel20(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()

	// get the boot kernel from our kernel snap
	bootKern := boot.Kernel(s.kern1, snap.TypeKernel, coreDev)
	// can't use FitsTypeOf with coreKernel here, cause that causes an import
	// loop as boottest imports boot and coreKernel is unexported
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// extract the kernel assets from the coreKernel
	// the container here doesn't really matter since it's just being passed
	// to the mock bootloader method anyways
	kernelContainer := snaptest.MockContainer(c, nil)
	err := bootKern.ExtractKernelAssets(kernelContainer)
	c.Assert(err, IsNil)

	// make sure that the bootloader was told to extract some assets
	c.Assert(s.bootloader.ExtractKernelAssetsCalls, DeepEquals, []snap.PlaceInfo{s.kern1})

	// now remove the kernel assets and ensure that we get those calls
	err = bootKern.RemoveKernelAssets()
	c.Assert(err, IsNil)

	// make sure that the bootloader was told to remove assets
	c.Assert(s.bootloader.RemoveKernelAssetsCalls, DeepEquals, []snap.PlaceInfo{s.kern1})
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextSameKernelSnap(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()

	// get the boot kernel participant from our kernel snap
	bootKern := boot.Participant(s.kern1, snap.TypeKernel, coreDev)

	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

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

	// the modeenv is still the same as well
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename()})

	// finally we didn't call SetBootVars on the bootloader because nothing
	// changed
	c.Assert(s.bootloader.SetBootVarsCalls, Equals, 0)
}

func (s *bootenv20EnvRefKernelSuite) TestCoreParticipant20SetNextSameKernelSnap(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()

	// get the boot kernel participant from our kernel snap
	bootKern := boot.Participant(s.kern1, snap.TypeKernel, coreDev)

	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// make the kernel used on next boot
	rebootRequired, err := bootKern.SetNextBoot()
	c.Assert(err, IsNil)
	c.Assert(rebootRequired, Equals, false)

	// ensure that bootenv is unchanged
	m, err := s.bootloader.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": "",
	})

	// the modeenv is still the same as well
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename()})

	// finally we didn't call SetBootVars on the bootloader because nothing
	// changed
	c.Assert(s.bootloader.SetBootVarsCalls, Equals, 0)
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextNewKernelSnap(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()

	// get the boot kernel participant from our new kernel snap
	bootKern := boot.Participant(s.kern2, snap.TypeKernel, coreDev)
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
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{s.kern2})

	// and that the modeenv now has this kernel listed
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename(), s.kern2.Filename()})
}

func (s *bootenv20EnvRefKernelSuite) TestCoreParticipant20SetNextNewKernelSnap(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()

	// get the boot kernel participant from our new kernel snap
	bootKern := boot.Participant(s.kern2, snap.TypeKernel, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// make the kernel used on next boot
	rebootRequired, err := bootKern.SetNextBoot()
	c.Assert(err, IsNil)
	c.Assert(rebootRequired, Equals, true)

	// make sure the env was updated
	m := s.bootloader.BootVars
	c.Assert(m, DeepEquals, map[string]string{
		"kernel_status":   boot.TryStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": s.kern2.Filename(),
	})

	// and that the modeenv now has this kernel listed
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename(), s.kern2.Filename()})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20KernelStatusTryingNoKernelSnapCleansUp(c *C) {
	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// set all the same vars as if we were doing trying, except don't set a try
	// kernel
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.base1.Filename(),
				CurrentKernels: []string{s.kern1.Filename(), s.kern2.Filename()},
			},
			kern: s.kern1,
			// no try-kernel
			kernStatus: boot.TryingStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check that the bootloader variable was cleaned
	expected := map[string]string{"kernel_status": boot.DefaultStatus}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that MarkBootSuccessful didn't enable a kernel (since there was no
	// try kernel)
	_, nEnableCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(nEnableCalls, Equals, 0)

	// we will always end up disabling a try-kernel though as cleanup
	_, nDisableTryCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 1)

	// do it again, verify it's still okay
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// no new enabled kernels
	_, nEnableCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(nEnableCalls, Equals, 0)

	// again we will try to cleanup any leftover try-kernels
	_, nDisableTryCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 2)

	// check that the modeenv re-wrote the CurrentKernels
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename()})
}

func (s *bootenv20EnvRefKernelSuite) TestMarkBootSuccessful20KernelStatusTryingNoKernelSnapCleansUp(c *C) {
	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// set all the same vars as if we were doing trying, except don't set a try
	// kernel
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.base1.Filename(),
				CurrentKernels: []string{s.kern1.Filename(), s.kern2.Filename()},
			},
			kern: s.kern1,
			// no try-kernel
			kernStatus: boot.TryingStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// make sure the env was updated
	expected := map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": "",
	}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// do it again, verify it's still okay
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that the modeenv re-wrote the CurrentKernels
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename()})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20BaseStatusTryingNoTryBaseSnapCleansUp(c *C) {
	m := &boot.Modeenv{
		Mode: "run",
		Base: s.base1.Filename(),
		// no TryBase set
		BaseStatus: boot.TryingStatus,
	}
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv: m,
			// no kernel setup necessary
		},
	)
	defer r()

	coreDev := boottest.MockUC20Device("core20")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
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

func (s *bootenv20Suite) TestCoreParticipant20SetNextSameBaseSnap(c *C) {
	coreDev := boottest.MockUC20Device("core20")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	m := &boot.Modeenv{
		Mode: "run",
		Base: s.base1.Filename(),
	}
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv: m,
			// no kernel setup necessary
		},
	)
	defer r()

	// get the boot base participant from our base snap
	bootBase := boot.Participant(s.base1, snap.TypeBase, coreDev)
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

func (s *bootenv20Suite) TestCoreParticipant20SetNextNewBaseSnap(c *C) {
	coreDev := boottest.MockUC20Device("core20")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// default state
	m := &boot.Modeenv{
		Mode: "run",
		Base: s.base1.Filename(),
	}
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv: m,
			// no kernel setup necessary
		},
	)
	defer r()

	// get the boot base participant from our new base snap
	bootBase := boot.Participant(s.base2, snap.TypeBase, coreDev)
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
	c.Assert(m2.TryBase, Equals, s.base2.Filename())
}

func (s *bootenvSuite) TestMarkBootSuccessfulAllSnap(c *C) {
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

func (s *bootenv20Suite) TestMarkBootSuccessful20AllSnap(c *C) {
	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// bonus points: we were trying both a base snap and a kernel snap
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		TryBase:        s.base2.Filename(),
		BaseStatus:     boot.TryingStatus,
		CurrentKernels: []string{s.kern1.Filename(), s.kern2.Filename()},
	}
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			tryKern:    s.kern2,
			kernStatus: boot.TryingStatus,
		},
	)
	defer r()

	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the bootloader variables
	expected := map[string]string{
		// cleared
		"kernel_status": boot.DefaultStatus,
	}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that we called EnableKernel() on the try-kernel
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{s.kern2})

	// and that we disabled a try kernel
	_, nDisableTryCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 1)

	// also check that the modeenv was updated
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.Base, Equals, s.base2.Filename())
	c.Assert(m2.TryBase, Equals, "")
	c.Assert(m2.BaseStatus, Equals, boot.DefaultStatus)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// no new enabled kernels
	actual, _ = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{s.kern2})
	// we always disable the try kernel as a cleanup operation, so there's one
	// more call here
	_, nDisableTryCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 2)
}

func (s *bootenv20EnvRefKernelSuite) TestMarkBootSuccessful20AllSnap(c *C) {
	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// bonus points: we were trying both a base snap and a kernel snap
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		TryBase:        s.base2.Filename(),
		BaseStatus:     boot.TryingStatus,
		CurrentKernels: []string{s.kern1.Filename(), s.kern2.Filename()},
	}
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			tryKern:    s.kern2,
			kernStatus: boot.TryingStatus,
		},
	)
	defer r()

	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the bootloader variables
	expected := map[string]string{
		// cleared
		"kernel_status":   boot.DefaultStatus,
		"snap_try_kernel": "",
		// enabled new kernel
		"snap_kernel": s.kern2.Filename(),
	}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// also check that the modeenv was updated
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.Base, Equals, s.base2.Filename())
	c.Assert(m2.TryBase, Equals, "")
	c.Assert(m2.BaseStatus, Equals, boot.DefaultStatus)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)
}

func (s *bootenvSuite) TestMarkBootSuccessfulKernelUpdate(c *C) {
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

func (s *bootenvSuite) TestMarkBootSuccessfulBaseUpdate(c *C) {
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

func (s *bootenv20Suite) TestMarkBootSuccessful20KernelUpdate(c *C) {
	// trying a kernel snap
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename(), s.kern2.Filename()},
	}
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			tryKern:    s.kern2,
			kernStatus: boot.TryingStatus,
		},
	)
	defer r()

	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the bootloader variables
	expected := map[string]string{"kernel_status": boot.DefaultStatus}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that MarkBootSuccessful enabled the try kernel
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{s.kern2})

	// and that we disabled a try kernel
	_, nDisableTryCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 1)

	// check that the new kernel is the only one in modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// no new bootloader calls
	actual, _ = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{s.kern2})

	// we did disable the kernel again because we always do this to cleanup in
	// case there were leftovers
	_, nDisableTryCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 2)
}

func (s *bootenv20EnvRefKernelSuite) TestMarkBootSuccessful20KernelUpdate(c *C) {
	// trying a kernel snap
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename(), s.kern2.Filename()},
	}
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			tryKern:    s.kern2,
			kernStatus: boot.TryingStatus,
		},
	)
	defer r()

	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the bootloader variables
	expected := map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern2.Filename(),
		"snap_try_kernel": "",
	}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that the new kernel is the only one in modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)
}

func (s *bootenv20Suite) TestMarkBootSuccessful20BaseUpdate(c *C) {
	// we were trying a base snap
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		TryBase:        s.base2.Filename(),
		BaseStatus:     boot.TryingStatus,
		CurrentKernels: []string{s.kern1.Filename()},
	}
	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	coreDev := boottest.MockUC20Device("some-snap")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.Base, Equals, s.base2.Filename())
	c.Assert(m2.TryBase, Equals, "")
	c.Assert(m2.BaseStatus, Equals, "")

	// do it again, verify its still valid
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv again
	m3, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m3.Base, Equals, s.base2.Filename())
	c.Assert(m3.TryBase, Equals, "")
	c.Assert(m3.BaseStatus, Equals, "")
}

type recoveryBootenv20Suite struct {
	baseBootenvSuite

	bootloader *bootloadertest.MockBootloader

	dev boot.Device
}

var _ = Suite(&recoveryBootenv20Suite{})

func (s *recoveryBootenv20Suite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	s.forceBootloader(s.bootloader)

	s.dev = boottest.MockUC20Device("some-snap")
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeHappy(c *C) {
	err := boot.SetRecoveryBootSystemAndMode(s.dev, "1234", "install")
	c.Assert(err, IsNil)
	c.Check(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snapd_recovery_system": "1234",
		"snapd_recovery_mode":   "install",
	})
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeSetErr(c *C) {
	s.bootloader.SetErr = errors.New("no can do")
	err := boot.SetRecoveryBootSystemAndMode(s.dev, "1234", "install")
	c.Assert(err, ErrorMatches, `no can do`)
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeNonUC20(c *C) {
	non20Dev := boottest.MockDevice("some-snap")
	err := boot.SetRecoveryBootSystemAndMode(non20Dev, "1234", "install")
	c.Assert(err, Equals, boot.ErrUnsupportedSystemMode)
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeErrClumsy(c *C) {
	err := boot.SetRecoveryBootSystemAndMode(s.dev, "", "install")
	c.Assert(err, ErrorMatches, "internal error: system label is unset")
	err = boot.SetRecoveryBootSystemAndMode(s.dev, "1234", "")
	c.Assert(err, ErrorMatches, "internal error: system mode is unset")
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeRealHappy(c *C) {
	bootloader.Force(nil)

	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	err := os.MkdirAll(mockSeedGrubDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(mockSeedGrubDir, "grub.cfg"), nil, 0644)
	c.Assert(err, IsNil)

	err = boot.SetRecoveryBootSystemAndMode(s.dev, "1234", "install")
	c.Assert(err, IsNil)

	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	blvars, err := bl.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Check(blvars, DeepEquals, map[string]string{
		"snapd_recovery_system": "1234",
		"snapd_recovery_mode":   "install",
	})
}
