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

// recoverDegradedState is a map which usually contains the following keys:
// - "ubuntu-data" - a map of states for the partition including the keys below
// - "ubuntu-boot" - same as ubuntu-data
// - "ubuntu-save" - same as ubuntu-data
// - "error-log" - a list of errors encountered while setting up recover mode
//
// partitions can have the following keys in their maps:
// - "mount-state" - whether it was mounted successfully or not
// - "mount-location" - where this partition was mounted
// - "device" - what device the partition corresponds to
// - "locate-state" - whether the partition on the disk was located or not
// - "unlock-state" - whether it was unlocked successfully or not
// - "unlock-key" - what key it was unlocked with (TODO: should this be rolled into "unlock-state" ?)
type recoverDegradedState map[string]interface{}

func (r recoverDegradedState) setPartitionKeyValue(diskName, k, v string) {
	if obj, ok := r[diskName]; ok {
		m, ok := obj.(map[string]string)
		if !ok {
			panic("expected disk object to be a map[string]string")
		}
		m[k] = v
		r[diskName] = m
	} else {
		r[diskName] = map[string]string{
			k: v,
		}
	}
}

func (r recoverDegradedState) getPartitionKey(diskName, k string) string {
	if obj, ok := r[diskName]; ok {
		m, ok := obj.(map[string]string)
		if !ok {
			panic("expected disk object to be a map[string]string")
		}
		return m[k]
	}
	return ""
}

func (r recoverDegradedState) LogErrorf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if _, ok := r["error-log"]; ok {
		errLog, ok := r["error-log"].([]string)
		if !ok {
			panic("internal error: error-log is not a slice")
		}
		errLog = append(errLog, msg)
		r["error-log"] = errLog
	} else {
		r["error-log"] = []string{msg}
	}
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
+---+---+           +-------+----+                +-------+----+                        |               |
|       |           | mount      |       success  | mount data |                        |               |
| done  +<----------+ save       |      +---------+            +<-----------------------+               |
|       |           |            |      |         |            |                                        |
+--+----+           +----+-------+      |         +----------+-+                                        |
   ^                     ^              |                    |                                          |
   |                     | success      v                    |                                          |
   |                     |     +--------+----+   fail        |fail                                      |
   |                     |     | unlock save +--------+      |                                          |
   |                     +-----+ w/ run key  |        v      v                                          |
   |                     ^     +-------------+   +----+------+-----+                                    |
   |                     |                       | unlock save     |                                    |
   |                     |                       | w/ fallback key +------------------------------------+
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

	// the disk we have all our partitions on
	disk disks.Disk

	// TODO: roll these next two into degradedState instead ?

	// the device for ubuntu-data that is to be mounted - either an unencrypted
	// volume, or a decrypted mapper volume if ubuntu-data is really encrypted
	// on the physical disk
	dataDevice string
	// the device for ubuntu-save that is to be mounted
	saveDevice string

	isEncryptedDev bool

	// state for tracking what happens as we progress through degraded mode of
	// recovery
	degradedState recoverDegradedState
}

func (m *stateMachine) diskOpts() *disks.Options {
	if m.isEncryptedDev {
		return &disks.Options{
			IsDecryptedDevice: true,
		}
	}
	return nil
}

func (m *stateMachine) verifyMountPointCtx(dir, name string) error {
	matches, err := m.disk.MountPointIsFromDisk(dir, m.diskOpts())
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("cannot validate boot: %s mountpoint is expected to be from disk %s but is not", name, m.disk.Dev())
	}
	return nil
}

// ensureUnlockResConsistency does a couple of things for ubuntu-data and
// ubuntu-save respectively, it:
// - updates the state machine with new, consistent information from the unlock
//   result
// - ensures that previous state the state machine has seen is consistent with
//   the new unlock result we have
// - verifies the logical consistency of the unlock error and the unlock result
func (m *stateMachine) ensureUnlockResConsistency(part string, unlockRes secboot.UnlockResult, unlockErr error) error {
	var stateDevice *string
	switch part {
	case "ubuntu-data":
		stateDevice = &m.dataDevice
	case "ubuntu-save":
		stateDevice = &m.saveDevice
	}
	// TODO: we do a lot of checking on the result here to simplify later
	//       decisions, perhaps this is too much?

	// before checking err, make sure that the results from UnlockRes match
	// what we've seen before and put valid results inside the state machine

	// ensure consistency between encrypted state of the device/disk and what we
	// may have seen previously
	if m.isEncryptedDev && !unlockRes.IsDecryptedDevice {
		// then we previously were able to positively identify an
		// ubuntu-data-enc but can't anymore, so we have inconsistent results
		// from inspecting the disk which is suspicious and we should fail
		return fmt.Errorf("inconsistent disk encryption status: previous access resulted in encrypted, but now is unencrypted")
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
	if unlockRes.Device != "" && *stateDevice != "" && unlockRes.Device != *stateDevice {
		return fmt.Errorf("inconsistent partitions found for ubuntu-data: previously found %s but now found %s", *stateDevice, unlockRes.Device)
	}

	if unlockRes.Device != "" {
		*stateDevice = unlockRes.Device
		m.degradedState.setPartitionKeyValue(part, "locate-state", "found")
		m.degradedState.setPartitionKeyValue(part, "device", unlockRes.Device)
	}

	if !unlockRes.IsDecryptedDevice && unlockRes.Device != "" && unlockErr != nil {
		// this case should be impossible to enter, if we have an unencrypted
		// device and we know what the device is then what is the error?
		return fmt.Errorf("internal error: inconsistent return values from UnlockVolumeUsingSealedKeyIfEncrypted")
	}

	return nil
}

func newStateMachine(disk disks.Disk) *stateMachine {
	m := &stateMachine{
		disk:          disk,
		degradedState: recoverDegradedState{},
	}
	// first step is to mount ubuntu-boot to check for run mode keys to unlock
	// ubuntu-data
	m.current = m.mountBoot
	return m
}

func (m *stateMachine) execute() (finished bool, err error) {
	next, err := m.current()
	m.current = next
	return next == nil, err
}

// mountBoot is the first state to execute in the state machine, it can
// transition to the following states:
// - if ubuntu-boot is mounted successfully, execute unlockDataRunKey
// - if ubuntu-boot can't be mounted, execute unlockDataFallbackKey
// - if we mounted the wrong ubuntu-boot (or otherwise can't verify which one we
//   mounted), return fatal error
func (m *stateMachine) mountBoot() (stateFunc, error) {
	// use the disk we mounted ubuntu-seed from as a reference to find
	// ubuntu-seed and mount it
	partUUID, err := m.disk.FindMatchingPartitionUUID("ubuntu-boot")
	if err != nil {
		// if we didn't find ubuntu-boot, we can't try to unlock data with the
		// run key, and should instead just jump straight to attempting to
		// unlock with the fallback key
		if _, ok := err.(disks.FilesystemLabelNotFoundError); !ok {
			m.degradedState.setPartitionKeyValue("ubuntu-boot", "locate-state", "err-finding")
			m.degradedState.LogErrorf("cannot find ubuntu-boot partition on disk %s", m.disk.Dev())
		} else {
			m.degradedState.setPartitionKeyValue("ubuntu-boot", "locate-state", "not-found")
			m.degradedState.LogErrorf("error locating ubuntu-boot partition on disk %s: %v", m.disk.Dev(), err)
		}

		return m.unlockDataFallbackKey, nil
	}

	dev := fmt.Sprintf("/dev/disk/by-partuuid/%s", partUUID)
	m.degradedState.setPartitionKeyValue("ubuntu-boot", "device", dev)
	m.degradedState.setPartitionKeyValue("ubuntu-boot", "locate-state", "found")

	// should we fsck ubuntu-boot? probably yes because on some platforms
	// (u-boot for example) ubuntu-boot is vfat and it could have been unmounted
	// dirtily, and we need to fsck it to ensure it is mounted safely before
	// reading keys from it
	fsckSystemdOpts := &systemdMountOptions{
		NeedsFsck: true,
	}
	if err := doSystemdMount(dev, boot.InitramfsUbuntuBootDir, fsckSystemdOpts); err != nil {
		// didn't manage to mount boot, so try to use fallback key for data
		m.degradedState.LogErrorf("failed to mount ubuntu-boot: %v", err)
		m.degradedState.setPartitionKeyValue("ubuntu-boot", "mount-state", "failed-to-mount")
		return m.unlockDataFallbackKey, nil
	}

	// verify ubuntu-boot comes from same disk as ubuntu-seed
	if err := m.verifyMountPointCtx(boot.InitramfsUbuntuBootDir, "ubuntu-boot"); err != nil {
		return nil, err
	}

	m.degradedState.setPartitionKeyValue("ubuntu-boot", "mount-state", "mounted")
	m.degradedState.setPartitionKeyValue("ubuntu-boot", "mount-location", boot.InitramfsUbuntuBootDir)

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
	unlockRes, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-data", runModeKey, unlockOpts)

	// save the device if we found it
	if unlockRes.Device != "" {
		m.dataDevice = unlockRes.Device
		m.degradedState.setPartitionKeyValue("ubuntu-data", "locate-state", "found")
		m.degradedState.setPartitionKeyValue("ubuntu-data", "device", unlockRes.Device)
	} else {
		m.degradedState.setPartitionKeyValue("ubuntu-data", "locate-state", "not-found")
	}

	if unlockRes.IsDecryptedDevice {
		m.isEncryptedDev = true
	}
	if err != nil {
		// we couldn't unlock ubuntu-data with the primary key, or we didn't
		// find it in the unencrypted case
		if unlockRes.IsDecryptedDevice {
			devStr := ""
			if unlockRes.Device != "" {
				devStr = fmt.Sprintf("(device %s)", unlockRes.Device)
			}
			m.degradedState.LogErrorf("cannot unlock encrypted ubuntu-data%s with sealed run key: %v", devStr, err)
			// we know the device is encrypted, so the next state is to try
			// unlocking with the fallback key
			return m.unlockDataFallbackKey, nil
		}

		// not an encrypted device, so nothing to fall back to try and unlock
		// data, so just mark it as not found and continue on to try and mount
		// an unencrypted ubuntu-save directly
		m.degradedState.LogErrorf("failed to find ubuntu-data partition for mounting host data: %v", err)
		return m.locateUnencryptedSave, nil
	}

	// otherwise successfully unlocked it (if it was encrypted)

	// successfully unlocked it with the run key, so just mark that in the
	// state and move on to trying to mount it
	if unlockRes.IsDecryptedDevice {
		m.degradedState.setPartitionKeyValue("ubuntu-data", "unlock-state", "unlocked")
		m.degradedState.setPartitionKeyValue("ubuntu-data", "key-state", "run")
	}

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
	// TODO: this prompts again for a recover key, but really this is the
	// reinstall key we will prompt for
	// TODO: we should somehow customize the prompt to mention what key we need
	// the user to enter, and what we are unlocking (as currently the prompt
	// says "recovery key" and the partition UUID for what is being unlocked)
	dataFallbackKey := filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key")
	unlockRes, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-data", dataFallbackKey, unlockOpts)

	consistencyErr := m.ensureUnlockResConsistency("ubuntu-data", unlockRes, err)
	if consistencyErr != nil {
		return nil, consistencyErr
	}

	if err != nil {
		if m.isEncryptedDev {
			m.degradedState.LogErrorf("cannot unlock encrypted ubuntu-data partition with sealed fallback key: %v", err)
			m.degradedState.setPartitionKeyValue("ubuntu-data", "unlock-state", "failed")
		} else {
			// if we don't have an encrypted device and err != nil, then the
			// device must not be found, see the above if
			m.degradedState.LogErrorf("cannot locate ubuntu-data partition: %v", err)
		}

		// skip trying to mount data, since we did not unlock data we cannot
		// open save with with the run key, so try the fallback one
		return m.unlockSaveFallbackKey, nil
	}

	if m.isEncryptedDev {
		// we unlocked data with the fallback key, we are not in
		// "fully" degraded mode, but we do need to track that we had to
		// use the fallback key
		switch unlockRes.UnlockMethod {
		case secboot.UnlockedWithSealedKey:
			m.degradedState.setPartitionKeyValue("ubuntu-data", "key-state", "fallback")
		case secboot.UnlockedWithRecoveryKey:
			m.degradedState.setPartitionKeyValue("ubuntu-data", "key-state", "recovery")

			// TODO: should we fail with internal error for default case here?
		}
		m.degradedState.setPartitionKeyValue("ubuntu-data", "unlock-state", "unlocked")
	}

	return m.mountData, nil
}

func (m *stateMachine) mountData() (stateFunc, error) {
	// don't do fsck on the data partition, it could be corrupted
	if err := doSystemdMount(m.dataDevice, boot.InitramfsHostUbuntuDataDir, nil); err != nil {
		m.degradedState.LogErrorf("cannot mount ubuntu-data: %v", err)
		// we failed to mount it, proceed with degraded mode
		m.degradedState.setPartitionKeyValue("ubuntu-data", "mount-state", "failed-to-mount")

		// no point trying to unlock save with the run key, we need data to be
		// mounted for that and we failed to mount it
		return m.unlockSaveFallbackKey, nil
	}
	// we mounted it successfully, verify it comes from the right disk
	if err := m.verifyMountPointCtx(boot.InitramfsHostUbuntuDataDir, "ubuntu-data"); err != nil {
		m.degradedState.LogErrorf("cannot verify ubuntu-data mount point at %v: %v",
			boot.InitramfsHostUbuntuDataDir, err)
		return nil, err
	}

	m.degradedState.setPartitionKeyValue("ubuntu-data", "mount-state", "mounted")
	m.degradedState.setPartitionKeyValue("ubuntu-data", "mount-location", boot.InitramfsHostUbuntuDataDir)

	// next step: try to unlock with run save key if we are encrypted
	if m.isEncryptedDev {
		return m.unlockSaveRunKey, nil
	}

	// if we are unencrypted just try to find unencrypted ubuntu-save and then
	// maybe mount it
	return m.locateUnencryptedSave, nil
}

func (m *stateMachine) locateUnencryptedSave() (stateFunc, error) {
	partUUID, err := m.disk.FindMatchingPartitionUUID("ubuntu-save")
	if err != nil {
		// error locating ubuntu-save
		if _, ok := err.(disks.FilesystemLabelNotFoundError); !ok {
			// the error is not "not-found", so we have a real error
			// identifying whether save exists or not
			m.degradedState.LogErrorf("error identifying ubuntu-save partition: %v", err)
			m.degradedState.setPartitionKeyValue("ubuntu-save", "locate-state", "err-finding")
		} else {
			// this is ok, ubuntu-save may not exist for
			// non-encrypted device
			// TODO: should this be a locate-state or mount-state setting?
			m.degradedState.setPartitionKeyValue("ubuntu-save", "locate-state", "not-needed")
		}

		// all done, nothing left to try and mount, even if errors
		// occurred
		return nil, nil
	}

	// we found the unencrypted device, now mount it
	m.saveDevice = filepath.Join("/dev/disk/by-partuuid", partUUID)
	m.degradedState.setPartitionKeyValue("ubuntu-save", "device", m.saveDevice)
	m.degradedState.setPartitionKeyValue("ubuntu-save", "locate-state", "found")
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

	saveDevice, err := secbootUnlockEncryptedVolumeUsingKey(m.disk, "ubuntu-save", key)
	if err != nil {
		m.degradedState.LogErrorf("cannot unlock encrypted ubuntu-save with run key: %v", err)
		// failed to unlock with run key, try fallback key
		return m.unlockSaveFallbackKey, nil
	}

	m.degradedState.setPartitionKeyValue("ubuntu-save", "unlock-state", "unlocked")
	m.degradedState.setPartitionKeyValue("ubuntu-save", "key-state", "run")

	// unlocked it properly, go mount it
	m.saveDevice = saveDevice
	m.degradedState.setPartitionKeyValue("ubuntu-save", "device", saveDevice)
	m.degradedState.setPartitionKeyValue("ubuntu-save", "locate-state", "found")
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
	unlockRes, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(m.disk, "ubuntu-save", saveFallbackKey, unlockOpts)

	consistencyErr := m.ensureUnlockResConsistency("ubuntu-save", unlockRes, err)
	if consistencyErr != nil {
		return nil, consistencyErr
	}

	if err != nil {
		if m.isEncryptedDev {
			m.degradedState.LogErrorf("cannot unlock encrypted ubuntu-save partition with fallback key: %v", err)
			m.degradedState.setPartitionKeyValue("ubuntu-save", "unlock-state", "failed")
		} else {
			// if we don't have an encrypted device and err != nil, then the
			// device must not be found, see the above if
			m.degradedState.LogErrorf("cannot locate ubuntu-save partition: %v", err)
		}

		// all done, nothing left to try and mount, everything failed
		return nil, nil
	}

	switch unlockRes.UnlockMethod {
	case secboot.UnlockedWithSealedKey:
		m.degradedState.setPartitionKeyValue("ubuntu-save", "key-state", "fallback")
	case secboot.UnlockedWithRecoveryKey:
		m.degradedState.setPartitionKeyValue("ubuntu-save", "key-state", "recovery")

		// TODO: should we fail with internal error for default case here?
	}

	m.saveDevice = unlockRes.Device
	m.degradedState.setPartitionKeyValue("ubuntu-save", "unlock-state", "unlocked")
	m.degradedState.setPartitionKeyValue("ubuntu-save", "device", m.saveDevice)
	m.degradedState.setPartitionKeyValue("ubuntu-save", "locate-state", "found")

	return m.mountSave, nil
}

func (m *stateMachine) mountSave() (stateFunc, error) {
	// TODO: should we fsck ubuntu-save ?
	if err := doSystemdMount(m.saveDevice, boot.InitramfsUbuntuSaveDir, nil); err != nil {
		m.degradedState.LogErrorf("error mounting ubuntu-save from partition %s: %v", m.saveDevice, err)
		m.degradedState.setPartitionKeyValue("ubuntu-save", "mount-state", "failed-to-mount")
		return nil, nil
	}
	// if we couldn't verify whether the mounted save is valid, bail out of
	// the state machine and exit snap-bootstrap
	if err := m.verifyMountPointCtx(boot.InitramfsUbuntuSaveDir, "ubuntu-save"); err != nil {
		m.degradedState.LogErrorf("cannot verify ubuntu-save mount at %v: %v",
			boot.InitramfsUbuntuSaveDir, err)

		// we are done, even if errors occurred
		return nil, err
	}

	m.degradedState.setPartitionKeyValue("ubuntu-save", "mount-state", "mounted")
	m.degradedState.setPartitionKeyValue("ubuntu-save", "mount-location", boot.InitramfsUbuntuSaveDir)

	// all done, nothing left to try and mount
	return nil, nil
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

	// ensure that the last thing we do after mounting everything is to lock
	// access to sealed keys
	defer func() {
		if err := secbootLockTPMSealedKeys(); err != nil {
			logger.Noticef("error locking access to sealed keys: %v", err)
		}
	}()

	// first state to execute is to unlock ubuntu-data with the run key
	machine := newStateMachine(disk)
	for {
		final, err := machine.execute()
		// TODO: consider whether certain errors are fatal or not
		if err != nil {
			return err
		}
		if final {
			break
		}
	}

	// 3.1 write out degraded.json
	b, err := json.Marshal(machine.degradedState)
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
	if machine.degradedState.getPartitionKey("ubuntu-data", "mount-location") != "" {
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

	// 3.3. mount ubuntu-save (if present)
	haveSave, err := maybeMountSave(disk, boot.InitramfsWritableDir, unlockRes.IsDecryptedDevice, fsckSystemdOpts)
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
