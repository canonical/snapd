// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/mkfs"
)

func firstVol(volumes map[string]*gadget.Volume) *gadget.Volume {
	for _, vol := range volumes {
		return vol
	}
	return nil
}

func createPartitions(bootDevice string, volumes map[string]*gadget.Volume) error {
	if len(volumes) != 1 {
		return fmt.Errorf("got unexpected number of volumes %v", len(volumes))
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(bootDevice)
	if err != nil {
		return fmt.Errorf("cannot read %v partitions: %v", bootDevice, err)
	}
	if len(diskLayout.Structure) > 0 {
		return fmt.Errorf("cannot yet install on a disk that has partitions")
	}

	constraints := gadget.LayoutConstraints{
		// XXX: cargo-culted
		NonMBRStartOffset: 1 * quantity.OffsetMiB,
		// at this point we only care about creating partitions
		SkipResolveContent:         true,
		SkipLayoutStructureContent: true,
	}

	// FIXME: refactor gadget/install code to not take these dirs
	gadgetRoot := ""
	kernelRoot := ""

	// TODO: support multiple volumes, see gadget/install/install.go
	vol := firstVol(volumes)
	lvol, err := gadget.LayoutVolume(gadgetRoot, kernelRoot, vol, constraints)
	if err != nil {
		return fmt.Errorf("cannot layout volume: %v", err)
	}

	iconst := &install.Constraints{AllPartitions: true}
	created, err := install.CreateMissingPartitions(gadgetRoot, diskLayout, lvol, iconst)
	if err != nil {
		return fmt.Errorf("cannot create parititons: %v", err)
	}
	logger.Noticef("created %v partitions", created)

	return nil
}

func runMntFor(label string) string {
	return filepath.Join(dirs.GlobalRootDir, "/run/mnt/", label)
}

func postSystemsInstallSetupStorageEncryption(details *client.SystemDetails) error {
	// TODO: check details.StorageEncryption and call POST
	// /systems/<seed-label> with "action":"install" and
	// "step":"setup-storage-encryption"
	return nil
}

func postSystemsInstallFinish(details *client.SystemDetails) error {
	// TODO: call POST /systems/<seed-label> with
	// "action":"install" and "step":"finish"
	return nil
}

// XXX: pass in created partitions instead?
// XXX2: should we mount or will snapd mount?
func createAndMountFilesystems(bootDevice string, volumes map[string]*gadget.Volume) error {
	// only support a single volume for now
	if len(volumes) != 1 {
		return fmt.Errorf("got unexpected number of volumes %v", len(volumes))
	}

	disk, err := disks.DiskFromDeviceName(bootDevice)
	if err != nil {
		return err
	}
	vol := firstVol(volumes)

	for _, stru := range vol.Structure {
		if stru.Label == "" || stru.Filesystem == "" {
			continue
		}

		part, err := disk.FindMatchingPartitionWithPartLabel(stru.Label)
		if err != nil {
			return err
		}
		// XXX: reuse
		// gadget/install/content.go:mountFilesystem() instead
		// (it will also call udevadm)
		if err := mkfs.Make(stru.Filesystem, part.KernelDeviceNode, stru.Label, 0, 0); err != nil {
			return err
		}

		// mount
		mountPoint := runMntFor(stru.Label)
		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			return err
		}
		// XXX: is there a better way?
		if output, err := exec.Command("mount", part.KernelDeviceNode, mountPoint).CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}
	}
	return nil
}

func createClassicRootfsIfNeeded(rootfsCreator string) error {
	dst := runMntFor("ubuntu-data")

	if output, err := exec.Command(rootfsCreator, dst).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}

	return nil
}

func createSeedOnTarget(bootDevice, seedLabel string) error {
	// XXX: too naive?
	dataMnt := runMntFor("ubuntu-data")
	src := dirs.SnapSeedDir
	dst := dirs.SnapSeedDirUnder(dataMnt)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if output, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}

	return nil
}

// XXX: or will POST {"action":"install","step":"finalize"} do that?
func writeModeenvOnTarget(seedLabel string) error {
	// TODO: write modeenv
	return nil
}

func run(seedLabel, bootDevice, rootfsCreator string) error {
	if len(os.Args) != 4 {
		// xxx: allow installing real UC without a classic-rootfs later
		return fmt.Errorf("need seed-label, target-device and classic-rootfs as argument\n")
	}
	os.Setenv("SNAPD_DEBUG", "1")
	logger.SimpleSetup()

	logger.Noticef("installing on %q", bootDevice)

	cli := client.New(nil)
	details, err := cli.SystemDetails(seedLabel)
	if err != nil {
		return err
	}
	// TODO: grow the data-partition based on disk size
	if err := createPartitions(bootDevice, details.Volumes); err != nil {
		return fmt.Errorf("cannot setup partitions: %v", err)
	}
	if err := postSystemsInstallSetupStorageEncryption(details); err != nil {
		return fmt.Errorf("cannot setup storage encryption: %v", err)
	}
	if err := createAndMountFilesystems(bootDevice, details.Volumes); err != nil {
		return fmt.Errorf("cannot create filesystems: %v", err)
	}
	if err := createClassicRootfsIfNeeded(rootfsCreator); err != nil {
		return fmt.Errorf("cannot create classic rootfs: %v", err)
	}
	if err := createSeedOnTarget(bootDevice, seedLabel); err != nil {
		return fmt.Errorf("cannot create seed on target: %v", err)
	}
	// XXX: or will POST {"action":"install","step":"finalize"} do that?
	if err := writeModeenvOnTarget(seedLabel); err != nil {
		return fmt.Errorf("cannot write modeenv on target: %v", err)
	}
	// XXX: will the POST below trigger a reboot on the snapd side? if
	//      not we need to reboot here
	if err := postSystemsInstallFinish(details); err != nil {
		return fmt.Errorf("cannot finalize install: %v", err)
	}

	return nil
}

func main() {
	seedLabel := os.Args[1]
	bootDevice := os.Args[2]
	rootfsCreator := os.Args[3]

	if err := run(seedLabel, bootDevice, rootfsCreator); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
