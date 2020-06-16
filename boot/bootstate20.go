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
	"github.com/snapcore/snapd/strutil"
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
// bootloaderKernelState20 methods
//

type bootloaderKernelState20 interface {
	// load will setup any state / actors needed to use other methods
	load() error
	// kernelStatus returns the current status of the kernel, i.e. the
	// kernel_status bootenv
	kernelStatus() string
	// kernel returns the current non-try kernel
	kernel() snap.PlaceInfo
	// kernel returns the current try kernel if it exists on the bootloader
	tryKernel() (snap.PlaceInfo, error)

	// setNextKernel marks the kernel as the next, if it's not the currently
	// booted kernel, then the specified kernel is setup as a try-kernel
	setNextKernel(sn snap.PlaceInfo, status string) error
	// markSuccessfulKernel marks the specified kernel as having booted
	// successfully, whether that kernel is the current kernel or the try-kernel
	markSuccessfulKernel(sn snap.PlaceInfo) error
}

// extractedRunKernelImageBootloaderKernelState implements bootloaderKernelState20 for
// bootloaders that implement ExtractedRunKernelImageBootloader
type extractedRunKernelImageBootloaderKernelState struct {
	// the bootloader
	ebl bootloader.ExtractedRunKernelImageBootloader
	// the current kernel status as read by the bootloader's bootenv
	currentKernelStatus string
	// the current kernel on the bootloader (not the try-kernel)
	currentKernel snap.PlaceInfo
}

func (bks *extractedRunKernelImageBootloaderKernelState) load() error {
	// get the kernel_status
	m, err := bks.ebl.GetBootVars("kernel_status")
	if err != nil {
		return err
	}

	bks.currentKernelStatus = m["kernel_status"]

	// get the current kernel for this bootloader to compare during commit() for
	// markSuccessful() if we booted the current kernel or not
	kernel, err := bks.ebl.Kernel()
	if err != nil {
		return fmt.Errorf("cannot identify kernel snap with bootloader %s: %v", bks.ebl.Name(), err)
	}

	bks.currentKernel = kernel

	return nil
}

func (bks *extractedRunKernelImageBootloaderKernelState) kernel() snap.PlaceInfo {
	return bks.currentKernel
}

func (bks *extractedRunKernelImageBootloaderKernelState) tryKernel() (snap.PlaceInfo, error) {
	return bks.ebl.TryKernel()
}

func (bks *extractedRunKernelImageBootloaderKernelState) kernelStatus() string {
	return bks.currentKernelStatus
}

func (bks *extractedRunKernelImageBootloaderKernelState) markSuccessfulKernel(sn snap.PlaceInfo) error {
	// set the boot vars first, then enable the successful kernel, then disable
	// the old try-kernel, see the comment in bootState20MarkSuccessful.commit()
	// for details

	// the ordering here is very important for boot reliability!

	// If we have successfully just booted from a try-kernel and are
	// marking it successful (this implies that snap_kernel=="trying" as set
	// by the boot script), we need to do the following in order (since we
	// have the added complexity of moving the kernel symlink):
	// 1. Update kernel_status to ""
	// 2. Move kernel symlink to point to the new try kernel
	// 3. Remove try-kernel symlink
	// 4. Remove old kernel from modeenv (this happens one level up from this
	//    function)
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

	// always set the boot vars first before mutating any of the kernel symlinks
	// etc.
	// for markSuccessful, we will always set the status to Default, even if
	// technically this boot wasn't "successful" - it was successful in the
	// sense that we booted some combination of boot snaps and made it all the
	// way to snapd in user space
	if bks.currentKernelStatus != DefaultStatus {
		m := map[string]string{
			"kernel_status": DefaultStatus,
		}

		// set the boot variables
		err := bks.ebl.SetBootVars(m)
		if err != nil {
			return err
		}
	}

	// if the kernel we booted is not the current one, we must have tried
	// a new kernel, so enable that one as the current one now
	if bks.currentKernel.Filename() != sn.Filename() {
		err := bks.ebl.EnableKernel(sn)
		if err != nil {
			return err
		}
	}

	// always disable the try kernel snap to cleanup in case we have upgrade
	// failures which leave behind try-kernel.efi
	err := bks.ebl.DisableTryKernel()
	if err != nil {
		return err
	}

	return nil
}

func (bks *extractedRunKernelImageBootloaderKernelState) setNextKernel(sn snap.PlaceInfo, status string) error {
	// always enable the try-kernel first, if we did the reverse and got
	// rebooted after setting the boot vars but before enabling the try-kernel
	// we could get stuck where the bootloader can't find the try-kernel and
	// gets stuck waiting for a user to reboot, at which point we would fallback
	// see i.e. https://github.com/snapcore/pc-amd64-gadget/issues/36
	if sn.Filename() != bks.currentKernel.Filename() {
		err := bks.ebl.EnableTryKernel(sn)
		if err != nil {
			return err
		}
	}

	// only if the new kernel status is different from what we read should we
	// run SetBootVars() to minimize wear/corruption possibility on the bootenv
	if status != bks.currentKernelStatus {
		m := map[string]string{
			"kernel_status": status,
		}

		// set the boot variables
		return bks.ebl.SetBootVars(m)
	}

	return nil
}

// envRefExtractedKernelBootloaderKernelState implements bootloaderKernelState20 for
// bootloaders that only support using bootloader env and i.e. don't support
// ExtractedRunKernelImageBootloader
type envRefExtractedKernelBootloaderKernelState struct {
	// the bootloader
	bl bootloader.Bootloader

	// the current state of env
	env map[string]string

	// the state of env to commit
	toCommit map[string]string

	// the current kernel
	kern snap.PlaceInfo
}

func (envbks *envRefExtractedKernelBootloaderKernelState) load() error {
	// for uc20, we only care about kernel_status, snap_kernel, and
	// snap_try_kernel
	m, err := envbks.bl.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel")
	if err != nil {
		return err
	}

	// the default commit env is the same state as the current env
	envbks.env = m
	envbks.toCommit = make(map[string]string, len(m))
	for k, v := range m {
		envbks.toCommit[k] = v
	}

	// snap_kernel is the current kernel snap
	// parse the filename here because the kernel() method doesn't return an err
	sn, err := snap.ParsePlaceInfoFromSnapFileName(envbks.env["snap_kernel"])
	if err != nil {
		return err
	}

	envbks.kern = sn

	return nil
}

func (envbks *envRefExtractedKernelBootloaderKernelState) kernel() snap.PlaceInfo {
	return envbks.kern
}

func (envbks *envRefExtractedKernelBootloaderKernelState) tryKernel() (snap.PlaceInfo, error) {
	// empty snap_try_kernel is special case
	if envbks.env["snap_try_kernel"] == "" {
		return nil, bootloader.ErrNoTryKernelRef
	}
	sn, err := snap.ParsePlaceInfoFromSnapFileName(envbks.env["snap_try_kernel"])
	if err != nil {
		return nil, err
	}

	return sn, nil
}

func (envbks *envRefExtractedKernelBootloaderKernelState) kernelStatus() string {
	return envbks.env["kernel_status"]
}

func (envbks *envRefExtractedKernelBootloaderKernelState) commonStateCommitUpdate(sn snap.PlaceInfo, bootvar string) bool {
	envChanged := false

	// check kernel_status
	if envbks.env["kernel_status"] != envbks.toCommit["kernel_status"] {
		envChanged = true
	}

	// if the specified snap is not the current snap, update the bootvar
	if sn.Filename() != envbks.kern.Filename() {
		envbks.toCommit[bootvar] = sn.Filename()
		envChanged = true
	}

	return envChanged
}

func (envbks *envRefExtractedKernelBootloaderKernelState) markSuccessfulKernel(sn snap.PlaceInfo) error {
	// the ordering here doesn't matter, as the only actual state we mutate is
	// writing the bootloader env vars, so just do that once at the end after
	// processing all the changes

	// always set kernel_status to DefaultStatus
	envbks.toCommit["kernel_status"] = DefaultStatus
	envChanged := envbks.commonStateCommitUpdate(sn, "snap_kernel")

	// if the snap_try_kernel is set, we should unset that to both cleanup after
	// a successful trying -> "" transition, but also to cleanup if we got
	// rebooted during the process and have it leftover
	if envbks.env["snap_try_kernel"] != "" {
		envChanged = true
		envbks.toCommit["snap_try_kernel"] = ""
	}

	if envChanged {
		return envbks.bl.SetBootVars(envbks.toCommit)
	}

	return nil
}

func (envbks *envRefExtractedKernelBootloaderKernelState) setNextKernel(sn snap.PlaceInfo, status string) error {
	envbks.toCommit["kernel_status"] = status
	bootenvChanged := envbks.commonStateCommitUpdate(sn, "snap_try_kernel")

	if bootenvChanged {
		return envbks.bl.SetBootVars(envbks.toCommit)
	}

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
	bks bootloaderKernelState20

	// the kernel snap that was booted for markSuccessful()
	bootedKernelSnap snap.PlaceInfo

	// the kernel snap to try for setNext()
	nextKernelSnap snap.PlaceInfo

	// the kernel_status to commit during commit()
	commitKernelStatus string

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
	if ks20.bks != nil {
		return nil
	}

	// find the bootloader and ensure it's an extracted run kernel image
	// bootloader

	var opts *bootloader.Options
	if ks20.blOpts != nil {
		opts = ks20.blOpts
	} else {
		// we want extracted run kernel images for uc20
		// TODO:UC20: the name of this flag is now confusing, as it is being
		//            slightly abused to tell the uboot bootloader to just look
		//            in a different directory, even when we don't have an
		//            actual extracted kernel image for that impl
		opts = &bootloader.Options{
			ExtractedRunKernelImage: true,
		}
	}
	bl, err := bootloader.Find(ks20.blDir, opts)
	if err != nil {
		return err
	}
	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if ok {
		// use the new 20-style ExtractedRunKernelImage implementation
		ks20.bks = &extractedRunKernelImageBootloaderKernelState{ebl: ebl}
	} else {
		// use fallback pure bootloader env implementation
		ks20.bks = &envRefExtractedKernelBootloaderKernelState{bl: bl}
	}

	// setup the bootloaderKernelState20
	if err := ks20.bks.load(); err != nil {
		return err
	}

	return nil
}

func (ks20 *bootState20Kernel) revisions() (curSnap, trySnap snap.PlaceInfo, tryingStatus string, err error) {
	var tryBootSn snap.PlaceInfo
	err = ks20.loadBootenv()
	if err != nil {
		return nil, nil, "", err
	}

	status := ks20.bks.kernelStatus()
	kern := ks20.bks.kernel()

	tryKernel, err := ks20.bks.tryKernel()
	// if err is ErrNoTryKernelRef, then we will just return nil as the trySnap
	if err != nil && err != bootloader.ErrNoTryKernelRef {
		return kern, nil, "", newTrySnapErrorf("cannot identify try kernel snap: %v", err)
	}

	if err == nil {
		tryBootSn = tryKernel
	}

	return kern, tryBootSn, status, nil
}

func (ks20 *bootState20Kernel) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with this object to do most of the legwork
	u, sn, err := selectSuccessfulBootSnap(ks20, update)
	if err != nil {
		return nil, err
	}

	// save this object inside the update to share bootenv / modeenv between
	// multiple calls to markSuccessful for different snap types, but the same
	// bootStateUpdate object
	u.bootState20Kernel = *ks20

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
	ks20.nextKernelSnap = next
	if nextStatus == TryStatus {
		rebootRequired = true
	}
	ks20.commitKernelStatus = nextStatus

	// any state changes done so far are consumed in commit()

	return rebootRequired, ks20, nil
}

// commit for bootState20Kernel is meant only to be used with setNext().
// For markSuccessful(), use bootState20MarkSuccessful.
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

	// add the kernel to the modeenv if it is not the current kernel (if it is
	// the current kernel then it must already be in the modeenv)
	currentKernel := ks20.bks.kernel()
	if ks20.nextKernelSnap.Filename() != currentKernel.Filename() {
		// add the kernel to the modeenv
		ks20.kModeenv.modeenv.CurrentKernels = append(
			ks20.kModeenv.modeenv.CurrentKernels,
			ks20.nextKernelSnap.Filename(),
		)
		err := ks20.kModeenv.modeenv.Write()
		if err != nil {
			return err
		}
	}

	err := ks20.bks.setNextKernel(ks20.nextKernelSnap, ks20.commitKernelStatus)
	if err != nil {
		return err
	}

	return nil
}

// selectAndCommitSnapInitramfsMount chooses which snap should be mounted
// during the initramfs, and commits that choice if it needs state updated.
// Choosing to boot/mount the base snap needs to be committed to the
// modeenv, but no state needs to be committed when choosing to mount a
// kernel snap.
func (ks20 *bootState20Kernel) selectAndCommitSnapInitramfsMount() (sn snap.PlaceInfo, err error) {
	err = ks20.kModeenv.loadModeenv()
	if err != nil {
		return nil, err
	}

	// first do the generic choice of which snap to use
	first, second, expectFallback, err := genericInitramfsSelectSnap(ks20, TryingStatus, "kernel")
	if err != nil {
		return nil, err
	}

	// now validate the chosen kernel snap against the modeenv CurrentKernel's
	// setting

	// always try the first and fallback to the second if we fail
	if strutil.ListContains(ks20.kModeenv.modeenv.CurrentKernels, first.Filename()) {
		return first, nil
	}

	// first isn't trusted, so if we expected a fallback then use it
	if second != nil {
		if strutil.ListContains(ks20.kModeenv.modeenv.CurrentKernels, second.Filename()) {
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
			return bootSn, nil, "", newTrySnapErrorf("cannot get snap revision: modeenv try base boot variable is invalid: %v", err)
		}
	}

	return bootSn, tryBootSn, bs20.modeenv.BaseStatus, nil
}

func (bs20 *bootState20Base) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with this object to do most of the legwork
	u, sn, err := selectSuccessfulBootSnap(bs20, update)
	if err != nil {
		return nil, err
	}

	// save this object inside the update to share bootenv / modeenv between
	// multiple calls to markSuccessful for different snap types, but the same
	// bootStateUpdate object
	u.bootState20Base = *bs20

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

// selectAndCommitSnapInitramfsMount chooses which snap should be mounted
// during the early boot sequence, i.e. the initramfs, and commits that
// choice if it needs state updated.
// Choosing to boot/mount the base snap needs to be committed to the
// modeenv, but no state needs to be committed when choosing to mount a
// kernel snap.
func (bs20 *bootState20Base) selectAndCommitSnapInitramfsMount() (sn snap.PlaceInfo, err error) {
	err = bs20.loadModeenv()
	if err != nil {
		return nil, err
	}

	// first do the generic choice of which snap to use
	// the logic in that function is sufficient to pick the base snap entirely,
	// so we don't ever need to look at the fallback snap, we just need to know
	// whether the chosen snap is a try snap or not, if it is then we process
	// the modeenv in the "try" -> "trying" case
	first, _, currentlyTryingSnap, err := genericInitramfsSelectSnap(bs20, TryStatus, "base")
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

// selectSuccessfulBootSnap inspects the specified boot state to pick what
// boot snap should be marked as successful and use as a valid rollback target.
// If the first return value is non-nil, the second return value will be the
// snap that was booted and should be marked as successful.
func selectSuccessfulBootSnap(b bootState, update bootStateUpdate) (
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

	// kernel_status and base_status go from "" -> "try" (set by snapd), to
	// "try" -> "trying" (set by the boot script)
	// so if we are in "trying" mode, then we should choose the try snap
	if status == TryingStatus && trySnap != nil {
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

	// for full explanation of the robustness and ordering, see the comments
	// on the implementations of bks.markSuccessfulKernel

	// kernel snap first, slightly higher priority

	// bootedKernelSnap will only ever be non-nil if we aren't marking a kernel
	// snap successful, i.e. we are only marking a base snap successful
	// this shouldn't happen except in tests, but let's be robust against it
	// just in case
	if bsmark.bootedKernelSnap != nil {
		// always mark the kernel snap successful _before_ any other state
		// mutating that may happen in bks.markSuccessful, because what we don't
		// want to happen is to remove the old kernel and only trust the new
		// try kernel before we actually set it up to boot from the new try
		// kernel - that would brick us because we wouldn't trust the new kernel
		// but the bootloader still thinks it should boot from the old kernel
		err := bsmark.bks.markSuccessfulKernel(bsmark.bootedKernelSnap)
		if err != nil {
			return err
		}

		// also always set current_kernels to be just the kernel we booted, for
		// same reason we always disable the try-kernel
		bsmark.modeenv.CurrentKernels = []string{bsmark.bootedKernelSnap.Filename()}
		modeenvChanged = true
	}

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

// genericInitramfsSelectSnap will run the logic to choose which snap should be
// mounted during the initramfs using the given bootState and the expected try
// status. The try status is needed because during the initramfs we will have
// different statuses for kernel vs base snaps, where base snap is expected to
// be in "try" mode, but kernel is expected to be in "trying" mode. It returns
// the first and second choice for what snaps to mount, and the bool indicates
// if there is a second snap set or not. If there is a second snap, then that
// snap is the fallback or non-trying snap and the first snap is the try snap.
func genericInitramfsSelectSnap(bs bootState, expectedTryStatus, typeString string) (
	firstChoice, secondChoice snap.PlaceInfo,
	fallbackExpected bool,
	err error,
) {
	curSnap, trySnap, snapTryStatus, err := bs.revisions()

	if err != nil && !isTrySnapError(err) {
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

	if err != nil && isTrySnapError(err) {
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
