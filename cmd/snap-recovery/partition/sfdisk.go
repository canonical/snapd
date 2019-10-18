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

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
)

var (
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

type SFDisk struct {
	device         string
	partitionTable *sfdiskPartitionTable
}

func NewSFDisk(device string) *SFDisk {
	return &SFDisk{
		device: device,
	}
}

// Layout obtains the partitioning and filesystem information from the block
// device and expresses it as a laid out volume.
func (sf *SFDisk) Layout() (*gadget.LaidOutVolume, error) {
	output, err := exec.Command("sfdisk", "--json", "-d", sf.device).CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(output, err)
	}

	var dump sfdiskDeviceDump
	if err := json.Unmarshal(output, &dump); err != nil {
		return nil, fmt.Errorf("cannot parse sfdisk output: %v", err)
	}

	pv, err := positionedVolumeFromDump(&dump)
	if err != nil {
		return nil, err
	}

	// Hold the partition table for later use when creating partitions
	sf.partitionTable = &dump.PartitionTable

	return pv, nil
}

// Create creates the partitions listed in positionedVolume
func (sf *SFDisk) Create(pv *gadget.LaidOutVolume) (map[string]gadget.LaidOutStructure, error) {
	// Layout() will update sf.partitionTable
	if _, err := sf.Layout(); err != nil {
		return nil, err
	}
	buf, created := buildPartitionList(sf.partitionTable, pv)

	// Write the partition table, note that sfdisk will re-read the
	// partition table by itself: see disk-utils/sfdisk.c:write_changes()
	cmd := exec.Command("sfdisk", sf.device)
	cmd.Stdin = buf
	if output, err := cmd.CombinedOutput(); err != nil {
		return created, osutil.OutputErr(output, err)
	}

	return created, nil
}

// positionedVolumeFromDump takes an sfdisk dump format and returns the partitioning
// information as a laid out volume.
func positionedVolumeFromDump(dump *sfdiskDeviceDump) (*gadget.LaidOutVolume, error) {
	ptable := dump.PartitionTable

	if ptable.Unit != "sectors" {
		return nil, fmt.Errorf("cannot position partitions: unknown unit %q", ptable.Unit)
	}

	structure := make([]gadget.VolumeStructure, len(ptable.Partitions))
	ps := make([]gadget.LaidOutStructure, len(ptable.Partitions))

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
		ps[i] = gadget.LaidOutStructure{
			VolumeStructure: &structure[i],
			StartOffset:     gadget.Size(p.Start) * sectorSize,
			Index:           i + 1,
		}
	}

	pv := &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			ID:        ptable.ID,
			Structure: structure,
		},
		Size:             gadget.Size(ptable.LastLBA),
		SectorSize:       sectorSize,
		LaidOutStructure: ps,
	}

	return pv, nil
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
func buildPartitionList(ptable *sfdiskPartitionTable, pv *gadget.LaidOutVolume) (sfdiskInput *bytes.Buffer, toBeCreated map[string]gadget.LaidOutStructure) {
	buf := &bytes.Buffer{}
	toBeCreated = make(map[string]gadget.LaidOutStructure, len(pv.LaidOutStructure))

	// Write partition data in sfdisk dump format
	fmt.Fprintf(buf, "label: %s\nlabel-id: %s\ndevice: %s\nunit: %s\nfirst-lba: %d\nlast-lba: %d\n\n",
		ptable.Label, ptable.ID, ptable.Device, ptable.Unit, ptable.FirstLBA, ptable.LastLBA)

	// Keep track what partitions we already have on disk
	seen := map[uint64]bool{}
	for _, p := range ptable.Partitions {
		fmt.Fprintf(buf, "%s : start=%12d, size=%12d, type=%s, uuid=%s", p.Node, p.Start,
			p.Size, p.Type, p.UUID)
		if p.Name != "" {
			fmt.Fprintf(buf, ", name=%q", p.Name)
		}
		fmt.Fprintf(buf, "\n")
		seen[p.Start] = true
	}

	// Add missing partitions
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

		// Are roles unique so we can use it to map nodes? Should we use labels instead?
		toBeCreated[node] = p
	}

	return buf, toBeCreated
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
