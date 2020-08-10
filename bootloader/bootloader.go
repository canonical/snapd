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

	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

var (
	// ErrBootloader is returned if the bootloader can not be determined.
	ErrBootloader = errors.New("cannot determine bootloader")

	// ErrNoTryKernelRef is returned if the bootloader finds no enabled
	// try-kernel.
	ErrNoTryKernelRef = errors.New("no try-kernel referenced")
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

	// TODO:UC20 consider different/better names for flags that follow

	// NoSlashBoot indicates to use the run mode bootloader but
	// under the native layout and not the /boot mount.
	NoSlashBoot bool

	// ExtractedRunKernelImage is whether to force kernel asset extraction.
	ExtractedRunKernelImage bool
}

// Bootloader provides an interface to interact with the system
// bootloader.
type Bootloader interface {
	// Return the value of the specified bootloader variable.
	GetBootVars(names ...string) (map[string]string, error)

	// Set the value of the specified bootloader variable.
	SetBootVars(values map[string]string) error

	// Name returns the bootloader name.
	Name() string

	// ConfigFile returns the name of the config file.
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
	GetRecoverySystemEnv(recoverySystemDir string, key string) (string, error)
}

type ExtractedRecoveryKernelImageBootloader interface {
	Bootloader
	ExtractRecoveryKernelAssets(recoverySystemDir string, s snap.PlaceInfo, snapf snap.Container) error
}

// ExtractedRunKernelImageBootloader is a Bootloader that also supports specific
// methods needed to setup booting from an extracted kernel, which is needed to
// implement encryption and/or secure boot. Prototypical implementation is UC20
// grub implementation with FDE.
type ExtractedRunKernelImageBootloader interface {
	Bootloader

	// EnableKernel enables the specified kernel on ubuntu-boot to be used
	// during normal boots. The specified kernel should already have been
	// extracted. This is usually implemented with a "kernel.efi" symlink
	// pointing to the extracted kernel image.
	EnableKernel(snap.PlaceInfo) error

	// EnableTryKernel enables the specified kernel on ubuntu-boot to be
	// tried by the bootloader on a reboot, to be used in conjunction with
	// setting "kernel_status" to "try". The specified kernel should already
	// have been extracted. This is usually implemented with a
	// "try-kernel.efi" symlink pointing to the extracted kernel image.
	EnableTryKernel(snap.PlaceInfo) error

	// Kernel returns the current enabled kernel on the bootloader, not
	// necessarily the kernel that was used to boot the current session, but the
	// kernel that is enabled to boot on "normal" boots.
	// If error is not nil, the first argument shall be non-nil.
	Kernel() (snap.PlaceInfo, error)

	// TryKernel returns the current enabled try-kernel on the bootloader, if
	// there is no such enabled try-kernel, then ErrNoTryKernelRef is returned.
	// If error is not nil, the first argument shall be non-nil.
	TryKernel() (snap.PlaceInfo, error)

	// DisableTryKernel disables the current enabled try-kernel on the
	// bootloader, if it exists. It does not need to return an error if the
	// enabled try-kernel does not exist or is in an inconsistent state before
	// disabling it, errors should only be returned when the implementation
	// fails to disable the try-kernel.
	DisableTryKernel() error
}

// ManagedAssetsBootloader has its boot assets (typically boot config) managed
// by snapd.
type ManagedAssetsBootloader interface {
	Bootloader

	// IsCurrentlyManaged returns true when the on disk boot assets are managed.
	IsCurrentlyManaged() (bool, error)
	// ManagedAssets returns a list of boot assets managed by the bootloader
	// in the boot filesystem.
	ManagedAssets() []string
	// UpdateBootConfig updates the boot config assets used by the bootloader.
	UpdateBootConfig(*Options) error
	// CommandLine returns the kernel command line composed of mode and
	// system arguments, built-in bootloader specific static arguments
	// corresponding to the on-disk boot asset edition, followed by any
	// extra arguments. The command line may be different when using a
	// recovery bootloader.
	CommandLine(modeArg, systemArg, extraArgs string) (string, error)
	// CandidateCommandLine is similar to CommandLine, but uses the current
	// edition of managed built-in boot assets as reference.
	CandidateCommandLine(modeArg, systemArg, extraArgs string) (string, error)
}

// TrustedAssetsBootloader has boot assets that take part in secure boot
// process.
type TrustedAssetsBootloader interface {
	// TrustedAssetsChain returns the list of relative paths to files inside
	// the bootloader's rootdir that are measured in the boot process in the
	// order of loading during the boot.
	TrustedAssetsChain() ([]string, error)
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

func genericSetBootConfigFromAsset(systemFile, assetName string) (bool, error) {
	bootConfig := assets.Internal(assetName)
	if bootConfig == nil {
		return true, fmt.Errorf("internal error: no boot asset for %q", assetName)
	}
	if err := os.MkdirAll(filepath.Dir(systemFile), 0755); err != nil {
		return true, err
	}
	return true, osutil.AtomicWriteFile(systemFile, bootConfig, 0644, 0)
}

func genericUpdateBootConfigFromAssets(systemFile string, assetName string) error {
	currentBootConfigEdition, err := editionFromDiskConfigAsset(systemFile)
	if err != nil && err != errNoEdition {
		return err
	}
	if err == errNoEdition {
		return nil
	}
	newBootConfig := assets.Internal(assetName)
	if len(newBootConfig) == 0 {
		return fmt.Errorf("no boot config asset with name %q", assetName)
	}
	bc, err := configAssetFrom(newBootConfig)
	if err != nil {
		return err
	}
	if bc.Edition() <= currentBootConfigEdition {
		// edition of the candidate boot config is lower than or equal
		// to one currently installed
		return nil
	}
	return osutil.AtomicWriteFile(systemFile, bc.Raw(), 0644, 0)
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
	if uboot := newUboot(rootdir, opts); uboot != nil {
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

func extractKernelAssetsToBootDir(dstDir string, snapf snap.Container, assets []string) error {
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
	blobName := s.Filename()
	dstDir := filepath.Join(bootDir, blobName)
	if err := os.RemoveAll(dstDir); err != nil {
		return err
	}

	return nil
}
