// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/cmd/snap-bootstrap/blkid"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/secboot"
)

var bootFindPartitionUUIDForBootedKernelDisk = boot.FindPartitionUUIDForBootedKernelDisk

// Filesystem is the information reported by blkid on a filesystem.
type Filesystem struct {
	// FilesystemType is the type of the filesystem (vfat, ext4, etc.)
	FilesystemType string
	// FilesystemLabel is the label of the filesystem of the partition.
	FilesystemLabel string
	// FilesystemUUID is the filesystem UUID.
	FilesystemUUID string
}

// Partition contains information about a partition detected in the system.
type Partition struct {
	// Partition number
	Number int
	// Path in /dev
	Node string
	// Partition label, only for GPT
	Name string
	// Partition UUID, only for GPT
	UUID string
	// Filesystem info if there is one in this partition.
	Filesystem
}

// Implemention of interface needed by secboot.
var _ = secboot.Partition(&Partition{})

func (p *Partition) PartitionNode() string {
	return p.Node
}

func (p *Partition) PartitionLabel() string {
	return p.Name
}

func (p *Partition) PartitionUUID() string {
	return p.UUID
}

func (p *Partition) FilesystemUUID() string {
	return p.Filesystem.FilesystemUUID
}

// Disk contains information about a disk detected in the system.
type Disk struct {
	// Path in /dev
	Node string
	// Partition information read from disk
	Parts []*Partition
}

// SecbootDisk implements secboot.Disk - we use a different type to Disk to
// avoid a name conflict in the PartitionWithFsLabel method.
type SecbootDisk struct {
	Disk *Disk
}

// Implemention of interface needed by secboot.
var _ = secboot.Disk(&SecbootDisk{})

func (d *SecbootDisk) PartitionWithFsLabel(label string) (secboot.Partition, error) {
	return d.Disk.PartitionWithFsLabel(label)
}

var osutilDeviceMajorAndMinor = osutil.DeviceMajorAndMinor

func (d *SecbootDisk) DiskModel() string {
	// Get file information for the device node
	major, minor, err := osutilDeviceMajorAndMinor(d.Disk.Node)
	if err != nil {
		logger.Noticef("failed to find major/minor for %s: %s", d.Disk.Node, err)
		return "unknown"
	}

	// The model can be found in /sys/dev/block/<major>:<minor>/device/model
	sysfsPath := fmt.Sprintf(filepath.Join(dirs.GlobalRootDir, "/sys/dev/block/%d:%d/device/model"),
		major, minor)

	// Read the model file
	modelBytes, err := os.ReadFile(sysfsPath)
	if err != nil {
		// In some cases there is no file
		logger.Debugf("failed to read model from sysfs: %s", err)
		return "unknown"
	}
	trimmed := strings.TrimSpace(string(modelBytes))
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

// PartitionWithFsLabel returns a partition with a filesystem matching fsLabel.
func (d *Disk) PartitionWithFsLabel(fsLabel string) (*Partition, error) {
	return d.matchingPartition(func(p *Partition) bool {
		// We are case-insensitive for vfat
		if p.FilesystemType == "vfat" {
			return strings.EqualFold(p.FilesystemLabel, fsLabel)
		}
		return p.FilesystemLabel == fsLabel
	})
}

// PartitionWithUUID returns a partition with matching partuuid.
func (d *Disk) PartitionWithUUID(partuuid string) (*Partition, error) {
	return d.matchingPartition(func(p *Partition) bool { return p.UUID == partuuid })
}

// matchingPartition returns a partition in disk that matches the criteria of
// the passed match callback.
func (d *Disk) matchingPartition(match func(*Partition) bool) (*Partition, error) {
	for _, p := range d.Parts {
		if match(p) {
			return p, nil
		}
	}

	return nil, fmt.Errorf("%s: partition not found", d.Node)
}

func isGpt(probe blkid.AbstractBlkidProbe) bool {
	pttype, err := probe.LookupValue("PTTYPE")
	if err != nil {
		return false
	}
	return pttype == "gpt"
}

// probeFilesystemInfo probes a filesystem to obtain the filesystem label.
// <start> and <size> must be byte offsets, not sector counts.
func probeFilesystemInfo(node string, part int, start, size int64) (*Filesystem, error) {
	probe, err := blkid.NewProbeFromRange(node, start, size)
	if err != nil {
		return nil, err
	}
	defer probe.Close()

	probe.EnableSuperblocks(true)
	probe.SetSuperblockFlags(blkid.BLKID_SUBLKS_TYPE |
		blkid.BLKID_SUBLKS_LABEL |
		blkid.BLKID_SUBLKS_UUID)

	if err := probe.DoSafeprobe(); err != nil {
		return nil, err
	}

	typ, err := probe.LookupValue("TYPE")
	if err != nil {
		logger.Noticef("WARNING: no filesystem type on partition %d of %s: %s", part, node, err)
	}
	label, err := probe.LookupValue("LABEL")
	if err != nil {
		// This can happen for instance on the pi (mbr), where during the installation of a preseeded
		// image, it can trigger udev, which retriggers snap-bootstrap scan-disk where in the non-gpt
		// case we try to probe the filesystem too early, before it's formatted (so no LABEL).
		logger.Noticef("WARNING: no filesystem label on partition %d of %s: %s", part, node, err)
	}
	fsUUID, err := probe.LookupValue("UUID")
	if err != nil {
		logger.Noticef("WARNING: no filesystem UUID on partition %d of %s: %s", part, node, err)
	}
	// It is ok if we did not find useful information here
	return &Filesystem{FilesystemType: typ, FilesystemLabel: label, FilesystemUUID: fsUUID}, nil
}

// Options for probeDisk
type probeDiskOpts struct {
	// probeFsAlways is set if we need filesystem information even for GPT disks
	probeFsAlways bool
}

// probeDisk probes a device node using libblkid, filling Disk with the found data.
func probeDisk(node string, opts probeDiskOpts) (*Disk, error) {
	if osutil.IsSymlink(node) {
		return nil, fmt.Errorf("%q is a symlink", node)
	}

	probe, err := blkid.NewProbeFromFilename(node)
	if err != nil {
		return nil, err
	}
	defer probe.Close()

	probe.EnablePartitions(true)
	probe.SetPartitionsFlags(blkid.BLKID_PARTS_ENTRY_DETAILS)

	if err := probe.DoSafeprobe(); err != nil {
		return nil, err
	}

	gpt := isGpt(probe)
	partitions, err := probe.GetPartitions()
	if err != nil {
		return nil, err
	} else if partitions == nil {
		// Observed on rpi4 with loop-devices from snaps, no partitions
		// exists, ensure we return something that avoid crashes.
		return &Disk{Node: node, Parts: []*Partition{}}, nil
	}

	parts := make([]*Partition, 0)
	// We might have a "p" in the partition node, see below.
	maybeP := ""
	if unicode.IsDigit(rune(node[len(node)-1])) {
		maybeP = "p"
	}
	for _, partition := range partitions.GetPartitions() {
		var p Partition
		p.Number = partition.GetNumber()

		// In the scan command case, we probe the filesystem only if
		// this is not GPT, to avoid expending cycles for data that we
		// are not going to use.
		if opts.probeFsAlways || !gpt {
			fsInfo, err := probeFilesystemInfo(node, p.Number,
				partition.GetStart(), partition.GetSize())
			if err != nil {
				logger.Noticef("WARNING: cannot probe filesystem on partition %d of %s: %s",
					p.Number, node, err)
				continue
			}
			p.Filesystem = *fsInfo
		}

		// Build the partition node name now, as in Linux add_partition():
		// https://github.com/torvalds/linux/blob/v6.18/block/partitions/core.c#L335
		p.Node = fmt.Sprint(node, maybeP, p.Number)
		if gpt {
			p.Name = partition.GetName()
			p.UUID = partition.GetUUID()
		}
		parts = append(parts, &p)
	}

	return &Disk{Node: node, Parts: parts}, nil
}

func diskNodeFromDiskSnapdSymlink() (string, error) {
	symLink, err := os.Readlink(filepath.Join(dirs.GlobalRootDir, "/dev/disk/snapd/disk"))
	if err != nil {
		return "", err
	}
	node := filepath.Base(symLink)
	return filepath.Join("/dev", node), nil
}

// findBootDisk finds the boot disk partitions using the boot
// package function FindPartitionUUIDForBootedKernelDisk to determine what
// partition the booted kernel came from.
//
// If "snap-bootstrap scan-disk" was run as part of udev it will
// restrict the search of the partition to the boot disk it found.
//
// If "snap-bootstrap scan-disk" is not in use (legacy case),
// it will look for any partition that matches the boot.
//
// If the disk kernel came from cannot be determined, then it will fallback to
// looking at the specified disk label.
func findBootDisk(fallbacklabel string) (*Disk, string, error) {
	var bootPart string
	var disk *Disk

	if diskNode, err := diskNodeFromDiskSnapdSymlink(); err == nil {
		// We have symlinks, we already know the disk (scan-disk was run)
		logger.Debugf("probing boot disk %s found by scan command", diskNode)
		disk, err = probeDisk(diskNode, probeDiskOpts{probeFsAlways: true})
		if err != nil {
			return nil, "", err
		}

		var part *Partition
		partuuid, err := bootFindPartitionUUIDForBootedKernelDisk()
		if err == nil {
			part, err = disk.PartitionWithUUID(partuuid)
			if err != nil {
				return nil, "", err
			}
		} else {
			part, err = disk.PartitionWithFsLabel(fallbacklabel)
			if err != nil {
				return nil, "", err
			}
		}
		bootPart = part.Node
	} else {
		// This is the legacy case (older initramfs). Get boot disk
		// from UEFI if possible, otherwise use command line or fs
		// label.
		partuuid, err := bootFindPartitionUUIDForBootedKernelDisk()
		if err == nil {
			bootPart = filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partuuid", partuuid)
		} else {
			bootPart, err = getNonUEFISystemDisk(fallbacklabel)
			if err != nil {
				return nil, "", err
			}
		}

		// The partition uuid is read from the EFI variables. At this point
		// the kernel may not have initialized the storage HW yet so poll
		// here.
		logger.Debugf("waiting for partition %s", bootPart)
		if err := waitForDevice(bootPart); err != nil {
			return nil, "", err
		}

		// Resolve if it is a symlink
		bootPart, err = filepath.EvalSymlinks(bootPart)
		if err != nil {
			return nil, "", err
		}
		bootPart = dirs.StripRootDir(bootPart)

		// Find out disk and probe
		diskNode = strings.TrimRight(bootPart, "0123456789")
		lenNode := len(diskNode)
		if diskNode[lenNode-1] == 'p' && unicode.IsDigit(rune(diskNode[lenNode-2])) {
			diskNode = diskNode[0 : lenNode-1]
		}
		disk, err = probeDisk(diskNode, probeDiskOpts{probeFsAlways: true})
		if err != nil {
			return nil, "", err
		}
	}
	return disk, bootPart, nil
}

func getNonUEFISystemDisk(fallbacklabel string) (string, error) {
	values, err := kcmdline.KeyValues("snapd_system_disk")
	if err != nil {
		return "", err
	}
	if value, ok := values["snapd_system_disk"]; ok {
		if err := waitForDevice(value); err != nil {
			return "", err
		}
		// TODO probe instead using blkid. Note that this path is used
		// only when we do not run the scan command (UC20/22).
		systemdDisk, err := disks.DiskFromDeviceName(value)
		if err != nil {
			systemdDiskDevicePath, errDevicePath := disks.DiskFromDevicePath(value)
			if errDevicePath != nil {
				return "", fmt.Errorf("%q can neither be used as a device nor as a block: %v; %v",
					value, errDevicePath, err)
			}
			systemdDisk = systemdDiskDevicePath
		}
		partition, err := systemdDisk.FindMatchingPartitionWithFsLabel(fallbacklabel)
		if err != nil {
			return "", err
		}
		return partition.KernelDeviceNode, nil
	}

	candidate, err := waitForCandidateByLabelPath(fallbacklabel)
	if err != nil {
		return "", err
	}

	return candidate, nil
}

// waitFile waits for the given file/device-node/directory to appear.
var waitFile = func(path string, wait time.Duration, n int) error {
	for i := 0; i < n; i++ {
		if osutil.FileExists(path) {
			return nil
		}
		time.Sleep(wait)
	}

	return fmt.Errorf("no %v after waiting for %v", path, time.Duration(n)*wait)
}

// TODO: those have to be waited by udev instead
func waitForDevice(path string) error {
	if !osutil.FileExists(path) {
		pollWait := 50 * time.Millisecond
		pollIterations := 1200
		logger.Noticef("waiting up to %v for %v to appear", time.Duration(pollIterations)*pollWait, path)
		if err := waitFile(path, pollWait, pollIterations); err != nil {
			return fmt.Errorf("cannot find device: %v", err)
		}
	}
	return nil
}

// Defined externally for faster unit tests
var pollWaitForLabel = 50 * time.Millisecond
var pollWaitForLabelIters = 1200

// TODO: those have to be waited by udev instead
func waitForCandidateByLabelPath(label string) (string, error) {
	logger.Noticef("waiting up to %v for label %v to appear",
		time.Duration(pollWaitForLabelIters)*pollWaitForLabel, label)
	var err error
	for i := 0; i < pollWaitForLabelIters; i++ {
		var candidate string
		// Ideally depending on the type of error we would return
		// immediately or try again, but that would complicate code more
		// than necessary and the extra wait will happen only when we
		// will fail to boot anyway. Note also that this code is
		// actually racy as we could get a not-best-possible-label (say,
		// we get "Ubuntu-boot" while actually an exact "ubuntu-boot"
		// label exists but the link has not been created yet): this is
		// not a fully solvable problem although waiting by udev will
		// help if the disk is present on boot.
		if candidate, err = disks.CandidateByLabelPath(label); err == nil {
			logger.Noticef("label %q found", candidate)
			return candidate, nil
		}
		time.Sleep(pollWaitForLabel)
	}

	// This is the last error from CandidateByLabelPath
	return "", err
}
