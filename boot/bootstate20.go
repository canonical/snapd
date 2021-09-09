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
	"path/filepath"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

func newBootState20(typ snap.Type, dev Device) bootState {
	switch typ {
	case snap.TypeBase:
		return &bootState20Base{}
	case snap.TypeKernel:
		return &bootState20Kernel{
			dev: dev,
		}
	default:
		panic(fmt.Sprintf("cannot make a bootState20 for snap type %q", typ))
	}
}

func loadModeenv() (*Modeenv, error) {
	modeenv, err := ReadModeenv("")
	if err != nil {
		return nil, fmt.Errorf("cannot get snap revision: unable to read modeenv: %v", err)
	}
	return modeenv, nil
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

//
// bootStateUpdate for 20 methods
//

type bootCommitTask func() error

// bootStateUpdate20 implements the bootStateUpdate interface for both kernel
// and base snaps on UC20.
type bootStateUpdate20 struct {
	// tasks to run before the modeenv has been written
	preModeenvTasks []bootCommitTask

	// the modeenv that was read from disk
	modeenv *Modeenv

	// the modeenv that will be written out in commit
	writeModeenv *Modeenv

	// tasks to run after the modeenv has been written
	postModeenvTasks []bootCommitTask
}

func (u20 *bootStateUpdate20) preModeenv(task bootCommitTask) {
	u20.preModeenvTasks = append(u20.preModeenvTasks, task)
}

func (u20 *bootStateUpdate20) postModeenv(task bootCommitTask) {
	u20.postModeenvTasks = append(u20.postModeenvTasks, task)
}

func newBootStateUpdate20(m *Modeenv) (*bootStateUpdate20, error) {
	u20 := &bootStateUpdate20{}
	if m == nil {
		var err error
		m, err = loadModeenv()
		if err != nil {
			return nil, err
		}
	}
	// copy the modeenv for the write object
	u20.modeenv = m
	var err error
	u20.writeModeenv, err = m.Copy()
	if err != nil {
		return nil, err
	}
	return u20, nil
}

// commit will write out boot state persistently to disk.
func (u20 *bootStateUpdate20) commit() error {
	// The actual actions taken here will depend on what things were called
	// before commit(), either setNextBoot for a single type of kernel snap, or
	// markSuccessful for kernel and/or base snaps.
	// It is expected that the caller code is carefully analyzed to avoid
	// critical points where a hard system reset during that critical point
	// would brick a device or otherwise severely fail an update.
	// There are three things that callers can do before calling commit(),
	// 1. modify writeModeenv to specify new values for things that will be
	//    written to disk in the modeenv.
	// 2. Add tasks to run before writing the modeenv.
	// 3. Add tasks to run after writing the modeenv.

	// first handle any pre-modeenv writing tasks
	for _, t := range u20.preModeenvTasks {
		if err := t(); err != nil {
			return err
		}
	}

	modeenvRewritten := false
	// next write the modeenv if it changed
	if !u20.writeModeenv.deepEqual(u20.modeenv) {
		if err := u20.writeModeenv.Write(); err != nil {
			return err
		}
		modeenvRewritten = true
	}

	// next reseal using the modeenv values, we do this before any
	// post-modeenv tasks so if we are rebooted at any point after
	// the reseal even before the post tasks are completed, we
	// still boot properly

	// if there is ambiguity whether the boot chains have
	// changed because of unasserted kernels, then pass a
	// flag as hint whether to reseal based on whether we
	// wrote the modeenv
	expectReseal := modeenvRewritten
	if err := resealKeyToModeenv(dirs.GlobalRootDir, u20.writeModeenv, expectReseal); err != nil {
		return err
	}

	// finally handle any post-modeenv writing tasks
	for _, t := range u20.postModeenvTasks {
		if err := t(); err != nil {
			return err
		}
	}

	return nil
}

//
// kernel snap methods
//

// bootState20Kernel implements the bootState interface for kernel snaps on
// UC20. It is used for both setNext() and markSuccessful(), with both of those
// methods returning bootStateUpdate20 to be used with bootStateUpdate.
type bootState20Kernel struct {
	bks bootloaderKernelState20

	// used to find the bootloader to manipulate the enabled kernel, etc.
	blOpts *bootloader.Options
	blDir  string

	dev Device
}

func (ks20 *bootState20Kernel) loadBootenv() error {
	// don't setup multiple times
	if ks20.bks != nil {
		return nil
	}

	// find the run-mode bootloader
	var opts *bootloader.Options
	if ks20.blOpts != nil {
		opts = ks20.blOpts
	} else {
		opts = &bootloader.Options{
			Role: bootloader.RoleRunMode,
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

func (ks20 *bootState20Kernel) revisionsFromModeenv(*Modeenv) (curSnap, trySnap snap.PlaceInfo, tryingStatus string, err error) {
	// the kernel snap doesn't use modeenv at all for getting their revisions
	return ks20.revisions()
}

func (ks20 *bootState20Kernel) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with this object to do most of the legwork
	u20, sn, err := selectSuccessfulBootSnap(ks20, update)
	if err != nil {
		return nil, err
	}

	// XXX: this if arises because some unit tests rely on not setting up kernel
	// details and just operating on the base snap but this situation would
	// never happen in reality
	if sn != nil {
		// On commit, mark the kernel successful before rewriting the modeenv
		// because if we first rewrote the modeenv then got rebooted before
		// marking the kernel successful, the bootloader would see that the boot
		// failed to mark it successful and then fall back to the original
		// kernel, but that kernel would no longer be in the modeenv, so we
		// would die in the initramfs
		u20.preModeenv(func() error { return ks20.bks.markSuccessfulKernel(sn) })

		// On commit, set CurrentKernels as just this kernel because that is the
		// successful kernel we booted
		u20.writeModeenv.CurrentKernels = []string{sn.Filename()}
	}

	return u20, nil
}

func (ks20 *bootState20Kernel) setNext(next snap.PlaceInfo) (rebootRequired bool, u bootStateUpdate, err error) {
	u20, nextStatus, err := genericSetNext(ks20, next)
	if err != nil {
		return false, nil, err
	}

	// if we are setting a snap as a try snap, then we need to reboot
	rebootRequired = false
	if nextStatus == TryStatus {
		rebootRequired = true
	}

	currentKernel := ks20.bks.kernel()
	if next.Filename() != currentKernel.Filename() {
		// on commit, add this kernel to the modeenv
		u20.writeModeenv.CurrentKernels = append(
			u20.writeModeenv.CurrentKernels,
			next.Filename(),
		)
	}

	// On commit, if we are about to try an update, and need to set the next
	// kernel before rebooting, we need to do that after updating the modeenv,
	// because if we did it before and got rebooted in between setting the next
	// kernel and updating the modeenv, the initramfs would fail the boot
	// because the modeenv doesn't "trust" or expect the new kernel that booted.
	// As such, set the next kernel as a post modeenv task.
	u20.postModeenv(func() error { return ks20.bks.setNextKernel(next, nextStatus) })

	return rebootRequired, u20, nil
}

// selectAndCommitSnapInitramfsMount chooses which snap should be mounted
// during the initramfs, and commits that choice if it needs state updated.
// Choosing to boot/mount the base snap needs to be committed to the
// modeenv, but no state needs to be committed when choosing to mount a
// kernel snap.
func (ks20 *bootState20Kernel) selectAndCommitSnapInitramfsMount(modeenv *Modeenv) (sn snap.PlaceInfo, err error) {
	// first do the generic choice of which snap to use
	first, second, err := genericInitramfsSelectSnap(ks20, modeenv, TryingStatus, "kernel")
	if err != nil && err != errTrySnapFallback {
		return nil, err
	}

	if err == errTrySnapFallback {
		// this should not actually return, it should immediately reboot
		return nil, initramfsReboot()
	}

	// now validate the chosen kernel snap against the modeenv CurrentKernel's
	// setting
	if strutil.ListContains(modeenv.CurrentKernels, first.Filename()) {
		return first, nil
	}

	// if we didn't trust the first kernel in the modeenv, and second is set as
	// a fallback, that means we booted a try kernel which is the first kernel,
	// but we need to fallback to the second kernel, but we can't do that in the
	// initramfs, we need to reboot so the bootloader boots the fallback kernel
	// for us

	if second != nil {
		// this should not actually return, it should immediately reboot
		return nil, initramfsReboot()
	}

	// no fallback expected, so first snap _is_ the only kernel and isn't
	// trusted!
	// since we have nothing to fallback to, we don't issue a reboot and will
	// instead just fail the systemd unit in the initramfs for an operator to
	// debug/fix
	return nil, fmt.Errorf("fallback kernel snap %q is not trusted in the modeenv", first.Filename())
}

//
// base snap methods
//

// bootState20Base implements the bootState interface for base snaps on UC20.
// It is used for both setNext() and markSuccessful(), with both of those
// methods returning bootStateUpdate20 to be used with bootStateUpdate.
type bootState20Base struct{}

// revisions returns the current boot snap and optional try boot snap for the
// type specified in bsgeneric.
func (bs20 *bootState20Base) revisions() (curSnap, trySnap snap.PlaceInfo, tryingStatus string, err error) {
	modeenv, err := loadModeenv()
	if err != nil {
		return nil, nil, "", err
	}
	return bs20.revisionsFromModeenv(modeenv)
}

func (bs20 *bootState20Base) revisionsFromModeenv(modeenv *Modeenv) (curSnap, trySnap snap.PlaceInfo, tryingStatus string, err error) {
	var bootSn, tryBootSn snap.PlaceInfo

	if modeenv.Base == "" {
		return nil, nil, "", fmt.Errorf("cannot get snap revision: modeenv base boot variable is empty")
	}

	bootSn, err = snap.ParsePlaceInfoFromSnapFileName(modeenv.Base)
	if err != nil {
		return nil, nil, "", fmt.Errorf("cannot get snap revision: modeenv base boot variable is invalid: %v", err)
	}

	if modeenv.BaseStatus != DefaultStatus && modeenv.TryBase != "" {
		tryBootSn, err = snap.ParsePlaceInfoFromSnapFileName(modeenv.TryBase)
		if err != nil {
			return bootSn, nil, "", newTrySnapErrorf("cannot get snap revision: modeenv try base boot variable is invalid: %v", err)
		}
	}

	return bootSn, tryBootSn, modeenv.BaseStatus, nil
}

func (bs20 *bootState20Base) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	// call the generic method with this object to do most of the legwork
	u20, sn, err := selectSuccessfulBootSnap(bs20, update)
	if err != nil {
		return nil, err
	}

	// on commit, always clear the base_status and try_base when marking
	// successful, this has the useful side-effect of cleaning up if we have
	// base_status=trying but no try_base set, or if we had an issue with
	// try_base being invalid
	u20.writeModeenv.BaseStatus = DefaultStatus
	u20.writeModeenv.TryBase = ""

	// set the base
	u20.writeModeenv.Base = sn.Filename()

	return u20, nil
}

func (bs20 *bootState20Base) setNext(next snap.PlaceInfo) (rebootRequired bool, u bootStateUpdate, err error) {
	u20, nextStatus, err := genericSetNext(bs20, next)
	if err != nil {
		return false, nil, err
	}

	// if we are setting a snap as a try snap, then we need to reboot
	rebootRequired = false
	if nextStatus == TryStatus {
		// only update the try base if we are actually in try status
		u20.writeModeenv.TryBase = next.Filename()
		rebootRequired = true
	}

	// always update the base status
	u20.writeModeenv.BaseStatus = nextStatus

	return rebootRequired, u20, nil
}

// selectAndCommitSnapInitramfsMount chooses which snap should be mounted
// during the early boot sequence, i.e. the initramfs, and commits that
// choice if it needs state updated.
// Choosing to boot/mount the base snap needs to be committed to the
// modeenv, but no state needs to be committed when choosing to mount a
// kernel snap.
func (bs20 *bootState20Base) selectAndCommitSnapInitramfsMount(modeenv *Modeenv) (sn snap.PlaceInfo, err error) {
	// first do the generic choice of which snap to use
	// the logic in that function is sufficient to pick the base snap entirely,
	// so we don't ever need to look at the fallback snap, we just need to know
	// whether the chosen snap is a try snap or not, if it is then we process
	// the modeenv in the "try" -> "trying" case
	first, second, err := genericInitramfsSelectSnap(bs20, modeenv, TryStatus, "base")
	// errTrySnapFallback is handled manually by inspecting second below
	if err != nil && err != errTrySnapFallback {
		return nil, err
	}

	modeenvChanged := false

	// apply the update logic to the choices modeenv
	switch modeenv.BaseStatus {
	case TryStatus:
		// if we were in try status and we have a fallback, then we are in a
		// normal try state and we change status to TryingStatus now
		// all other cleanup of state is left to user space snapd
		if second != nil {
			modeenv.BaseStatus = TryingStatus
			modeenvChanged = true
		}
	case TryingStatus:
		// we tried to boot a try base snap and failed, so we need to reset
		// BaseStatus
		modeenv.BaseStatus = DefaultStatus
		modeenvChanged = true
	case DefaultStatus:
		// nothing to do
	default:
		// log a message about invalid setting
		logger.Noticef("invalid setting for \"base_status\" in modeenv : %q", modeenv.BaseStatus)
	}

	if modeenvChanged {
		err = modeenv.Write()
		if err != nil {
			return nil, err
		}
	}

	return first, nil
}

//
// generic methods
//

type bootState20 interface {
	bootState
	// revisionsFromModeenv implements bootState.revisions but starting
	// from an already loaded Modeenv.
	revisionsFromModeenv(*Modeenv) (curSnap, trySnap snap.PlaceInfo, tryingStatus string, err error)
}

// genericSetNext implements the generic logic for setting up a snap to be tried
// for boot and works for both kernel and base snaps (though not
// simultaneously).
func genericSetNext(b bootState20, next snap.PlaceInfo) (u20 *bootStateUpdate20, setStatus string, err error) {
	u20, err = newBootStateUpdate20(nil)
	if err != nil {
		return nil, "", err
	}

	// get the current snap
	current, _, _, err := b.revisionsFromModeenv(u20.modeenv)
	if err != nil {
		return nil, "", err
	}

	// check if the next snap is really the same as the current snap, in which
	// case we either do nothing or just clear the status (and not reboot)
	if current.SnapName() == next.SnapName() && next.SnapRevision() == current.SnapRevision() {
		// if we are setting the next snap as the current snap, don't need to
		// change any snaps, just reset the status to default
		return u20, DefaultStatus, nil
	}

	// by default we will set the status as "try" to prepare for an update,
	// which also by default will require a reboot
	return u20, TryStatus, nil
}

func toBootStateUpdate20(update bootStateUpdate) (u20 *bootStateUpdate20, err error) {
	// try to extract bootStateUpdate20 out of update
	if update != nil {
		var ok bool
		if u20, ok = update.(*bootStateUpdate20); !ok {
			return nil, fmt.Errorf("internal error, cannot thread %T with update for UC20", update)
		}
	}
	if u20 == nil {
		// make a new one, also loading modeenv
		u20, err = newBootStateUpdate20(nil)
		if err != nil {
			return nil, err
		}
	}
	return u20, nil
}

// selectSuccessfulBootSnap inspects the specified boot state to pick what
// boot snap should be marked as successful and use as a valid rollback target.
// If the first return value is non-nil, the second return value will be the
// snap that was booted and should be marked as successful.
func selectSuccessfulBootSnap(b bootState20, update bootStateUpdate) (
	u20 *bootStateUpdate20,
	bootedSnap snap.PlaceInfo,
	err error,
) {
	u20, err = toBootStateUpdate20(update)
	if err != nil {
		return nil, nil, err
	}

	// get the try snap and the current status
	sn, trySnap, status, err := b.revisionsFromModeenv(u20.modeenv)
	if err != nil {
		return nil, nil, err
	}

	// kernel_status and base_status go from "" -> "try" (set by snapd), to
	// "try" -> "trying" (set by the boot script)
	// so if we are in "trying" mode, then we should choose the try snap
	if status == TryingStatus && trySnap != nil {
		return u20, trySnap, nil
	}

	// if we are not in trying then choose the normal snap
	return u20, sn, nil
}

// genericInitramfsSelectSnap will run the logic to choose which snap should be
// mounted during the initramfs using the given bootState and the expected try
// status. The try status is needed because during the initramfs we will have
// different statuses for kernel vs base snaps, where base snap is expected to
// be in "try" mode, but kernel is expected to be in "trying" mode. It returns
// the first and second choice for what snaps to mount. If there is a second
// snap, then that snap is the fallback or non-trying snap and the first snap is
// the try snap.
func genericInitramfsSelectSnap(bs bootState20, modeenv *Modeenv, expectedTryStatus, typeString string) (
	firstChoice, secondChoice snap.PlaceInfo,
	err error,
) {
	curSnap, trySnap, snapTryStatus, err := bs.revisionsFromModeenv(modeenv)

	if err != nil && !isTrySnapError(err) {
		// we have no fallback snap!
		return nil, nil, fmt.Errorf("fallback %s snap unusable: %v", typeString, err)
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
		return nil, nil, fmt.Errorf("%s snap %q does not exist on ubuntu-data", typeString, file)
	}

	if err != nil && isTrySnapError(err) {
		// just log that we had issues with the try snap and continue with
		// using the normal snap
		logger.Noticef("unable to process try %s snap: %v", typeString, err)
		return curSnap, nil, errTrySnapFallback
	}
	if snapTryStatus != expectedTryStatus {
		// the status is unexpected, log if its value is invalid and continue
		// with the normal snap
		fallbackErr := errTrySnapFallback
		switch snapTryStatus {
		case DefaultStatus:
			fallbackErr = nil
		case TryStatus, TryingStatus:
		default:
			logger.Noticef("\"%s_status\" has an invalid setting: %q", typeString, snapTryStatus)
		}
		return curSnap, nil, fallbackErr
	}
	// then we are trying a snap update and there should be a try snap
	if trySnap == nil {
		// it is unexpected when there isn't one
		logger.Noticef("try-%[1]s snap is empty, but \"%[1]s_status\" is \"trying\"", typeString)
		return curSnap, nil, errTrySnapFallback
	}
	trySnapPath := filepath.Join(dirs.SnapBlobDirUnder(InitramfsWritableDir), trySnap.Filename())
	if !osutil.FileExists(trySnapPath) {
		// or when the snap file does not exist
		logger.Noticef("try-%s snap %q does not exist", typeString, trySnap.Filename())
		return curSnap, nil, errTrySnapFallback
	}

	// we have a try snap and everything appears in order
	return trySnap, curSnap, nil
}

//
// non snap boot resources
//

// bootState20BootAssets implements the successfulBootState interface for trusted
// boot assets UC20.
type bootState20BootAssets struct {
	dev Device
}

func (ba20 *bootState20BootAssets) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	u20, err := toBootStateUpdate20(update)
	if err != nil {
		return nil, err
	}

	if len(u20.modeenv.CurrentTrustedBootAssets) == 0 && len(u20.modeenv.CurrentTrustedRecoveryBootAssets) == 0 {
		// not using trusted boot assets, nothing more to do
		return update, nil
	}

	newM, dropAssets, err := observeSuccessfulBootAssets(u20.writeModeenv)
	if err != nil {
		return nil, fmt.Errorf("cannot mark successful boot assets: %v", err)
	}
	// update modeenv
	u20.writeModeenv = newM

	if len(dropAssets) == 0 {
		// nothing to drop, we're done
		return u20, nil
	}

	u20.postModeenv(func() error {
		cache := newTrustedAssetsCache(dirs.SnapBootAssetsDir)
		// drop listed assets from cache
		for _, ta := range dropAssets {
			err := cache.Remove(ta.blName, ta.name, ta.hash)
			if err != nil {
				// XXX: should this be a log instead?
				return fmt.Errorf("cannot remove unused boot asset %v:%v: %v", ta.name, ta.hash, err)
			}
		}
		return nil
	})
	return u20, nil
}

func trustedAssetsBootState(dev Device) *bootState20BootAssets {
	return &bootState20BootAssets{
		dev: dev,
	}
}

// bootState20CommandLine implements the successfulBootState interface for
// kernel command line
type bootState20CommandLine struct {
	dev Device
}

func (bcl20 *bootState20CommandLine) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	u20, err := toBootStateUpdate20(update)
	if err != nil {
		return nil, err
	}
	newM, err := observeSuccessfulCommandLine(bcl20.dev.Model(), u20.writeModeenv)
	if err != nil {
		return nil, fmt.Errorf("cannot mark successful boot command line: %v", err)
	}
	u20.writeModeenv = newM
	return u20, nil
}

func trustedCommandLineBootState(dev Device) *bootState20CommandLine {
	return &bootState20CommandLine{
		dev: dev,
	}
}

// bootState20RecoverySystem implements the successfulBootState interface for
// tried recovery systems
type bootState20RecoverySystem struct {
	dev Device
}

func (brs20 *bootState20RecoverySystem) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	u20, err := toBootStateUpdate20(update)
	if err != nil {
		return nil, err
	}

	newM, err := observeSuccessfulSystems(u20.writeModeenv)
	if err != nil {
		return nil, fmt.Errorf("cannot mark successful recovery system: %v", err)
	}
	u20.writeModeenv = newM
	return u20, nil
}

func recoverySystemsBootState(dev Device) *bootState20RecoverySystem {
	return &bootState20RecoverySystem{dev: dev}
}

// bootState20Model implements the successfulBootState interface for device
// model related bookkeeping
type bootState20Model struct {
	dev Device
}

func (brs20 *bootState20Model) markSuccessful(update bootStateUpdate) (bootStateUpdate, error) {
	u20, err := toBootStateUpdate20(update)
	if err != nil {
		return nil, err
	}

	// sign key ID was not being populated in earlier versions of snapd, try
	// to remedy that
	if u20.modeenv.ModelSignKeyID == "" {
		if err != nil {
			return nil, err
		}
		u20.writeModeenv.ModelSignKeyID = brs20.dev.Model().SignKeyID()
	}
	return u20, nil
}

func modelBootState(dev Device) *bootState20Model {
	return &bootState20Model{dev: dev}
}
