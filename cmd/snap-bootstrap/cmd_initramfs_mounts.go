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
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	const (
		short = "Generate initramfs mount tuples"
		long  = "Generate mount tuples for the initramfs until nothing more can be done"
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
	// Stdout - can be overridden in tests
	stdout io.Writer = os.Stdout

	secbootMeasureSnapSystemEpochWhenPossible = secboot.MeasureSnapSystemEpochWhenPossible
	secbootMeasureSnapModelWhenPossible       = secboot.MeasureSnapModelWhenPossible
	secbootUnlockVolumeIfEncrypted            = secboot.UnlockVolumeIfEncrypted

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

	mst := newInitramfsMountsState(mode, recoverySystem)

	switch mode {
	case "recover":
		// XXX: don't pass both args
		return generateMountsModeRecover(mst, recoverySystem)
	case "install":
		// XXX: don't pass both args
		return generateMountsModeInstall(mst, recoverySystem)
	case "run":
		return generateMountsModeRun(mst)
	}
	// this should never be reached
	return fmt.Errorf("internal error: mode in generateInitramfsMounts not handled")
}

// generateMountsMode* is called multiple times from initramfs until it
// no longer generates more mount points and just returns an empty output.
func generateMountsModeInstall(mst *initramfsMountsState, recoverySystem string) error {
	// steps 1 and 2 are shared with recover mode
	allMounted, err := generateMountsCommonInstallRecover(mst, recoverySystem)
	if err != nil {
		return err
	}
	if !allMounted {
		return nil
	}

	// 3. final step: write modeenv to tmpfs data dir and disable cloud-init in
	//   install mode
	modeEnv := &boot.Modeenv{
		Mode:           "install",
		RecoverySystem: recoverySystem,
	}
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir); err != nil {
		return err
	}
	// we need to put the file to disable cloud-init in the
	// _writable_defaults dir for writable-paths(5) to install it properly
	writableDefaultsDir := sysconfig.WritableDefaultsDir(boot.InitramfsWritableDir)
	if err := sysconfig.DisableCloudInit(writableDefaultsDir); err != nil {
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
	} {
		if err := copyFromGlobHelper(src, dst, globEx); err != nil {
			return err
		}
	}
	return nil
}

// copyUbuntuDataAuth copies the authenication files like
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
		// so that the time in recover mode moves forward to what it was in run
		// mode
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

func generateMountsModeRecover(mst *initramfsMountsState, recoverySystem string) error {
	// steps 1 and 2 are shared with install mode
	allMounted, err := generateMountsCommonInstallRecover(mst, recoverySystem)
	if err != nil {
		return err
	}
	if !allMounted {
		return nil
	}

	// 3. mount ubuntu-data for recovery
	isRecoverDataMounted, err := mst.IsMounted(boot.InitramfsHostUbuntuDataDir)
	if err != nil {
		return err
	}
	if !isRecoverDataMounted {
		const lockKeysForLast = true
		device, err := secbootUnlockVolumeIfEncrypted("ubuntu-data", lockKeysForLast)
		if err != nil {
			return err
		}

		fmt.Fprintf(stdout, "%s %s\n", device, boot.InitramfsHostUbuntuDataDir)
		return nil
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

	modeEnv := &boot.Modeenv{
		Mode:           "recover",
		RecoverySystem: recoverySystem,
	}
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir); err != nil {
		return err
	}
	// we need to put the file to disable cloud-init in the
	// _writable_defaults dir for writable-paths(5) to install it properly
	writableDefaultsDir := sysconfig.WritableDefaultsDir(boot.InitramfsWritableDir)
	if err := sysconfig.DisableCloudInit(writableDefaultsDir); err != nil {
		return err
	}

	// done, no output, no error indicates to initramfs we are done with
	// mounting stuff
	return nil
}

// TODO:UC20: move all of this to a helper in boot?
// selectPartitionToMount will select the partition to mount at dir, preferring
// to use efi variables to determine which partition matches the disk we booted
// the kernel from. If it can't figure out which disk the kernel came from, then
// it will fallback to mounting via the specified label
func selectPartitionMatchingKernelDisk(dir, fallbacklabel string) error {
	// TODO:UC20: should this only run on grade > dangerous? where do we
	//            get the model at this point?
	partuuid, err := bootFindPartitionUUIDForBootedKernelDisk()
	if err != nil {
		// no luck, try mounting by label instead
		fmt.Fprintf(stdout, "/dev/disk/by-label/%s %s\n", fallbacklabel, dir)
		return nil
	}
	// TODO: the by-partuuid is only available on gpt disks, on mbr we need
	//       to use by-uuid or by-id
	fmt.Fprintf(stdout, "/dev/disk/by-partuuid/%s %s\n", partuuid, dir)
	return nil
}

func generateMountsCommonInstallRecover(mst *initramfsMountsState, recoverySystem string) (allMounted bool, err error) {
	// 1. always ensure seed partition is mounted first before the others,
	//      since the seed partition is needed to mount the snap files there
	isMounted, err := mst.IsMounted(boot.InitramfsUbuntuSeedDir)
	if err != nil {
		return false, err
	}
	if !isMounted {
		return false, selectPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "ubuntu-seed")
	}

	// 2.1. measure model
	err = stampedAction(fmt.Sprintf("%s-model-measured", recoverySystem), func() error {
		return secbootMeasureSnapModelWhenPossible(mst.Model)
	})
	if err != nil {
		return false, err
	}

	// 2.2. (auto) select recovery system and mount seed snaps
	isBaseMounted, err := mst.IsMounted(filepath.Join(boot.InitramfsRunMntDir, "base"))
	if err != nil {
		return false, err
	}
	isKernelMounted, err := mst.IsMounted(filepath.Join(boot.InitramfsRunMntDir, "kernel"))
	if err != nil {
		return false, err
	}
	isSnapdMounted, err := mst.IsMounted(filepath.Join(boot.InitramfsRunMntDir, "snapd"))
	if err != nil {
		return false, err
	}
	if !isBaseMounted || !isKernelMounted || !isSnapdMounted {
		// load the recovery system and generate mounts for kernel/base
		// and snapd
		var whichTypes []snap.Type
		if !isBaseMounted {
			whichTypes = append(whichTypes, snap.TypeBase)
		}
		if !isKernelMounted {
			whichTypes = append(whichTypes, snap.TypeKernel)
		}
		if !isSnapdMounted {
			whichTypes = append(whichTypes, snap.TypeSnapd)
		}
		essSnaps, err := mst.RecoverySystemEssentialSnaps("", whichTypes)
		if err != nil {
			return false, fmt.Errorf("cannot load metadata and verify essential bootstrap snaps %v: %v", whichTypes, err)
		}

		// TODO:UC20: do we need more cross checks here?
		for _, essentialSnap := range essSnaps {
			switch essentialSnap.EssentialType {
			case snap.TypeBase:
				fmt.Fprintf(stdout, "%s %s\n", essentialSnap.Path, filepath.Join(boot.InitramfsRunMntDir, "base"))
			case snap.TypeKernel:
				// TODO:UC20: we need to cross-check the kernel path with snapd_recovery_kernel used by grub
				fmt.Fprintf(stdout, "%s %s\n", essentialSnap.Path, filepath.Join(boot.InitramfsRunMntDir, "kernel"))
			case snap.TypeSnapd:
				fmt.Fprintf(stdout, "%s %s\n", essentialSnap.Path, filepath.Join(boot.InitramfsRunMntDir, "snapd"))
			}
		}
	}

	// 2.3. mount "ubuntu-data" on a tmpfs
	isMounted, err = mst.IsMounted(boot.InitramfsDataDir)
	if err != nil {
		return false, err
	}
	if !isMounted {
		fmt.Fprintf(stdout, "--type=tmpfs tmpfs %s\n", boot.InitramfsDataDir)
		return false, nil
	}

	return true, nil
}

func generateMountsModeRun(mst *initramfsMountsState) error {
	// 1. mount ubuntu-boot
	isMounted, err := mst.IsMounted(boot.InitramfsUbuntuBootDir)
	if err != nil {
		return err
	}
	if !isMounted {
		return selectPartitionMatchingKernelDisk(boot.InitramfsUbuntuBootDir, "ubuntu-boot")
	}

	// 2. mount ubuntu-seed
	isMounted, err = mst.IsMounted(boot.InitramfsUbuntuSeedDir)
	if err != nil {
		return err
	}
	if !isMounted {
		// TODO:UC20: use the ubuntu-boot partition as a reference for what
		//            partition to mount for ubuntu-seed
		fmt.Fprintf(stdout, "/dev/disk/by-label/ubuntu-seed %s\n", boot.InitramfsUbuntuSeedDir)
		return nil
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

	// 3.2. mount Data, and exit, as it needs to be mounted for us to do step 2
	isDataMounted, err := mst.IsMounted(boot.InitramfsDataDir)
	if err != nil {
		return err
	}
	if !isDataMounted {
		const lockKeysForLast = true
		device, err := secbootUnlockVolumeIfEncrypted("ubuntu-data", lockKeysForLast)
		if err != nil {
			return err
		}

		fmt.Fprintf(stdout, "%s %s\n", device, boot.InitramfsDataDir)
		return nil
	}

	// 4.1. read modeenv
	modeEnv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
	if err != nil {
		return err
	}

	// 4.2. check if base is mounted
	isBaseMounted, err := mst.IsMounted(filepath.Join(boot.InitramfsRunMntDir, "base"))
	if err != nil {
		return err
	}
	if !isBaseMounted {
		// 4.3. use modeenv base_status and try_base to see if we are trying
		//      an update to the base snap
		base := modeEnv.Base
		if base == "" {
			// we have no fallback base!
			return fmt.Errorf("modeenv corrupt: missing base setting")
		}
		if modeEnv.BaseStatus == boot.TryStatus {
			// then we are trying a base snap update and there should be a
			// try_base set in the modeenv too
			if modeEnv.TryBase != "" {
				// check that the TryBase exists in ubuntu-data
				tryBaseSnapPath := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), modeEnv.TryBase)
				if osutil.FileExists(tryBaseSnapPath) {
					// set the TryBase and have the initramfs mount this base
					// snap
					modeEnv.BaseStatus = boot.TryingStatus
					base = modeEnv.TryBase
				} else {
					logger.Noticef("try-base snap %q does not exist", modeEnv.TryBase)
				}
			} else {
				logger.Noticef("try-base snap is empty, but \"base_status\" is \"trying\"")
			}
		} else if modeEnv.BaseStatus == boot.TryingStatus {
			// snapd failed to start with the base snap update, so we need to
			// fallback to the old base snap and clear base_status
			modeEnv.BaseStatus = boot.DefaultStatus
		} else if modeEnv.BaseStatus != boot.DefaultStatus {
			logger.Noticef("\"base_status\" has an invalid setting: %q", modeEnv.BaseStatus)
		}

		baseSnapPath := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), base)
		fmt.Fprintf(stdout, "%s %s\n", baseSnapPath, filepath.Join(boot.InitramfsRunMntDir, "base"))
	}

	// 4.5 check if the kernel is mounted
	isKernelMounted, err := mst.IsMounted(filepath.Join(boot.InitramfsRunMntDir, "kernel"))
	if err != nil {
		return err
	}
	if !isKernelMounted {
		// 4.6. use bootloader boot env to see if we are trying an update to the
		//      kernel snap
		// make a map to easily check if a kernel snap is valid or not
		validKernels := make(map[string]bool, len(modeEnv.CurrentKernels))
		for _, validKernel := range modeEnv.CurrentKernels {
			validKernels[validKernel] = true
		}

		// find ubuntu-boot bootloader to get the kernel_status and kernel.efi
		// status so we can determine the right kernel snap to have mounted

		// TODO:UC20: should all this logic move to boot package? feels awfully
		// similar to the logic in revisions() for bootState20

		// At this point the run mode bootloader is under the native
		// layout, no /boot mount.
		opts := &bootloader.Options{NoSlashBoot: true}
		bl, err := bootloader.Find(boot.InitramfsUbuntuBootDir, opts)
		if err != nil {
			return fmt.Errorf("internal error: cannot find run system bootloader: %v", err)
		}

		var kern, tryKern snap.PlaceInfo
		var kernStatus string

		ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
		if ok {
			// use ebl methods
			kern, err = ebl.Kernel()
			if err != nil {
				return fmt.Errorf("no fallback kernel snap: %v", err)
			}

			tryKern, err = ebl.TryKernel()
			if err != nil && err != bootloader.ErrNoTryKernelRef {
				return err
			}

			m, err := ebl.GetBootVars("kernel_status")
			if err != nil {
				return fmt.Errorf("cannot get kernel_status from bootloader %s", ebl.Name())
			}

			kernStatus = m["kernel_status"]
		} else {
			// use the bootenv
			m, err := bl.GetBootVars("snap_kernel", "snap_try_kernel", "kernel_status")
			if err != nil {
				return err
			}
			kern, err = snap.ParsePlaceInfoFromSnapFileName(m["snap_kernel"])
			if err != nil {
				return fmt.Errorf("no fallback kernel snap: %v", err)
			}

			// only try to parse snap_try_kernel if it is set
			if m["snap_try_kernel"] != "" {
				tryKern, err = snap.ParsePlaceInfoFromSnapFileName(m["snap_try_kernel"])
				if err != nil {
					logger.Noticef("try-kernel setting is invalid: %v", err)
				}
			}

			kernStatus = m["kernel_status"]
		}

		kernelFile := kern.Filename()
		if !validKernels[kernelFile] {
			// we don't trust the fallback kernel!
			return fmt.Errorf("fallback kernel snap %q is not trusted in the modeenv", kernelFile)
		}

		if kernStatus == boot.TryingStatus {
			// check for the try kernel
			if tryKern != nil {
				tryKernelFile := tryKern.Filename()
				if validKernels[tryKernelFile] {
					kernelFile = tryKernelFile
				} else {
					logger.Noticef("try-kernel %q is not trusted in the modeenv", tryKernelFile)
				}
			} else {
				logger.Noticef("missing try-kernel, even though \"kernel_status\" is \"trying\"")
			}

			// TODO:UC20: actually we really shouldn't be falling back here at
			//            all - if the kernel we booted isn't mountable in the
			//            initramfs, we should trigger a reboot so that we boot
			//            the fallback kernel and then mount that one
		}

		kernelPath := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), kernelFile)
		fmt.Fprintf(stdout, "%s %s\n", kernelPath, filepath.Join(boot.InitramfsRunMntDir, "kernel"))
	}

	// 4.7. Maybe mount the snapd snap on first boot of run-mode
	if modeEnv.RecoverySystem != "" {
		isSnapdMounted, err := mst.IsMounted(filepath.Join(boot.InitramfsRunMntDir, "snapd"))
		if err != nil {
			return err
		}

		if !isSnapdMounted {
			// load the recovery system and generate mount for snapd
			essSnaps, err := mst.RecoverySystemEssentialSnaps(modeEnv.RecoverySystem, []snap.Type{snap.TypeSnapd})
			if err != nil {
				return fmt.Errorf("cannot load metadata and verify snapd snap: %v", err)
			}
			fmt.Fprintf(stdout, "%s %s\n", essSnaps[0].Path, filepath.Join(boot.InitramfsRunMntDir, "snapd"))
		}
	}

	// 4.8. Write the modeenv out again
	return modeEnv.Write()
}
