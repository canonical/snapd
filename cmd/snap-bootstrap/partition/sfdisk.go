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
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

const (
	ubuntuBootLabel = "ubuntu-boot"
	ubuntuSeedLabel = "ubuntu-seed"
	ubuntuDataLabel = "ubuntu-data"

	sectorSize gadget.Size = 512

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

type DeviceLayout struct {
	Structure []DeviceStructure
	ID        string
	Device    string
	Schema    string
	// size in bytes
	Size gadget.Size
	// sector size in bytes
	SectorSize     gadget.Size
	partitionTable *sfdiskPartitionTable
}

type DeviceStructure struct {
	gadget.LaidOutStructure

	Node                 string
	CreatedDuringInstall bool
}

// DeviceLayoutFromDisk obtains the partitioning and filesystem information from
// the block device.
func DeviceLayoutFromDisk(device string) (*DeviceLayout, error) {
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

var (
	ensureNodesExist = ensureNodesExistImpl
)

// CreateMissing creates the partitions listed in the positioned volume pv
// that are missing from the existing device layout.
func (dl *DeviceLayout) CreateMissing(pv *gadget.LaidOutVolume) ([]DeviceStructure, error) {
	buf, created := buildPartitionList(dl, pv)
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

// RemoveCreated removes partitions added during a previous failed install
// attempt.
func (dl *DeviceLayout) RemoveCreated() error {
	indexes := make([]string, 0, len(dl.partitionTable.Partitions))
	for i, s := range dl.Structure {
		if s.CreatedDuringInstall {
			logger.Noticef("partition %s was created during previous install", s.Node)
			indexes = append(indexes, strconv.Itoa(i+1))
		}
	}
	if len(indexes) == 0 {
		return nil
	}

	// Delete disk partitions
	cmd := exec.Command("sfdisk", append([]string{"--no-reread", "--delete", dl.Device}, indexes...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}

	// Reload the partition table
	if err := reloadPartitionTable(dl.Device); err != nil {
		return err
	}

	// Re-read the partition table from the device to update our partition list
	layout, err := DeviceLayoutFromDisk(dl.Device)
	if err != nil {
		return fmt.Errorf("cannot read disk layout: %v", err)
	}
	if dl.ID != layout.ID {
		return fmt.Errorf("partition table IDs don't match")
	}
	dl.Structure = layout.Structure
	dl.partitionTable = layout.partitionTable

	// Ensure all created partitions were removed
	if remaining := listCreatedPartitions(layout); len(remaining) > 0 {
		return fmt.Errorf("cannot remove partitions: %s", strings.Join(remaining, ", "))
	}

	return nil
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

func blockDeviceSizeInSectors(devpath string) (gadget.Size, error) {
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
	return gadget.Size(sz), nil
}

// deviceLayoutFromPartitionTable takes an sfdisk dump partition table and returns
// the partitioning information as a device layout.
func deviceLayoutFromPartitionTable(ptable sfdiskPartitionTable) (*DeviceLayout, error) {
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

		vsType, err := fromSfdiskPartitionType(p.Type, ptable.Label)
		if err != nil {
			return nil, fmt.Errorf("cannot convert sfdisk partition type %q: %v", p.Type, err)
		}

		structure[i] = gadget.VolumeStructure{
			Name:       p.Name,
			Size:       gadget.Size(p.Size) * sectorSize,
			Label:      bd.Label,
			Type:       vsType,
			Filesystem: bd.FSType,
		}

		ds[i] = DeviceStructure{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &structure[i],
				StartOffset:     gadget.Size(p.Start) * sectorSize,
				Index:           i + 1,
			},
			Node:                 p.Node,
			CreatedDuringInstall: isCreatedDuringInstall(&p, &bd, ptable.Label),
		}
	}

	var numSectors gadget.Size
	if ptable.LastLBA != 0 {
		// sfdisk reports the last usable LBA for GPT disks only
		numSectors = gadget.Size(ptable.LastLBA + 1)
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

	dl := &DeviceLayout{
		Structure:      ds,
		ID:             ptable.ID,
		Device:         ptable.Device,
		Schema:         ptable.Label,
		Size:           numSectors * sectorSize,
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
// device contents and gadget structure list, in sfdisk dump format, and
// returns a partitioning description suitable for sfdisk input and a
// list of the partitions to be created.
func buildPartitionList(dl *DeviceLayout, pv *gadget.LaidOutVolume) (sfdiskInput *bytes.Buffer, toBeCreated []DeviceStructure) {
	ptable := dl.partitionTable

	// Keep track what partitions we already have on disk
	seen := map[uint64]bool{}
	for _, p := range ptable.Partitions {
		seen[p.Start] = true
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
		if s.Role == gadget.SystemData && canExpandData && p.StartOffset+s.Size < dl.Size {
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
		case gadget.SystemBoot:
			s.Label = ubuntuBootLabel
		case gadget.SystemSeed:
			s.Label = ubuntuSeedLabel
		case gadget.SystemData:
			s.Label = ubuntuDataLabel
		}

		toBeCreated = append(toBeCreated, DeviceStructure{
			LaidOutStructure:     p,
			Node:                 node,
			CreatedDuringInstall: true,
		})
	}

	return buf, toBeCreated
}

// listCreatedPartitions returns a list of partitions created during the
// install process.
func listCreatedPartitions(layout *DeviceLayout) []string {
	created := make([]string, 0, len(layout.Structure))
	for _, s := range layout.Structure {
		if s.CreatedDuringInstall {
			created = append(created, s.Node)
		}
	}
	return created
}

// udevTrigger triggers udev for the specified device and waits until
// all events in the udev queue are handled.
func udevTrigger(device string) error {
	if output, err := exec.Command("udevadm", "trigger", "--settle", device).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

// reloadPartitionTable instructs the kernel to re-read the partition
// table of a given block device.
func reloadPartitionTable(device string) error {
	// Re-read the partition table using the BLKPG ioctl, which doesn't
	// remove existing partitions, only appends new partitions with the right
	// size and offset. As long as we provide consistent partitioning from
	// userspace we're safe. At this point we also trigger udev to create
	// the new partition device nodes.
	output, err := exec.Command("partx", "-u", device).CombinedOutput()
	if err != nil {
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
