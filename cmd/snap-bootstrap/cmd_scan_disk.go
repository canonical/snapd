// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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
 * This tool expects to be called from a udev rules file such as:
 *
 * ```
 * SUBSYSTEM!="block", GOTO="ubuntu_core_partitions_end"
 *
 * ENV{DEVTYPE}=="disk", IMPORT{program}="/usr/lib/snapd/snap-bootstrap scan-disk"
 * ENV{DEVTYPE}=="partition", IMPORT{parent}="UBUNTU_DISK"
 * ENV{UBUNTU_DISK}!="1", GOTO="ubuntu_core_partitions_end"
 *
 * ENV{DEVTYPE}=="disk", SYMLINK+="disk/snapd/disk"
 * ENV{DEVTYPE}=="partition", ENV{ID_PART_ENTRY_NAME}=="ubuntu-seed", SYMLINK+="disk/snapd/ubuntu-seed"
 * ENV{DEVTYPE}=="partition", ENV{ID_PART_ENTRY_NAME}=="ubuntu-boot", SYMLINK+="disk/snapd/ubuntu-boot"
 * ENV{DEVTYPE}=="partition", ENV{ID_PART_ENTRY_NAME}=="ubuntu-data", ENV{ID_FS_TYPE}=="crypto_LUKS", SYMLINK+="disk/snapd/ubuntu-data-luks"
 * ENV{DEVTYPE}=="partition", ENV{ID_PART_ENTRY_NAME}=="ubuntu-data", ENV{ID_FS_TYPE}!="crypto_LUKS", SYMLINK+="disk/snapd/ubuntu-data"
 * ENV{DEVTYPE}=="partition", ENV{ID_PART_ENTRY_NAME}=="ubuntu-save", ENV{ID_FS_TYPE}=="crypto_LUKS", SYMLINK+="disk/snapd/ubuntu-save-luks"
 * ENV{DEVTYPE}=="partition", ENV{ID_PART_ENTRY_NAME}=="ubuntu-save", ENV{ID_FS_TYPE}!="crypto_LUKS", SYMLINK+="disk/snapd/ubuntu-save"
 *
 * LABEL="ubuntu_core_partitions_end"
 *
 * ENV{DM_UUID}=="CRYPT-*", ENV{DM_NAME}=="ubuntu-data-*", SYMLINK+="disk/snapd/ubuntu-data"
 * ENV{DM_UUID}=="CRYPT-*", ENV{DM_NAME}=="ubuntu-save-*", SYMLINK+="disk/snapd/ubuntu-save"
 * ```
 *
 * See
 * core-initrd/latest/factory/usr/lib/udev/rules.d/90-ubuntu-core-partitions.rules
 * for implementation.
 *
 * Note that symlink /dev/disk/snapd/disk can be expected by
 * snap-bootstrap. In that case, snap-initramfs-mounts.service should
 * have:
 *
 * ```
 * BindsTo=dev-disk-snapd--disk.device
 * After=dev-disk-snapd-disk.device
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
	"github.com/snapcore/snapd/cmd/snap-bootstrap/blkid"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/kcmdline"
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

	// FilesystemLabel is the label of the filesystem of the
	// partition. On MBR schemas, partitions do not have a name,
	// instead we probe for the label of the filesystem. So this
	// will only be set if it's a non-GPT.
	FilesystemLabel string
}

func isGpt(probe blkid.AbstractBlkidProbe) bool {
	pttype, err := probe.LookupValue("PTTYPE")
	if err != nil {
		return false
	}
	return pttype == "gpt"
}

func probeFilesystem(node string) (Partition, error) {
	var p Partition

	probe, err := blkid.NewProbeFromFilename(node)
	if err != nil {
		return p, err
	}
	defer probe.Close()

	probe.EnableSuperblocks(true)
	probe.SetSuperblockFlags(blkid.BLKID_SUBLKS_LABEL)

	if err := probe.DoSafeprobe(); err != nil {
		return p, err
	}

	val, err := probe.LookupValue("LABEL")
	if err != nil {
		return p, err
	}
	p.FilesystemLabel = val
	return p, nil
}

func probeDisk(node string) ([]Partition, error) {
	probe, err := blkid.NewProbeFromFilename(node)
	if err != nil {
		return nil, err
	}
	defer probe.Close()

	probe.EnablePartitions(true)
	probe.SetPartitionsFlags(blkid.BLKID_PARTS_ENTRY_DETAILS)

	if err := probe.DoSafeprobe(); err != nil {
		return nil, err
	}

	gpt := isGpt(probe)
	partitions, err := probe.GetPartitions()
	if err != nil {
		return nil, err
	}

	ret := make([]Partition, 0)
	for _, partition := range partitions.GetPartitions() {
		if !gpt {
			// For MBR we have to probe the filesystem for details
			pnode := fmt.Sprintf("%sp%d", node, partition.GetPartNo())
			p, err := probeFilesystem(pnode)
			if err != nil {
				return ret, err
			}
			ret = append(ret, p)
		} else {
			ret = append(ret, Partition{
				Name: partition.GetName(),
				UUID: partition.GetUUID(),
			})
		}
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

func scanDiskNodeFallback(output io.Writer, node string) error {
	var fallbackPartition string

	partitions, err := probeDisk(node)
	if err != nil {
		return fmt.Errorf("cannot get partitions: %s\n", err)
	}

	/*
	 * If LoaderDevicePartUUID was not set, it is probably because
	 * we did not boot with UEFI. In that case we try to detect
	 * disk with partition labels.
	 */

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

	/*
	 * If we are not in UEFI mode and snapd_system_disk is
	 * defined, we need to verify the disk also matches that. If
	 * not, we just return, ignoring this disk.
	 */
	values, err := kcmdline.KeyValues("snapd_system_disk")
	if err != nil {
		return fmt.Errorf("cannot read kernel command line: %s\n", err)
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
			return fmt.Errorf("cannot check snapd_system_disk kernel parameter: %s\n", err)
		}
		if !same {
			/*
			 * This block device is not the device
			 * requested from the command line. But this
			 * is not an error. There are lots of block
			 * devices.
			 */
			return nil
		}
	}

	for _, part := range partitions {
		if part.FilesystemLabel == fallbackPartition {
			fmt.Fprintf(output, "UBUNTU_DISK=1\n")
			return nil
		}
	}

	/*
	 * We have found the block device is not a boot device. But
	 * this is not an error. There are plenty of block devices
	 * that are not the boot device.
	 */
	return nil
}

func isCVM() (bool, error) {
	m, err := kcmdline.KeyValues("snapd_recovery_mode")
	if err != nil {
		return false, err
	}

	mode, hasMode := m["snapd_recovery_mode"]

	return hasMode && mode == boot.ModeRunCVM, nil
}

func scanDiskNode(output io.Writer, node string) error {
	/*
	 * We need to find out if the given node contains the ESP that
	 * was booted.  The boot loader will set
	 * LoaderDevicePartUUID. We will need to scan all the
	 * partitions for that UUID.
	 */

	bootUUID, err := bootFindPartitionUUIDForBootedKernelDisk()
	if err != nil {
		return scanDiskNodeFallback(output, node)
	}

	partitions, err := probeDisk(node)
	if err != nil {
		return fmt.Errorf("cannot get partitions: %s\n", err)
	}

	/*
	 * Now we scan the partitions. We need to find the partition
	 * grub booted from.
	 */
	found := false
	hasSeed := false
	hasBoot := false
	for _, part := range partitions {
		if part.UUID == bootUUID {
			/*
			 * We have just found the ESP boot partition!
			 */
			found = true
		}

		if part.Name == "ubuntu-seed" {
			hasSeed = true
		} else if part.Name == "ubuntu-boot" {
			hasBoot = true
		}
	}

	cvm, err := isCVM()
	if err != nil {
		logger.Noticef("WARNING: error while reading recovery mode: %v", err)
		return nil
	}

	/*
	 * We now print the result if we confirmed we found the boot ESP.
	 */
	if found && (hasSeed || hasBoot || cvm) {
		fmt.Fprintf(output, "UBUNTU_DISK=1\n")
	}

	/*
	 * We have found the block device is not a boot device. But
	 * this is not an error. There are plenty of block devices
	 * that are not the boot device.
	 */
	return nil
}

func ScanDisk(output io.Writer) error {
	devname := osGetenv("DEVNAME")
	if osGetenv("DEVTYPE") == "disk" {
		return scanDiskNode(output, devname)
	} else {
		return fmt.Errorf("unknown type for block device %s\n", devname)
	}
}
