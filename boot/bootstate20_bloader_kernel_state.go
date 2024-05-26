// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/snap"
)

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
	m := mylog.Check2(bks.ebl.GetBootVars("kernel_status"))

	bks.currentKernelStatus = m["kernel_status"]

	// get the current kernel for this bootloader to compare during commit() for
	// markSuccessful() if we booted the current kernel or not
	kernel := mylog.Check2(bks.ebl.Kernel())

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
		mylog.Check(

			// set the boot variables
			bks.ebl.SetBootVars(m))

	}

	// if the kernel we booted is not the current one, we must have tried
	// a new kernel, so enable that one as the current one now
	if bks.currentKernel.Filename() != sn.Filename() {
		mylog.Check(bks.ebl.EnableKernel(sn))
	}
	mylog.

		// always disable the try kernel snap to cleanup in case we have upgrade
		// failures which leave behind try-kernel.efi
		Check(bks.ebl.DisableTryKernel())

	return nil
}

func (bks *extractedRunKernelImageBootloaderKernelState) setNextKernel(sn snap.PlaceInfo, status string) error {
	// always enable the try-kernel first, if we did the reverse and got
	// rebooted after setting the boot vars but before enabling the try-kernel
	// we could get stuck where the bootloader can't find the try-kernel and
	// gets stuck waiting for a user to reboot, at which point we would fallback
	// see i.e. https://github.com/snapcore/pc-amd64-gadget/issues/36
	if sn.Filename() != bks.currentKernel.Filename() {
		mylog.Check(bks.ebl.EnableTryKernel(sn))
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

func (bks *extractedRunKernelImageBootloaderKernelState) setNextKernelNoTry(sn snap.PlaceInfo) error {
	if sn.Filename() != bks.currentKernel.Filename() {
		mylog.Check(bks.ebl.EnableKernel(sn))
	}

	// Make sure that no try-kernel.efi link is left around. We do
	// not really care if this method fails as depending on when
	// we are undoing it might be there or not.
	bks.ebl.DisableTryKernel()

	if bks.currentKernelStatus != DefaultStatus {
		m := map[string]string{
			"kernel_status": DefaultStatus,
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
	m := mylog.Check2(envbks.bl.GetBootVars("kernel_status", "snap_kernel", "snap_try_kernel"))

	// the default commit env is the same state as the current env
	envbks.env = m
	envbks.toCommit = make(map[string]string, len(m))
	for k, v := range m {
		envbks.toCommit[k] = v
	}

	// snap_kernel is the current kernel snap
	// parse the filename here because the kernel() method doesn't return an err
	sn := mylog.Check2(snap.ParsePlaceInfoFromSnapFileName(envbks.env["snap_kernel"]))

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
	sn := mylog.Check2(snap.ParsePlaceInfoFromSnapFileName(envbks.env["snap_try_kernel"]))

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

func (envbks *envRefExtractedKernelBootloaderKernelState) setNextKernelNoTry(sn snap.PlaceInfo) error {
	envbks.toCommit["kernel_status"] = ""
	bootenvChanged := envbks.commonStateCommitUpdate(sn, "snap_kernel")

	if bootenvChanged {
		return envbks.bl.SetBootVars(envbks.toCommit)
	}

	return nil
}
