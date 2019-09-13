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
package volmgr

import (
	"fmt"
	"os/exec"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
)

// PartitionTool is used to query and manipulate the device partition
// table.
type partitionTool interface {
	deviceInfo() (*gadget.LaidOutVolume, error)
	createPartitions(*gadget.LaidOutVolume, []bool, map[string]string) error
}

func newPartitionTool(device string) partitionTool {
	return newSFDisk(device)
}

// VolumeManager holds information about the gadget and a disk to check
// partitioning information and create missing partitions.
type VolumeManager struct {
	gadgetRoot       string
	device           string
	partitionTool    partitionTool
	info             *gadget.Info
	positionedVolume map[string]*gadget.LaidOutVolume
	deviceVolume     *gadget.LaidOutVolume
}

func NewVolumeManager(gadgetRoot, device string) (*VolumeManager, error) {
	info, err := gadget.ReadInfo(gadgetRoot, false)
	if err != nil {
		return nil, err
	}

	constraints := gadget.LayoutConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		SectorSize:        512,
	}

	positionedVolume := map[string]*gadget.LaidOutVolume{}

	for name, vol := range info.Volumes {
		pvol, err := gadget.LayoutVolume(gadgetRoot, &vol, constraints)
		if err != nil {
			return nil, err
		}
		positionedVolume[name] = pvol
	}

	v := &VolumeManager{
		gadgetRoot:       gadgetRoot,
		device:           device,
		partitionTool:    newPartitionTool(device),
		info:             info,
		positionedVolume: positionedVolume,
	}

	return v, nil
}

func (v *VolumeManager) Run() error {

	if err := v.readDevice(); err != nil {
		return err
	}

	if err := v.completeLayout(); err != nil {
		return err
	}

	return nil
}

// readDevice reads the partition layout from the block device.
func (v *VolumeManager) readDevice() error {

	var err error
	if v.deviceVolume, err = v.partitionTool.deviceInfo(); err != nil {
		return err
	}

	return nil
}

// completeLayout adds missing structures from the gadget specification.
func (v *VolumeManager) completeLayout() error {

	// Limit ourselves to just one volume for now.
	if len(v.positionedVolume) != 1 {
		return fmt.Errorf("multiple volumes are not supported")
	}
	var name string
	for k := range v.positionedVolume {
		name = k
	}
	gadgetVolume := v.positionedVolume[name]

	// Check if all existing device partitions are also in gadget
	usedPartitions := make([]bool, len(gadgetVolume.LaidOutStructure))
	for _, ps := range v.deviceVolume.LaidOutStructure {
		index, err := findStructureInVolume(&ps, gadgetVolume)
		if err != nil {
			return err
		}
		if index >= len(usedPartitions) {
			return fmt.Errorf("gadget structure indexes are inconsistent")
		}
		usedPartitions[index] = true
	}

	// Map device nodes to structure names
	deviceMap := map[string]string{}

	// Create missing partitions
	if err := v.partitionTool.createPartitions(gadgetVolume, usedPartitions, deviceMap); err != nil {
		return err
	}

	// Make filesystems on the newly created partitions
	for i, p := range gadgetVolume.LaidOutStructure {
		s := p.VolumeStructure
		// Skip partitions that are already in the volume
		if usedPartitions[i] {
			continue
		}
		// Skip MBR structure
		if s.Type == "mbr" {
			continue
		}

		node, ok := deviceMap[s.Name]
		if !ok {
			continue
		}
		if err := makeFilesystem(node, s.Label, s.Filesystem); err != nil {
			return err
		}
	}

	return nil
}

func makeFilesystem(node, label, filesystem string) error {
	switch filesystem {
	case "vfat":
		return makeVFATFilesystem(node, label)
	default:
		return fmt.Errorf("cannot create unsupported filesystem %q", filesystem)
	}
}

func makeVFATFilesystem(node, label string) error {
	output, err := exec.Command("mkfs.vfat", "-n", label, node).CombinedOutput()
	if err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

func findStructureInVolume(needle *gadget.LaidOutStructure, haystack *gadget.LaidOutVolume) (int, error) {
	found := -1
	n := needle.VolumeStructure

	// FIXME: when run-time partitions are explicitly marked in gadget, check
	//        if we should use that information instead
	for _, ps := range haystack.LaidOutStructure {
		h := ps.VolumeStructure
		if h.Name != n.Name {
			continue
		}
		if h.Label != n.Label {
			continue
		}
		if ps.StartOffset != needle.StartOffset {
			continue
		}
		if h.Size != n.Size {
			continue
		}
		if h.Filesystem != n.Filesystem {
			continue
		}
		found = ps.Index

		fmt.Printf("structure %q found in gadget definition (index %d)\n", h.Name, ps.Index)
	}

	var err error
	if found < 0 {
		err = fmt.Errorf("cannot find structure %q (partition %v) in gadget", n.Name, needle)
	}

	return found, err
}
