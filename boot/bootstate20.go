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
	"fmt"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
)

func newBootState20(typ snap.Type) bootState {
	switch typ {
	case snap.TypeBase:
		return &bootState20Base{}
	case snap.TypeKernel:
		return &bootState20Kernel{}
	default:
		panic(fmt.Sprintf("cannot make a bootState20 for snap type %q", typ))
	}
}

//
// kernel snap methods
//

// bootState20Kernel implements the bootState and bootStateUpdate interfaces for
// kernel snaps on UC20. It is used for setNext() and markSuccessful() - though
// note that for markSuccessful() a different bootStateUpdate implementation is
// returned, see bootState20MarkSuccessful
type bootState20Kernel struct {
	// the bootloader to manipulate kernel assets
	ebl bootloader.ExtractedRunKernelImageBootloader

	// the kernel_status variable currently in the bootloader, initialized by
	// setupBootloader()
	kernelStatus string

	// the kernel_status value to be written in commit()
	commitKernelStatus string

	// the kernel snap that was tried for markSuccessful()
	triedKernelSnap snap.PlaceInfo

	// the kernel snap to try for setNext()
	tryKernelSnap snap.PlaceInfo
}

func (ks20 *bootState20Kernel) loadBootenv() error {
	// don't setup multiple times
	if ks20.ebl != nil {
		return nil
	}
	// find the bootloader and ensure it's an extracted run kernel image
	// bootloader
	bl, err := bootloader.Find("", nil)
	if err != nil {
		return err
	}
	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if !ok {
		return fmt.Errorf("cannot use %s bootloader: does not support extracted run kernel images", bl.Name())
	}

	ks20.ebl = ebl

	// also get the kernel_status
	m, err := ebl.GetBootVars("kernel_status")
	if err != nil {
		return err
	}

	ks20.kernelStatus = m["kernel_status"]

	// the default commit status is the same as the kernel status was before
	ks20.commitKernelStatus = ks20.kernelStatus

	return nil
}

func (ks20 *bootState20Kernel) revisions() (curSnap, trySnap snap.PlaceInfo, tryingStatus string, err error) {
	var bootSn, tryBootSn snap.PlaceInfo
	err = ks20.loadBootenv()
	if err != nil {
		return nil, nil, "", err
	}

	// get the kernel for this bootloader
	bootSn, err = ks20.ebl.Kernel()
	if err != nil {
		return nil, nil, "", fmt.Errorf("cannot identify kernel snap with bootloader %s: %v", ks20.ebl.Name(), err)
	}

	tryKernel, tryKernelExists, err := ks20.ebl.TryKernel()
	if err != nil {
		return nil, nil, "", fmt.Errorf("cannot identify try kernel snap with bootloader %s: %v", ks20.ebl.Name(), err)
	}

	if tryKernelExists {
		tryBootSn = tryKernel
	}

	return bootSn, tryBootSn, ks20.kernelStatus, nil
}

func (ks20 *bootState20Kernel) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with this object to do most of the legwork
	u, sn, err := genericMarkSuccessful(ks20, update)
	if err != nil {
		return nil, err
	}

	// u should always be non-nil if err is nil
	u.triedKernelSnap = sn
	return u, nil
}

func (ks20 *bootState20Kernel) setNext(next snap.PlaceInfo) (rebootRequired bool, u bootStateUpdate, err error) {
	nextStatus, err := genericSetNext(ks20, next)
	if err != nil {
		return false, nil, err
	}
	// if we are setting a snap as a try snap, then we need to reboot
	rebootRequired = false
	if nextStatus == TryStatus {
		ks20.tryKernelSnap = next
		rebootRequired = true
	}
	ks20.commitKernelStatus = nextStatus

	// any state changes done so far are consumed in to commit()

	return rebootRequired, ks20, nil
}

// commit for bootState20Kernel is meant only to be used with setNext(), for
// markSuccessful(), use bootState20MarkSuccessful.
func (ks20 *bootState20Kernel) commit() error {
	// The ordering of this is very important for boot safety/reliability!!!

	// If we are about to try an update, and need to add the try-kernel symlink,
	// we need to do things in this order:
	// 1. Add try-kernel symlink
	// 2. Update kernel_status to "try"
	//
	// This is because if we get rebooted in between 1 and 2, kernel_status
	// is still unset and boot scripts proceeds to boot with the old kernel,
	// effectively ignoring the try-kernel symlink.
	// If we did it in the opposite order however, we would set kernel_status to
	// "try" and then get rebooted before we could create the try-kernel
	// symlink, so the bootloader would try to boot from the non-existent
	// try-kernel symlink and become broken.

	// add the try-kernel symlink
	// trySnap could be nil here if we called setNext on the current kernel
	// snap
	if ks20.tryKernelSnap != nil {
		err := ks20.ebl.EnableTryKernel(ks20.tryKernelSnap)
		if err != nil {
			return err
		}
	}

	// only if the new kernel status is different from what we read should we
	// run SetBootVars()
	if ks20.commitKernelStatus != ks20.kernelStatus {
		m := map[string]string{
			"kernel_status": ks20.commitKernelStatus,
		}

		// set the boot variables
		return ks20.ebl.SetBootVars(m)
	}

	return nil
}

//
// base snap methods
//

// bootState20Kernel implements the bootState and bootStateUpdate interfaces for
// base snaps on UC20. It is used for setNext() and markSuccessful() - though
// note that for markSuccessful() a different bootStateUpdate implementation is
// returned, see bootState20MarkSuccessful
type bootState20Base struct {
	// the modeenv for the base snap, initialized with loadModeenv()
	modeenv *Modeenv

	// the base_status to be written to the modeenv, stored separately to
	// eliminate unnecessary writes to the modeenv when it's already in the
	// state we want it in
	commitBaseStatus string

	// the base snap that was tried for markSuccessful()
	triedBaseSnap snap.PlaceInfo

	// the base snap to try for setNext()
	tryBaseSnap snap.PlaceInfo
}

func (bs20 *bootState20Base) loadModeenv() error {
	// don't read modeenv multiple times
	if bs20.modeenv != nil {
		return nil
	}
	modeenv, err := ReadModeenv(dirs.GlobalRootDir)
	if err != nil {
		return fmt.Errorf("cannot get snap revision: unable to read modeenv: %v", err)
	}
	bs20.modeenv = modeenv

	// default commit status is the current status
	bs20.commitBaseStatus = bs20.modeenv.BaseStatus

	return nil
}

// revisions returns the current boot snap and optional try boot snap for the
// type specified in bsgeneric.
func (bs20 *bootState20Base) revisions() (curSnap, trySnap snap.PlaceInfo, tryingStatus string, err error) {
	var bootSn, tryBootSn snap.PlaceInfo
	err = bs20.loadModeenv()
	if err != nil {
		return nil, nil, "", err
	}

	if bs20.modeenv.Base == "" {
		return nil, nil, "", fmt.Errorf("cannot get snap revision: modeenv base boot variable is empty")
	}

	bootSn, err = snap.ParsePlaceInfoFromSnapFileName(bs20.modeenv.Base)
	if err != nil {
		return nil, nil, "", fmt.Errorf("cannot get snap revision: modeenv base boot variable is invalid: %v", err)
	}

	if bs20.modeenv.BaseStatus == TryingStatus && bs20.modeenv.TryBase != "" {
		tryBootSn, err = snap.ParsePlaceInfoFromSnapFileName(bs20.modeenv.TryBase)
		if err != nil {
			return nil, nil, "", fmt.Errorf("cannot get snap revision: modeenv try base boot variable is invalid: %v", err)
		}
	}

	return bootSn, tryBootSn, bs20.modeenv.BaseStatus, nil
}

func (bs20 *bootState20Base) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with this object to do most of the legwork
	u, sn, err := genericMarkSuccessful(bs20, update)
	if err != nil {
		return nil, err
	}

	// u should always be non-nil if err is nil
	u.triedBaseSnap = sn
	return u, nil
}

func (bs20 *bootState20Base) setNext(next snap.PlaceInfo) (rebootRequired bool, u bootStateUpdate, err error) {
	nextStatus, err := genericSetNext(bs20, next)
	if err != nil {
		return false, nil, err
	}
	// if we are setting a snap as a try snap, then we need to reboot
	rebootRequired = false
	if nextStatus == TryStatus {
		bs20.tryBaseSnap = next
		rebootRequired = true
	}
	bs20.commitBaseStatus = nextStatus

	// any state changes done so far are consumed in to commit()

	return rebootRequired, bs20, nil
}

// commit for bootState20Base is meant only to be used with setNext(), for
// markSuccessful(), use bootState20MarkSuccessful.
func (bs20 *bootState20Base) commit() error {
	// the ordering here is less important than the kernel commit(), since the
	// only operation that has side-effects is writing the modeenv at the end,
	// and that uses an atomic file writing operation, so it's not a concern if
	// we get rebooted during this snippet like it is with the kernel snap above

	// the TryBase is the snap we are trying - note this could be nil if we
	// are calling setNext on the same snap that is current
	changed := false
	if bs20.tryBaseSnap != nil {
		tryBase := bs20.tryBaseSnap.Filename()
		if tryBase != bs20.modeenv.TryBase {
			bs20.modeenv.TryBase = tryBase
			changed = true
		}
	}

	if bs20.commitBaseStatus != bs20.modeenv.BaseStatus {
		bs20.modeenv.BaseStatus = bs20.commitBaseStatus
		changed = true
	}

	// only write the modeenv if we actually changed it
	if changed {
		return bs20.modeenv.Write("")
	}
	return nil
}

//
// generic methods
//

// genericSetNext implements the generic logic for setting up a snap to be tried
// for boot and works for both kernel and base snaps (though not
// simultaneously).
func genericSetNext(b bootState, next snap.PlaceInfo) (setStatus string, err error) {
	// get the current snap
	current, _, _, err := b.revisions()
	if err != nil {
		return "", err
	}

	// check if the next snap is really the same as the current snap, in which
	// case we either do nothing or just clear the status (and not reboot)
	if current.SnapName() == next.SnapName() && next.SnapRevision() == current.SnapRevision() {
		// if we are setting the next snap as the current snap, don't need to
		// change any snaps, just reset the status to default
		return DefaultStatus, nil
	}

	// by default we will set the status as "try" to prepare for an update,
	// which also by default will require a reboot
	return TryStatus, nil
}

// bootState20MarkSuccessful is like bootState20Base and
// bootState20Kernel, but is the combination of both of those things so we can
// mark both snaps successful in one go
type bootState20MarkSuccessful struct {
	// base snap
	bootState20Base
	// kernel snap
	bootState20Kernel
}

// threadBootState20MarkSuccessful is a helper method that will either create a
// new bootState20MarkSuccessful for the given type, or it will add to the
// provided bootStateUpdate
func threadBootState20MarkSuccessful(update bootStateUpdate) (*bootState20MarkSuccessful, error) {
	var bsmark *bootState20MarkSuccessful

	// try to extract bsmark out of update
	var ok bool
	if update != nil {
		if bsmark, ok = update.(*bootState20MarkSuccessful); !ok {
			return nil, fmt.Errorf("internal error, cannot thread %T with update for UC20", update)
		}
	}

	if bsmark == nil {
		bsmark = &bootState20MarkSuccessful{}
	}

	// initialize both types in case we need to mark both
	err := bsmark.loadBootenv()
	if err != nil {
		return nil, err
	}
	err = bsmark.loadModeenv()
	if err != nil {
		return nil, err
	}

	return bsmark, nil
}

// genericMarkSuccessful sets the necessary boot variables, etc. to mark the
// given boot snap as successful and a valid rollback target. If err is nil,
// then the first return value is guaranteed to always be non-nil.
func genericMarkSuccessful(b bootState, update bootStateUpdate) (bsmark *bootState20MarkSuccessful, trySnap snap.PlaceInfo, err error) {
	// create a new object or combine the existing one with this type
	bsmark, err = threadBootState20MarkSuccessful(update)
	if err != nil {
		return nil, nil, err
	}

	// get the try snap and the current status
	_, trySnap, status, err := b.revisions()
	if err != nil {
		return nil, nil, err
	}

	// kernel_status and base_status go from "" -> "try" (set by snapd), to
	// "try" -> "trying" (set by the boot script)
	// so if we are not in "trying" mode, nothing to do here
	if status != TryingStatus {
		return bsmark, nil, nil
	}

	return bsmark, trySnap, nil
}

// commit will persistently write out the boot variables, etc. needed to mark
// the snaps saved in bsmark as successful boot targets/combinations.
// note that this makes the assumption that markSuccessful() has been called for
// both the base and kernel snaps here, if that assumption is not true anymore,
// this could end up auto-cleaning status variables for something it shouldn't
// be.
func (bsmark *bootState20MarkSuccessful) commit() error {
	// kernel snap first, slightly higher priority

	// the ordering here is very important for boot reliability!

	// If we have successfully just booted from a try-kernel and are
	// marking it successful (this implies that snap_kernel=="trying" as set
	// by the boot script), we need to do the following in order (since we
	// have the added complexity of moving the kernel symlink):
	// 1. Update kernel_status to ""
	// 2. Move kernel symlink to point to the new try kernel
	// 3. Remove try-kernel symlink
	//
	// If we got rebooted after step 1, then the bootloader is booting the wrong
	// kernel, but is at least booting a known good kernel and snapd in
	// user-space would be able to figure out the inconsistency.
	// If we got rebooted after step 2, the bootloader would boot from the new
	// try-kernel which is okay because we were in the middle of committing
	// that new kernel as good and all that's left is for snapd to cleanup
	// the left-over try-kernel symlink.
	//
	// If instead we had moved the kernel symlink first to point to the new try
	// kernel, and got rebooted before the kernel_status was updated, we would
	// have kernel_status="trying" which would cause the bootloader to think
	// the boot failed, and revert to booting using the kernel symlink, but that
	// now points to the new kernel we were trying and we did not successfully
	// boot from that kernel to know we should trust it.
	// The try-kernel symlink removal should happen last because it will not
	// affect anything, except that if it was removed before updating
	// kernel_status to "", the bootloader will think that the try kernel failed
	// to boot and fall back to booting the old kernel which is safe.

	// always set the kernel_status to default "" when marking successful, but
	// only call SetBootVars if needed
	// this has the useful side-effect of cleaning up if we happen to have
	// kernel_status = "trying" but don't have a try-kernel set
	if bsmark.commitKernelStatus != DefaultStatus {
		m := map[string]string{
			"kernel_status": DefaultStatus,
		}

		// set the boot variables
		err := bsmark.ebl.SetBootVars(m)
		if err != nil {
			return err
		}
	}

	if bsmark.triedKernelSnap != nil {
		// enable the kernel we tried
		err := bsmark.ebl.EnableKernel(bsmark.triedKernelSnap)
		if err != nil {
			return err
		}

		// finally disable the try kernel symlink
		err = bsmark.ebl.DisableTryKernel()
		if err != nil {
			return err
		}
	}

	// base snap next
	// the ordering here is less important, since the only operation that
	// has side-effects is writing the modeenv at the end, and that uses an
	// atomic file writing operation, so it's not a concern if we get
	// rebooted during this snippet like it is with the kernel snap above

	baseChanged := false

	// always clear the base_status when marking successful, this has the useful
	// side-effect of cleaning up if we have base_status=trying but no try_base
	// set
	if bsmark.modeenv.BaseStatus != DefaultStatus {
		baseChanged = true
		bsmark.modeenv.BaseStatus = DefaultStatus
	}

	if bsmark.triedBaseSnap != nil {
		// set the new base as the tried base snap
		tryBase := bsmark.triedBaseSnap.Filename()
		if bsmark.modeenv.Base != tryBase {
			bsmark.modeenv.Base = tryBase
			baseChanged = true
		}

		// clear the TryBase
		if bsmark.modeenv.TryBase != "" {
			bsmark.modeenv.TryBase = ""
			baseChanged = true
		}
	}

	// write the modeenv
	if baseChanged {
		return bsmark.modeenv.Write("")
	}

	return nil
}
