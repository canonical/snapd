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

type bootState20Base struct{}

func (s20 *bootState20Base) revisions() (theSnap, theTrySnap snap.PlaceInfo, trying bool, err error) {
	// TODO:UC20: implement support for trying with base_status and try_base
	modeenv, err := ReadModeenv(dirs.GlobalRootDir)
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot get snap revision: unable to read modeenv: %v", err)
	}

	if modeenv.Base == "" {
		return nil, nil, false, fmt.Errorf("cannot get snap revision: modeenv base boot variable is empty")
	}

	sn, err := snap.ParsePlaceInfoFromSnapFileName(modeenv.Base)
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot get snap revision: modeenv base boot variable is invalid: %v", err)
	}

	return sn, nil, false, nil
}

// TODO:UC20: implement this
func (s20 *bootState20Base) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	return update, nil
}

// TODO:UC20: implement this with modeenv
func (s20 *bootState20Base) setNext(nextKernel snap.PlaceInfo) (rebootRequired bool, u bootStateUpdate, err error) {
	return false, nil, nil
}

type bootState20Kernel struct{}

func newBootState20(typ snap.Type) bootState {
	switch typ {
	case snap.TypeKernel:
		return &bootState20Kernel{}
	case snap.TypeBase:
		return &bootState20Base{}
	default:
		panic("unsupported snap type update")
	}
}

func (s20 *bootState20Kernel) revisions() (theSnap, theTrySnap snap.PlaceInfo, trying bool, err error) {
	var bootSnap, tryBootSnap snap.PlaceInfo

	// the trying snap is the one given by the try-kernel.efi symlink, while
	// the known-good snap is the one given by the kernel.efi symlink
	// we can determine if we are trying the try snap by the presence of
	// 1. a try-kernel.efi symlink
	// 2. kernel_status == trying

	bl, err := bootloader.Find("", nil)
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot find any bootloader: %v", err)
	}

	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if !ok {
		return nil, nil, false, fmt.Errorf("cannot use %s bootloader: does not support extracted run kernel images", ebl.Name())
	}

	// get the kernel for this bootloader
	bootSnap, err = ebl.Kernel()
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot locate kernel snap with bootloader %s: %v", ebl.Name(), err)
	}

	tryKernel, tryKernelExists, err := ebl.TryKernel()
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot locate try kernel snap with bootloader %s: %v", ebl.Name(), err)
	}

	if tryKernelExists {
		tryBootSnap = tryKernel
	}

	m, err := bl.GetBootVars("kernel_status")
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot read boot variables with bootloader %s: %v", ebl.Name(), err)
	}

	trying = (m["kernel_status"] == "trying")

	return bootSnap, tryBootSnap, trying, nil
}

func (s20 *bootState20Kernel) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	var u20 *bootStateUpdate20
	var err error
	u20, err = newBootStateUpdate20(update, snap.TypeKernel)
	if err != nil {
		return nil, err
	}

	// kernel_status goes from "" -> "try" -> "trying" -> ""
	// so if we are not in "trying" mode, nothing to do here
	if u20.env["kernel_status"] != "trying" {
		return u20, nil
	}

	// if kernel_status is trying, then we need to figure out what kernel we did
	// boot from
	sn, exists, err := u20.ebl.TryKernel()
	if err != nil {
		return nil, err
	}

	if !exists {
		// somehow we booted with kernel_status=trying, but no try-kernel
		// symlink :-/
		return nil, fmt.Errorf("cannot mark boot successful: kernel_status is \"trying\", but no try-kernel enabled")
	}

	// set the snap we tried to the snap from the try-kernel symlink
	u20.snapTried = sn

	// update the boot var
	u20.toCommit["kernel_status"] = ""

	return u20, nil
}

func (s20 *bootState20Kernel) setNext(nextKernel snap.PlaceInfo) (rebootRequired bool, u bootStateUpdate, err error) {
	u20, err := newBootStateUpdate20(nil, snap.TypeKernel)
	if err != nil {
		return false, nil, err
	}

	currentKernel, err := u20.ebl.Kernel()
	if err != nil {
		return false, nil, err
	}

	kernelStatus := "try"
	rebootRequired = true
	// check if the next kernel snap is the same as the current kernel snap,
	// in which case we either don't need to do anything or we just need to
	// clear the status and not reboot
	if nextKernel.SnapName() == currentKernel.SnapName() && nextKernel.SnapRevision() == currentKernel.SnapRevision() {
		// If we were in anything but default ("") mode before
		// and switched to the good core/kernel again, make
		// sure to clean the kernel_status here. This also
		// mitigates https://forum.snapcraft.io/t/5253
		if u20.env["kernel_status"] == "" {
			// already clean
			return false, nil, nil
		}

		// clean
		kernelStatus = ""
		rebootRequired = false
	} else {
		// need to enable the symlink
		u20.snapToTry = nextKernel
	}

	u20.toCommit["kernel_status"] = kernelStatus

	return rebootRequired, u20, nil
}

type bootStateUpdate20 struct {
	typ       snap.Type
	ebl       bootloader.ExtractedRunKernelImageBootloader
	env       map[string]string
	toCommit  map[string]string
	snapToTry snap.PlaceInfo
	snapTried snap.PlaceInfo
}

func newBootStateUpdate20(u bootStateUpdate, typ snap.Type) (*bootStateUpdate20, error) {
	// TODO:UC20: can a base snap update be chained with a kernel snap update?
	//            because this implementation assumes that it _cannot_, so we
	//            only handle one type per bootStateUpdate20 instance
	// TODO:UC20: handle base_status from modeenv as well
	if u != nil {
		u20, ok := u.(*bootStateUpdate20)
		if !ok {
			return nil, fmt.Errorf("internal error: threading unexpected boot state update with 20: %T", u)
		}
		return u20, nil
	}
	bl, err := bootloader.Find("", nil)
	if err != nil {
		return nil, err
	}
	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if !ok {
		return nil, fmt.Errorf("cannot use %s bootloader: does not support extracted run kernel images", bl.Name())
	}

	m, err := bl.GetBootVars("kernel_status")
	if err != nil {
		return nil, err
	}
	return &bootStateUpdate20{
		ebl:      ebl,
		env:      m,
		toCommit: make(map[string]string),
		typ:      typ,
	}, nil
}

func (u20 *bootStateUpdate20) commit() error {
	// TODO:UC20: handle base_status from modeenv as well
	if len(u20.toCommit) == 0 {
		// nothing to do
		return nil
	}

	// make a copy of the env map, because we specifically need to check
	// later on what the original value of kernel_status was that we entered in
	// with was
	envCopy := make(map[string]string, len(u20.env))
	for k, v := range u20.env {
		envCopy[k] = v
	}
	// The ordering of this is very important for boot safety/reliability!!!

	// If we are about to try an update, and need to add the try-kernel symlink,
	// we need to do things in this order:
	// 1. Add try-kernel symlink
	// 2. Update kernel_status to "try"
	//
	// This is because if we get rebooted in between 1 and 2, kernel_status is
	// still "trying", so the bootloader will interpret this to mean the boot
	// failed and the bootloader should revert back to the "known good" kernel
	// at the kernel symlink. Since all we did was add a try-kernel symlink,
	// this won't break booting from the old kernel symlink.
	// If we did it in the opposite order however, we would set kernel_status to
	// "try" and then get rebooted before we could create the try-kernel
	// symlink, so the bootloader would try to boot from the non-existent
	// try-kernel symlink and become broken.

	// However, if we have successfully just booted from a try-kernel and are
	// marking it successful (this implies that snap_kernel=="trying"), we need
	// to do the following order (since we have the added complexity of moving
	// the kernel symlink):
	// 1. Update kernel_status to ""
	// 2. Move kernel symlink to point to the new try kernel
	// 3. Remove try-kernel symlink
	//
	// If we got rebooted after step 1, then the bootloader is booting the wrong
	// kernel, but is at least booting a known good kernel and snapd in
	// user-space would be able to figure out the inconsistency
	// If we got rebooted after step 2, the bootloader would boot from the new
	// try-kernel which is okay and all that's left is for snapd to cleanup the
	// left-over try-kernel symlink
	// If instead we had moved the kernel symlink first to point to the new try
	// kernel, and got rebooted before the kernel_status was updated, we would
	// have kernel_status="trying" which would cause the bootloader to think
	// the boot failed, and revert to booting using the kernel symlink, but that
	// now points to the new kernel we were trying and we did not successfully
	// boot from that kernel to know we should trust it
	// The try-kernel symlink removal should happen last because it will not
	// affect anything, except that if it was removed before updating
	// kernel_status to "", the bootloader will think that the try kernel failed
	// to boot and fall back to booting the old kernel which is safe

	// enable snaps that need to be enabled
	// * step 1 for about to try an update
	// * NOP for successful boot after an update
	if u20.snapToTry != nil {
		switch u20.typ {
		case snap.TypeKernel:
			err := u20.ebl.EnableTryKernel(u20.snapToTry)
			if err != nil {
				return err
			}
		default:
			panic("base update not ready yet sorry :-(")
		}
	}

	// update the bootloader env variables
	// * step 2 for about to try an update
	// * step 1 for successful boot after an update
	for k, v := range u20.toCommit {
		// TODO:UC20: handle writing base_status in the modeenv as well
		switch k {
		case "kernel_status":
			envCopy[k] = v
		}
	}
	err := u20.ebl.SetBootVars(envCopy)
	if err != nil {
		return err
	}

	// move kernel symlink
	// * step 2 for successful boot after an update
	// * NOP for about to try an update
	if u20.env["kernel_status"] == "trying" && u20.snapTried != nil {
		// we did try a kernel, move the symlink
		err := u20.ebl.EnableKernel(u20.snapTried)
		if err != nil {
			return err
		}

		// remove try kernel symlink
		// * step 3 for successful boot after an update
		// * NOP for about to try an update
		err = u20.ebl.DisableTryKernel()
		if err != nil {
			return err
		}
	}

	return nil
}
