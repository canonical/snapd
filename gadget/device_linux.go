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

package gadget

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
)

var ErrDeviceNotFound = errors.New("device not found")
var ErrMountNotFound = errors.New("mount point not found")
var ErrNoFilesystemDefined = errors.New("no filesystem defined")

var evalSymlinks = filepath.EvalSymlinks

// FindDeviceForStructure attempts to find an existing block device matching
// given volume structure, by inspecting its name and, optionally, the
// filesystem label. Assumes that the host's udev has set up device symlinks
// correctly.
func FindDeviceForStructure(ps *LaidOutStructure) (string, error) {
	var candidates []string

	if ps.Name != "" {
		byPartlabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", disks.BlkIDEncodeLabel(ps.Name))
		candidates = append(candidates, byPartlabel)
	}
	if ps.HasFilesystem() {
		fsLabel := ps.Label
		if fsLabel == "" && ps.Name != "" {
			// when image is built and the structure has no
			// filesystem label, the structure name will be used by
			// default as the label
			fsLabel = ps.Name
		}
		if fsLabel != "" {
			byFsLabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/", disks.BlkIDEncodeLabel(fsLabel))
			candidates = append(candidates, byFsLabel)
		}
	}

	var found string
	var match string
	for _, candidate := range candidates {
		if !osutil.FileExists(candidate) {
			continue
		}
		if !osutil.IsSymlink(candidate) {
			// /dev/disk/by-label/* and /dev/disk/by-partlabel/* are
			// expected to be symlink
			return "", fmt.Errorf("candidate %v is not a symlink", candidate)
		}
		target, err := evalSymlinks(candidate)
		if err != nil {
			return "", fmt.Errorf("cannot read device link: %v", err)
		}
		if found != "" && target != found {
			// partition and filesystem label links point to
			// different devices
			return "", fmt.Errorf("conflicting device match, %q points to %q, previous match %q points to %q",
				candidate, target, match, found)
		}
		found = target
		match = candidate
	}

	if found == "" {
		return "", ErrDeviceNotFound
	}

	return found, nil
}

// findDeviceForStructureWithFallback attempts to find an existing block device
// partition containing given non-filesystem volume structure, by inspecting the
// structure's name.
//
// Should there be no match, attempts to find the block device corresponding to
// the volume enclosing the structure under the following conditions:
// - the structure has no filesystem
// - and the structure is of type: bare (no partition table entry)
// - or the structure has no name, but a partition table entry (hence no label
//   by which we could find it)
//
// The fallback mechanism uses the fact that Core devices always have a mount at
// /writable. The system is booted from the parent of the device mounted at
// /writable.
//
// Returns the device name and an offset at which the structure content starts
// within the device or an error.
func findDeviceForStructureWithFallback(ps *LaidOutStructure) (dev string, offs quantity.Offset, err error) {
	if ps.HasFilesystem() {
		return "", 0, fmt.Errorf("internal error: cannot use with filesystem structures")
	}

	dev, err = FindDeviceForStructure(ps)
	if err == nil {
		// found exact device representing this structure, thus the
		// structure starts at 0 offset within the device
		return dev, 0, nil
	}
	if err != ErrDeviceNotFound {
		// error out on other errors
		return "", 0, err
	}
	if err == ErrDeviceNotFound && ps.IsPartition() && ps.Name != "" {
		// structures with partition table entry and a name must have
		// been located already
		return "", 0, err
	}

	// we're left with structures that have no partition table entry, or
	// have a partition but no name that could be used to find them

	dev, err = findParentDeviceWithWritableFallback()
	if err != nil {
		return "", 0, err
	}
	// start offset is calculated as an absolute position within the volume
	return dev, ps.StartOffset, nil
}

// findMountPointForStructure locates a mount point of a device that matches
// given structure. The structure must have a filesystem defined, otherwise an
// error is raised.
func findMountPointForStructure(ps *LaidOutStructure) (string, error) {
	if !ps.HasFilesystem() {
		return "", ErrNoFilesystemDefined
	}

	devpath, err := FindDeviceForStructure(ps)
	if err != nil {
		return "", err
	}

	var mountPoint string
	mountInfo, err := osutil.LoadMountInfo()
	if err != nil {
		return "", fmt.Errorf("cannot read mount info: %v", err)
	}
	for _, entry := range mountInfo {
		if entry.Root != "/" {
			// only interested at the location where root of the
			// structure filesystem is mounted
			continue
		}
		if entry.MountSource == devpath && entry.FsType == ps.Filesystem {
			mountPoint = entry.MountDir
			break
		}
	}

	if mountPoint == "" {
		return "", ErrMountNotFound
	}

	return mountPoint, nil
}

func isWritableMount(entry *osutil.MountInfoEntry) bool {
	// example mountinfo entry:
	// 26 27 8:3 / /writable rw,relatime shared:7 - ext4 /dev/sda3 rw,data=ordered
	return entry.Root == "/" && entry.MountDir == "/writable" && entry.FsType == "ext4"
}

func findDeviceForWritable() (device string, err error) {
	mountInfo, err := osutil.LoadMountInfo()
	if err != nil {
		return "", fmt.Errorf("cannot read mount info: %v", err)
	}
	for _, entry := range mountInfo {
		if isWritableMount(entry) {
			return entry.MountSource, nil
		}
	}
	return "", ErrDeviceNotFound
}

func findParentDeviceWithWritableFallback() (string, error) {
	partitionWritable, err := findDeviceForWritable()
	if err != nil {
		return "", err
	}
	return ParentDiskFromMountSource(partitionWritable)
}

// ParentDiskFromMountSource will find the parent disk device for the given
// partition. E.g. /dev/nvmen0n1p5 -> /dev/nvme0n1.
//
// When the mount source is a symlink, it is resolved to the actual device that
// is mounted. Should the device be one created by device mapper, it is followed
// up to the actual underlying block device. As an example, this is how devices
// are followed with a /writable mounted from an encrypted volume:
//
// /dev/mapper/ubuntu-data-<uuid> (a symlink)
//   ⤷ /dev/dm-0 (set up by device mapper)
//       ⤷ /dev/hda4 (actual partition with the content)
//          ⤷ /dev/hda (returned by this function)
//
func ParentDiskFromMountSource(mountSource string) (string, error) {
	// mount source can be a symlink
	st, err := os.Lstat(mountSource)
	if err != nil {
		return "", err
	}
	if mode := st.Mode(); mode&os.ModeSymlink != 0 {
		// resolve to actual device
		target, err := filepath.EvalSymlinks(mountSource)
		if err != nil {
			return "", fmt.Errorf("cannot resolve mount source symlink %v: %v", mountSource, err)
		}
		mountSource = target
	}
	// /dev/sda3 -> sda3
	devname := filepath.Base(mountSource)

	if strings.HasPrefix(devname, "dm-") {
		// looks like a device set up by device mapper
		resolved, err := resolveParentOfDeviceMapperDevice(devname)
		if err != nil {
			return "", fmt.Errorf("cannot resolve device mapper device %v: %v", devname, err)
		}
		devname = resolved
	}

	// do not bother with investigating major/minor devices (inconsistent
	// across block device types) or mangling strings, but look at sys
	// hierarchy for block devices instead:
	// /sys/block/sda               - main SCSI device
	// /sys/block/sda/sda1          - partition 1
	// /sys/block/sda/sda<n>        - partition n
	// /sys/block/nvme0n1           - main NVME device
	// /sys/block/nvme0n1/nvme0n1p1 - partition 1
	matches, err := filepath.Glob(filepath.Join(dirs.GlobalRootDir, "/sys/block/*/", devname))
	if err != nil {
		return "", fmt.Errorf("cannot glob /sys/block/ entries: %v", err)
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("unexpected number of matches (%v) for /sys/block/*/%s", len(matches), devname)
	}

	// at this point we have /sys/block/sda/sda3
	// /sys/block/sda/sda3 -> /dev/sda
	mainDev := filepath.Join(dirs.GlobalRootDir, "/dev/", filepath.Base(filepath.Dir(matches[0])))

	if !osutil.FileExists(mainDev) {
		return "", fmt.Errorf("device %v does not exist", mainDev)
	}
	return mainDev, nil
}

func resolveParentOfDeviceMapperDevice(devname string) (string, error) {
	// devices set up by device mapper have /dev/block/dm-*/slaves directory
	// which lists the devices that are upper in the chain, follow that to
	// find the first device that is non-dm one
	dmSlavesLevel := 0
	const maxDmSlavesLevel = 5
	for strings.HasPrefix(devname, "dm-") {
		// /sys/block/dm-*/slaves/ lists a device that this dm device is part of
		slavesGlob := filepath.Join(dirs.GlobalRootDir, "/sys/block", devname, "slaves/*")
		slaves, err := filepath.Glob(slavesGlob)
		if err != nil {
			return "", fmt.Errorf("cannot glob slaves of dm device %v: %v", devname, err)
		}
		if len(slaves) != 1 {
			return "", fmt.Errorf("unexpected number of dm device %v slaves: %v", devname, len(slaves))
		}
		devname = filepath.Base(slaves[0])

		// if we're this deep in resolving dm devices, things are clearly getting out of hand
		dmSlavesLevel++
		if dmSlavesLevel >= maxDmSlavesLevel {
			return "", fmt.Errorf("too many levels")
		}

	}
	return devname, nil
}
