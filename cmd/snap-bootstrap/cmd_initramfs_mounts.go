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
	"crypto/subtle"
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
	"github.com/snapcore/snapd/secboot"
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
	secbootUnlockVolumeUsingSealedKeyIfEncrypted func(disk disks.Disk, name string, encryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error)
	secbootUnlockEncryptedVolumeUsingKey         func(disk disks.Disk, name string, key []byte) (string, error)

	secbootLockTPMSealedKeys func() error

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
	if _, err := generateMountsCommonInstallRecover(mst); err != nil {
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

// states for partition state
const (
	// states for LocateState
	partitionFound      = "found"
	partitionNotFound   = "not-found"
	partitionErrFinding = "error-finding"
	// states for MountState
	partitionMounted          = "mounted"
	partitionErrMounting      = "error-mounting"
	partitionAbsentOptional   = "absent-but-optional"
	partitionMountedUntrusted = "mounted-untrusted"
	// states for UnlockState
	partitionUnlocked     = "unlocked"
	partitionErrUnlocking = "error-unlocking"
	// keys used to unlock for UnlockKey
	keyRun      = "run"
	keyFallback = "fallback"
	keyRecovery = "recovery"
)

// partitionState is the state of a partition after recover mode has completed
// for degraded mode.
type partitionState struct {
	// MountState is whether the partition was mounted successfully or not.
	MountState string `json:"mount-state,omitempty"`
	// MountLocation is where the partition was mounted.
	MountLocation string `json:"mount-location,omitempty"`
	// Device is what device the partition corresponds to.
	Device string `json:"device,omitempty"`
	// FindState indicates whether the partition was found on the disk or not.
	FindState string `json:"find-state,omitempty"`
	// UnlockState was whether the partition was unlocked successfully or not.
	UnlockState string `json:"unlock-state,omitempty"`
	// UnlockKey was what key the partition was unlocked with, either "run",
	// "fallback" or "recovery".
	UnlockKey string `json:"unlock-key,omitempty"`
}

type recoverDegradedState struct {
	// UbuntuData is the state of the ubuntu-data (or ubuntu-data-enc)
	// partition.
	UbuntuData partitionState `json:"ubuntu-data,omitempty"`
	// UbuntuBoot is the state of the ubuntu-boot partition.
	UbuntuBoot partitionState `json:"ubuntu-boot,omitempty"`
	// UbuntuSave is the state of the ubuntu-save (or ubuntu-save-enc)
	// partition.
	UbuntuSave partitionState `json:"ubuntu-save,omitempty"`
	// ErrorLog is the log of error messages encountered during recover mode
	// setting up degraded mode.
	ErrorLog []string `json:"error-log"`
}

func (r *recoverDegradedState) partition(part string) *partitionState {
	switch part {
	case "ubuntu-data":
		return &r.UbuntuData
	case "ubuntu-boot":
		return &r.UbuntuBoot
	case "ubuntu-save":
		return &r.UbuntuSave
	}
	panic(fmt.Sprintf("unknown partition %s", part))
}

func (r *recoverDegradedState) LogErrorf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	r.ErrorLog = append(r.ErrorLog, msg)
	logger.Noticef(msg)
}

// stateFunc is a function which executes a state action, returns the next
// function (for the next) state or nil if it is the final state.
type stateFunc func() (stateFunc, error)

// stateMachine is a state machine implementing the logic for degraded recover
// mode. the following state diagram shows the logic for the various states and
// transitions:
/**


TODO: this state diagram actually is missing a state transition from
"unlock save w/ run key" to "locate unencrypted save" (which is a state that is
missing from this diagram), and then from "locate unencrypted save" to either
"done" or "mount save" states


                         +---------+                    +----------+
                         | start   |                    | mount    |       fail
                         |         +------------------->+ boot     +------------------------+
                         |         |                    |          |                        |
                         +---------+                    +----+-----+                        |
                                                             |                              |
                                                     success |                              |
                                                             |                              |
                                                             v                              v
        fail or        +-------------------+  fail,     +----+------+  fail,       +--------+-------+
        not needed     |    locate save    |  unencrypt |unlock data|  encrypted   | unlock data w/ |
        +--------------+    unencrypted    +<-----------+w/ run key +--------------+ fallback key   +-------+
        |              |                   |            |           |              |                |       |
        |              +--------+----------+            +-----+-----+              +--------+-------+       |
        |                       |                             |                             |               |
        |                       |success                      |success                      |               |
        |                       |                             |                    success  |        fail   |
        v                       v                             v                             |               |
+---+---+           +-------+----+                +-------+----+                            |               |
|       |           | mount      |       success  | mount data |                            |               |
| done  +<----------+ save       |      +---------+            +<---------------------------+               |
|       |           |            |      |         |            |                                            |
+--+----+           +----+-------+      |         +----------+-+                                            |
   ^                     ^              |                    |                                              |
   |                     | success      v                    |                                              |
   |                     |     +--------+----+   fail        |fail                                          |
   |                     |     | unlock save +--------+      |                                              |
   |                     +-----+ w/ run key  |        v      v                                              |
   |                     ^     +-------------+   +----+------+-----+                                        |
   |                     |                       | unlock save     |                                        |
   |                     |                       | w/ fallback key +----------------------------------------+
   |                     +-----------------------+                 |
   |                             success         +-------+---------+
   |                                                     |
   |                                                     |
   |                                                     |
   +-----------------------------------------------------+
                                                fail

*/

type stateMachine struct {
	// the current state is the one that is about to be executed
	current stateFunc

	// device model
	model *asserts.Model

	// the disk we have all our partitions on
	disk disks.Disk

	isEncryptedDev bool

	// state for tracking what happens as we progress through degraded mode of
	// recovery
	degradedState *recoverDegradedState
}

// degraded returns whether a degraded recover mode state has fallen back from
// the typical operation to some sort of degraded mode.
func (m *stateMachine) degraded() bool {
	r := m.degradedState

	if m.isEncryptedDev {
		// for encrypted devices, we need to have ubuntu-save mounted
		if r.UbuntuSave.MountState != partitionMounted {
			return true
		}

		// we also should have all the unlock keys as run keys
		if r.UbuntuData.UnlockKey != keyRun {
			return true
		}

		if r.UbuntuSave.UnlockKey != keyRun {
			return true
		}
	} else {
		// for unencrypted devices, ubuntu-save must either be mounted or
		// absent-but-optional
		if r.UbuntuSave.MountState != partitionMounted {
			if r.UbuntuSave.MountState != partitionAbsentOptional {
				return true
			}
		}
	}

	// ubuntu-boot and ubuntu-data should both be mounted
	if r.UbuntuBoot.MountState != partitionMounted {
		return true
	}
	if r.UbuntuData.MountState != partitionMounted {
		return true
	}

	// TODO: should we also check MountLocation too?

	// we should have nothing in the error log
	if len(r.ErrorLog) != 0 {
		return true
	}

	return false
}

func (m *stateMachine) diskOpts() *disks.Options {
	if m.isEncryptedDev {
		return &disks.Options{
			IsDecryptedDevice: true,
		}
	}
	return nil
}

func (m *stateMachine) verifyMountPoint(dir, name string) error {
	matches, err := m.disk.MountPointIsFromDisk(dir, m.diskOpts())
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("cannot validate mount: %s mountpoint target %s is expected to be from disk %s but is not", name, dir, m.disk.Dev())
	}
	return nil
}

func (m *stateMachine) setFindState(part, partUUID string, err error, logNotFoundErr bool) error {
	if err == nil {
		// device was found
		part := m.degradedState.partition(part)
		part.FindState = partitionFound
		part.Device = fmt.Sprintf("/dev/disk/by-partuuid/%s", partUUID)
		return nil
	}
	if _, ok := err.(disks.FilesystemLabelNotFoundError); ok {
		// explicit error that the device was not found
		m.degradedState.partition(part).FindState = partitionNotFound
		if logNotFoundErr {
			m.degradedState.LogErrorf("cannot find %v partition on disk %s", part, m.disk.Dev())
		}
		return nil
	}
	// the error is not "not-found", so we have a real error
	m.degradedState.partition(part).FindState = partitionErrFinding
	m.degradedState.LogErrorf("error finding %v partition on disk %s: %v", part, m.disk.Dev(), err)
	return nil
}

func (m *stateMachine) setMountState(part, where string, err error) error {
	if err != nil {
		m.degradedState.LogErrorf("cannot mount %v: %v", part, err)
		m.degradedState.partition(part).MountState = partitionErrMounting
		return nil
	}

	m.degradedState.partition(part).MountState = partitionMounted
	m.degradedState.partition(part).MountLocation = where

	if err := m.verifyMountPoint(where, part); err != nil {
		m.degradedState.LogErrorf("cannot verify %s mount point at %v: %v",
			part, where, err)
		return err
	}
	return nil
}

func (m *stateMachine) setUnlockStateWithRunKey(partName string, unlockRes secboot.UnlockResult, err error) error {
	part := m.degradedState.partition(partName)
	// save the device if we found it from secboot
	if unlockRes.Device != "" {
		part.FindState = partitionFound
		part.Device = unlockRes.Device
	} else {
		part.FindState = partitionNotFound
	}
	if unlockRes.IsDecryptedDevice {
		// if the unlock result deduced we have a decrypted device, save that
		m.isEncryptedDev = true
	}

	if err != nil {
		// create different error message for encrypted vs unencrypted
		if unlockRes.IsDecryptedDevice {
			devStr := partName
			if unlockRes.Device != "" {
				devStr += fmt.Sprintf(" (device %s)", unlockRes.Device)
			}
			m.degradedState.LogErrorf("cannot unlock encrypted %s with sealed run key: %v", devStr, err)
			part.UnlockState = partitionErrUnlocking

		} else {
			// TODO: we don't know if this is a plain not found or  a different error
			m.degradedState.LogErrorf("cannot locate %s partition for mounting host data: %v", part, err)
		}

		return nil
	}

	if unlockRes.IsDecryptedDevice {
		part.UnlockState = partitionUnlocked
		part.UnlockKey = keyRun
	}

	return nil
}

func (m *stateMachine) setUnlockStateWithFallbackKey(partName string, unlockRes secboot.UnlockResult, err error) error {
	part := m.degradedState.partition(partName)

	// first check the result and error for consistency; since we are using udev
	// there could be inconsistent results at different points in time
	// TODO: when we refactor UnlockVolumeUsingSealedKeyIfEncrypted to not also
	//       find the partition on the disk, we should eliminate this
	//       consistency checking as we can code it such that we don't get these
	//       possible inconsistencies
	// ensure consistency between encrypted state of the device/disk and what we
	// may have seen previously
	if m.isEncryptedDev && !unlockRes.IsDecryptedDevice {
		// then we previously were able to positively identify an
		// ubuntu-data-enc but can't anymore, so we have inconsistent results
		// from inspecting the disk which is suspicious and we should fail
		return fmt.Errorf("inconsistent disk encryption status: previous access resulted in encrypted, but now is unencrypted from partition %s", partName)
	}

	// if isEncryptedDev hasn't been set on the state machine yet, then set that
	// on the state machine before continuing - this is okay because we might
	// not have been able to do anything with ubuntu-data if we couldn't mount
	// ubuntu-boot, so this might be the first time we tried to unlock
	// ubuntu-data and m.isEncryptedDev may have the default value of false
	if !m.isEncryptedDev && unlockRes.IsDecryptedDevice {
		m.isEncryptedDev = unlockRes.IsDecryptedDevice
	}

	// also make sure that if we previously saw a device that we see the same
	// device again
	if unlockRes.Device != "" && part.Device != "" && unlockRes.Device != part.Device {
		return fmt.Errorf("inconsistent partitions found for %s: previously found %s but now found %s", partName, part.Device, unlockRes.Device)
	}

	if unlockRes.Device != "" {
		part.FindState = partitionFound
		part.Device = unlockRes.Device
	}

	if !unlockRes.IsDecryptedDevice && unlockRes.Device != "" && err != nil {
		// this case should be impossible to enter, if we have an unencrypted
		// device and we know what the device is then what is the error?
		return fmt.Errorf("internal error: inconsistent return values from UnlockVolumeUsingSealedKeyIfEncrypted for partition %s", partName)
	}

	if err != nil {
		// create different error message for encrypted vs unencrypted
		if m.isEncryptedDev {
			m.degradedState.LogErrorf("cannot unlock encrypted %s partition with sealed fallback key: %v", partName, err)
			part.UnlockState = partitionErrUnlocking
		} else {
			// if we don't have an encrypted device and err != nil, then the
			// device must be not-found, see above checks
			m.degradedState.LogErrorf("cannot locate %s partition: %v", partName, err)
		}

		return nil
	}

	if m.isEncryptedDev {
		part.UnlockState = partitionUnlocked

		// figure out which key/method we used to unlock the partition
		switch unlockRes.UnlockMethod {
		case secboot.UnlockedWithSealedKey:
			part.UnlockKey = keyFallback
		case secboot.UnlockedWithRecoveryKey:
			part.UnlockKey = keyRecovery

			// TODO: should we fail with internal error for default case here?
		}
	}

	return nil
}

func newStateMachine(model *asserts.Model, disk disks.Disk) *stateMachine {
	m := &stateMachine{
		model: model,
		disk:  disk,
		degradedState: &recoverDegradedState{
			ErrorLog: []string{},
		},
	}
	// first step is to mount ubuntu-boot to check for run mode keys to unlock
	// ubuntu-data
	m.current = m.mountBoot
	return m
}

func (m *stateMachine) execute() (finished bool, err error) {
	next, err := m.current()
	m.current = next
	finished = next == nil
	if finished && err == nil {
		if err := m.finalize(); err != nil {
			return true, err
		}
	}
	return finished, err
}

func (m *stateMachine) finalize() error {
	// check soundness
	// the grade check makes sure that if data was mounted unencrypted
	// but the model is secured it will end up marked as untrusted
	isEncrypted := m.isEncryptedDev || m.model.Grade() == asserts.ModelSecured
	part := m.degradedState.partition("ubuntu-data")
	if part.MountState == partitionMounted && isEncrypted {
		// check that save and data match
		// We want to avoid a chosen ubuntu-data
		// (e.g. activated with a recovery key) to get access
		// via its logins to the secrets in ubuntu-save (in
		// particular the policy update auth key)
		trustData, _ := checkDataAndSavaPairing(boot.InitramfsHostWritableDir)
		if !trustData {
			part.MountState = partitionMountedUntrusted
			m.degradedState.LogErrorf("cannot trust ubuntu-data, ubuntu-save and ubuntu-data are not marked as from the same install")
		}
	}
	return nil
}

func (m *stateMachine) trustData() bool {
	return m.degradedState.partition("ubuntu-data").MountState == partitionMounted
}

// mountBoot is the first state to execute in the state machine, it can
// transition to the following states:
// - if ubuntu-boot is mounted successfully, execute unlockDataRunKey
// - if ubuntu-boot can't be mounted, execute unlockDataFallbackKey
// - if we mounted the wrong ubuntu-boot (or otherwise can't verify which one we
//   mounted), return fatal error
func (m *stateMachine) mountBoot() (stateFunc, error) {
	part := m.degradedState.partition("ubuntu-boot")
	// use the disk we mounted ubuntu-seed from as a reference to find
	// ubuntu-seed and mount it
	partUUID, findErr := m.disk.FindMatchingPartitionUUID("ubuntu-boot")
	if err := m.setFindState("ubuntu-boot", partUUID, findErr, true); err != nil {
		return nil, err
	}
	if part.FindState != partitionFound {
		// if we didn't find ubuntu-boot, we can't try to unlock data with the
		// run key, and should instead just jump straight to attempting to
		// unlock with the fallback key
		return m.unlockDataFallbackKey, nil
	}

	// should we fsck ubuntu-boot? probably yes because on some platforms
	// (u-boot for example) ubuntu-boot is vfat and it could have been unmounted
	// dirtily, and we need to fsck it to ensure it is mounted safely before
	// reading keys from it
	fsckSystemdOpts := &systemdMountOptions{
		NeedsFsck: true,
	}
	mountErr := doSystemdMount(part.Device, boot.InitramfsUbuntuBootDir, fsckSystemdOpts)
	if err := m.setMountState("ubuntu-boot", boot.InitramfsUbuntuBootDir, mountErr); err != nil {
		return nil, err
	}
	if part.MountState == partitionErrMounting {
		// if we didn't mount data, then try to unlock data with the
		// fallback key
		return m.unlockDataFallbackKey, nil
	}

	// next step try to unlock data with run object
	return m.unlockDataRunKey, nil
}

// stateUnlockDataRunKey will try to unlock ubuntu-data with the normal run-mode
// key, and if it fails, progresses to the next state, which is either:
// - failed to unlock data, but we know it's an encrypted device -> try to unlock with fallback key
// - failed to find data at all -> try to unlock save
// - unlocked data with run key -> mount data
func (m *stateMachine) unlockDataRunKey() (stateFunc, error) {
	// TODO: don't allow recovery key at all for this invocation, we only allow
	// recovery key to be used after we try the fallback key
	runModeKey := filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key")
	unlockOpts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		// don't allow using the recovery key to unlock, we only try using the
		// recovery key after we first try the fallback object
		AllowRecoveryKey: false,
		// don't lock keys, we manually do that at the end always, we don't know
		// if this call to unlock a volume will be the last one or not
		LockKeysOnFinish: false,
	}
	unlockRes, unlockErr := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-data", runModeKey, unlockOpts)
	if err := m.setUnlockStateWithRunKey("ubuntu-data", unlockRes, unlockErr); err != nil {
		return nil, err
	}
	if unlockErr != nil {
		// we couldn't unlock ubuntu-data with the primary key, or we didn't
		// find it in the unencrypted case
		if unlockRes.IsDecryptedDevice {
			// we know the device is encrypted, so the next state is to try
			// unlocking with the fallback key
			return m.unlockDataFallbackKey, nil
		}

		// not an encrypted device, so nothing to fall back to try and unlock
		// data, so just mark it as not found and continue on to try and mount
		// an unencrypted ubuntu-save directly
		return m.locateUnencryptedSave, nil
	}

	// otherwise successfully unlocked it (or just found it if it was unencrypted)
	// so just mount it
	return m.mountData, nil
}

func (m *stateMachine) unlockDataFallbackKey() (stateFunc, error) {
	// try to unlock data with the fallback key on ubuntu-seed, which must have
	// been mounted at this point
	unlockOpts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		// we want to allow using the recovery key if the fallback key fails as
		// using the fallback object is the last chance before we give up trying
		// to unlock data
		AllowRecoveryKey: true,
		// don't lock keys, we manually do that at the end always, we don't know
		// if this call to unlock a volume will be the last one or not
		LockKeysOnFinish: false,
	}
	// TODO: this prompts for a recovery key
	// TODO: we should somehow customize the prompt to mention what key we need
	// the user to enter, and what we are unlocking (as currently the prompt
	// says "recovery key" and the partition UUID for what is being unlocked)
	dataFallbackKey := filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key")
	unlockRes, unlockErr := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-data", dataFallbackKey, unlockOpts)
	if err := m.setUnlockStateWithFallbackKey("ubuntu-data", unlockRes, unlockErr); err != nil {
		return nil, err
	}
	if unlockErr != nil {
		// skip trying to mount data, since we did not unlock data we cannot
		// open save with with the run key, so try the fallback one
		return m.unlockSaveFallbackKey, nil
	}

	// unlocked it, now go mount it
	return m.mountData, nil
}

func (m *stateMachine) mountData() (stateFunc, error) {
	data := m.degradedState.partition("ubuntu-data")
	// don't do fsck on the data partition, it could be corrupted
	mountErr := doSystemdMount(data.Device, boot.InitramfsHostUbuntuDataDir, nil)
	if err := m.setMountState("ubuntu-data", boot.InitramfsHostUbuntuDataDir, mountErr); err != nil {
		return nil, err
	}
	if data.MountState == partitionErrMounting {
		// no point trying to unlock save with the run key, we need data to be
		// mounted for that and we failed to mount it
		return m.unlockSaveFallbackKey, nil
	}

	// next step: try to unlock with run save key if we are encrypted
	if m.isEncryptedDev {
		return m.unlockSaveRunKey, nil
	}

	// if we are unencrypted just try to find unencrypted ubuntu-save and then
	// maybe mount it
	return m.locateUnencryptedSave, nil
}

func (m *stateMachine) locateUnencryptedSave() (stateFunc, error) {
	part := m.degradedState.partition("ubuntu-save")
	partUUID, findErr := m.disk.FindMatchingPartitionUUID("ubuntu-save")
	if err := m.setFindState("ubuntu-save", partUUID, findErr, false); err != nil {
		return nil, nil
	}
	if part.FindState != partitionFound {
		if part.FindState == partitionNotFound {
			// this is ok, ubuntu-save may not exist for
			// non-encrypted device
			part.MountState = partitionAbsentOptional
		}
		// all done, nothing left to try and mount, even if errors
		// occurred
		return nil, nil
	}

	// we found the unencrypted device, now mount it
	return m.mountSave, nil
}

func (m *stateMachine) unlockSaveRunKey() (stateFunc, error) {
	// to get to this state, we needed to have mounted ubuntu-data on host, so
	// if encrypted, we can try to read the run key from host ubuntu-data
	saveKey := filepath.Join(dirs.SnapFDEDirUnder(boot.InitramfsHostWritableDir), "ubuntu-save.key")
	key, err := ioutil.ReadFile(saveKey)
	if err != nil {
		// log the error and skip to trying the fallback key
		m.degradedState.LogErrorf("cannot access run ubuntu-save key: %v", err)
		return m.unlockSaveFallbackKey, nil
	}

	saveDevice, unlockErr := secbootUnlockEncryptedVolumeUsingKey(m.disk, "ubuntu-save", key)
	// TODO:UC20: UnlockEncryptedVolumeUsingKey should return an UnlockResult,
	//            but until then we create our own and pass it along
	unlockRes := secboot.UnlockResult{
		Device:            saveDevice,
		IsDecryptedDevice: true,
	}
	if err := m.setUnlockStateWithRunKey("ubuntu-save", unlockRes, unlockErr); err != nil {
		return nil, err
	}
	if unlockErr != nil {
		// failed to unlock with run key, try fallback key
		return m.unlockSaveFallbackKey, nil
	}

	// unlocked it properly, go mount it
	return m.mountSave, nil
}

func (m *stateMachine) unlockSaveFallbackKey() (stateFunc, error) {
	// try to unlock save with the fallback key on ubuntu-seed, which must have
	// been mounted at this point
	unlockOpts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		// we want to allow using the recovery key if the fallback key fails as
		// using the fallback object is the last chance before we give up trying
		// to unlock save
		AllowRecoveryKey: true,
		// while this is technically always the last call to unlock the volume
		// if we get here, to keep things simple we just always lock after
		// running the state machine so don't lock keys here
		LockKeysOnFinish: false,
	}
	saveFallbackKey := filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key")
	// TODO: this prompts again for a recover key, but really this is the
	// reinstall key we will prompt for
	// TODO: we should somehow customize the prompt to mention what key we need
	// the user to enter, and what we are unlocking (as currently the prompt
	// says "recovery key" and the partition UUID for what is being unlocked)
	unlockRes, unlockErr := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-save", saveFallbackKey, unlockOpts)
	if err := m.setUnlockStateWithFallbackKey("ubuntu-save", unlockRes, unlockErr); err != nil {
		return nil, err
	}
	if unlockErr != nil {
		// all done, nothing left to try and mount, everything failed
		return nil, nil
	}

	// otherwise we unlocked it, so go mount it
	return m.mountSave, nil
}

func (m *stateMachine) mountSave() (stateFunc, error) {
	saveDev := m.degradedState.partition("ubuntu-save").Device
	// TODO: should we fsck ubuntu-save ?
	mountErr := doSystemdMount(saveDev, boot.InitramfsUbuntuSaveDir, nil)
	if err := m.setMountState("ubuntu-save", boot.InitramfsUbuntuSaveDir, mountErr); err != nil {
		return nil, err
	}
	// all done, nothing left to try and mount
	return nil, nil
}

func generateMountsModeRecover(mst *initramfsMountsState) error {
	// steps 1 and 2 are shared with install mode
	model, err := generateMountsCommonInstallRecover(mst)
	if err != nil {
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

	machine, err := func() (machine *stateMachine, err error) {
		// ensure that the last thing we do after mounting everything is to lock
		// access to sealed keys
		defer func() {
			if err := secbootLockTPMSealedKeys(); err != nil {
				logger.Noticef("error locking access to sealed keys: %v", err)
			}
		}()

		// first state to execute is to unlock ubuntu-data with the run key
		machine = newStateMachine(model, disk)
		for {
			finished, err := machine.execute()
			// TODO: consider whether certain errors are fatal or not
			if err != nil {
				return nil, err
			}
			if finished {
				break
			}
		}

		return machine, nil
	}()
	if err != nil {
		return err
	}

	// 3.1 write out degraded.json if we ended up falling back somewhere
	if machine.degraded() {
		b, err := json.Marshal(machine.degradedState)
		if err != nil {
			return err
		}

		err = os.MkdirAll(dirs.SnapBootstrapRunDir, 0755)
		if err != nil {
			return err
		}

		// leave the information about degraded state at an ephemeral location
		err = ioutil.WriteFile(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), b, 0644)
		if err != nil {
			return err
		}
	}

	// 4. final step: copy the auth data and network config from
	//    the real ubuntu-data dir to the ephemeral ubuntu-data
	//    dir, write the modeenv to the tmpfs data, and disable
	//    cloud-init in recover mode

	// if we have the host location, then we were able to successfully mount
	// ubuntu-data, and as such we can proceed with copying files from there
	// onto the tmpfs
	// Proceed only if we trust ubuntu-data to be paired with ubuntu-save
	if machine.trustData() {
		// TODO: erroring here should fallback to copySafeDefaultData and
		// proceed on with degraded mode anyways
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

// checkDataAndSavaPairing make sure that ubuntu-data and ubuntu-save
// come from the same install by comparing secret markers in them
func checkDataAndSavaPairing(rootdir string) (bool, error) {
	// read the secret marker file from ubuntu-data
	markerFile1 := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "marker")
	marker1, err := ioutil.ReadFile(markerFile1)
	if err != nil {
		return false, err
	}
	// read the secret marker file from ubuntu-save
	// TODO:UC20: this is a bit of an abuse of the Install*Dir variable, we
	// should really only be using Initramfs*Dir variables since we are in the
	// initramfs and not in install mode, no?
	markerFile2 := filepath.Join(boot.InstallHostFDESaveDir, "marker")
	marker2, err := ioutil.ReadFile(markerFile2)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(marker1, marker2) == 1, nil
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

func generateMountsCommonInstallRecover(mst *initramfsMountsState) (*asserts.Model, error) {
	// 1. always ensure seed partition is mounted first before the others,
	//      since the seed partition is needed to mount the snap files there
	if err := mountPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "ubuntu-seed"); err != nil {
		return nil, err
	}

	// load model and verified essential snaps metadata
	typs := []snap.Type{snap.TypeBase, snap.TypeKernel, snap.TypeSnapd, snap.TypeGadget}
	model, essSnaps, err := mst.ReadEssential("", typs)
	if err != nil {
		return nil, fmt.Errorf("cannot load metadata and verify essential bootstrap snaps %v: %v", typs, err)
	}

	// 2.1. measure model
	err = stampedAction(fmt.Sprintf("%s-model-measured", mst.recoverySystem), func() error {
		return secbootMeasureSnapModelWhenPossible(func() (*asserts.Model, error) {
			return model, nil
		})
	})
	if err != nil {
		return nil, err
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
			return nil, err
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
		return nil, err
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
	if err := sysconfig.ConfigureTargetSystem(configOpts); err != nil {
		return nil, err
	}

	return model, err
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
	runModeKey := filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key")
	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		LockKeysOnFinish: true,
		AllowRecoveryKey: true,
	}
	unlockRes, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-data", runModeKey, opts)
	if err != nil {
		return err
	}

	// TODO: do we actually need fsck if we are mounting a mapper device?
	// probably not?
	if err := doSystemdMount(unlockRes.Device, boot.InitramfsDataDir, fsckSystemdOpts); err != nil {
		return err
	}
	isEncryptedDev := unlockRes.IsDecryptedDevice

	// 3.3. mount ubuntu-save (if present)
	haveSave, err := maybeMountSave(disk, boot.InitramfsWritableDir, isEncryptedDev, fsckSystemdOpts)
	if err != nil {
		return err
	}

	// 4.1 verify that ubuntu-data comes from where we expect it to
	diskOpts := &disks.Options{}
	if unlockRes.IsDecryptedDevice {
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

		if isEncryptedDev {
			// in run mode the path to open an encrypted save is for
			// data to be encrypted and the save key in it
			// to be successfully used. This already should stop
			// allowing to chose ubuntu-data to try to access
			// save. as safety boot also stops if the keys cannot
			// be locked.
			// for symmetry with recover code and extra paranoia
			// though also check that the markers match.
			paired, err := checkDataAndSavaPairing(boot.InitramfsWritableDir)
			if err != nil {
				return err
			}
			if !paired {
				return fmt.Errorf("cannot validate boot: ubuntu-save and ubuntu-data are not marked as from the same install")
			}
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
