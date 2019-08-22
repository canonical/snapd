// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
)

// A BootParticipant is a snap that is involved in a device's boot
// process.
type BootParticipant interface {
	// SetNextBoot will schedule the snap to be used in the next boot. For
	// base snaps it is up to the caller to select the right bootable base
	// (from the model assertion).
	SetNextBoot() error
	// ChangeRequiresReboot returns whether a reboot is required to switch
	// to the snap.
	ChangeRequiresReboot() bool
}

// A Kernel is a snap.TypeKernel BootParticipant
type Kernel interface {
	BootParticipant
	// RemoveKernelAssets removes the unpacked kernel/initrd for the given
	// kernel snap.
	RemoveKernelAssets() error
	// ExtractKernelAssets extracts kernel/initrd/dtb data from the given
	// kernel snap, if required, to a versioned bootloader directory so
	// that the bootloader can use it.
	ExtractKernelAssets(snap.Container) error
}

type Model interface {
	Kernel() string
	Base() string
}

// Lookup figures out what the boot participant is for the given arguments, and
// returns it. The second return value indicates whether no boot participant
// exists, and if false the first return value will be nil.
//
// Currently, on classic, nothing is a boot participant (applicable
// will always be false).
func Lookup(s snap.PlaceInfo, t snap.Type, model Model, onClassic bool) (bp BootParticipant, applicable bool) {
	if onClassic {
		return nil, false
	}
	if t != snap.TypeOS && t != snap.TypeKernel && t != snap.TypeBase {
		return nil, false
	}

	if model != nil {
		switch t {
		case snap.TypeKernel:
			if s.InstanceName() != model.Kernel() {
				return nil, false
			}
		case snap.TypeBase, snap.TypeOS:
			base := model.Base()
			if base == "" {
				base = "core"
			}
			if s.InstanceName() != base {
				return nil, false
			}
		}
	}

	cbp := &coreBootParticipant{s: s, t: t}
	bp = cbp
	if t == snap.TypeKernel {
		bp = &coreKernel{cbp}
	}

	return bp, true
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
		errName = "base"
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
