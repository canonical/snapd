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

package install

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

var (
	ensureNodesExist = ensureNodesExistImpl
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

// createMissingPartitions creates the partitions listed in the laid out volume
// pv that are missing from the existing device layout, returning a list of
// structures that have been created.
func createMissingPartitions(dl *gadget.OnDiskVolume, pv *gadget.LaidOutVolume) ([]gadget.OnDiskStructure, error) {
	buf, created := buildPartitionList(dl, pv)
	if len(created) == 0 {
		return created, nil
	}

	logger.Debugf("create partitions on %s: %s", dl.Device, buf.String())

	// Write the partition table. By default sfdisk will try to re-read the
	// partition table with the BLKRRPART ioctl but will fail because the
	// kernel side rescan removes and adds partitions and we have partitions
	// mounted (so it fails on removal). Use --no-reread to skip this attempt.
	cmd := exec.Command("sfdisk", "--append", "--no-reread", dl.Device)
	cmd.Stdin = buf
	if output, err := cmd.CombinedOutput(); err != nil {
		return created, osutil.OutputErr(output, err)
	}

	// Re-read the partition table
	if err := reloadPartitionTable(dl.Device); err != nil {
		return nil, err
	}

	// Make sure the devices for the partitions we created are available
	if err := ensureNodesExist(created, 5*time.Second); err != nil {
		return nil, fmt.Errorf("partition not available: %v", err)
	}

	return created, nil
}

// buildPartitionList builds a list of partitions based on the current
// device contents and gadget structure list, in sfdisk dump format, and
// returns a partitioning description suitable for sfdisk input and a
// list of the partitions to be created.
func buildPartitionList(dl *gadget.OnDiskVolume, pv *gadget.LaidOutVolume) (sfdiskInput *bytes.Buffer, toBeCreated []gadget.OnDiskStructure) {
	sectorSize := dl.SectorSize

	// Keep track what partitions we already have on disk
	seen := map[quantity.Offset]bool{}
	for _, s := range dl.Structure {
		start := s.StartOffset / quantity.Offset(sectorSize)
		seen[start] = true
	}

	// Check if the last partition has a system-data role
	canExpandData := false
	if n := len(pv.LaidOutStructure); n > 0 {
		last := pv.LaidOutStructure[n-1]
		if last.VolumeStructure.Role == gadget.SystemData {
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
		start := p.StartOffset / quantity.Offset(sectorSize)
		if seen[start] {
			continue
		}

		// Only allow the creation of partitions with known GUIDs
		// TODO:UC20: also provide a mechanism for MBR (RPi)
		ptype := partitionType(dl.Schema, p.Type)
		if dl.Schema == "gpt" && !creationSupported(ptype) {
			logger.Noticef("cannot create partition with unsupported type %s", ptype)
			continue
		}

		// Check if the data partition should be expanded
		size := s.Size
		if s.Role == gadget.SystemData && canExpandData && quantity.Size(p.StartOffset)+s.Size < dl.Size {
			size = dl.Size - quantity.Size(p.StartOffset)
		}

		// Can we use the index here? Get the largest existing partition number and
		// build from there could be safer if the disk partitions are not consecutive
		// (can this actually happen in our images?)
		node := deviceName(dl.Device, pIndex)
		fmt.Fprintf(buf, "%s : start=%12d, size=%12d, type=%s, name=%q\n", node,
			p.StartOffset/quantity.Offset(sectorSize), size/sectorSize, ptype, s.Name)

		toBeCreated = append(toBeCreated, gadget.OnDiskStructure{
			LaidOutStructure: p,
			Node:             node,
			Size:             size,
		})
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

func deviceName(name string, index int) string {
	if len(name) > 0 {
		last := name[len(name)-1]
		if last >= '0' && last <= '9' {
			return fmt.Sprintf("%sp%d", name, index)
		}
	}
	return fmt.Sprintf("%s%d", name, index)
}

// removeCreatedPartitions removes partitions added during a previous install.
func removeCreatedPartitions(lv *gadget.LaidOutVolume, dl *gadget.OnDiskVolume) error {
	indexes := make([]string, 0, len(dl.Structure))
	for i, s := range dl.Structure {
		if wasCreatedDuringInstall(lv, s) {
			logger.Noticef("partition %s was created during previous install", s.Node)
			indexes = append(indexes, strconv.Itoa(i+1))
		}
	}
	if len(indexes) == 0 {
		return nil
	}

	// Delete disk partitions
	logger.Debugf("delete disk partitions %v", indexes)
	cmd := exec.Command("sfdisk", append([]string{"--no-reread", "--delete", dl.Device}, indexes...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}

	// Reload the partition table
	if err := reloadPartitionTable(dl.Device); err != nil {
		return err
	}

	// Re-read the partition table from the device to update our partition list
	if err := gadget.UpdatePartitionList(dl); err != nil {
		return err
	}

	// Ensure all created partitions were removed
	if remaining := createdDuringInstall(lv, dl); len(remaining) > 0 {
		return fmt.Errorf("cannot remove partitions: %s", strings.Join(remaining, ", "))
	}

	return nil
}

// ensureNodeExists makes sure the device nodes for all device structures are
// available and notified to udev, within a specified amount of time.
func ensureNodesExistImpl(dss []gadget.OnDiskStructure, timeout time.Duration) error {
	t0 := time.Now()
	for _, ds := range dss {
		found := false
		for time.Since(t0) < timeout {
			if osutil.FileExists(ds.Node) {
				found = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if found {
			if err := udevTrigger(ds.Node); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("device %s not available", ds.Node)
		}
	}
	return nil
}

// reloadPartitionTable instructs the kernel to re-read the partition
// table of a given block device.
func reloadPartitionTable(device string) error {
	// Re-read the partition table using the BLKPG ioctl, which doesn't
	// remove existing partitions, only appends new partitions with the right
	// size and offset. As long as we provide consistent partitioning from
	// userspace we're safe.
	output, err := exec.Command("partx", "-u", device).CombinedOutput()
	if err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

// udevTrigger triggers udev for the specified device and waits until
// all events in the udev queue are handled.
func udevTrigger(device string) error {
	if output, err := exec.Command("udevadm", "trigger", "--settle", device).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

// wasCreatedDuringInstall returns if the OnDiskStructure was created during
// install by referencing the gadget volume. A structure is only considered to
// be created during install if it is a role that is created during install and
// the start offsets match. We specifically don't look at anything on the
// structure such as filesystem information since this may be incomplete due to
// a failed installation, or due to the partial layout that is created by some
// ARM tools (i.e. ptool and fastboot) when flashing images to internal MMC.
func wasCreatedDuringInstall(lv *gadget.LaidOutVolume, s gadget.OnDiskStructure) bool {
	// for a structure to have been created during install, it must be one of
	// the system-boot, system-data, or system-save roles from the gadget, and
	// as such the on disk structure must exist in the exact same location as
	// the role from the gadget, so only return true if the provided structure
	// has the exact same StartOffset as one of those roles
	for _, gs := range lv.LaidOutStructure {
		// TODO: how to handle ubuntu-save here? maybe a higher level function
		//       should decide whether to delete it or not?
		switch gs.Role {
		case gadget.SystemSave, gadget.SystemData, gadget.SystemBoot:
			// then it was created during install or is to be created during
			// install, see if the offset matches the provided on disk structure
			// has
			if s.StartOffset == gs.StartOffset {
				return true
			}
		}
	}

	return false
}

// createdDuringInstall returns a list of partitions created during the
// install process.
func createdDuringInstall(lv *gadget.LaidOutVolume, layout *gadget.OnDiskVolume) (created []string) {
	created = make([]string, 0, len(layout.Structure))
	for _, s := range layout.Structure {
		if wasCreatedDuringInstall(lv, s) {
			created = append(created, s.Node)
		}
	}
	return created
}
