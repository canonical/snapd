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
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
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
		byPartlabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", encodeLabel(ps.Name))
		candidates = append(candidates, byPartlabel)
	}

	if ps.Label != "" {
		byFsLabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/", encodeLabel(ps.Label))
		candidates = append(candidates, byFsLabel)
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

// FindDeviceForStructureWithFallback attempts to find an existing block device
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
func FindDeviceForStructureWithFallback(ps *LaidOutStructure) (dev string, offs Size, err error) {
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
	if err == ErrDeviceNotFound && ps.Type != "bare" && ps.Name != "" {
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

// encodeLabel encodes a name for use a partition or filesystem label symlink by
// udev. The result matches the output of blkid_encode_string().
func encodeLabel(in string) string {
	const allowed = `#+-.:=@_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`

	buf := &bytes.Buffer{}

	for _, r := range in {
		switch {
		case utf8.RuneLen(r) > 1:
			buf.WriteRune(r)
		case !strings.ContainsRune(allowed, r):
			fmt.Fprintf(buf, `\x%x`, r)
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// FindMountPointForStructure locates a mount point of a device that matches
// given structure. The structure must have a filesystem defined, otherwise an
// error is raised.
func FindMountPointForStructure(ps *LaidOutStructure) (string, error) {
	if !ps.HasFilesystem() {
		return "", ErrNoFilesystemDefined
	}

	devpath, err := FindDeviceForStructure(ps)
	if err != nil {
		return "", err
	}

	var mountPoint string
	mountInfo, err := osutil.LoadMountInfo(filepath.Join(dirs.GlobalRootDir, osutil.ProcSelfMountInfo))
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
	mountInfo, err := osutil.LoadMountInfo(filepath.Join(dirs.GlobalRootDir, osutil.ProcSelfMountInfo))
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
	deviceWritable, err := findDeviceForWritable()
	if err != nil {
		return "", err
	}

	// /dev/sda3 -> sda3
	devname := filepath.Base(deviceWritable)

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
