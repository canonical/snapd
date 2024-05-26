// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/strutil"
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

	restore := release.MockOnClassic(false)
	s.AddCleanup(restore)

	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	restore = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.AddCleanup(restore)

	s.bootdir = filepath.Join(s.rootdir, "boot")

	s.cmdlineFile = filepath.Join(c.MkDir(), "cmdline")
	restore = kcmdline.MockProcCmdline(s.cmdlineFile)
	s.AddCleanup(restore)
}

func (s *baseBootenvSuite) forceBootloader(bloader bootloader.Bootloader) {
	bootloader.Force(bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *baseBootenvSuite) stampSealedKeys(c *C, rootdir string) {
	stamp := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	c.Assert(os.MkdirAll(filepath.Dir(stamp), 0755), IsNil)
	mylog.Check(os.WriteFile(stamp, nil, 0644))

}

func (s *baseBootenvSuite) mockCmdline(c *C, cmdline string) {
	c.Assert(os.WriteFile(s.cmdlineFile, []byte(cmdline), 0644), IsNil)
}

// mockAssetsCache mocks the listed assets in the boot assets cache by creating
// an empty file for each.
func mockAssetsCache(c *C, rootdir, bootloaderName string, cachedAssets []string) {
	p := filepath.Join(dirs.SnapBootAssetsDirUnder(rootdir), bootloaderName)
	mylog.Check(os.MkdirAll(p, 0755))

	for _, cachedAsset := range cachedAssets {
		mylog.Check(os.WriteFile(filepath.Join(p, cachedAsset), nil, 0644))

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

	kern1   snap.PlaceInfo
	kern2   snap.PlaceInfo
	ukern1  snap.PlaceInfo
	ukern2  snap.PlaceInfo
	base1   snap.PlaceInfo
	base2   snap.PlaceInfo
	gadget1 snap.PlaceInfo
	gadget2 snap.PlaceInfo

	normalDefaultState      *bootenv20Setup
	normalTryingKernelState *bootenv20Setup
}

func (s *baseBootenv20Suite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	s.kern1 = mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap"))

	s.kern2 = mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap"))


	s.ukern1 = mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc-kernel_x1.snap"))

	s.ukern2 = mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc-kernel_x2.snap"))


	s.base1 = mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("core20_1.snap"))

	s.base2 = mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("core20_2.snap"))


	s.gadget1 = mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc_1.snap"))

	s.gadget2 = mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc_2.snap"))


	// default boot state for robustness tests, etc.
	s.normalDefaultState = &bootenv20Setup{
		modeenv: &boot.Modeenv{
			// base is base1
			Base: s.base1.Filename(),
			// no try base
			TryBase: "",
			// base status is default
			BaseStatus: boot.DefaultStatus,
			// gadget is gadget1
			Gadget: s.gadget1.Filename(),
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
			// gadget is gadget2
			Gadget: s.gadget2.Filename(),
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

type bootenv20RebootBootloaderSuite struct {
	baseBootenv20Suite

	bootloader *bootloadertest.MockRebootBootloader
}

var (
	_ = Suite(&bootenv20Suite{})
	_ = Suite(&bootenv20EnvRefKernelSuite{})
	_ = Suite(&bootenv20RebootBootloaderSuite{})
)

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

func (s *bootenv20RebootBootloaderSuite) SetUpTest(c *C) {
	s.baseBootenv20Suite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir()).WithRebootBootloader()
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
	origEnv := mylog.Check2(bl.GetBootVars("kernel_status"))

	mylog.Check(bl.SetBootVars(map[string]string{"kernel_status": opts.kernStatus}))

	cleanups = append(cleanups, func() {
		mylog.Check(bl.SetBootVars(origEnv))

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
		// for non-extracted, we need to use the bootenv to set the current kernels
		r := setupUC20MockBootloaderEnv(c, bl, opts)
		cleanups = append(cleanups, r)
		// don't count any calls to SetBootVars made thus far
		vbl.SetBootVarsCalls = 0
	case *bootloadertest.MockRebootBootloader:
		// for non-extracted, we need to use the bootenv to set the current kernels
		r := setupUC20MockBootloaderEnv(c, bl, opts)
		cleanups = append(cleanups, r)
		// don't count any calls to SetBootVars made thus far
		vbl.SetBootVarsCalls = 0
	default:
		c.Fatalf("unsupported bootloader %T", bl)
	}

	return func() {
		for _, r := range cleanups {
			r()
		}
	}
}

func setupUC20MockBootloaderEnv(c *C, bl bootloader.Bootloader, opts *bootenv20Setup) (restore func()) {
	origEnv := mylog.Check2(bl.GetBootVars("snap_kernel", "snap_try_kernel"))

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
	mylog.Check(bl.SetBootVars(m))


	return func() {
		mylog.Check(bl.SetBootVars(origEnv))

	}
}

func (s *bootenvSuite) TestInUseClassic(c *C) {
	classicDev := boottest.MockDevice("")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))

	inUse := mylog.Check2(boot.InUse(snap.TypeBase, classicDev))

	c.Check(inUse("core18", snap.R(41)), Equals, false)
}

func (s *bootenvSuite) TestInUseIrrelevantTypes(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))

	inUse := mylog.Check2(boot.InUse(snap.TypeGadget, coreDev))

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
		inUse := mylog.Check2(boot.InUse(typ, coreDev))

		c.Assert(inUse(t.snapName, t.snapRev), Equals, t.inUse, Commentf("unexpected result: %s %s %v", t.snapName, t.snapRev, t.inUse))
	}
}

func (s *bootenv20Suite) TestInUseCore20(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)
	c.Assert(coreDev.IsCoreBoot(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv: &boot.Modeenv{
				// base is base1
				Base: s.base1.Filename(),
				// no try base
				TryBase: "",
				// gadget is gadget1
				Gadget: s.gadget1.Filename(),
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
		})
	defer r()

	inUse := mylog.Check2(boot.InUse(snap.TypeKernel, coreDev))
	c.Check(err, IsNil)
	c.Check(inUse(s.kern1.SnapName(), s.kern1.SnapRevision()), Equals, true)
	c.Check(inUse(s.kern2.SnapName(), s.kern2.SnapRevision()), Equals, false)

	_ = mylog.Check2(boot.InUse(snap.TypeBase, coreDev))
	c.Check(err, IsNil)
}

func (s *bootenvSuite) TestInUseEphemeral(c *C) {
	coreDev := boottest.MockDevice("some-snap@install")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))

	inUse := mylog.Check2(boot.InUse(snap.TypeBase, coreDev))

	c.Check(inUse("whatever", snap.R(0)), Equals, true)
}

func (s *bootenvSuite) TestInUseUnhappy(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// make GetVars fail
	s.bootloader.GetErr = errors.New("zap")
	_ := mylog.Check2(boot.InUse(snap.TypeKernel, coreDev))
	c.Check(err, ErrorMatches, `cannot get boot variables: zap`)

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	_ = mylog.Check2(boot.InUse(snap.TypeKernel, coreDev))
	c.Check(err, ErrorMatches, `cannot get boot settings: broken bootloader`)
}

func (s *bootenvSuite) TestCurrentBootNameAndRevision(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	s.bootloader.BootVars["snap_core"] = "core_2.snap"
	s.bootloader.BootVars["snap_kernel"] = "canonical-pc-linux_2.snap"

	current := mylog.Check2(boot.GetCurrentBoot(snap.TypeOS, coreDev))
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, "core")
	c.Check(current.SnapRevision(), Equals, snap.R(2))

	current = mylog.Check2(boot.GetCurrentBoot(snap.TypeKernel, coreDev))
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, "canonical-pc-linux")
	c.Check(current.SnapRevision(), Equals, snap.R(2))

	s.bootloader.BootVars["snap_mode"] = boot.TryingStatus
	_ = mylog.Check2(boot.GetCurrentBoot(snap.TypeKernel, coreDev))
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

	current := mylog.Check2(boot.GetCurrentBoot(snap.TypeBase, coreDev))
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, s.base1.SnapName())
	c.Check(current.SnapRevision(), Equals, snap.R(1))

	current = mylog.Check2(boot.GetCurrentBoot(snap.TypeKernel, coreDev))
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, s.kern1.SnapName())
	c.Check(current.SnapRevision(), Equals, snap.R(1))

	s.bootloader.BootVars["kernel_status"] = boot.TryingStatus
	_ = mylog.Check2(boot.GetCurrentBoot(snap.TypeKernel, coreDev))
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

	current := mylog.Check2(boot.GetCurrentBoot(snap.TypeKernel, coreDev))

	c.Assert(current.SnapName(), Equals, s.kern1.SnapName())
	c.Assert(current.SnapRevision(), Equals, snap.R(1))
}

func (s *bootenvSuite) TestCurrentBootNameAndRevisionUnhappy(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	_ := mylog.Check2(boot.GetCurrentBoot(snap.TypeKernel, coreDev))
	c.Check(err, ErrorMatches, `cannot get name and revision of kernel \(snap_kernel\): boot variable unset`)

	_ = mylog.Check2(boot.GetCurrentBoot(snap.TypeOS, coreDev))
	c.Check(err, ErrorMatches, `cannot get name and revision of boot base \(snap_core\): boot variable unset`)

	_ = mylog.Check2(boot.GetCurrentBoot(snap.TypeBase, coreDev))
	c.Check(err, ErrorMatches, `cannot get name and revision of boot base \(snap_core\): boot variable unset`)

	_ = mylog.Check2(boot.GetCurrentBoot(snap.TypeApp, coreDev))
	c.Check(err, ErrorMatches, `internal error: no boot state handling for snap type "app"`)

	// validity check
	s.bootloader.BootVars["snap_kernel"] = "kernel_41.snap"
	current := mylog.Check2(boot.GetCurrentBoot(snap.TypeKernel, coreDev))
	c.Check(err, IsNil)
	c.Check(current.SnapName(), Equals, "kernel")
	c.Check(current.SnapRevision(), Equals, snap.R(41))

	// make GetVars fail
	s.bootloader.GetErr = errors.New("zap")
	_ = mylog.Check2(boot.GetCurrentBoot(snap.TypeKernel, coreDev))
	c.Check(err, ErrorMatches, "cannot get boot variables: zap")
	s.bootloader.GetErr = nil

	// make bootloader.Find fail
	bootloader.ForceError(errors.New("broken bootloader"))
	_ = mylog.Check2(boot.GetCurrentBoot(snap.TypeKernel, coreDev))
	c.Check(err, ErrorMatches, "cannot get boot settings: broken bootloader")
}

func (s *bootenvSuite) TestSnapTypeParticipatesInBoot(c *C) {
	classicDev := boottest.MockDevice("")
	legacyCoreDev := boottest.MockDevice("some-snap")
	coreDev := boottest.MockUC20Device("", nil)
	coreDevInstallMode := boottest.MockUC20Device("install", nil)

	for _, typ := range []snap.Type{
		snap.TypeKernel,
		snap.TypeOS,
		snap.TypeBase,
	} {
		c.Check(boot.SnapTypeParticipatesInBoot(typ, classicDev), Equals, false)
		c.Check(boot.SnapTypeParticipatesInBoot(typ, legacyCoreDev), Equals, true)
		c.Check(boot.SnapTypeParticipatesInBoot(typ, coreDev), Equals, true)
		c.Check(boot.SnapTypeParticipatesInBoot(typ, coreDevInstallMode), Equals, true)
	}

	classicWithModesDev := boottest.MockClassicWithModesDevice("", nil)
	c.Check(boot.SnapTypeParticipatesInBoot(snap.TypeKernel, classicWithModesDev), Equals, true)
	c.Check(boot.SnapTypeParticipatesInBoot(snap.TypeOS, classicWithModesDev), Equals, false)
	c.Check(boot.SnapTypeParticipatesInBoot(snap.TypeBase, classicWithModesDev), Equals, false)

	classicWithModesDevInstallMode := boottest.MockClassicWithModesDevice("install", nil)
	c.Check(boot.SnapTypeParticipatesInBoot(snap.TypeKernel, classicWithModesDevInstallMode), Equals, true)
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
	core20 := &snap.Info{SideInfo: snap.SideInfo{RealName: "core20"}, SnapType: snap.TypeBase}

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
		},
		{
			with:  core,
			model: "core",
			nop:   false,
		},
		{
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
		{
			with:  core20,
			model: "core@run",
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

func (s *bootenvSuite) TestParticipantGadgetWithModel(c *C) {
	gadget := &snap.Info{SideInfo: snap.SideInfo{RealName: "pc"}, SnapType: snap.TypeGadget}

	type tableT struct {
		with  *snap.Info
		model string
		nop   bool
	}

	table := []tableT{
		{
			with:  gadget,
			model: "",
			nop:   true,
		},
		{
			with:  gadget,
			model: "pc",
			nop:   true,
		},
		{
			with:  gadget,
			model: "pc@run",
			nop:   false,
		},
		{
			with:  gadget,
			model: "other-gadget",
			nop:   true,
		},
		{
			with:  gadget,
			model: "pc@install",
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

func (s *bootenvSuite) TestParticipantClassicWithModesWithModel(c *C) {
	modelHdrs := map[string]interface{}{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "baz-3000",
		"architecture": "amd64",
		"classic":      "true",
		"distribution": "ubuntu",
		"base":         "core22",
		"snaps": []interface{}{
			map[string]interface{}{
				"name": "kernel",
				"id":   "pclinuxdidididididididididididid",
				"type": "kernel",
			},
			map[string]interface{}{
				"name": "gadget",
				"id":   "pcididididididididididididididid",
				"type": "gadget",
			},
		},
	}
	model := assertstest.FakeAssertion(modelHdrs).(*asserts.Model)
	classicWithModesDev := boottest.MockClassicWithModesDevice("", model)

	tests := []struct {
		name       string
		typ        snap.Type
		nonTrivial bool
	}{
		{"some-snap", snap.TypeApp, false},
		{"core22", snap.TypeBase, false},
		{"kernel", snap.TypeKernel, true},
		{"gadget", snap.TypeGadget, true},
	}

	for _, t := range tests {
		info := &snap.Info{}
		info.RealName = t.name

		bp := boot.Participant(info, t.typ, classicWithModesDev)
		if !t.nonTrivial {
			c.Check(bp.IsTrivial(), Equals, true)
		} else {
			c.Check(bp, DeepEquals, boot.NewCoreBootParticipant(info, t.typ, classicWithModesDev))
		}
	}
}

func (s *bootenvSuite) TestMarkBootSuccessfulKernelStatusTryingNoTryKernelSnapCleansUp(c *C) {
	coreDev := boottest.MockDevice("some-snap")
	mylog.

		// set all the same vars as if we were doing trying, except don't set a try
		// kernel
		Check(s.bootloader.SetBootVars(map[string]string{
			"snap_kernel": "kernel_41.snap",
			"snap_mode":   boot.TryingStatus,
		}))

	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check that the bootloader variables were cleaned
	expected := map[string]string{
		"snap_mode":       boot.DefaultStatus,
		"snap_kernel":     "kernel_41.snap",
		"snap_try_kernel": "",
	}
	m := mylog.Check2(s.bootloader.GetBootVars("snap_mode", "snap_try_kernel", "snap_kernel"))

	c.Assert(m, DeepEquals, expected)
	mylog.

		// do it again, verify it's still okay
		Check(boot.MarkBootSuccessful(coreDev))

	m2 := mylog.Check2(s.bootloader.GetBootVars("snap_mode", "snap_try_kernel", "snap_kernel"))

	c.Assert(m2, DeepEquals, expected)
}

func (s *bootenvSuite) TestMarkBootSuccessfulTryKernelKernelStatusDefaultCleansUp(c *C) {
	coreDev := boottest.MockDevice("some-snap")
	mylog.

		// set an errant snap_try_kernel
		Check(s.bootloader.SetBootVars(map[string]string{
			"snap_kernel":     "kernel_41.snap",
			"snap_try_kernel": "kernel_42.snap",
			"snap_mode":       boot.DefaultStatus,
		}))

	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check that the bootloader variables were cleaned
	expected := map[string]string{
		"snap_mode":       boot.DefaultStatus,
		"snap_kernel":     "kernel_41.snap",
		"snap_try_kernel": "",
	}
	m := mylog.Check2(s.bootloader.GetBootVars("snap_mode", "snap_try_kernel", "snap_kernel"))

	c.Assert(m, DeepEquals, expected)
	mylog.

		// do it again, verify it's still okay
		Check(boot.MarkBootSuccessful(coreDev))

	m2 := mylog.Check2(s.bootloader.GetBootVars("snap_mode", "snap_try_kernel", "snap_kernel"))

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
	mylog.Check(bootKern.ExtractKernelAssets(kernelContainer))


	// make sure that the bootloader was told to extract some assets
	c.Assert(s.bootloader.ExtractKernelAssetsCalls, DeepEquals, []snap.PlaceInfo{s.kern1})
	mylog.

		// now remove the kernel assets and ensure that we get those calls
		Check(bootKern.RemoveKernelAssets())


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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// make sure that the bootloader was asked for the current kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)

	// ensure that kernel_status is still empty
	c.Assert(s.bootloader.BootVars["kernel_status"], Equals, boot.DefaultStatus)

	// there was no attempt to enable a kernel
	_, enableKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(enableKernelCalls, Equals, 0)

	// the modeenv is still the same as well
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// ensure that bootenv is unchanged
	m := mylog.Check2(s.bootloader.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(m, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": "",
	})

	// the modeenv is still the same as well
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, DeepEquals, boot.RebootInfo{
		RebootRequired: true,
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	})

	// make sure that the bootloader was asked for the current kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)

	// ensure that kernel_status is now try
	c.Assert(s.bootloader.BootVars["kernel_status"], Equals, boot.TryStatus)

	// and we were asked to enable kernel2 as the try kernel
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{s.kern2})

	// and that the modeenv now has this kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename(), s.kern2.Filename()})
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextNewKernelSnapWithReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

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
			"asset": []string{dataHash},
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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, DeepEquals, boot.RebootInfo{
		RebootRequired: true,
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	})

	// make sure the env was updated
	bvars := mylog.Check2(tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.TryStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": s.kern2.Filename(),
	})

	// and that the modeenv now has this kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename(), s.kern2.Filename()})

	c.Check(resealCalls, Equals, 1)
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextNewUnassertedKernelSnapWithReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

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
			"asset": []string{dataHash},
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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, DeepEquals, boot.RebootInfo{
		RebootRequired: true,
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	})

	bvars := mylog.Check2(tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.TryStatus,
		"snap_kernel":     s.ukern1.Filename(),
		"snap_try_kernel": s.ukern2.Filename(),
	})

	// and that the modeenv now has this kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.ukern1.Filename(), s.ukern2.Filename()})

	c.Check(resealCalls, Equals, 1)
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextSameKernelSnapNoReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

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
			"asset": []string{dataHash},
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
	mylog.Check(boot.WriteBootChains(bootChains, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0))


	// make the kernel used on next boot
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// make sure the env is as expected
	bvars := mylog.Check2(tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": "",
	})

	// and that the modeenv now has the one kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

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

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

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
			"asset": []string{dataHash},
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
	mylog.Check(boot.WriteBootChains(bootChains, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0))


	// make the kernel used on next boot
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// make sure the env is as expected
	bvars := mylog.Check2(tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.ukern1.Filename(),
		"snap_try_kernel": "",
	})

	// and that the modeenv now has the one kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, DeepEquals, boot.RebootInfo{
		RebootRequired: true,
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	})

	// make sure the env was updated
	m := s.bootloader.BootVars
	c.Assert(m, DeepEquals, map[string]string{
		"kernel_status":   boot.TryStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": s.kern2.Filename(),
	})

	// and that the modeenv now has this kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


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
	mylog.

		// do it again, verify it's still okay
		Check(boot.MarkBootSuccessful(coreDev))

	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// no new enabled kernels
	_, nEnableCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(nEnableCalls, Equals, 0)

	// again we will try to cleanup any leftover try-kernels
	_, nDisableTryCalls = s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 2)

	// check that the modeenv re-wrote the CurrentKernels
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// make sure the env was updated
	expected := map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": "",
	}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)
	mylog.

		// do it again, verify it's still okay
		Check(boot.MarkBootSuccessful(coreDev))


	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that the modeenv re-wrote the CurrentKernels
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check that the modeenv base_status was re-written to default
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.BaseStatus, Equals, boot.DefaultStatus)
	c.Assert(m2.Base, Equals, m.Base)
	c.Assert(m2.TryBase, Equals, m.TryBase)
	mylog.

		// do it again, verify it's still okay
		Check(boot.MarkBootSuccessful(coreDev))


	m3 := mylog.Check2(boot.ReadModeenv(""))

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
	rebootRequired := mylog.Check2(bootBase.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))


	// we don't need to reboot because it's the same base snap
	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// make sure the modeenv wasn't changed
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	rebootRequired := mylog.Check2(bootBase.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: true})

	// make sure the modeenv was updated
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.Base, Equals, m.Base)
	c.Assert(m2.BaseStatus, Equals, boot.TryStatus)
	c.Assert(m2.TryBase, Equals, s.base2.Filename())
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextNewBaseSnapNoReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	// set up all the bits required for an encrypted system
	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})
	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
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
	mylog.Check(boot.WriteBootChains(boot.ToPredictableBootChains(bootChains), filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0))


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
			"asset": []string{dataHash},
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
	rebootRequired := mylog.Check2(bootBase.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: true})

	// make sure the modeenv was updated
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.Check(boot.MarkBootSuccessful(coreDev))


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
	mylog.

		// do it again, verify its still valid
		Check(boot.MarkBootSuccessful(coreDev))

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
	mylog.Check(boot.MarkBootSuccessful(coreDev))


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
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.Base, Equals, s.base2.Filename())
	c.Assert(m2.TryBase, Equals, "")
	c.Assert(m2.BaseStatus, Equals, boot.DefaultStatus)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})
	mylog.

		// do it again, verify its still valid
		Check(boot.MarkBootSuccessful(coreDev))

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
	mylog.Check(boot.MarkBootSuccessful(coreDev))


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
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.Base, Equals, s.base2.Filename())
	c.Assert(m2.TryBase, Equals, "")
	c.Assert(m2.BaseStatus, Equals, boot.DefaultStatus)
	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})
	mylog.

		// do it again, verify its still valid
		Check(boot.MarkBootSuccessful(coreDev))

	c.Assert(s.bootloader.BootVars, DeepEquals, expected)
}

func (s *bootenvSuite) TestMarkBootSuccessfulKernelUpdate(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	s.bootloader.BootVars["snap_mode"] = boot.TryingStatus
	s.bootloader.BootVars["snap_core"] = "os1"
	s.bootloader.BootVars["snap_kernel"] = "k1"
	s.bootloader.BootVars["snap_try_core"] = ""
	s.bootloader.BootVars["snap_try_kernel"] = "k2"
	mylog.Check(boot.MarkBootSuccessful(coreDev))

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
	mylog.Check(boot.MarkBootSuccessful(coreDev))

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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


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
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})
	mylog.

		// do it again, verify its still valid
		Check(boot.MarkBootSuccessful(coreDev))

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

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)

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
			"asset": []string{dataHash},
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
	mylog.Check(boot.WriteBootChains(boot.ToPredictableBootChains(bootChains), filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0))


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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


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
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the bootloader variables
	expected := map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern2.Filename(),
		"snap_try_kernel": "",
	}
	c.Assert(s.bootloader.BootVars, DeepEquals, expected)

	// check that the new kernel is the only one in modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})
	mylog.

		// do it again, verify its still valid
		Check(boot.MarkBootSuccessful(coreDev))

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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.Base, Equals, s.base2.Filename())
	c.Assert(m2.TryBase, Equals, "")
	c.Assert(m2.BaseStatus, Equals, "")
	mylog.

		// do it again, verify its still valid
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv again
	m3 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m3.Base, Equals, s.base2.Filename())
	c.Assert(m3.TryBase, Equals, "")
	c.Assert(m3.BaseStatus, Equals, "")
}

func (s *bootenv20Suite) bootloaderWithTrustedAssets(c *C, trustedAssets map[string]string) *bootloadertest.MockTrustedAssetsBootloader {
	// TODO:UC20: this should be an ExtractedRecoveryKernelImageBootloader
	// because that would reflect our main currently supported
	// trusted assets bootloader (grub)
	tab := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(tab)
	tab.TrustedAssetsMap = trustedAssets
	s.AddCleanup(func() { bootloader.Force(nil) })
	return tab
}

func (s *bootenv20Suite) TestMarkBootSuccessful20BootAssetsUpdateHappy(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
		"shim":  "shim",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"

	c.Assert(os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755), IsNil)
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755), IsNil)
	// only asset for ubuntu
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	// shim and asset for seed
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "shim"), shim, 0644), IsNil)

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
			"asset": []string{"assethash", dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"recoveryassethash", dataHash},
			"shim":  []string{"recoveryshimhash", shimHash},
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
						secboot.NewLoadChain(runKernelBf))),
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(recoveryKernelBf))),
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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

	// update assets are in the list
	c.Check(m2.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": []string{dataHash},
	})
	c.Check(m2.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": []string{dataHash},
		"shim":  []string{shimHash},
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

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"nested/asset": "asset",
		"shim":         "shim",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "nested"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "nested"), 0755), IsNil)
	// only asset for ubuntu-boot
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "nested/asset"), data, 0644), IsNil)
	// shim and asset for ubuntu-seed
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "nested/asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "shim"), shim, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"shim-" + shimHash,
		"asset-" + dataHash,
	})

	runKernelBf := bootloader.NewBootFile(filepath.Join(s.kern1.Filename()), "kernel.efi", bootloader.RoleRunMode)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "nested/asset", bootloader.RoleRecovery),
		runKernelBf,
	}
	tab.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "nested/asset", bootloader.RoleRecovery),
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
			"asset": []string{dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{dataHash},
			"shim":  []string{shimHash},
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
		KernelCmdlines: []string{
			"snapd_recovery_mode=factory-reset snapd_recovery_system=system",
			"snapd_recovery_mode=recover snapd_recovery_system=system",
		},
	}}

	recoveryBootChains := []boot.BootChain{bootChains[1]}
	mylog.Check(boot.WriteBootChains(boot.ToPredictableBootChains(bootChains), filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0))

	mylog.Check(boot.WriteBootChains(boot.ToPredictableBootChains(recoveryBootChains), filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"), 0))

	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// modeenv is unchanged
	m2 := mylog.Check2(boot.ReadModeenv(""))

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

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"nested/asset": "asset",
		"shim":         "shim",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "nested"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "nested"), 0755), IsNil)
	// only asset for ubuntu-boot
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "nested/asset"), data, 0644), IsNil)
	// shim and asset for ubuntu-seed
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "nested/asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "shim"), shim, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"shim-" + shimHash,
		"asset-" + dataHash,
	})

	runKernelBf := bootloader.NewBootFile(filepath.Join(s.ukern1.Filename()), "kernel.efi", bootloader.RoleRunMode)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)

	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "nested/asset", bootloader.RoleRecovery),
		runKernelBf,
	}
	tab.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "nested/asset", bootloader.RoleRecovery),
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
			"asset": []string{dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{dataHash},
			"shim":  []string{shimHash},
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
		KernelCmdlines: []string{
			"snapd_recovery_mode=factory-reset snapd_recovery_system=system",
			"snapd_recovery_mode=recover snapd_recovery_system=system",
		},
	}}

	recoveryBootChains := []boot.BootChain{bootChains[1]}
	mylog.Check(boot.WriteBootChains(boot.ToPredictableBootChains(bootChains), filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0))

	mylog.Check(boot.WriteBootChains(boot.ToPredictableBootChains(recoveryBootChains), filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"), 0))

	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// modeenv is unchanged
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"EFI/asset": "efi:asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "EFI"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "EFI/asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/asset"), data, 0644), IsNil)
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
			"asset": []string{"one", "two"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"one", "two"},
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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot mark boot successful: cannot mark successful boot assets: system booted with unexpected run mode bootloader asset "EFI/asset" hash %s`, dataHash))

	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
			"asset": []string{"one"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"one"},
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
	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})
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
	mylog.
		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

	// modeenv is unchaged
	c.Check(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run candidate panic=-1",
	})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedOld(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run panic=-1")
	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})
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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

	// modeenv is unchaged
	c.Check(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run panic=-1",
	})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedMismatch(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run different")
	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})
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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))
	c.Assert(err, ErrorMatches, `cannot mark boot successful: cannot mark successful boot command line: current command line content "snapd_recovery_mode=run different" not matching any expected entry`)
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedFallbackOnBootSuccessful(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run panic=-1")
	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})
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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

	// modeenv is unchaged
	c.Check(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run panic=-1",
	})
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineUpdatedFallbackOnBootMismatch(c *C) {
	s.mockCmdline(c, "snapd_recovery_mode=run panic=-1 unexpected")
	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})
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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))
	c.Assert(err, ErrorMatches, `cannot mark boot successful: cannot mark successful boot command line: unexpected current command line: "snapd_recovery_mode=run panic=-1 unexpected"`)
}

func (s *bootenv20Suite) TestMarkBootSuccessful20CommandLineNonRunMode(c *C) {
	// recover mode
	s.mockCmdline(c, "snapd_recovery_mode=recover snapd_recovery_system=1234 panic=-1")
	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})
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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.
		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.
		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(coreDev))


	// check the modeenv
	m2 := mylog.Check2(boot.ReadModeenv(""))

	// model's sign key ID has been set
	c.Check(m2.ModelSignKeyID, Equals, "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij")
	c.Check(m2.Model, Equals, "my-model-uc20")
	c.Check(m2.BrandID, Equals, "my-brand")
	c.Check(m2.Grade, Equals, "dangerous")
}

type recoveryBootenv20Suite struct {
	baseBootenvSuite

	bootloader *bootloadertest.MockBootloader

	dev snap.Device
}

var _ = Suite(&recoveryBootenv20Suite{})

func (s *recoveryBootenv20Suite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	s.forceBootloader(s.bootloader)

	s.dev = boottest.MockUC20Device("", nil)
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeHappy(c *C) {
	mylog.Check(boot.SetRecoveryBootSystemAndMode(s.dev, "1234", "install"))

	c.Check(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snapd_recovery_system": "1234",
		"snapd_recovery_mode":   "install",
	})
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeSetErr(c *C) {
	s.bootloader.SetErr = errors.New("no can do")
	mylog.Check(boot.SetRecoveryBootSystemAndMode(s.dev, "1234", "install"))
	c.Assert(err, ErrorMatches, `no can do`)
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeNonUC20(c *C) {
	non20Dev := boottest.MockDevice("some-snap")
	mylog.Check(boot.SetRecoveryBootSystemAndMode(non20Dev, "1234", "install"))
	c.Assert(err, Equals, boot.ErrUnsupportedSystemMode)
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeErrClumsy(c *C) {
	mylog.Check(boot.SetRecoveryBootSystemAndMode(s.dev, "", "install"))
	c.Assert(err, ErrorMatches, "internal error: system label is unset")
	mylog.Check(boot.SetRecoveryBootSystemAndMode(s.dev, "1234", ""))
	c.Assert(err, ErrorMatches, "internal error: system mode is unset")
}

func (s *recoveryBootenv20Suite) TestSetRecoveryBootSystemAndModeRealHappy(c *C) {
	bootloader.Force(nil)

	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mylog.Check(os.MkdirAll(mockSeedGrubDir, 0755))

	mylog.Check(os.WriteFile(filepath.Join(mockSeedGrubDir, "grub.cfg"), nil, 0644))

	mylog.Check(boot.SetRecoveryBootSystemAndMode(s.dev, "1234", "install"))


	bl := mylog.Check2(bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery}))


	blvars := mylog.Check2(bl.GetBootVars("snapd_recovery_mode", "snapd_recovery_system"))

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

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`

	s.mockCmdline(c, "snapd_recovery_mode=run this is mocked panic=-1")
	s.gadgetSnap = snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{{"meta/gadget.yaml", mockGadgetYaml}})
}

func (s *bootConfigSuite) mockCmdline(c *C, cmdline string) {
	c.Assert(os.WriteFile(s.cmdlineFile, []byte(cmdline), 0644), IsNil)
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

	updated := mylog.Check2(boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap, ""))

	c.Check(updated, Equals, true)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 0)
}

func (s *bootConfigSuite) testBootConfigUpdateHappyWithReseal(c *C, cmdlineAppend string) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	runKernelBf := bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_600.snap", "kernel.efi", bootloader.RoleRunMode)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-hash-1",
	})

	s.bootloader.TrustedAssetsMap = map[string]string{"asset": "asset"}
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

	newCmdline := strutil.JoinNonEmpty([]string{
		"snapd_recovery_mode=run mocked candidate panic=-1", cmdlineAppend,
	}, " ")
	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		c.Assert(params, NotNil)
		c.Assert(params.ModelParams, HasLen, 1)
		c.Check(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
			newCmdline,
			"snapd_recovery_mode=run this is mocked panic=-1",
		})
		return nil
	})
	defer restore()

	updated := mylog.Check2(boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap, cmdlineAppend))

	c.Check(updated, Equals, true)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 1)

	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run this is mocked panic=-1",
		newCmdline,
	})
}

func (s *bootConfigSuite) TestBootConfigUpdateHappyWithReseal(c *C) {
	s.testBootConfigUpdateHappyWithReseal(c, "")
}

func (s *bootConfigSuite) TestBootConfigUpdateHappyCmdlineAppendWithReseal(c *C) {
	s.testBootConfigUpdateHappyWithReseal(c, "foo bar")
}

func (s *bootConfigSuite) testBootConfigUpdateHappyNoChange(c *C, cmdlineAppend string) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	s.bootloader.StaticCommandLine = "mocked unchanged panic=-1"
	s.bootloader.CandidateStaticCommandLine = "mocked unchanged panic=-1"

	m := &boot.Modeenv{
		Mode: "run",
		CurrentKernelCommandLines: boot.BootCommandLines{
			strutil.JoinNonEmpty([]string{
				"snapd_recovery_mode=run mocked unchanged panic=-1", cmdlineAppend,
			}, " "),
		},
	}
	c.Assert(m.WriteTo(""), IsNil)

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	updated := mylog.Check2(boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap, cmdlineAppend))

	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 0)

	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernelCommandLines, HasLen, 1)
}

func (s *bootConfigSuite) TestBootConfigUpdateHappyNoChange(c *C) {
	s.testBootConfigUpdateHappyNoChange(c, "")
}

func (s *bootConfigSuite) TestBootConfigUpdateHappyCmdlineAppendNoChange(c *C) {
	s.testBootConfigUpdateHappyNoChange(c, "foo bar")
}

func (s *bootConfigSuite) TestBootConfigUpdateNonUC20DoesNothing(c *C) {
	nonUC20coreDev := boottest.MockDevice("pc-kernel")
	c.Assert(nonUC20coreDev.HasModeenv(), Equals, false)
	updated := mylog.Check2(boot.UpdateManagedBootConfigs(nonUC20coreDev, s.gadgetSnap, ""))

	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 0)
}

func (s *bootConfigSuite) TestBootConfigUpdateBadModeErr(c *C) {
	uc20Dev := boottest.MockUC20Device("recover", nil)
	c.Assert(uc20Dev.HasModeenv(), Equals, true)
	updated := mylog.Check2(boot.UpdateManagedBootConfigs(uc20Dev, s.gadgetSnap, ""))
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

	updated := mylog.Check2(boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap, ""))
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

	updated := mylog.Check2(boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap, ""))
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

	updated := mylog.Check2(boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap, ""))

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

	updated := mylog.Check2(boot.UpdateManagedBootConfigs(coreDev, s.gadgetSnap, ""))
	c.Assert(err, ErrorMatches, "internal error: cannot find trusted assets bootloader under .*: mocked find error")
	c.Check(updated, Equals, false)
	c.Check(s.bootloader.UpdateCalls, Equals, 0)
}

func (s *bootConfigSuite) TestBootConfigUpdateWithGadgetAndReseal(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	gadgetSnap := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "foo bar baz"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	runKernelBf := bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_600.snap", "kernel.efi", bootloader.RoleRunMode)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-hash-1",
	})

	s.bootloader.TrustedAssetsMap = map[string]string{"asset": "asset"}
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

	updated := mylog.Check2(boot.UpdateManagedBootConfigs(coreDev, gadgetSnap, ""))

	c.Check(updated, Equals, true)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 1)

	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run this is mocked panic=-1 foo bar baz",
		"snapd_recovery_mode=run mocked candidate panic=-1 foo bar baz",
	})
}

func (s *bootConfigSuite) TestBootConfigUpdateWithGadgetFullAndReseal(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	gadgetSnap := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.full", "foo bar baz"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// a minimal bootloader and modeenv setup that works because reseal is
	// not executed
	s.bootloader.TrustedAssetsMap = map[string]string{"asset": "asset"}
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

	updated := mylog.Check2(boot.UpdateManagedBootConfigs(coreDev, gadgetSnap, ""))

	c.Check(updated, Equals, true)
	c.Check(s.bootloader.UpdateCalls, Equals, 1)
	c.Check(resealCalls, Equals, 0)

	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run foo bar baz",
	})
}

type bootKernelCommandLineSuite struct {
	baseBootenvSuite

	bootloader            *bootloadertest.MockTrustedAssetsBootloader
	gadgetSnap            string
	uc20dev               snap.Device
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
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

	s.bootloader = bootloadertest.Mock("trusted", c.MkDir()).WithTrustedAssets()
	s.bootloader.TrustedAssetsMap = map[string]string{"asset": "asset"}
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

	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(nonUC20dev, sf, ""))
	c.Assert(err, ErrorMatches, `internal error: command line component cannot be updated on pre-UC20 devices`)
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

	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, ""))

	c.Assert(reboot, Equals, false)
	c.Check(bl.SetBootVarsCalls, Equals, 0)
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20ArgsAdded(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "args from gadget"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, ""))

	c.Assert(reboot, Equals, true)

	// reseal happened
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 args from gadget",
	}})

	// modeenv has been updated
	newM := mylog.Check2(boot.ReadModeenv(""))

	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 args from gadget",
	})

	// bootloader variables too
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	args := mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "static mocked panic=-1 args from gadget",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20ArgsSwitch(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "no change"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1 no change"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)
	mylog.Check(s.bootloader.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "no change",
		// this is intentionally filled and will be cleared
		"snapd_full_cmdline_args": "canary",
	}))

	s.bootloader.SetBootVarsCalls = 0

	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, ""))

	c.Assert(reboot, Equals, false)

	// no reseal needed
	c.Check(s.resealCalls, Equals, 0)

	newM := mylog.Check2(boot.ReadModeenv(""))

	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 no change",
	})
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	args := mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "no change",
		// canary is still present, as nothing was modified
		"snapd_full_cmdline_args": "canary",
	})

	// let's change them now
	sfChanged := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "changed"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	reboot = mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sfChanged, ""))

	c.Assert(reboot, Equals, true)

	// reseal was applied
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run static mocked panic=-1 changed",
		"snapd_recovery_mode=run static mocked panic=-1 no change",
	}})

	// modeenv has been updated
	newM = mylog.Check2(boot.ReadModeenv(""))

	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 no change",
		// new ones are appended
		"snapd_recovery_mode=run static mocked panic=-1 changed",
	})
	// and bootloader env too
	args = mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "static mocked panic=-1 changed",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20UnencryptedArgsRemoved(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	// pretend we used to have additional arguments from the gadget, but
	// those will be gone with new update
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1 from-gadget"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)
	mylog.Check(s.bootloader.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "from-gadget",
		// this is intentionally filled and will be cleared
		"snapd_full_cmdline_args": "canary",
	}))

	s.bootloader.SetBootVarsCalls = 0

	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, ""))

	c.Assert(reboot, Equals, true)

	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 from-gadget",
	}})

	newM := mylog.Check2(boot.ReadModeenv(""))

	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 from-gadget",
		"snapd_recovery_mode=run static mocked panic=-1",
	})
	// bootloader variables were explicitly cleared
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	args := mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "static mocked panic=-1",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20SetError(c *C) {
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	// pretend we used to have additional arguments from the gadget, but
	// those will be gone with new update
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "this-is-not-applied"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	s.bootloader.SetErr = fmt.Errorf("set fails")

	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, ""))
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

	newM := mylog.Check2(boot.ReadModeenv(""))

	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		// this will be cleared on next reboot or will get overwritten
		// by an update
		"snapd_recovery_mode=run static mocked panic=-1 this-is-not-applied",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateWithResealError(c *C) {
	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	gadgetSnap := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "args from gadget"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	s.stampSealedKeys(c, dirs.GlobalRootDir)
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return fmt.Errorf("reseal fails")
	})
	defer restore()

	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, gadgetSnap, ""))
	c.Assert(err, ErrorMatches, "cannot reseal the encryption key: reseal fails")
	c.Check(reboot, Equals, false)
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	c.Check(resealCalls, Equals, 1)

	m2 := mylog.Check2(boot.ReadModeenv(""))

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
	mylog.Check(s.bootloader.SetBootVars(map[string]string{
		// those are intentionally filled by the test
		"snapd_extra_cmdline_args": "canary",
		"snapd_full_cmdline_args":  "canary",
	}))

	s.bootloader.SetBootVarsCalls = 0

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	// transition to gadget with cmdline.extra
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "extra args"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})
	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, ""))

	c.Assert(reboot, Equals, true)
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	}})
	s.resealCommandLines = nil

	newM := mylog.Check2(boot.ReadModeenv(""))

	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	args := mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "static mocked panic=-1 extra args",
	})
	// this normally happens after booting
	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1 extra args"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	// transition to full override from gadget
	sfFull := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.full", "full args"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})
	reboot = mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sfFull, ""))

	c.Assert(reboot, Equals, true)
	c.Check(s.resealCalls, Equals, 2)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run full args",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	}})
	s.resealCommandLines = nil
	// modeenv has been updated
	newM = mylog.Check2(boot.ReadModeenv(""))

	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
		// new ones are appended
		"snapd_recovery_mode=run full args",
	})
	// and bootloader env too
	args = mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

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
	sfNone := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", mockGadgetYaml},
	})
	reboot = mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sfNone, ""))


	c.Assert(reboot, Equals, true)
	c.Check(s.resealCalls, Equals, 3)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run full args",
		"snapd_recovery_mode=run static mocked panic=-1",
	}})
	// modeenv has been updated again
	newM = mylog.Check2(boot.ReadModeenv(""))

	c.Check(newM.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run full args",
		// new ones are appended
		"snapd_recovery_mode=run static mocked panic=-1",
	})
	// and bootloader env too
	args = mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "static mocked panic=-1",
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
	mylog.Check(os.WriteFile(cmdlineFile, []byte("snapd_recovery_mode=run static mocked panic=-1"), 0644))

	restore = kcmdline.MockProcCmdline(cmdlineFile)
	s.AddCleanup(restore)
	mylog.Check(s.bootloader.SetBootVars(map[string]string{
		// those are intentionally filled by the test
		"snapd_extra_cmdline_args": "canary",
		"snapd_full_cmdline_args":  "canary",
	}))

	s.bootloader.SetBootVarsCalls = 0

	restoreBootloaderNoPanic := s.bootloader.SetMockToPanic("SetBootVars")
	defer restoreBootloaderNoPanic()

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	// transition to gadget with cmdline.extra
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "extra args"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	// let's panic on reseal first
	resealPanic = true
	c.Assert(func() {
		boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, "")
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
	m := mylog.Check2(boot.ReadModeenv(""))

	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})

	// REBOOT
	resealPanic = false
	mylog.Check(boot.MarkBootSuccessful(s.uc20dev))

	// we resealed after reboot, since modeenv was updated and carries the
	// current command line only
	c.Check(s.resealCalls, Equals, 2)
	m = mylog.Check2(boot.ReadModeenv(""))

	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
	})

	// try the update again, but no panic in reseal this time
	s.resealCalls = 0
	s.resealCommandLines = nil
	resealPanic = false
	// but panic in set
	c.Assert(func() {
		boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, "")
	}, PanicMatches, "mocked reboot panic in SetBootVars")
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	}})
	// the call to bootloader wasn't counted, because it called panic
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	m = mylog.Check2(boot.ReadModeenv(""))

	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})
	mylog.

		// REBOOT
		Check(boot.MarkBootSuccessful(s.uc20dev))

	// we resealed after reboot again
	c.Check(s.resealCalls, Equals, 2)
	m = mylog.Check2(boot.ReadModeenv(""))

	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
	})

	// try again, for the last time, things should go smoothly
	s.resealCalls = 0
	s.resealCommandLines = nil
	restoreBootloaderNoPanic()
	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, ""))

	c.Check(reboot, Equals, true)
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	// all done, modeenv
	m = mylog.Check2(boot.ReadModeenv(""))

	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})
	args := mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "static mocked panic=-1 extra args",
	})
}

func (s *bootKernelCommandLineSuite) TestCommandLineUpdateUC20OverSpuriousRebootsAfterBootVars(c *C) {
	// simulate spurious reboots
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	// no command line arguments from gadget
	s.modeenvWithEncryption.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run static mocked panic=-1"}
	c.Assert(s.modeenvWithEncryption.WriteTo(""), IsNil)

	cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
	restore := kcmdline.MockProcCmdline(cmdlineFile)
	s.AddCleanup(restore)
	mylog.Check(s.bootloader.SetBootVars(map[string]string{
		// those are intentionally filled by the test
		"snapd_extra_cmdline_args": "canary",
		"snapd_full_cmdline_args":  "canary",
	}))

	s.bootloader.SetBootVarsCalls = 0

	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`
	// transition to gadget with cmdline.extra
	sf := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"cmdline.extra", "extra args"},
		{"meta/gadget.yaml", mockGadgetYaml},
	})

	// let's panic after setting bootenv, but before returning, such that if
	// executed by a task handler, the task's status would not get updated
	s.bootloader.SetErrFunc = func() error {
		panic("mocked reboot panic after SetBootVars")
	}
	c.Assert(func() {
		boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, "")
	}, PanicMatches, "mocked reboot panic after SetBootVars")
	c.Check(s.resealCalls, Equals, 1)
	c.Check(s.resealCommandLines, DeepEquals, [][]string{{
		// those come from boot chains which use predictable sorting
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	}})
	// the call to bootloader was executed
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	m := mylog.Check2(boot.ReadModeenv(""))

	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1",
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})
	args := mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "static mocked panic=-1 extra args",
	})

	// REBOOT; since we rebooted after updating the bootenv, the kernel
	// command line will include arguments that came from gadget snap
	s.bootloader.SetBootVarsCalls = 0
	s.resealCalls = 0
	mylog.Check(os.WriteFile(cmdlineFile, []byte("snapd_recovery_mode=run static mocked panic=-1 extra args"), 0644))

	mylog.Check(boot.MarkBootSuccessful(s.uc20dev))

	// we resealed after reboot again
	c.Check(s.resealCalls, Equals, 1)
	// bootenv wasn't touched
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	m = mylog.Check2(boot.ReadModeenv(""))

	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})

	// try again, as if the task handler gets to run again
	s.resealCalls = 0
	reboot := mylog.Check2(boot.UpdateCommandLineForGadgetComponent(s.uc20dev, sf, ""))

	// nothing changed now, we already booted with the new command line
	c.Check(reboot, Equals, false)
	// not reseal since nothing changed
	c.Check(s.resealCalls, Equals, 0)
	// no changes to the bootenv either
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	// all done, modeenv
	m = mylog.Check2(boot.ReadModeenv(""))

	c.Check(m.CurrentKernelCommandLines, DeepEquals, boot.BootCommandLines{
		"snapd_recovery_mode=run static mocked panic=-1 extra args",
	})
	args = mylog.Check2(s.bootloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args"))

	c.Check(args, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "static mocked panic=-1 extra args",
	})
}

func (s *bootenv20RebootBootloaderSuite) TestCoreParticipant20WithRebootBootloader(c *C) {
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
	rebootInfo := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: false}))

	c.Assert(rebootInfo.RebootRequired, Equals, true)
	// Test that we get the bootloader options
	c.Assert(rebootInfo.BootloaderOptions, DeepEquals,
		&bootloader.Options{
			Role: bootloader.RoleRunMode,
		})

	// make sure the env was updated
	m := s.bootloader.BootVars
	c.Assert(m, DeepEquals, map[string]string{
		"kernel_status":   boot.TryStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": s.kern2.Filename(),
	})

	// and that the modeenv now has this kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename(), s.kern2.Filename()})
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextSameGadgetSnap(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()
	r = boot.MockResealKeyToModeenv(func(_ string, _ *boot.Modeenv, expectReseal bool, _ boot.Unlocker) error {
		c.Assert(expectReseal, Equals, false)
		return nil
	})
	defer r()

	// get the gadget participant
	bootGadget := boot.Participant(s.gadget1, snap.TypeGadget, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootGadget.IsTrivial(), Equals, false)

	// make the gadget used on next boot
	rebootRequired := mylog.Check2(bootGadget.SetNextBoot(boot.NextBootContext{}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// the modeenv is still the same
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.Gadget, Equals, s.gadget1.Filename())

	// we didn't call SetBootVars on the bootloader (unneeded for gadget)
	c.Assert(s.bootloader.SetBootVarsCalls, Equals, 0)
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextNewGadgetSnap(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()
	r = boot.MockResealKeyToModeenv(func(_ string, _ *boot.Modeenv, expectReseal bool, _ boot.Unlocker) error {
		c.Assert(expectReseal, Equals, false)
		return nil
	})
	defer r()

	// get the gadget participant
	bootGadget := boot.Participant(s.gadget2, snap.TypeGadget, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootGadget.IsTrivial(), Equals, false)

	// make the gadget used on next boot
	rebootRequired := mylog.Check2(bootGadget.SetNextBoot(boot.NextBootContext{}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// and that the modeenv now contains gadget2
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.Gadget, Equals, s.gadget2.Filename())

	// we didn't call SetBootVars on the bootloader (unneeded for gadget)
	c.Assert(s.bootloader.SetBootVarsCalls, Equals, 0)
}

func (s *bootenv20Suite) TestCoreParticipant20UndoKernelSnapInstallSame(c *C) {
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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// make sure that the bootloader was asked for the current kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)

	// ensure that kernel_status is still empty
	c.Assert(s.bootloader.BootVars["kernel_status"], Equals, boot.DefaultStatus)

	// there was no attempt to try a kernel
	_, enableKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(enableKernelCalls, Equals, 0)

	// the modeenv is still the same as well
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename()})

	// finally we didn't call SetBootVars on the bootloader because nothing
	// changed
	c.Assert(s.bootloader.SetBootVarsCalls, Equals, 0)
}

func (s *bootenv20EnvRefKernelSuite) TestCoreParticipant20UndoKernelSnapInstallSame(c *C) {
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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// ensure that bootenv is unchanged
	m := mylog.Check2(s.bootloader.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(m, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": "",
	})

	// the modeenv is still the same as well
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename()})

	// finally we didn't call SetBootVars on the bootloader because nothing
	// changed
	c.Assert(s.bootloader.SetBootVarsCalls, Equals, 0)
}

func (s *bootenv20Suite) TestCoreParticipant20UndoKernelSnapInstallNew(c *C) {
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

	// make the kernel used on next boot, reverting the installation
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, DeepEquals, boot.RebootInfo{
		RebootRequired: true,
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	})

	// make sure that the bootloader was asked for the current kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)

	// ensure that kernel_status is the default
	c.Assert(s.bootloader.BootVars["kernel_status"], Equals, boot.DefaultStatus)

	// and we were asked to enable kernel2 as kernel, not as try kernel
	_, numTry := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(numTry, Equals, 0)
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{s.kern2})

	// and that the modeenv now has only this kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})
}

func (s *bootenv20EnvRefKernelSuite) TestCoreParticipant20UndoKernelSnapInstallNew(c *C) {
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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, DeepEquals, boot.RebootInfo{
		RebootRequired: true,
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	})

	// make sure the env was updated
	m := s.bootloader.BootVars
	c.Assert(m, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern2.Filename(),
		"snap_try_kernel": "",
	})

	// and that the modeenv now has this kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})
}

func (s *bootenv20Suite) TestCoreParticipant20UndoKernelSnapInstallNewWithReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-" + dataHash,
	})

	assetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir,
		"trusted", fmt.Sprintf("asset-%s", dataHash)), bootloader.RoleRunMode)
	runKernelBf := bootloader.NewBootFile(filepath.Join(s.kern2.Filename()),
		"kernel.efi", bootloader.RoleRunMode)

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
			"asset": []string{dataHash},
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
		})
		// actual paths are seen only here
		c.Check(tab.BootChainKernelPath, DeepEquals, []string{
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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, DeepEquals, boot.RebootInfo{
		RebootRequired: true,
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	})

	// make sure the env was updated
	bvars := mylog.Check2(tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern2.Filename(),
		"snap_try_kernel": "",
	})

	// and that the modeenv now has this kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern2.Filename()})

	c.Check(resealCalls, Equals, 1)
}

func (s *bootenv20Suite) TestCoreParticipant20UndoUnassertedKernelSnapInstallNewWithReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

	// mock the files in cache
	mockAssetsCache(c, dirs.GlobalRootDir, "trusted", []string{
		"asset-" + dataHash,
	})

	assetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)), bootloader.RoleRunMode)
	runKernelBf := bootloader.NewBootFile(filepath.Join(s.ukern2.Filename()), "kernel.efi", bootloader.RoleRunMode)

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
			"asset": []string{dataHash},
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
		})
		// actual paths are seen only here
		c.Check(tab.BootChainKernelPath, DeepEquals, []string{
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
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, DeepEquals, boot.RebootInfo{
		RebootRequired: true,
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	})

	bvars := mylog.Check2(tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.ukern2.Filename(),
		"snap_try_kernel": "",
	})

	// and that the modeenv now has this kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.ukern2.Filename()})

	c.Check(resealCalls, Equals, 1)
}

func (s *bootenv20Suite) TestCoreParticipant20UndoKernelSnapInstallSameNoReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

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
			"asset": []string{dataHash},
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
		return fmt.Errorf("unexpected call to mocked secbootResealKeys")
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
	mylog.Check(boot.WriteBootChains(bootChains, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0))


	// make the kernel used on next boot
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// make sure the env is as expected
	bvars := mylog.Check2(tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.kern1.Filename(),
		"snap_try_kernel": "",
	})

	// and that the modeenv now has the one kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename()})

	// boot chains were built
	c.Check(tab.BootChainKernelPath, DeepEquals, []string{
		s.kern1.MountFile(),
	})
	// no actual reseal
	c.Check(resealCalls, Equals, 0)
}

func (s *bootenv20Suite) TestCoreParticipant20UndoUnassertedKernelSnapInstallSameNoReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

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
			"asset": []string{dataHash},
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
	mylog.Check(boot.WriteBootChains(bootChains, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0))


	// make the kernel used on next boot
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// make sure the env is as expected
	bvars := mylog.Check2(tab.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	c.Assert(bvars, DeepEquals, map[string]string{
		"kernel_status":   boot.DefaultStatus,
		"snap_kernel":     s.ukern1.Filename(),
		"snap_try_kernel": "",
	})

	// and that the modeenv now has the one kernel listed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.ukern1.Filename()})

	// boot chains were built
	c.Check(tab.BootChainKernelPath, DeepEquals, []string{
		s.ukern1.MountFile(),
	})
	// no actual reseal
	c.Check(resealCalls, Equals, 0)
}

func (s *bootenv20Suite) TestCoreParticipant20UndoBaseSnapInstallSame(c *C) {
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
	rebootRequired := mylog.Check2(bootBase.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))


	// we don't need to reboot because it's the same base snap
	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// make sure the modeenv wasn't changed
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.Base, Equals, m.Base)
	c.Assert(m2.BaseStatus, Equals, m.BaseStatus)
	c.Assert(m2.TryBase, Equals, m.TryBase)
}

func (s *bootenv20Suite) TestCoreParticipant20UndoBaseSnapInstallNew(c *C) {
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

	// make the base used on next boot, reverting the current one installed
	rebootRequired := mylog.Check2(bootBase.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: true})

	// make sure the modeenv was updated
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.Base, Equals, s.base2.Filename())
	c.Assert(m2.BaseStatus, Equals, "")
	c.Assert(m2.TryBase, Equals, "")
}

func (s *bootenv20Suite) TestCoreParticipant20UndoBaseSnapInstallNewNoReseal(c *C) {
	// checked by resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)

	// set up all the bits required for an encrypted system
	tab := s.bootloaderWithTrustedAssets(c, map[string]string{
		"asset": "asset",
	})
	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
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
	mylog.Check(boot.WriteBootChains(boot.ToPredictableBootChains(bootChains), filepath.Join(dirs.SnapFDEDir, "boot-chains"), 0))


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
			"asset": []string{dataHash},
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
	rebootRequired := mylog.Check2(bootBase.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: true})

	// make sure the modeenv was updated
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.Base, Equals, s.base2.Filename())
	c.Assert(m2.BaseStatus, Equals, boot.DefaultStatus)
	c.Assert(m2.TryBase, Equals, "")

	// no reseal
	c.Check(resealCalls, Equals, 0)
}

func (s *bootenv20Suite) TestInUseClassicWithModes(c *C) {
	classicWithModesDev := boottest.MockClassicWithModesDevice("", nil)
	c.Assert(classicWithModesDev.IsCoreBoot(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		&bootenv20Setup{
			modeenv: &boot.Modeenv{
				// gadget is gadget1
				Gadget: s.gadget1.Filename(),
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
		})
	defer r()

	inUse := mylog.Check2(boot.InUse(snap.TypeKernel, classicWithModesDev))
	c.Check(err, IsNil)
	c.Check(inUse(s.kern1.SnapName(), s.kern1.SnapRevision()), Equals, true)
	c.Check(inUse(s.kern2.SnapName(), s.kern2.SnapRevision()), Equals, false)

	_ = mylog.Check2(boot.InUse(snap.TypeBase, classicWithModesDev))
	c.Check(err, IsNil)
}

func (s *bootenv20Suite) TestCoreParticipant20SetNextCurrentKernelSnap(c *C) {
	coreDev := boottest.MockUC20Device("", nil)
	c.Assert(coreDev.HasModeenv(), Equals, true)

	r := setupUC20Bootenv(
		c,
		s.bootloader,
		s.normalDefaultState,
	)
	defer r()

	// get the boot kernel participant from our current kernel snap
	bootKern := boot.Participant(s.kern1, snap.TypeKernel, coreDev)
	// make sure it's not a trivial boot participant
	c.Assert(bootKern.IsTrivial(), Equals, false)

	// Make it the kernel used on next boot. This sort of situation (same
	// current and next kernel) can happen when an installation of a new
	// kernel is aborted before we reboot: in that case we need to clean up
	// some things although the current kernel did not really change.
	rebootRequired := mylog.Check2(bootKern.SetNextBoot(boot.NextBootContext{BootWithoutTry: true}))

	c.Assert(rebootRequired, Equals, boot.RebootInfo{RebootRequired: false})

	// make sure that the bootloader was asked for the current kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)

	// ensure that kernel_status is not set
	c.Assert(s.bootloader.BootVars["kernel_status"], Equals, "")

	// we were not asked to enable a try kernel
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(len(actual), Equals, 0)

	// and we were asked to disable the try kernel
	_, nDisableTryCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(nDisableTryCalls, Equals, 1)

	// and that the modeenv has this kernel listed only once
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Assert(m2.CurrentKernels, DeepEquals, []string{s.kern1.Filename()})
}

func (s *bootenv20Suite) TestMarkBootSuccessfulClassModes(c *C) {
	// MarkBootSuccessful on classic+modes will not have a "base"
	// in the modeenv
	m := &boot.Modeenv{
		Mode:           "run",
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

	classicWithModesDev := boottest.MockClassicWithModesDevice("", nil)
	c.Assert(classicWithModesDev.HasModeenv(), Equals, true)
	mylog.

		// mark successful
		Check(boot.MarkBootSuccessful(classicWithModesDev))


	// no error, modeenv is unchanged
	m2 := mylog.Check2(boot.ReadModeenv(""))

	c.Check(m2.Base, Equals, "")
	c.Check(m2.TryBase, Equals, "")
}
