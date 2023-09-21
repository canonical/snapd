// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snap"
)

var (
	// ErrBootloader is returned if the bootloader can not be determined.
	ErrBootloader = errors.New("cannot determine bootloader")

	// ErrNoTryKernelRef is returned if the bootloader finds no enabled
	// try-kernel.
	ErrNoTryKernelRef = errors.New("no try-kernel referenced")
)

// Role indicates whether the bootloader is used for recovery or run mode.
type Role string

const (
	// RoleSole applies to the sole bootloader used by UC16/18.
	RoleSole Role = ""
	// RoleRunMode applies to the run mode booloader.
	RoleRunMode Role = "run-mode"
	// RoleRecovery apllies to the recovery bootloader.
	RoleRecovery Role = "recovery"
)

// Options carries bootloader options.
type Options struct {
	// PrepareImageTime indicates whether the booloader is being
	// used at prepare-image time, that means not on a runtime
	// system.
	PrepareImageTime bool `json:"prepare-image-time,omitempty"`

	// Role specifies to use the bootloader for the given role.
	Role Role `json:"role,omitempty"`

	// NoSlashBoot indicates to use the native layout of the
	// bootloader partition and not the /boot mount.
	// It applies only for RoleRunMode.
	// It is implied and ignored for RoleRecovery.
	// It is an error to set it for RoleSole.
	NoSlashBoot bool `json:"no-slash-boot,omitempty"`
}

func (o *Options) validate() error {
	if o == nil {
		return nil
	}
	if o.NoSlashBoot && o.Role == RoleSole {
		return fmt.Errorf("internal error: bootloader.RoleSole doesn't expect NoSlashBoot set")
	}
	if o.PrepareImageTime && o.Role == RoleRunMode {
		return fmt.Errorf("internal error: cannot use run mode bootloader at prepare-image time")
	}
	return nil
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

	// Present returns whether the bootloader is currently present on the
	// system - in other words whether this bootloader has been installed to the
	// current system. Implementations should only return non-nil error if they
	// can positively identify that the bootloader is installed, but there is
	// actually an error with the installation.
	Present() (bool, error)

	// InstallBootConfig will try to install the boot config in the
	// given gadgetDir to rootdir.
	InstallBootConfig(gadgetDir string, opts *Options) error

	// ExtractKernelAssets extracts kernel assets from the given kernel snap.
	ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error

	// RemoveKernelAssets removes the assets for the given kernel snap.
	RemoveKernelAssets(s snap.PlaceInfo) error
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

// ComamndLineComponents carries the components of the kernel command line. The
// bootloader is expected to combine the provided components, optionally
// including its built-in static set of arguments, and produce a command line
// that will be passed to the kernel during boot.
type CommandLineComponents struct {
	// Argument related to mode selection.
	ModeArg string
	// Argument related to recovery system selection, relevant for given
	// mode argument.
	SystemArg string
	// Extra arguments requested by the system.
	ExtraArgs string
	// A complete set of arguments that overrides both the built-in static
	// set and ExtraArgs. Note that, it is an error if extra and full
	// arguments are non-empty.
	FullArgs string
	// A list of patterns to remove arguments from the default command line
	RemoveArgs []kcmdline.ArgumentPattern
}

func (c *CommandLineComponents) Validate() error {
	if c.ExtraArgs != "" && c.FullArgs != "" {
		return fmt.Errorf("cannot use both full and extra components of command line")
	}
	return nil
}

// TrustedAssetsBootloader has boot assets that take part in the secure boot
// process and need to be tracked, while other boot assets (typically boot
// config) are managed by snapd.
type TrustedAssetsBootloader interface {
	Bootloader

	// ManagedAssets returns a list of boot assets managed by the bootloader
	// in the boot filesystem. Does not require rootdir to be set.
	ManagedAssets() []string
	// UpdateBootConfig attempts to update the boot config assets used by
	// the bootloader. Returns true when assets were updated.
	UpdateBootConfig() (bool, error)
	// CommandLine returns the kernel command line composed of mode and
	// system arguments, followed by either a built-in bootloader specific
	// static arguments corresponding to the on-disk boot asset edition, and
	// any extra arguments or a separate set of arguments provided in the
	// components. The command line may be different when using a recovery
	// bootloader.
	CommandLine(pieces CommandLineComponents) (string, error)
	// CandidateCommandLine is similar to CommandLine, but uses the current
	// edition of managed built-in boot assets as reference.
	CandidateCommandLine(pieces CommandLineComponents) (string, error)

	// DefaultCommandLine returns the default kernel command-line
	// used by the bootloader excluding the recovery mode and
	// system parameters.
	DefaultCommandLine(candidate bool) (string, error)

	// TrustedAssets returns a map of relative paths to asset
	// identifers. The paths are inside the bootloader's rootdir
	// that are measured in the boot process. The asset
	// identifiers correspond to the backward compatible names
	// recorded in the modeenv (CurrentTrustedBootAssets and
	// CurrentTrustedRecoveryBootAssets).
	TrustedAssets() (map[string]string, error)

	// RecoveryBootChains returns the possible load chains for
	// recovery modes.  It should be called on a RoleRecovery
	// bootloader.
	RecoveryBootChains(kernelPath string) ([][]BootFile, error)

	// BootChains returns the possible load chains for run mode.
	// It should be called on a RoleRecovery bootloader passing
	// the RoleRunMode bootloader.
	BootChains(runBl Bootloader, kernelPath string) ([][]BootFile, error)
}

// NotScriptableBootloader cannot change the bootloader environment
// because it supports no scripting or cannot do any writes. This
// applies to piboot for the moment.
type NotScriptableBootloader interface {
	Bootloader

	// Sets boot variables from initramfs - this is needed in
	// addition to SetBootVars() to prevent side effects like
	// re-writing the bootloader configuration.
	SetBootVarsFromInitramfs(values map[string]string) error
}

// RebootBootloader needs arguments to the reboot syscall when snaps
// are being updated.
type RebootBootloader interface {
	Bootloader

	// GetRebootArguments returns the needed reboot arguments
	GetRebootArguments() (string, error)
}

// UefiBootloader provides data for setting EFI boot variables.
type UefiBootloader interface {
	Bootloader

	// EfiLoadOptionParameters returns the data which may be used to construct
	// an EFI load option.
	EfiLoadOptionParameters() (description string, assetPath string, optionalData []byte, err error)
}

func genericInstallBootConfig(gadgetFile, systemFile string) error {
	if err := os.MkdirAll(filepath.Dir(systemFile), 0755); err != nil {
		return err
	}
	return osutil.CopyFile(gadgetFile, systemFile, osutil.CopyFlagOverwrite)
}

func genericSetBootConfigFromAsset(systemFile, assetName string) error {
	bootConfig := assets.Internal(assetName)
	if bootConfig == nil {
		return fmt.Errorf("internal error: no boot asset for %q", assetName)
	}
	if err := os.MkdirAll(filepath.Dir(systemFile), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(systemFile, bootConfig, 0644, 0)
}

func genericUpdateBootConfigFromAssets(systemFile string, assetName string) (updated bool, err error) {
	currentBootConfigEdition, err := editionFromDiskConfigAsset(systemFile)
	if err != nil && err != errNoEdition {
		return false, err
	}
	if err == errNoEdition {
		return false, nil
	}
	newBootConfig := assets.Internal(assetName)
	if len(newBootConfig) == 0 {
		return false, fmt.Errorf("no boot config asset with name %q", assetName)
	}
	bc, err := configAssetFrom(newBootConfig)
	if err != nil {
		return false, err
	}
	if bc.Edition() <= currentBootConfigEdition {
		// edition of the candidate boot config is lower than or equal
		// to one currently installed
		return false, nil
	}
	if err := osutil.AtomicWriteFile(systemFile, bc.Raw(), 0644, 0); err != nil {
		return false, err
	}
	return true, nil
}

// InstallBootConfig installs the bootloader config from the gadget
// snap dir into the right place.
func InstallBootConfig(gadgetDir, rootDir string, opts *Options) error {
	if err := opts.validate(); err != nil {
		return err
	}
	bl, err := ForGadget(gadgetDir, rootDir, opts)
	if err != nil {
		return fmt.Errorf("cannot find boot config in %q", gadgetDir)
	}
	return bl.InstallBootConfig(gadgetDir, opts)
}

type bootloaderNewFunc func(rootdir string, opts *Options) Bootloader

var (
	//  bootloaders list all possible bootloaders by their constructor
	//  function.
	bootloaders = []bootloaderNewFunc{
		newUboot,
		newGrub,
		newAndroidBoot,
		newLk,
		newPiboot,
	}
)

var (
	forcedBootloader Bootloader
	forcedError      error
)

// Find returns the bootloader for the system
// or an error if no bootloader is found.
//
// The rootdir option is useful for image creation operations. It
// can also be used to find the recovery bootloader, e.g. on uc20:
//
//	bootloader.Find("/run/mnt/ubuntu-seed")
func Find(rootdir string, opts *Options) (Bootloader, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if forcedBootloader != nil || forcedError != nil {
		return forcedBootloader, forcedError
	}

	if rootdir == "" {
		rootdir = dirs.GlobalRootDir
	}
	if opts == nil {
		opts = &Options{}
	}

	// note that the order of this is not deterministic
	for _, blNew := range bootloaders {
		bl := blNew(rootdir, opts)
		present, err := bl.Present()
		if err != nil {
			return nil, fmt.Errorf("bootloader %q found but not usable: %v", bl.Name(), err)
		}
		if present {
			return bl, nil
		}
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

// ForGadget returns a bootloader matching a given gadget by inspecting the
// contents of gadget directory or an error if no matching bootloader is found.
func ForGadget(gadgetDir, rootDir string, opts *Options) (Bootloader, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if forcedBootloader != nil || forcedError != nil {
		return forcedBootloader, forcedError
	}
	for _, blNew := range bootloaders {
		bl := blNew(rootDir, opts)
		markerConf := filepath.Join(gadgetDir, bl.Name()+".conf")
		// do we have a marker file?
		if osutil.FileExists(markerConf) {
			return bl, nil
		}
	}
	return nil, ErrBootloader
}

// BootFile represents each file in the chains of trusted assets and
// kernels used in the boot process. For example a boot file can be an
// EFI binary or a snap file containing an EFI binary.
type BootFile struct {
	// Path is the path to the file in the filesystem or, if Snap
	// is set, the relative path inside the snap file.
	Path string
	// Snap contains the path to the snap file if a snap file is used.
	Snap string
	// Role is set to the role of the bootloader this boot file
	// originates from.
	Role Role
}

func NewBootFile(snap, path string, role Role) BootFile {
	return BootFile{
		Snap: snap,
		Path: path,
		Role: role,
	}
}

// WithPath returns a copy of the BootFile with path updated to the
// specified value.
func (b BootFile) WithPath(path string) BootFile {
	b.Path = path
	return b
}
