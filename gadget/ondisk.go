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
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

const (
	ubuntuBootLabel = "ubuntu-boot"
	ubuntuSeedLabel = "ubuntu-seed"
	ubuntuDataLabel = "ubuntu-data"

	sectorSize Size = 512

	createdPartitionAttr = "59"
)

var createdPartitionGUID = []string{
	"0FC63DAF-8483-4772-8E79-3D69D8477DE4", // Linux filesystem data
	"0657FD6D-A4AB-43C4-84E5-0933C84B4F4F", // Linux swap partition
}

// creationSupported returns whether we support and expect to create partitions
// of the given type, it also means we are ready to remove them for re-installation
// or retried installation if they are appropriately marked with createdPartitionAttr.
func creationSupported(ptype string) bool {
	return strutil.ListContains(createdPartitionGUID, strings.ToUpper(ptype))
}

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

func isCreatedDuringInstall(p *sfdiskPartition, fs *lsblkBlockDevice, sfdiskLabel string) bool {
	switch sfdiskLabel {
	case "gpt":
		// the created partitions use specific GPT GUID types and set a
		// specific bit in partition attributes
		if !creationSupported(p.Type) {
			return false
		}
		for _, a := range strings.Fields(p.Attrs) {
			if !strings.HasPrefix(a, "GUID:") {
				continue
			}
			attrs := strings.Split(a[5:], ",")
			if strutil.ListContains(attrs, createdPartitionAttr) {
				return true
			}
		}
	case "dos":
		// we have no similar type/bit attribute setting for MBR, on top
		// of that MBR does not support partition names, fall back to
		// reasonable assumption that only partitions carrying
		// ubuntu-boot and ubuntu-data labels are created during
		// install, everything else was part of factory image

		// TODO:UC20 consider using gadget layout information to build a
		// mapping of partition start offset to label/name
		createdDuringInstall := []string{ubuntuBootLabel, ubuntuDataLabel}
		return strutil.ListContains(createdDuringInstall, fs.Label)
	}
	return false
}

// TODO: consider looking into merging LaidOutVolume/Structure OnDiskVolume/Structure

// OnDiskStructure represents a gadget structure laid on a block device.
type OnDiskStructure struct {
	LaidOutStructure

	// Node identifies the device node of the block device.
	Node string
	// CreatedDuringInstall is true when the structure has properties indicating
	// it was created based on the gadget description during installation.
	CreatedDuringInstall bool
}

// OnDiskVolume holds information about the disk device including its partitioning
// schema, the partition table, and the structure layout it contains.
type OnDiskVolume struct {
	Structure []OnDiskStructure
	ID        string
	Device    string
	Schema    string
	// size in bytes
	Size Size
	// sector size in bytes
	SectorSize     Size
	PartitionTable *sfdiskPartitionTable
}

// OnDiskVolumeFromDevice obtains the partitioning and filesystem information from
// the block device.
func OnDiskVolumeFromDevice(device string) (*OnDiskVolume, error) {
	output, err := exec.Command("sfdisk", "--json", "-d", device).Output()
	if err != nil {
		return nil, osutil.OutputErr(output, err)
	}

	var dump sfdiskDeviceDump
	if err := json.Unmarshal(output, &dump); err != nil {
		return nil, fmt.Errorf("cannot parse sfdisk output: %v", err)
	}

	dl, err := deviceLayoutFromPartitionTable(dump.PartitionTable)
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

func blockDeviceSizeInSectors(devpath string) (Size, error) {
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
	return Size(sz), nil
}

// deviceLayoutFromPartitionTable takes an sfdisk dump partition table and returns
// the partitioning information as a device layout.
func deviceLayoutFromPartitionTable(ptable sfdiskPartitionTable) (*OnDiskVolume, error) {
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
			Size:       Size(p.Size) * sectorSize,
			Label:      bd.Label,
			Type:       vsType,
			Filesystem: bd.FSType,
		}

		ds[i] = OnDiskStructure{
			LaidOutStructure: LaidOutStructure{
				VolumeStructure: &structure[i],
				StartOffset:     Size(p.Start) * sectorSize,
				Index:           i + 1,
			},
			Node:                 p.Node,
			CreatedDuringInstall: isCreatedDuringInstall(&p, &bd, ptable.Label),
		}
	}

	var numSectors Size
	if ptable.LastLBA != 0 {
		// sfdisk reports the last usable LBA for GPT disks only
		numSectors = Size(ptable.LastLBA + 1)
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
		Structure:      ds,
		ID:             ptable.ID,
		Device:         ptable.Device,
		Schema:         ptable.Label,
		Size:           numSectors * sectorSize,
		SectorSize:     sectorSize,
		PartitionTable: &ptable,
	}

	return dl, nil
}

func deviceName(name string, index int) string {
	if len(name) > 0 {
		last := name[len(name)-1]
		if last >= '0' && last <= '9' {
			return fmt.Sprintf("%sp%d", name, index)
		}
	}
	return fmt.Sprintf("%s%d", name, index)
}

// BuildPartitionList builds a list of partitions based on the current
// device contents and gadget structure list, in sfdisk dump format, and
// returns a partitioning description suitable for sfdisk input and a
// list of the partitions to be created.
func BuildPartitionList(dl *OnDiskVolume, pv *LaidOutVolume) (sfdiskInput *bytes.Buffer, toBeCreated []OnDiskStructure) {
	ptable := dl.PartitionTable

	// Keep track what partitions we already have on disk
	seen := map[uint64]bool{}
	for _, p := range ptable.Partitions {
		seen[p.Start] = true
	}

	// Check if the last partition has a system-data role
	canExpandData := false
	if n := len(pv.LaidOutStructure); n > 0 {
		last := pv.LaidOutStructure[n-1]
		if last.VolumeStructure.Role == SystemData {
			canExpandData = true
		}
	}

	// The partition index
	pIndex := 0

	// Write new partition data in named-fields format
	buf := &bytes.Buffer{}
	for _, p := range pv.LaidOutStructure {
		if !p.IsPartition() {
			continue
		}

		pIndex++
		s := p.VolumeStructure

		// Skip partitions that are already in the volume
		start := p.StartOffset / sectorSize
		if seen[uint64(start)] {
			continue
		}

		// Only allow the creation of partitions with known GUIDs
		// TODO:UC20: also provide a mechanism for MBR (RPi)
		ptype := partitionType(ptable.Label, p.Type)
		if ptable.Label == "gpt" && !creationSupported(ptype) {
			logger.Noticef("cannot create partition with unsupported type %s", ptype)
			continue
		}

		// Check if the data partition should be expanded
		size := s.Size
		if s.Role == SystemData && canExpandData && p.StartOffset+s.Size < dl.Size {
			size = dl.Size - p.StartOffset
		}

		// Can we use the index here? Get the largest existing partition number and
		// build from there could be safer if the disk partitions are not consecutive
		// (can this actually happen in our images?)
		node := deviceName(ptable.Device, pIndex)
		fmt.Fprintf(buf, "%s : start=%12d, size=%12d, type=%s, name=%q, attrs=\"GUID:%s\"\n", node,
			p.StartOffset/sectorSize, size/sectorSize, ptype, s.Name, createdPartitionAttr)

		// Set expected labels based on role
		switch s.Role {
		case SystemBoot:
			s.Label = ubuntuBootLabel
		case SystemSeed:
			s.Label = ubuntuSeedLabel
		case SystemData:
			s.Label = ubuntuDataLabel
		}

		toBeCreated = append(toBeCreated, OnDiskStructure{
			LaidOutStructure:     p,
			Node:                 node,
			CreatedDuringInstall: true,
		})
	}

	return buf, toBeCreated
}

// ListCreatedPartitions returns a list of partitions created during the
// install process.
func ListCreatedPartitions(layout *OnDiskVolume) []string {
	created := make([]string, 0, len(layout.Structure))
	for _, s := range layout.Structure {
		if s.CreatedDuringInstall {
			created = append(created, s.Node)
		}
	}
	return created
}

func partitionType(label, ptype string) string {
	t := strings.Split(ptype, ",")
	if len(t) < 1 {
		return ""
	}
	if len(t) == 1 {
		return t[0]
	}
	if label == "gpt" {
		return t[1]
	}
	return t[0]
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
