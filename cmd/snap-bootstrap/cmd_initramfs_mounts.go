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
	//   (in the case we failed to unlock it, but we know it is there, then
	//   )
	DataKey string
	// TODO: make these values enums?
	// DataState is the state of the ubuntu-data mountpoint, it can be:
	// - "mounted" for mounted and available
	// - "enc-not-found" for state where we found an ubuntu-data-enc
	//   partition and tried to unlock it, but failed _before mounting it_.
	//   Errors mounting are identified differently.
	// TODO:UC20: "other" is a really bad term here replace with something better
	// - "other" for some other error where we couldn't identify if there's an
	//    ubuntu-data (or ubuntu-data-enc) partition at all
	DataState string
	// SaveKey is which key was used to unlock ubuntu-save (if any). Same values
	// as DataKey.
	SaveKey string
	// SaveState is the state of the ubuntu-save mountpoint, it can be in any of
	// the states documented for DataState, with the additional state of:
	// - "not-needed" for state when we don't have ubuntu-save, but we don't need it (unencrypted only)
	SaveState string
	// HostLocation is the location where the host's ubuntu-data is mounted
	// and available. If this is the empty string, then the host's
	// ubuntu-data is not available anywhere.
	HostLocation string
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

	degradedState := recoverDegradedState{}

	// TODO: what follows is an awful lot of if statements, smells very iffy to
	//       me

	// 3. try to unlock or locate ubuntu-data for the recovery host mountpoint
	//    we have a few states for ubuntu-data after this block, enumerated in
	//    the order they may appear
	//    1) unlocked via run mode key (or unencrypted) + mounted successfully
	//    2) unlocked via run mode key + failed to mount
	//    3) unlocked via fallback object + mounted successfully
	//    4) unlocked via fallback object + failed to mount
	//    5) failed to find/unlock at all + mount not attempted
	const lockKeysOnFinish = true
	runModeKey := filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-data.sealed-key")
	dataDevice, isDecryptDev, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-data", runModeKey, false)
	if err == nil {
		// successfully unlocked it with the run key, so just mark that in the
		// state and move on
		degradedState.DataKey = "run"
	} else {
		// we couldn't unlock ubuntu-data with the primary key, we need to check
		// a few things
		if isDecryptDev {
			// we at least made it to the point where we know we have an
			// encrypted device we need to unlock, but we failed to unlock it
			// with the run key, so instead try to use the fallback key object
			dataFallbackKey := filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-data.recovery.sealed-key")
			dataDevice, isDecryptDev, err = secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-data", dataFallbackKey, false)
			if err == nil {
				// we unlocked data with the fallback key, we are not in
				// "fully" degraded mode, but we do need to track that we had to
				// use the fallback key
				degradedState.DataKey = "fallback"
			} else {
				// TODO: should we introspect err here to get a more detailed
				//       response in degradedState and in the log message
				// we failed to decrypt the device again with the fallback key,
				// so we are for sure in degraded mode
				logger.Noticef("failed to find or unlock encrypted ubuntu-data partition for mounting host data: %v", err)
				degradedState.DataState = "enc-not-found"
			}
		} else {
			// not a decrypted device, so nothing to fall back to try and unlock
			// data, so just mark it not-found
			// TODO:UC20: should we save the specific error in degradedState
			//            somewhere in addition to logging it?
			logger.Noticef("failed to find ubuntu-data partition for mounting host data: %v", err)
			degradedState.DataState = "other"
		}
	}

	// 3.1 try to mount ubuntu-data and verify it comes from the right disk
	diskOpts := &disks.Options{}
	if isDecryptDev {
		// then we need to specify that the data mountpoint is expected to be a
		// decrypted device, applies to both ubuntu-data and ubuntu-save
		diskOpts.IsDecryptedDevice = true
	}

	if degradedState.DataState == "" {
		// then we have not failed catastrophically, and dataDevice us what we
		// want to mount

		// don't do fsck on the data partition, it could be corrupted
		if err := doSystemdMount(dataDevice, boot.InitramfsHostUbuntuDataDir, nil); err == nil {
			// we mounted it successfully, verify it comes from the right disk
			matches, err := disk.MountPointIsFromDisk(boot.InitramfsHostUbuntuDataDir, diskOpts)
			if err != nil {
				return err
			}
			if !matches {
				return fmt.Errorf("cannot validate boot: ubuntu-data mountpoint is expected to be from disk %s but is not", disk.Dev())
			}

			// otherwise it matches
			degradedState.DataState = "mounted"
			degradedState.HostLocation = boot.InitramfsHostUbuntuDataDir
		} else {
			// we failed to mount it, proceed with degraded mode
			degradedState.DataState = "not-mounted"
		}
	}
	// otherwise we failed / exhausted all efforts around finding the device
	// to mount for ubuntu-data host and DataState already contains information
	// about what happened before this, so we just skip trying to mount it
	// entirely

	// 3.2  try to mount ubuntu-save (if present)
	var haveSave bool
	if isDecryptDev {
		var saveDevice string
		// we know we are encrypted, so we should try to unlock ubuntu-save
		// first method we try is the run key, but we can only do this if we
		// have the host ubuntu-data mounted with the unsealed "bare" key
		if degradedState.HostLocation != "" {
			// meh this is cheating a bit
			saveKey := filepath.Join(dirs.SnapFDEDirUnder(boot.InitramfsHostWritableDir), "ubuntu-save.key")
			// we have save.key, volume exists and is encrypted
			key, err := ioutil.ReadFile(saveKey)
			if err == nil {
				saveDevice, err = secbootUnlockEncryptedVolumeUsingKey(disk, "ubuntu-save", key)
				if err != nil {
					logger.Noticef("cannot unlock ubuntu-save volume: %v", err)
					degradedState.SaveState = "other"
				}
				if err := doSystemdMount(saveDevice, boot.InitramfsUbuntuSaveDir, nil); err == nil {
					degradedState.SaveKey = "run"
					degradedState.SaveState = "mounted"
					haveSave = true
				} else {
					// ubuntu-save exists but we couldn't mount it with the
					// main unencrypted key for some reason - we need to fall
					// back and try with the recovery save key
					saveFallbackKey := filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-save.recovery.sealed-key")
					saveDevice, isDecryptDev, err = secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-save", saveFallbackKey, lockKeysOnFinish)
					if err != nil {
						if isDecryptDev {
							logger.Noticef("failed to find or unlock encrypted ubuntu-save partition: %v", err)
							degradedState.SaveState = "enc-not-found"
						} else {
							// either catastrophic error or inconsistent disk, if
							// ubuntu-data was an encrypted device, then ubuntu-save
							// must be also
							logger.Noticef("cannot unlock ubuntu-save: %v", err)
							degradedState.SaveState = "other"
						}
					} else {
						// now mount ubuntu-save
						if err := doSystemdMount(saveDevice, boot.InitramfsUbuntuSaveDir, nil); err == nil {
							degradedState.SaveKey = "fallback"
							degradedState.SaveState = "mounted"
							haveSave = true
						} else {
							// ubuntu-save exists but we couldn't mount it
							logger.Noticef("error mounting decrypted ubuntu-save from partition %s: %v", saveDevice, err)
							degradedState.SaveState = "not-mounted"
						}
					}
				}
			} else {
				logger.Noticef("cannot read run-mode key for ubuntu-save: %v", err)
				degradedState.SaveState = "other"
			}
		} else {
			// we don't have ubuntu-data host to get the unsealed "bare" key, so
			// we have to unlock with the sealed one from ubuntu-seed
			saveFallbackKey := filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-save.recovery.sealed-key")
			saveDevice, isDecryptDev, err = secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-save", saveFallbackKey, lockKeysOnFinish)
			if err != nil {
				if isDecryptDev {
					logger.Noticef("failed to find or unlock encrypted ubuntu-save partition: %v", err)
					degradedState.SaveState = "enc-not-found"
				} else {
					// either catastrophic error or inconsistent disk, if
					// ubuntu-data was an encrypted device, then ubuntu-save
					// must be also
					logger.Noticef("cannot unlock ubuntu-save: %v", err)
					degradedState.SaveState = "other"
				}
			} else {
				// now mount ubuntu-save
				if err := doSystemdMount(saveDevice, boot.InitramfsUbuntuSaveDir, nil); err == nil {
					degradedState.SaveState = "mounted"
					degradedState.SaveKey = "fallback"
					haveSave = true
				} else {
					// ubuntu-save exists but we couldn't mount it
					logger.Noticef("error mounting decrypted ubuntu-save from partition %s: %v", saveDevice, err)
					degradedState.SaveState = "not-mounted"
				}
			}
		}
	} else {
		// we are not on an encrypted system, so try to mount ubuntu-save
		// directly
		partUUID, err := disk.FindMatchingPartitionUUID("ubuntu-save")
		if err == nil {
			// found it, try to mount it w/o any options
			// TODO: should we fsck ubuntu-save ?
			saveDevice := filepath.Join("/dev/disk/by-partuuid", partUUID)
			if err := doSystemdMount(saveDevice, boot.InitramfsUbuntuSaveDir, nil); err == nil {
				degradedState.SaveState = "mounted"
				haveSave = true
			} else {
				// ubuntu-save exists but we couldn't mount it
				logger.Noticef("error mounting ubuntu-save from partition %s: %v", partUUID, err)
				degradedState.SaveState = "not-mounted"
			}
		} else {
			// error locating ubuntu-save
			if _, ok := err.(disks.FilesystemLabelNotFoundError); ok {
				// this is ok, ubuntu-save may not exist for
				// non-encrypted device
				degradedState.SaveState = "not-needed"
			} else {
				// the error is not "not-found", so we have a real error
				// mounting a save that exists
				logger.Noticef("error identifying ubuntu-save partition: %v", err)
				degradedState.SaveState = "other"
			}
		}
	}

	if haveSave {
		// 3.2a we have ubuntu-save, verify it as well
		matches, err := disk.MountPointIsFromDisk(boot.InitramfsUbuntuSaveDir, diskOpts)
		if err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("cannot validate boot: ubuntu-save mountpoint is expected to be from disk %s but is not", disk.Dev())
		}
	}

	// 3.3 write out degraded.json
	b, err := json.Marshal(degradedState)
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
	if degradedState.HostLocation != "" {
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

		// TODO
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
