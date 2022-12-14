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

package main

//#cgo CFLAGS: -D_FILE_OFFSET_BITS=64
//#cgo pkg-config: blkid
//#cgo LDFLAGS:
//
//#include <stdlib.h>
//#include <blkid.h>
import "C"

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/bootloader/efi"
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
	return scanDisk()
}

type Partition struct {
	Name string
	UUID string
}

func getProbeValue(probe C.blkid_probe, name string) (string, error) {
	var value *C.char
	var value_len C.size_t
	entryname := C.CString(name)
	defer C.free(unsafe.Pointer(entryname))
	res := C.blkid_probe_lookup_value(probe, entryname, &value, &value_len)
	if res < 0 {
		return "", fmt.Errorf("Probe value was not found: %s", name)
	}
	if value_len > 0 {
		return C.GoStringN(value, C.int(value_len-1)), nil
	} else {
		return "", fmt.Errorf("Probe value has unexpected size")
	}
}

func isGpt(probe C.blkid_probe) bool {
	pttype, err := getProbeValue(probe, "PTTYPE")
	if err != nil {
		return false
	}
	return pttype == "gpt"
}

func probePartitions(node string) ([]Partition, error) {
	cnode := C.CString(node)
	defer C.free(unsafe.Pointer(cnode))
	probe, err := C.blkid_new_probe_from_filename(cnode)
	if probe == nil {
		return nil, err
	}
	defer C.blkid_free_probe(probe)

	C.blkid_probe_enable_partitions(probe, 1)
	C.blkid_probe_set_partitions_flags(probe, C.BLKID_PARTS_ENTRY_DETAILS)
	C.blkid_probe_enable_superblocks(probe, 1)

	res, err := C.blkid_do_safeprobe(probe)
	if res < 0 {
		return nil, err
	}

	if !isGpt(probe) {
		return nil, nil
	}

	partitions, err := C.blkid_probe_get_partitions(probe)
	if partitions == nil {
		return nil, err
	}

	npartitions := C.blkid_partlist_numof_partitions(partitions)
	ret := make([]Partition, 0)
	for i := 0; i < int(npartitions); i++ {
		partition := C.blkid_partlist_get_partition(partitions, C.int(i))
		label := C.GoString(C.blkid_partition_get_name(partition))
		uuid := C.GoString(C.blkid_partition_get_uuid(partition))
		fmt.Fprintf(os.Stderr, "Found partition %s %s\n", label, uuid)
		ret = append(ret, Partition{label, uuid})
	}

	return ret, nil
}

func scanDiskNode(node string) error {
	fmt.Fprintf(os.Stderr, "Scanning disk %s\n", node)
	fallback := false
	bootUUID, _, err := efi.ReadVarString("LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f")
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

	found := false
	has_seed := false
	var seed_uuid string
	has_boot := false
	var boot_uuid string
	has_data := false
	var data_uuid string
	has_save := false
	var save_uuid string
	for _, part := range partitions {
		if !fallback {
			if part.UUID == bootUUID {
				found = true
			}
		}
		if part.Name == "ubuntu-seed" {
			has_seed = true
			seed_uuid = part.UUID
		} else if part.Name == "ubuntu-boot" {
			has_boot = true
			boot_uuid = part.UUID
		} else if part.Name == "ubuntu-data-enc" {
			has_data = true
			data_uuid = part.UUID
		} else if part.Name == "ubuntu-data" {
			has_data = true
			data_uuid = part.UUID
		} else if part.Name == "ubuntu-save-enc" {
			has_save = true
			save_uuid = part.UUID
		} else if part.Name == "ubuntu-save" {
			has_save = true
			save_uuid = part.UUID
		}
	}

	if (!fallback && found) || (fallback && has_seed) {
		fmt.Printf("UBUNTU_DISK=1\n")
		if has_seed {
			fmt.Fprintf(os.Stderr, "Detected partition for seed: %s\n", seed_uuid)
			fmt.Printf("UBUNTU_SEED_UUID=%s\n", seed_uuid)
		}
		if has_boot {
			fmt.Fprintf(os.Stderr, "Detected partition for boot: %s\n", boot_uuid)
			fmt.Printf("UBUNTU_BOOT_UUID=%s\n", boot_uuid)
		}
		if has_data {
			fmt.Fprintf(os.Stderr, "Detected partition for data: %s\n", data_uuid)
			fmt.Printf("UBUNTU_DATA_UUID=%s\n", data_uuid)
		}
		if has_save {
			fmt.Fprintf(os.Stderr, "Detected partition for save: %s\n", save_uuid)
			fmt.Printf("UBUNTU_SAVE_UUID=%s\n", save_uuid)
		}
	}

	return nil
}

func checkPartitionUUID(suffix string, partUUID string) {
	varname := fmt.Sprintf("UBUNTU_%s_UUID", suffix)
	expectedUUID := os.Getenv(varname)
	if len(expectedUUID) > 0 && expectedUUID == partUUID {
		fmt.Fprintf(os.Stderr, "Detected partition as %s\n", suffix)
		fmt.Printf("UBUNTU_%s=1\n", suffix)
	}
}

func scanPartitionNode(node string) error {
	fmt.Fprintf(os.Stderr, "Scanning partition %s\n", node)

	cnode := C.CString(node)
	defer C.free(unsafe.Pointer(cnode))
	probe, err := C.blkid_new_probe_from_filename(cnode)
	if probe == nil {
		return fmt.Errorf("Cannot create probe for partition %s: %s\n", node, err)
	}
	defer C.blkid_free_probe(probe)

	C.blkid_probe_enable_partitions(probe, 1)
	C.blkid_probe_set_partitions_flags(probe, C.BLKID_PARTS_ENTRY_DETAILS)
	C.blkid_probe_enable_superblocks(probe, 1)

	res, err := C.blkid_do_safeprobe(probe)
	if res < 0 {
		return fmt.Errorf("Cannot probe partition %s: %s\n", node, err)
	}

	partUUID, err := getProbeValue(probe, "PART_ENTRY_UUID")
	if err != nil {
		return fmt.Errorf("Cannot get uuid for partition: %s\n", err)
	}

	for _, suffix := range []string{"SEED", "BOOT", "DATA", "SAVE"} {
		checkPartitionUUID(suffix, partUUID)
	}

	return nil
}

func scanDisk() error {
	devname := os.Getenv("DEVNAME")
	if os.Getenv("DEVTYPE") == "disk" {
		return scanDiskNode(devname)
	} else if os.Getenv("DEVTYPE") == "partition" {
		return scanPartitionNode(devname)
	} else {
		return fmt.Errorf("Unknown type for block device %s\n", devname)
	}
}
