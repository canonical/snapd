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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/snapcore/snapd/gadget"
)

var (
	deployMountpoint = "/run/snap-recover"

	sysMount   = syscall.Mount
	sysUnmount = syscall.Unmount
)

func deployFilesystemContent(part DeviceStructure, gadgetRoot string) (err error) {
	mountpoint := filepath.Join(deployMountpoint, strconv.Itoa(part.Index))
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return err
	}

	// temporarily mount the filesystem
	if err := sysMount(part.Node, mountpoint, part.Filesystem, 0, ""); err != nil {
		return fmt.Errorf("cannot mount filesystem %q to %q: %v", part.Node, mountpoint, err)
	}
	defer func() {
		errUnmount := sysUnmount(mountpoint, 0)
		if err == nil {
			err = errUnmount
		}
	}()

	fs, err := gadget.NewMountedFilesystemWriter(gadgetRoot, &part.LaidOutStructure)
	if err != nil {
		return fmt.Errorf("cannot create filesystem image writer: %v", err)
	}

	var preserveFiles []string
	if err := fs.Write(mountpoint, preserveFiles); err != nil {
		return fmt.Errorf("cannot create filesystem image: %v", err)
	}

	return nil
}

func deployNonFSContent(part DeviceStructure, gadgetRoot string) error {
	f, err := os.OpenFile(part.Node, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("cannot deploy bare content for %q: %v", part.Node, err)
	}
	defer f.Close()

	// Laid out structures start relative to the beginning of the
	// volume, shift the structure offsets to 0, so that it starts
	// at the beginning of the partition
	l := gadget.ShiftStructureTo(part.LaidOutStructure, 0)
	raw, err := gadget.NewRawStructureWriter(gadgetRoot, &l)
	if err != nil {
		return err
	}
	return raw.Write(f)
}

func DeployContent(created []DeviceStructure, gadgetRoot string) error {
	for _, part := range created {
		switch {
		case !part.IsPartition():
			return fmt.Errorf("cannot deploy non-partitions yet")
		case !part.HasFilesystem():
			if err := deployNonFSContent(part, gadgetRoot); err != nil {
				return err
			}
		case part.HasFilesystem():
			if err := deployFilesystemContent(part, gadgetRoot); err != nil {
				return err
			}
		}
	}

	return nil
}

func MountFilesystems(created []DeviceStructure, baseMntPoint string) error {
	for _, part := range created {
		if part.Label == "" || !part.HasFilesystem() {
			continue
		}

		mountpoint := filepath.Join(baseMntPoint, part.Label)
		if err := os.MkdirAll(mountpoint, 0755); err != nil {
			return fmt.Errorf("cannot create mountpoint: %v", err)
		}
		if err := sysMount(part.Node, mountpoint, part.Filesystem, 0, ""); err != nil {
			return fmt.Errorf("cannot mount filesystem %q to %q: %v", part.Node, mountpoint, err)
		}
	}

	return nil
}
