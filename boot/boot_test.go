// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func TestBoot(t *testing.T) { TestingT(t) }

type baseBootenvSuite struct {
	testutil.BaseTest

	rootdir     string
	bootdir     string
	cmdlineFile string
}

func (s *baseBootenvSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.AddCleanup(restore)

	s.bootdir = filepath.Join(s.rootdir, "boot")

	s.cmdlineFile = filepath.Join(c.MkDir(), "cmdline")
	restore = osutil.MockProcCmdline(s.cmdlineFile)
	s.AddCleanup(restore)
}

func (s *baseBootenvSuite) forceBootloader(bloader bootloader.Bootloader) {
	bootloader.Force(bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *baseBootenvSuite) stampSealedKeys(c *C, rootdir string) {
	stamp := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	c.Assert(os.MkdirAll(filepath.Dir(stamp), 0755), IsNil)
	err := ioutil.WriteFile(stamp, nil, 0644)
	c.Assert(err, IsNil)
}

func (s *baseBootenvSuite) mockCmdline(c *C, cmdline string) {
	c.Assert(ioutil.WriteFile(s.cmdlineFile, []byte(cmdline), 0644), IsNil)
}

// mockAssetsCache mocks the listed assets in the boot assets cache by creating
// an empty file for each.
func mockAssetsCache(c *C, rootdir, bootloaderName string, cachedAssets []string) {
	p := filepath.Join(dirs.SnapBootAssetsDirUnder(rootdir), bootloaderName)
	err := os.MkdirAll(p, 0755)
	c.Assert(err, IsNil)
	for _, cachedAsset := range cachedAssets {
		err = ioutil.WriteFile(filepath.Join(p, cachedAsset), nil, 0644)
		c.Assert(err, IsNil)
	}
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

	kern1  snap.PlaceInfo
	kern2  snap.PlaceInfo
	ukern1 snap.PlaceInfo
	ukern2 snap.PlaceInfo
	base1  snap.PlaceInfo
	base2  snap.PlaceInfo

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

	s.ukern1, err = snap.ParsePlaceInfoFromSnapFileName("pc-kernel_x1.snap")
	c.Assert(err, IsNil)
	s.ukern2, err = snap.ParsePlaceInfoFromSnapFileName("pc-kernel_x2.snap")
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

	s.mockCmdline(c, "snapd_recovery_mode=run")
}

type bootenv20Suite struct {
	baseBootenv20Suite

	bootloader *bootloadertest.MockExtractedRunKernelImageBootloader
}

type bootenv20EnvRefKernelSuite struct {
	baseBootenv20Suite

	bootloader *bootloadertest.MockBootloader
}

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
	default:
		c.Fatalf("unsupported bootloader %T", bl)
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
	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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

func (s *bootenv20Suite) TestCoreParticipant20SetNextNewKernelSnapWithReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-" + dataHash,
	})

	assetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)), bootloader.RoleRunMode)
	runKernelBf := bootloader.NewBootFile(filepath.Join(s.kern1.Filename()), "kernel.efi", bootloader.RoleRunMode)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		// TODO:UC20: fix mocked trusted assets bootloader to actually
		// geenerate kernel boot files
		runKernelBf,
	}

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	model := coreDev.Model()

	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}

	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++

		c.Assert(params.ModelParams, HasLen, 1)
		mp := params.ModelParams[0]
		c.Check(mp.Model.Model(), Equals, model.Model())
		for _, ch := range mp.EFILoadChains {
			printChain(c, ch, "-")
		}
		c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
			secboot.NewLoadChain(assetBf,
				secboot.NewLoadChain(runKernelBf)),
			secboot.NewLoadChain(assetBf,
				// TODO:UC20: once mock trusted assets
				// bootloader can generated boot files for the
				// kernel this will use candidate kernel
				secboot.NewLoadChain(runKernelBf)),
		})
		// actual paths are seen only here
		c.Check(tab.BootChainKernelPath, DeepEquals, []string{
			s.kern1.MountFile(),
			s.kern2.MountFile(),
		})
		return nil
	})
	defer restore()

	// get the boot kernel participant from our new kernel snap
	bootKern := boot.Participant(s.kern2, snap.TypeKernel, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// make the kernel used on next boot
	rebootRequired, err := bootKern.SetNextBoot()
	c.Assert(err, IsNil)
	c.Assert(rebootRequired, Equals, true)

	// make sure the env was updated
	bvars, err := tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel")
	c.Assert(err, IsNil)
	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.TryStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": s.kern2.Filename(),
	})

	// and that the modeenv now has this kernel listed
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename(), s.kern2.Filename()})

	c.Check(resealCalls, Equals, 1)
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextNewUnassertedKernelSnapWithReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-" + dataHash,
	})

	assetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)), bootloader.RoleRunMode)
	runKernelBf := bootloader.NewBootFile(filepath.Join(s.ukern1.Filename()), "kernel.efi", bootloader.RoleRunMode)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		// TODO:UC20: fix mocked trusted assets bootloader to actually
		// geenerate kernel boot files
		runKernelBf,
	}

	uc20Model := boottest.MakeMockUC20Model()
	coreDev := boottest.MockUC20Device("", uc20Model)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.ukern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		Model:          uc20Model.Model(),
		BrandID:        uc20Model.BrandID(),
		Grade:          string(uc20Model.Grade()),
		ModelSignKeyID: uc20Model.SignKeyID(),
	}

	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.ukern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++

		c.Assert(params.ModelParams, HasLen, 1)
		mp := params.ModelParams[0]
		c.Check(mp.Model.Model(), Equals, uc20Model.Model())
		for _, ch := range mp.EFILoadChains {
			printChain(c, ch, "-")
		}
		c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
			secboot.NewLoadChain(assetBf,
				secboot.NewLoadChain(runKernelBf)),
			secboot.NewLoadChain(assetBf,
				// TODO:UC20: once mock trusted assets
				// bootloader can generated boot files for the
				// kernel this will use candidate kernel
				secboot.NewLoadChain(runKernelBf)),
		})
		// actual paths are seen only here
		c.Check(tab.BootChainKernelPath, DeepEquals, []string{
			s.ukern1.MountFile(),
			s.ukern2.MountFile(),
		})
		return nil
	})
	defer restore()

	// get the boot kernel participant from our new kernel snap
	bootKern := boot.Participant(s.ukern2, snap.TypeKernel, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// make the kernel used on next boot
	rebootRequired, err := bootKern.SetNextBoot()
	c.Assert(err, IsNil)
	c.Assert(rebootRequired, Equals, true)

	bvars, err := tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel")
	c.Assert(err, IsNil)
	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.TryStatus,
		"snap_kernel":     s.ukern1.Filename(),
		"snap_try_kernel": s.ukern2.Filename(),
	})

	// and that the modeenv now has this kernel listed
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.ukern1.Filename(), s.ukern2.Filename()})

	c.Check(resealCalls, Equals, 1)
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextSameKernelSnapNoReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-" + dataHash,
	})

	runKernelBf := bootloader.NewBootFile(filepath.Join(s.kern1.Filename()), "kernel.efi", bootloader.RoleRunMode)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		runKernelBf,
	}

	uc20Model := boottest.MakeMockUC20Model()
	coreDev := boottest.MockUC20Device("", uc20Model)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		CurrentKernelCommandLines: boot.BootCommandLines{"snapd_recovery_mode=run"},

		Model:          uc20Model.Model(),
		BrandID:        uc20Model.BrandID(),
		Grade:          string(uc20Model.Grade()),
		ModelSignKeyID: uc20Model.SignKeyID(),
	}

	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	// get the boot kernel participant from our kernel snap
	bootKern := boot.Participant(s.kern1, snap.TypeKernel, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// write boot-chains for current state that will stay unchanged
	bootChains := []boot.BootChain{{
		BrandID:        "my-brand",
		Model:          "my-model-uc20",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
		AssetChain: []boot.BootAsset{
			{
				Role: bootloader.RoleRunMode,
				Name: "asset",
				Hashes: []string{
					"0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8",
				},
			},
		},
		Kernel:         "pc-kernel",
		KernelRevision: "1",
		KernelCmdlines: []string{"snapd_recovery_mode=run"},
	}}
	err := boot.WriteBootChains(bootChains, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0)
	c.Assert(err, IsNil)

	// make the kernel used on next boot
	rebootRequired, err := bootKern.SetNextBoot()
	c.Assert(err, IsNil)
	c.Assert(rebootRequired, Equals, false)

	// make sure the env is as expected
	bvars, err := tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel")
	c.Assert(err, IsNil)
	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": "",
	})

	// and that the modeenv now has the one kernel listed
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename()})

	// boot chains were built
	c.Check(tab.BootChainKernelPath, DeepEquals, []string{
		s.kern1.MountFile(),
	})
	// no actual reseal
	c.Check(resealCalls, Equals, 0)
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextSameUnassertedKernelSnapNoReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-" + dataHash,
	})

	runKernelBf := bootloader.NewBootFile(filepath.Join(s.ukern1.Filename()), "kernel.efi", bootloader.RoleRunMode)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		runKernelBf,
	}

	uc20Model := boottest.MakeMockUC20Model()
	coreDev := boottest.MockUC20Device("", uc20Model)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.ukern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		CurrentKernelCommandLines: boot.BootCommandLines{"snapd_recovery_mode=run"},

		Model:          uc20Model.Model(),
		BrandID:        uc20Model.BrandID(),
		Grade:          string(uc20Model.Grade()),
		ModelSignKeyID: uc20Model.SignKeyID(),
	}

	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.ukern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	// get the boot kernel participant from our kernel snap
	bootKern := boot.Participant(s.ukern1, snap.TypeKernel, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// write boot-chains for current state that will stay unchanged
	bootChains := []boot.BootChain{{
		BrandID:        "my-brand",
		Model:          "my-model-uc20",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
		AssetChain: []boot.BootAsset{
			{
				Role: bootloader.RoleRunMode,
				Name: "asset",
				Hashes: []string{
					"0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8",
				},
			},
		},
		Kernel:         "pc-kernel",
		KernelRevision: "",
		KernelCmdlines: []string{"snapd_recovery_mode=run"},
	}}
	err := boot.WriteBootChains(bootChains, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0)
	c.Assert(err, IsNil)

	// make the kernel used on next boot
	rebootRequired, err := bootKern.SetNextBoot()
	c.Assert(err, IsNil)
	c.Assert(rebootRequired, Equals, false)

	// make sure the env is as expected
	bvars, err := tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel")
	c.Assert(err, IsNil)
	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.ukern1.Filename(),
		"snap_try_kernel": "",
	})

	// and that the modeenv now has the one kernel listed
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.ukern1.Filename()})

	// boot chains were built
	c.Check(tab.BootChainKernelPath, DeepEquals, []string{
		s.ukern1.MountFile(),
	})
	// no actual reseal
	c.Check(resealCalls, Equals, 0)
}

func (s *bootenv20EnvRefKernelSuite) TestCoreParticipant20SetNextNewKernelSnap(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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

	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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

func (s *bootenv20Suite) TestCoreParticipant20SetNextNewBaseSnapNoReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	// set up all the bits required for an encrypted system
	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})
	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-" + dataHash,
	})
	runKernelBf := bootloader.NewBootFile(filepath.Join(s.kern1.Filename()), "kernel.efi", bootloader.RoleRunMode)
	// write boot-chains for current state that will stay unchanged even
	// though base is changed
	bootChains := []boot.BootChain{{
		BrandID:        "my-brand",
		Model:          "my-model-uc20",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
		AssetChain: []boot.BootAsset{{
			Role: bootloader.RoleRunMode, Name: "asset", Hashes: []string{
				"0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8",
			},
		}},
		Kernel:         "pc-kernel",
		KernelRevision: "1",
		KernelCmdlines: []string{"snapd_recovery_mode=run"},
	}}

	err := boot.WriteBootChains(boot.ToPredictableBootChains(bootChains), filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0)
	c.Assert(err, IsNil)

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	model := coreDev.Model()

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		runKernelBf,
	}

	// default state
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
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

	// no reseal
	c.Check(resealCalls, Equals, 0)
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
	coreDev := boottest.MockUC20Device("", nil)
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
	coreDev := boottest.MockUC20Device("", nil)
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

	coreDev := boottest.MockUC20Device("", nil)
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

func (s *bootenv20Suite) TestMarkBootSuccessful20KernelUpdateWithReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-" + dataHash,
	})

	assetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)), bootloader.RoleRunMode)
	newRunKernelBf := bootloader.NewBootFile(filepath.Join(s.kern2.Filename()), "kernel.efi", bootloader.RoleRunMode)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		newRunKernelBf,
	}

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	model := coreDev.Model()

	// trying a kernel snap
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename(), s.kern2.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			tryKern:    s.kern2,
			kernStatus: boot.TryingStatus,
		},
	)
	defer r()

	// write boot-chains that describe a state in which we have a new kernel
	// candidate (pc-kernel_2)
	bootChains := []boot.BootChain{{
		BrandID:        "my-brand",
		Model:          "my-model-uc20",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
		AssetChain: []boot.BootAsset{{
			Role: bootloader.RoleRunMode, Name: "asset", Hashes: []string{
				"0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8",
			},
		}},
		Kernel:         "pc-kernel",
		KernelRevision: "1",
		KernelCmdlines: []string{"snapd_recovery_mode=run"},
	}, {
		BrandID:        "my-brand",
		Model:          "my-model-uc20",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
		AssetChain: []boot.BootAsset{{
			Role: bootloader.RoleRunMode, Name: "asset", Hashes: []string{
				"0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8",
			},
		}},
		Kernel:         "pc-kernel",
		KernelRevision: "2",
		KernelCmdlines: []string{"snapd_recovery_mode=run"},
	}}

	err := boot.WriteBootChains(boot.ToPredictableBootChains(bootChains), filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0)
	c.Assert(err, IsNil)

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++

		c.Assert(params.ModelParams, HasLen, 1)
		mp := params.ModelParams[0]
		c.Check(mp.Model.Model(), Equals, model.Model())
		for _, ch := range mp.EFILoadChains {
			printChain(c, ch, "-")
		}
		c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
			secboot.NewLoadChain(assetBf,
				secboot.NewLoadChain(newRunKernelBf)),
		})
		return nil
	})
	defer restore()

	// mark successful
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	c.Check(resealCalls, Equals, 1)
	// check the bootloader variables
	expected := map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern2.Filename(),
		"snap_try_kernel": boot.DefaultStatus,
	}
	c.Assert(tab.BootVars, DeepEquals, expected)
	c.Check(tab.BootChainKernelPath, DeepEquals, []string{s.kern2.MountFile()})

	// check that the new kernel is the only one in modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})
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

	coreDev := boottest.MockUC20Device("", nil)
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

	coreDev := boottest.MockUC20Device("", nil)
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

func (s *bootenv20Suite) bootloaderWithTrustedAssets(c *C, trustedAssets []string) *bootloadertest.MockTrustedAssetsBootloader {
	// TODO:UC20: this should be an ExtractedRecoveryKernelImageBootloader
	// because that would reflect our main currently supported
	// trusted assets bootloader (grub)
	tab := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(tab)
	tab.TrustedAssetsList = trustedAssets
	s.AddCleanup(func() { bootloader.Force(nil) })
	return tab
}

func (s *bootenv20Suite) TestMarkBootSuccessful20BootAssetsUpdateHappy(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, []string{"asset", "shim"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"

	c.Assert(os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755), IsNil)
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755), IsNil)
	// only asset for ubuntu
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	// shim and asset for seed
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "shim"), shim, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"shim-recoveryshimhash",
		"shim-" + shimHash,
		"asset-assethash",
		"asset-recoveryassethash",
		"asset-" + dataHash,
	})

	shimBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)), bootloader.RoleRecovery)
	assetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)), bootloader.RoleRecovery)
	runKernelBf := bootloader.NewBootFile(filepath.Join(s.kern1.Filename()), "kernel.efi", bootloader.RoleRunMode)
	recoveryKernelBf := bootloader.NewBootFile("pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		runKernelBf,
	}
	tab.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		recoveryKernelBf,
	}

	uc20Model := boottest.MakeMockUC20Model()

	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		return uc20Model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	// we were trying an update of boot assets
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash", dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"recoveryassethash", dataHash},
			"shim":  {"recoveryshimhash", shimHash},
		},
		CurrentRecoverySystems: []string{"system"},

		Model:          uc20Model.Model(),
		BrandID:        uc20Model.BrandID(),
		Grade:          string(uc20Model.Grade()),
		ModelSignKeyID: uc20Model.SignKeyID(),
	}
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	coreDev := boottest.MockUC20Device("", uc20Model)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	resealCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++

		c.Assert(params.ModelParams, HasLen, 1)
		mp := params.ModelParams[0]
		c.Check(mp.Model.Model(), Equals, uc20Model.Model())
		for _, ch := range mp.EFILoadChains {
			printChain(c, ch, "-")
		}
		switch resealCalls {
		case 1:
			c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(recoveryKernelBf))),
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(runKernelBf))),
			})
		case 2:
			c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(recoveryKernelBf))),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKey (call # %d)", resealCalls)
		}
		return nil
	})
	defer restore()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// update assets are in the list
	c.Check(m2.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
	})
	c.Check(m2.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
		"shim":  {shimHash},
	})
	// unused files were dropped from cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-"+dataHash),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-"+shimHash),
	})
	c.Check(resealCalls, Equals, 2)
}

func (s *bootenv20Suite) TestMarkBootSuccessful20BootAssetsStableStateHappy(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, []string{"nested/asset", "shim"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "nested"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "nested"), 0755), IsNil)
	// only asset for ubuntu-boot
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "nested/asset"), data, 0644), IsNil)
	// shim and asset for ubuntu-seed
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "nested/asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "shim"), shim, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"shim-" + shimHash,
		"asset-" + dataHash,
	})

	runKernelBf := bootloader.NewBootFile(filepath.Join(s.kern1.Filename()), "kernel.efi", bootloader.RoleRunMode)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		runKernelBf,
	}
	tab.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		recoveryKernelBf,
	}

	uc20Model := boottest.MakeMockUC20Model()

	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		return uc20Model, []*seed.Snap{mockNamedKernelSeedSnap(snap.R(1), "pc-kernel-recovery"), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	// we were trying an update of boot assets
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
			"shim":  {shimHash},
		},
		CurrentRecoverySystems:    []string{"system"},
		CurrentKernelCommandLines: boot.BootCommandLines{"snapd_recovery_mode=run"},

		Model:          uc20Model.Model(),
		BrandID:        uc20Model.BrandID(),
		Grade:          string(uc20Model.Grade()),
		ModelSignKeyID: uc20Model.SignKeyID(),
	}
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	coreDev := boottest.MockUC20Device("", uc20Model)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	resealCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	// write boot-chains for current state that will stay unchanged
	bootChains := []boot.BootChain{{
		BrandID:        "my-brand",
		Model:          "my-model-uc20",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
		AssetChain: []boot.BootAsset{{
			Role: bootloader.RoleRecovery, Name: "shim",
			Hashes: []string{
				"dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b",
			},
		}, {
			Role: bootloader.RoleRecovery, Name: "asset", Hashes: []string{
				"0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8",
			},
		}},
		Kernel:         "pc-kernel",
		KernelRevision: "1",
		KernelCmdlines: []string{"snapd_recovery_mode=run"},
	}, {
		BrandID:        "my-brand",
		Model:          "my-model-uc20",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
		AssetChain: []boot.BootAsset{{
			Role: bootloader.RoleRecovery, Name: "shim",
			Hashes: []string{
				"dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b",
			},
		}, {
			Role: bootloader.RoleRecovery, Name: "asset", Hashes: []string{
				"0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8",
			},
		}},
		Kernel:         "pc-kernel-recovery",
		KernelRevision: "1",
		KernelCmdlines: []string{"snapd_recovery_mode=recover snapd_recovery_system=system"},
	}}

	recoveryBootChains := []boot.BootChain{bootChains[1]}

	err := boot.WriteBootChains(boot.ToPredictableBootChains(bootChains), filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0)
	c.Assert(err, IsNil)

	err = boot.WriteBootChains(boot.ToPredictableBootChains(recoveryBootChains), filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"), 0)
	c.Assert(err, IsNil)

	// mark successful
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// modeenv is unchanged
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m2.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(m2.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	// files are still in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-"+dataHash),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-"+shimHash),
	})

	// boot chains were built
	c.Check(tab.BootChainKernelPath, DeepEquals, []string{
		s.kern1.MountFile(),
	})
	// no actual reseal
	c.Check(resealCalls, Equals, 0)
}

func (s *bootenv20Suite) TestMarkBootSuccessful20BootUnassertedKernelAssetsStableStateHappy(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, []string{"nested/asset", "shim"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "nested"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "nested"), 0755), IsNil)
	// only asset for ubuntu-boot
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "nested/asset"), data, 0644), IsNil)
	// shim and asset for ubuntu-seed
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "nested/asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "shim"), shim, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"shim-" + shimHash,
		"asset-" + dataHash,
	})

	runKernelBf := bootloader.NewBootFile(filepath.Join(s.ukern1.Filename()), "kernel.efi", bootloader.RoleRunMode)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		runKernelBf,
	}
	tab.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		recoveryKernelBf,
	}

	uc20Model := boottest.MakeMockUC20Model()

	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		return uc20Model, []*seed.Snap{mockNamedKernelSeedSnap(snap.R(1), "pc-kernel-recovery"), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	// we were trying an update of boot assets
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.ukern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
			"shim":  {shimHash},
		},
		CurrentRecoverySystems:    []string{"system"},
		GoodRecoverySystems:       []string{"system"},
		CurrentKernelCommandLines: boot.BootCommandLines{"snapd_recovery_mode=run"},
		// leave this comment to keep old gofmt happy
		Model:          "my-model-uc20",
		BrandID:        "my-brand",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
	}
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.ukern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	coreDev := boottest.MockUC20Device("", uc20Model)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	resealCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	// write boot-chains for current state that will stay unchanged
	bootChains := []boot.BootChain{{
		BrandID:        "my-brand",
		Model:          "my-model-uc20",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
		AssetChain: []boot.BootAsset{{
			Role: bootloader.RoleRecovery, Name: "shim",
			Hashes: []string{
				"dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b",
			},
		}, {
			Role: bootloader.RoleRecovery, Name: "asset", Hashes: []string{
				"0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8",
			},
		}},
		Kernel: "pc-kernel",
		// unasserted kernel snap
		KernelRevision: "",
		KernelCmdlines: []string{"snapd_recovery_mode=run"},
	}, {
		BrandID:        "my-brand",
		Model:          "my-model-uc20",
		Grade:          "dangerous",
		ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
		AssetChain: []boot.BootAsset{{
			Role: bootloader.RoleRecovery, Name: "shim",
			Hashes: []string{
				"dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b",
			},
		}, {
			Role: bootloader.RoleRecovery, Name: "asset", Hashes: []string{
				"0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8",
			},
		}},
		Kernel:         "pc-kernel-recovery",
		KernelRevision: "1",
		KernelCmdlines: []string{"snapd_recovery_mode=recover snapd_recovery_system=system"},
	}}

	recoveryBootChains := []boot.BootChain{bootChains[1]}

	err := boot.WriteBootChains(boot.ToPredictableBootChains(bootChains), filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0)
	c.Assert(err, IsNil)

	err = boot.WriteBootChains(boot.ToPredictableBootChains(recoveryBootChains), filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"), 0)
	c.Assert(err, IsNil)

	// mark successful
	err = boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// modeenv is unchanged
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m2.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(m2.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	// files are still in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-"+dataHash),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-"+shimHash),
	})

	// boot chains were built
	c.Check(tab.BootChainKernelPath, DeepEquals, []string{
		s.ukern1.MountFile(),
	})
	// no actual reseal
	c.Check(resealCalls, Equals, 0)
}

func (s *bootenv20Suite) TestMarkBootSuccessful20BootAssetsUpdateUnexpectedAsset(c *C) {
	tab := s.bootloaderWithTrustedAssets(c, []string{"EFI/asset"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "EFI"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "EFI/asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/asset"), data, 0644), IsNil)
	// mock some state in the cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-one",
		"asset-two",
	})

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	model := coreDev.Model()

	// we were trying an update of boot assets
	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			// hash will not match
			"asset": {"one", "two"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"one", "two"},
		},
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot mark boot successful: cannot mark successful boot assets: system booted with unexpected run mode bootloader asset "EFI/asset" hash %s`, dataHash))

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchaged
	c.Check(m2.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(m2.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	// nothing was removed from cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-one"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-two"),
	})
}

func (s *bootenv20Suite) setupMarkBootSuccessful20CommandLine(c *C, model *asserts.Model, mode string, cmdlines boot.BootCommandLines) *boot.Modeenv {
	// mock some state in the cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-one",
	})
	// a pending kernel command line change
	m := &boot.Modeenv{
		Mode:           mode,
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename()},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"one"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"one"},
		},
		CurrentKernelCommandLines: cmdlines,

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	return m
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedHappy(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run candidate panic=-1")
	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	m := s.setupMarkBootSuccessful20CommandLine(c, coreDev.Model(), "run", boot.BootCommandLines{
		"snapd_recovery_mode=run panic=-1",
		"snapd_recovery_mode=run candidate panic=-1",
	})

	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()
	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchaged
	c.Check(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run candidate panic=-1",
	})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedOld(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run panic=-1")
	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	m := s.setupMarkBootSuccessful20CommandLine(c, coreDev.Model(), "run", boot.BootCommandLines{
		"snapd_recovery_mode=run panic=-1",
		"snapd_recovery_mode=run candidate panic=-1",
	})
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchaged
	c.Check(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run panic=-1",
	})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedMismatch(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run different")
	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	m := s.setupMarkBootSuccessful20CommandLine(c, coreDev.Model(), "run", boot.BootCommandLines{
		"snapd_recovery_mode=run",
		"snapd_recovery_mode=run candidate",
	})
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, ErrorMatches, `cannot mark boot successful: cannot mark successful boot command line: current command line content "snapd_recovery_mode=run different" not matching any expected entry`)
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedFallbackOnBootSuccessful(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run panic=-1")
	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})
	tab.StaticCommandLine = "panic=-1"
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	m := s.setupMarkBootSuccessful20CommandLine(c, coreDev.Model(), "run", nil)
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchaged
	c.Check(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run panic=-1",
	})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedFallbackOnBootMismatch(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run panic=-1 unexpected")
	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})
	tab.StaticCommandLine = "panic=-1"
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	m := s.setupMarkBootSuccessful20CommandLine(c, coreDev.Model(), "run", nil)
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, ErrorMatches, `cannot mark boot successful: cannot mark successful boot command line: unexpected current command line: "snapd_recovery_mode=run panic=-1 unexpected"`)
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineNonRunMode(c *C) {
	// recover mode
	s.mockCmdline(c, "snapd_recovery_mode=recover snapd_recovery_system=1234 panic=-1")
	tab := s.bootloaderWithTrustedAssets(c, []string{"asset"})
	tab.StaticCommandLine = "panic=-1"
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	// current command line does not match any of the run mode command lines
	m := s.setupMarkBootSuccessful20CommandLine(c, coreDev.Model(), "recover", boot.BootCommandLines{
		"snapd_recovery_mode=run panic=-1",
		"snapd_recovery_mode=run candidate panic=-1",
	})
	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchaged
	c.Check(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run panic=-1",
		"snapd_recovery_mode=run candidate panic=-1",
	})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedNoFDEManagedBootloader(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run candidate panic=-1")
	tab := s.bootloaderWithTrustedAssets(c, nil)
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	m := s.setupMarkBootSuccessful20CommandLine(c, coreDev.Model(), "run", boot.BootCommandLines{
		"snapd_recovery_mode=run panic=-1",
		"snapd_recovery_mode=run candidate panic=-1",
	})
	// without encryption, the trusted assets are not tracked in the modeenv,
	// but we still may want to track command lines so that the gadget can
	// contribute to the system command line
	m.CurrentTrustedBootAssets = nil
	m.CurrentTrustedRecoveryBootAssets = nil

	r := setupUC20Bootenv(
		c,
		tab.MockBootloader,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchaged
	c.Check(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run candidate panic=-1",
	})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineCompatNonTrustedBootloader(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run candidate panic=-1")
	// bootloader has no trusted assets
	bl := bootloadertest.Mock("not-trusted", "")
	bootloader.Force(bl)
	s.AddCleanup(func() { bootloader.Force(nil) })
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	m := s.setupMarkBootSuccessful20CommandLine(c, coreDev.Model(), "run", nil)
	// no trusted assets
	m.CurrentTrustedBootAssets = nil
	m.CurrentTrustedRecoveryBootAssets = nil
	// no kernel command lines tracked
	m.CurrentKernelCommandLines = nil

	r := setupUC20Bootenv(
		c,
		bl,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv isn't changed
	c.Check(m2.CurrentKernelCommandLines, HasLen, 0)
}

func (s *bootenv20Suite) TestMarkBootSuccessful20SystemsCompat(c *C) {
	b := bootloadertest.Mock("mock", s.bootdir)
	s.forceBootloader(b)

	m := &boot.Modeenv{
		Mode:                   "run",
		Base:                   s.base1.Filename(),
		CurrentKernels:         []string{s.kern1.Filename()},
		CurrentRecoverySystems: []string{"1234"},
	}

	r := setupUC20Bootenv(
		c,
		b,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// the list of good recovery systems has not been modified
	c.Check(m2.GoodRecoverySystems, DeepEquals, []string{"1234"})
	c.Check(m2.CurrentRecoverySystems, DeepEquals, []string{"1234"})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20SystemsPopulated(c *C) {
	b := bootloadertest.Mock("mock", s.bootdir)
	s.forceBootloader(b)

	m := &boot.Modeenv{
		Mode:                   "run",
		Base:                   s.base1.Filename(),
		CurrentKernels:         []string{s.kern1.Filename()},
		CurrentRecoverySystems: []string{"1234", "9999"},
		GoodRecoverySystems:    []string{"1234"},
	}

	r := setupUC20Bootenv(
		c,
		b,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// good recovery systems has been populated
	c.Check(m2.GoodRecoverySystems, DeepEquals, []string{"1234"})
	c.Check(m2.CurrentRecoverySystems, DeepEquals, []string{"1234", "9999"})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20ModelSignKeyIDPopulated(c *C) {
	b := bootloadertest.Mock("mock", s.bootdir)
	s.forceBootloader(b)

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	m := &boot.Modeenv{
		Mode:           "run",
		Base:           s.base1.Filename(),
		CurrentKernels: []string{s.kern1.Filename()},
		Model:          "my-model-uc20",
		BrandID:        "my-brand",
		Grade:          "dangerous",
		// sign key ID is unset
	}

	r := setupUC20Bootenv(
		c,
		b,
		&bootenv20Setup{
			modeenv:    m,
			kern:       s.kern1,
			kernStatus: boot.DefaultStatus,
		},
	)
	defer r()

	// mark successful
	err := boot.MarkBootSuccessful(coreDev)
	c.Assert(err, IsNil)

	// check the modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// model's sign key ID has been set
	c.Check(m2.ModelSignKeyID, Equals, "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij")
	c.Check(m2.Model, Equals, "my-model-uc20")
	c.Check(m2.BrandID, Equals, "my-brand")
	c.Check(m2.Grade, Equals, "dangerous")
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

	s.dev = boottest.MockUC20Device("", nil)
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

type bootConfigSuite struct {
	baseBootenvSuite

	bootloader *bootloadertest.MockTrustedAssetsBootloader
	gadgetSnap string
}

var _ = Suite(&bootConfigSuite{})

func (s *bootConfigSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("trusted", c.MkDir()).WithTrustedAssets()
	s.bootloader.StaticCommandLine = "this is mocked panic=-1"
	s.bootloader.CandidateStaticCommandLine = "mocked candidate panic=-1"
	s.forceBootloader(s.bootloader)

	s.mockCmdline(c, "snapd_recovery_mode=run this is mocked panic=-1")
	s.gadgetSnap = snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, nil)
}

func (s *bootConfigSuite) mockCmdline(c *C, cmdline string) {
	c.Assert(ioutil.WriteFile(s.cmdlineFile, []byte(cmdline), 0644), IsNil)
}

func (s *bootConfigSuite) TestBootConfigUpdateHappyNoKeysNoReseal(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	m := &boot.Modeenv{
		Mode: "run",
		CurrentKernelCommandLines: boot.BootCommandLines{
			"snapd_recovery_mode=run this is mocked panic=-1",
		},
	}
	c.Assert(m.WriteTo(""), IsNil)

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	updated, err := boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 0)
}

func (s *bootConfigSuite) TestBootConfigUpdateHappyWithReseal(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	runKernelBf := bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_600.snap", "kernel.efi", bootloader.RoleRunMode)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-hash-1",
	})

	s.bootloader.TrustedAssetsList = []string{"asset"}
	s.bootloader.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		runKernelBf,
	}
	s.bootloader.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		recoveryKernelBf,
	}
	m := &boot.Modeenv{
		Mode:           "run",
		CurrentKernels: []string{"pc-kernel_500.snap"},
		CurrentKernelCommandLines: boot.BootCommandLines{
			"snapd_recovery_mode=run this is mocked panic=-1",
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"hash-1"},
		},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"hash-1"},
		},
	}
	c.Assert(m.WriteTo(""), IsNil)

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		c.Assert(params, NotNil)
		c.Assert(params.ModelParams, HasLen, 1)
		c.Check(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
			"snapd_recovery_mode=run mocked candidate panic=-1",
			"snapd_recovery_mode=run this is mocked panic=-1",
		})
		return nil
	})
	defer restore()

	updated, err := boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 1)

	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run this is mocked panic=-1",
		"snapd_recovery_mode=run mocked candidate panic=-1",
	})
}

func (s *bootConfigSuite) TestBootConfigUpdateHappyNoChange(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	s.bootloader.StaticCommandLine = "mocked unchanged panic=-1"
	s.bootloader.CandidateStaticCommandLine = "mocked unchanged panic=-1"
	s.mockCmdline(c, "snapd_recovery_mode=run mocked unchanged panic=-1")

	m := &boot.Modeenv{
		Mode: "run",
		CurrentKernelCommandLines: boot.BootCommandLines{
			"snapd_recovery_mode=run mocked unchanged panic=-1",
		},
	}
	c.Assert(m.WriteTo(""), IsNil)

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	updated, err := boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 0)

	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernelCommandLines, HasLen, 1)
}

func (s *bootConfigSuite) TestBootConfigUpdateNonUC20DoesNothing(c *C) {
	nonUC20coreDev := boottest.MockDevice("pc-kernel")
	c.Assert(nonUC20coreDev.HasModeenv(), Equals, false)
	updated, err := boot.UpdateManagedBootConfigs(nonUC20coreDev, s.gadgetSnap)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 0)
}

func (s *bootConfigSuite) TestBootConfigUpdateBadModeErr(c *C) {
	uc20Dev := boottest.MockUC20Device("recover", nil)
	c.Assert(uc20Dev.HasModeenv(), Equals, true)
	updated, err := boot.UpdateManagedBootConfigs(uc20Dev, s.gadgetSnap)
	c.Assert(err, ErrorMatches, "internal error: boot config can only be updated in run mode")
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 0)
}

func (s *bootConfigSuite) TestBootConfigUpdateFailErr(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	m := &boot.Modeenv{
		Mode: "run",
		CurrentKernelCommandLines: boot.BootCommandLines{
			"snapd_recovery_mode=run this is mocked panic=-1",
		},
	}
	c.Assert(m.WriteTo(""), IsNil)

	s.bootloader.UpdateErr = errors.New("update fail")

	updated, err := boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap)
	c.Assert(err, ErrorMatches, "update fail")
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
}

func (s *bootConfigSuite) TestBootConfigUpdateCmdlineMismatchErr(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	m := &boot.Modeenv{
		Mode: "run",
	}
	c.Assert(m.WriteTo(""), IsNil)

	s.mockCmdline(c, "snapd_recovery_mode=run unexpected cmdline")

	updated, err := boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap)
	c.Assert(err, ErrorMatches, `internal error: current kernel command lines is unset`)
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 0)
}

func (s *bootConfigSuite) TestBootConfigUpdateNotManagedErr(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	bl := bootloadertest.Mock("not-managed", c.MkDir())
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	m := &boot.Modeenv{
		Mode: "run",
	}
	c.Assert(m.WriteTo(""), IsNil)

	updated, err := boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 0)
}

func (s *bootConfigSuite) TestBootConfigUpdateBootloaderFindErr(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	bootloader.ForceError(errors.New("mocked find error"))
	defer bootloader.ForceError(nil)

	m := &boot.Modeenv{
		Mode: "run",
	}
	c.Assert(m.WriteTo(""), IsNil)

	updated, err := boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap)
	c.Assert(err, ErrorMatches, "internal error: cannot find trusted assets bootloader under .*: mocked find error")
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 0)
}

func (s *bootConfigSuite) TestBootConfigUpdateWithGadgetAndReseal(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	gadgetSnap := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "foo bar baz"},
	})
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	runKernelBf := bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_600.snap", "kernel.efi", bootloader.RoleRunMode)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-hash-1",
	})

	s.bootloader.TrustedAssetsList = []string{"asset"}
	s.bootloader.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		runKernelBf,
	}
	s.bootloader.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		recoveryKernelBf,
	}
	m := &boot.Modeenv{
		Mode:           "run",
		CurrentKernels: []string{"pc-kernel_500.snap"},
		CurrentKernelCommandLines: boot.BootCommandLines{
			// the extra arguments would be included in the current
			// command line already
			"snapd_recovery_mode=run this is mocked panic=-1 foo bar baz",
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"hash-1"},
		},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"hash-1"},
		},
	}
	c.Assert(m.WriteTo(""), IsNil)

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		c.Assert(params, NotNil)
		c.Assert(params.ModelParams, HasLen, 1)
		c.Check(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
			"snapd_recovery_mode=run mocked candidate panic=-1 foo bar baz",
			"snapd_recovery_mode=run this is mocked panic=-1 foo bar baz",
		})
		return nil
	})
	defer restore()

	updated, err := boot.UpdateManagedBootConfigs(coreDev, gadgetSnap)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 1)

	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run this is mocked panic=-1 foo bar baz",
		"snapd_recovery_mode=run mocked candidate panic=-1 foo bar baz",
	})
}

func (s *bootConfigSuite) TestBootConfigUpdateWithGadgetFullAndReseal(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	gadgetSnap := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.full", "foo bar baz"},
	})
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// a minimal bootloader and modeenv setup that works because reseal is
	// not executed
	s.bootloader.TrustedAssetsList = []string{"asset"}
	m := &boot.Modeenv{
		Mode: "run",
		CurrentKernelCommandLines: boot.BootCommandLines{
			// the full arguments would be included in the current
			// command line already
			"snapd_recovery_mode=run foo bar baz",
		},
	}
	c.Assert(m.WriteTo(""), IsNil)

	s.bootloader.Updated = true

	resealCalls := 0
	// reseal does not happen, because the gadget overrides the static
	// command line which is part of boot config, thus there's no resulting
	// change in the command lines tracked in modeenv and no need to reseal
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	updated, err := boot.UpdateManagedBootConfigs(coreDev, gadgetSnap)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 0)

	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run foo bar baz",
	})
}

type bootKernelCommandLineSuite struct {
	baseBootenvSuite

	bootloader            *bootloadertest.MockTrustedAssetsBootloader
	gadgetSnap            string
	uc20dev               boot.Device
	recoveryKernelBf      bootloader.BootFile
	runKernelBf           bootloader.BootFile
	modeenvWithEncryption *boot.Modeenv
	resealCalls           int
	resealCommandLines    [][]string
}

var _ = Suite(&bootKernelCommandLineSuite{})

func (s *bootKernelCommandLineSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

	s.bootloader = bootloadertest.Mock("trusted", c.MkDir()).WithTrustedAssets()
	s.bootloader.TrustedAssetsList = []string{"asset"}
	s.bootloader.StaticCommandLine = "static mocked panic=-1"
	s.bootloader.CandidateStaticCommandLine = "mocked candidate panic=-1"
	s.forceBootloader(s.bootloader)

	s.mockCmdline(c, "snapd_recovery_mode=run this is mocked panic=-1")
	s.gadgetSnap = snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, nil)
	s.uc20dev = boottest.MockUC20Device("", boottest.MakeMockUC20Model(nil))
	s.runKernelBf = bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_600.snap", "kernel.efi", bootloader.RoleRunMode)
	s.recoveryKernelBf = bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-" + dataHash,
	})

	s.bootloader.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		s.runKernelBf,
	}
	s.bootloader.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		s.recoveryKernelBf,
	}
	s.modeenvWithEncryption = &boot.Modeenv{
		Mode:           "run",
		CurrentKernels: []string{"pc-kernel_500.snap"},
		Base:           "core20_1.snap",
		BaseStatus:     boot.DefaultStatus,
		CurrentKernelCommandLines: boot.BootCommandLines{
			// the extra arguments would be included in the current
			// command line already
			"snapd_recovery_mode=run static mocked panic=-1",
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{dataHash},
		},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{dataHash},
		},
	}
	s.bootloader.SetBootVars(map[string]string{
		"snap_kernel": "pc-kernel_500.snap",
	})
	s.bootloader.SetBootVarsCalls = 0

	s.resealCommandLines = nil
	s.resealCalls = 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		s.resealCalls++
		c.Assert(params, NotNil)
		c.Assert(params.ModelParams, HasLen, 1)
		s.resealCommandLines = append(s.resealCommandLines, params.ModelParams[0].KernelCmdlines)
		return nil
	})
	s.AddCleanup(restore)
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateNonUC20(c *C) {
	nonUC20dev := boottest.MockDevice("")

	// gadget which would otherwise trigger an update
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "foo"},
	})

	reboot, err := boot.UpdateCommandLineForGadgetComponent(nonUC20dev, sf)
	c.Assert(err, ErrorMatches, "internal error: command line component cannot be updated on non UC20 devices")
	c.Assert(reboot, Equals, false)
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20NotManagedBootloader(c *C) {
	// gadget which would otherwise trigger an update
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "foo"},
	})

	// but the bootloader is not managed by snapd
	bl := bootloadertest.Mock("not-managed", c.MkDir())
	bl.SetErr = fmt.Errorf("unexpected call")
	s.forceBootloader(bl)

	reboot, err := boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, false)
	c.Check(bl.SetBootVarsCalls, Equals, 0)
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20ArgsAdded(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "args from gadget"},
	})

	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	reboot, err := boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, true)

	// reseal happened
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 args from gadget",
	}})

	// modeenv has been updated
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 args from gadget",
	})

	// bootloader variables too
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	args, err := s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "args from gadget",
		"snapd_full_cmdline_args":  "",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20ArgsSwitch(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "no change"},
	})

	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1 no change"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)
	err := s.bootloader.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "no change",
		// this is intentionally filled and will be cleared
		"snapd_full_cmdline_args": "canary",
	})
	c.Assert(err, IsNil)
	s.bootloader.SetBootVarsCalls = 0

	reboot, err := boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, false)

	// no reseal needed
	c.Check(s.resealCalls, Equals, 0)

	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 no change",
	})
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	args, err := s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "no change",
		// canary is still present, as nothing was modified
		"snapd_full_cmdline_args": "canary",
	})

	// let's change them now
	sfChanged := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "changed"},
	})

	reboot, err = boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sfChanged)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, true)

	// reseal was applied
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run static mocked panic=-1 changed",
		"snapd_recovery_mode=run static mocked panic=-1 no change",
	}})

	// modeenv has been updated
	newM, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 no change",
		// new ones are appended
		"snapd_recovery_mode=run static mocked panic=-1 changed",
	})
	// and bootloader env too
	args, err = s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "changed",
		// canary has been cleared as bootenv was modified
		"snapd_full_cmdline_args": "",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20UnencryptedArgsRemoved(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	// pretend we used to have additional arguments from the gadget, but
	// those will be gone with new update
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, nil)

	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1 from-gadget"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)
	err := s.bootloader.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "from-gadget",
		// this is intentionally filled and will be cleared
		"snapd_full_cmdline_args": "canary",
	})
	c.Assert(err, IsNil)
	s.bootloader.SetBootVarsCalls = 0

	reboot, err := boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, true)

	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 from-gadget",
	}})

	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 from-gadget",
		"snapd_recovery_mode=run static mocked panic=-1",
	})
	// bootloader variables were explicitly cleared
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	args, err := s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20SetError(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	// pretend we used to have additional arguments from the gadget, but
	// those will be gone with new update
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "this-is-not-applied"},
	})

	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	s.bootloader.SetErr = fmt.Errorf("set fails")

	reboot, err := boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	c.Assert(err, ErrorMatches, "cannot set run system kernel command line arguments: set fails")
	c.Assert(reboot, Equals, false)
	// set boot vars was called and failed
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)

	// reseal with new parameters happened though
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 this-is-not-applied",
	}})

	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		// this will be cleared on next reboot or will get overwritten
		// by an update
		"snapd_recovery_mode=run static mocked panic=-1 this-is-not-applied",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateWithResealError(c *C) {
	gadgetSnap := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "args from gadget"},
	})

	s.stampSealedKeys(c, dirs.GlobalRootDir)
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return fmt.Errorf("reseal fails")
	})
	defer restore()

	reboot, err := boot.UpdateCommandLineForGadgetComponent(s.uc20dev, gadgetSnap)
	c.Assert(err, ErrorMatches, "cannot reseal the encryption key: reseal fails")
	c.Check(reboot, Equals, false)
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	c.Check(resealCalls, Equals, 1)

	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 args from gadget",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20TransitionFullExtraAndBack(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	// no command line arguments from gadget
	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)
	err := s.bootloader.SetBootVars(map[string]string{
		// those are intentionally filled by the test
		"snapd_extra_cmdline_args": "canary",
		"snapd_full_cmdline_args":  "canary",
	})
	c.Assert(err, IsNil)
	s.bootloader.SetBootVarsCalls = 0

	// transition to gadget with cmdline.extra
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "extra args"},
	})
	reboot, err := boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, true)
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	}})
	s.resealCommandLines = nil

	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	args, err := s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "extra args",
		// canary has been cleared
		"snapd_full_cmdline_args": "",
	})
	// this normally happens after booting
	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1 extra args"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	// transition to full override from gadget
	sfFull := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.full", "full args"},
	})
	reboot, err = boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sfFull)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, true)
	c.Check(s.resealCalls, Equals, 2)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run full args",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	}})
	s.resealCommandLines = nil
	// modeenv has been updated
	newM, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
		// new ones are appended
		"snapd_recovery_mode=run full args",
	})
	// and bootloader env too
	args, err = s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		// cleared
		"snapd_extra_cmdline_args": "",
		// and full arguments were set
		"snapd_full_cmdline_args": "full args",
	})
	// this normally happens after booting
	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run full args"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	// transition back to no arguments from the gadget
	sfNone := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, nil)
	reboot, err = boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sfNone)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, true)
	c.Check(s.resealCalls, Equals, 3)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run full args",
		"snapd_recovery_mode=run static mocked panic=-1",
	}})
	// modeenv has been updated again
	newM, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run full args",
		// new ones are appended
		"snapd_recovery_mode=run static mocked panic=-1",
	})
	// and bootloader env too
	args, err = s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		// both env variables have been cleared
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20OverSpuriousRebootsBeforeBootVarsSet(c *C) {
	// simulate spurious reboots
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	resealPanic := false
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		s.resealCalls++
		c.Logf("reseal call %v", s.resealCalls)
		c.Assert(params, NotNil)
		c.Assert(params.ModelParams, HasLen, 1)
		s.resealCommandLines = append(s.resealCommandLines, params.ModelParams[0].KernelCmdlines)
		if resealPanic {
			panic("reseal panic")
		}
		return nil
	})
	defer restore()

	// no command line arguments from gadget
	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
	err := ioutil.WriteFile(cmdlineFile, []byte("snapd_recovery_mode=run static mocked panic=-1"), 0644)
	c.Assert(err, IsNil)
	restore = osutil.MockProcCmdline(cmdlineFile)
	s.AddCleanup(restore)

	err = s.bootloader.SetBootVars(map[string]string{
		// those are intentionally filled by the test
		"snapd_extra_cmdline_args": "canary",
		"snapd_full_cmdline_args":  "canary",
	})
	c.Assert(err, IsNil)
	s.bootloader.SetBootVarsCalls = 0

	restoreBootloaderNoPanic := s.bootloader.SetMockToPanic("SetBootVars")
	defer restoreBootloaderNoPanic()

	// transition to gadget with cmdline.extra
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "extra args"},
	})

	// let's panic on reseal first
	resealPanic = true
	c.Assert(func() {
		boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	}, PanicMatches, "reseal panic")
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	}})
	// bootenv hasn't been updated yet
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	// but modeenv has already been updated
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})

	// REBOOT
	resealPanic = false
	err = boot.MarkBootSuccessful(s.uc20dev)
	c.Assert(err, IsNil)
	// we resealed after reboot, since modeenv was updated and carries the
	// current command line only
	c.Check(s.resealCalls, Equals, 2)
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
	})

	// try the update again, but no panic in reseal this time
	s.resealCalls = 0
	s.resealCommandLines = nil
	resealPanic = false
	// but panic in set
	c.Assert(func() {
		boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	}, PanicMatches, "mocked reboot panic in SetBootVars")
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	}})
	// the call to bootloader wasn't counted, because it called panic
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})

	// REBOOT
	err = boot.MarkBootSuccessful(s.uc20dev)
	c.Assert(err, IsNil)
	// we resealed after reboot again
	c.Check(s.resealCalls, Equals, 2)
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
	})

	// try again, for the last time, things should go smoothly
	s.resealCalls = 0
	s.resealCommandLines = nil
	restoreBootloaderNoPanic()
	reboot, err := boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	c.Assert(err, IsNil)
	c.Check(reboot, Equals, true)
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	// all done, modeenv
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})
	args, err := s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "extra args",
		// canary has been cleared
		"snapd_full_cmdline_args": "",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20OverSpuriousRebootsAfterBootVars(c *C) {
	// simulate spurious reboots
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	// no command line arguments from gadget
	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
	restore := osutil.MockProcCmdline(cmdlineFile)
	s.AddCleanup(restore)

	err := s.bootloader.SetBootVars(map[string]string{
		// those are intentionally filled by the test
		"snapd_extra_cmdline_args": "canary",
		"snapd_full_cmdline_args":  "canary",
	})
	c.Assert(err, IsNil)
	s.bootloader.SetBootVarsCalls = 0

	// transition to gadget with cmdline.extra
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "extra args"},
	})

	// let's panic after setting bootenv, but before returning, such that if
	// executed by a task handler, the task's status would not get updated
	s.bootloader.SetErrFunc = func() error {
		panic("mocked reboot panic after SetBootVars")
	}
	c.Assert(func() {
		boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	}, PanicMatches, "mocked reboot panic after SetBootVars")
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	}})
	// the call to bootloader was executed
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})
	args, err := s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "extra args",
		// canary has been cleared
		"snapd_full_cmdline_args": "",
	})

	// REBOOT; since we rebooted after updating the bootenv, the kernel
	// command line will include arguments that came from gadget snap
	s.bootloader.SetBootVarsCalls = 0
	s.resealCalls = 0
	err = ioutil.WriteFile(cmdlineFile, []byte("snapd_recovery_mode=run static mocked panic=-1 extra args"), 0644)
	c.Assert(err, IsNil)
	err = boot.MarkBootSuccessful(s.uc20dev)
	c.Assert(err, IsNil)
	// we resealed after reboot again
	c.Check(s.resealCalls, Equals, 1)
	// bootenv wasn't touched
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})

	// try again, as if the task handler gets to run again
	s.resealCalls = 0
	reboot, err := boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf)
	c.Assert(err, IsNil)
	// nothing changed now, we already booted with the new command line
	c.Check(reboot, Equals, false)
	// not reseal since nothing changed
	c.Check(s.resealCalls, Equals, 0)
	// no changes to the bootenv either
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	// all done, modeenv
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})
	args, err = s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "extra args",
		"snapd_full_cmdline_args":  "",
	})
}
