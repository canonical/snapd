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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
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

func createPartitions(bootDevice string, volumes map[string]*gadget.Volume) ([]gadget.OnDiskStructure, error) {
	// TODO: support multiple volumes, see gadget/install/install.go
	if len(volumes) != 1 {
		return nil, fmt.Errorf("got unexpected number of volumes %v", len(volumes))
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(bootDevice)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v partitions: %v", bootDevice, err)
	}
	if len(diskLayout.Structure) > 0 {
		return nil, fmt.Errorf("cannot yet install on a disk that has partitions")
	}

	layoutOpts := &gadget.LayoutOptions{
		IgnoreContent: true,
	}

	vol := firstVol(volumes)
	lvol, err := gadget.LayoutVolume(vol, gadget.DefaultConstraints, layoutOpts)
	if err != nil {
		return nil, fmt.Errorf("cannot layout volume: %v", err)
	}

	iconst := &install.CreateOptions{CreateAllMissingPartitions: true}
	created, err := install.CreateMissingPartitions(diskLayout, lvol, iconst)
	if err != nil {
		return nil, fmt.Errorf("cannot create parititons: %v", err)
	}
	logger.Noticef("created %v partitions", created)

	return created, nil
}

func runMntFor(label string) string {
	return filepath.Join(dirs.GlobalRootDir, "/run/installer-mnt/", label)
}

func postSystemsInstallSetupStorageEncryption(details *client.SystemDetails) error {
	// TODO: check details.StorageEncryption and call POST
	// /systems/<seed-label> with "action":"install" and
	// "step":"setup-storage-encryption"
	return nil
}

// XXX: reuse/extract cmd/snap/wait.go:waitMixin()
func waitChange(chgId string) error {
	cli := client.New(nil)
	for {
		chg, err := cli.Change(chgId)
		if err != nil {
			return err
		}

		if chg.Err != "" {
			return errors.New(chg.Err)
		}
		if chg.Ready {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
}

// TODO laidoutStructs is used to get the devices, when encryption is
// happening maybe we need to find the information differently.
func postSystemsInstallFinish(cli *client.Client,
	details *client.SystemDetails, bootDevice string,
	laidoutStructs []gadget.OnDiskStructure) error {

	vols := make(map[string]*gadget.Volume)
	for volName, gadgetVol := range details.Volumes {
		laidIdx := 0
		for i := range gadgetVol.Structure {
			// TODO mbr is special, what is the device for that?
			var device string
			if gadgetVol.Structure[i].Role == "mbr" {
				device = bootDevice
			} else {
				device = laidoutStructs[laidIdx].Node
				laidIdx++
			}
			gadgetVol.Structure[i].Device = device
		}
		vols[volName] = gadgetVol
	}

	// Finish steps does the writing of assets
	opts := &client.InstallSystemOptions{
		Step:      client.InstallStepFinish,
		OnVolumes: vols,
	}
	chgId, err := cli.InstallSystem(details.Label, opts)
	if err != nil {
		return err
	}
	fmt.Printf("Change %s created\n", chgId)
	return waitChange(chgId)
}

// createAndMountFilesystems creates and mounts filesystems. It returns
// an slice with the paths where the filesystems have been mounted to.
func createAndMountFilesystems(bootDevice string, volumes map[string]*gadget.Volume) ([]string, error) {
	// only support a single volume for now
	if len(volumes) != 1 {
		return nil, fmt.Errorf("got unexpected number of volumes %v", len(volumes))
	}

	disk, err := disks.DiskFromDeviceName(bootDevice)
	if err != nil {
		return nil, err
	}
	vol := firstVol(volumes)

	var mountPoints []string
	for _, stru := range vol.Structure {
		if stru.Label == "" || stru.Filesystem == "" {
			continue
		}

		part, err := disk.FindMatchingPartitionWithPartLabel(stru.Label)
		if err != nil {
			return nil, err
		}
		// XXX: reuse
		// gadget/install/content.go:mountFilesystem() instead
		// (it will also call udevadm)
		if err := mkfs.Make(stru.Filesystem, part.KernelDeviceNode, stru.Label, 0, 0); err != nil {
			return nil, err
		}

		// mount
		mountPoint := runMntFor(stru.Label)
		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			return nil, err
		}
		// XXX: is there a better way?
		if output, err := exec.Command("mount", part.KernelDeviceNode, mountPoint).CombinedOutput(); err != nil {
			return nil, osutil.OutputErr(output, err)
		}
		mountPoints = append(mountPoints, mountPoint)
	}

	return mountPoints, nil
}

func unmountFilesystems(mntPts []string) error {
	for _, mntPt := range mntPts {
		if output, err := exec.Command("umount", mntPt).CombinedOutput(); err != nil {
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
	laidoutStructs, err := createPartitions(bootDevice, details.Volumes)
	if err != nil {
		return fmt.Errorf("cannot setup partitions: %v", err)
	}
	if err := postSystemsInstallSetupStorageEncryption(details); err != nil {
		return fmt.Errorf("cannot setup storage encryption: %v", err)
	}
	mntPts, err := createAndMountFilesystems(bootDevice, details.Volumes)
	if err != nil {
		return fmt.Errorf("cannot create filesystems: %v", err)
	}
	if err := createClassicRootfsIfNeeded(rootfsCreator); err != nil {
		return fmt.Errorf("cannot create classic rootfs: %v", err)
	}
	if err := createSeedOnTarget(bootDevice, seedLabel); err != nil {
		return fmt.Errorf("cannot create seed on target: %v", err)
	}
	// Unmount filesystems
	if err := unmountFilesystems(mntPts); err != nil {
		return fmt.Errorf("cannot unmount filesystems: %v", err)
	}
	if err := postSystemsInstallFinish(cli, details, bootDevice, laidoutStructs); err != nil {
		return fmt.Errorf("cannot finalize install: %v", err)
	}
	// TODO: reboot here automatically (optional)

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
	logger.Noticef("install done, please reboot")
}
