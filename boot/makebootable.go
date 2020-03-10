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
)

// BootableSet represents the boot snaps of a system to be made bootable.
type BootableSet struct {
	Base       *snap.Info
	BasePath   string
	Kernel     *snap.Info
	KernelPath string

	RecoverySystemDir string

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
func MakeBootable(model *asserts.Model, rootdir string, bootWith *BootableSet) error {
	if model.Grade() == asserts.ModelGradeUnset {
		return makeBootable16(model, rootdir, bootWith)
	}

	if !bootWith.Recovery {
		return makeBootable20RunMode(model, rootdir, bootWith)
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
	kernelf, err := snap.Open(bootWith.KernelPath)
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

	opts := &bootloader.Options{
		PrepareImageTime: true,
		// setup the recovery bootloader
		Recovery: true,
	}

	// install the bootloader configuration from the gadget
	if err := bootloader.InstallBootConfig(bootWith.UnpackedGadgetDir, rootdir, opts); err != nil {
		return err
	}

	// TODO:UC20: extract kernel for e.g. ARM

	// now install the recovery system specific boot config
	bl, err := bootloader.Find(rootdir, opts)
	if err != nil {
		return fmt.Errorf("internal error: cannot find bootloader: %v", err)
	}
	rbl, ok := bl.(bootloader.RecoveryAwareBootloader)
	if !ok {
		return fmt.Errorf("cannot use %s bootloader: does not support recovery systems", bl.Name())
	}
	kernelPath, err := filepath.Rel(rootdir, bootWith.KernelPath)
	if err != nil {
		return fmt.Errorf("cannot construct kernel boot path: %v", err)
	}
	blVars := map[string]string{
		"snapd_recovery_kernel": filepath.Join("/", kernelPath),
	}
	if err := rbl.SetRecoverySystemEnv(bootWith.RecoverySystemDir, blVars); err != nil {
		return fmt.Errorf("cannot set recovery system environment: %v", err)
	}

	return nil
}

func makeBootable20RunMode(model *asserts.Model, rootdir string, bootWith *BootableSet) error {
	// XXX: move to dirs ?
	runMnt := filepath.Join(rootdir, "/run/mnt/")

	// TODO:UC20:
	// - create grub.cfg instead of using the gadget one

	// copy kernel/base into the ubuntu-data partition
	ubuntuDataMnt := filepath.Join(runMnt, "ubuntu-data")
	snapBlobDir := dirs.SnapBlobDirUnder(filepath.Join(ubuntuDataMnt, "system-data"))
	if err := os.MkdirAll(snapBlobDir, 0755); err != nil {
		return err
	}
	for _, fn := range []string{bootWith.BasePath, bootWith.KernelPath} {
		dst := filepath.Join(snapBlobDir, filepath.Base(fn))
		if err := osutil.CopyFile(fn, dst, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync); err != nil {
			return err
		}
	}

	// write modeenv on the ubuntu-data partition
	modeenv := &Modeenv{
		Mode:           "run",
		RecoverySystem: filepath.Base(bootWith.RecoverySystemDir),
		Base:           filepath.Base(bootWith.BasePath),
		CurrentKernels: []string{bootWith.Kernel.Filename()},
	}
	if err := modeenv.Write(filepath.Join(runMnt, "ubuntu-data", "system-data")); err != nil {
		return fmt.Errorf("cannot write modeenv: %v", err)
	}

	ubuntuBootPartition := filepath.Join(runMnt, "ubuntu-boot")

	// get the ubuntu-boot grub and extract the kernel there
	opts := &bootloader.Options{
		// At this point the run mode bootloader is under the native
		// layout, no /boot mount.
		NoSlashBoot: true,

		// extract efi kernel assets to the ubuntu-boot partition
		ExtractedRunKernelImage: true,
	}
	bl, err := bootloader.Find(ubuntuBootPartition, opts)
	if err != nil {
		return fmt.Errorf("internal error: cannot find run system bootloader: %v", err)
	}

	// extract the kernel first, then mark kernel_status ready, then make the
	// symlink and finally transition to run-mode in case we get rebooted in
	// between anywhere here
	kernelf, err := snap.Open(bootWith.KernelPath)
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
	if err := bl.SetBootVars(blVars); err != nil {
		return fmt.Errorf("cannot set run system environment: %v", err)
	}

	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if !ok {
		return fmt.Errorf("cannot use %s bootloader: does not support extracted run kernel images", bl.Name())
	}

	if err := ebl.EnableKernel(bootWith.Kernel); err != nil {
		return err
	}

	// LAST step: update recovery grub's grubenv to indicate that
	// we transition to run mode now
	opts = &bootloader.Options{
		// setup the recovery bootloader
		Recovery: true,
	}
	bl, err = bootloader.Find(filepath.Join(runMnt, "ubuntu-seed"), opts)
	if err != nil {
		return fmt.Errorf("internal error: cannot find recovery system bootloader: %v", err)
	}
	blVars = map[string]string{
		"snapd_recovery_mode": "run",
	}
	if err := bl.SetBootVars(blVars); err != nil {
		return fmt.Errorf("cannot set recovery system environment: %v", err)
	}
	return nil
}
