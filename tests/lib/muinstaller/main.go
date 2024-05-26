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
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/mkfs"
	"github.com/snapcore/snapd/secboot"
)

func waitForDevice() string {
	for {
		devices := mylog.Check2(emptyFixedBlockDevices())

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
	removable := mylog.Check2(filepath.Glob(filepath.Join(dirs.GlobalRootDir, "/sys/block/*/removable")))

devicesLoop:
	for _, removableAttr := range removable {
		val := mylog.Check2(os.ReadFile(removableAttr))
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
		output, stderr := mylog.Check3(osutil.RunSplitOutput("lsblk", "--output", "fstype", "--noheadings", devNode))

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
	output, stderr := mylog.Check3(osutil.RunSplitOutput("blkid", "--probe", "--match-types", "gpt", bootDevice))
	exitCode := mylog.Check2(osutil.ExitCode(err))

	switch exitCode {
	case 0:
		// partition table already exists, nothing to do
	case 2:
		// no match found, create partition table
		cmd := exec.Command("sfdisk", bootDevice)
		cmd.Stdin = bytes.NewBufferString("label: gpt\n")
		output, stderr := mylog.Check3(osutil.RunCmd(cmd))

		// ensure udev is aware of the new attributes
		output, stderr := mylog.Check3(osutil.RunSplitOutput("udevadm", "settle"))

	default:
		// unknown error
		return fmt.Errorf("unexpected exit code from blkid: %v", osutil.OutputErrCombine(output, stderr, err))
	}

	return nil
}

func createPartitions(bootDevice string, volumes map[string]*gadget.Volume, encType secboot.EncryptionType) ([]*gadget.OnDiskAndGadgetStructurePair, error) {
	vol := firstVol(volumes)
	mylog.Check(
		// snapd does not create partition tables so we have to do it here
		// or gadget.OnDiskVolumeFromDevice() will fail
		maybeCreatePartitionTable(bootDevice, vol.Schema))

	diskLayout := mylog.Check2(gadget.OnDiskVolumeFromDevice(bootDevice))

	if len(diskLayout.Structure) > 0 && !vol.HasPartial(gadget.PartialStructure) {
		return nil, fmt.Errorf("cannot yet install on a disk that has partitions")
	}

	opts := &install.CreateOptions{CreateAllMissingPartitions: true}
	// Fill index, as it is not passed around to muinstaller
	for i := range vol.Structure {
		vol.Structure[i].YamlIndex = i
	}
	created := mylog.Check2(install.CreateMissingPartitions(diskLayout, vol, opts))

	logger.Noticef("created %d partitions", len(created))

	return created, nil
}

func runMntFor(label string) string {
	return filepath.Join(dirs.GlobalRootDir, "/run/muinstaller-mnt/", label)
}

func postSystemsInstallSetupStorageEncryption(cli *client.Client,
	details *client.SystemDetails, bootDevice string,
	dgpairs []*gadget.OnDiskAndGadgetStructurePair,
) (map[string]string, error) {
	// We are modifiying the details struct here
	for _, gadgetVol := range details.Volumes {
		for i := range gadgetVol.Structure {
			switch gadgetVol.Structure[i].Role {
			case "system-save", "system-data":
				// only roles for which we will want encryption
			default:
				continue
			}
			gadgetVol.Structure[i].Device = nodeForPartLabel(dgpairs, gadgetVol.Structure[i].Name)
		}
	}

	// Storage encryption makes specified partitions encrypted
	opts := &client.InstallSystemOptions{
		Step:      client.InstallStepSetupStorageEncryption,
		OnVolumes: details.Volumes,
	}
	chgId := mylog.Check2(cli.InstallSystem(details.Label, opts))

	fmt.Printf("Change %s created\n", chgId)
	mylog.Check(waitChange(chgId))

	chg := mylog.Check2(cli.Change(chgId))

	encryptedDevices := make(map[string]string)
	mylog.Check(chg.Get("encrypted-devices", &encryptedDevices))

	return encryptedDevices, nil
}

// XXX: reuse/extract cmd/snap/wait.go:waitMixin()
func waitChange(chgId string) error {
	cli := client.New(nil)
	for {
		chg := mylog.Check2(cli.Change(chgId))

		if chg.Err != "" {
			return errors.New(chg.Err)
		}
		if chg.Ready {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
}

// nodeForPartLabel returns the node where a gadget structure is expected to be.
func nodeForPartLabel(dgpairs []*gadget.OnDiskAndGadgetStructurePair, name string) string {
	for _, pair := range dgpairs {
		// Same partition label
		if pair.GadgetStructure.Name == name {
			return pair.DiskStructure.Node
		}
	}
	return ""
}

// TODO laidoutStructs is used to get the devices, when encryption is
// happening maybe we need to find the information differently.
func postSystemsInstallFinish(cli *client.Client,
	details *client.SystemDetails, bootDevice string,
	dgpairs []*gadget.OnDiskAndGadgetStructurePair,
) error {
	vols := make(map[string]*gadget.Volume)
	for volName, gadgetVol := range details.Volumes {
		for i := range gadgetVol.Structure {
			// TODO mbr is special, what is the device for that?
			if gadgetVol.Structure[i].Role == "mbr" {
				gadgetVol.Structure[i].Device = bootDevice
				continue
			}
			gadgetVol.Structure[i].Device = nodeForPartLabel(dgpairs, gadgetVol.Structure[i].Name)
			logger.Debugf("partition to install: %q", gadgetVol.Structure[i].Device)
		}
		vols[volName] = gadgetVol
	}

	// Finish steps does the writing of assets
	opts := &client.InstallSystemOptions{
		Step:      client.InstallStepFinish,
		OnVolumes: vols,
	}
	chgId := mylog.Check2(cli.InstallSystem(details.Label, opts))

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

	disk := mylog.Check2(disks.DiskFromDeviceName(bootDevice))

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
			part := mylog.Check2(disk.FindMatchingPartitionWithPartLabel(volStruct.Name))

			partNode = part.KernelDeviceNode
		}

		logger.Debugf("making filesystem in %q", partNode)
		mylog.Check(mkfs.Make(volStruct.Filesystem, partNode, volStruct.Label, 0, 0))

		// Mount filesystem
		// XXX: reuse gadget/install/content.go:mountFilesystem()
		// instead (it will also call udevadm)
		mountPoint := runMntFor(volStruct.Label)
		mylog.Check(os.MkdirAll(mountPoint, 0755))

		// XXX: is there a better way?
		output, stderr := mylog.Check3(osutil.RunSplitOutput("mount", partNode, mountPoint))

		mountPoints = append(mountPoints, mountPoint)
	}

	return mountPoints, nil
}

func unmountFilesystems(mntPts []string) (err error) {
	for _, mntPt := range mntPts {
		// We try to unmount all mount points, and return the
		// last error if any.
		output, stderr := mylog.Check3(osutil.RunSplitOutput("umount", mntPt))
	}
	return err
}

func createClassicRootfsIfNeeded(rootfsCreator string) error {
	dst := runMntFor("ubuntu-data")

	output, stderr := mylog.Check3(osutil.RunSplitOutput(rootfsCreator, dst))

	return nil
}

func copySeedDir(src, dst string) error {
	mylog.Check(os.MkdirAll(filepath.Dir(dst), 0755))

	// Note that we do not use the -a option as cp returns an error if trying to
	// preserve attributes in a fat filesystem. And this is fine for files from
	// the seed, that do not need anything too special in that regard.
	output, stderr := mylog.Check3(osutil.RunSplitOutput("cp", "-r", src, dst))

	return nil
}

func copySeedToDataPartition() error {
	src := dirs.SnapSeedDir
	dataMnt := runMntFor("ubuntu-data")
	dst := dirs.SnapSeedDirUnder(dataMnt)
	mylog.Check(
		// Remove any existing seed on the target fs and then put the
		// selected seed in place on the target
		os.RemoveAll(dst))

	return copySeedDir(src, dst)
}

func detectStorageEncryption(seedLabel string) (bool, error) {
	cli := client.New(nil)
	details := mylog.Check2(cli.SystemDetails(seedLabel))

	logger.Noticef("detect encryption: %+v", details.StorageEncryption)
	if details.StorageEncryption.Support == client.StorageEncryptionSupportDefective {
		return false, errors.New(details.StorageEncryption.UnavailableReason)
	}
	return details.StorageEncryption.Support == client.StorageEncryptionSupportAvailable, nil
}

// fillPartiallyDefinedVolume fills partial gadget information by
// looking at the provided disk. Schema, filesystems, and sizes are
// filled. If partial structure is set, to remove it we would need to
// add to the volume the existing partitions present on the disk but
// not in the gadget. But as snapd is fine with these partitions as
// far as partial strucuture is defined, we just do nothing.
func fillPartiallyDefinedVolume(vol *gadget.Volume, bootDevice string) error {
	if len(vol.Partial) == 0 {
		return nil
	}

	logger.Noticef("partial gadget for: %q", vol.Partial)

	if vol.HasPartial(gadget.PartialSchema) && vol.Schema == "" {
		vol.Schema = "gpt"
		logger.Debugf("volume %q schema set to %q", vol.Name, vol.Schema)
	}

	if vol.HasPartial(gadget.PartialFilesystem) {
		for sidx := range vol.Structure {
			s := &vol.Structure[sidx]
			if s.HasFilesystem() && s.Filesystem == "" {
				switch s.Role {
				case gadget.SystemSeed, gadget.SystemSeedNull:
					s.Filesystem = "vfat"
				default:
					s.Filesystem = "ext4"
				}
				logger.Debugf("%q filesystem set to %s", s.Name, s.Filesystem)
			}
		}
	}

	// Fill sizes: for the moment, to avoid complicating unnecessarily the
	// code, we do size=min-size except for the last partition.
	output, stderr := mylog.Check3(osutil.RunSplitOutput("lsblk", "--bytes", "--noheadings", "--output", "SIZE", bootDevice))
	exitCode := mylog.Check2(osutil.ExitCode(err))

	if exitCode != 0 {
		return fmt.Errorf("cannot find size of %q: %q (stderr: %s)", bootDevice, string(output), string(stderr))
	}
	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 {
		return fmt.Errorf("error splitting %q (stderr: %s)", string(output), string(stderr))
	}
	diskSize := mylog.Check2(strconv.Atoi(lines[0]))

	partStart := quantity.Offset(0)
	if vol.HasPartial(gadget.PartialSize) {
		lastIdx := len(vol.Structure) - 1
		for sidx := range vol.Structure {
			s := &vol.Structure[sidx]
			if s.Offset != nil {
				partStart = *s.Offset
			}
			if s.Size == 0 {
				if sidx == lastIdx {
					// Last partition, give it all remaining space
					// (except space for secondary GPT header).
					s.Size = quantity.Size(diskSize) - quantity.Size(partStart) - 6*4096
				} else {
					s.Size = s.MinSize
				}
				logger.Debugf("size of %q set to %d", s.Name, s.Size)
			}
			if s.Offset == nil {
				offset := partStart
				s.Offset = &offset
				logger.Debugf("offset of %q set to %d", s.Name, *s.Offset)
			}
			partStart += quantity.Offset(s.Size)
		}
	}

	return nil
}

func run(seedLabel, bootDevice, rootfsCreator string) error {
	isCore := rootfsCreator == ""
	logger.Noticef("installing on %q", bootDevice)

	cli := client.New(nil)
	details := mylog.Check2(cli.SystemDetails(seedLabel))

	shouldEncrypt := mylog.Check2(detectStorageEncryption(seedLabel))

	// TODO: support multiple volumes, see gadget/install/install.go
	if len(details.Volumes) != 1 {
		return fmt.Errorf("gadget defines %v volumes, while we support only one at the moment", len(details.Volumes))
	}
	mylog.Check(

		// If partial gadget, fill missing information based on the installation target
		fillPartiallyDefinedVolume(firstVol(details.Volumes), bootDevice))

	// TODO: grow the data-partition based on disk size
	encType := secboot.EncryptionTypeNone
	if shouldEncrypt {
		encType = secboot.EncryptionTypeLUKS
	}
	dgpairs := mylog.Check2(createPartitions(bootDevice, details.Volumes, encType))

	encryptedDevices := make(map[string]string)
	if shouldEncrypt {
		encryptedDevices = mylog.Check2(postSystemsInstallSetupStorageEncryption(cli, details, bootDevice, dgpairs))
	}
	mntPts := mylog.Check2(createAndMountFilesystems(bootDevice, details.Volumes, encryptedDevices))

	if !isCore {
		mylog.Check(createClassicRootfsIfNeeded(rootfsCreator))
	}
	mylog.Check(copySeedToDataPartition())
	mylog.Check(unmountFilesystems(mntPts))
	mylog.Check(postSystemsInstallFinish(cli, details, bootDevice, dgpairs))

	// TODO: reboot here automatically (optional)

	return nil
}

func main() {
	if len(os.Args) < 3 || len(os.Args) > 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <seed-label> <target-device> [rootfs-creator]\n"+
			"If [rootfs-creator] is specified, classic Ubuntu with core boot will be installed.\n"+
			"Otherwise, Ubuntu Core will be installed\n", os.Args[0])
		os.Exit(1)
	}
	logger.SimpleSetup()

	seedLabel := os.Args[1]
	bootDevice := os.Args[2]
	rootfsCreator := ""
	if len(os.Args) > 3 {
		rootfsCreator = os.Args[3]
	}
	if bootDevice == "auto" {
		bootDevice = waitForDevice()
	}
	mylog.Check(run(seedLabel, bootDevice, rootfsCreator))

	msg := "install done, please remove installation media and reboot"
	fmt.Println(msg)
	exec.Command("wall", msg).Run()
}
