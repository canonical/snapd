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

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/cmd/snap-bootstrap/blkid"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/kcmdline"
)

var (
	efiReadVarString = efi.ReadVarString
	osGetenv         = os.Getenv
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
	fmt.Fprintf(os.Stderr, "Scanning disk %s\n", node)
	fallback := false
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
		fmt.Fprintf(output, "UBUNTU_DISK=1\n")
		if has_seed {
			fmt.Fprintf(os.Stderr, "Detected partition for seed: %s\n", seed_uuid)
			fmt.Fprintf(output, "UBUNTU_SEED_UUID=%s\n", seed_uuid)
		}
		if has_boot {
			fmt.Fprintf(os.Stderr, "Detected partition for boot: %s\n", boot_uuid)
			fmt.Fprintf(output, "UBUNTU_BOOT_UUID=%s\n", boot_uuid)
		}
		if has_data {
			fmt.Fprintf(os.Stderr, "Detected partition for data: %s\n", data_uuid)
			fmt.Fprintf(output, "UBUNTU_DATA_UUID=%s\n", data_uuid)
		}
		if has_save {
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
