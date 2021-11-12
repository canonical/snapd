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

// sfdiskDeviceDump represents the sfdisk --dump JSON output format.
type sfdiskDeviceDump struct {
	PartitionTable sfdiskPartitionTable `json:"partitiontable"`
}

type sfdiskPartitionTable struct {
	// Label from sfdisk is really the schema for the disk; DOS or GPT.
	Label string `json:"label"`
	// ID is the disk's identifier, it is a UUID for GPT disks or an unsigned
	// integer for DOS disks encoded as a string in hexadecimal as in
	// "0x1212e868".
	ID string `json:"id"`
	// Device is the full device node path in /dev as in /dev/sda.
	Device     string            `json:"device"`
	Unit       string            `json:"unit"`
	FirstLBA   uint64            `json:"firstlba"`
	LastLBA    uint64            `json:"lastlba"`
	Partitions []sfdiskPartition `json:"partitions"`
}

type sfdiskPartition struct {
	// Node is the full device node path in /dev as in /dev/sda1.
	Node  string `json:"node"`
	Start uint64 `json:"start"`
	Size  uint64 `json:"size"`
	// List of GPT partition attributes in <attr>[ <attr>] format, numeric attributes
	// are listed as GUID:<bit>[,<bit>]. Note that the even though the sfdisk(8) manpage
	// says --part-attrs takes a space or comma separated list, the output from
	// --json/--dump uses a different format.
	Attrs string `json:"attrs"`
	// Type is the structure type, which is the same as VolumeStructure's Type,
	// see that doc-comment for full details, but note that sfdisk may format
	// the type differently than would normally be used in gadget.yaml so
	// conversion should be done before using this Type field directly.
	Type string `json:"type"`
	// UUID is the partition UUID for the partition.
	UUID string `json:"uuid"`
	// Name is the GPT partition label for GPT disks. It is empty for MBR/DOS
	// disks.
	Name string `json:"name"`
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
	// ID is the disk's identifier, it is a UUID for GPT disks or an unsigned
	// integer for DOS disks encoded as a string in hexadecimal as in
	// "0x1212e868".
	ID string
	// Device is the full device node path for the disk, such as /dev/vda.
	Device string
	// Schema is the disk schema, GPT or DOS.
	Schema string
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

func blockdevSizeCmd(cmd, devpath string) (quantity.Size, error) {
	out, err := exec.Command("blockdev", cmd, devpath).CombinedOutput()
	if err != nil {
		return 0, osutil.OutputErr(out, err)
	}
	nospace := strings.TrimSpace(string(out))
	sz, err := strconv.Atoi(nospace)
	if err != nil {
		return 0, fmt.Errorf("cannot parse blockdev %s result size %q: %v", cmd, nospace, err)
	}
	return quantity.Size(sz), nil
}

func blockDeviceSizeInSectors(devpath string) (quantity.Size, error) {
	// the size is always reported in 512-byte sectors, even if the device does
	// not have a physical sector size of 512
	// XXX: consider using /sys/block/<dev>/size directly
	return blockdevSizeCmd("--getsz", devpath)
}

func blockDeviceSectorSize(devpath string) (quantity.Size, error) {
	// the size is reported in raw bytes
	sz, err := blockdevSizeCmd("--getss", devpath)
	if err != nil {
		return 0, err
	}

	// ensure that the sector size is a multiple of 512, since we rely on that
	// when we calculate the size in sectors, as blockdev --getsz always returns
	// the size in 512-byte sectors
	if sz%512 != 0 {
		return 0, fmt.Errorf("cannot calculate structure size: sector size (%s) is not a multiple of 512", sz.String())
	}
	if sz == 0 {
		// extra paranoia
		return 0, fmt.Errorf("internal error: sector size returned as 0")
	}
	return sz, nil
}

// onDiskVolumeFromPartitionTable takes an sfdisk dump partition table and returns
// the partitioning information as an on-disk volume.
func onDiskVolumeFromPartitionTable(ptable sfdiskPartitionTable) (*OnDiskVolume, error) {
	if ptable.Unit != "sectors" {
		return nil, fmt.Errorf("cannot position partitions: unknown unit %q", ptable.Unit)
	}

	structure := make([]VolumeStructure, len(ptable.Partitions))
	ds := make([]OnDiskStructure, len(ptable.Partitions))

	sectorSize, err := blockDeviceSectorSize(ptable.Device)
	if err != nil {
		return nil, err
	}

	for i, p := range ptable.Partitions {
		bd, err := filesystemInfoForPartition(p.Node)
		if err != nil {
			return nil, fmt.Errorf("cannot obtain filesystem information: %v", err)
		}

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
			ID:         strings.ToUpper(p.UUID),
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

		// since blockdev always reports the size in 512-byte sectors, if for
		// some reason we are on a disk that does not 512-byte sectors, we will
		// get confused, so in this case, multiply the number of 512-byte
		// sectors by 512, then divide by the actual sector size to get the
		// number of sectors

		// this will never have a divisor, since we verified that sector size is
		// a multiple of 512 above
		numSectors = sz * 512 / sectorSize
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

// lsblkInfo represents the lsblk JSON output format.
type lsblkInfo struct {
	BlockDevices []lsblkBlockDevice `json:"blockdevices"`
}

// lsblkBlockDevice represents a block device from the output of lsblk, which
// could either be a loopback device or a physical disk or a partition, etc.
// As such, only some fields are set depending on the context that the struct is
// returned in.
type lsblkBlockDevice struct {
	// common shared fields

	// Name is the name of the block device as identified by the node in /dev,
	// such as mmcblk0p1 or loop319 or vda. This is specifically just the name,
	// not the full path in /dev/.
	Name string `json:"name"`
	// Mountpoint is the mount point of the specific block device if it is
	// mounted, as determined by lsblk. Note that there could be multiple
	// mountpoints, it is unclear which specific one lsblk chooses to use as
	// this setting in this situation. For physical disk devices (not partition
	// devices), this is null/empty.
	Mountpoint string `json:"mountpoint"`

	// --fs option specific fields

	// FSType is the type of filesystem on this device, i.e. ext4, squashfs,
	// vfat, etc.
	FSType string `json:"fstype"`
	// Label is the filesystem label for a partition/device.
	Label string `json:"label"`
	// UUID is the filesystem UUID for a partition/device.
	UUID string `json:"uuid"`

	// no --fs option specific fields

	// Type is the type of block device, i.e. loop or disk typically.
	Type string `json:"type"`
}

// filesystemInfoForPartition returns information about the filesystem of a
// single partition, identified by the device node path for this partition such
// as /dev/mmcblk0p1 or /dev/sda1.
func filesystemInfoForPartition(node string) (blk lsblkBlockDevice, err error) {
	// verify that the specified node is indeed a partition by first running
	// lsblk without the --fs
	output, err := exec.Command("lsblk", "--json", node).CombinedOutput()
	if err != nil {
		return blk, osutil.OutputErr(output, err)
	}

	var info lsblkInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return blk, fmt.Errorf("cannot parse lsblk output: %v", err)
	}

	if len(info.BlockDevices) != 1 || strings.ToLower(info.BlockDevices[0].Type) != "part" {
		return blk, fmt.Errorf("device node %s is not a partition", node)
	}

	// otherwise the device is indeed a partition, get the information for the
	// filesystem

	// we only expect a single block device
	output, err = exec.Command("lsblk", "--fs", "--json", node).CombinedOutput()
	if err != nil {
		return blk, osutil.OutputErr(output, err)
	}

	if err := json.Unmarshal(output, &info); err != nil {
		return blk, fmt.Errorf("cannot parse lsblk output: %v", err)
	}

	switch len(info.BlockDevices) {
	case 1:
		// ok, expected, make the UUID capitalized for consistency
		toUpperUUID(&info.BlockDevices[0])
		return info.BlockDevices[0], nil
	case 0:
		// very unexpected, there was previously only one block device just
		// above but now we somehow don't have one
		return blk, fmt.Errorf("block device for device %s unexpectedly disappeared", node)
	default:
		// we now have more block devices for this partition, which is also
		// unexpected
		return blk, fmt.Errorf("block device for device %s unexpectedly multiplied", node)
	}
}

func toUpperUUID(bd *lsblkBlockDevice) {
	bd.UUID = strings.ToUpper(bd.UUID)
}
