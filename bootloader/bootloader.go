// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package bootloader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

var (
	// ErrBootloader is returned if the bootloader can not be determined
	ErrBootloader = errors.New("cannot determine bootloader")
)

// Options carries bootloader options.
type Options struct {
	// PrepareImageTime indicates whether the booloader is being
	// used at prepare-image time, that means not on a runtime
	// system.
	PrepareImageTime bool

	// Recovery indicates to use the recovery bootloader. Note that
	// UC16/18 do not have a recovery partition.
	Recovery bool

	// NoSlashBoot indicates to use the run mode bootloader but
	// under the native layout and not the /boot mount.
	NoSlashBoot bool

	// ExtractedRunKernelImage is whether to force kernel asset extraction.
	ExtractedRunKernelImage bool
}

// Bootloader provides an interface to interact with the system
// bootloader
type Bootloader interface {
	// Return the value of the specified bootloader variable
	GetBootVars(names ...string) (map[string]string, error)

	// Set the value of the specified bootloader variable
	SetBootVars(values map[string]string) error

	// Name returns the bootloader name
	Name() string

	// ConfigFile returns the name of the config file
	ConfigFile() string

	// InstallBootConfig will try to install the boot config in the
	// given gadgetDir to rootdir. If no boot config for this bootloader
	// is found ok is false.
	InstallBootConfig(gadgetDir string, opts *Options) (ok bool, err error)

	// ExtractKernelAssets extracts kernel assets from the given kernel snap.
	ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error

	// RemoveKernelAssets removes the assets for the given kernel snap.
	RemoveKernelAssets(s snap.PlaceInfo) error
}

type installableBootloader interface {
	Bootloader
	setRootDir(string)
}

type RecoveryAwareBootloader interface {
	Bootloader
	SetRecoverySystemEnv(recoverySystemDir string, values map[string]string) error
}

type ExtractedRunKernelImageBootloader interface {
	Bootloader
	EnableKernel(snap.PlaceInfo) error        // makes the symlink
	EnableTryKernel(snap.PlaceInfo) error     // makes the symlink
	Kernel() (snap.PlaceInfo, error)          // gives the symlink
	TryKernel() (snap.PlaceInfo, bool, error) // gives the symlink (if exists)
	DisableTryKernel() error                  // removes the symlink
}

func genericInstallBootConfig(gadgetFile, systemFile string) (bool, error) {
	if !osutil.FileExists(gadgetFile) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(systemFile), 0755); err != nil {
		return true, err
	}
	return true, osutil.CopyFile(gadgetFile, systemFile, osutil.CopyFlagOverwrite)
}

// InstallBootConfig installs the bootloader config from the gadget
// snap dir into the right place.
func InstallBootConfig(gadgetDir, rootDir string, opts *Options) error {
	for _, bl := range []installableBootloader{&grub{}, &uboot{}, &androidboot{}, &lk{}} {
		bl.setRootDir(rootDir)
		ok, err := bl.InstallBootConfig(gadgetDir, opts)
		if ok {
			return err
		}
	}

	return fmt.Errorf("cannot find boot config in %q", gadgetDir)
}

var (
	forcedBootloader Bootloader
	forcedError      error
)

// Find returns the bootloader for the system
// or an error if no bootloader is found.
//
// The rootdir option is useful for image creation operations. It
// can also be used to find the recovery bootloader, e.g. on uc20:
//   bootloader.Find("/run/mnt/ubuntu-seed")
func Find(rootdir string, opts *Options) (Bootloader, error) {
	if forcedBootloader != nil || forcedError != nil {
		return forcedBootloader, forcedError
	}

	if rootdir == "" {
		rootdir = dirs.GlobalRootDir
	}
	if opts == nil {
		opts = &Options{}
	}

	// try uboot
	if uboot := newUboot(rootdir); uboot != nil {
		return uboot, nil
	}

	// no, try grub
	if grub := newGrub(rootdir, opts); grub != nil {
		return grub, nil
	}

	// no, try androidboot
	if androidboot := newAndroidBoot(rootdir); androidboot != nil {
		return androidboot, nil
	}

	// no, try lk
	if lk := newLk(rootdir, opts); lk != nil {
		return lk, nil
	}

	// no, weeeee
	return nil, ErrBootloader
}

// Force can be used to force Find to always find the specified bootloader; use
// nil to reset to normal lookup.
func Force(booloader Bootloader) {
	forcedBootloader = booloader
	forcedError = nil
}

// ForceError can be used to force Find to return an error; use nil to
// reset to normal lookup.
func ForceError(err error) {
	forcedBootloader = nil
	forcedError = err
}

func extractKernelAssetsToBootDir(dstDir string, s snap.PlaceInfo, snapf snap.Container, assets []string) error {
	// now do the kernel specific bits
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	dir, err := os.Open(dstDir)
	if err != nil {
		return err
	}
	defer dir.Close()

	for _, src := range assets {
		if err := snapf.Unpack(src, dstDir); err != nil {
			return err
		}
		if err := dir.Sync(); err != nil {
			return err
		}
	}
	return nil
}

func removeKernelAssetsFromBootDir(bootDir string, s snap.PlaceInfo) error {
	// remove the kernel blob
	blobName := filepath.Base(s.MountFile())
	dstDir := filepath.Join(bootDir, blobName)
	if err := os.RemoveAll(dstDir); err != nil {
		return err
	}

	return nil
}
