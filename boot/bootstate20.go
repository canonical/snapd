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
	"path/filepath"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

// bootSnap is an interface that is used to make the generic methods, setNext()
// and markSuccessful() easier to implement
type bootSnap interface {
	bootState

	status() (string, error)
	setTrySnap(snap.PlaceInfo)
	setStatus(string)
}

func newBootState20Generic(typ snap.Type) bootState {
	switch typ {
	case snap.TypeBase:
		return &bootState20Base{}
	case snap.TypeKernel:
		return &bootState20Kernel{}
	default:
		// TODO:UC20: this should be handled by bootStateFor, but is there a
		// better thing to do here?
		return nil
	}
}

//
// kernel snap methods
//

// bootState20Kernel is stand-alone and implements the bootState,
// bootStateUpdate, and bootSnap interfaces
type bootState20Kernel struct {
	// the bootloader to manipulate kernel assets
	ebl bootloader.ExtractedRunKernelImageBootloader

	// the kernel_status variable, initialized by setupBootloader()
	kernelStatus string

	// the kernel snap that was tried for markSuccessful()
	triedKernelSnap snap.PlaceInfo

	// the kernel snap to try for setNext()
	tryKernelSnap snap.PlaceInfo
}

func (ks20 *bootState20Kernel) loadBootloader() error {
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

	return nil
}

func (ks20 *bootState20Kernel) status() (string, error) {
	err := ks20.loadBootloader()
	if err != nil {
		return "", err
	}

	return ks20.kernelStatus, nil
}

func (ks20 *bootState20Kernel) setTrySnap(sn snap.PlaceInfo) {
	ks20.tryKernelSnap = sn
}

func (ks20 *bootState20Kernel) setStatus(status string) {
	ks20.kernelStatus = status
}

func (ks20 *bootState20Kernel) revisions() (snap.PlaceInfo, snap.PlaceInfo, bool, error) {
	var bootSn, tryBootSn snap.PlaceInfo
	var trying bool
	err := ks20.loadBootloader()
	if err != nil {
		return nil, nil, false, err
	}

	// get the kernel for this bootloader
	bootSn, err = ks20.ebl.Kernel()
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot identify kernel snap with bootloader %s: %v", ks20.ebl.Name(), err)
	}

	tryKernel, tryKernelExists, err := ks20.ebl.TryKernel()
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot identify try kernel snap with bootloader %s: %v", ks20.ebl.Name(), err)
	}

	if tryKernelExists {
		tryBootSn = tryKernel
	}

	m, err := ks20.ebl.GetBootVars("kernel_status")
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot read boot variables with bootloader %s: %v", ks20.ebl.Name(), err)
	}

	trying = (m["kernel_status"] == "trying")

	return bootSn, tryBootSn, trying, nil
}

func (ks20 *bootState20Kernel) setNext(next snap.PlaceInfo) (bool, bootStateUpdate, error) {
	err := ks20.loadBootloader()
	if err != nil {
		return false, nil, err
	}

	r, err := genericSetNext(ks20, next)
	if err != nil {
		return false, nil, err
	}

	return r, ks20, nil
}

func (ks20 *bootState20Kernel) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with to do most of the legwork
	u, sn, err := genericMarkSuccessful(
		ks20,
		update,
	)
	if err != nil {
		return nil, err
	}

	// u should always be non-nil if err is nil
	u.triedKernelSnap = sn
	return u, nil
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

	m := map[string]string{
		"kernel_status": ks20.kernelStatus,
	}

	// set the boot variables
	return ks20.ebl.SetBootVars(m)
}

//
// base snap methods
//

// bootState20Base is meant to be embedded in the bootStateSetNext and
// bootStateMarkSuccessful structs, handling reading/initializing the modeenv.
type bootState20Base struct {
	// the modeenv for the base snap, initialized with loadModeenv()
	modeenv *Modeenv

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

	return nil
}

func (bs20 *bootState20Base) status() (string, error) {
	err := bs20.loadModeenv()
	if err != nil {
		return "", err
	}

	return bs20.modeenv.BaseStatus, nil
}

func (bs20 *bootState20Base) setTrySnap(sn snap.PlaceInfo) {
	bs20.tryBaseSnap = sn
}

func (bs20 *bootState20Base) setStatus(status string) {
	bs20.modeenv.BaseStatus = status
}

// revisions returns the current boot snap and optional try boot snap for the
// type specified in bsgeneric.
func (bs20 *bootState20Base) revisions() (snap.PlaceInfo, snap.PlaceInfo, bool, error) {
	var bootSn, tryBootSn snap.PlaceInfo
	var trying bool
	bs := &bootState20Base{}
	err := bs.loadModeenv()
	if err != nil {
		return nil, nil, false, err
	}

	if bs.modeenv.Base == "" {
		return nil, nil, false, fmt.Errorf("cannot get snap revision: modeenv base boot variable is empty")
	}

	bootSn, err = snap.ParsePlaceInfoFromSnapFileName(bs.modeenv.Base)
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot get snap revision: modeenv base boot variable is invalid: %v", err)
	}

	if bs.modeenv.BaseStatus == "trying" {
		if bs.modeenv.TryBase == "" {
			// this is an error condition, log some info about the current
			// Base setting in the modeenv as well
			logger.Noticef("boot modeenv does is in trying state, but does not have a TryBase, Base is %s", bs.modeenv.Base)
			return nil, nil, false, fmt.Errorf("cannot get try snap revision: modeenv boot variable base_status is set, but try_base is empty")
		}

		tryBootSn, err = snap.ParsePlaceInfoFromSnapFileName(bs.modeenv.TryBase)
		if err != nil {
			return nil, nil, false, fmt.Errorf("cannot get snap revision: modeenv try base boot variable is invalid: %v", err)
		}
		trying = true
	}

	return bootSn, tryBootSn, trying, nil
}

func (bs20 *bootState20Base) setNext(next snap.PlaceInfo) (bool, bootStateUpdate, error) {
	err := bs20.loadModeenv()
	if err != nil {
		return false, nil, err
	}

	r, err := genericSetNext(bs20, next)
	if err != nil {
		return false, nil, err
	}

	return r, bs20, nil
}

func (bs20 *bootState20Base) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with to do most of the legwork
	u, sn, err := genericMarkSuccessful(bs20, update)
	if err != nil {
		return nil, err
	}

	// u should always be non-nil if err is nil
	u.triedBaseSnap = sn
	return u, nil
}

func (bs20 *bootState20Base) commit() error {
	// the ordering here is less important than the kernel commit(), since the
	// only operation that has side-effects is writing the modeenv at the end,
	// and that uses an atomic file writing operation, so it's not a concern if
	// we get rebooted during this snippet like it is with the kernel snap above

	// the TryBase is the snap we are trying - note this could be nil if we
	// are calling setNext on the same snap that is current
	if bs20.tryBaseSnap != nil {
		bs20.modeenv.TryBase = filepath.Base(bs20.tryBaseSnap.MountFile())
	}

	return bs20.modeenv.Write("")
}

//
// generic methods
//

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
func threadBootState20MarkSuccessful(bsmark *bootState20MarkSuccessful) (*bootState20MarkSuccessful, error) {
	if bsmark == nil {
		bsmark = &bootState20MarkSuccessful{}
	}

	// initialize both types in case we need to mark both
	err := bsmark.loadBootloader()
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
func genericMarkSuccessful(b bootSnap, update bootStateUpdate) (*bootState20MarkSuccessful, snap.PlaceInfo, error) {
	// either combine the provided bootStateUpdate with a new one for this type
	// or create a new one for this type
	var bsmark *bootState20MarkSuccessful
	var err error
	var ok bool
	if update != nil {
		if bsmark, ok = update.(*bootState20MarkSuccessful); !ok {
			return nil, nil, fmt.Errorf("internal error, cannot thread %T with update for UC20", update)
		}
	}

	// create a new object or combine the existing one with this type
	bsmark, err = threadBootState20MarkSuccessful(bsmark)
	if err != nil {
		return nil, nil, err
	}

	// kernel_status and base_status go from "" -> "try" (set by snapd), to
	// "try" -> "trying" (set by the boot script)
	// so if we are not in "trying" mode, nothing to do here
	status, err := b.status()
	if err != nil {
		return nil, nil, err
	}
	if status != "trying" {
		return bsmark, nil, nil
	}

	// get the try snap
	_, trySnap, exists, err := b.revisions()
	if err != nil {
		return nil, nil, err
	}
	// it should be impossible for trying to not exist, because we are in trying
	// mode, but just to be double extra sure
	if !exists {
		return nil, nil, fmt.Errorf("cannot mark successful: boot variable is trying, but there is no try snap")
	}

	return bsmark, trySnap, nil
}

// commit will persistently write out the boot variables, etc. needed to mark
// the snaps saved in bsmark as successful boot targets/combinations.
func (bsmark *bootState20MarkSuccessful) commit() error {
	// kernel snap first, slightly higher priority
	if bsmark.triedKernelSnap != nil {
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

		// clear the kernel_status boot var first
		err := bsmark.ebl.SetBootVars(map[string]string{"kernel_status": ""})
		if err != nil {
			return err
		}

		// enable the kernel we tried
		err = bsmark.ebl.EnableKernel(bsmark.triedKernelSnap)
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
	if bsmark.triedBaseSnap != nil {
		// the ordering here is less important, since the only operation that
		// has side-effects is writing the modeenv at the end, and that uses an
		// atomic file writing operation, so it's not a concern if we get
		// rebooted during this snippet like it is with the kernel snap above

		// clear the status
		bsmark.modeenv.BaseStatus = ""
		// set the new base as the tried base snap
		bsmark.modeenv.Base = filepath.Base(bsmark.triedBaseSnap.MountFile())
		// clear the TryBase
		bsmark.modeenv.TryBase = ""
		// write the modeenv
		err := bsmark.modeenv.Write("")
		if err != nil {
			return err
		}
	}

	return nil
}

// genericSetNext implements the generic logic for setting up a snap to be tried
// for boot and works for both kernel and base snaps (though not
// simultaneously).
func genericSetNext(b bootSnap, next snap.PlaceInfo) (rebootRequired bool, err error) {
	// get the current snap
	current, _, _, err := b.revisions()
	if err != nil {
		return false, err
	}

	// by default we will set the status as "try" to prepare for an update,
	// which also by default will require a reboot
	status := "try"
	rebootRequired = true

	// check if the next snap is really the same as the current snap, in which
	// case we either do nothing or just clear the status (and not reboot)
	if current.SnapName() == next.SnapName() && next.SnapRevision() == current.SnapRevision() {
		// If we were in anything but default ("") mode before
		// and switched to the good core/kernel again, make
		// sure to clean the kernel_status here. This also
		// mitigates https://forum.snapcraft.io/t/5253
		currentStatus, err := b.status()
		if err != nil {
			return false, err
		}
		if currentStatus == "" {
			// already clean
			return false, nil
		}

		// clean
		status = ""
		rebootRequired = false
	} else {
		// save the snap for commit() to enable
		b.setTrySnap(next)
	}

	// set the status to be saved in commit()
	b.setStatus(status)

	return rebootRequired, nil
}
