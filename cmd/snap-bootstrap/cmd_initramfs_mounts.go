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

	// 2.X mount ubuntu-boot for access to the run mode key to unseal
	//     ubuntu-data
	// use the disk we mounted ubuntu-seed from as a reference to find
	// ubuntu-seed and mount it
	// TODO: w/ degraded mode we need to be robust against not being able to
	// find/mount ubuntu-boot and fallback to using keys from ubuntu-seed in
	// that case
	partUUID, err := disk.FindMatchingPartitionUUID("ubuntu-boot")
	if err != nil {
		return err
	}

	// should we fsck ubuntu-boot? probably yes because on some platforms
	// (u-boot for example) ubuntu-boot is vfat and it could have been unmounted
	// dirtily, and we need to fsck it to ensure it is mounted safely before
	// reading keys from it
	fsckSystemdOpts := &systemdMountOptions{
		NeedsFsck: true,
	}
	if err := doSystemdMount(fmt.Sprintf("/dev/disk/by-partuuid/%s", partUUID), boot.InitramfsUbuntuBootDir, fsckSystemdOpts); err != nil {
		return err
	}

	// 2.X+1, verify ubuntu-boot comes from same disk as ubuntu-seed
	matches, err := disk.MountPointIsFromDisk(boot.InitramfsUbuntuBootDir, nil)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("cannot validate boot: ubuntu-boot mountpoint is expected to be from disk %s but is not", disk.Dev())
	}

	// 3. mount ubuntu-data for recovery using run mode key
	runModeKey := filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key")
	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		LockKeysOnFinish: true,
		AllowRecoveryKey: true,
	}
	unlockRes, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-data", runModeKey, opts)
	if err != nil {
		return err
	}

	// don't do fsck on the data partition, it could be corrupted
	if err := doSystemdMount(unlockRes.Device, boot.InitramfsHostUbuntuDataDir, nil); err != nil {
		return err
	}

	// 3.1. mount ubuntu-save (if present)
	haveSave, err := maybeMountSave(disk, boot.InitramfsHostWritableDir, unlockRes.IsDecryptedDevice, nil)
	if err != nil {
		return err
	}

	// 3.2 verify that the host ubuntu-data comes from where we expect it to
	// right device
	diskOpts := &disks.Options{}
	if unlockRes.IsDecryptedDevice {
		// then we need to specify that the data mountpoint is expected to be a
		// decrypted device, applies to both ubuntu-data and ubuntu-save
		diskOpts.IsDecryptedDevice = true
	}

	matches, err = disk.MountPointIsFromDisk(boot.InitramfsHostUbuntuDataDir, diskOpts)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("cannot validate boot: ubuntu-data mountpoint is expected to be from disk %s but is not", disk.Dev())
	}
	if haveSave {
		// 3.2a we have ubuntu-save, verify it as well
		matches, err = disk.MountPointIsFromDisk(boot.InitramfsUbuntuSaveDir, diskOpts)
		if err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("cannot validate boot: ubuntu-save mountpoint is expected to be from disk %s but is not", disk.Dev())
		}
	}

	// 4. final step: copy the auth data and network config from
	//    the real ubuntu-data dir to the ephemeral ubuntu-data
	//    dir, write the modeenv to the tmpfs data, and disable
	//    cloud-init in recover mode
	if err := copyUbuntuDataAuth(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
		return err
	}
	if err := copyNetworkConfig(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
		return err
	}
	if err := copyUbuntuDataMisc(boot.InitramfsHostUbuntuDataDir, boot.InitramfsDataDir); err != nil {
		return err
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
