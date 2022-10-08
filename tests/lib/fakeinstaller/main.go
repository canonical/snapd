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
	"github.com/snapcore/snapd/overlord/state"
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

	opts := &install.CreateOptions{CreateAllMissingPartitions: true}
	created, err := install.CreateMissingPartitions(diskLayout, lvol, opts)
	if err != nil {
		return nil, fmt.Errorf("cannot create parititons: %v", err)
	}
	logger.Noticef("created %v partitions", created)

	return created, nil
}

func runMntFor(label string) string {
	return filepath.Join(dirs.GlobalRootDir, "/run/fakeinstaller-mnt/", label)
}

func postSystemsInstallSetupStorageEncryption(cli *client.Client,
	details *client.SystemDetails, bootDevice string,
	onDiskParts []gadget.OnDiskStructure) (map[string]string, error) {

	// We are modifiying the details struct here
	for _, gadgetVol := range details.Volumes {
		for i := range gadgetVol.Structure {
			switch gadgetVol.Structure[i].Role {
			case "system-save", "system-data":
				// only roles for which we will want encryption
			default:
				continue
			}
			for _, part := range onDiskParts {
				if part.Name == gadgetVol.Structure[i].Name {
					gadgetVol.Structure[i].Device = part.Node
					break
				}
			}
		}
	}

	// Storage encryption makes specified partitions encrypted
	opts := &client.InstallSystemOptions{
		Step:      client.InstallStepSetupStorageEncryption,
		OnVolumes: details.Volumes,
	}
	chgId, err := cli.InstallSystem(details.Label, opts)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Change %s created\n", chgId)
	if err := waitChange(chgId); err != nil {
		return nil, err
	}

	chg, err := cli.Change(chgId)
	if err != nil {
		return nil, err
	}

	var encryptedDevices = make(map[string]string)
	if err := chg.Get("encrypted-devices", &encryptedDevices); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, fmt.Errorf("cannot get encrypted-devices from change: %v", err)
	}

	return encryptedDevices, nil
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
	onDiskParts []gadget.OnDiskStructure) error {

	vols := make(map[string]*gadget.Volume)
	for volName, gadgetVol := range details.Volumes {
		for i := range gadgetVol.Structure {
			// TODO mbr is special, what is the device for that?
			if gadgetVol.Structure[i].Role == "mbr" {
				gadgetVol.Structure[i].Device = bootDevice
				continue
			}
			for _, part := range onDiskParts {
				// Same partition label
				if part.Name == gadgetVol.Structure[i].Name {
					node := part.Node
					logger.Debugf("partition to install: %q", node)
					gadgetVol.Structure[i].Device = node
					break
				}
			}
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
func createAndMountFilesystems(bootDevice string, volumes map[string]*gadget.Volume, encryptedDevices map[string]string) ([]string, error) {
	// only support a single volume for now
	if len(volumes) != 1 {
		return nil, fmt.Errorf("got unexpected number of volumes %v", len(volumes))
	}
	// XXX: make this more elegant
	shouldEncrypt := len(encryptedDevices) > 0

	disk, err := disks.DiskFromDeviceName(bootDevice)
	if err != nil {
		return nil, err
	}
	vol := firstVol(volumes)

	var mountPoints []string
	for _, volStruct := range vol.Structure {
		if volStruct.Label == "" || volStruct.Filesystem == "" {
			continue
		}

		var partNode string
		if shouldEncrypt && (volStruct.Role == gadget.SystemSave || volStruct.Role == gadget.SystemData) {
			encryptedDevice := encryptedDevices[volStruct.Role]
			if encryptedDevice == "" {
				return nil, fmt.Errorf("no encrypted device found for %s role", volStruct.Role)
			}
			partNode = encryptedDevice
		} else {
			part, err := disk.FindMatchingPartitionWithPartLabel(volStruct.Label)
			if err != nil {
				return nil, err
			}
			partNode = part.KernelDeviceNode
		}

		logger.Debugf("making filesystem in %q", partNode)
		if err := mkfs.Make(volStruct.Filesystem, partNode, volStruct.Label, 0, 0); err != nil {
			return nil, err
		}

		// Mount filesystem
		// XXX: reuse gadget/install/content.go:mountFilesystem()
		// instead (it will also call udevadm)
		mountPoint := runMntFor(volStruct.Label)
		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			return nil, err
		}
		// XXX: is there a better way?
		if output, err := exec.Command("mount", partNode, mountPoint).CombinedOutput(); err != nil {
			return nil, osutil.OutputErr(output, err)
		}
		mountPoints = append(mountPoints, mountPoint)
	}

	return mountPoints, nil
}

func unmountFilesystems(mntPts []string) (err error) {
	for _, mntPt := range mntPts {
		// We try to unmount all mount points, and return the
		// last error if any.
		if output, errUmnt := exec.Command("umount", mntPt).CombinedOutput(); err != nil {
			errUmnt = osutil.OutputErr(output, errUmnt)
			logger.Noticef("error: cannot unmount %q: %v", mntPt, errUmnt)
			err = errUmnt
		}
	}
	return err
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

func detectStorageEncryption(seedLabel string) (bool, error) {
	cli := client.New(nil)
	details, err := cli.SystemDetails(seedLabel)
	if err != nil {
		return false, err
	}
	logger.Noticef("detect encryption: %v", details)
	if details.StorageEncryption.Support == client.StorageEncryptionSupportDefective {
		return false, errors.New(details.StorageEncryption.UnavailableReason)
	}
	return details.StorageEncryption.Support == client.StorageEncryptionSupportAvailable, nil
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
	shouldEncrypt, err := detectStorageEncryption(seedLabel)
	if err != nil {
		return err
	}
	var encryptedDevices = make(map[string]string)
	if shouldEncrypt {
		encryptedDevices, err = postSystemsInstallSetupStorageEncryption(cli, details, bootDevice, laidoutStructs)
		if err != nil {
			return fmt.Errorf("cannot setup storage encryption: %v", err)
		}
	}
	mntPts, err := createAndMountFilesystems(bootDevice, details.Volumes, encryptedDevices)
	if err != nil {
		return fmt.Errorf("cannot create filesystems: %v", err)
	}
	if err := createClassicRootfsIfNeeded(rootfsCreator); err != nil {
		return fmt.Errorf("cannot create classic rootfs: %v", err)
	}
	if err := createSeedOnTarget(bootDevice, seedLabel); err != nil {
		return fmt.Errorf("cannot create seed on target: %v", err)
	}
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
