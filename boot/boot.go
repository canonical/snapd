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

// A BootParticipant is a snap that is involved in a device's boot process.
type BootParticipant interface {
	// SetNextBoot will schedule the snap to be used in the next boot. For
	// base snaps it is up to the caller to select the right bootable base
	// (from the model assertion).
	SetNextBoot() error
	// ChangeRequiresReboot returns whether a reboot is required to switch
	// to the snap.
	ChangeRequiresReboot() bool
	// Is this a trivial implementation of the interface?
	IsTrivial() bool
}

// A Kernel exposes functionality that some bootloaders need
type Kernel interface {
	// RemoveKernelAssets removes the unpacked kernel/initrd for the given
	// kernel snap.
	RemoveKernelAssets() error
	// ExtractKernelAssets extracts kernel/initrd/dtb data from the given
	// kernel snap, if required, to a versioned bootloader directory so
	// that the bootloader can use it.
	ExtractKernelAssets(snap.Container) error
	// Is this a trivial implementation of the interface?
	IsTrivial() bool
}

type trivial struct{}

func (trivial) SetNextBoot() error                       { return nil }
func (trivial) ChangeRequiresReboot() bool               { return false }
func (trivial) IsTrivial() bool                              { return true }
func (trivial) RemoveKernelAssets() error                { return nil }
func (trivial) ExtractKernelAssets(snap.Container) error { return nil }

// ensure trivial is a BootParticipant
var _ BootParticipant = trivial{}

// ensure trivial is a Kernel
var _ Kernel = trivial{}

// Model carries information about the model that is relevant to boot.
// Note *asserts.Model implements this, and that's the expected use case.
type Model interface {
	Kernel() string
	Base() string
}

// Lookup figures out what the boot participant is for the given
// arguments, and returns it. If the snap does _not_ participate in
// the boot process, the returned object will be a NOP, so it's safe
// to call anything on it always.
//
// Currently, on classic, nothing is a boot participant (returned will
// always be NOP).
func Lookup(s snap.PlaceInfo, t snap.Type, model Model, onClassic bool) BootParticipant {
	if applicable(s, t, model, onClassic) {
		return &coreBootParticipant{s: s, t: t}
	}
	return trivial{}
}

// LookupKernel checks that the given arguments refer to a kernel snap
// that participates in the boot process, and returns the associated
// Kernel, or a trivial implementation otherwise.
func LookupKernel(s snap.PlaceInfo, t snap.Type, model Model, onClassic bool) Kernel {
	if t == snap.TypeKernel && applicable(s, t, model, onClassic) {
		return &coreKernel{s: s}
	}
	return trivial{}
}

func applicable(s snap.PlaceInfo, t snap.Type, model Model, onClassic bool) bool {
	if onClassic {
		return false
	}
	if t != snap.TypeOS && t != snap.TypeKernel && t != snap.TypeBase {
		// note we don't currently have anything useful to do with gadgets
		return false
	}

	if model != nil {
		switch t {
		case snap.TypeKernel:
			if s.InstanceName() != model.Kernel() {
				// a remodel might leave you in this state
				return false
			}
		case snap.TypeBase, snap.TypeOS:
			base := model.Base()
			if base == "" {
				base = "core"
			}
			if s.InstanceName() != base {
				return false
			}
		}
	}

	return true
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

	bloader, err := bootloader.Find()
	if err != nil {
		return nil, fmt.Errorf("cannot get boot settings: %s", err)
	}

	m, err := bloader.GetBootVars(bootVar, "snap_mode")
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
func MarkBootSuccessful() error {
	bl, err := bootloader.Find()
	if err != nil {
		return fmt.Errorf("cannot mark boot successful: %s", err)
	}
	m, err := bl.GetBootVars("snap_mode", "snap_try_core", "snap_try_kernel")
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
	m["snap_mode"] = ""

	return bl.SetBootVars(m)
}
