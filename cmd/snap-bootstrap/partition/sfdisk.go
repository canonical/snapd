// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package partition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
)

const (
	ubuntuBootLabel = "ubuntu-boot"
	ubuntuSeedLabel = "ubuntu-seed"
	ubuntuDataLabel = "ubuntu-data"

	sectorSize gadget.Size = 512
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
	Type  string `json:"type"`
	UUID  string `json:"uuid"`
	Name  string `json:"name"`
}

type DeviceLayout struct {
	Structure      []DeviceStructure
	ID             string
	Device         string
	Schema         string
	Size           gadget.Size
	SectorSize     gadget.Size
	partitionTable *sfdiskPartitionTable
}

type DeviceStructure struct {
	gadget.LaidOutStructure

	Node string
}

// NewDeviceLayout obtains the partitioning and filesystem information from the
// block device.
func DeviceLayoutFromDisk(device string) (*DeviceLayout, error) {
	output, err := exec.Command("sfdisk", "--json", "-d", device).Output()
	if err != nil {
		return nil, osutil.OutputErr(output, err)
	}

	var dump sfdiskDeviceDump
	if err := json.Unmarshal(output, &dump); err != nil {
		return nil, fmt.Errorf("cannot parse sfdisk output: %v", err)
	}

	dl, err := deviceLayoutFromDump(&dump)
	if err != nil {
		return nil, err
	}
	dl.Device = device

	return dl, nil
}

var (
	ensureNodesExist = ensureNodesExistImpl
)

// CreateMissing creates the partitions listed in the positioned volume pv
// that are missing from the existing device layout.
func (dl *DeviceLayout) CreateMissing(pv *gadget.LaidOutVolume) ([]DeviceStructure, error) {
	buf, created := buildPartitionList(dl.partitionTable, pv)
	if len(created) == 0 {
		return created, nil
	}

	// Write the partition table. By default sfdisk will try to re-read the
	// partition table with the BLKRRPART ioctl but will fail because the
	// kernel side rescan removes and adds partitions and we have partitions
	// mounted (so it fails on removal). Use --no-reread to skip this attempt.
	cmd := exec.Command("sfdisk", "--append", "--no-reread", dl.Device)
	cmd.Stdin = buf
	if output, err := cmd.CombinedOutput(); err != nil {
		return created, osutil.OutputErr(output, err)
	}

	// Re-read the partition table using the BLKPG ioctl, which doesn't
	// remove existing partitions only appends new partitions with the right
	// size and offset. As long as we provide consistent partitioning from
	// userspace we're safe. At this point we also trigger udev to create
	// the new partition device nodes.
	output, err := exec.Command("partx", "-u", dl.Device).CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(output, err)
	}

	// Make sure the devices for the partitions we created are available
	if err := ensureNodesExist(created, 5*time.Second); err != nil {
		return nil, fmt.Errorf("partition not available: %v", err)
	}

	return created, nil
}

// ensureNodeExists makes sure the device nodes for all device structures are
// available and notified to udev, within a specified amount of time.
func ensureNodesExistImpl(ds []DeviceStructure, timeout time.Duration) error {
	t0 := time.Now()
	for _, part := range ds {
		found := false
		for time.Since(t0) < timeout {
			if osutil.FileExists(part.Node) {
				found = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if found {
			if err := udevTrigger(part.Node); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("device %s not available", part.Node)
		}
	}
	return nil
}

// deviceLayoutFromDump takes an sfdisk dump format and returns the partitioning
// information as a device layout.
func deviceLayoutFromDump(dump *sfdiskDeviceDump) (*DeviceLayout, error) {
	ptable := dump.PartitionTable

	if ptable.Unit != "sectors" {
		return nil, fmt.Errorf("cannot position partitions: unknown unit %q", ptable.Unit)
	}

	structure := make([]gadget.VolumeStructure, len(ptable.Partitions))
	ds := make([]DeviceStructure, len(ptable.Partitions))

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

		structure[i] = gadget.VolumeStructure{
			Name:       p.Name,
			Size:       gadget.Size(p.Size) * sectorSize,
			Label:      bd.Label,
			Type:       p.Type,
			Filesystem: bd.FSType,
		}
		ds[i] = DeviceStructure{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &structure[i],
				StartOffset:     gadget.Size(p.Start) * sectorSize,
				Index:           i + 1,
			},
			Node: p.Node,
		}
	}

	dl := &DeviceLayout{
		Structure:      ds,
		ID:             ptable.ID,
		Device:         ptable.Device,
		Schema:         ptable.Label,
		Size:           gadget.Size(ptable.LastLBA),
		SectorSize:     sectorSize,
		partitionTable: &ptable,
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

// buildPartitionList builds a list of partitions based on the current
// device contents and gadget structure list, in sfdisk dump
// format. Return a partitioning description suitable for sfdisk input
// and a list of the partitions to be created
func buildPartitionList(ptable *sfdiskPartitionTable, pv *gadget.LaidOutVolume) (sfdiskInput *bytes.Buffer, toBeCreated []DeviceStructure) {
	// Keep track what partitions we already have on disk
	seen := map[uint64]bool{}
	for _, p := range ptable.Partitions {
		seen[p.Start] = true
	}

	// Write new partition data in named-fields format
	buf := &bytes.Buffer{}
	for _, p := range pv.LaidOutStructure {
		s := p.VolumeStructure
		// Skip partitions that are already in the volume
		// Skip MBR structure
		if s.Type == "mbr" || s.Type == "bare" {
			continue
		}
		start := p.StartOffset / sectorSize
		if seen[uint64(start)] {
			continue
		}
		// Can we use the index here? Get the largest existing partition number and
		// build from there could be safer if the disk partitions are not consecutive
		// (can this actually happen in our images?)
		node := deviceName(ptable.Device, p.Index)
		fmt.Fprintf(buf, "%s : start=%12d, size=%12d, type=%s, name=%q\n", node, p.StartOffset/sectorSize,
			s.Size/sectorSize, partitionType(ptable.Label, p.Type), s.Name)

		// TODO:UC20: also add an attribute to mark partitions created at install
		//            time so they can be removed case the installation fails.

		// Set expected labels based on role
		switch s.Role {
		case gadget.SystemBoot:
			s.Label = ubuntuBootLabel
		case gadget.SystemSeed:
			s.Label = ubuntuSeedLabel
		case gadget.SystemData:
			s.Label = ubuntuDataLabel
		}

		toBeCreated = append(toBeCreated, DeviceStructure{p, node})
	}

	return buf, toBeCreated
}

// udevTrigger triggers udev for the specified device and waits until
// all events in the udev queue are handled.
func udevTrigger(device string) error {
	if output, err := exec.Command("udevadm", "trigger", "--settle", device).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
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
