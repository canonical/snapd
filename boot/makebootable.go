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

package boot

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
)

// BootableSet represents the boot snaps of a system to be made bootable.
type BootableSet struct {
	Base       *snap.Info
	BasePath   string
	Kernel     *snap.Info
	KernelPath string

	RecoverySystemLabel string
	RecoverySystemDir   string

	UnpackedGadgetDir string

	// Recover is set when making the recovery partition bootable.
	Recovery bool
}

// MakeBootable sets up the given bootable set and target filesystem
// such that the system can be booted.
//
// rootdir points to an image filesystem (UC 16/18), image recovery
// filesystem (UC20 at prepare-image time) or ephemeral system (UC20
// install mode).
func MakeBootable(model *asserts.Model, rootdir string, bootWith *BootableSet, sealer *TrustedAssetsInstallObserver) error {
	if model.Grade() == asserts.ModelGradeUnset {
		return makeBootable16(model, rootdir, bootWith)
	}

	if !bootWith.Recovery {
		return makeBootable20RunMode(model, rootdir, bootWith, sealer)
	}
	return makeBootable20(model, rootdir, bootWith)
}

// makeBootable16 setups the image filesystem for boot with UC16
// and UC18 models. This entails:
//  - installing the bootloader configuration from the gadget
//  - creating symlinks for boot snaps from seed to the runtime blob dir
//  - setting boot env vars pointing to the revisions of the boot snaps to use
//  - extracting kernel assets as needed by the bootloader
func makeBootable16(model *asserts.Model, rootdir string, bootWith *BootableSet) error {
	opts := &bootloader.Options{
		PrepareImageTime: true,
	}

	// install the bootloader configuration from the gadget
	if err := bootloader.InstallBootConfig(bootWith.UnpackedGadgetDir, rootdir, opts); err != nil {
		return err
	}

	// setup symlinks for kernel and boot base from the blob directory
	// to the seed snaps

	snapBlobDir := dirs.SnapBlobDirUnder(rootdir)
	if err := os.MkdirAll(snapBlobDir, 0755); err != nil {
		return err
	}

	for _, fn := range []string{bootWith.BasePath, bootWith.KernelPath} {
		dst := filepath.Join(snapBlobDir, filepath.Base(fn))
		// construct a relative symlink from the blob dir
		// to the seed snap file
		relSymlink, err := filepath.Rel(snapBlobDir, fn)
		if err != nil {
			return fmt.Errorf("cannot build symlink for boot snap: %v", err)
		}
		if err := os.Symlink(relSymlink, dst); err != nil {
			return err
		}
	}

	// Set bootvars for kernel/core snaps so the system boots and
	// does the first-time initialization. There is also no
	// mounted kernel/core/base snap, but just the blobs.
	bl, err := bootloader.Find(rootdir, opts)
	if err != nil {
		return fmt.Errorf("cannot set kernel/core boot variables: %s", err)
	}

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
	kernelf, err := snapfile.Open(bootWith.KernelPath)
	if err != nil {
		return err
	}
	if err := bl.ExtractKernelAssets(bootWith.Kernel, kernelf); err != nil {
		return err
	}
	setBoot("snap_kernel", bootWith.KernelPath)

	if err := bl.SetBootVars(m); err != nil {
		return err
	}

	return nil
}

func makeBootable20(model *asserts.Model, rootdir string, bootWith *BootableSet) error {
	// we can only make a single recovery system bootable right now
	recoverySystems, err := filepath.Glob(filepath.Join(rootdir, "systems/*"))
	if err != nil {
		return fmt.Errorf("cannot validate recovery systems: %v", err)
	}
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

	// install the bootloader configuration from the gadget
	if err := bootloader.InstallBootConfig(bootWith.UnpackedGadgetDir, rootdir, opts); err != nil {
		return err
	}

	// now install the recovery system specific boot config
	bl, err := bootloader.Find(rootdir, opts)
	if err != nil {
		return fmt.Errorf("internal error: cannot find bootloader: %v", err)
	}

	// record which recovery system is to be used on the bootloader, note
	// that this goes on the main bootloader environment, and not on the
	// recovery system bootloader environment, for example for grub
	// bootloader, this env var is set on the ubuntu-seed root grubenv, and
	// not on the recovery system grubenv in the systems/20200314/ subdir on
	// ubuntu-seed
	blVars := map[string]string{
		"snapd_recovery_system": bootWith.RecoverySystemLabel,
	}
	if err := bl.SetBootVars(blVars); err != nil {
		return fmt.Errorf("cannot set recovery environment: %v", err)
	}

	// on e.g. ARM we need to extract the kernel assets on the recovery
	// system as well, but the bootloader does not load any environment from
	// the recovery system
	erkbl, ok := bl.(bootloader.ExtractedRecoveryKernelImageBootloader)
	if ok {
		kernelf, err := snapfile.Open(bootWith.KernelPath)
		if err != nil {
			return err
		}

		err = erkbl.ExtractRecoveryKernelAssets(
			bootWith.RecoverySystemDir,
			bootWith.Kernel,
			kernelf,
		)
		if err != nil {
			return fmt.Errorf("cannot extract recovery system kernel assets: %v", err)
		}

		return nil
	}

	rbl, ok := bl.(bootloader.RecoveryAwareBootloader)
	if !ok {
		return fmt.Errorf("cannot use %s bootloader: does not support recovery systems", bl.Name())
	}
	kernelPath, err := filepath.Rel(rootdir, bootWith.KernelPath)
	if err != nil {
		return fmt.Errorf("cannot construct kernel boot path: %v", err)
	}
	recoveryBlVars := map[string]string{
		"snapd_recovery_kernel": filepath.Join("/", kernelPath),
	}
	if err := rbl.SetRecoverySystemEnv(bootWith.RecoverySystemDir, recoveryBlVars); err != nil {
		return fmt.Errorf("cannot set recovery system environment: %v", err)
	}
	return nil
}

func makeBootable20RunMode(model *asserts.Model, rootdir string, bootWith *BootableSet, sealer *TrustedAssetsInstallObserver) error {
	// TODO:UC20:
	// - figure out what to do for uboot gadgets, currently we require them to
	//   install the boot.sel onto ubuntu-boot directly, but the file should be
	//   managed by snapd instead

	// copy kernel/base into the ubuntu-data partition
	snapBlobDir := dirs.SnapBlobDirUnder(InstallHostWritableDir)
	if err := os.MkdirAll(snapBlobDir, 0755); err != nil {
		return err
	}
	for _, fn := range []string{bootWith.BasePath, bootWith.KernelPath} {
		dst := filepath.Join(snapBlobDir, filepath.Base(fn))
		// if the source filename is a symlink, don't copy the symlink, copy the
		// target file instead of copying the symlink, as the initramfs won't
		// follow the symlink when it goes to mount the base and kernel snaps by
		// design as the initramfs should only be using trusted things from
		// ubuntu-data to boot in run mode
		if osutil.IsSymlink(fn) {
			link, err := os.Readlink(fn)
			if err != nil {
				return err
			}
			fn = link
		}
		if err := osutil.CopyFile(fn, dst, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync); err != nil {
			return err
		}
	}

	// replicate the boot assets cache in host's writable
	if err := CopyBootAssetsCacheToRoot(InstallHostWritableDir); err != nil {
		return fmt.Errorf("cannot replicate boot assets cache: %v", err)
	}

	var currentTrustedBootAssets bootAssetsMap
	var currentTrustedRecoveryBootAssets bootAssetsMap
	if sealer != nil {
		currentTrustedBootAssets = sealer.currentTrustedBootAssetsMap()
		currentTrustedRecoveryBootAssets = sealer.currentTrustedRecoveryBootAssetsMap()
	}
	recoverySystemLabel := filepath.Base(bootWith.RecoverySystemDir)
	// write modeenv on the ubuntu-data partition
	modeenv := &Modeenv{
		Mode:           "run",
		RecoverySystem: recoverySystemLabel,
		// default to the system we were installed from
		CurrentRecoverySystems:           []string{recoverySystemLabel},
		CurrentTrustedBootAssets:         currentTrustedBootAssets,
		CurrentTrustedRecoveryBootAssets: currentTrustedRecoveryBootAssets,
		// keep this comment to make gofmt 1.9 happy
		Base:           filepath.Base(bootWith.BasePath),
		CurrentKernels: []string{bootWith.Kernel.Filename()},
		BrandID:        model.BrandID(),
		Model:          model.Model(),
		Grade:          string(model.Grade()),
	}
	if err := modeenv.WriteTo(InstallHostWritableDir); err != nil {
		return fmt.Errorf("cannot write modeenv: %v", err)
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
	bl, err := bootloader.ForGadget(bootWith.UnpackedGadgetDir, InitramfsUbuntuBootDir, opts)
	if err != nil {
		return fmt.Errorf("internal error: cannot identify run system bootloader: %v", err)
	}

	// extract the kernel first and mark kernel_status ready
	kernelf, err := snapfile.Open(bootWith.KernelPath)
	if err != nil {
		return err
	}

	err = bl.ExtractKernelAssets(bootWith.Kernel, kernelf)
	if err != nil {
		return err
	}

	blVars := map[string]string{
		"kernel_status": "",
	}

	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if ok {
		// the bootloader supports additional extracted kernel handling

		// enable the kernel on the bootloader and finally transition to
		// run-mode last in case we get rebooted in between anywhere here

		// it's okay to enable the kernel before writing the boot vars, because
		// we haven't written snapd_recovery_mode=run, which is the critical
		// thing that will inform the bootloader to try booting from ubuntu-boot
		if err := ebl.EnableKernel(bootWith.Kernel); err != nil {
			return err
		}
	} else {
		// TODO:UC20: should we make this more explicit with a new
		//            bootloader interface that is checked for first before
		//            ExtractedRunKernelImageBootloader the same way we do with
		//            ExtractedRecoveryKernelImageBootloader?

		// the bootloader does not support additional handling of
		// extracted kernel images, we must name the kernel to be used
		// explicitly in bootloader variables
		blVars["snap_kernel"] = bootWith.Kernel.Filename()
	}

	// set the ubuntu-boot bootloader variables before triggering transition to
	// try and boot from ubuntu-boot (that transition happens when we write
	// snapd_recovery_mode below)
	if err := bl.SetBootVars(blVars); err != nil {
		return fmt.Errorf("cannot set run system environment: %v", err)
	}

	_, ok = bl.(bootloader.TrustedAssetsBootloader)
	if ok {
		// the bootloader can manage its boot config

		// installing boot config must be performed after the boot
		// partition has been populated with gadget data
		ok, err := bl.InstallBootConfig(bootWith.UnpackedGadgetDir, opts)
		if err != nil {
			return fmt.Errorf("cannot install managed bootloader assets: %v", err)
		}
		if !ok {
			return fmt.Errorf("cannot install boot config with a mismatched gadget")
		}
	}

	if sealer != nil {
		// seal the encryption key to the parameters specified in modeenv
		if err := sealKeyToModeenv(sealer.dataEncryptionKey, sealer.saveEncryptionKey, model, bootWith, modeenv); err != nil {
			return err
		}
	}

	// LAST step: update recovery bootloader environment to indicate that we
	// transition to run mode now
	opts = &bootloader.Options{
		// let the bootloader know we will be touching the recovery
		// partition
		Role: bootloader.RoleRecovery,
	}
	bl, err = bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return fmt.Errorf("internal error: cannot find recovery system bootloader: %v", err)
	}
	blVars = map[string]string{
		"snapd_recovery_mode": "run",
	}
	if err := bl.SetBootVars(blVars); err != nil {
		return fmt.Errorf("cannot set recovery environment: %v", err)
	}
	return nil
}
