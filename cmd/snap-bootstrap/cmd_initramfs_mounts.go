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

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/timings"
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
)

var (
	osutilIsMounted = osutil.IsMounted

	bootFindPartitionUUIDForBootedKernelDisk = boot.FindPartitionUUIDForBootedKernelDisk

	systemdCryptSetup = "/usr/lib/systemd/systemd-cryptsetup"
)

func recoverySystemEssentialSnaps(seedDir, recoverySystem string, essentialTypes []snap.Type) ([]*seed.Snap, error) {
	systemSeed, err := seed.Open(seedDir, recoverySystem)
	if err != nil {
		return nil, err
	}

	seed20, ok := systemSeed.(seed.EssentialMetaLoaderSeed)
	if !ok {
		return nil, fmt.Errorf("internal error: UC20 seed must implement EssentialMetaLoaderSeed")
	}

	// load assertions into a temporary database
	if err := systemSeed.LoadAssertions(nil, nil); err != nil {
		return nil, err
	}

	// load and verify metadata only for the relevant essential snaps
	perf := timings.New(nil)
	if err := seed20.LoadEssentialMeta(essentialTypes, perf); err != nil {
		return nil, err
	}

	return seed20.EssentialSnaps(), nil
}

// TODO:UC20: move all of this to a helper in boot?
// selectPartitionToMount will select the partition to mount at dir, preferring
// to use efi variables to determine which partition matches the disk we booted
// the kernel from. If it can't figure out which disk the kernel came from, then
// it will fallback to mounting via the specified label
func selectPartitionMatchingKernelDisk(dir, fallbacklabel string) error {
	// TODO:UC20: should this only run on grade > dangerous? where do we
	//            get the model at this point?
	if partuuid, err := bootFindPartitionUUIDForBootedKernelDisk(); err == nil {
		fmt.Fprintf(stdout, "/dev/disk/by-partuuid/%s %s\n", partuuid, dir)
		return nil
	}
	// no luck, try mounting by label instead
	fmt.Fprintf(stdout, "/dev/disk/by-label/%s %s\n", fallbacklabel, dir)
	return nil
}

// generateMountsMode* is called multiple times from initramfs until it
// no longer generates more mount points and just returns an empty output.
func generateMountsModeInstall(recoverySystem string) error {
	// 1.1 mount ubuntu-boot first, using the LoaderDevicePartUUID to identify
	//     the partuiid partition that should be mounted
	//     in install mode, we should be getting our kernel from ubuntu-seed
	isMounted, err := osutilIsMounted(boot.InitramfsUbuntuSeedDir)
	if err != nil {
		return err
	}
	if !isMounted {
		return selectPartitionMatchingKernelDisk(boot.InitramfsUbuntuSeedDir, "ubuntu-seed")
	}

	// 2. (auto) select recovery system for now
	isBaseMounted, err := osutilIsMounted(filepath.Join(boot.InitramfsRunMntDir, "base"))
	if err != nil {
		return err
	}
	isKernelMounted, err := osutilIsMounted(filepath.Join(boot.InitramfsRunMntDir, "kernel"))
	if err != nil {
		return err
	}
	isSnapdMounted, err := osutilIsMounted(filepath.Join(boot.InitramfsRunMntDir, "snapd"))
	if err != nil {
		return err
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
		essSnaps, err := recoverySystemEssentialSnaps(boot.InitramfsUbuntuSeedDir, recoverySystem, whichTypes)
		if err != nil {
			return fmt.Errorf("cannot load metadata and verify essential bootstrap snaps %v: %v", whichTypes, err)
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

	// 3. mount "ubuntu-data" on a tmpfs
	isMounted, err = osutilIsMounted(boot.InitramfsUbuntuDataDir)
	if err != nil {
		return err
	}
	if !isMounted {
		// TODO:UC20: is there a better way?
		fmt.Fprintf(stdout, "--type=tmpfs tmpfs /run/mnt/ubuntu-data\n")
		return nil
	}

	// 4. final step: write $(ubuntu_data)/var/lib/snapd/modeenv - this
	//    is the tmpfs we just created above
	modeEnv := &boot.Modeenv{
		Mode:           "install",
		RecoverySystem: recoverySystem,
	}
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir); err != nil {
		return err
	}
	// and disable cloud-init in install mode
	if err := sysconfig.DisableCloudInit(boot.InitramfsWritableDir); err != nil {
		return err
	}

	// 5. done, no output, no error indicates to initramfs we are done
	//    with mounting stuff
	return nil
}

func generateMountsModeRecover(recoverySystem string) error {
	return fmt.Errorf("recover mode mount generation not implemented yet")
}

// helper to mount a partition from the same disk as another previously mounted
// partition
func mountPartitionLabelFromSameDiskAsMountPoint(diskmountpoint, label, target string) error {
	// get the disk for the mountpoint
	disk, err := partition.DiskFromMountPoint(diskmountpoint, nil)
	if err != nil {
		return err
	}

	// find the partition on that disk with the label
	partuuid, err := disk.FindMatchingPartitionUUID(label)
	if err != nil {
		return err
	}

	// mount that and return
	fmt.Fprintf(stdout, "/dev/disk/by-partuuid/%s %s\n", partuuid, target)
	return nil
}

func generateMountsModeRun() error {
	// 1. mount ubuntu-boot first, using the LoaderDevicePartUUID to identify
	//    the partuiid partition that should be mounted
	//    in run mode, we should be getting our kernel from ubuntu-boot

	isMounted, err := osutilIsMounted(boot.InitramfsUbuntuBootDir)
	if err != nil {
		return err
	}
	if !isMounted {
		return selectPartitionMatchingKernelDisk(boot.InitramfsUbuntuBootDir, "ubuntu-boot")
	}

	// 2.1 mount ubuntu-seed partition from same disk as ubuntu-boot
	isMounted, err = osutilIsMounted(boot.InitramfsUbuntuSeedDir)
	if err != nil {
		return err
	}
	if !isMounted {
		// get the disk that the ubuntu-boot mount point comes from, as that
		// should have been where we booted the kernel from in run mode
		return mountPartitionLabelFromSameDiskAsMountPoint(
			boot.InitramfsUbuntuBootDir, // existing mountpoint
			"ubuntu-seed",               // label to mount
			boot.InitramfsUbuntuSeedDir, // destination mountpoint
		)
	}

	// 2.2 cross check that ubuntu-boot and ubuntu-seed mounts come from the
	//       same physical disk, if they don't something went wrong and we need
	//       to stop booting
	bootDisk, err := partition.DiskFromMountPoint(boot.InitramfsUbuntuBootDir, nil)
	if err != nil {
		return err
	}
	same, err := bootDisk.MountPointIsFromDisk(boot.InitramfsUbuntuSeedDir, nil)
	if err != nil {
		return err
	}
	if !same {
		// whoops ...
		return fmt.Errorf("ubuntu-seed partition and ubuntu-boot partition are not from the same disk")
	}

	// 3.1 mount Data, and exit, as it needs to be mounted for us to do step 2
	isDataMounted, err := osutilIsMounted(boot.InitramfsUbuntuDataDir)
	if err != nil {
		return err
	}
	dataName := filepath.Base(boot.InitramfsUbuntuDataDir)
	if !isDataMounted {
		return maybeUnlockAndMount(dataName)
	}

	// 3.2 cross check that ubuntu-data and ubuntu-boot mounts come from the
	//     same physical disk, if they don't something went wrong and we need
	//     to stop booting
	if err := crossCheckDataPartition(dataName, bootDisk); err != nil {
		return err
	}

	// 4.1 read modeenv
	modeEnv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
	if err != nil {
		return err
	}

	// 4.2 check if base is mounted
	isBaseMounted, err := osutilIsMounted(filepath.Join(boot.InitramfsRunMntDir, "base"))
	if err != nil {
		return err
	}
	if !isBaseMounted {
		// 4.2.1 use modeenv base_status and try_base to see if we are trying
		// an update to the base snap
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
			// TODO:UC20: log a message if try_base is unset here?
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

	// 4.3 check if the kernel is mounted
	isKernelMounted, err := osutilIsMounted(filepath.Join(boot.InitramfsRunMntDir, "kernel"))
	if err != nil {
		return err
	}
	if !isKernelMounted {
		// 4.3.1 choose which kernel snap to mount

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

		// make sure it supports extracted run kernel images, as we have to find the
		// extracted run kernel image
		ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
		if !ok {
			return fmt.Errorf("cannot use %s bootloader: does not support extracted run kernel images", bl.Name())
		}

		// get the primary extracted run kernel
		kernel, err := ebl.Kernel()
		if err != nil {
			// we don't have a fallback kernel!
			return fmt.Errorf("no fallback kernel snap: %v", err)
		}

		kernelFile := kernel.Filename()
		if !validKernels[kernelFile] {
			// we don't trust the fallback kernel!
			return fmt.Errorf("fallback kernel snap %q is not trusted in the modeenv", kernelFile)
		}

		// get kernel_status
		m, err := ebl.GetBootVars("kernel_status")
		if err != nil {
			return fmt.Errorf("cannot get kernel_status from bootloader %s", ebl.Name())
		}

		if m["kernel_status"] == boot.TryingStatus {
			// check for the try kernel
			tryKernel, err := ebl.TryKernel()
			if err == nil {
				tryKernelFile := tryKernel.Filename()
				if validKernels[tryKernelFile] {
					kernelFile = tryKernelFile
				} else {
					logger.Noticef("try-kernel %q is not trusted in the modeenv", tryKernelFile)
				}
			} else if err != bootloader.ErrNoTryKernelRef {
				logger.Noticef("missing try-kernel, even though \"kernel_status\" is \"trying\"")
			}
			// if we didn't have a try kernel, but we do have kernel_status ==
			// trying we just fallback to using the normal kernel
			// same goes for try kernel being untrusted - we will fallback to
			// the normal kernel snap

			// TODO:UC20: actually we really shouldn't be falling back here at
			//            all - if the kernel we booted isn't mountable in the
			//            initramfs, we should trigger a reboot so that we boot
			//            the fallback kernel and then mount that one
		}

		kernelPath := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), kernelFile)
		fmt.Fprintf(stdout, "%s %s\n", kernelPath, filepath.Join(boot.InitramfsRunMntDir, "kernel"))
	}

	// 5. Maybe mount the snapd snap on first boot of run-mode
	// TODO:UC20: Make RecoverySystem empty after successful first boot
	// somewhere in devicestate
	if modeEnv.RecoverySystem != "" {
		isSnapdMounted, err := osutilIsMounted(filepath.Join(boot.InitramfsRunMntDir, "snapd"))
		if err != nil {
			return err
		}

		if !isSnapdMounted {
			// load the recovery system and generate mount for snapd
			essSnaps, err := recoverySystemEssentialSnaps(boot.InitramfsUbuntuSeedDir, modeEnv.RecoverySystem, []snap.Type{snap.TypeSnapd})
			if err != nil {
				return fmt.Errorf("cannot load metadata and verify snapd snap: %v", err)
			}
			fmt.Fprintf(stdout, "%s %s\n", essSnaps[0].Path, filepath.Join(boot.InitramfsRunMntDir, "snapd"))
		}
	}

	// 6. Write the modeenv out again
	return modeEnv.Write()
}

func generateInitramfsMounts() error {
	mode, recoverySystem, err := boot.ModeAndRecoverySystemFromKernelCommandLine()
	if err != nil {
		return err
	}
	switch mode {
	case "recover":
		return generateMountsModeRecover(recoverySystem)
	case "install":
		return generateMountsModeInstall(recoverySystem)
	case "run":
		return generateMountsModeRun()
	}
	// this should never be reached
	return fmt.Errorf("internal error: mode in generateInitramfsMounts not handled")
}

func crossCheckDataPartition(name string, d partition.Disk) error {
	// if ubuntu-data is encrypted (as indicated by the keyfile here), then we
	// need to verify that the physical backing partition for ubuntu-data (i.e.
	// the actual encrypted partition ubuntu-data-enc) is on the same disk
	// TODO:UC20: update this with the real way to check if the disk is
	//            encrypted when that's available
	keyfile := filepath.Join(boot.InitramfsUbuntuBootDir, name+".keyfile.unsealed")
	opts := &partition.Options{}
	if osutil.FileExists(keyfile) {
		opts.IsDecryptedDevice = true
	}

	same, err := d.MountPointIsFromDisk(boot.InitramfsUbuntuDataDir, opts)
	if err != nil {
		return fmt.Errorf("cannot verify ubuntu-data partition and ubuntu-boot partition are from the same disk: %v", err)
	}
	if !same {
		// whoops ...
		return fmt.Errorf("ubuntu-data partition and ubuntu-boot partition are not from the same disk")
	}

	return nil
}

func maybeUnlockAndMount(name string) error {
	// TODO:UC20: will need to unseal key to unlock LUKS here
	keyfile := filepath.Join(boot.InitramfsUbuntuBootDir, name+".keyfile.unsealed")
	if osutil.FileExists(keyfile) {
		// encrypted case, so we need to mount the name-enc
		// we only unlock in run mode, and in run mode the kernel should have
		// come from the ubuntu-boot mount point, so first use the ubuntu-boot
		// mountpoint to find the partition uuid for ubuntu-boot

		// get the disk for the mountpoint
		disk, err := partition.DiskFromMountPoint(boot.InitramfsUbuntuBootDir, nil)
		if err != nil {
			return err
		}

		// find the partition on that disk with the encrypted device label
		// (which is just the name + -enc at the end, i.e. ubuntu-data-enc)
		partuuid, err := disk.FindMatchingPartitionUUID(name + "-enc")
		if err != nil {
			return err
		}

		diskDevice := filepath.Join("/dev/disk/by-partuuid", partuuid)
		cmd := exec.Command(systemdCryptSetup, "attach", name, diskDevice, keyfile)
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "SYSTEMD_LOG_TARGET=console")
		if output, err := cmd.CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}

		// now request it to be mounted - note we need to use the label name
		// here as when systemd seeds it needs to mount that label, it will
		// internally go to unlock the partition uuid we specified above
		fmt.Fprintf(stdout, "%s %s\n", filepath.Join("/dev/disk/by-label", name), boot.InitramfsUbuntuDataDir)
		return nil
	}

	// unencrypted case, just cross-check that the partitions match and mount
	// ubuntu-data directly
	return mountPartitionLabelFromSameDiskAsMountPoint(
		boot.InitramfsUbuntuBootDir, // existing mountpoint
		name,                        // label to mount
		boot.InitramfsUbuntuDataDir, // destination mountpoint
	)
}
