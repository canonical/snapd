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

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/sysconfig"

	// to set sysconfig.ApplyFilesystemOnlyDefaultsImpl
	_ "github.com/snapcore/snapd/overlord/configstate/configcore"
)

func init() {
	const (
		short = "Generate mounts for the initramfs"
		long  = "Generate and perform all mounts for the initramfs before transitioning to userspace"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("initramfs-mounts", short, long, &cmdInitramfsMounts{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdInitramfsMounts struct{}

func (c *cmdInitramfsMounts) Execute(args []string) error {
	return generateInitramfsMounts()
}

var (
	osutilIsMounted = osutil.IsMounted

	snapTypeToMountDir = map[snap.Type]string{
		snap.TypeBase:   "base",
		snap.TypeKernel: "kernel",
		snap.TypeSnapd:  "snapd",
	}

	secbootMeasureSnapSystemEpochWhenPossible    func() error
	secbootMeasureSnapModelWhenPossible          func(findModel func() (*asserts.Model, error)) error
	secbootUnlockVolumeUsingSealedKeyIfEncrypted func(disk disks.Disk, name string, encryptionKeyDir string, lockKeysOnFinish bool) (string, bool, error)
	secbootUnlockEncryptedVolumeUsingKey         func(disk disks.Disk, name string, key []byte) (string, error)

	bootFindPartitionUUIDForBootedKernelDisk = boot.FindPartitionUUIDForBootedKernelDisk
)

func stampedAction(stamp string, action func() error) error {
	stampFile := filepath.Join(dirs.SnapBootstrapRunDir, stamp)
	if osutil.FileExists(stampFile) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(stampFile), 0755); err != nil {
		return err
	}
	if err := action(); err != nil {
		return err
	}
	return ioutil.WriteFile(stampFile, nil, 0644)
}

func generateInitramfsMounts() error {
	// Ensure there is a very early initial measurement
	err := stampedAction("secboot-epoch-measured", func() error {
		return secbootMeasureSnapSystemEpochWhenPossible()
	})
	if err != nil {
		return err
	}

	mode, recoverySystem, err := boot.ModeAndRecoverySystemFromKernelCommandLine()
	if err != nil {
		return err
	}

	mst := &initramfsMountsState{
		mode:           mode,
		recoverySystem: recoverySystem,
	}

	switch mode {
	case "recover":
		return generateMountsModeRecover(mst)
	case "install":
		return generateMountsModeInstall(mst)
	case "run":
		return generateMountsModeRun(mst)
	}
	// this should never be reached
	return fmt.Errorf("internal error: mode in generateInitramfsMounts not handled")
}

// generateMountsMode* is called multiple times from initramfs until it
// no longer generates more mount points and just returns an empty output.
func generateMountsModeInstall(mst *initramfsMountsState) error {
	// steps 1 and 2 are shared with recover mode
	if err := generateMountsCommonInstallRecover(mst); err != nil {
		return err
	}

	// 3. final step: write modeenv to tmpfs data dir and disable cloud-init in
	//   install mode
	modeEnv := &boot.Modeenv{
		Mode:           "install",
		RecoverySystem: mst.recoverySystem,
	}
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir); err != nil {
		return err
	}

	// done, no output, no error indicates to initramfs we are done with
	// mounting stuff
	return nil
}

// copyNetworkConfig copies the network configuration to the target
// directory. This is used to copy the network configuration
// data from a real uc20 ubuntu-data partition into a ephemeral one.
func copyNetworkConfig(src, dst string) error {
	for _, globEx := range []string{
		// for network configuration setup by console-conf, etc.
		// TODO:UC20: we want some way to "try" or "verify" the network
		//            configuration or to only use known-to-be-good network
		//            configuration i.e. from ubuntu-save before installing it
		//            onto recover mode, because the network configuration could
		//            have been what was broken so we don't want to break
		//            network configuration for recover mode as well, but for
		//            now this is fine
		"system-data/etc/netplan/*",
		// etc/machine-id is part of what systemd-networkd uses to generate a
		// DHCP clientid (the other part being the interface name), so to have
		// the same IP addresses across run mode and recover mode, we need to
		// also copy the machine-id across
		"system-data/etc/machine-id",
	} {
		if err := copyFromGlobHelper(src, dst, globEx); err != nil {
			return err
		}
	}
	return nil
}

// copyUbuntuDataMisc copies miscellaneous other files from the run mode system
// to the recover system such as:
//  - timesync clock to keep the same time setting in recover as in run mode
func copyUbuntuDataMisc(src, dst string) error {
	for _, globEx := range []string{
		// systemd's timesync clock file so that the time in recover mode moves
		// forward to what it was in run mode
		// NOTE: we don't sync back the time movement from recover mode to run
		// mode currently, unclear how/when we could do this, but recover mode
		// isn't meant to be long lasting and as such it's probably not a big
		// problem to "lose" the time spent in recover mode
		"system-data/var/lib/systemd/timesync/clock",
	} {
		if err := copyFromGlobHelper(src, dst, globEx); err != nil {
			return err
		}
	}

	return nil
}

// copyUbuntuDataAuth copies the authentication files like
//  - extrausers passwd,shadow etc
//  - sshd host configuration
//  - user .ssh dir
// to the target directory. This is used to copy the authentication
// data from a real uc20 ubuntu-data partition into a ephemeral one.
func copyUbuntuDataAuth(src, dst string) error {
	for _, globEx := range []string{
		"system-data/var/lib/extrausers/*",
		"system-data/etc/ssh/*",
		"user-data/*/.ssh/*",
		// this ensures we get proper authentication to snapd from "snap"
		// commands in recover mode
		"user-data/*/.snap/auth.json",
		// this ensures we also get non-ssh enabled accounts copied
		"user-data/*/.profile",
		// so that users have proper perms, i.e. console-conf added users are
		// sudoers
		"system-data/etc/sudoers.d/*",
	} {
		if err := copyFromGlobHelper(src, dst, globEx); err != nil {
			return err
		}
	}

	// ensure the user state is transferred as well
	srcState := filepath.Join(src, "system-data/var/lib/snapd/state.json")
	dstState := filepath.Join(dst, "system-data/var/lib/snapd/state.json")
	err := state.CopyState(srcState, dstState, []string{"auth.users", "auth.macaroon-key", "auth.last-id"})
	if err != nil && err != state.ErrNoState {
		return fmt.Errorf("cannot copy user state: %v", err)
	}

	return nil
}

// copySafeDefaultData will copy to the destination a "safe" set of data for
// a blank recover mode, i.e. one where we cannot copy authentication, etc. from
// the actual host ubuntu-data. Currently this is just a file to disable
// console-conf from running.
func copySafeDefaultData(dst string) error {
	consoleConfCompleteFile := filepath.Join(dst, "system-data/var/lib/console-conf/complete")
	if err := os.MkdirAll(filepath.Dir(consoleConfCompleteFile), 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(consoleConfCompleteFile, nil, 0644)
}

func copyFromGlobHelper(src, dst, globEx string) error {
	matches, err := filepath.Glob(filepath.Join(src, globEx))
	if err != nil {
		return err
	}
	for _, p := range matches {
		comps := strings.Split(strings.TrimPrefix(p, src), "/")
		for i := range comps {
			part := filepath.Join(comps[0 : i+1]...)
			fi, err := os.Stat(filepath.Join(src, part))
			if err != nil {
				return err
			}
			if fi.IsDir() {
				if err := os.Mkdir(filepath.Join(dst, part), fi.Mode()); err != nil && !os.IsExist(err) {
					return err
				}
				st, ok := fi.Sys().(*syscall.Stat_t)
				if !ok {
					return fmt.Errorf("cannot get stat data: %v", err)
				}
				if err := os.Chown(filepath.Join(dst, part), int(st.Uid), int(st.Gid)); err != nil {
					return err
				}
			} else {
				if err := osutil.CopyFile(p, filepath.Join(dst, part), osutil.CopyFlagPreserveAll); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

type recoverDegradedState struct {
	// TODO: make these values enums?
	// DataKey is which key was used to unlock ubuntu-data (if any). Valid
	// values are:
	// - "run" for the normal run mode key object
	// - "fallback" for the fallback recover mode specific key object
	// - "" for either unencrypted case, or if we were unable to unlock it
	//   (in the case we failed to unlock it, but we know it is there,
	//   the DataState field provides more information)
	DataKey string `json:"data-key"`
	// TODO: make these values enums?
	// DataState is the state of the ubuntu-data mountpoint, it can be:
	// - "mounted" for mounted and available
	// - "enc-not-found" for state where we found an ubuntu-data-enc
	//   partition and tried to unlock it, but failed _before mounting it_.
	//   Errors mounting are identified differently.
	// TODO:UC20: "other" is a really bad term here replace with something better
	// - "other" for some other error where we couldn't identify if there's an
	//    ubuntu-data (or ubuntu-data-enc) partition at all
	DataState string `json:"data-state"`
	// SaveKey is which key was used to unlock ubuntu-save (if any). Same values
	// as DataKey.
	SaveKey string `json:"save-key"`
	// SaveState is the state of the ubuntu-save mountpoint, it can be in any of
	// the states documented for DataState, with the additional state of:
	// - "not-needed" for state when we don't have ubuntu-save, but we don't need it (unencrypted only)
	SaveState string `json:"save-state"`
	// HostLocation is the location where the host's ubuntu-data is mounted
	// and available. If this is the empty string, then the host's
	// ubuntu-data is not available anywhere.
	HostLocation string `json:"host-location"`
}

type context struct {
	// the disk we have all our partitions on
	disk disks.Disk
	// options for finding partitions on the disk we have all our partitions on
	diskOpts *disks.Options
	// the device for ubuntu-data that is to be mounted - either an unencrypted
	// volume, or a decrypted mapper volume if ubuntu-data is really encrypted
	// on the physical disk
	dataDevice string
	// the device for ubuntu-save that is to be mounted
	saveDevice string

	// XXX: would be nice to get rid of tracking this here
	isEncryptedDev bool

	// state for tracking what happens
	degradedState recoverDegradedState
}

// TODO: should this be a method on *context ?
func verifyMountPointCtx(ctx *context, dir, name string) error {
	matches, err := ctx.disk.MountPointIsFromDisk(dir, ctx.diskOpts)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("cannot validate boot: %s mountpoint is expected to be from disk %s but is not", name, ctx.disk.Dev())
	}
	return nil
}

type stateFunc func(ctx *context) (stateFunc, error)

var (
	// errStateDone is the error returned when the state machine is done
	// executing without a critical error
	errStateDone = fmt.Errorf("state machine done")
)

type stateMachine struct {
	current stateFunc
}

func newStateMachine() *stateMachine {
	m := &stateMachine{}
	m.current = m.unlockDataRunKey
	return m
}

func (m *stateMachine) execute(ctx *context) error {
	next, err := m.current(ctx)
	m.current = next
	return err
}

// stateUnlockDataRunKey will try to unlock ubuntu-data with the normal run-mode
// key, and if it fails, progresses to the next state, which is either:
// - failed to unlock data, but we know it's an encrypted device -> try to unlock with fallback key
// - failed to find data at all -> try to unlock save
// - unlocked data with run key -> mount data
func (m *stateMachine) unlockDataRunKey(ctx *context) (stateFunc, error) {
	runModeKey := filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-data.sealed-key")
	dataDevice, isEncryptedDev, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(ctx.disk, "ubuntu-data", runModeKey, false)
	if isEncryptedDev {
		ctx.diskOpts = &disks.Options{
			IsDecryptedDevice: true,
		}
		ctx.isEncryptedDev = true
	}
	if err != nil {
		// we couldn't unlock ubuntu-data with the primary key, or we didn't
		// find it in the unencrypted case
		if isEncryptedDev {
			// we know the device is encrypted, so the next state is to try
			// unlocking with the fallback key
			return m.unlockDataFallbackKey, nil
		}

		// not an encrypted device, so nothing to fall back to try and unlock
		// data, so just mark it as not found and continue on to try and mount
		// an unencrypted ubuntu-save directly
		// TODO:UC20: should we save the specific error in degradedState
		//            somewhere in addition to logging it?
		logger.Noticef("failed to find ubuntu-data partition for mounting host data: %v", err)
		ctx.degradedState.DataState = "other"
		return m.locateUnencryptedSave, nil
	}

	// otherwise successfully unlocked it (if it was encrypted)
	ctx.dataDevice = dataDevice

	// successfully unlocked it with the run key, so just mark that in the
	// state and move on to trying to mount it
	if isEncryptedDev {
		ctx.degradedState.DataKey = "run"
	}

	return m.mountData, nil
}

func (m *stateMachine) unlockDataFallbackKey(ctx *context) (stateFunc, error) {
	dataFallbackKey := filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-data.recovery.sealed-key")
	// XXX: we don't check isDecryptDev here, if we are here then the previous
	// call trying to unlock ubuntu-data already said it was encrypted
	dataDevice, _, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(ctx.disk, "ubuntu-data", dataFallbackKey, false)
	if err != nil {
		// TODO: should we introspect err here to get a more detailed
		//       response in degradedState and in the log message?
		// we failed to decrypt the device again with the fallback key,
		// so we are for sure in degraded mode
		logger.Noticef("failed to find or unlock encrypted ubuntu-data partition for mounting host data: %v", err)
		ctx.degradedState.DataState = "enc-not-found"

		// skip trying to mount data
		return m.unlockSaveRunKey, nil
	}

	// we unlocked data with the fallback key, we are not in
	// "fully" degraded mode, but we do need to track that we had to
	// use the fallback key
	ctx.degradedState.DataKey = "fallback"
	ctx.dataDevice = dataDevice

	return m.mountData, nil
}

func (m *stateMachine) mountData(ctx *context) (stateFunc, error) {
	// don't do fsck on the data partition, it could be corrupted
	if err := doSystemdMount(ctx.dataDevice, boot.InitramfsHostUbuntuDataDir, nil); err != nil {
		// we failed to mount it, proceed with degraded mode
		ctx.degradedState.DataState = "not-mounted"

		// no point trying to unlock save with the run key, we need data to be
		// mounted for that and we failed to mount it
		return m.unlockSaveFallbackKey, nil
	}
	// we mounted it successfully, verify it comes from the right disk
	if err := verifyMountPointCtx(ctx, boot.InitramfsHostUbuntuDataDir, "ubuntu-data"); err != nil {
		return nil, err
	}

	ctx.degradedState.DataState = "mounted"
	ctx.degradedState.HostLocation = boot.InitramfsHostUbuntuDataDir

	// next step: try to unlock with run save key
	return m.unlockSaveRunKey, nil
}

func (m *stateMachine) locateUnencryptedSave(ctx *context) (stateFunc, error) {
	partUUID, err := ctx.disk.FindMatchingPartitionUUID("ubuntu-save")
	if err != nil {
		// error locating ubuntu-save
		if _, ok := err.(disks.FilesystemLabelNotFoundError); ok {
			// this is ok, ubuntu-save may not exist for
			// non-encrypted device
			ctx.degradedState.SaveState = "not-needed"
		} else {
			// the error is not "not-found", so we have a real error
			// identifying whether save exists or not
			logger.Noticef("error identifying ubuntu-save partition: %v", err)
			ctx.degradedState.SaveState = "other"
		}

		// all done, nothing left to try and mount
		return nil, errStateDone
	}

	// we found the unencrypted device, now mount it
	ctx.saveDevice = filepath.Join("/dev/disk/by-partuuid", partUUID)
	return m.mountSave, nil
}

func (m *stateMachine) unlockSaveRunKey(ctx *context) (stateFunc, error) {
	// XXX: would be nice to not need this redirect here
	if !ctx.isEncryptedDev {
		return m.locateUnencryptedSave, nil
	}

	// to get to this state, we needed to have mounted ubuntu-data on host, so
	// if encrypted, we can try to read the run key from host ubuntu-data
	saveKey := filepath.Join(dirs.SnapFDEDirUnder(boot.InitramfsHostWritableDir), "ubuntu-save.key")
	key, err := ioutil.ReadFile(saveKey)
	if err != nil {
		// log the error and skip to trying the fallback key

		// XXX: do we need to log this?
		logger.Noticef("couldn't access run ubuntu-save key: %v", err)
		return m.unlockSaveFallbackKey, nil
	}

	saveDevice, err := secbootUnlockEncryptedVolumeUsingKey(ctx.disk, "ubuntu-save", key)
	if err != nil {
		// failed to unlock with run key, try fallback key
		return m.unlockSaveFallbackKey, nil
	}

	// unlocked it properly, go mount it
	ctx.degradedState.SaveKey = "run"
	ctx.saveDevice = saveDevice
	return m.mountSave, nil
}

func (m *stateMachine) unlockSaveFallbackKey(ctx *context) (stateFunc, error) {
	// we don't have ubuntu-data host to get the unsealed "bare" key, so
	// we have to unlock with the sealed one from ubuntu-seed
	saveFallbackKey := filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-save.recovery.sealed-key")
	saveDevice, isEncryptedDev, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(ctx.disk, "ubuntu-save", saveFallbackKey, true)
	if err != nil {
		if isEncryptedDev {
			logger.Noticef("failed to find or unlock encrypted ubuntu-save partition: %v", err)
			ctx.degradedState.SaveState = "enc-not-found"
		} else {
			// either catastrophic error or inconsistent disk, if
			// ubuntu-data was an encrypted device, then ubuntu-save
			// must be also
			logger.Noticef("cannot unlock ubuntu-save: %v", err)
			ctx.degradedState.SaveState = "other"
		}

		return nil, errStateDone
	}

	ctx.degradedState.SaveKey = "fallback"
	ctx.saveDevice = saveDevice

	return m.mountSave, nil
}

func (m *stateMachine) mountSave(ctx *context) (stateFunc, error) {
	// TODO: should we fsck ubuntu-save ?
	if err := doSystemdMount(ctx.saveDevice, boot.InitramfsUbuntuSaveDir, nil); err != nil {
		logger.Noticef("error mounting ubuntu-save from partition %s: %v", ctx.saveDevice, err)
		ctx.degradedState.SaveState = "not-mounted"
	} else {
		// if we couldn't verify whether the mounted save is valid, bail out of
		// the state machine and exit snap-bootstrap
		if err := verifyMountPointCtx(ctx, boot.InitramfsUbuntuSaveDir, "ubuntu-save"); err != nil {
			return nil, err
		}

		ctx.degradedState.SaveState = "mounted"
	}

	return nil, errStateDone
}

func generateMountsModeRecover(mst *initramfsMountsState) error {
	// steps 1 and 2 are shared with install mode
	if err := generateMountsCommonInstallRecover(mst); err != nil {
		return err
	}

	// get the disk that we mounted the ubuntu-seed partition from as a
	// reference point for future mounts
	disk, err := disks.DiskFromMountPoint(boot.InitramfsUbuntuSeedDir, nil)
	if err != nil {
		return err
	}

	// 3. run the state machine logic for mounting partitions, this involves
	//    trying to unlock then mount ubuntu-data, and then unlocking and
	//    mounting ubuntu-save
	//    see the state* functions for details of what each step does and
	//    possible transition points

	// TODO: should we just put a diagram here explaining the states too?

	ctx := &context{
		disk:          disk,
		degradedState: recoverDegradedState{},
	}

	// first state to execute is to unlock ubuntu-data with the run key
	machine := newStateMachine()
	for {
		err := machine.execute(ctx)
		if err != nil {
			if err == errStateDone {
				break
			}
			return err
		}
	}

	// 3.1 write out degraded.json
	b, err := json.Marshal(ctx.degradedState)
	if err != nil {
		return err
	}

	// needed?
	err = os.MkdirAll(boot.InitramfsHostUbuntuDataDir, 0755)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join(boot.InitramfsHostUbuntuDataDir, "degraded.json"), b, 0644)
	if err != nil {
		return err
	}

	// 4. final step: copy the auth data and network config from
	//    the real ubuntu-data dir to the ephemeral ubuntu-data
	//    dir, write the modeenv to the tmpfs data, and disable
	//    cloud-init in recover mode

	// if we have the host location, then we were able to successfully mount
	// ubuntu-data, and as such we can proceed with copying files from there
	// onto the tmpfs
	if ctx.degradedState.HostLocation != "" {
		if err := copyUbuntuDataAuth(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
			return err
		}
		if err := copyNetworkConfig(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
			return err
		}
		if err := copyUbuntuDataMisc(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
			return err
		}
	} else {
		// we don't have ubuntu-data host mountpoint, so we should setup safe
		// defaults for i.e. console-conf in the running image to block
		// attackers from accessing the system - just because we can't access
		// ubuntu-data doesn't mean that attackers wouldn't be able to if they
		// could login

		if err := copySafeDefaultData(boot.InitramfsHostUbuntuDataDir); err != nil {
			return err
		}
	}

	modeEnv := &boot.Modeenv{
		Mode:           "recover",
		RecoverySystem: mst.recoverySystem,
	}
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir); err != nil {
		return err
	}

	// finally we need to modify the bootenv to mark the system as successful,
	// this ensures that when you reboot from recover mode without doing
	// anything else, you are auto-transitioned back to run mode
	// TODO:UC20: as discussed unclear we need to pass the recovery system here
	if err := boot.EnsureNextBootToRunMode(mst.recoverySystem); err != nil {
		return err
	}

	// done, no output, no error indicates to initramfs we are done with
	// mounting stuff
	return nil
}

// mountPartitionMatchingKernelDisk will select the partition to mount at dir,
// using the boot package function FindPartitionUUIDForBootedKernelDisk to
// determine what partition the booted kernel came from. If which disk the
// kernel came from cannot be determined, then it will fallback to mounting via
// the specified disk label.
func mountPartitionMatchingKernelDisk(dir, fallbacklabel string) error {
	partuuid, err := bootFindPartitionUUIDForBootedKernelDisk()
	// TODO: the by-partuuid is only available on gpt disks, on mbr we need
	//       to use by-uuid or by-id
	partSrc := filepath.Join("/dev/disk/by-partuuid", partuuid)
	if err != nil {
		// no luck, try mounting by label instead
		partSrc = filepath.Join("/dev/disk/by-label", fallbacklabel)
	}

	opts := &systemdMountOptions{
		// always fsck the partition when we are mounting it, as this is the
		// first partition we will be mounting, we can't know if anything is
		// corrupted yet
		NeedsFsck: true,
	}
	return doSystemdMount(partSrc, dir, opts)
}

func generateMountsCommonInstallRecover(mst *initramfsMountsState) error {
	// 1. always ensure seed partition is mounted first before the others,
	//      since the seed partition is needed to mount the snap files there
	if err := mountPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "ubuntu-seed"); err != nil {
		return err
	}

	// load model and verified essential snaps metadata
	typs := []snap.Type{snap.TypeBase, snap.TypeKernel, snap.TypeSnapd, snap.TypeGadget}
	model, essSnaps, err := mst.ReadEssential("", typs)
	if err != nil {
		return fmt.Errorf("cannot load metadata and verify essential bootstrap snaps %v: %v", typs, err)
	}

	// 2.1. measure model
	err = stampedAction(fmt.Sprintf("%s-model-measured", mst.recoverySystem), func() error {
		return secbootMeasureSnapModelWhenPossible(func() (*asserts.Model, error) {
			return model, nil
		})
	})
	if err != nil {
		return err
	}

	// 2.2. (auto) select recovery system and mount seed snaps
	// TODO:UC20: do we need more cross checks here?
	for _, essentialSnap := range essSnaps {
		if essentialSnap.EssentialType == snap.TypeGadget {
			// don't need to mount the gadget anywhere, but we use the snap
			// later hence it is loaded
			continue
		}
		dir := snapTypeToMountDir[essentialSnap.EssentialType]
		// TODO:UC20: we need to cross-check the kernel path with snapd_recovery_kernel used by grub
		if err := doSystemdMount(essentialSnap.Path, filepath.Join(boot.InitramfsRunMntDir, dir), nil); err != nil {
			return err
		}
	}

	// TODO:UC20: after we have the kernel and base snaps mounted, we should do
	//            the bind mounts from the kernel modules on top of the base
	//            mount and delete the corresponding systemd units from the
	//            initramfs layout

	// TODO:UC20: after the kernel and base snaps are mounted, we should setup
	//            writable here as well to take over from "the-modeenv" script
	//            in the initrd too

	// TODO:UC20: after the kernel and base snaps are mounted and writable is
	//            mounted, we should also implement writable-paths here too as
	//            writing it in Go instead of shellscript is desirable

	// 2.3. mount "ubuntu-data" on a tmpfs
	mntOpts := &systemdMountOptions{
		Tmpfs: true,
	}
	err = doSystemdMount("tmpfs", boot.InitramfsDataDir, mntOpts)
	if err != nil {
		return err
	}

	// finally get the gadget snap from the essential snaps and use it to
	// configure the ephemeral system
	// should only be one seed snap
	gadgetPath := ""
	for _, essentialSnap := range essSnaps {
		if essentialSnap.EssentialType == snap.TypeGadget {
			gadgetPath = essentialSnap.Path
		}
	}
	gadgetSnap := squashfs.New(gadgetPath)

	// we need to configure the ephemeral system with defaults and such using
	// from the seed gadget
	configOpts := &sysconfig.Options{
		// never allow cloud-init to run inside the ephemeral system, in the
		// install case we don't want it to ever run, and in the recover case
		// cloud-init will already have run in run mode, so things like network
		// config and users should already be setup and we will copy those
		// further down in the setup for recover mode
		AllowCloudInit: false,
		TargetRootDir:  boot.InitramfsWritableDir,
		GadgetSnap:     gadgetSnap,
	}
	return sysconfig.ConfigureTargetSystem(configOpts)
}

func maybeMountSave(disk disks.Disk, rootdir string, encrypted bool, mountOpts *systemdMountOptions) (haveSave bool, err error) {
	var saveDevice string
	if encrypted {
		saveKey := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "ubuntu-save.key")
		// if ubuntu-save exists and is encrypted, the key has been created during install
		if !osutil.FileExists(saveKey) {
			// ubuntu-data is encrypted, but we appear to be missing
			// a key to open ubuntu-save
			return false, fmt.Errorf("cannot find ubuntu-save encryption key at %v", saveKey)
		}
		// we have save.key, volume exists and is encrypted
		key, err := ioutil.ReadFile(saveKey)
		if err != nil {
			return true, err
		}
		saveDevice, err = secbootUnlockEncryptedVolumeUsingKey(disk, "ubuntu-save", key)
		if err != nil {
			return true, fmt.Errorf("cannot unlock ubuntu-save volume: %v", err)
		}
	} else {
		partUUID, err := disk.FindMatchingPartitionUUID("ubuntu-save")
		if err != nil {
			if _, ok := err.(disks.FilesystemLabelNotFoundError); ok {
				// this is ok, ubuntu-save may not exist for
				// non-encrypted device
				return false, nil
			}
			return false, err
		}
		saveDevice = filepath.Join("/dev/disk/by-partuuid", partUUID)
	}
	if err := doSystemdMount(saveDevice, boot.InitramfsUbuntuSaveDir, mountOpts); err != nil {
		return true, err
	}
	return true, nil
}

func generateMountsModeRun(mst *initramfsMountsState) error {
	// 1. mount ubuntu-boot
	if err := mountPartitionMatchingKernelDisk(boot.InitramfsUbuntuBootDir, "ubuntu-boot"); err != nil {
		return err
	}

	// get the disk that we mounted the ubuntu-boot partition from as a
	// reference point for future mounts
	disk, err := disks.DiskFromMountPoint(boot.InitramfsUbuntuBootDir, nil)
	if err != nil {
		return err
	}

	// 2. mount ubuntu-seed
	// use the disk we mounted ubuntu-boot from as a reference to find
	// ubuntu-seed and mount it
	partUUID, err := disk.FindMatchingPartitionUUID("ubuntu-seed")
	if err != nil {
		return err
	}

	// fsck is safe to run on ubuntu-seed as per the manpage, it should not
	// meaningfully contribute to corruption if we fsck it every time we boot,
	// and it is important to fsck it because it is vfat and mounted writable
	// TODO:UC20: mount it as read-only here and remount as writable when we
	//            need it to be writable for i.e. transitioning to recover mode
	fsckSystemdOpts := &systemdMountOptions{
		NeedsFsck: true,
	}
	if err := doSystemdMount(fmt.Sprintf("/dev/disk/by-partuuid/%s", partUUID), boot.InitramfsUbuntuSeedDir, fsckSystemdOpts); err != nil {
		return err
	}

	// 3.1. measure model
	err = stampedAction("run-model-measured", func() error {
		return secbootMeasureSnapModelWhenPossible(mst.UnverifiedBootModel)
	})
	if err != nil {
		return err
	}
	// TODO:UC20: cross check the model we read from ubuntu-boot/model with
	// one recorded in ubuntu-data modeenv during install

	// 3.2. mount Data
	// TODO: we need to decide when to lock keys
	const lockKeysOnFinish = true
	runModeKey := filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-data.sealed-key")
	device, isDecryptDev, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-data", runModeKey, lockKeysOnFinish)
	if err != nil {
		return err
	}

	// TODO: do we actually need fsck if we are mounting a mapper device?
	// probably not?
	if err := doSystemdMount(device, boot.InitramfsDataDir, fsckSystemdOpts); err != nil {
		return err
	}

	// 3.3. mount ubuntu-save (if present)
	haveSave, err := maybeMountSave(disk, boot.InitramfsWritableDir, isDecryptDev, fsckSystemdOpts)
	if err != nil {
		return err
	}

	// 4.1 verify that ubuntu-data comes from where we expect it to
	diskOpts := &disks.Options{}
	if isDecryptDev {
		// then we need to specify that the data mountpoint is expected to be a
		// decrypted device, applies to both ubuntu-data and ubuntu-save
		diskOpts.IsDecryptedDevice = true
	}

	matches, err := disk.MountPointIsFromDisk(boot.InitramfsDataDir, diskOpts)
	if err != nil {
		return err
	}
	if !matches {
		// failed to verify that ubuntu-data mountpoint comes from the same disk
		// as ubuntu-boot
		return fmt.Errorf("cannot validate boot: ubuntu-data mountpoint is expected to be from disk %s but is not", disk.Dev())
	}
	if haveSave {
		// 4.1a we have ubuntu-save, verify it as well
		matches, err = disk.MountPointIsFromDisk(boot.InitramfsUbuntuSaveDir, diskOpts)
		if err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("cannot validate boot: ubuntu-save mountpoint is expected to be from disk %s but is not", disk.Dev())
		}
	}

	// 4.2. read modeenv
	modeEnv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
	if err != nil {
		return err
	}

	typs := []snap.Type{snap.TypeBase, snap.TypeKernel}

	// 4.2 choose base and kernel snaps (this includes updating modeenv if
	//     needed to try the base snap)
	mounts, err := boot.InitramfsRunModeSelectSnapsToMount(typs, modeEnv)
	if err != nil {
		return err
	}

	// TODO:UC20: with grade > dangerous, verify the kernel snap hash against
	//            what we booted using the tpm log, this may need to be passed
	//            to the function above to make decisions there, or perhaps this
	//            code actually belongs in the bootloader implementation itself

	// 4.3 mount base and kernel snaps
	// make sure this is a deterministic order
	for _, typ := range []snap.Type{snap.TypeBase, snap.TypeKernel} {
		if sn, ok := mounts[typ]; ok {
			dir := snapTypeToMountDir[typ]
			snapPath := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), sn.Filename())
			if err := doSystemdMount(snapPath, filepath.Join(boot.InitramfsRunMntDir, dir), nil); err != nil {
				return err
			}
		}
	}

	// 4.4 mount snapd snap only on first boot
	if modeEnv.RecoverySystem != "" {
		// load the recovery system and generate mount for snapd
		_, essSnaps, err := mst.ReadEssential(modeEnv.RecoverySystem, []snap.Type{snap.TypeSnapd})
		if err != nil {
			return fmt.Errorf("cannot load metadata and verify snapd snap: %v", err)
		}

		return doSystemdMount(essSnaps[0].Path, filepath.Join(boot.InitramfsRunMntDir, "snapd"), nil)
	}

	return nil
}
