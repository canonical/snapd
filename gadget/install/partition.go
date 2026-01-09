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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
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
	// an error if other partition are missing.
	CreateAllMissingPartitions bool
}

// CreateMissingPartitions calls createMissingPartitions but returns only
// OnDiskStructure, as it is meant to be used externally (i.e. by
// muinstaller).
func CreateMissingPartitions(dv *gadget.OnDiskVolume, gv *gadget.Volume, opts *CreateOptions) ([]*gadget.OnDiskAndGadgetStructurePair, error) {
	dgpairs, err := createMissingPartitions(dv, gv, opts, nil)
	if err != nil {
		return nil, err
	}
	return dgpairs, nil
}

// createMissingPartitions creates the partitions listed in the gadget volume
// gv that are missing from the disk dv taking into account options opts. The
// map of gadget indexes to deleted partitions is needed because if they were
// removed, when creating we need to use the same size. This returns a list of
// structures that have been created.
func createMissingPartitions(dv *gadget.OnDiskVolume, gv *gadget.Volume, opts *CreateOptions, deletedOffsetSize map[int]StructOffsetSize) ([]*gadget.OnDiskAndGadgetStructurePair, error) {
	if opts == nil {
		opts = &CreateOptions{}
	}

	buf, created, err := buildPartitionList(dv, gv, opts, deletedOffsetSize)
	if err != nil {
		return nil, err
	}
	if len(created) == 0 {
		return created, nil
	}

	logger.Debugf("create partitions on %s: %s", dv.Device, buf.String())

	// Write the partition table. By default sfdisk will try to re-read the
	// partition table with the BLKRRPART ioctl but will fail because the
	// kernel side rescan removes and adds partitions and we have partitions
	// mounted (so it fails on removal). Use --no-reread to skip this attempt.
	cmd := exec.Command("sfdisk", "--append", "--no-reread", dv.Device)
	cmd.Stdin = buf
	if output, err := cmd.CombinedOutput(); err != nil {
		return created, osutil.OutputErr(output, err)
	}

	// Re-read the partition table
	// TODO this is not really working if, in a reinstall, we deleted
	// partitions of a different size of the ones we are creating (see
	// comment on why we are not doing this in buildPartitionList). In any
	// case, it seems that we have had problems in the past with partx and
	// maybe we should try something else (partprobe?).
	if err := reloadPartitionTable(opts.GadgetRootDir, dv.Device); err != nil {
		return nil, err
	}

	// run udevadm settle to wait for udev events that may have been triggered
	// by reloading the partition table to be processed, as we need the udev
	// database to be freshly updated
	if out, err := exec.Command("udevadm", "settle", "--timeout=180").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("cannot wait for udev to settle after reloading partition table: %v", osutil.OutputErr(out, err))
	}

	// Make sure the devices for the partitions we created are available
	nodes := []string{}
	for _, ls := range created {
		nodes = append(nodes, ls.DiskStructure.Node)
	}
	// do it in deterministic order
	sort.Strings(nodes)
	if err := ensureNodesExist(nodes, 5*time.Second); err != nil {
		return nil, fmt.Errorf("partition not available: %v", err)
	}

	return created, nil
}

// buildPartitionList builds a list of partitions based on the current device
// contents and gadget structure list, in sfdisk dump format, and returns a
// partitioning description suitable for sfdisk input and a list of the
// partitions to be created. To determine the size we need the gadget, volume
// and map of gadget indexes to just deleted partitions.
func buildPartitionList(dl *gadget.OnDiskVolume, vol *gadget.Volume, opts *CreateOptions, deletedOffsetSize map[int]StructOffsetSize) (sfdiskInput *bytes.Buffer, toBeCreated []*gadget.OnDiskAndGadgetStructurePair, err error) {
	if opts == nil {
		opts = &CreateOptions{}
	}
	sectorSize := uint64(dl.SectorSize)

	// For eMMC volumes, do not build any partitions
	if vol.Schema == "emmc" {
		return nil, nil, nil
	}

	// The partition / disk index - we find the current max number
	// currently on the disk and we start from there for the partitions we
	// create. This is necessary as some partitions might not be defined by
	// the gadget if we have a gadget with PartialStructure set. Note that
	// this condition is checked by EnsureVolumeCompatibility, which is
	// called before this function. muinstaller also checks for
	// PartialStructure before this is run.
	pIndex := 0
	for _, s := range dl.Structure {
		if s.DiskIndex > pIndex {
			pIndex = s.DiskIndex
		}
	}

	// Find out partitions already on the disk, if we don't want to create
	// all. If CreateAllMissingPartitions is set we are being called from
	// muinstaller and no partitions are expected on the disk.
	// TODO we should avoid using createMissingPartitions as ancillary
	// method from muinstaller to avoid this sort of situation, maybe by copying
	// the code around.
	matchedStructs := map[int]*gadget.OnDiskStructure{}
	if !opts.CreateAllMissingPartitions {
		// EnsureVolumeCompatibility will ignore missing partitions as
		// the AssumeCreatablePartitionsCreated option is false by default.
		if matchedStructs, err = gadget.EnsureVolumeCompatibility(vol, dl, nil); err != nil {
			return nil, nil, fmt.Errorf(
				"gadget and boot device %v partition table not compatible: %v",
				dl.Device, err)
		}
	}

	// Check if the last partition has a system-data role
	canExpandData := false
	if n := len(vol.Structure); n > 0 {
		last := vol.Structure[n-1]
		if last.Role == gadget.SystemData {
			canExpandData = true
		}
	}

	// Write new partition data in named-fields format
	buf := &bytes.Buffer{}
	lastEnd := quantity.Offset(0)
	toBeCreated = []*gadget.OnDiskAndGadgetStructurePair{}
	for _, vs := range vol.Structure {
		if !vs.IsPartition() {
			continue
		}
		// Skip partitions defined in the gadget that are already in the volume
		if ds, ok := matchedStructs[vs.YamlIndex]; ok {
			lastEnd = ds.StartOffset + quantity.Offset(ds.Size)
			continue
		}

		// We use the offset/size of removed partitions in reinstalls
		// instead of using "size" from the gadget. These data can be
		// different from the gadget size field when it is possible to
		// specify sizes in the [min-size, size] interval. With this
		// approach we make sure that:
		//
		// - There are no overlaps with non-deleted partitions after
		//   the deleted ones
		// - We do not end up with smaller than before data partition
		// - If using an installer, it might have decided on partition
		//   sizes and we would be overriding that decision
		//
		// If we decide a different approach in the future these points
		// will need to be considered.
		var offset quantity.Offset
		var size quantity.Size
		if prevOffsetSize, ok := deletedOffsetSize[vs.YamlIndex]; ok {
			offset = prevOffsetSize.StartOffset
			size = prevOffsetSize.Size
		} else {
			// Work out offset, might have not been set if min-size
			// is used (but note that we use the size value as we
			// are creating for the first time)
			if vs.Offset != nil {
				offset = *vs.Offset
			} else {
				offset = lastEnd
			}
			size = vs.Size
		}

		lastEnd = offset + quantity.Offset(size)

		pIndex++

		// Only allow creating certain partitions, namely the ubuntu-* roles
		if !opts.CreateAllMissingPartitions && !gadget.IsCreatableAtInstall(&vs) {
			return nil, nil, fmt.Errorf("cannot create partition #%d (%q)", vs.YamlIndex, vs.Name)
		}

		// Check if the data partition should be expanded
		startInSectors := uint64(offset) / sectorSize
		newSizeInSectors := uint64(size) / sectorSize
		if vs.Role == gadget.SystemData && canExpandData && startInSectors+newSizeInSectors < dl.UsableSectorsEnd {
			// note that if startInSectors + newSizeInSectors == dl.UsableSectorEnd
			// then we won't hit this branch, but it would be redundant anyways
			newSizeInSectors = dl.UsableSectorsEnd - startInSectors
		}

		ptype := partitionType(dl.Schema, vs.Type)

		// synthesize the node name and on disk structure
		node := deviceName(dl.Device, pIndex)

		// format sfdisk input for creating this partition
		fmt.Fprintf(buf, "%s : start=%12d, size=%12d, type=%s, name=%q\n", node,
			startInSectors, newSizeInSectors, ptype, vs.Name)

		diskSt := &gadget.OnDiskStructure{
			Name:             vs.Name,
			PartitionFSLabel: vs.Label,
			Type:             vs.Type,
			PartitionFSType:  vs.LinuxFilesystem(),
			StartOffset:      offset,
			Node:             node,
			DiskIndex:        pIndex,
			Size:             quantity.Size(newSizeInSectors * sectorSize),
		}
		// Make per-iter pointer (vs is per-loop)
		newVs := vs
		toBeCreated = append(toBeCreated,
			&gadget.OnDiskAndGadgetStructurePair{
				DiskStructure: diskSt, GadgetStructure: &newVs})
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

// StructOffsetSize contains current offset and size of a partition that we are
// about to delete.
type StructOffsetSize struct {
	StartOffset quantity.Offset
	Size        quantity.Size
}

// removeCreatedPartitions removes partitions added during a previous install.
// For this it matches partitions from the gadget to the partitions currently
// existing in the volume. It needs gadgetRoot to find some configuration
// options. It returns a map of gadget.yaml indexes to information about the
// removed partitions.
func removeCreatedPartitions(gadgetRoot string, gv *gadget.Volume, dl *gadget.OnDiskVolume) (map[int]StructOffsetSize, error) {
	sfdiskIndexes := make([]string, 0, len(dl.Structure))
	// up to 3 possible partitions are creatable and thus removable:
	// ubuntu-data, ubuntu-boot, and ubuntu-save
	deletedIndexes := make(map[int]bool, 3)
	deletedOffsetSize := make(map[int]StructOffsetSize, 3)
	startFromIdx := 0
	for i, s := range dl.Structure {
		gadgetIndexes := indexIfCreatedDuringInstall(gv, s, startFromIdx)
		if gadgetIndexes.YamlIdx >= 0 {
			startFromIdx = gadgetIndexes.OrderIdx + 1
			logger.Noticef("partition %s was created during previous install", s.Node)
			sfdiskIndexes = append(sfdiskIndexes, strconv.Itoa(i+1))
			deletedOffsetSize[gadgetIndexes.YamlIdx] = StructOffsetSize{
				StartOffset: s.StartOffset,
				Size:        s.Size,
			}
			deletedIndexes[i] = true
		}
	}
	if len(sfdiskIndexes) == 0 {
		return deletedOffsetSize, nil
	}

	// Delete disk partitions
	logger.Debugf("delete disk partitions %v", sfdiskIndexes)
	cmd := exec.Command("sfdisk", append([]string{"--no-reread", "--delete", dl.Device}, sfdiskIndexes...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, osutil.OutputErr(output, err)
	}

	// Reload the partition table - note that this specifically does not trigger
	// udev events to remove the deleted devices, see the doc-comment below
	if err := reloadPartitionTable(gadgetRoot, dl.Device); err != nil {
		return nil, err
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

	return deletedOffsetSize, nil
}

// ensureNodesExistImpl makes sure that the specified device nodes are available
// and notified to udev, within a specified amount of time.
func ensureNodesExistImpl(nodes []string, timeout time.Duration) error {
	t0 := time.Now()
	for _, node := range nodes {
		found := false
		for time.Since(t0) < timeout {
			if osutil.FileExists(node) {
				found = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if found {
			if err := udevTrigger(node); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("device %s not available", node)
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
	f, err := os.OpenFile(rescanFile, os.O_WRONLY, 0o644)
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

// GadgetVolumeIdexes has indexes for volumes defined in gadget.yaml.
type GadgetVolumeIdexes struct {
	// YamlIdx is the index in the yaml file
	YamlIdx int
	// OrderIdx is the index after ordering (offsets in gadget.yaml make it
	// possible that this is different to YamlIdx)
	OrderIdx int
}

// indexIfCreatedDuringInstall returns the gadget indexes (yaml and order ones)
// for a gadget structure if the OnDiskStructure was created during install by
// referencing the gadget volume, -1 for both indexes otherwise. The OrderIdx
// can be used by the caller to avoid duplicated matches, as gadget partition
// definitions can have ranges for the start offset if using min-size. For
// this, the minimum expected order index startFromIdx can be passed to the
// function, and it should be the last matched order index plus 1.
//
// A structure is only considered to be created during install if it is a role
// that is created during install and the start offsets match. We specifically
// don't look at anything on the structure such as filesystem information since
// this may be incomplete due to a failed installation, or due to the partial
// layout that is created by some ARM tools (i.e. ptool and fastboot) when
// flashing images to internal MMC.
func indexIfCreatedDuringInstall(gv *gadget.Volume, s gadget.OnDiskStructure, startFromIdx int) GadgetVolumeIdexes {
	// For a structure to have been created during install, it must be one
	// of the system-boot, system-data, or system-save roles from the
	// gadget, and as such the on disk structure must exist in the exact
	// same location as the role from the gadget, so only return true if
	// the provided structure has the exact same StartOffset as one of
	// those roles. We start from the first partition not assigned
	// previously. This is relevant as multiple partitions can match a
	// given gadget structure, if having big valid intervals when using
	// min-size.
	for i := startFromIdx; i < len(gv.Structure); i++ {
		// TODO: how to handle ubuntu-save here? maybe a higher level function
		//       should decide whether to delete it or not?
		switch gv.Structure[i].Role {
		case gadget.SystemSave, gadget.SystemData, gadget.SystemBoot:
			// then it was created during install or is to be created during
			// install, see if the offset matches the provided on disk structure
			// has
			if gadget.CheckValidStartOffset(s.StartOffset, gv.Structure, i) == nil {
				return GadgetVolumeIdexes{YamlIdx: gv.Structure[i].YamlIndex, OrderIdx: i}
			}
		}
	}

	return GadgetVolumeIdexes{YamlIdx: -1, OrderIdx: -1}
}
