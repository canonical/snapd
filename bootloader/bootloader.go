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
	"strings"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

const (
	// bootloader variable used to determine if boot was
	// successful.  Set to value of either modeTry (when
	// attempting to boot a new rootfs) or modeSuccess (to denote
	// that the boot of the new rootfs was successful).
	bootmodeVar = "snap_mode"

	// Initial and final values
	modeTry     = "try"
	modeSuccess = ""
)

var (
	// ErrBootloader is returned if the bootloader can not be determined
	ErrBootloader = errors.New("cannot determine bootloader")
)

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

	// ExtractKernelAssets extracts kernel assets from the given kernel snap
	ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error

	// RemoveKernelAssets removes the assets for the given kernel snap.
	RemoveKernelAssets(s snap.PlaceInfo) error
}

// InstallBootConfig installs the bootloader config from the gadget
// snap dir into the right place.
func InstallBootConfig(gadgetDir string) error {
	for _, bl := range []Bootloader{&grub{}, &uboot{}, &androidboot{}} {
		// the bootloader config file has to be root of the gadget snap
		gadgetFile := filepath.Join(gadgetDir, bl.Name()+".conf")
		if !osutil.FileExists(gadgetFile) {
			continue
		}

		systemFile := bl.ConfigFile()
		if err := os.MkdirAll(filepath.Dir(systemFile), 0755); err != nil {
			return err
		}
		return osutil.CopyFile(gadgetFile, systemFile, osutil.CopyFlagOverwrite)
	}

	return fmt.Errorf("cannot find boot config in %q", gadgetDir)
}

var (
	forcedBootloader Bootloader
	forcedError      error
)

// Find returns the bootloader for the given system
// or an error if no bootloader is found
func Find() (Bootloader, error) {
	if forcedBootloader != nil || forcedError != nil {
		return forcedBootloader, forcedError
	}

	// try uboot
	if uboot := newUboot(); uboot != nil {
		return uboot, nil
	}

	// no, try grub
	if grub := newGrub(); grub != nil {
		return grub, nil
	}

	// no, try androidboot
	if androidboot := newAndroidBoot(); androidboot != nil {
		return androidboot, nil
	}

	// no, weeeee
	return nil, ErrBootloader
}

// Force can be used to force setting a booloader to that Find will not use the
// usual lookup process; use nil to reset to normal lookup.
func Force(booloader Bootloader) {
	forcedBootloader = booloader
	forcedError = nil
}

// Force can be used to force Find to return an error; use nil to
// reset to normal lookup.
func ForceError(err error) {
	forcedBootloader = nil
	forcedError = err
}

// MarkBootSuccessful marks the current boot as successful. This means
// that snappy will consider this combination of kernel/os a valid
// target for rollback.
//
// The states that a boot goes through are the following:
// - By default snap_mode is "" in which case the bootloader loads
//   two squashfs'es denoted by variables snap_core and snap_kernel.
// - On a refresh of core/kernel snapd will set snap_mode=try and
//   will also set snap_try_{core,kernel} to the core/kernel that
//   will be tried next.
// - On reboot the bootloader will inspect the snap_mode and if the
//   mode is set to "try" it will set "snap_mode=trying" and then
//   try to boot the snap_try_{core,kernel}".
// - On a successful boot snapd resets snap_mode to "" and copies
//   snap_try_{core,kernel} to snap_{core,kernel}. The snap_try_*
//   values are cleared afterwards.
// - On a failing boot the bootloader will see snap_mode=trying which
//   means snapd did not start successfully. In this case the bootloader
//   will set snap_mode="" and the system will boot with the known good
//   values from snap_{core,kernel}
func MarkBootSuccessful(bootloader Bootloader) error {
	m, err := bootloader.GetBootVars("snap_mode", "snap_try_core", "snap_try_kernel")
	if err != nil {
		return err
	}

	// snap_mode goes from "" -> "try" -> "trying" -> ""
	// so if we are not in "trying" mode, nothing to do here
	if m["snap_mode"] != "trying" {
		return nil
	}

	// update the boot vars
	for _, k := range []string{"kernel", "core"} {
		tryBootVar := fmt.Sprintf("snap_try_%s", k)
		bootVar := fmt.Sprintf("snap_%s", k)
		// update the boot vars
		if m[tryBootVar] != "" {
			m[bootVar] = m[tryBootVar]
			m[tryBootVar] = ""
		}
	}
	m["snap_mode"] = modeSuccess

	return bootloader.SetBootVars(m)
}

// SetNextBoot will schedule the snap to be used in the next boot. For
// base snaps it is up to the caller to select the right bootable base
// (from the model assertion).
func SetNextBoot(s snap.PlaceInfo, t snap.Type) error {
	bootloader, err := Find()
	if err != nil {
		return fmt.Errorf("cannot set next boot: %s", err)
	}

	var nextBoot, goodBoot string
	switch t {
	case snap.TypeOS, snap.TypeBase:
		nextBoot = "snap_try_core"
		goodBoot = "snap_core"
	case snap.TypeKernel:
		nextBoot = "snap_try_kernel"
		goodBoot = "snap_kernel"
	}
	blobName := filepath.Base(s.MountFile())

	// check if we actually need to do anything, i.e. the exact same
	// kernel/core revision got installed again (e.g. firstboot)
	// and we are not in any special boot mode
	m, err := bootloader.GetBootVars("snap_mode", goodBoot)
	if err != nil {
		return fmt.Errorf("cannot set next boot: %s", err)
	}
	if m[goodBoot] == blobName {
		// If we were in anything but default ("") mode before
		// and now switch to the good core/kernel again, make
		// sure to clean the snap_mode here. This also
		// mitigates https://forum.snapcraft.io/t/5253
		if m["snap_mode"] != "" {
			return bootloader.SetBootVars(map[string]string{
				"snap_mode": "",
				nextBoot:    "",
			})
		}
		return nil
	}

	return bootloader.SetBootVars(map[string]string{
		nextBoot:    blobName,
		"snap_mode": "try",
	})
}

// ChangeRequiresReboot returns whether a reboot is required to switch
// to the snap.
func ChangeRequiresReboot(s snap.PlaceInfo, t snap.Type) (bool, error) {
	bootloader, err := Find()
	if err != nil {
		return false, fmt.Errorf("cannot get boot settings: %s", err)
	}

	var nextBoot, goodBoot string
	switch t {
	case snap.TypeKernel:
		nextBoot = "snap_try_kernel"
		goodBoot = "snap_kernel"
	case snap.TypeOS, snap.TypeBase:
		nextBoot = "snap_try_core"
		goodBoot = "snap_core"
	}

	m, err := bootloader.GetBootVars(nextBoot, goodBoot)
	if err != nil {
		return false, fmt.Errorf("cannot get boot variables: %s", err)
	}

	squashfsName := filepath.Base(s.MountFile())
	if m[nextBoot] == squashfsName && m[goodBoot] != m[nextBoot] {
		return true, nil
	}

	return false, nil
}

func extractKernelAssetsToBootDir(bootDir string, s snap.PlaceInfo, snapf snap.Container) error {
	// now do the kernel specific bits
	blobName := filepath.Base(s.MountFile())
	dstDir := filepath.Join(bootDir, blobName)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	dir, err := os.Open(dstDir)
	if err != nil {
		return err
	}
	defer dir.Close()

	for _, src := range []string{"kernel.img", "initrd.img"} {
		if err := snapf.Unpack(src, dstDir); err != nil {
			return err
		}
		if err := dir.Sync(); err != nil {
			return err
		}
	}
	if err := snapf.Unpack("dtbs/*", dstDir); err != nil {
		return err
	}

	return dir.Sync()
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

// InUse checks if the given name/revision is used in the
// boot environment
func InUse(name string, rev snap.Revision) (bool, error) {
	bootloader, err := Find()
	if err != nil {
		return false, fmt.Errorf("cannot get boot settings: %s", err)
	}

	bootVars, err := bootloader.GetBootVars("snap_kernel", "snap_try_kernel", "snap_core", "snap_try_core")
	if err != nil {
		return false, fmt.Errorf("cannot get boot vars: %s", err)
	}

	snapFile := filepath.Base(snap.MountFile(name, rev))
	for _, bootVar := range bootVars {
		if bootVar == snapFile {
			return true, nil
		}
	}

	return false, nil
}

var (
	ErrBootNameAndRevisionNotReady = errors.New("boot revision not yet established")
)

type NameAndRevision struct {
	Name     string
	Revision snap.Revision
}

// GetCurrentBoot returns the currently set name and revision for boot for the given
// type of snap, which can be snap.TypeBase (or snap.TypeOS), or snap.TypeKernel.
// Returns ErrBootNameAndRevisionNotReady if the values are temporarily not established.
func GetCurrentBoot(t snap.Type) (*NameAndRevision, error) {
	var bootVar, errName string
	switch t {
	case snap.TypeKernel:
		bootVar = "snap_kernel"
		errName = "kernel"
	case snap.TypeOS, snap.TypeBase:
		bootVar = "snap_core"
		errName = "base"
	default:
		return nil, fmt.Errorf("internal error: cannot find boot revision for snap type %q", t)
	}

	loader, err := Find()
	if err != nil {
		return nil, fmt.Errorf("cannot get boot settings: %s", err)
	}

	m, err := loader.GetBootVars(bootVar, "snap_mode")
	if err != nil {
		return nil, fmt.Errorf("cannot get boot variables: %s", err)
	}

	if m["snap_mode"] == "trying" {
		return nil, ErrBootNameAndRevisionNotReady
	}

	nameAndRevno, err := nameAndRevnoFromSnap(m[bootVar])
	if err != nil {
		return nil, fmt.Errorf("cannot get name and revision of boot %s: %v", errName, err)
	}

	return nameAndRevno, nil
}

// nameAndRevnoFromSnap grabs the snap name and revision from the
// value of a boot variable. E.g., foo_2.snap -> name "foo", revno 2
func nameAndRevnoFromSnap(sn string) (*NameAndRevision, error) {
	if sn == "" {
		return nil, fmt.Errorf("boot variable unset")
	}
	idx := strings.IndexByte(sn, '_')
	if idx < 1 {
		return nil, fmt.Errorf("input %q has invalid format (not enough '_')", sn)
	}
	name := sn[:idx]
	revnoNSuffix := sn[idx+1:]
	rev, err := snap.ParseRevision(strings.TrimSuffix(revnoNSuffix, ".snap"))
	if err != nil {
		return nil, err
	}
	return &NameAndRevision{Name: name, Revision: rev}, nil
}
