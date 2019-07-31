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

package boot

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// RemoveKernelAssets removes the unpacked kernel/initrd for the given
// kernel snap.
func RemoveKernelAssets(s snap.PlaceInfo) error {
	bootloader, err := bootloader.Find()
	if err != nil {
		return fmt.Errorf("no not remove kernel assets: %s", err)
	}

	// ask bootloader to remove the kernel assets if needed
	return bootloader.RemoveKernelAssets(s)
}

// ExtractKernelAssets extracts kernel/initrd/dtb data from the given
// kernel snap, if required, to a versioned bootloader directory so
// that the bootloader can use it.
func ExtractKernelAssets(s *snap.Info, snapf snap.Container) error {
	if s.GetType() != snap.TypeKernel {
		return fmt.Errorf("cannot extract kernel assets from snap type %q", s.GetType())
	}

	bootloader, err := bootloader.Find()
	if err != nil {
		return fmt.Errorf("cannot extract kernel assets: %s", err)
	}

	// ask bootloader to extract the kernel assets if needed
	return bootloader.ExtractKernelAssets(s, snapf)
}

// SetNextBoot will schedule the given OS or base or kernel snap to be
// used in the next boot. For base snaps it up to the caller to select
// the right bootable base (from the model assertion).
func SetNextBoot(s *snap.Info) error {
	if release.OnClassic {
		return fmt.Errorf("cannot set next boot on classic systems")
	}

	if s.GetType() != snap.TypeOS && s.GetType() != snap.TypeKernel && s.GetType() != snap.TypeBase {
		return fmt.Errorf("cannot set next boot to snap %q with type %q", s.SnapName(), s.GetType())
	}

	bootloader, err := bootloader.Find()
	if err != nil {
		return fmt.Errorf("cannot set next boot: %s", err)
	}

	var nextBoot, goodBoot string
	switch s.GetType() {
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
		return err
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
// to the given OS, base or kernel snap.
func ChangeRequiresReboot(s *snap.Info) bool {
	if s.GetType() != snap.TypeKernel && s.GetType() != snap.TypeOS && s.GetType() != snap.TypeBase {
		return false
	}

	bootloader, err := bootloader.Find()
	if err != nil {
		logger.Noticef("cannot get boot settings: %s", err)
		return false
	}

	var nextBoot, goodBoot string
	switch s.GetType() {
	case snap.TypeKernel:
		nextBoot = "snap_try_kernel"
		goodBoot = "snap_kernel"
	case snap.TypeOS, snap.TypeBase:
		nextBoot = "snap_try_core"
		goodBoot = "snap_core"
	}

	m, err := bootloader.GetBootVars(nextBoot, goodBoot)
	if err != nil {
		logger.Noticef("cannot get boot variables: %s", err)
		return false
	}

	squashfsName := filepath.Base(s.MountFile())
	if m[nextBoot] == squashfsName && m[goodBoot] != m[nextBoot] {
		return true
	}

	return false
}

// InUse checks if the given name/revision is used in the
// boot environment
func InUse(name string, rev snap.Revision) bool {
	bootloader, err := bootloader.Find()
	if err != nil {
		logger.Noticef("cannot get boot settings: %s", err)
		return false
	}

	bootVars, err := bootloader.GetBootVars("snap_kernel", "snap_try_kernel", "snap_core", "snap_try_core")
	if err != nil {
		logger.Noticef("cannot get boot vars: %s", err)
		return false
	}

	snapFile := filepath.Base(snap.MountFile(name, rev))
	for _, bootVar := range bootVars {
		if bootVar == snapFile {
			return true
		}
	}

	return false
}

var (
	ErrBootNameAndRevisionAgain = errors.New("boot revision not yet established")
)

type NameAndRevision struct {
	Name     string
	Revision snap.Revision
}

// GetCurrentBoot returns the currently set name and revision for boot for the given
// type of snap, which can be snap.TypeBase (or snap.TypeOS), or snap.TypeKernel.
// Returns ErrBootNameAndRevisionAgain if the values are temporarily not established.
func GetCurrentBoot(t snap.Type) (*NameAndRevision, error) {
	var bootVar, errName string
	switch t {
	case snap.TypeKernel:
		bootVar = "snap_kernel"
		errName = "kernel"
	case snap.TypeOS, snap.TypeBase:
		bootVar = "snap_core"
		errName = "snap"
	default:
		return nil, fmt.Errorf("internal error: cannot find boot revision for snap type %q", t)
	}

	loader, err := bootloader.Find()
	if err != nil {
		return nil, fmt.Errorf("cannot get boot settings: %s", err)
	}

	m, err := loader.GetBootVars(bootVar, "snap_mode")
	if err != nil {
		return nil, fmt.Errorf("cannot get boot variables: %s", err)
	}

	if m["snap_mode"] == "trying" {
		return nil, ErrBootNameAndRevisionAgain
	}

	nameAndRevno, err := nameAndRevnoFromSnap(m[bootVar])
	if err != nil {
		return nil, fmt.Errorf("cannot get name and revision of boot %s: %v", errName, err)
	}

	return nameAndRevno, nil
}

func nameAndRevnoFromSnap(sn string) (*NameAndRevision, error) {
	if sn == "" {
		return nil, fmt.Errorf("unset")
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
