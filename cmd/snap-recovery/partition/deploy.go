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

func deployFilesystemContent(part DeviceStructure, gadgetRoot string) error {
	mountpoint := filepath.Join(deployMountpoint, strconv.Itoa(part.Index))
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return err
	}

	// temporarily mount the filesystem
	if err := sysMount(part.Node, mountpoint, part.Filesystem, 0, ""); err != nil {
		return fmt.Errorf("cannot mount filesystem %q to %q: %v", part.Node, mountpoint, err)
	}
	defer sysUnmount(mountpoint, 0)

	fs, err := gadget.NewMountedFilesystemWriter(gadgetRoot, &part.LaidOutStructure)
	if err != nil {
		return fmt.Errorf("cannot create filesystem image writer: %v", err)
	}

	if err := fs.Write(mountpoint, []string{}); err != nil {
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
		case part.Type == "bare":
			return fmt.Errorf("cannot deploy type 'bare' yet")
		case part.Filesystem == "":
			if err := deployNonFSContent(part, gadgetRoot); err != nil {
				return err
			}
		case part.Filesystem != "":
			if err := deployFilesystemContent(part, gadgetRoot); err != nil {
				return err
			}
		}
	}

	return nil
}
