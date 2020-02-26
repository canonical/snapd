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
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
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
	runMnt = "/run/mnt"

	osutilIsMounted = osutil.IsMounted
)

// generateMountsMode* is called multiple times from initramfs until it
// no longer generates more mount points and just returns an empty output.
func generateMountsModeInstall(recoverySystem string) error {
	seedDir := filepath.Join(runMnt, "ubuntu-seed")

	// 1. always ensure seed partition is mounted
	isMounted, err := osutilIsMounted(seedDir)
	if err != nil {
		return err
	}
	if !isMounted {
		fmt.Fprintf(stdout, "/dev/disk/by-label/ubuntu-seed %s\n", seedDir)
		return nil
	}

	// 2. (auto) select recovery system for now
	isBaseMounted, err := osutilIsMounted(filepath.Join(runMnt, "base"))
	if err != nil {
		return err
	}
	isKernelMounted, err := osutilIsMounted(filepath.Join(runMnt, "kernel"))
	if err != nil {
		return err
	}
	isSnapdMounted, err := osutilIsMounted(filepath.Join(runMnt, "snapd"))
	if err != nil {
		return err
	}
	if !isBaseMounted || !isKernelMounted || !isSnapdMounted {
		// load the recovery system  and generate mounts for kernel/base
		systemSeed, err := seed.Open(seedDir, recoverySystem)
		if err != nil {
			return err
		}
		// load assertions into a temporary database
		if err := systemSeed.LoadAssertions(nil, nil); err != nil {
			return err
		}
		perf := timings.New(nil)
		// TODO:UC20: LoadMeta will verify all the snaps in the
		// seed, that is probably too much. We can expose more
		// dedicated helpers for this later.
		if err := systemSeed.LoadMeta(perf); err != nil {
			return err
		}
		// TODO:UC20: do we need more cross checks here?
		for _, essentialSnap := range systemSeed.EssentialSnaps() {
			switch essentialSnap.EssentialType {
			case snap.TypeBase:
				if !isBaseMounted {
					fmt.Fprintf(stdout, "%s %s\n", essentialSnap.Path, filepath.Join(runMnt, "base"))
				}
			case snap.TypeKernel:
				if !isKernelMounted {
					// TODO:UC20: we need to cross-check the kernel path with snapd_recovery_kernel used by grub
					fmt.Fprintf(stdout, "%s %s\n", essentialSnap.Path, filepath.Join(runMnt, "kernel"))
				}
			case snap.TypeSnapd:
				if !isSnapdMounted {
					fmt.Fprintf(stdout, "%s %s\n", essentialSnap.Path, filepath.Join(runMnt, "snapd"))
				}
			}
		}
	}

	// 3. mount "ubuntu-data" on a tmpfs
	isMounted, err = osutilIsMounted(filepath.Join(runMnt, "ubuntu-data"))
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
	if err := modeEnv.Write(filepath.Join(runMnt, "ubuntu-data", "system-data")); err != nil {
		return err
	}

	// 5. done, no output, no error indicates to initramfs we are done
	//    with mounting stuff
	return nil
}

func generateMountsModeRecover(recoverySystem string) error {
	return fmt.Errorf("recover mode mount generation not implemented yet")
}

func generateMountsModeRun() error {
	seedDir := filepath.Join(runMnt, "ubuntu-seed")
	bootDir := filepath.Join(runMnt, "ubuntu-boot")
	dataDir := filepath.Join(runMnt, "ubuntu-data")

	// 1.1 always ensure basic partitions are mounted
	for _, d := range []string{seedDir, bootDir} {
		isMounted, err := osutilIsMounted(d)
		if err != nil {
			return err
		}
		if !isMounted {
			fmt.Fprintf(stdout, "/dev/disk/by-label/%s %s\n", filepath.Base(d), d)
		}
	}

	// TODO:UC20: possibly will need to unseal key, and unlock LUKS here before
	//            proceeding to mount data

	// 1.2 mount Data, and exit, as it needs to be mounted for us to do step 2
	isDataMounted, err := osutilIsMounted(dataDir)
	if err != nil {
		return err
	}
	if !isDataMounted {
		fmt.Fprintf(stdout, "/dev/disk/by-label/%s %s\n", filepath.Base(dataDir), dataDir)
		return nil
	}
	// 2.1 read modeenv
	modeEnv, err := boot.ReadModeenv(filepath.Join(dataDir, "system-data"))
	if err != nil {
		return err
	}

	// 2.2.1 check if base is mounted
	isBaseMounted, err := osutilIsMounted(filepath.Join(runMnt, "base"))
	if err != nil {
		return err
	}
	if !isBaseMounted {
		// 2.2.2 use modeenv base_status and try_base to see  if we are trying
		// an update to the base snap
		base := modeEnv.Base
		if base == "" {
			// we have no fallback base!
			return fmt.Errorf("modeenv corrupt: missing base setting")
		}
		if modeEnv.BaseStatus == "try" {
			// then we are trying a base snap update and there should be a
			// try_base set in the modeenv too
			if modeEnv.TryBase != "" {
				// check that the TryBase exists in ubuntu-data
				tryBaseSnapPath := filepath.Join(dataDir, "system-data", dirs.SnapBlobDir, modeEnv.TryBase)
				if osutil.FileExists(tryBaseSnapPath) {
					// set the TryBase and have the initramfs mount this base
					// snap
					modeEnv.BaseStatus = "trying"
					base = modeEnv.TryBase
				}
				// TODO:UC20: log a message somewhere if try base snap does not
				//            exist?
			}
			// TODO:UC20: log a message if try_base is unset here?
		} else if modeEnv.BaseStatus == "trying" {
			// snapd failed to start with the base snap update, so we need to
			// fallback to the old base snap and clear base_status
			modeEnv.BaseStatus = ""
		}

		baseSnapPath := filepath.Join(dataDir, "system-data", dirs.SnapBlobDir, base)
		fmt.Fprintf(stdout, "%s %s\n", baseSnapPath, filepath.Join(runMnt, "base"))
	}

	// 2.3.1 check if the kernel is mounted
	isKernelMounted, err := osutilIsMounted(filepath.Join(runMnt, "kernel"))
	if err != nil {
		return err
	}
	if !isKernelMounted {
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
		bl, err := bootloader.Find(bootDir, opts)
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

		if m["kernel_status"] == "trying" {
			// check for the try kernel
			tryKernel, tryKernelExists, err := ebl.TryKernel()
			// TODO:UC20: can we log somewhere if err != nil here?
			if err == nil && tryKernelExists {
				// TODO:UC20: can we log somewhere if this kernel snap isn't in the
				//            list of trusted kernel snaps?
				tryKernelFile := tryKernel.Filename()
				if validKernels[tryKernelFile] {
					kernelFile = tryKernelFile
				}
			}
			// if we didn't have a try kernel, but we do have kernel_status ==
			// trying we just fallback to using the normal kernel
			// same goes for try kernel being untrusted - we will fallback to
			// the normal kernel snap
		}

		kernelPath := filepath.Join(dataDir, "system-data", dirs.SnapBlobDir, kernelFile)
		fmt.Fprintf(stdout, "%s %s\n", kernelPath, filepath.Join(runMnt, "kernel"))
	}

	// 3.1 Maybe mount the snapd snap on first boot of run-mode
	// TODO:UC20: Make RecoverySystem empty after successful first boot
	// somewhere in devicestate
	if modeEnv.RecoverySystem != "" {
		isSnapdMounted, err := osutilIsMounted(filepath.Join(runMnt, "snapd"))
		if err != nil {
			return err
		}

		if !isSnapdMounted {
			// TODO:UC20: refactor to combine this code with the install mode
			// bit in generateMountsModeInstall
			// load the recovery system  and generate mounts for kernel/base
			systemSeed, err := seed.Open(seedDir, modeEnv.RecoverySystem)
			if err != nil {
				return err
			}
			// load assertions into a temporary database
			if err := systemSeed.LoadAssertions(nil, nil); err != nil {
				return err
			}
			perf := timings.New(nil)
			// TODO:UC20: LoadMeta will verify all the snaps in the
			// seed, that is probably too much. We can expose more
			// dedicated helpers for this later.
			if err := systemSeed.LoadMeta(perf); err != nil {
				return err
			}
			// TODO:UC20: do we need more cross checks here?
			snapdSnapPath := ""
			for _, essentialSnap := range systemSeed.EssentialSnaps() {
				if essentialSnap.EssentialType == snap.TypeSnapd {
					snapdSnapPath = essentialSnap.Path
					break
				}
			}
			if snapdSnapPath == "" {
				// this should be impossible, LoadMeta and LoadAssertions will have
				// validated that the seed is valid and the snapd snap must exist
				// to be a valid seed
				return fmt.Errorf("internal error: seed on recovery system %s is broken", modeEnv.RecoverySystem)
			}

			fmt.Fprintf(stdout, "%s %s\n", snapdSnapPath, filepath.Join(runMnt, "snapd"))
		}
	}

	// 4.1 Write the modeenv out again
	return modeEnv.Write(filepath.Join(dataDir, "system-data"))
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
