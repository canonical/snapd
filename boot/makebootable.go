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

package boot

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/strutil"
)

var sealKeyToModeenv = sealKeyToModeenvImpl

// BootableSet represents the boot snaps of a system to be made bootable.
type BootableSet struct {
	Base       *snap.Info
	BasePath   string
	Kernel     *snap.Info
	KernelPath string
	Gadget     *snap.Info
	GadgetPath string

	RecoverySystemLabel string
	// RecoverySystemDir is a path to a directory with recovery system
	// assets. The path is relative to the recovery bootloader root
	// directory.
	RecoverySystemDir string

	UnpackedGadgetDir string

	// Recovery is set when making the recovery partition bootable.
	Recovery bool
}

// MakeBootableImage sets up the given bootable set and target filesystem
// such that the image can be booted.
//
// rootdir points to an image filesystem (UC 16/18) or an image recovery
// filesystem (UC20 at prepare-image time).
// On UC20, bootWith.Recovery must be true, as this function makes the recovery
// system bootable. It does not make a run system bootable, for that
// functionality see MakeRunnableSystem, which is meant to be used at runtime
// from UC20 install mode.
// For a UC20 image a set of boot flags that will be set in the recovery
// boot environment can be specified.
func MakeBootableImage(model *asserts.Model, rootdir string, bootWith *BootableSet, bootFlags []string) error {
	if model.Grade() == asserts.ModelGradeUnset {
		if len(bootFlags) != 0 {
			return fmt.Errorf("no boot flags support for UC16/18")
		}
		return makeBootable16(model, rootdir, bootWith)
	}

	if !bootWith.Recovery {
		return fmt.Errorf("internal error: MakeBootableImage called at runtime, use MakeRunnableSystem instead")
	}
	return makeBootable20(model, rootdir, bootWith, bootFlags)
}

// MakeBootablePartition configures a partition mounted on rootdir
// using information from bootWith and bootFlags. Contrarily to
// MakeBootableImage this happens in a live system.
func MakeBootablePartition(partDir string, opts *bootloader.Options, bootWith *BootableSet, bootMode string, bootFlags []string) error {
	if bootWith.RecoverySystemDir != "" {
		return fmt.Errorf("internal error: RecoverySystemDir unexpectedly set for MakeBootablePartition")
	}
	return configureBootloader(partDir, opts, bootWith, bootMode, bootFlags)
}

// makeBootable16 setups the image filesystem for boot with UC16
// and UC18 models. This entails:
//   - installing the bootloader configuration from the gadget
//   - creating symlinks for boot snaps from seed to the runtime blob dir
//   - setting boot env vars pointing to the revisions of the boot snaps to use
//   - extracting kernel assets as needed by the bootloader
func makeBootable16(model *asserts.Model, rootdir string, bootWith *BootableSet) error {
	opts := &bootloader.Options{
		PrepareImageTime: true,
	}
	mylog.Check(

		// install the bootloader configuration from the gadget
		bootloader.InstallBootConfig(bootWith.UnpackedGadgetDir, rootdir, opts))

	// setup symlinks for kernel and boot base from the blob directory
	// to the seed snaps

	snapBlobDir := dirs.SnapBlobDirUnder(rootdir)
	mylog.Check(os.MkdirAll(snapBlobDir, 0755))

	for _, fn := range []string{bootWith.BasePath, bootWith.KernelPath} {
		dst := filepath.Join(snapBlobDir, filepath.Base(fn))
		// construct a relative symlink from the blob dir
		// to the seed snap file
		relSymlink := mylog.Check2(filepath.Rel(snapBlobDir, fn))
		mylog.Check(os.Symlink(relSymlink, dst))

	}

	// Set bootvars for kernel/core snaps so the system boots and
	// does the first-time initialization. There is also no
	// mounted kernel/core/base snap, but just the blobs.
	bl := mylog.Check2(bootloader.Find(rootdir, opts))

	m := map[string]string{
		"snap_mode":       "",
		"snap_try_core":   "",
		"snap_try_kernel": "",
	}
	if model.DisplayName() != "" {
		m["snap_menuentry"] = model.DisplayName()
	}

	setBoot := func(name, fn string) {
		m[name] = filepath.Base(fn)
	}
	// base
	setBoot("snap_core", bootWith.BasePath)

	// kernel
	kernelf := mylog.Check2(snapfile.Open(bootWith.KernelPath))
	mylog.Check(bl.ExtractKernelAssets(bootWith.Kernel, kernelf))

	setBoot("snap_kernel", bootWith.KernelPath)
	mylog.Check(bl.SetBootVars(m))

	return nil
}

func configureBootloader(rootdir string, opts *bootloader.Options, bootWith *BootableSet, bootMode string, bootFlags []string) error {
	blVars := make(map[string]string, 3)
	if len(bootFlags) != 0 {
		mylog.Check(setImageBootFlags(bootFlags, blVars))
	}
	mylog.Check(

		// install the bootloader configuration from the gadget
		bootloader.InstallBootConfig(bootWith.UnpackedGadgetDir, rootdir, opts))

	// now install the recovery system specific boot config
	bl := mylog.Check2(bootloader.Find(rootdir, opts))

	blVars["snapd_recovery_mode"] = bootMode
	if bootWith.RecoverySystemLabel != "" {
		// record which recovery system is to be used on the bootloader, note
		// that this goes on the main bootloader environment, and not on the
		// recovery system bootloader environment, for example for grub
		// bootloader, this env var is set on the ubuntu-seed root grubenv, and
		// not on the recovery system grubenv in the systems/20200314/ subdir on
		// ubuntu-seed
		blVars["snapd_recovery_system"] = bootWith.RecoverySystemLabel
	}
	mylog.Check(bl.SetBootVars(blVars))

	return nil
}

func makeBootable20(model *asserts.Model, rootdir string, bootWith *BootableSet, bootFlags []string) error {
	// we can only make a single recovery system bootable right now
	recoverySystems := mylog.Check2(filepath.Glob(filepath.Join(rootdir, "systems/*")))

	if len(recoverySystems) > 1 {
		return fmt.Errorf("cannot make multiple recovery systems bootable yet")
	}

	if bootWith.RecoverySystemLabel == "" {
		return fmt.Errorf("internal error: recovery system label unset")
	}

	opts := &bootloader.Options{
		PrepareImageTime: true,
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	mylog.Check(configureBootloader(rootdir, opts, bootWith, ModeInstall, bootFlags))

	return MakeRecoverySystemBootable(model, rootdir, bootWith.RecoverySystemDir, &RecoverySystemBootableSet{
		Kernel:           bootWith.Kernel,
		KernelPath:       bootWith.KernelPath,
		GadgetSnapOrDir:  bootWith.UnpackedGadgetDir,
		PrepareImageTime: true,
	})
}

// RecoverySystemBootableSet is a set of snaps relevant to booting a recovery
// system.
type RecoverySystemBootableSet struct {
	Kernel          *snap.Info
	KernelPath      string
	GadgetSnapOrDir string
	// PrepareImageTime is true when the structure is being used when
	// preparing a bootable system image.
	PrepareImageTime bool
}

// MakeRecoverySystemBootable prepares a recovery system under a path relative
// to recovery bootloader's rootdir for booting.
func MakeRecoverySystemBootable(model *asserts.Model, rootdir string, relativeRecoverySystemDir string, bootWith *RecoverySystemBootableSet) error {
	opts := &bootloader.Options{
		// XXX: this is only needed by LK, it is unclear whether LK does
		// too much when extracting recovery kernel assets, in the end
		// it is currently not possible to create a recovery system at
		// runtime when using LK.
		PrepareImageTime: bootWith.PrepareImageTime,
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}

	bl := mylog.Check2(bootloader.Find(rootdir, opts))

	// on e.g. ARM we need to extract the kernel assets on the recovery
	// system as well, but the bootloader does not load any environment from
	// the recovery system
	erkbl, ok := bl.(bootloader.ExtractedRecoveryKernelImageBootloader)
	if ok {
		kernelf := mylog.Check2(snapfile.Open(bootWith.KernelPath))
		mylog.Check(erkbl.ExtractRecoveryKernelAssets(
			relativeRecoverySystemDir,
			bootWith.Kernel,
			kernelf,
		))

		return nil
	}

	rbl, ok := bl.(bootloader.RecoveryAwareBootloader)
	if !ok {
		return fmt.Errorf("cannot use %s bootloader: does not support recovery systems", bl.Name())
	}
	kernelPath := mylog.Check2(filepath.Rel(rootdir, bootWith.KernelPath))

	recoveryBlVars := map[string]string{
		"snapd_recovery_kernel": filepath.Join("/", kernelPath),
	}
	if tbl, ok := bl.(bootloader.TrustedAssetsBootloader); ok {
		// Look at gadget default values for system.kernel.*cmdline-append options
		cmdlineAppend := mylog.Check2(buildOptionalKernelCommandLine(model, bootWith.GadgetSnapOrDir))

		candidate := false
		defaultCmdLine := mylog.Check2(tbl.DefaultCommandLine(candidate))

		// to set cmdlineAppend.
		recoveryCmdlineArgs := mylog.Check2(bootVarsForTrustedCommandLineFromGadget(bootWith.GadgetSnapOrDir, cmdlineAppend, defaultCmdLine, model))

		for k, v := range recoveryCmdlineArgs {
			recoveryBlVars[k] = v
		}
	}
	mylog.Check(rbl.SetRecoverySystemEnv(relativeRecoverySystemDir, recoveryBlVars))

	return nil
}

type makeRunnableOptions struct {
	Standalone     bool
	AfterDataReset bool
	SeedDir        string
	StateUnlocker  Unlocker
}

func copyBootSnap(orig string, dstInfo *snap.Info, dstSnapBlobDir string) error {
	// if the source path is a symlink, don't copy the symlink, copy the
	// target file instead of copying the symlink, as the initramfs won't
	// follow the symlink when it goes to mount the base and kernel snaps by
	// design as the initramfs should only be using trusted things from
	// ubuntu-data to boot in run mode
	if osutil.IsSymlink(orig) {
		link := mylog.Check2(os.Readlink(orig))

		orig = link
	}
	// note that we need to use the "Filename()" here because unasserted
	// snaps will have names like pc-kernel_5.19.4.snap but snapd expects
	// "pc-kernel_x1.snap"
	dst := filepath.Join(dstSnapBlobDir, dstInfo.Filename())
	mylog.Check(osutil.CopyFile(orig, dst, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync))

	return nil
}

func makeRunnableSystem(model *asserts.Model, bootWith *BootableSet, sealer *TrustedAssetsInstallObserver, makeOpts makeRunnableOptions) error {
	if model.Grade() == asserts.ModelGradeUnset {
		return fmt.Errorf("internal error: cannot make pre-UC20 system runnable")
	}
	if bootWith.RecoverySystemDir != "" {
		return fmt.Errorf("internal error: RecoverySystemDir unexpectedly set for MakeRunnableSystem")
	}
	modeenvLock()
	defer modeenvUnlock()

	// TODO:UC20:
	// - figure out what to do for uboot gadgets, currently we require them to
	//   install the boot.sel onto ubuntu-boot directly, but the file should be
	//   managed by snapd instead

	// copy kernel/base/gadget into the ubuntu-data partition
	snapBlobDir := dirs.SnapBlobDirUnder(InstallHostWritableDir(model))
	mylog.Check(os.MkdirAll(snapBlobDir, 0755))

	for _, origDest := range []struct {
		orig     string
		destInfo *snap.Info
	}{
		{orig: bootWith.BasePath, destInfo: bootWith.Base},
		{orig: bootWith.KernelPath, destInfo: bootWith.Kernel},
		{orig: bootWith.GadgetPath, destInfo: bootWith.Gadget},
	} {
		mylog.Check(copyBootSnap(origDest.orig, origDest.destInfo, snapBlobDir))
	}
	mylog.Check(

		// replicate the boot assets cache in host's writable
		CopyBootAssetsCacheToRoot(InstallHostWritableDir(model)))

	var currentTrustedBootAssets bootAssetsMap
	var currentTrustedRecoveryBootAssets bootAssetsMap
	if sealer != nil {
		currentTrustedBootAssets = sealer.currentTrustedBootAssetsMap()
		currentTrustedRecoveryBootAssets = sealer.currentTrustedRecoveryBootAssetsMap()
	}
	recoverySystemLabel := bootWith.RecoverySystemLabel
	// write modeenv on the ubuntu-data partition
	modeenv := &Modeenv{
		Mode:           "run",
		RecoverySystem: recoverySystemLabel,
		// default to the system we were installed from
		CurrentRecoverySystems: []string{recoverySystemLabel},
		// which is also considered to be good
		GoodRecoverySystems:              []string{recoverySystemLabel},
		CurrentTrustedBootAssets:         currentTrustedBootAssets,
		CurrentTrustedRecoveryBootAssets: currentTrustedRecoveryBootAssets,
		// kernel command lines are set later once a boot config is
		// installed
		CurrentKernelCommandLines: nil,
		// keep this comment to make gofmt 1.9 happy
		Gadget:         bootWith.Gadget.Filename(),
		CurrentKernels: []string{bootWith.Kernel.Filename()},
		BrandID:        model.BrandID(),
		Model:          model.Model(),
		// TODO: test this
		Classic:        model.Classic(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	// Note on classic systems there is no boot base, the system boots
	// from debs.
	if !model.Classic() {
		modeenv.Base = bootWith.Base.Filename()
	}

	// get the ubuntu-boot bootloader and extract the kernel there
	opts := &bootloader.Options{
		// Bootloader for run mode
		Role: bootloader.RoleRunMode,
		// At this point the run mode bootloader is under the native
		// run partition layout, no /boot mount.
		NoSlashBoot: true,
	}
	// the bootloader config may have been installed when the ubuntu-boot
	// partition was created, but for a trusted assets the bootloader config
	// will be installed further down; for now identify the run mode
	// bootloader by looking at the gadget
	bl := mylog.Check2(bootloader.ForGadget(bootWith.UnpackedGadgetDir, InitramfsUbuntuBootDir, opts))

	// extract the kernel first and mark kernel_status ready
	kernelf := mylog.Check2(snapfile.Open(bootWith.KernelPath))
	mylog.Check(bl.ExtractKernelAssets(bootWith.Kernel, kernelf))

	blVars := map[string]string{
		"kernel_status": "",
	}

	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if ok {
		mylog.Check(
			// the bootloader supports additional extracted kernel handling

			// enable the kernel on the bootloader and finally transition to
			// run-mode last in case we get rebooted in between anywhere here

			// it's okay to enable the kernel before writing the boot vars, because
			// we haven't written snapd_recovery_mode=run, which is the critical
			// thing that will inform the bootloader to try booting from ubuntu-boot
			ebl.EnableKernel(bootWith.Kernel))
	} else {
		// the bootloader does not support additional handling of
		// extracted kernel images, we must name the kernel to be used
		// explicitly in bootloader variables
		blVars["snap_kernel"] = bootWith.Kernel.Filename()
	}
	mylog.Check(

		// set the ubuntu-boot bootloader variables before triggering transition to
		// try and boot from ubuntu-boot (that transition happens when we write
		// snapd_recovery_mode below)
		bl.SetBootVars(blVars))

	tbl, ok := bl.(bootloader.TrustedAssetsBootloader)
	if ok {
		mylog.Check(
			// the bootloader can manage its boot config

			// installing boot config must be performed after the boot
			// partition has been populated with gadget data
			bl.InstallBootConfig(bootWith.UnpackedGadgetDir, opts))

		// determine the expected command line
		cmdline := mylog.Check2(ComposeCandidateCommandLine(model, bootWith.UnpackedGadgetDir))

		modeenv.CurrentKernelCommandLines = bootCommandLines{cmdline}

		// Look at gadget default values for system.kernel.*cmdline-append options
		cmdlineAppend := mylog.Check2(buildOptionalKernelCommandLine(model, bootWith.UnpackedGadgetDir))

		candidate := false
		defaultCmdLine := mylog.Check2(tbl.DefaultCommandLine(candidate))

		cmdlineVars := mylog.Check2(bootVarsForTrustedCommandLineFromGadget(bootWith.UnpackedGadgetDir, cmdlineAppend, defaultCmdLine, model))
		mylog.Check(bl.SetBootVars(cmdlineVars))

	}
	mylog.Check(

		// all fields that needed to be set in the modeenv must have been set by
		// now, write modeenv to disk
		modeenv.WriteTo(InstallHostWritableDir(model)))

	if sealer != nil {
		hasHook := mylog.Check2(HasFDESetupHook(bootWith.Kernel))

		flags := sealKeyToModeenvFlags{
			HasFDESetupHook: hasHook,
			FactoryReset:    makeOpts.AfterDataReset,
			SeedDir:         makeOpts.SeedDir,
			StateUnlocker:   makeOpts.StateUnlocker,
		}
		if makeOpts.Standalone {
			flags.SnapsDir = snapBlobDir
		}
		mylog.Check(
			// seal the encryption key to the parameters specified in modeenv
			sealKeyToModeenv(sealer.dataEncryptionKey, sealer.saveEncryptionKey, model, modeenv, flags))

	}
	mylog.Check(

		// so far so good, we managed to install the system, so it can be used
		// for recovery as well
		MarkRecoveryCapableSystem(recoverySystemLabel))

	return nil
}

func buildOptionalKernelCommandLine(model *asserts.Model, gadgetSnapOrDir string) (string, error) {
	sf := mylog.Check2(snapfile.Open(gadgetSnapOrDir))

	gadgetInfo := mylog.Check2(gadget.ReadInfoFromSnapFile(sf, nil))

	defaults := gadget.SystemDefaults(gadgetInfo.Defaults)

	var cmdlineAppend, cmdlineAppendDangerous string

	if cmdlineAppendIf, ok := defaults["system.kernel.cmdline-append"]; ok {
		cmdlineAppend, ok = cmdlineAppendIf.(string)
		if !ok {
			return "", fmt.Errorf("system.kernel.cmdline-append is not a string")
		}
	}

	if cmdlineAppendIf, ok := defaults["system.kernel.dangerous-cmdline-append"]; ok {
		cmdlineAppendDangerous, ok = cmdlineAppendIf.(string)
		if !ok {
			return "", fmt.Errorf("system.kernel.dangerous-cmdline-append is not a string")
		}
		if model.Grade() != asserts.ModelDangerous {
			// Print a warning and ignore
			logger.Noticef("WARNING: system.kernel.dangerous-cmdline-append ignored by non-dangerous models")
			return "", nil
		}
	}

	if cmdlineAppend != "" {
		// TODO perform validation against what is allowed by the gadget
	}

	cmdlineAppend = strutil.JoinNonEmpty([]string{cmdlineAppend, cmdlineAppendDangerous}, " ")

	return cmdlineAppend, nil
}

// MakeRunnableSystem is like MakeBootableImage in that it sets up a system to
// be able to boot, but is unique in that it is intended to be called from UC20
// install mode and makes the run system bootable (hence it is called
// "runnable").
// Note that this function does not update the recovery bootloader env to
// actually transition to run mode here, that is left to the caller via
// something like boot.EnsureNextBootToRunMode(). This is to enable separately
// setting up a run system and actually transitioning to it, with hooks, etc.
// running in between.
func MakeRunnableSystem(model *asserts.Model, bootWith *BootableSet, sealer *TrustedAssetsInstallObserver) error {
	return makeRunnableSystem(model, bootWith, sealer, makeRunnableOptions{
		SeedDir: dirs.SnapSeedDir,
	})
}

// MakeRunnableStandaloneSystem operates like MakeRunnableSystem but does
// not assume that the run system being set up is related to the current
// system. This is appropriate e.g when installing from a classic installer.
func MakeRunnableStandaloneSystem(model *asserts.Model, bootWith *BootableSet, sealer *TrustedAssetsInstallObserver, unlocker Unlocker) error {
	// TODO consider merging this back into MakeRunnableSystem but need
	// to consider the properties of the different input used for sealing
	return makeRunnableSystem(model, bootWith, sealer, makeRunnableOptions{
		Standalone:    true,
		SeedDir:       dirs.SnapSeedDir,
		StateUnlocker: unlocker,
	})
}

// MakeRunnableStandaloneSystemFromInitrd is the same as MakeRunnableStandaloneSystem
// but uses seed dir path expected in initrd.
func MakeRunnableStandaloneSystemFromInitrd(model *asserts.Model, bootWith *BootableSet, sealer *TrustedAssetsInstallObserver) error {
	// TODO consider merging this back into MakeRunnableSystem but need
	// to consider the properties of the different input used for sealing
	return makeRunnableSystem(model, bootWith, sealer, makeRunnableOptions{
		Standalone: true,
		SeedDir:    filepath.Join(InitramfsRunMntDir, "ubuntu-seed"),
	})
}

// MakeRunnableSystemAfterDataReset sets up the system to be able to boot, but it is
// intended to be called from UC20 factory reset mode right before switching
// back to the new run system.
func MakeRunnableSystemAfterDataReset(model *asserts.Model, bootWith *BootableSet, sealer *TrustedAssetsInstallObserver) error {
	return makeRunnableSystem(model, bootWith, sealer, makeRunnableOptions{
		AfterDataReset: true,
		SeedDir:        dirs.SnapSeedDir,
	})
}
