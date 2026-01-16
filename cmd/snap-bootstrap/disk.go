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
	"strings"
	"syscall"
	"unicode"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/blkid"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
)

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

// Implemention of interface needed by secboot.
var _ = secboot.Disk(&Disk{})

func (d *Disk) SecbootPartitionWithFsLabel(label string) (secboot.Partition, error) {
	return d.PartitionWithFsLabel(label)
}

func (d *Disk) Model() string {
	// Get file information for the device node
	info, err := os.Stat(d.Node)
	if err != nil {
		logger.Noticef("failed to stat device node %s: %s", d.Node, err)
		return "unknown"
	}

	// Extract the raw stat data
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		logger.Noticef("cannot retrieve stat raw data for %s", d.Node)
		return "unknown"
	}

	// Extract major and minor numbers from Rdev. The glibc format is 64
	// bits of the form MMMM Mmmm mmmM MMmm. See
	// https://github.com/bminor/glibc/blob/glibc-2.42/bits/sysmacros.h for
	// the macro definitions of major() and minor() in glibc.
	major := (stat.Rdev & 0x00000000000fff00) >> 8
	major |= (stat.Rdev & 0xfffff00000000000) >> 8
	minor := stat.Rdev & 0x00000000000000ff
	minor |= (stat.Rdev & 0x00000ffffff00000) >> 12

	// The model can be found in /sys/dev/block/<major>:<minor>/device/model
	sysfsPath := fmt.Sprintf("/sys/dev/block/%d:%d/device/model", major, minor)

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
	// fullProbe is set if we need filesystem information even for GPT disks
	fullProbe bool
}

// probeDisk probes a device node using libblkid, filling Disk with the found data.
func probeDisk(node string, opts probeDiskOpts) (*Disk, error) {
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
		if opts.fullProbe || !gpt {
			fsInfo, err := probeFilesystemInfo(node, p.Number, partition.GetStart(), partition.GetSize())
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
