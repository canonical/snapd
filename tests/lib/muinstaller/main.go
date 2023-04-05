// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

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
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/mkfs"
	"github.com/snapcore/snapd/secboot"
)

func waitForDevice() string {
	for {
		devices, err := emptyFixedBlockDevices()
		if err != nil {
			logger.Noticef("cannot list devices: %v", err)
		}
		switch len(devices) {
		case 0:
			logger.Noticef("cannot use automatic mode, no empty disk found")
		case 1:
			// found exactly one target
			return devices[0]
		default:
			logger.Noticef("cannot use automatic mode, multiple empty disks found: %v", devices)
		}
		time.Sleep(5 * time.Second)
	}
}

// emptyFixedBlockDevices finds any non-removable physical disk that has
// no partitions. It will exclude loop devices.
func emptyFixedBlockDevices() (devices []string, err error) {
	// eg. /sys/block/sda/removable
	removable, err := filepath.Glob(filepath.Join(dirs.GlobalRootDir, "/sys/block/*/removable"))
	if err != nil {
		return nil, err
	}
devicesLoop:
	for _, removableAttr := range removable {
		val, err := ioutil.ReadFile(removableAttr)
		if err != nil || string(val) != "0\n" {
			// removable, ignore
			continue
		}
		dev := filepath.Base(filepath.Dir(removableAttr))
		if strings.HasPrefix(dev, "loop") {
			// is loop device, ignore
			continue
		}
		// let's see if it has partitions
		pattern := fmt.Sprintf(filepath.Join(dirs.GlobalRootDir, "/sys/block/%s/%s*/partition"), dev, dev)
		// eg. /sys/block/sda/sda1/partition
		partitionAttrs, _ := filepath.Glob(pattern)
		if len(partitionAttrs) != 0 {
			// has partitions, ignore
			continue
		}
		// check that there was no previous filesystem
		devNode := fmt.Sprintf("/dev/%s", dev)
		output, err := exec.Command("lsblk", "--output", "fstype", "--noheadings", devNode).CombinedOutput()
		if err != nil {
			return nil, osutil.OutputErr(output, err)
		}
		if strings.TrimSpace(string(output)) != "" {
			// found a filesystem, ignore
			continue devicesLoop
		}

		devices = append(devices, devNode)
	}
	sort.Strings(devices)
	return devices, nil
}

func firstVol(volumes map[string]*gadget.Volume) *gadget.Volume {
	for _, vol := range volumes {
		return vol
	}
	return nil
}

func maybeCreatePartitionTable(bootDevice, schema string) error {
	switch schema {
	case "dos":
		return fmt.Errorf("cannot use partition schema %v yet", schema)
	case "gpt":
		// ok
	default:
		return fmt.Errorf("cannot use unknown partition schema %v", schema)
	}

	// check if there is a GPT partition table already
	output, err := exec.Command("blkid", "--probe", "--match-types", "gpt", bootDevice).CombinedOutput()
	exitCode, err := osutil.ExitCode(err)
	if err != nil {
		return err
	}
	switch exitCode {
	case 0:
		// partition table already exists, nothing to do
	case 2:
		// no match found, create partition table
		cmd := exec.Command("sfdisk", bootDevice)
		cmd.Stdin = bytes.NewBufferString("label: gpt\n")
		if output, err := cmd.CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}
		// ensure udev is aware of the new attributes
		if output, err := exec.Command("udevadm", "settle").CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}
	default:
		// unknown error
		return fmt.Errorf("unexpected exit code from blkid: %v", osutil.OutputErr(output, err))
	}

	return nil
}

func createPartitions(bootDevice string, volumes map[string]*gadget.Volume, encType secboot.EncryptionType) ([]gadget.OnDiskStructure, error) {
	// TODO: support multiple volumes, see gadget/install/install.go
	if len(volumes) != 1 {
		return nil, fmt.Errorf("got unexpected number of volumes %v", len(volumes))
	}

	vol := firstVol(volumes)
	// snapd does not create partition tables so we have to do it here
	// or gadget.OnDiskVolumeFromDevice() will fail
	if err := maybeCreatePartitionTable(bootDevice, vol.Schema); err != nil {
		return nil, err
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
		EncType:       encType,
	}

	lvol, err := gadget.LayoutVolume(vol, layoutOpts)
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
	return filepath.Join(dirs.GlobalRootDir, "/run/muinstaller-mnt/", label)
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
	if err := chg.Get("encrypted-devices", &encryptedDevices); err != nil {
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
		if volStruct.Filesystem == "" {
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
			part, err := disk.FindMatchingPartitionWithPartLabel(volStruct.Name)
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
	// Remove any existing seed on the target fs and then put the
	// selected seed in place on the target
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
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
	logger.Noticef("detect encryption: %+v", details.StorageEncryption)
	if details.StorageEncryption.Support == client.StorageEncryptionSupportDefective {
		return false, errors.New(details.StorageEncryption.UnavailableReason)
	}
	return details.StorageEncryption.Support == client.StorageEncryptionSupportAvailable, nil
}

func run(seedLabel, rootfsCreator, bootDevice string) error {
	logger.Noticef("installing on %q", bootDevice)

	cli := client.New(nil)
	details, err := cli.SystemDetails(seedLabel)
	if err != nil {
		return err
	}
	shouldEncrypt, err := detectStorageEncryption(seedLabel)
	if err != nil {
		return err
	}
	// TODO: grow the data-partition based on disk size
	encType := secboot.EncryptionTypeNone
	if shouldEncrypt {
		encType = secboot.EncryptionTypeLUKS
	}
	laidoutStructs, err := createPartitions(bootDevice, details.Volumes, encType)
	if err != nil {
		return fmt.Errorf("cannot setup partitions: %v", err)
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
	if len(os.Args) != 4 {
		// XXX: allow installing real UC without a classic-rootfs later
		fmt.Fprintf(os.Stderr, "need seed-label, target-device and classic-rootfs as argument\n")
		os.Exit(1)
	}
	logger.SimpleSetup()

	seedLabel := os.Args[1]
	rootfsCreator := os.Args[2]
	bootDevice := os.Args[3]
	if bootDevice == "auto" {
		bootDevice = waitForDevice()
	}

	if err := run(seedLabel, rootfsCreator, bootDevice); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	msg := "install done, please remove installation media and reboot"
	fmt.Println(msg)
	exec.Command("wall", msg).Run()
}
