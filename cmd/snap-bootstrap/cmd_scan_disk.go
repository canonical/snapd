// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022-2023 Canonical Ltd
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

/*
 * This tool expects to be called for a udev rules file such as:
 *
 * ```
 * SUBSYSTEM!="block", GOTO="ubuntu_core_partitions_end"
 *
 * ENV{DEVTYPE}=="partition", IMPORT{parent}="UBUNTU_*_UUID"
 * IMPORT{program}="/usr/lib/snapd/snap-bootstrap scan-disk"
 * ENV{DEVTYPE}=="disk", ENV{UBUNTU_DISK}=="1", SYMLINK+="disk/ubuntu/disk"
 * ENV{DEVTYPE}=="partition", ENV{UBUNTU_SEED}=="1", SYMLINK+="disk/ubuntu/seed"
 * ENV{DEVTYPE}=="partition", ENV{UBUNTU_BOOT}=="1", SYMLINK+="disk/ubuntu/boot"
 * ENV{DEVTYPE}=="partition", ENV{UBUNTU_DATA}=="1", ENV{ID_FS_TYPE}=="crypto_LUKS", SYMLINK+="disk/ubuntu/data-luks"
 * ENV{DEVTYPE}=="partition", ENV{UBUNTU_DATA}=="1", ENV{ID_FS_TYPE}!="crypto_LUKS", SYMLINK+="disk/ubuntu/data"
 * ENV{DEVTYPE}=="partition", ENV{UBUNTU_SAVE}=="1", ENV{ID_FS_TYPE}=="crypto_LUKS", SYMLINK+="disk/ubuntu/save-luks"
 * ENV{DEVTYPE}=="partition", ENV{UBUNTU_SAVE}=="1", ENV{ID_FS_TYPE}!="crypto_LUKS", SYMLINK+="disk/ubuntu/save"
 * ENV{DM_UUID}=="CRYPT-*", ENV{DM_NAME}=="ubuntu-data-*", SYMLINK+="disk/ubuntu/data"
 * ENV{DM_UUID}=="CRYPT-*", ENV{DM_NAME}=="ubuntu-save-*", SYMLINK+="disk/ubuntu/save"
 *
 * LABEL="ubuntu_core_partitions_end"
 * ```
 *
 * See
 * core-initrd/latest/factory/usr/lib/udev/rules.d/90-ubuntu-core-partitions.rules
 * for implementation.
 *
 * Note that symlink /dev/disk/ubuntu/disk can be expected by
 * snap-bootstrap. In that case, snap-initramfs-mounts.service should
 * have:
 *
 * ```
 * BindsTo=dev-disk-ubuntu-disk.device
 * After=dev-disk-ubuntu-disk.device
 * ```
 *
 */

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/cmd/snap-bootstrap/blkid"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/kcmdline"
)

var (
	efiReadVarString = efi.ReadVarString
)

func init() {
	const (
		short = "Verify that a disk is the booting disk"
		long  = "This tool is expected to be called from udev"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("scan-disk", short, long, &cmdScanDisk{}); err != nil {
			panic(err)
		}
	})
}

type cmdScanDisk struct{}

func (c *cmdScanDisk) Execute([]string) error {
	return ScanDisk(os.Stdout)
}

type Partition struct {
	Name string
	UUID string
}

func isGpt(probe blkid.AbstractBlkidProbe) bool {
	pttype, err := probe.LookupValue("PTTYPE")
	if err != nil {
		return false
	}
	return pttype == "gpt"
}

func probePartitions(node string) ([]Partition, error) {
	probe, err := blkid.NewProbeFromFilename(node)
	if err != nil {
		return nil, err
	}
	defer probe.Close()

	probe.EnablePartitions(true)
	probe.SetPartitionsFlags(blkid.BLKID_PARTS_ENTRY_DETAILS)
	probe.EnableSuperblocks(true)

	if err := probe.DoSafeprobe(); err != nil {
		return nil, err
	}

	if !isGpt(probe) {
		return nil, nil
	}

	partitions, err := probe.GetPartitions()
	if partitions == nil {
		return nil, err
	}

	ret := make([]Partition, 0)
	for _, partition := range partitions.GetPartitions() {
		label := partition.GetName()
		uuid := partition.GetUUID()
		fmt.Fprintf(os.Stderr, "Found partition %s %s\n", label, uuid)
		ret = append(ret, Partition{label, uuid})
	}

	return ret, nil
}

func samePath(a, b string) (bool, error) {
	aSt, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bSt, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(aSt, bSt), nil
}

func scanDiskNode(output io.Writer, node string) error {
	/*
	 * We need to find out if the give contains the ESP that was booted.
	 * The boot loader will set LoaderDevicePartUUID. We will need to scan all
	 * the partitions for that UUID.
	 */
	fmt.Fprintf(os.Stderr, "Scanning disk %s\n", node)
	fallback := false
	var fallbackPartition string
	bootUUID, _, err := efiReadVarString("LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f")
	if err != nil {
		fmt.Fprintf(os.Stderr, "No efi var, falling back: %s\n", err)
		fallback = true
	} else {
		bootUUID = strings.ToLower(bootUUID)
	}

	partitions, err := probePartitions(node)
	if err != nil {
		return fmt.Errorf("Cannot get partitions: %s\n", err)
	}

	/*
	 * If LoaderDevicePartUUID was not set, it is probably because
	 * we did not boot with UEFI. In that case we try to detect
	 * disk with partition labels.
	 */
	if fallback {
		mode, _, err := boot.ModeAndRecoverySystemFromKernelCommandLine()
		if err != nil {
			return err
		}
		switch mode {
		case "recover":
			fallbackPartition = "ubuntu-seed"
		case "install":
			fallbackPartition = "ubuntu-seed"
		case "factory-reset":
			fallbackPartition = "ubuntu-seed"
		case "run":
			fallbackPartition = "ubuntu-boot"
		case "cloudimg-rootfs":
			fallbackPartition = "ubuntu-boot"
		default:
			return fmt.Errorf("internal error: mode not handled")
		}
	}

	/*
	 * If we are not in UEFI mode and snapd_system_disk is
	 * defined, we need to verify the disk also matches that. If
	 * not, we just return, ignoring this disk.
	 */
	if fallback {
		values, err := kcmdline.KeyValues("snapd_system_disk")
		if err != nil {
			return fmt.Errorf("Cannot read kernel command line: %s\n", err)
		}

		if value, ok := values["snapd_system_disk"]; ok {
			var currentPath string
			var expectedPath string
			if strings.HasPrefix(value, "/dev/") || !strings.HasPrefix(value, "/") {
				name := strings.TrimPrefix(value, "/dev/")
				expectedPath = fmt.Sprintf("/dev/%s", name)
				currentPath = node
			} else {
				expectedPath = value
				currentPath = osGetenv("DEVPATH")
			}

			same, err := samePath(filepath.Join(dirs.GlobalRootDir, expectedPath),
				filepath.Join(dirs.GlobalRootDir, currentPath))
			if err != nil {
				return fmt.Errorf("Cannot check snapd_system_disk kernel parameter: %s\n", err)
			}
			if !same {
				return nil
			}
		}
	}

	/*
	 * Now we scan the partitions. For each partition we need to find out:
	 *  - If its label matches a known partition type. In this
	 *    case we save the UUID of that partition.
	 *  - If the paritition is the booted ESP. In that case, we can confirm
	 *    we are looking at the booted disk.
	 */
	found := false
	hasFallbackPartition := false
	hasSeed := false
	var seed_uuid string
	hasBoot := false
	var boot_uuid string
	hasData := false
	var data_uuid string
	hasSave := false
	var save_uuid string
	for _, part := range partitions {
		if !fallback {
			if part.UUID == bootUUID {
				/*
				 * We have just found the ESP boot partition!
				 */
				found = true
			}
		}
		if fallback && part.Name == fallbackPartition {
			/*
			 * We are not in UEFI boot, and we have found
			 * a partition that looks like the boot
			 * partition.
			 */
			hasFallbackPartition = true
		}
		if part.Name == "ubuntu-seed" {
			hasSeed = true
			seed_uuid = part.UUID
		} else if part.Name == "ubuntu-boot" {
			hasBoot = true
			boot_uuid = part.UUID
		} else if part.Name == "ubuntu-data-enc" {
			hasData = true
			data_uuid = part.UUID
		} else if part.Name == "ubuntu-data" {
			hasData = true
			data_uuid = part.UUID
		} else if part.Name == "ubuntu-save-enc" {
			hasSave = true
			save_uuid = part.UUID
		} else if part.Name == "ubuntu-save" {
			hasSave = true
			save_uuid = part.UUID
		}
	}

	/*
	 * We now print the result if either:
	 *  - We are in UEFI mode and we confirmed we found the boot ESP.
	 *  - We are not in UEFI mode and the disks look like the boot disk.
	 */
	if (!fallback && found && (hasSeed || hasBoot)) || (fallback && hasFallbackPartition) {
		fmt.Fprintf(output, "UBUNTU_DISK=1\n")
		if hasSeed {
			fmt.Fprintf(os.Stderr, "Detected partition for seed: %s\n", seed_uuid)
			fmt.Fprintf(output, "UBUNTU_SEED_UUID=%s\n", seed_uuid)
		}
		if hasBoot {
			fmt.Fprintf(os.Stderr, "Detected partition for boot: %s\n", boot_uuid)
			fmt.Fprintf(output, "UBUNTU_BOOT_UUID=%s\n", boot_uuid)
		}
		if hasData {
			fmt.Fprintf(os.Stderr, "Detected partition for data: %s\n", data_uuid)
			fmt.Fprintf(output, "UBUNTU_DATA_UUID=%s\n", data_uuid)
		}
		if hasSave {
			fmt.Fprintf(os.Stderr, "Detected partition for save: %s\n", save_uuid)
			fmt.Fprintf(output, "UBUNTU_SAVE_UUID=%s\n", save_uuid)
		}
	}

	return nil
}

func checkPartitionUUID(output io.Writer, suffix string, partUUID string) {
	varname := fmt.Sprintf("UBUNTU_%s_UUID", suffix)
	expectedUUID := osGetenv(varname)
	if len(expectedUUID) > 0 && expectedUUID == partUUID {
		fmt.Fprintf(os.Stderr, "Detected partition as %s\n", suffix)
		fmt.Fprintf(output, "UBUNTU_%s=1\n", suffix)
	}
}

func scanPartitionNode(output io.Writer, node string) error {
	/*
	 * scanDiskNode has scanned the partition table. And exported
	 * information about partitions in `UBUNTU_*_UUID`
	 * variables. No we are looking at a partition. We need to
	 * confirm which partition it is.
	 */

	fmt.Fprintf(os.Stderr, "Scanning partition %s\n", node)

	probe, err := blkid.NewProbeFromFilename(node)
	if err != nil {
		return err
	}
	defer probe.Close()

	probe.EnablePartitions(true)
	probe.SetPartitionsFlags(blkid.BLKID_PARTS_ENTRY_DETAILS)
	probe.EnableSuperblocks(true)

	if err := probe.DoSafeprobe(); err != nil {
		return fmt.Errorf("Cannot probe partition %s: %s\n", node, err)
	}

	partUUID, err := probe.LookupValue("PART_ENTRY_UUID")
	if err != nil {
		return fmt.Errorf("Cannot get uuid for partition: %s\n", err)
	}

	for _, suffix := range []string{"SEED", "BOOT", "DATA", "SAVE"} {
		checkPartitionUUID(output, suffix, partUUID)
	}

	return nil
}

func ScanDisk(output io.Writer) error {
	devname := osGetenv("DEVNAME")
	if osGetenv("DEVTYPE") == "disk" {
		return scanDiskNode(output, devname)
	} else if osGetenv("DEVTYPE") == "partition" {
		return scanPartitionNode(output, devname)
	} else {
		return fmt.Errorf("Unknown type for block device %s\n", devname)
	}
}
