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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/strutil"
)

var (
	ensureNodesExist = ensureNodesExistImpl
)

// reloadPartitionTable reloads the partition table depending on what the gadget
// says - if the gadget has a special marker file, then we will use a special
// reload mechanism, implemented for a specific device.
func reloadPartitionTable(gadgetRoot string, device string) error {
	if osutil.FileExists(filepath.Join(gadgetRoot, "meta", "force-partition-table-reload-via-device-rescan")) {
		// TODO: remove this method when we are able to, this exists for a very
		// specific device + kernel combination which is not compatible with
		// using partx and so instead we must use this rescan trick
		return reloadPartitionTableWithDeviceRescan(device)
	} else {
		// use partx like normal
		return reloadPartitionTableWithPartx(device)
	}
}

type CreateOptions struct {
	// The gadget root dir
	GadgetRootDir string

	// Create all missing partitions. If unset only
	// role-{data,boot,save} partitions will get created and it's
	// an error other partition is missing.
	CreateAllMissingPartitions bool
}

// createMissingPartitions creates the partitions listed in the laid out volume
// pv that are missing from the existing device layout, returning a list of
// structures that have been created.
func CreateMissingPartitions(dl *gadget.OnDiskVolume, pv *gadget.LaidOutVolume, opts *CreateOptions) ([]gadget.OnDiskStructure, error) {
	if opts == nil {
		opts = &CreateOptions{}
	}

	buf, created, err := buildPartitionList(dl, pv, opts)
	if err != nil {
		return nil, err
	}
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
	if err := reloadPartitionTable(opts.GadgetRootDir, dl.Device); err != nil {
		return nil, err
	}

	// run udevadm settle to wait for udev events that may have been triggered
	// by reloading the partition table to be processed, as we need the udev
	// database to be freshly updated
	if out, err := exec.Command("udevadm", "settle", "--timeout=180").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("cannot wait for udev to settle after reloading partition table: %v", osutil.OutputErr(out, err))
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
func buildPartitionList(dl *gadget.OnDiskVolume, pv *gadget.LaidOutVolume, opts *CreateOptions) (sfdiskInput *bytes.Buffer, toBeCreated []gadget.OnDiskStructure, err error) {
	if opts == nil {
		opts = &CreateOptions{}
	}
	sectorSize := uint64(dl.SectorSize)

	// Keep track what partitions we already have on disk - the keys to this map
	// is the starting sector of the structure we have seen.
	// TODO: use quantity.SectorOffset or similar when that is available

	seen := map[uint64]bool{}
	for _, s := range dl.Structure {
		start := uint64(s.StartOffset) / sectorSize
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

	// The partition / disk index - note that it will start at 1, we increment
	// it before we use it in the loop below
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
		startInSectors := uint64(p.StartOffset) / sectorSize
		if seen[startInSectors] {
			continue
		}

		// Only allow creating certain partitions, namely the ubuntu-* roles
		if !opts.CreateAllMissingPartitions && !gadget.IsCreatableAtInstall(p.VolumeStructure) {
			return nil, nil, fmt.Errorf("cannot create partition %s", p)
		}

		// Check if the data partition should be expanded
		newSizeInSectors := uint64(s.Size) / sectorSize
		if s.Role == gadget.SystemData && canExpandData && startInSectors+newSizeInSectors < dl.UsableSectorsEnd {
			// note that if startInSectors + newSizeInSectors == dl.UsableSectorEnd
			// then we won't hit this branch, but it would be redundant anyways
			newSizeInSectors = dl.UsableSectorsEnd - startInSectors
		}

		ptype := partitionType(dl.Schema, p.Type)

		// synthesize the node name and on disk structure
		node := deviceName(dl.Device, pIndex)
		ps := gadget.OnDiskStructure{
			LaidOutStructure: p,
			Node:             node,
			DiskIndex:        pIndex,
			Size:             quantity.Size(newSizeInSectors * sectorSize),
		}

		// format sfdisk input for creating this partition
		fmt.Fprintf(buf, "%s : start=%12d, size=%12d, type=%s, name=%q\n", node,
			startInSectors, newSizeInSectors, ptype, s.Name)

		toBeCreated = append(toBeCreated, ps)
	}

	return buf, toBeCreated, nil
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
func removeCreatedPartitions(gadgetRoot string, lv *gadget.LaidOutVolume, dl *gadget.OnDiskVolume) error {
	sfdiskIndexes := make([]string, 0, len(dl.Structure))
	// up to 3 possible partitions are creatable and thus removable:
	// ubuntu-data, ubuntu-boot, and ubuntu-save
	deletedIndexes := make(map[int]bool, 3)
	for i, s := range dl.Structure {
		if wasCreatedDuringInstall(lv, s) {
			logger.Noticef("partition %s was created during previous install", s.Node)
			sfdiskIndexes = append(sfdiskIndexes, strconv.Itoa(i+1))
			deletedIndexes[i] = true
		}
	}
	if len(sfdiskIndexes) == 0 {
		return nil
	}

	// Delete disk partitions
	logger.Debugf("delete disk partitions %v", sfdiskIndexes)
	cmd := exec.Command("sfdisk", append([]string{"--no-reread", "--delete", dl.Device}, sfdiskIndexes...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}

	// Reload the partition table - note that this specifically does not trigger
	// udev events to remove the deleted devices, see the doc-comment below
	if err := reloadPartitionTable(gadgetRoot, dl.Device); err != nil {
		return err
	}

	// Remove the partitions we deleted from the OnDiskVolume - note that we
	// specifically don't try to just re-build the OnDiskVolume since doing
	// so correctly requires using only information from the partition table
	// we just updated with sfdisk (since we used --no-reread above, and we can't
	// really tell the kernel to re-read the partition table without hitting
	// EBUSY as the disk is still mounted even though the deleted partitions
	// were deleted), but to do so would essentially just be testing that sfdisk
	// updated the partition table in a way we expect. The partition parsing
	// code we use to build the OnDiskVolume also must not be reliant on using
	// sfdisk (since it has to work in the initrd where we don't have sfdisk),
	// so either that code would just be a duplication of what sfdisk is doing
	// or that code would fail to update the deleted partitions anyways since
	// at this point the only thing that knows about the deleted partitions is
	// the physical partition table on the disk.
	newStructure := make([]gadget.OnDiskStructure, 0, len(dl.Structure)-len(deletedIndexes))
	for i, structure := range dl.Structure {
		if !deletedIndexes[i] {
			newStructure = append(newStructure, structure)
		}
	}

	dl.Structure = newStructure

	// Ensure all created partitions were removed
	if remaining := createdDuringInstall(lv, dl); len(remaining) > 0 {
		return fmt.Errorf("cannot remove partitions: %s", strings.Join(remaining, ", "))
	}

	return nil
}

func partitionsWithRolesAndContent(lv *gadget.LaidOutVolume, dl *gadget.OnDiskVolume, roles []string) []gadget.OnDiskStructure {
	roleForOffset := map[quantity.Offset]*gadget.LaidOutStructure{}
	for idx, gs := range lv.LaidOutStructure {
		if gs.Role != "" {
			roleForOffset[gs.StartOffset] = &lv.LaidOutStructure[idx]
		}
	}

	var parts []gadget.OnDiskStructure
	for _, part := range dl.Structure {
		gs := roleForOffset[part.StartOffset]
		if gs == nil || gs.Role == "" || !strutil.ListContains(roles, gs.Role) {
			continue
		}
		// now that we have a match, override the laid out structure
		// such that the fields reflect what was declared in the gadget,
		// the on-disk-structure already has the right size as read from
		// the partition table
		part.LaidOutStructure = *gs
		parts = append(parts, part)
	}
	return parts
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

// reloadPartitionTableWithDeviceRescan instructs the kernel to re-read the
// partition table of a given block device via a workaround proposed for a
// specific device in the form of executing the equivalent of:
// bash -c "echo 1 > /sys/block/sd?/device/rescan"
func reloadPartitionTableWithDeviceRescan(device string) error {
	disk, err := disks.DiskFromDeviceName(device)
	if err != nil {
		return err
	}

	rescanFile := filepath.Join(disk.KernelDevicePath(), "device", "rescan")

	logger.Noticef("reload partition table via rescan file %s for device %s as indicated by gadget", rescanFile, device)
	f, err := os.OpenFile(rescanFile, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// this could potentially fail with strange sysfs errno's since rescan isn't
	// a real file
	if _, err := f.WriteString("1\n"); err != nil {
		return fmt.Errorf("unable to trigger reload with rescan file: %v", err)
	}

	return nil
}

// reloadPartitionTableWithPartx instructs the kernel to re-read the partition
// table of a given block device.
func reloadPartitionTableWithPartx(device string) error {
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
