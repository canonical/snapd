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
	"github.com/snapcore/snapd/osutil"
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
// modeenv methods
//

type bootState20Modeenv struct {
	modeenv *Modeenv
}

func (bsm *bootState20Modeenv) loadModeenv() error {
	// don't read modeenv multiple times
	if bsm.modeenv != nil {
		return nil
	}
	modeenv, err := ReadModeenv("")
	if err != nil {
		return fmt.Errorf("cannot get snap revision: unable to read modeenv: %v", err)
	}
	bsm.modeenv = modeenv

	return nil
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

	// the kernel snap that was booted for markSuccessful()
	bootedKernelSnap snap.PlaceInfo

	// the current kernel as indicated by the bootloader
	currentKernel snap.PlaceInfo

	// the kernel snap to try for setNext()
	tryKernelSnap snap.PlaceInfo

	// don't embed this struct - it will conflict with embedding
	// bootState20Modeenv in bootState20Base when both bootState20Base and
	// bootState20Kernel are embedded in bootState20MarkSuccessful
	// also we only need to use it with setNext()
	kModeenv bootState20Modeenv

	blOpts *bootloader.Options
	blDir  string
}

func (ks20 *bootState20Kernel) loadBootenv() error {
	// don't setup multiple times
	if ks20.ebl != nil {
		return nil
	}

	// find the bootloader and ensure it's an extracted run kernel image
	// bootloader
	bl, err := bootloader.Find(ks20.blDir, ks20.blOpts)
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

	// get the current kernel for this bootloader to compare during commit() for
	// markSuccessful() if we booted the current kernel or not
	kernel, err := ks20.ebl.Kernel()
	if err != nil {
		return fmt.Errorf("cannot identify kernel snap with bootloader %s: %v", ks20.ebl.Name(), err)
	}

	ks20.currentKernel = kernel

	return nil
}

func (ks20 *bootState20Kernel) revisions() (curSnap, trySnap snap.PlaceInfo, tryingStatus string, err error) {
	var tryBootSn snap.PlaceInfo
	err = ks20.loadBootenv()
	if err != nil {
		return nil, nil, "", err
	}

	tryKernel, err := ks20.ebl.TryKernel()
	// if err is ErrNoTryKernelRef, then we will just return nil as the trySnap
	if err != nil && err != bootloader.ErrNoTryKernelRef {
		return ks20.currentKernel, nil, "", trySnapError(fmt.Sprintf("cannot identify try kernel snap with bootloader %s: %v", ks20.ebl.Name(), err))
	}

	if err == nil {
		tryBootSn = tryKernel
	}

	return ks20.currentKernel, tryBootSn, ks20.kernelStatus, nil
}

func (ks20 *bootState20Kernel) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with this object to do most of the legwork
	u, sn, err := chooseBootSnapToMarkSuccessful(ks20, update)
	if err != nil {
		return nil, err
	}

	// u should always be non-nil if err is nil
	u.bootedKernelSnap = sn
	return u, nil
}

func (ks20 *bootState20Kernel) setNext(next snap.PlaceInfo) (rebootRequired bool, u bootStateUpdate, err error) {
	// commit() for setNext() also needs to add to the kernels in modeenv
	err = ks20.kModeenv.loadModeenv()
	if err != nil {
		return false, nil, err
	}

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
	// 1. Add the kernel snap to the modeenv
	// 2. Create try-kernel symlink
	// 3. Update kernel_status to "try"
	//
	// This is because if we get rebooted in before 3, kernel_status is still
	// unset and boot scripts proceeds to boot with the old kernel, effectively
	// ignoring the try-kernel symlink.
	// If we did it in the opposite order however, we would set kernel_status to
	// "try" and then get rebooted before we could create the try-kernel
	// symlink, so the bootloader would try to boot from the non-existent
	// try-kernel symlink and become broken.
	//
	// Adding the kernel snap to the modeenv's list of trusted kernel snaps can
	// effectively happen any time before we update the kernel_status to "try"
	// for the same reasoning as for creating the try-kernel symlink. Putting it
	// first is currently a purely aesthetic choice.

	// add the kernel to the modeenv and add the try-kernel symlink
	// tryKernelSnap could be nil here if we called setNext on the current
	// kernel snap
	if ks20.tryKernelSnap != nil {
		// add the kernel to the modeenv
		ks20.kModeenv.modeenv.CurrentKernels = append(
			ks20.kModeenv.modeenv.CurrentKernels,
			ks20.tryKernelSnap.Filename(),
		)
		err := ks20.kModeenv.modeenv.Write()
		if err != nil {
			return err
		}

		err = ks20.ebl.EnableTryKernel(ks20.tryKernelSnap)
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

// chooseAndCommitSnapInitramfsMount chooses which snap should be mounted
// during the early boot sequence, i.e. the initramfs, and commits that
// choice if it needs state updated.
// Choosing to boot/mount the base snap needs to be committed to the
// modeenv, but no state needs to be committed when choosing to mount a
// kernel snap.
func (ks20 *bootState20Kernel) chooseAndCommitSnapInitramfsMount() (sn snap.PlaceInfo, err error) {
	err = ks20.kModeenv.loadModeenv()
	if err != nil {
		return nil, err
	}

	// first do the generic choice of which snap to use
	first, second, expectFallback, err := genericEarlyBootChooseSnap(ks20, TryingStatus, "kernel")
	// first, second, err := genericEarlyBootChooseSnap(ks20, TryingStatus, "kernel")
	if err != nil {
		return nil, err
	}

	// now validate the chosen kernel snap against the modeenv CurrentKernel's
	// setting
	// make a map to easily check if a kernel snap is valid or not
	validKernels := make(map[string]bool, len(ks20.kModeenv.modeenv.CurrentKernels))
	for _, validKernel := range ks20.kModeenv.modeenv.CurrentKernels {
		validKernels[validKernel] = true
	}

	// always try the first and fallback to the second if we fail
	if validKernels[first.Filename()] {
		return first, nil
	}

	// first isn't trusted, so if we expected a fallback then use it
	if expectFallback {
		if validKernels[second.Filename()] {
			// TODO:UC20: actually we really shouldn't be falling back here at
			//            all - if the kernel we booted isn't mountable in the
			//            initramfs, we should trigger a reboot so that we boot
			//            the fallback kernel and then mount that one when we
			//            get back to the initramfs again
			return second, nil
		}
	}

	// no fallback expected, so first snap _is_ the fallback and isn't trusted!
	return nil, fmt.Errorf("fallback kernel snap %q is not trusted in the modeenv", first.Filename())
}

//
// base snap methods
//

// bootState20Kernel implements the bootState and bootStateUpdate interfaces for
// base snaps on UC20. It is used for setNext() and markSuccessful() - though
// note that for markSuccessful() a different bootStateUpdate implementation is
// returned, see bootState20MarkSuccessful
type bootState20Base struct {
	bootState20Modeenv

	// the base_status to be written to the modeenv, stored separately to
	// eliminate unnecessary writes to the modeenv when it's already in the
	// state we want it in
	commitBaseStatus string

	// the base snap that was booted for markSuccessful()
	bootedBaseSnap snap.PlaceInfo

	// the base snap to try for setNext()
	tryBaseSnap snap.PlaceInfo
}

func (bs20 *bootState20Base) loadModeenv() error {
	// don't read modeenv multiple times
	if bs20.modeenv != nil {
		return nil
	}
	modeenv, err := ReadModeenv("")
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

	if bs20.modeenv.BaseStatus != DefaultStatus && bs20.modeenv.TryBase != "" {
		tryBootSn, err = snap.ParsePlaceInfoFromSnapFileName(bs20.modeenv.TryBase)
		if err != nil {
			return bootSn, nil, "", trySnapError(fmt.Sprintf("cannot get snap revision: modeenv try base boot variable is invalid: %v", err))
		}
	}

	return bootSn, tryBootSn, bs20.modeenv.BaseStatus, nil
}

func (bs20 *bootState20Base) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with this object to do most of the legwork
	u, sn, err := chooseBootSnapToMarkSuccessful(bs20, update)
	if err != nil {
		return nil, err
	}

	// u should always be non-nil if err is nil
	u.bootedBaseSnap = sn
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
		return bs20.modeenv.Write()
	}
	return nil
}

// chooseAndCommitSnapInitramfsMount chooses which snap should be mounted
// during the early boot sequence, i.e. the initramfs, and commits that
// choice if it needs state updated.
// Choosing to boot/mount the base snap needs to be committed to the
// modeenv, but no state needs to be committed when choosing to mount a
// kernel snap.
func (bs20 *bootState20Base) chooseAndCommitSnapInitramfsMount() (sn snap.PlaceInfo, err error) {
	err = bs20.loadModeenv()
	if err != nil {
		return nil, err
	}

	// first do the generic choice of which snap to use
	// the logic in that function is sufficient to pick the base snap entirely,
	// so we don't ever need to look at the fallback snap, we just need to know
	// whether the chosen snap is a try snap or not, if it is then we process
	// the modeenv in the "try" -> "trying" case
	first, _, currentlyTryingSnap, err := genericEarlyBootChooseSnap(bs20, TryStatus, "base")
	if err != nil {
		return nil, err
	}

	modeenvChanged := false

	// apply the update logic to the choices modeenv
	switch bs20.modeenv.BaseStatus {
	case TryStatus:
		// if we were in try status and we have a fallback, then we are in a
		// normal try state and we change status to TryingStatus now
		// all other cleanup of state is left to user space snapd
		if currentlyTryingSnap {
			bs20.modeenv.BaseStatus = TryingStatus
			modeenvChanged = true
		}
	case TryingStatus:
		// we tried to boot a try base snap and failed, so we need to reset
		// BaseStatus
		bs20.modeenv.BaseStatus = DefaultStatus
		modeenvChanged = true
	case DefaultStatus:
		// nothing to do
	default:
		// log a message about invalid setting
		logger.Noticef("invalid setting for \"base_status\" in modeenv : %q", bs20.modeenv.BaseStatus)
	}

	if modeenvChanged {
		err = bs20.modeenv.Write()
		if err != nil {
			return nil, err
		}
	}

	return first, nil
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

// chooseBootSnapToMarkSuccessful inspects the specified boot state to pick what
// boot snap should be marked as successful and use as a valid rollback target.
// If the
func chooseBootSnapToMarkSuccessful(b bootState, update bootStateUpdate) (
	bsmark *bootState20MarkSuccessful,
	bootedSnap snap.PlaceInfo,
	err error,
) {
	// get the try snap and the current status
	sn, trySnap, status, err := b.revisions()
	if err != nil {
		return nil, nil, err
	}

	// try to extract bsmark out of update
	var ok bool
	if update != nil {
		if bsmark, ok = update.(*bootState20MarkSuccessful); !ok {
			return nil, nil, fmt.Errorf("internal error, cannot thread %T with update for UC20", update)
		}
	}

	if bsmark == nil {
		bsmark = &bootState20MarkSuccessful{}
	}

	// incorporate the bootState into the bsmark so we don't need to reload
	// bootenv / modeenv, as bootState will already have been initialized in
	// the call to revisions() above
	switch bsGround := b.(type) {
	case *bootState20Base:
		bsmark.bootState20Base = *bsGround
	case *bootState20Kernel:
		bsmark.bootState20Kernel = *bsGround
	}

	// kernel_status and base_status go from "" -> "try" (set by snapd), to
	// "try" -> "trying" (set by the boot script)
	// so if we are in "trying" mode, then we should choose the try snap
	if status == TryingStatus {
		return bsmark, trySnap, nil
	}

	// if we are not in trying then choose the normal snap
	return bsmark, sn, nil
}

// commit will persistently write out the boot variables, etc. needed to mark
// the snaps saved in bsmark as successful boot targets/combinations.
// note that this makes the assumption that markSuccessful() has been called for
// both the base and kernel snaps here, if that assumption is not true anymore,
// this could end up auto-cleaning status variables for something it shouldn't
// be.
func (bsmark *bootState20MarkSuccessful) commit() error {
	// the base and kernel snap updates will modify the modeenv, so we only
	// issue a single write at the end if something changed
	modeenvChanged := false

	// kernel snap first, slightly higher priority

	// the ordering here is very important for boot reliability!

	// If we have successfully just booted from a try-kernel and are
	// marking it successful (this implies that snap_kernel=="trying" as set
	// by the boot script), we need to do the following in order (since we
	// have the added complexity of moving the kernel symlink):
	// 1. Update kernel_status to ""
	// 2. Move kernel symlink to point to the new try kernel
	// 3. Remove try-kernel symlink
	// 4. Remove old kernel from modeenv
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
	//
	// Removing the old kernel from the modeenv needs to happen after it is
	// impossible for the bootloader to boot from that kernel, otherwise we
	// could end up in a state where the bootloader doesn't want to boot the
	// new kernel, but the initramfs doesn't trust the old kernel and we are
	// stuck. As such, do this last, after the symlink no longer exists.
	//
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

	if bsmark.bootedKernelSnap != nil {
		// if the kernel we booted is not the current one, we must have tried
		// a new kernel, so enable that one as the current one now
		if bsmark.currentKernel.Filename() != bsmark.bootedKernelSnap.Filename() {
			err := bsmark.ebl.EnableKernel(bsmark.bootedKernelSnap)
			if err != nil {
				return err
			}
		}

		// always disable the try kernel snap to cleanup in case we have upgrade
		// failures which leave behind try-kernel.efi
		err := bsmark.ebl.DisableTryKernel()
		if err != nil {
			return err
		}

		// also always set current_kernels to be just the kernel we booted, for
		// same reason we always disable the try-kernel
		bsmark.modeenv.CurrentKernels = []string{bsmark.bootedKernelSnap.Filename()}
		modeenvChanged = true
	}

	// always clean up the try kernel, as it may be leftover from a failed boot

	// base snap next
	// the ordering here is less important, since the only operation that
	// has side-effects is writing the modeenv at the end, and that uses an
	// atomic file writing operation, so it's not a concern if we get
	// rebooted during this snippet like it is with the kernel snap above

	// always clear the base_status and try_base when marking successful, this
	// has the useful side-effect of cleaning up if we have base_status=trying
	// but no try_base set, or if we had an issue with try_base being invalid
	if bsmark.modeenv.BaseStatus != DefaultStatus {
		modeenvChanged = true
		bsmark.modeenv.TryBase = ""
		bsmark.modeenv.BaseStatus = DefaultStatus
	}

	if bsmark.bootedBaseSnap != nil {
		// set the new base as the tried base snap
		tryBase := bsmark.bootedBaseSnap.Filename()
		if bsmark.modeenv.Base != tryBase {
			bsmark.modeenv.Base = tryBase
			modeenvChanged = true
		}

		// clear the TryBase
		if bsmark.modeenv.TryBase != "" {
			bsmark.modeenv.TryBase = ""
			modeenvChanged = true
		}
	}

	// write the modeenv
	if modeenvChanged {
		return bsmark.modeenv.Write()
	}

	return nil
}

// genericEarlyBootChooseSnap will run the logic to choose which snap should be
// mounted during the early boot, i.e. initramfs. Specifically, it only works
// with kernel and base snaps. It returns the first and second choice for what
// snaps to mount, if second is set, then it is the primary fallback snap, and
// the first snap is the try snap, if second is unset, then first is the primary
// fallback snap. It returns both so that a higher level function can do
// additional verification of the try snap if it wants, such as the kernel snaps
// being verified in the modeenv, or the base snaps updating the modeenv with
// new values for base_status, etc.
func genericEarlyBootChooseSnap(bs bootState, expectedTryStatus, typeString string) (
	firstChoice, secondChoice snap.PlaceInfo,
	fallbackExpected bool,
	err error,
) {
	curSnap, trySnap, snapTryStatus, err := bs.revisions()

	if err != nil && !IsTrySnapError(err) {
		// we have no fallback snap!
		return nil, nil, false, fmt.Errorf("fallback %s snap unusable: %v", typeString, err)
	}

	// check that the current snap actually exists
	file := curSnap.Filename()
	snapPath := filepath.Join(dirs.SnapBlobDirUnder(InitramfsWritableDir), file)
	if !osutil.FileExists(snapPath) {
		// somehow the boot snap doesn't exist in ubuntu-data
		// for a kernel, this could happen if we have some bug where ubuntu-boot
		// isn't properly updated and never changes, but snapd thinks it was
		// updated and eventually snapd garbage collects old revisions of
		// the kernel snap as it is "refreshed"
		// for a base, this could happen if the modeenv is manipulated
		// out-of-band from snapd
		return nil, nil, false, fmt.Errorf("%s snap %q does not exist on ubuntu-data", typeString, file)
	}

	if err != nil && IsTrySnapError(err) {
		// just log that we had issues with the try snap and continue with
		// using the normal snap
		logger.Noticef("unable to process try %s snap: %v", typeString, err)
	} else {
		if snapTryStatus == expectedTryStatus {
			// then we are trying a snap update and there should be a try snap
			if trySnap != nil {
				// check that the TryBase exists in ubuntu-data, if it doesn't
				// we will fall back to using the normal snap
				trySnapPath := filepath.Join(dirs.SnapBlobDirUnder(InitramfsWritableDir), trySnap.Filename())
				if osutil.FileExists(trySnapPath) {
					return trySnap, curSnap, true, nil
				}
				logger.Noticef("try-%s snap %q does not exist", typeString, trySnap.Filename())
			} else {
				logger.Noticef("try-%[1]s snap is empty, but \"%[1]s_status\" is \"trying\"", typeString)
			}
		} else {
			switch snapTryStatus {
			case TryStatus, DefaultStatus, TryingStatus:
			default:
				logger.Noticef("\"%s_status\" has an invalid setting: %q", typeString, snapTryStatus)
			}
		}
	}

	return curSnap, nil, false, nil
}
