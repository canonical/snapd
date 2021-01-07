// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package gadget

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
)

const (
	sectorSize quantity.Size = 512
)

// sfdiskDeviceDump represents the sfdisk --dump JSON output format.
type sfdiskDeviceDump struct {
	PartitionTable sfdiskPartitionTable `json:"partitiontable"`
}

type sfdiskPartitionTable struct {
	Label      string            `json:"label"`
	ID         string            `json:"id"`
	Device     string            `json:"device"`
	Unit       string            `json:"unit"`
	FirstLBA   uint64            `json:"firstlba"`
	LastLBA    uint64            `json:"lastlba"`
	Partitions []sfdiskPartition `json:"partitions"`
}

type sfdiskPartition struct {
	Node  string `json:"node"`
	Start uint64 `json:"start"`
	Size  uint64 `json:"size"`
	// List of GPT partition attributes in <attr>[ <attr>] format, numeric attributes
	// are listed as GUID:<bit>[,<bit>]. Note that the even though the sfdisk(8) manpage
	// says --part-attrs takes a space or comma separated list, the output from
	// --json/--dump uses a different format.
	Attrs string `json:"attrs"`
	Type  string `json:"type"`
	UUID  string `json:"uuid"`
	Name  string `json:"name"`
}

// TODO: consider looking into merging LaidOutVolume/Structure OnDiskVolume/Structure

// OnDiskStructure represents a gadget structure laid on a block device.
type OnDiskStructure struct {
	LaidOutStructure

	// Node identifies the device node of the block device.
	Node string

	// Size of the on disk structure, which is at least equal to the
	// LaidOutStructure.Size but may be bigger if the partition was
	// expanded.
	Size quantity.Size
}

// OnDiskVolume holds information about the disk device including its partitioning
// schema, the partition table, and the structure layout it contains.
type OnDiskVolume struct {
	Structure []OnDiskStructure
	ID        string
	Device    string
	Schema    string
	// size in bytes
	Size quantity.Size
	// sector size in bytes
	SectorSize quantity.Size
}

// OnDiskVolumeFromDevice obtains the partitioning and filesystem information from
// the block device.
func OnDiskVolumeFromDevice(device string) (*OnDiskVolume, error) {
	output, err := exec.Command("sfdisk", "--json", device).Output()
	if err != nil {
		return nil, osutil.OutputErr(output, err)
	}

	var dump sfdiskDeviceDump
	if err := json.Unmarshal(output, &dump); err != nil {
		return nil, fmt.Errorf("cannot parse sfdisk output: %v", err)
	}

	dl, err := onDiskVolumeFromPartitionTable(dump.PartitionTable)
	if err != nil {
		return nil, err
	}
	dl.Device = device

	return dl, nil
}

func fromSfdiskPartitionType(st string, sfdiskLabel string) (string, error) {
	switch sfdiskLabel {
	case "dos":
		// sometimes sfdisk reports what is "0C" in gadget.yaml as "c",
		// normalize the values
		v, err := strconv.ParseUint(st, 16, 8)
		if err != nil {
			return "", fmt.Errorf("cannot convert MBR partition type %q", st)
		}
		return fmt.Sprintf("%02X", v), nil
	case "gpt":
		return st, nil
	default:
		return "", fmt.Errorf("unsupported partitioning schema %q", sfdiskLabel)
	}
}

func blockDeviceSizeInSectors(devpath string) (quantity.Size, error) {
	// the size is reported in 512-byte sectors
	// XXX: consider using /sys/block/<dev>/size directly
	out, err := exec.Command("blockdev", "--getsz", devpath).CombinedOutput()
	if err != nil {
		return 0, osutil.OutputErr(out, err)
	}
	nospace := strings.TrimSpace(string(out))
	sz, err := strconv.Atoi(nospace)
	if err != nil {
		return 0, fmt.Errorf("cannot parse device size %q: %v", nospace, err)
	}
	return quantity.Size(sz), nil
}

// onDiskVolumeFromPartitionTable takes an sfdisk dump partition table and returns
// the partitioning information as an on-disk volume.
func onDiskVolumeFromPartitionTable(ptable sfdiskPartitionTable) (*OnDiskVolume, error) {
	if ptable.Unit != "sectors" {
		return nil, fmt.Errorf("cannot position partitions: unknown unit %q", ptable.Unit)
	}

	structure := make([]VolumeStructure, len(ptable.Partitions))
	ds := make([]OnDiskStructure, len(ptable.Partitions))

	for i, p := range ptable.Partitions {
		info, err := filesystemInfo(p.Node)
		if err != nil {
			return nil, fmt.Errorf("cannot obtain filesystem information: %v", err)
		}
		switch {
		case len(info.BlockDevices) == 0:
			continue
		case len(info.BlockDevices) > 1:
			return nil, fmt.Errorf("unexpected number of blockdevices for node %q: %v", p.Node, info.BlockDevices)
		}
		bd := info.BlockDevices[0]

		vsType, err := fromSfdiskPartitionType(p.Type, ptable.Label)
		if err != nil {
			return nil, fmt.Errorf("cannot convert sfdisk partition type %q: %v", p.Type, err)
		}

		structure[i] = VolumeStructure{
			Name:       p.Name,
			Size:       quantity.Size(p.Size) * sectorSize,
			Label:      bd.Label,
			Type:       vsType,
			Filesystem: bd.FSType,
		}

		ds[i] = OnDiskStructure{
			LaidOutStructure: LaidOutStructure{
				VolumeStructure: &structure[i],
				StartOffset:     quantity.Offset(p.Start) * quantity.Offset(sectorSize),
				Index:           i + 1,
			},
			Node: p.Node,
		}
	}

	var numSectors quantity.Size
	if ptable.LastLBA != 0 {
		// sfdisk reports the last usable LBA for GPT disks only
		numSectors = quantity.Size(ptable.LastLBA + 1)
	} else {
		// sfdisk does not report any information about the size of a
		// MBR partitioned disk, find out the size of the device by
		// other means
		sz, err := blockDeviceSizeInSectors(ptable.Device)
		if err != nil {
			return nil, fmt.Errorf("cannot obtain the size of device %q: %v", ptable.Device, err)
		}
		numSectors = sz
	}

	dl := &OnDiskVolume{
		Structure:  ds,
		ID:         ptable.ID,
		Device:     ptable.Device,
		Schema:     ptable.Label,
		Size:       numSectors * sectorSize,
		SectorSize: sectorSize,
	}

	return dl, nil
}

// UpdatePartitionList re-reads the partitioning data from the device and
// updates the volume structures in the specified volume.
func UpdatePartitionList(dl *OnDiskVolume) error {
	layout, err := OnDiskVolumeFromDevice(dl.Device)
	if err != nil {
		return fmt.Errorf("cannot read disk layout: %v", err)
	}
	if dl.ID != layout.ID {
		return fmt.Errorf("partition table IDs don't match")
	}

	dl.Structure = layout.Structure
	return nil
}

// lsblkFilesystemInfo represents the lsblk --fs JSON output format.
type lsblkFilesystemInfo struct {
	BlockDevices []lsblkBlockDevice `json:"blockdevices"`
}

type lsblkBlockDevice struct {
	Name       string `json:"name"`
	FSType     string `json:"fstype"`
	Label      string `json:"label"`
	UUID       string `json:"uuid"`
	Mountpoint string `json:"mountpoint"`
}

func filesystemInfo(node string) (*lsblkFilesystemInfo, error) {
	output, err := exec.Command("lsblk", "--fs", "--json", node).CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(output, err)
	}

	var info lsblkFilesystemInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("cannot parse lsblk output: %v", err)
	}

	return &info, nil
}
