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

package gadgettest

import (
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil/disks"
)

const oneMeg = uint64(quantity.SizeMiB)

//
// Raspi related examples
//

// from a rpi without the kernel assets or content layout for simplicity's sake
const RaspiSimplifiedYaml = `
volumes:
  pi:
    bootloader: u-boot
    schema: mbr
    structure:
    - filesystem: vfat
      name: ubuntu-seed
      role: system-seed
      size: 1200M
      type: 0C
    - filesystem: vfat
      name: ubuntu-boot
      role: system-boot
      size: 750M
      type: 0C
    - filesystem: ext4
      name: ubuntu-save
      role: system-save
      size: 16M
      type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
    - filesystem: ext4
      name: ubuntu-data
      role: system-data
      size: 1500M
      type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
`

// from a rpi without the kernel assets or content layout for simplicity's sake
// and without ubuntu-save
const RaspiSimplifiedNoSaveYaml = `
volumes:
  pi:
    bootloader: u-boot
    schema: mbr
    structure:
    - filesystem: vfat
      name: ubuntu-seed
      role: system-seed
      size: 1200M
      type: 0C
    - filesystem: vfat
      name: ubuntu-boot
      role: system-boot
      size: 750M
      type: 0C
    - filesystem: ext4
      name: ubuntu-data
      role: system-data
      size: 1500M
      type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
`

// from UC18 image, for testing the implicit system data partition case
const RaspiUC18SimplifiedYaml = `
volumes:
  pi:
    schema: mbr
    bootloader: u-boot
    structure:
      - type: 0C
        filesystem: vfat
        filesystem-label: system-boot
        size: 256M
`

var expPiSeedStructureTraits = gadget.DiskStructureDeviceTraits{
	OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
	OriginalKernelPath: "/dev/mmcblk0p1",
	PartitionUUID:      "7c301cbd-01",
	PartitionType:      "0C",
	FilesystemUUID:     "0E09-0822",
	FilesystemLabel:    "ubuntu-seed",
	FilesystemType:     "vfat",
	Offset:             quantity.OffsetMiB,
	Size:               (1200) * quantity.SizeMiB,
}

var expPiBootStructureTraits = gadget.DiskStructureDeviceTraits{
	OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p2",
	OriginalKernelPath: "/dev/mmcblk0p2",
	PartitionUUID:      "7c301cbd-02",
	PartitionType:      "0C",
	FilesystemUUID:     "23F9-881F",
	FilesystemLabel:    "ubuntu-boot",
	FilesystemType:     "vfat",
	Offset:             (1 + 1200) * quantity.OffsetMiB,
	Size:               (750) * quantity.SizeMiB,
}

var expPiSaveStructureTraits = gadget.DiskStructureDeviceTraits{
	OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
	OriginalKernelPath: "/dev/mmcblk0p3",
	PartitionUUID:      "7c301cbd-03",
	PartitionType:      "83",
	FilesystemUUID:     "1cdd5826-e9de-4d27-83f7-20249e710590",
	FilesystemType:     "ext4",
	FilesystemLabel:    "ubuntu-save",
	Offset:             (1 + 1200 + 750) * quantity.OffsetMiB,
	Size:               16 * quantity.SizeMiB,
}

var expPiDataStructureTraits = gadget.DiskStructureDeviceTraits{
	OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p4",
	OriginalKernelPath: "/dev/mmcblk0p4",
	PartitionUUID:      "7c301cbd-04",
	PartitionType:      "83",
	FilesystemUUID:     "d7f39661-1da0-48de-8967-ce41343d4345",
	FilesystemLabel:    "ubuntu-data",
	FilesystemType:     "ext4",
	Offset:             (1 + 1200 + 750 + 16) * quantity.OffsetMiB,
	// total size - offset of last structure
	Size: (30528 - (1 + 1200 + 750 + 16)) * quantity.SizeMiB,
}

var expPiDataNoSaveStructureTraits = gadget.DiskStructureDeviceTraits{
	OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
	OriginalKernelPath: "/dev/mmcblk0p3",
	PartitionUUID:      "7c301cbd-03",
	PartitionType:      "83",
	FilesystemUUID:     "d7f39661-1da0-48de-8967-ce41343d4345",
	FilesystemLabel:    "ubuntu-data",
	FilesystemType:     "ext4",
	Offset:             (1 + 1200 + 750) * quantity.OffsetMiB,
	// total size - offset of last structure
	Size: (30528 - (1 + 1200 + 750)) * quantity.SizeMiB,
}

var ExpectedRaspiDiskVolumeDeviceTraits = gadget.DiskVolumeDeviceTraits{
	OriginalDevicePath:  "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
	OriginalKernelPath:  "/dev/mmcblk0",
	DiskID:              "7c301cbd",
	Size:                30528 * quantity.SizeMiB, // ~ 32 GB SD card
	SectorSize:          512,
	Schema:              "dos",
	StructureEncryption: map[string]gadget.StructureEncryptionParameters{},
	Structure: []gadget.DiskStructureDeviceTraits{
		expPiSeedStructureTraits,
		expPiBootStructureTraits,
		expPiSaveStructureTraits,
		expPiDataStructureTraits,
	},
}

var ExpectedRaspiDiskVolumeDeviceNoSaveTraits = gadget.DiskVolumeDeviceTraits{
	OriginalDevicePath:  "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
	OriginalKernelPath:  "/dev/mmcblk0",
	DiskID:              "7c301cbd",
	Size:                30528 * quantity.SizeMiB, // ~ 32 GB SD card
	SectorSize:          512,
	Schema:              "dos",
	StructureEncryption: map[string]gadget.StructureEncryptionParameters{},
	Structure: []gadget.DiskStructureDeviceTraits{
		expPiSeedStructureTraits,
		expPiBootStructureTraits,
		expPiDataNoSaveStructureTraits,
	},
}

var expPiSaveEncStructureTraits = gadget.DiskStructureDeviceTraits{
	OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
	OriginalKernelPath: "/dev/mmcblk0p3",
	PartitionUUID:      "7c301cbd-03",
	PartitionType:      "83",
	FilesystemUUID:     "1cdd5826-e9de-4d27-83f7-20249e710590",
	FilesystemType:     "crypto_LUKS",
	FilesystemLabel:    "ubuntu-save-enc",
	Offset:             (1 + 1200 + 750) * quantity.OffsetMiB,
	Size:               16 * quantity.SizeMiB,
}

var expPiDataEncStructureTraits = gadget.DiskStructureDeviceTraits{
	OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p4",
	OriginalKernelPath: "/dev/mmcblk0p4",
	PartitionUUID:      "7c301cbd-04",
	PartitionType:      "83",
	FilesystemUUID:     "d7f39661-1da0-48de-8967-ce41343d4345",
	FilesystemLabel:    "ubuntu-data-enc",
	FilesystemType:     "crypto_LUKS",
	Offset:             (1 + 1200 + 750 + 16) * quantity.OffsetMiB,
	// total size - offset of last structure
	Size: (30528 - (1 + 1200 + 750 + 16)) * quantity.SizeMiB,
}

// ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits is like
// ExpectedRaspiDiskVolumeDeviceTraits, but it uses the "-enc" suffix for the
// filesystem labels and has crypto_LUKS as the filesystem types
var ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits = gadget.DiskVolumeDeviceTraits{
	OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
	OriginalKernelPath: "/dev/mmcblk0",
	DiskID:             "7c301cbd",
	Size:               30528 * quantity.SizeMiB, // ~ 32 GB SD card
	SectorSize:         512,
	Schema:             "dos",
	StructureEncryption: map[string]gadget.StructureEncryptionParameters{
		"ubuntu-data": {Method: gadget.EncryptionLUKS},
		"ubuntu-save": {Method: gadget.EncryptionLUKS},
	},
	Structure: []gadget.DiskStructureDeviceTraits{
		expPiSeedStructureTraits,
		expPiBootStructureTraits,
		expPiSaveEncStructureTraits,
		expPiDataEncStructureTraits,
	},
}

// ExpectedRaspiUC18DiskVolumeDeviceTraits, for testing the implicit system
// data partition case
var ExpectedRaspiUC18DiskVolumeDeviceTraits = gadget.DiskVolumeDeviceTraits{
	OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
	OriginalKernelPath: "/dev/mmcblk0",
	DiskID:             "7c301cbd",
	Size:               32010928128,
	SectorSize:         512,
	Schema:             "dos",
	Structure: []gadget.DiskStructureDeviceTraits{
		{
			OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
			OriginalKernelPath: "/dev/mmcblk0p1",
			PartitionUUID:      "7c301cbd-01",
			PartitionType:      "0C",
			PartitionLabel:     "",
			FilesystemUUID:     "23F9-881F",
			FilesystemLabel:    "system-boot",
			FilesystemType:     "vfat",
			Offset:             quantity.OffsetMiB,
			Size:               256 * quantity.SizeMiB,
		},
		// note no writable structure here - since it's not in the YAML, we
		// don't save it in the traits either
	},
}

var mockSeedPartition = disks.Partition{
	PartitionUUID:    "7c301cbd-01",
	PartitionType:    "0C",
	FilesystemLabel:  "ubuntu-seed",
	FilesystemUUID:   "0E09-0822",
	FilesystemType:   "vfat",
	Major:            179,
	Minor:            1,
	KernelDeviceNode: "/dev/mmcblk0p1",
	KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
	DiskIndex:        1,
	StartInBytes:     oneMeg,
	SizeInBytes:      1200 * oneMeg,
}

var mockBootPartition = disks.Partition{
	PartitionUUID:    "7c301cbd-02",
	PartitionType:    "0C",
	FilesystemLabel:  "ubuntu-boot",
	FilesystemUUID:   "23F9-881F",
	FilesystemType:   "vfat",
	Major:            179,
	Minor:            2,
	KernelDeviceNode: "/dev/mmcblk0p2",
	KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p2",
	DiskIndex:        2,
	StartInBytes:     (1 + 1200) * oneMeg,
	SizeInBytes:      750 * oneMeg,
}

var ExpectedRaspiMockDiskMapping = &disks.MockDiskMapping{
	DevNode:             "/dev/mmcblk0",
	DevPath:             "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
	DevNum:              "179:0",
	DiskUsableSectorEnd: 30528 * oneMeg / 512,
	DiskSizeInBytes:     30528 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "dos",
	ID:                  "7c301cbd",
	Structure: []disks.Partition{
		mockSeedPartition,
		mockBootPartition,
		{
			PartitionUUID:    "7c301cbd-03",
			PartitionType:    "83",
			FilesystemLabel:  "ubuntu-save",
			FilesystemUUID:   "1cdd5826-e9de-4d27-83f7-20249e710590",
			FilesystemType:   "ext4",
			Major:            179,
			Minor:            3,
			KernelDeviceNode: "/dev/mmcblk0p3",
			KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
			DiskIndex:        3,
			StartInBytes:     (1 + 1200 + 750) * oneMeg,
			SizeInBytes:      16 * oneMeg,
		},
		{
			PartitionUUID:    "7c301cbd-04",
			PartitionType:    "83",
			FilesystemLabel:  "ubuntu-data",
			FilesystemUUID:   "d7f39661-1da0-48de-8967-ce41343d4345",
			FilesystemType:   "ext4",
			Major:            179,
			Minor:            4,
			KernelDeviceNode: "/dev/mmcblk0p4",
			KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p4",
			DiskIndex:        4,
			StartInBytes:     (1 + 1200 + 750 + 16) * oneMeg,
			SizeInBytes:      (30528 - (1 + 1200 + 750 + 16)) * oneMeg,
		},
	},
}

var ExpectedRaspiMockDiskMappingNoSave = &disks.MockDiskMapping{
	DevNode:             "/dev/mmcblk0",
	DevPath:             "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
	DevNum:              "179:0",
	DiskUsableSectorEnd: 30528 * oneMeg / 512,
	DiskSizeInBytes:     30528 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "dos",
	ID:                  "7c301cbd",
	Structure: []disks.Partition{
		mockSeedPartition,
		mockBootPartition,
		{
			PartitionUUID:    "7c301cbd-03",
			PartitionType:    "83",
			FilesystemLabel:  "ubuntu-data",
			FilesystemUUID:   "d7f39661-1da0-48de-8967-ce41343d4345",
			FilesystemType:   "ext4",
			Major:            179,
			Minor:            3,
			KernelDeviceNode: "/dev/mmcblk0p3",
			KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
			DiskIndex:        3,
			StartInBytes:     (1 + 1200 + 750) * oneMeg,
			SizeInBytes:      (30528 - (1 + 1200 + 750)) * oneMeg,
		},
	},
}

// ExpectedLUKSEncryptedRaspiMockDiskMapping is like
// ExpectedRaspiMockDiskMapping, but it uses the "-enc" suffix for the
// filesystem labels and has crypto_LUKS as the filesystem types
var ExpectedLUKSEncryptedRaspiMockDiskMapping = &disks.MockDiskMapping{
	DevNode:             "/dev/mmcblk0",
	DevPath:             "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
	DevNum:              "179:0",
	DiskUsableSectorEnd: 30528 * oneMeg / 512,
	DiskSizeInBytes:     30528 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "dos",
	ID:                  "7c301cbd",
	Structure: []disks.Partition{
		mockSeedPartition,
		mockBootPartition,
		// pretend that we do LUKS encryption for the pi and make these
		// encrypted partitions
		{
			PartitionUUID:    "7c301cbd-03",
			PartitionType:    "83",
			FilesystemLabel:  "ubuntu-save-enc",
			FilesystemUUID:   "1cdd5826-e9de-4d27-83f7-20249e710590",
			FilesystemType:   "crypto_LUKS",
			Major:            179,
			Minor:            3,
			KernelDeviceNode: "/dev/mmcblk0p3",
			KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
			DiskIndex:        3,
			StartInBytes:     (1 + 1200 + 750) * oneMeg,
			SizeInBytes:      16 * oneMeg,
		},
		{
			PartitionUUID:    "7c301cbd-04",
			PartitionType:    "83",
			FilesystemLabel:  "ubuntu-data-enc",
			FilesystemUUID:   "d7f39661-1da0-48de-8967-ce41343d4345",
			FilesystemType:   "crypto_LUKS",
			Major:            179,
			Minor:            4,
			KernelDeviceNode: "/dev/mmcblk0p4",
			KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p4",
			DiskIndex:        4,
			StartInBytes:     (1 + 1200 + 750 + 16) * oneMeg,
			SizeInBytes:      (30528 - (1 + 1200 + 750 + 16)) * oneMeg,
		},
	},
}

// ExpectedRaspiMockDiskInstallModeMapping is like ExpectedRaspiMockDiskMapping
// but for fresh install mode image where we only have the ubuntu-seed partition
// on disk.
var ExpectedRaspiMockDiskInstallModeMapping = &disks.MockDiskMapping{
	DevNode:             "/dev/mmcblk0",
	DevPath:             "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
	DevNum:              "179:0",
	DiskUsableSectorEnd: 30528 * oneMeg / 512,
	DiskSizeInBytes:     30528 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "dos",
	ID:                  "7c301cbd",
	Structure: []disks.Partition{
		// only ubuntu-seed
		mockSeedPartition,
	},
}

// ExpectedRaspiUC18MockDiskMapping, for testing the implicit system data partition case
var ExpectedRaspiUC18MockDiskMapping = &disks.MockDiskMapping{
	DevNode:             "/dev/mmcblk0",
	DevPath:             "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
	DevNum:              "179:0",
	DiskUsableSectorEnd: 30528 * oneMeg / 512,
	DiskSizeInBytes:     30528 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "dos",
	ID:                  "7c301cbd",
	Structure: []disks.Partition{
		{
			PartitionUUID:    "7c301cbd-01",
			PartitionType:    "0C",
			FilesystemLabel:  "system-boot",
			FilesystemUUID:   "23F9-881F",
			FilesystemType:   "vfat",
			Major:            179,
			Minor:            1,
			KernelDeviceNode: "/dev/mmcblk0p1",
			KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
			DiskIndex:        1,
			StartInBytes:     oneMeg,
			SizeInBytes:      256 * oneMeg,
		},
		{
			PartitionUUID:    "7c301cbd-02",
			PartitionType:    "83",
			FilesystemLabel:  "writable",
			FilesystemUUID:   "cba2b8b3-c2e4-4e51-9a57-d35041b7bf9a",
			FilesystemType:   "ext4",
			Major:            179,
			Minor:            2,
			KernelDeviceNode: "/dev/mmcblk0p2",
			KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p2",
			DiskIndex:        2,
			StartInBytes:     (1 + 256) * oneMeg,
			SizeInBytes:      32270 * oneMeg,
		},
	},
}

const ExpectedRaspiDiskVolumeDeviceTraitsJSON = `
{
  "pi": {
    "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
    "kernel-path": "/dev/mmcblk0",
    "disk-id": "7c301cbd",
    "size": 32010928128,
    "sector-size": 512,
    "schema": "dos",
	"structure-encryption": {},
    "structure": [
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
        "kernel-path": "/dev/mmcblk0p1",
        "partition-uuid": "7c301cbd-01",
        "partition-label": "",
        "partition-type": "0C",
        "filesystem-label": "ubuntu-seed",
        "filesystem-uuid": "0E09-0822",
        "filesystem-type": "vfat",
        "offset": 1048576,
        "size": 1258291200
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p2",
        "kernel-path": "/dev/mmcblk0p2",
        "partition-uuid": "7c301cbd-02",
        "partition-label": "",
        "partition-type": "0C",
        "filesystem-label": "ubuntu-boot",
        "filesystem-uuid": "23F9-881F",
        "filesystem-type": "vfat",
        "offset": 1259339776,
        "size": 786432000
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
        "kernel-path": "/dev/mmcblk0p3",
        "partition-uuid": "7c301cbd-03",
        "partition-label": "",
        "partition-type": "83",
        "filesystem-label": "ubuntu-save",
        "filesystem-uuid": "1cdd5826-e9de-4d27-83f7-20249e710590",
        "filesystem-type": "ext4",
        "offset": 2045771776,
        "size": 16777216
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p4",
        "kernel-path": "/dev/mmcblk0p4",
        "partition-uuid": "7c301cbd-04",
        "partition-label": "",
        "partition-type": "83",
        "filesystem-label": "ubuntu-data",
        "filesystem-uuid": "d7f39661-1da0-48de-8967-ce41343d4345",
        "filesystem-type": "ext4",
        "offset": 2062548992,
        "size": 29948379136
      }
    ]
  }
}
`

const ExpectedRaspiDiskVolumeNoSaveDeviceTraitsJSON = `
{
  "pi": {
    "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
    "kernel-path": "/dev/mmcblk0",
    "disk-id": "7c301cbd",
    "size": 32010928128,
    "sector-size": 512,
    "schema": "dos",
	"structure-encryption": {},
    "structure": [
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
        "kernel-path": "/dev/mmcblk0p1",
        "partition-uuid": "7c301cbd-01",
        "partition-label": "",
        "partition-type": "0C",
        "filesystem-label": "ubuntu-seed",
        "filesystem-uuid": "0E09-0822",
        "filesystem-type": "vfat",
        "offset": 1048576,
        "size": 1258291200
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p2",
        "kernel-path": "/dev/mmcblk0p2",
        "partition-uuid": "7c301cbd-02",
        "partition-label": "",
        "partition-type": "0C",
        "filesystem-label": "ubuntu-boot",
        "filesystem-uuid": "23F9-881F",
        "filesystem-type": "vfat",
        "offset": 1259339776,
        "size": 786432000
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
        "kernel-path": "/dev/mmcblk0p3",
        "partition-uuid": "7c301cbd-03",
        "partition-label": "",
        "partition-type": "83",
        "filesystem-label": "ubuntu-data",
        "filesystem-uuid": "d7f39661-1da0-48de-8967-ce41343d4345",
        "filesystem-type": "ext4",
        "offset": 2045771776,
        "size": 29965156352
      }
    ]
  }
}
`

const ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraitsJSON = `
{
  "pi": {
    "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
    "kernel-path": "/dev/mmcblk0",
    "disk-id": "7c301cbd",
    "size": 32010928128,
    "sector-size": 512,
    "schema": "dos",
	"structure-encryption": {
		"ubuntu-data": {
			"method": "LUKS"
		},
		"ubuntu-save": {
			"method": "LUKS"
		}
	},
    "structure": [
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
        "kernel-path": "/dev/mmcblk0p1",
        "partition-uuid": "7c301cbd-01",
        "partition-label": "",
        "partition-type": "0C",
        "filesystem-label": "ubuntu-seed",
        "filesystem-uuid": "0E09-0822",
        "filesystem-type": "vfat",
        "offset": 1048576,
        "size": 1258291200
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p2",
        "kernel-path": "/dev/mmcblk0p2",
        "partition-uuid": "7c301cbd-02",
        "partition-label": "",
        "partition-type": "0C",
        "filesystem-label": "ubuntu-boot",
        "filesystem-uuid": "23F9-881F",
        "filesystem-type": "vfat",
        "offset": 1259339776,
        "size": 786432000
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
        "kernel-path": "/dev/mmcblk0p3",
        "partition-uuid": "7c301cbd-03",
        "partition-label": "",
        "partition-type": "83",
        "filesystem-label": "ubuntu-save-enc",
        "filesystem-uuid": "1cdd5826-e9de-4d27-83f7-20249e710590",
        "filesystem-type": "crypto_LUKS",
        "offset": 2045771776,
        "size": 16777216
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p4",
        "kernel-path": "/dev/mmcblk0p4",
        "partition-uuid": "7c301cbd-04",
        "partition-label": "",
        "partition-type": "83",
        "filesystem-label": "ubuntu-data-enc",
        "filesystem-uuid": "d7f39661-1da0-48de-8967-ce41343d4345",
        "filesystem-type": "crypto_LUKS",
        "offset": 2062548992,
        "size": 29948379136
      }
    ]
  }
}
`

//
// Mock devices
//

const MockExtraVolumeYAML = `
volumes:
  foo:
    bootloader: u-boot
    schema: gpt
    structure:
      - name: barething
        type: bare
        size: 1024
      - name: nofspart
        type: EBBEADAF-22C9-E33B-8F5D-0E81686A68CB
        size: 4096
      - name: some-filesystem
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

var MockExtraVolumeDiskMapping = &disks.MockDiskMapping{
	DevNode: "/dev/foo",
	DevPath: "/sys/block/foo",
	DevNum:  "525:1",
	// assume 34 sectors at end for GPT headers backup
	DiskUsableSectorEnd: 6000*oneMeg/512 - 34,
	DiskSizeInBytes:     6000 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "gpt",
	ID:                  "651AC800-B9FB-4B9D-B6D3-A72EB54D9006",
	Structure: []disks.Partition{
		// note that the "barething" is not present here since we
		// cannot really measure or observe its existence
		{
			PartitionLabel:   "nofspart",
			PartitionUUID:    "C5A930DF-E86A-4BAE-A4C5-C861353796E6",
			PartitionType:    "EBBEADAF-22C9-E33B-8F5D-0E81686A68CB",
			FilesystemType:   "",
			Major:            525,
			Minor:            2,
			KernelDeviceNode: "/dev/foo1",
			KernelDevicePath: "/sys/block/foo/foo1",
			DiskIndex:        1,
			// but note that the start does take the presence of the first
			// bare structure into account
			StartInBytes: oneMeg + 1024,
			SizeInBytes:  4096,
		},
		{
			PartitionLabel:   "some-filesystem",
			PartitionUUID:    "DA2ADBC8-90DF-4B1D-A93F-A92516C12E01",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemLabel:  "some-filesystem",
			FilesystemUUID:   "3E3D392C-5D50-4C84-8A6E-09B7A3FEA2C7",
			FilesystemType:   "ext4",
			Major:            525,
			Minor:            3,
			KernelDeviceNode: "/dev/foo2",
			KernelDevicePath: "/sys/block/foo/foo2",
			DiskIndex:        2,
			StartInBytes:     oneMeg + 1024 + 4096,
			SizeInBytes:      1024 * oneMeg,
		},
	},
}

var MockExtraVolumeDeviceTraits = gadget.DiskVolumeDeviceTraits{
	OriginalDevicePath: "/sys/block/foo",
	OriginalKernelPath: "/dev/foo",
	DiskID:             "651AC800-B9FB-4B9D-B6D3-A72EB54D9006",
	SectorSize:         512,
	Size:               6000 * quantity.SizeMiB,
	Schema:             "gpt",
	Structure: []gadget.DiskStructureDeviceTraits{
		// note that the barething is not present here since we
		// cannot really measure or observe its existence
		{
			PartitionLabel:     "nofspart",
			PartitionUUID:      "C5A930DF-E86A-4BAE-A4C5-C861353796E6",
			PartitionType:      "EBBEADAF-22C9-E33B-8F5D-0E81686A68CB",
			OriginalDevicePath: "/sys/block/foo/foo1",
			OriginalKernelPath: "/dev/foo1",
			// but the offsets take the barething's existence into
			// account
			Offset: quantity.OffsetMiB + quantity.OffsetKiB,
			Size:   4096,
		},
		{
			PartitionLabel:     "some-filesystem",
			PartitionUUID:      "DA2ADBC8-90DF-4B1D-A93F-A92516C12E01",
			PartitionType:      "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			OriginalDevicePath: "/sys/block/foo/foo2",
			OriginalKernelPath: "/dev/foo2",
			FilesystemLabel:    "some-filesystem",
			FilesystemUUID:     "3E3D392C-5D50-4C84-8A6E-09B7A3FEA2C7",
			FilesystemType:     "ext4",
			Offset:             quantity.OffsetMiB + quantity.OffsetKiB + 4096,
			Size:               quantity.SizeGiB,
		},
	},
}

//
// Real VM Device
//

const SingleVolumeUC20GadgetYaml = `
volumes:
  pc:
    schema: gpt
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        # whats the appropriate size?
        size: 750M
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

const SingleVolumeUC20GadgetYamlSeedNoBIOS = `
volumes:
  pc:
    schema: gpt
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        # whats the appropriate size?
        size: 750M
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

const SingleVolumeClassicWithModesGadgetYaml = `
volumes:
  pc:
    # bootloader configuration is shipped and managed by snapd
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        update:
          edition: 1
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        role: system-seed-null
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        update:
          edition: 2
        content:
          - image: pc-core.img
      - name: EFI System partition
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        # TODO make this realistically smaller?
        size: 99M
        update:
          edition: 2
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        offset: 1202M
        size: 750M
        update:
          edition: 1
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 4G
`

const SingleVolumeClassicWithModesPartialGadgetYaml = `
volumes:
  pc:
    partial: [schema, structure, filesystem, size]
    bootloader: grub
    structure:
      - name: ubuntu-seed
        role: system-seed-null
        filesystem: vfat
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        offset: 2M
        size: 1200M
      - name: ubuntu-boot
        role: system-boot
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
      - name: ubuntu-save
        role: system-save
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
      - name: ubuntu-data
        role: system-data
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

const SingleVolumeClassicWithModesFilledPartialGadgetYaml = `
volumes:
  pc:
    bootloader: grub
    partial: [structure]
    schema: gpt
    structure:
      - name: ubuntu-seed
        role: system-seed-null
        filesystem: vfat
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        offset: 2M
        size: 99M
        update:
          edition: 2
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        offset: 1202M
        size: 750M
        update:
          edition: 1
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
      - name: ubuntu-save
        filesystem: ext4
        role: system-save
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        filesystem: ext4
        role: system-data
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 4G
`

const MultiVolumeUC20GadgetYamlNoBIOS = SingleVolumeUC20GadgetYamlSeedNoBIOS + `
  foo:
    schema: gpt
    structure:
      - name: barething
        type: bare
        size: 4096
      - name: nofspart
        type: A11D2A7C-D82A-4C2F-8A01-1805240E6626
        size: 4096
      - name: some-filesystem
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

const MultiVolumeUC20GadgetYaml = SingleVolumeUC20GadgetYaml + `
  foo:
    schema: gpt
    structure:
      - name: barething
        type: bare
        size: 4096
      - name: nofspart
        type: A11D2A7C-D82A-4C2F-8A01-1805240E6626
        size: 4096
      - name: some-filesystem
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

const SingleVolumeClassicwithModesNoEncryptGadgetYaml = `
volumes:
  pc:
    schema: gpt
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: EFI System Partition
        role: system-seed-null
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 750M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

const SingleVolumeClassicwithModesEncryptGadgetYaml = `
volumes:
  pc:
    schema: gpt
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: EFI System Partition
        role: system-seed-null
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 750M
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

var VMExtraVolumeDiskMapping = &disks.MockDiskMapping{
	DevNode: "/dev/vdb",
	DevPath: "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb",
	DevNum:  "525:1",
	// assume 34 sectors at end for GPT headers backup
	DiskUsableSectorEnd: 5120*oneMeg/512 - 34,
	DiskSizeInBytes:     5120 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "gpt",
	ID:                  "86964016-3b5c-477e-9828-24ba9c552d39",
	Structure: []disks.Partition{
		// first structure is "barething" but it doesn't show up in the
		// partition table
		{
			PartitionLabel:   "nofspart",
			PartitionUUID:    "691d89b6-d4c1-4c08-a060-fc10d98337da",
			PartitionType:    "A11D2A7C-D82A-4C2F-8A01-1805240E6626",
			FilesystemType:   "",
			Major:            525,
			Minor:            2,
			KernelDeviceNode: "/dev/vdb1",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb/vdb1",
			DiskIndex:        1,
			// oneMeg is the NonMBRStartOffset, 4096 is barething
			StartInBytes: oneMeg + 4096,
			SizeInBytes:  4096,
		},
		{
			PartitionLabel:   "some-filesystem",
			PartitionUUID:    "7a3c051c-eae2-4c36-bed8-1914709ebf4c",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemLabel:  "some-filesystem",
			FilesystemUUID:   "afb36b05-3796-4edf-87aa-9f9aa22f9324",
			FilesystemType:   "ext4",
			Major:            525,
			Minor:            3,
			KernelDeviceNode: "/dev/vdb2",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb/vdb2",
			DiskIndex:        2,
			StartInBytes:     4096 + oneMeg + 4096,
			SizeInBytes:      1024 * oneMeg,
		},
	},
}

var VMSystemVolumeDiskMapping = &disks.MockDiskMapping{
	DevNode: "/dev/vda",
	DevPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda",
	DevNum:  "600:1",
	// assume 34 sectors at end for GPT headers backup
	DiskUsableSectorEnd: 5120*oneMeg/512 - 34,
	DiskSizeInBytes:     5120 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "gpt",
	ID:                  "f0eef013-a777-4a27-aaf0-dbb5cf68c2b6",
	Structure: []disks.Partition{
		{
			KernelDeviceNode: "/dev/vda1",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda1",
			PartitionUUID:    "420e5a20-b888-42e2-b7df-ced5cbf14517",
			PartitionLabel:   "BIOS\\x20Boot",
			PartitionType:    "21686148-6449-6E6F-744E-656564454649",
			StartInBytes:     oneMeg,
			SizeInBytes:      oneMeg,
			Major:            600,
			Minor:            2,
			DiskIndex:        1,
		},
		{
			KernelDeviceNode: "/dev/vda2",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda2",
			PartitionUUID:    "4b436628-71ba-43f9-aa12-76b84fe32728",
			PartitionLabel:   "ubuntu-seed",
			PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			FilesystemUUID:   "04D6-5AE2",
			FilesystemLabel:  "ubuntu-seed",
			FilesystemType:   "vfat",
			StartInBytes:     (1 + 1) * oneMeg,
			SizeInBytes:      1200 * oneMeg,
			Major:            600,
			Minor:            3,
			DiskIndex:        2,
		},
		{
			KernelDeviceNode: "/dev/vda3",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda3",
			PartitionUUID:    "ade3ba65-7831-fd40-bbe2-e01c9774ed5b",
			PartitionLabel:   "ubuntu-boot",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "5b3e775a-407d-4af7-aa16-b92a8b7507e6",
			FilesystemLabel:  "ubuntu-boot",
			FilesystemType:   "ext4",
			StartInBytes:     (1200 + 1 + 1) * oneMeg,
			SizeInBytes:      750 * oneMeg,
			Major:            600,
			Minor:            4,
			DiskIndex:        3,
		},
		{
			KernelDeviceNode: "/dev/vda4",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda4",
			PartitionUUID:    "f1d01870-194b-8a45-84c0-0d1c90e17d9d",
			PartitionLabel:   "ubuntu-save",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "6766b605-9cd5-47ae-bc48-807c778b9987",
			FilesystemLabel:  "ubuntu-save",
			FilesystemType:   "ext4",
			StartInBytes:     (1200 + 1 + 1 + 750) * oneMeg,
			SizeInBytes:      16 * oneMeg,
			Major:            600,
			Minor:            5,
			DiskIndex:        4,
		},
		{
			KernelDeviceNode: "/dev/vda5",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda5",
			PartitionUUID:    "4994f0e5-1ead-1a4d-b696-2d8cb1fa980d",
			PartitionLabel:   "ubuntu-data",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "4e29a1e9-526d-48fc-a5c2-4f97e7e011e2",
			FilesystemLabel:  "ubuntu-data",
			FilesystemType:   "ext4",
			StartInBytes:     (1200 + 1 + 1 + 750 + 16) * oneMeg,
			// including the last usable sector - the offset
			SizeInBytes: ((5120*oneMeg - 33*512) - (1+1+1200+750+16)*oneMeg),
			Major:       600,
			Minor:       6,
			DiskIndex:   5,
		},
	},
}

var VMSystemVolumeDiskMappingSeedFsLabelCaps = &disks.MockDiskMapping{
	DevNode: "/dev/vda",
	DevPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda",
	DevNum:  "600:1",
	// assume 34 sectors at end for GPT headers backup
	DiskUsableSectorEnd: 5120*oneMeg/512 - 34,
	DiskSizeInBytes:     5120 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "gpt",
	ID:                  "f0eef013-a777-4a27-aaf0-dbb5cf68c2b6",
	Structure: []disks.Partition{
		{
			KernelDeviceNode: "/dev/vda1",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda2",
			PartitionUUID:    "4b436628-71ba-43f9-aa12-76b84fe32728",
			PartitionLabel:   "ubuntu-seed",
			PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			FilesystemUUID:   "04D6-5AE2",
			FilesystemLabel:  "UBUNTU-SEED",
			FilesystemType:   "vfat",
			StartInBytes:     1 * oneMeg,
			SizeInBytes:      1200 * oneMeg,
			Major:            600,
			Minor:            3,
			DiskIndex:        1,
		},
		{
			KernelDeviceNode: "/dev/vda2",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda3",
			PartitionUUID:    "ade3ba65-7831-fd40-bbe2-e01c9774ed5b",
			PartitionLabel:   "ubuntu-boot",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "5b3e775a-407d-4af7-aa16-b92a8b7507e6",
			FilesystemLabel:  "ubuntu-boot",
			FilesystemType:   "ext4",
			StartInBytes:     (1200 + 1) * oneMeg,
			SizeInBytes:      750 * oneMeg,
			Major:            600,
			Minor:            4,
			DiskIndex:        2,
		},
		{
			KernelDeviceNode: "/dev/vda3",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda4",
			PartitionUUID:    "f1d01870-194b-8a45-84c0-0d1c90e17d9d",
			PartitionLabel:   "ubuntu-save",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "6766b605-9cd5-47ae-bc48-807c778b9987",
			FilesystemLabel:  "ubuntu-save",
			FilesystemType:   "ext4",
			StartInBytes:     (1200 + 1 + 750) * oneMeg,
			SizeInBytes:      16 * oneMeg,
			Major:            600,
			Minor:            5,
			DiskIndex:        3,
		},
		{
			KernelDeviceNode: "/dev/vda4",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda5",
			PartitionUUID:    "4994f0e5-1ead-1a4d-b696-2d8cb1fa980d",
			PartitionLabel:   "ubuntu-data",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "4e29a1e9-526d-48fc-a5c2-4f97e7e011e2",
			FilesystemLabel:  "ubuntu-data",
			FilesystemType:   "ext4",
			StartInBytes:     (1200 + 1 + 750 + 16) * oneMeg,
			// including the last usable sector - the offset
			SizeInBytes: ((5120*oneMeg - 33*512) - (1+1+1200+750+16)*oneMeg),
			Major:       600,
			Minor:       6,
			DiskIndex:   4,
		},
	},
}

var VMExtraVolumeDeviceTraits = gadget.DiskVolumeDeviceTraits{
	OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb",
	OriginalKernelPath: "/dev/vdb",
	DiskID:             "86964016-3b5c-477e-9828-24ba9c552d39",
	Size:               5120 * quantity.SizeMiB,
	SectorSize:         quantity.Size(512),
	Schema:             "gpt",
	Structure: []gadget.DiskStructureDeviceTraits{
		// first real structure is a bare structure that does not show up
		// in the partition table and thus is absent from the traits
		// Structure list

		// second structure is a partition with no filesystem so it shows up
		{
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb/vdb1",
			OriginalKernelPath: "/dev/vdb1",
			PartitionUUID:      "691d89b6-d4c1-4c08-a060-fc10d98337da",
			PartitionLabel:     "nofspart",
			PartitionType:      "A11D2A7C-D82A-4C2F-8A01-1805240E6626",
			Offset:             quantity.OffsetMiB + 4096,
			Size:               4096,
		},
		// this one has a filesystem though
		{
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb/vdb2",
			OriginalKernelPath: "/dev/vdb2",
			PartitionUUID:      "7a3c051c-eae2-4c36-bed8-1914709ebf4c",
			PartitionLabel:     "some-filesystem",
			PartitionType:      "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:     "afb36b05-3796-4edf-87aa-9f9aa22f9324",
			FilesystemLabel:    "some-filesystem",
			FilesystemType:     "ext4",
			Offset:             quantity.OffsetMiB + 4096 + 4096,
			Size:               quantity.SizeGiB,
		},
	},
}

var VMSystemVolumeDeviceTraits = gadget.DiskVolumeDeviceTraits{
	OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda",
	OriginalKernelPath: "/dev/vda",
	DiskID:             "f0eef013-a777-4a27-aaf0-dbb5cf68c2b6",
	Schema:             "gpt",
	Size:               5120 * quantity.SizeMiB,
	SectorSize:         quantity.Size(512),
	Structure: []gadget.DiskStructureDeviceTraits{
		{
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda1",
			OriginalKernelPath: "/dev/vda1",
			PartitionUUID:      "420e5a20-b888-42e2-b7df-ced5cbf14517",
			PartitionLabel:     "BIOS\\x20Boot",
			PartitionType:      "21686148-6449-6E6F-744E-656564454649",
			Offset:             quantity.OffsetMiB,
			Size:               quantity.SizeMiB,
		},
		{
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda2",
			OriginalKernelPath: "/dev/vda2",
			PartitionUUID:      "4b436628-71ba-43f9-aa12-76b84fe32728",
			PartitionLabel:     "ubuntu-seed",
			PartitionType:      "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			FilesystemUUID:     "04D6-5AE2",
			FilesystemLabel:    "ubuntu-seed",
			FilesystemType:     "vfat",
			Offset:             (1 + 1) * quantity.OffsetMiB,
			Size:               1200 * quantity.SizeMiB,
		},
		{
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda3",
			OriginalKernelPath: "/dev/vda3",
			PartitionUUID:      "ade3ba65-7831-fd40-bbe2-e01c9774ed5b",
			PartitionLabel:     "ubuntu-boot",
			PartitionType:      "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:     "5b3e775a-407d-4af7-aa16-b92a8b7507e6",
			FilesystemLabel:    "ubuntu-boot",
			FilesystemType:     "ext4",
			Offset:             (1 + 1 + 1200) * quantity.OffsetMiB,
			Size:               750 * quantity.SizeMiB,
		},
		{
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda4",
			OriginalKernelPath: "/dev/vda4",
			PartitionUUID:      "f1d01870-194b-8a45-84c0-0d1c90e17d9d",
			PartitionLabel:     "ubuntu-save",
			PartitionType:      "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:     "6766b605-9cd5-47ae-bc48-807c778b9987",
			FilesystemLabel:    "ubuntu-save",
			FilesystemType:     "ext4",
			Offset:             (1 + 1 + 1200 + 750) * quantity.OffsetMiB,
			Size:               16 * quantity.SizeMiB,
		},
		{
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda5",
			OriginalKernelPath: "/dev/vda5",
			PartitionUUID:      "4994f0e5-1ead-1a4d-b696-2d8cb1fa980d",
			PartitionLabel:     "ubuntu-data",
			PartitionType:      "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:     "4e29a1e9-526d-48fc-a5c2-4f97e7e011e2",
			FilesystemLabel:    "ubuntu-data",
			FilesystemType:     "ext4",
			Offset:             (1 + 1 + 1200 + 750 + 16) * quantity.OffsetMiB,
			// including the last usable sector - the offset
			Size: ((5120*quantity.SizeMiB - 33*512) - (1+1+1200+750+16)*quantity.SizeMiB),
		},
	},
}

// like VMMultiVolumeUC20DiskTraitsJSON but without the foo volume
const VMSingleVolumeUC20DiskTraitsJSON = `
{
	"pc": {
		"device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda",
		"kernel-path": "/dev/vda",
		"disk-id": "f0eef013-a777-4a27-aaf0-dbb5cf68c2b6",
		"size": 5368709120,
		"sector-size": 512,
		"schema": "gpt",
		"structure": [
		  {
			"device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda1",
			"kernel-path": "/dev/vda1",
			"partition-uuid": "420e5a20-b888-42e2-b7df-ced5cbf14517",
			"partition-label": "BIOS\\x20Boot",
			"partition-type": "21686148-6449-6E6F-744E-656564454649",
			"filesystem-uuid": "",
			"filesystem-label": "",
			"filesystem-type": "",
			"offset": 1048576,
			"size": 1048576
		  },
		  {
			"device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda2",
			"kernel-path": "/dev/vda2",
			"partition-uuid": "4b436628-71ba-43f9-aa12-76b84fe32728",
			"partition-label": "ubuntu-seed",
			"partition-type": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			"filesystem-uuid": "04D6-5AE2",
			"filesystem-label": "ubuntu-seed",
			"filesystem-type": "vfat",
			"offset": 2097152,
			"size": 1258291200
		  },
		  {
			"device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda3",
			"kernel-path": "/dev/vda3",
			"partition-uuid": "ade3ba65-7831-fd40-bbe2-e01c9774ed5b",
			"partition-label": "ubuntu-boot",
			"partition-type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			"filesystem-uuid": "5b3e775a-407d-4af7-aa16-b92a8b7507e6",
			"filesystem-label": "ubuntu-boot",
			"filesystem-type": "ext4",
			"offset": 1260388352,
			"size": 786432000
		  },
		  {
			"device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda4",
			"kernel-path": "/dev/vda4",
			"partition-uuid": "f1d01870-194b-8a45-84c0-0d1c90e17d9d",
			"partition-label": "ubuntu-save",
			"partition-type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			"filesystem-uuid": "6766b605-9cd5-47ae-bc48-807c778b9987",
			"filesystem-label": "ubuntu-save",
			"filesystem-type": "ext4",
			"offset": 2046820352,
			"size": 16777216
		  },
		  {
			"device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda5",
			"kernel-path": "/dev/vda5",
			"partition-uuid": "4994f0e5-1ead-1a4d-b696-2d8cb1fa980d",
			"partition-label": "ubuntu-data",
			"partition-type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			"filesystem-uuid": "4e29a1e9-526d-48fc-a5c2-4f97e7e011e2",
			"filesystem-label": "ubuntu-data",
			"filesystem-type": "ext4",
			"offset": 2063597568,
			"size": 3305094656
		  }
		]
	  }
	}
`

const VMMultiVolumeUC20DiskTraitsJSON = `
{
  "foo": {
    "device-path": "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb",
    "kernel-path": "/dev/vdb",
    "disk-id": "86964016-3b5c-477e-9828-24ba9c552d39",
    "size": 5368709120,
    "sector-size": 512,
    "schema": "gpt",
    "structure": [
      {
        "device-path": "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb/vdb1",
        "kernel-path": "/dev/vdb1",
        "partition-uuid": "691d89b6-d4c1-4c08-a060-fc10d98337da",
        "partition-label": "nofspart",
        "partition-type": "A11D2A7C-D82A-4C2F-8A01-1805240E6626",
        "filesystem-uuid": "",
        "filesystem-label": "",
        "filesystem-type": "",
        "offset": 1052672,
        "size": 4096
      },
      {
        "device-path": "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb/vdb2",
        "kernel-path": "/dev/vdb2",
        "partition-uuid": "7a3c051c-eae2-4c36-bed8-1914709ebf4c",
        "partition-label": "some-filesystem",
        "partition-type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
        "filesystem-uuid": "afb36b05-3796-4edf-87aa-9f9aa22f9324",
        "filesystem-label": "some-filesystem",
        "filesystem-type": "ext4",
        "offset": 1056768,
        "size": 1073741824
      }
    ]
  },
  "pc": {
    "device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda",
    "kernel-path": "/dev/vda",
    "disk-id": "f0eef013-a777-4a27-aaf0-dbb5cf68c2b6",
    "size": 5368709120,
    "sector-size": 512,
    "schema": "gpt",
    "structure": [
      {
        "device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda1",
        "kernel-path": "/dev/vda1",
        "partition-uuid": "420e5a20-b888-42e2-b7df-ced5cbf14517",
        "partition-label": "BIOS\\x20Boot",
        "partition-type": "21686148-6449-6E6F-744E-656564454649",
        "filesystem-uuid": "",
        "filesystem-label": "",
        "filesystem-type": "",
        "offset": 1048576,
        "size": 1048576
      },
      {
        "device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda2",
        "kernel-path": "/dev/vda2",
        "partition-uuid": "4b436628-71ba-43f9-aa12-76b84fe32728",
        "partition-label": "ubuntu-seed",
        "partition-type": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
        "filesystem-uuid": "04D6-5AE2",
        "filesystem-label": "ubuntu-seed",
        "filesystem-type": "vfat",
        "offset": 2097152,
        "size": 1258291200
      },
      {
        "device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda3",
        "kernel-path": "/dev/vda3",
        "partition-uuid": "ade3ba65-7831-fd40-bbe2-e01c9774ed5b",
        "partition-label": "ubuntu-boot",
        "partition-type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
        "filesystem-uuid": "5b3e775a-407d-4af7-aa16-b92a8b7507e6",
        "filesystem-label": "ubuntu-boot",
        "filesystem-type": "ext4",
        "offset": 1260388352,
        "size": 786432000
      },
      {
        "device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda4",
        "kernel-path": "/dev/vda4",
        "partition-uuid": "f1d01870-194b-8a45-84c0-0d1c90e17d9d",
        "partition-label": "ubuntu-save",
        "partition-type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
        "filesystem-uuid": "6766b605-9cd5-47ae-bc48-807c778b9987",
        "filesystem-label": "ubuntu-save",
        "filesystem-type": "ext4",
        "offset": 2046820352,
        "size": 16777216
      },
      {
        "device-path": "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda5",
        "kernel-path": "/dev/vda5",
        "partition-uuid": "4994f0e5-1ead-1a4d-b696-2d8cb1fa980d",
        "partition-label": "ubuntu-data",
        "partition-type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
        "filesystem-uuid": "4e29a1e9-526d-48fc-a5c2-4f97e7e011e2",
        "filesystem-label": "ubuntu-data",
        "filesystem-type": "ext4",
        "offset": 2063597568,
        "size": 3305094656
      }
    ]
  }
}
`

//
// implicit system-data role for uc16 / uc18
//

// adapted from https://github.com/snapcore/pc-amd64-gadget/blob/16/gadget.yaml
// but without the content
const UC16YAMLImplicitSystemData = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: EFI System
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: vfat
        filesystem-label: system-boot
        size: 50M
`

// uc16 layout from a VM that has an implicit system-data as the third
// partition
var UC16DeviceLayout = gadget.OnDiskVolume{
	Structure: []gadget.OnDiskStructure{
		{
			Name:        "BIOS Boot",
			Type:        "21686148-6449-6E6F-744E-656564454649",
			StartOffset: quantity.OffsetMiB,
			DiskIndex:   1,
			Node:        "/dev/sda1",
			Size:        quantity.SizeMiB,
		},
		{
			Name:             "EFI System",
			Type:             "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			PartitionFSLabel: "system-boot",
			PartitionFSType:  "vfat",
			StartOffset:      2097152,
			DiskIndex:        2,
			Node:             "/dev/sda2",
			Size:             52428800,
		},
		{
			Name:             "writable",
			Type:             "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			PartitionFSLabel: "writable",
			PartitionFSType:  "ext4",
			Size:             10682875392,
			StartOffset:      54525952,
			DiskIndex:        3,
			Node:             "/dev/sda3",
		},
	},
	ID:               "2a9b0671-4597-433b-b3ad-be99950e8c5e",
	Device:           "/dev/sda",
	Schema:           "gpt",
	Size:             10737418240,
	UsableSectorsEnd: 20971487,
	SectorSize:       512,
}

var UC16ImplicitSystemDataMockDiskMapping = &disks.MockDiskMapping{
	DevNode: "/dev/sda",
	DevPath: "/sys/devices/pci0000:00/0000:00:01.1/ata1/host0/target0:0:0/0:0:0:0/block/sda",
	DevNum:  "600:1",
	// assume 34 sectors at end for GPT headers backup
	DiskUsableSectorEnd: 10240*oneMeg/512 - 33,
	DiskSizeInBytes:     10240 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "gpt",
	ID:                  "f69dbcfe-1258-4b36-9d1f-817fdeb61aa3",
	Structure: []disks.Partition{
		{
			KernelDeviceNode: "/dev/sda1",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:01.1/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda1",
			PartitionUUID:    "420e5a20-b888-42e2-b7df-ced5cbf14517",
			PartitionLabel:   "BIOS\\x20Boot",
			PartitionType:    "21686148-6449-6E6F-744E-656564454649",
			StartInBytes:     oneMeg,
			SizeInBytes:      oneMeg,
			Major:            600,
			Minor:            2,
			DiskIndex:        1,
		},
		{
			KernelDeviceNode: "/dev/sda2",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:01.1/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda2",
			PartitionUUID:    "fc8626b9-af30-4b3c-83c4-05bed39bb82e",
			PartitionLabel:   "EFI\\x20System",
			PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			FilesystemUUID:   "6D21-B3FE",
			FilesystemLabel:  "system-boot",
			FilesystemType:   "vfat",
			// size of first structure + offset of first structure
			StartInBytes: (1 + 1) * oneMeg,
			SizeInBytes:  50 * oneMeg,
			Major:        600,
			Minor:        3,
			DiskIndex:    2,
		},
		// has writable partition here since it does physically exist on disk
		{
			KernelDeviceNode: "/dev/sda3",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:01.1/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda3",
			PartitionUUID:    "ade3ba65-7831-fd40-bbe2-e01c9774ed5b",
			PartitionLabel:   "writable",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "cba2b8b3-c2e4-4e51-9a57-d35041b7bf9a",
			FilesystemLabel:  "writable",
			FilesystemType:   "ext4",
			// size of first structure + offset of first structure and size + offset of second structure
			StartInBytes: (50 + 1 + 1) * oneMeg,
			SizeInBytes:  10682875392,
			Major:        600,
			Minor:        4,
			DiskIndex:    3,
		},
	},
}

var UC16ImplicitSystemDataDeviceTraits = gadget.DiskVolumeDeviceTraits{
	OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:01.1/ata1/host0/target0:0:0/0:0:0:0/block/sda",
	OriginalKernelPath: "/dev/sda",
	DiskID:             "f69dbcfe-1258-4b36-9d1f-817fdeb61aa3",
	Size:               10737418240,
	SectorSize:         512,
	Schema:             "gpt",
	Structure: []gadget.DiskStructureDeviceTraits{
		{
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:01.1/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda1",
			OriginalKernelPath: "/dev/sda1",
			PartitionUUID:      "420e5a20-b888-42e2-b7df-ced5cbf14517",
			PartitionType:      "21686148-6449-6E6F-744E-656564454649",
			PartitionLabel:     "BIOS\\x20Boot",
			Offset:             quantity.OffsetMiB,
			Size:               quantity.SizeMiB,
		},
		{
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:01.1/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda2",
			OriginalKernelPath: "/dev/sda2",
			PartitionUUID:      "fc8626b9-af30-4b3c-83c4-05bed39bb82e",
			PartitionType:      "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			PartitionLabel:     "EFI\\x20System",
			FilesystemType:     "vfat",
			FilesystemUUID:     "6D21-B3FE",
			FilesystemLabel:    "system-boot",
			Offset:             quantity.OffsetMiB + quantity.OffsetMiB,
			Size:               50 * quantity.SizeMiB,
		},
		// note no writable structure here - since it's not in the YAML, we
		// don't save it in the traits either
	},
}
