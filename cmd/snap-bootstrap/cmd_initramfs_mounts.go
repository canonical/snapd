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
	"github.com/snapcore/snapd/dirs"
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

var snapTypeToMountDir = map[snap.Type]string{
	snap.TypeBase:   "base",
	snap.TypeKernel: "kernel",
	snap.TypeSnapd:  "snapd",
}

var (
	osutilIsMounted = osutil.IsMounted
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

// generateMountsMode* is called multiple times from initramfs until it
// no longer generates more mount points and just returns an empty output.
func generateMountsModeInstall(recoverySystem string) error {
	// 1. always ensure seed partition is mounted
	isMounted, err := osutilIsMounted(boot.InitramfsUbuntuSeedDir)
	if err != nil {
		return err
	}
	if !isMounted {
		fmt.Fprintf(stdout, "/dev/disk/by-label/ubuntu-seed %s\n", boot.InitramfsUbuntuSeedDir)
		return nil
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

func generateMountsModeRun() error {
	// 1.1 always ensure basic partitions are mounted
	for _, d := range []string{boot.InitramfsUbuntuSeedDir, boot.InitramfsUbuntuBootDir} {
		isMounted, err := osutilIsMounted(d)
		if err != nil {
			return err
		}
		if !isMounted {
			fmt.Fprintf(stdout, "/dev/disk/by-label/%s %s\n", filepath.Base(d), d)
		}
	}

	// 1.2 mount Data, and exit, as it needs to be mounted for us to do step 2
	isDataMounted, err := osutilIsMounted(boot.InitramfsUbuntuDataDir)
	if err != nil {
		return err
	}
	if !isDataMounted {
		name := filepath.Base(boot.InitramfsUbuntuDataDir)
		device, err := unlockIfEncrypted(name)
		if err != nil {
			return err
		}

		fmt.Fprintf(stdout, "%s %s\n", device, boot.InitramfsUbuntuDataDir)
		return nil
	}

	// 2.1 read modeenv
	modeEnv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
	if err != nil {
		return err
	}

	// 2.2 check if base is mounted
	// 2.3 check if kernel is mounted
	var whichTypes []snap.Type
	for _, typ := range []snap.Type{snap.TypeBase, snap.TypeKernel} {
		dir := snapTypeToMountDir[typ]
		isMounted, err := osutilIsMounted(filepath.Join(boot.InitramfsRunMntDir, dir))
		if err != nil {
			return err
		}
		if !isMounted {
			whichTypes = append(whichTypes, typ)
		}
	}

	// 3. choose and mount base and kernel snaps (this includes updating modeenv
	//    if needed to try the base snap)
	mounts, err := boot.InitramfsRunModeChooseSnapsToMount(whichTypes, modeEnv)
	if err != nil {
		return err
	}

	// make sure this is a deterministic order
	// TODO:UC20: with grade > dangerous, verify the kernel snap hash against
	//            what we booted using the tpm log, this may need to be passed
	//            to the function above to make decisions there, or perhaps this
	//            code actually belongs in the bootloader implementation itself
	for _, typ := range []snap.Type{snap.TypeBase, snap.TypeKernel} {
		if sn, ok := mounts[typ]; ok {
			dir := snapTypeToMountDir[typ]
			snapPath := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), sn.Filename())
			fmt.Fprintf(stdout, "%s %s\n", snapPath, filepath.Join(boot.InitramfsRunMntDir, dir))
		}
	}

	// 4. check if snapd is mounted (only on first-boot will we mount it)
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

	return nil
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

func unlockIfEncrypted(name string) (string, error) {
	// TODO:UC20: will need to unseal key to unlock LUKS here
	device := filepath.Join("/dev/disk/by-label", name)
	keyfile := filepath.Join(boot.InitramfsUbuntuBootDir, name+".keyfile.unsealed")
	if osutil.FileExists(keyfile) {
		// TODO:UC20: snap-bootstrap should validate that <name>-enc is what
		//            we expect (and not e.g. an external disk), and also that
		//            <name> is from <name>-enc and not an unencrypted partition
		//            with the same name (LP #1863886)
		cmd := exec.Command("/usr/lib/systemd/systemd-cryptsetup", "attach", name, device+"-enc", keyfile)
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "SYSTEMD_LOG_TARGET=console")
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", osutil.OutputErr(output, err)
		}
	}
	return device, nil
}
