// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/snap"
)

// Unlocker functions are passed from code using boot to indicate that global
// state should be unlocked during slow operations, e.g sealing/unsealing.
// Boot code is then expected to call the unlocker around the slow section and
// relock using the returned function. Unlocker being nil indicates not to do
// this.
type Unlocker func() (relock func())

const (
	// DefaultStatus is the value of a status boot variable when nothing is
	// being tried
	DefaultStatus = ""
	// TryStatus is the value of a status boot variable when something is about
	// to be tried
	TryStatus = "try"
	// TryingStatus is the value of a status boot variable after we have
	// attempted a boot with a try snap - this status is only set in the early
	// boot sequence (bootloader, initramfs, etc.)
	TryingStatus = "trying"
)

// RebootInfo contains information about how to perform a reboot if
// required.
type RebootInfo struct {
	// RebootRequired is true if we need to reboot after an update.
	RebootRequired bool
	// BootloaderOptions will be used to find the correct bootloader when
	// checking for any set reboot arguments.
	BootloaderOptions *bootloader.Options
}

// NextBootContext carries additional significative information used when
// setting the next boot.
type NextBootContext struct {
	// BootWithoutTry is sets if we don't want to use the "try" logic. This
	// is useful if the next boot is part of an installation undo.
	BootWithoutTry bool
}

// A BootParticipant handles the boot process details for a snap involved in it.
type BootParticipant interface {
	// SetNextBoot will schedule the snap to be used in the next
	// boot. bootCtx contains context information that influences how the
	// next boot is performed. For base snaps it is up to the caller to
	// select the right bootable base (from the model assertion). It is a
	// noop for not relevant snaps.  Otherwise it returns whether a reboot
	// is required.
	SetNextBoot(bootCtx NextBootContext) (rebootInfo RebootInfo, err error)

	// Is this a trivial implementation of the interface?
	IsTrivial() bool
}

// A BootKernel handles the bootloader setup of a kernel.
type BootKernel interface {
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

func (trivial) SetNextBoot(bootCtx NextBootContext) (RebootInfo, error) {
	return RebootInfo{RebootRequired: false}, nil
}
func (trivial) IsTrivial() bool                          { return true }
func (trivial) RemoveKernelAssets() error                { return nil }
func (trivial) ExtractKernelAssets(snap.Container) error { return nil }

// ensure trivial is a BootParticipant
var _ BootParticipant = trivial{}

// ensure trivial is a Kernel
var _ BootKernel = trivial{}

// Participant figures out what the BootParticipant is for the given
// arguments, and returns it. If the snap does _not_ participate in
// the boot process, the returned object will be a NOP, so it's safe
// to call anything on it always.
//
// Currently, on classic, nothing is a boot participant (returned will
// always be NOP).
func Participant(s snap.PlaceInfo, t snap.Type, dev snap.Device) BootParticipant {
	if applicable(s, t, dev) {
		bs := mylog.Check2(bootStateFor(t, dev))

		// all internal errors at this point

		return &coreBootParticipant{s: s, bs: bs}
	}
	return trivial{}
}

// bootloaderOptionsForDeviceKernel returns a set of bootloader options that
// enable correct kernel extraction and removal for given device
func bootloaderOptionsForDeviceKernel(dev snap.Device) *bootloader.Options {
	if !dev.HasModeenv() {
		return nil
	}
	// find the run-mode bootloader with its kernel support for UC20
	return &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
}

// Kernel checks that the given arguments refer to a kernel snap
// that participates in the boot process, and returns the associated
// BootKernel, or a trivial implementation otherwise.
func Kernel(s snap.PlaceInfo, t snap.Type, dev snap.Device) BootKernel {
	if t == snap.TypeKernel && applicable(s, t, dev) {
		return &coreKernel{s: s, bopts: bootloaderOptionsForDeviceKernel(dev)}
	}
	return trivial{}
}

// SnapTypeParticipatesInBoot returns whether a snap type participates in the
// boot for a given device.
func SnapTypeParticipatesInBoot(t snap.Type, dev snap.Device) bool {
	if dev.IsClassicBoot() {
		return false
	}
	switch t {
	case snap.TypeBase, snap.TypeOS:
		// Bases are not boot participants for classic with modes
		return !dev.Classic()
	case snap.TypeKernel, snap.TypeGadget:
		return true
	}

	return false
}

func applicable(s snap.PlaceInfo, t snap.Type, dev snap.Device) bool {
	if !SnapTypeParticipatesInBoot(t, dev) {
		return false
	}
	// In ephemeral modes we never need to care about updating the boot
	// config. This will be done via boot.MakeBootable().
	if !dev.RunMode() {
		return false
	}

	switch t {
	case snap.TypeKernel:
		if s.InstanceName() != dev.Kernel() {
			// a remodel might leave behind installed a kernel that
			// is not the device kernel anymore, ignore such a
			// kernel by checking the name
			return false
		}
	case snap.TypeBase, snap.TypeOS:
		base := dev.Base()
		if base == "" {
			base = "core"
		}
		if s.InstanceName() != base {
			return false
		}
	case snap.TypeGadget:
		// First condition: gadget is not a boot participant for UC16/18
		// Second condition: a remodel might leave behind installed a
		// gadget that is not the device gadget anymore, ignore such a
		// gadget by checking the name
		if !dev.HasModeenv() || s.InstanceName() != dev.Gadget() {
			return false
		}
	default:
		return false
	}

	return true
}

// bootState exposes the boot state for a type of boot snap during
// normal running state, i.e. after the pivot_root and after the initramfs.
type bootState interface {
	// revisions retrieves the revisions of the current snap and
	// the try snap (only the latter might not be set), and
	// the status of the trying snap.
	// Note that the error could be only specific to the try snap, in which case
	// curSnap may still be non-nil and valid. Callers concerned with robustness
	// should always inspect a non-nil error with isTrySnapError, and use
	// curSnap instead if the error is only for the trySnap or tryingStatus.
	revisions() (curSnap, trySnap snap.PlaceInfo, tryingStatus string, err error)

	// setNext lazily implements setting the next boot target for the type's
	// boot snap. bootCtx specifies additional information bits we might
	// need. Actually committing the update is done via the returned
	// bootStateUpdate's commit method. It will return information for
	// rebooting if necessary.
	setNext(s snap.PlaceInfo, bootCtx NextBootContext) (rbi RebootInfo, u bootStateUpdate, err error)

	// markSuccessful lazily implements marking the boot
	// successful for the type's boot snap. The actual committing
	// of the update is done via bootStateUpdate's commit, that
	// way different markSuccessful can be folded together.
	markSuccessful(bootStateUpdate) (bootStateUpdate, error)
}

// successfulBootState exposes the state of resources requiring bookkeeping on a
// successful boot.
type successfulBootState interface {
	// markSuccessful lazily implements marking the boot
	// successful for the given type of resource.
	markSuccessful(bootStateUpdate) (bootStateUpdate, error)
}

// bootStateFor finds the right bootState implementation of the given
// snap type and Device, if applicable.
func bootStateFor(typ snap.Type, dev snap.Device) (s bootState, err error) {
	if !dev.RunMode() {
		return nil, fmt.Errorf("internal error: no boot state handling for ephemeral modes")
	}
	if typ == snap.TypeOS {
		typ = snap.TypeBase
	}
	newBootState := newBootState16
	participantTypes := []snap.Type{snap.TypeBase, snap.TypeKernel}
	if dev.HasModeenv() {
		newBootState = newBootState20
		participantTypes = append(participantTypes, snap.TypeGadget)
	}
	for _, partTyp := range participantTypes {
		if typ == partTyp {
			return newBootState(typ, dev), nil
		}
	}
	return nil, fmt.Errorf("internal error: no boot state handling for snap type %q", typ)
}

// InUseFunc is a function to check if the snap is in use or not.
type InUseFunc func(name string, rev snap.Revision) bool

func fixedInUse(inUse bool) InUseFunc {
	return func(string, snap.Revision) bool {
		return inUse
	}
}

// InUse returns a checker for whether a given name/revision is used in the
// boot environment for snaps of the relevant snap type.
func InUse(typ snap.Type, dev snap.Device) (InUseFunc, error) {
	modeenvLock()
	defer modeenvUnlock()

	if !dev.RunMode() {
		// ephemeral mode, block manipulations for now
		return fixedInUse(true), nil
	}
	if !SnapTypeParticipatesInBoot(typ, dev) || typ == snap.TypeGadget {
		return fixedInUse(false), nil
	}
	cands := make([]snap.PlaceInfo, 0, 2)
	s := mylog.Check2(bootStateFor(typ, dev))

	cand, tryCand, _ := mylog.Check4(s.revisions())

	cands = append(cands, cand)
	if tryCand != nil {
		cands = append(cands, tryCand)
	}

	return func(name string, rev snap.Revision) bool {
		for _, cand := range cands {
			if cand.SnapName() == name && cand.SnapRevision() == rev {
				return true
			}
		}
		return false
	}, nil
}

// ErrBootNameAndRevisionNotReady is returned when the boot revision is not
// established yet.
var ErrBootNameAndRevisionNotReady = errors.New("boot revision not yet established")

// GetCurrentBoot returns the currently set name and revision for boot for the given
// type of snap, which can be snap.TypeBase (or snap.TypeOS), or snap.TypeKernel.
// Returns ErrBootNameAndRevisionNotReady if the values are temporarily not established.
func GetCurrentBoot(t snap.Type, dev snap.Device) (snap.PlaceInfo, error) {
	modeenvLock()
	defer modeenvUnlock()

	s := mylog.Check2(bootStateFor(t, dev))

	snap, _, status := mylog.Check4(s.revisions())

	if status == TryingStatus {
		return nil, ErrBootNameAndRevisionNotReady
	}

	return snap, nil
}

// bootStateUpdate carries the state for an on-going boot state update.
// At the end it can be used to commit it.
type bootStateUpdate interface {
	commit() error
}

// MarkBootSuccessful marks the current boot as successful. This means
// that snappy will consider this combination of kernel/os a valid
// target for rollback.
//
// The states that a boot goes through for UC16/18 are the following:
//   - By default snap_mode is "" in which case the bootloader loads
//     two squashfs'es denoted by variables snap_core and snap_kernel.
//   - On a refresh of core/kernel snapd will set snap_mode=try and
//     will also set snap_try_{core,kernel} to the core/kernel that
//     will be tried next.
//   - On reboot the bootloader will inspect the snap_mode and if the
//     mode is set to "try" it will set "snap_mode=trying" and then
//     try to boot the snap_try_{core,kernel}".
//   - On a successful boot snapd resets snap_mode to "" and copies
//     snap_try_{core,kernel} to snap_{core,kernel}. The snap_try_*
//     values are cleared afterwards.
//   - On a failing boot the bootloader will see snap_mode=trying which
//     means snapd did not start successfully. In this case the bootloader
//     will set snap_mode="" and the system will boot with the known good
//     values from snap_{core,kernel}
func MarkBootSuccessful(dev snap.Device) error {
	modeenvLock()
	defer modeenvUnlock()

	const errPrefix = "cannot mark boot successful: %s"

	var u bootStateUpdate
	for _, t := range []snap.Type{snap.TypeBase, snap.TypeKernel} {
		if !SnapTypeParticipatesInBoot(t, dev) {
			continue
		}
		s := mylog.Check2(bootStateFor(t, dev))

		u = mylog.Check2(s.markSuccessful(u))

	}

	if dev.HasModeenv() {
		for _, bs := range []successfulBootState{
			trustedAssetsBootState(dev),
			trustedCommandLineBootState(dev),
			recoverySystemsBootState(dev),
			modelBootState(dev),
		} {
			u = mylog.Check2(bs.markSuccessful(u))
		}
	}

	if u != nil {
		mylog.Check(u.commit())
	}
	return nil
}

var ErrUnsupportedSystemMode = errors.New("system mode is unsupported")

// SetRecoveryBootSystemAndMode configures the recovery bootloader to boot into
// the given recovery system in a particular mode. Returns
// ErrUnsupportedSystemMode when booting into a recovery system is not supported
// by the device.
func SetRecoveryBootSystemAndMode(dev snap.Device, systemLabel, mode string) error {
	if !dev.HasModeenv() {
		// only UC20 devices are supported
		return ErrUnsupportedSystemMode
	}
	if systemLabel == "" {
		return fmt.Errorf("internal error: system label is unset")
	}
	if mode == "" {
		return fmt.Errorf("internal error: system mode is unset")
	}

	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	// TODO:UC20: should the recovery partition stay around as RW during run
	// mode all the time?
	bl := mylog.Check2(bootloader.Find(InitramfsUbuntuSeedDir, opts))

	m := map[string]string{
		"snapd_recovery_system": systemLabel,
		"snapd_recovery_mode":   mode,
	}
	return bl.SetBootVars(m)
}

// UpdateManagedBootConfigs updates managed boot config assets if
// those are present for the ubuntu-boot bootloader. To do this it
// needs information from the model, the gadget we are updating to,
// and any additional kernel command line arguments coming from system
// options. Returns true when an update was carried out.
func UpdateManagedBootConfigs(dev snap.Device, gadgetSnapOrDir, cmdlineAppend string) (updated bool, err error) {
	if !dev.HasModeenv() {
		// only UC20 devices use managed boot config
		return false, nil
	}
	if !dev.RunMode() {
		return false, fmt.Errorf("internal error: boot config can only be updated in run mode")
	}
	modeenvLock()
	defer modeenvUnlock()

	return updateManagedBootConfigForBootloader(dev, ModeRun, gadgetSnapOrDir, cmdlineAppend)
}

func updateCmdlineVars(tbl bootloader.TrustedAssetsBootloader, gadgetSnapOrDir, cmdlineAppend string, candidate bool, dev snap.Device) error {
	defaultCmdLine := mylog.Check2(tbl.DefaultCommandLine(candidate))

	cmdlineVars := mylog.Check2(bootVarsForTrustedCommandLineFromGadget(gadgetSnapOrDir, cmdlineAppend, defaultCmdLine, dev.Model()))
	mylog.Check(tbl.SetBootVars(cmdlineVars))

	return nil
}

func updateManagedBootConfigForBootloader(dev snap.Device, mode, gadgetSnapOrDir, cmdlineAppend string) (updated bool, err error) {
	if mode != ModeRun {
		return false, fmt.Errorf("internal error: updating boot config of recovery bootloader is not supported yet")
	}

	opts := &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	}
	tbl := mylog.Check2(getBootloaderManagingItsAssets(InitramfsUbuntuBootDir, opts))

	// we're not managing this bootloader's boot config

	// boot config update can lead to a change of kernel command line
	cmdlineChange := mylog.Check2(observeCommandLineUpdate(dev.Model(), commandLineUpdateReasonSnapd, gadgetSnapOrDir, cmdlineAppend))

	if cmdlineChange {
		candidate := true
		mylog.Check(updateCmdlineVars(tbl, gadgetSnapOrDir, cmdlineAppend, candidate, dev))

	}

	assetChange := mylog.Check2(tbl.UpdateBootConfig())

	return assetChange || cmdlineChange, nil
}

// UpdateCommandLineForGadgetComponent handles the update of a gadget
// that contributes to the kernel command line of the run system
// (appending any additional kernel command line arguments coming from
// system options). Returns true when a change in command line has
// been observed and a reboot is needed. The reboot, if needed, should
// be requested at the the earliest possible occasion.
func UpdateCommandLineForGadgetComponent(dev snap.Device, gadgetSnapOrDir, cmdlineAppend string) (needsReboot bool, err error) {
	if !dev.HasModeenv() {
		// only UC20 devices are supported
		return false, fmt.Errorf("internal error: command line component cannot be updated on pre-UC20 devices")
	}
	modeenvLock()
	defer modeenvUnlock()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	// TODO: add support for bootloaders that that do not have any managed
	// assets
	tbl := mylog.Check2(getBootloaderManagingItsAssets("", opts))

	// we're not managing this bootloader's boot config

	// gadget update can lead to a change of kernel command line
	cmdlineChange := mylog.Check2(observeCommandLineUpdate(dev.Model(), commandLineUpdateReasonGadget, gadgetSnapOrDir, cmdlineAppend))

	if !cmdlineChange {
		return false, nil
	}

	candidate := false
	mylog.Check(updateCmdlineVars(tbl, gadgetSnapOrDir, cmdlineAppend, candidate, dev))

	return cmdlineChange, nil
}

// MarkFactoryResetComplete runs a series of steps in a run system that complete a
// factory reset process.
func MarkFactoryResetComplete(encrypted bool) error {
	if !encrypted {
		// there is nothing to do on an unencrypted system
		return nil
	}
	mylog.Check(postFactoryResetCleanup())

	return nil
}
