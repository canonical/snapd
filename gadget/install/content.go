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
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/internal"
)

var contentMountpoint string

func init() {
	contentMountpoint = filepath.Join(dirs.SnapRunDir, "gadget-install")
}

// makeFilesystem creates a filesystem on the on-disk structure, according
// to the filesystem type defined in the gadget.
func makeFilesystem(ds *gadget.OnDiskStructure) error {
	if ds.HasFilesystem() {
		if err := internal.Mkfs(ds.VolumeStructure.Filesystem, ds.Node, ds.VolumeStructure.Label); err != nil {
			return err
		}
		if err := udevTrigger(ds.Node); err != nil {
			return err
		}
	}
	return nil
}

// writeContent populates the given on-disk structure, according to the contents
// defined in the gadget.
func writeContent(ds *gadget.OnDiskStructure, gadgetRoot string) error {
	switch {
	case !ds.IsPartition():
		return fmt.Errorf("cannot write non-partitions yet")
	case !ds.HasFilesystem():
		if err := writeNonFSContent(ds, gadgetRoot); err != nil {
			return err
		}
	case ds.HasFilesystem():
		if err := writeFilesystemContent(ds, gadgetRoot); err != nil {
			return err
		}
	}

	return nil
}

// mountFilesystem mounts the on-disk structure filesystem under the given base
// directory, using the label defined in the gadget as the mount point name.
func mountFilesystem(ds *gadget.OnDiskStructure, baseMntPoint string) error {
	if !ds.HasFilesystem() {
		return fmt.Errorf("cannot mount a partition with no filesystem")
	}
	if ds.Label == "" {
		return fmt.Errorf("cannot mount a filesystem with no label")
	}

	mountpoint := filepath.Join(baseMntPoint, ds.Label)
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return fmt.Errorf("cannot create mountpoint: %v", err)
	}
	if err := sysMount(ds.Node, mountpoint, ds.Filesystem, 0, ""); err != nil {
		return fmt.Errorf("cannot mount filesystem %q at %q: %v", ds.Node, mountpoint, err)
	}

	return nil
}

func writeFilesystemContent(ds *gadget.OnDiskStructure, gadgetRoot string) (err error) {
	mountpoint := filepath.Join(contentMountpoint, strconv.Itoa(ds.Index))
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return err
	}

	// temporarily mount the filesystem
	if err := sysMount(ds.Node, mountpoint, ds.Filesystem, 0, ""); err != nil {
		return fmt.Errorf("cannot mount filesystem %q at %q: %v", ds.Node, mountpoint, err)
	}
	defer func() {
		errUnmount := sysUnmount(mountpoint, 0)
		if err == nil {
			err = errUnmount
		}
	}()

	fs, err := gadget.NewMountedFilesystemWriter(gadgetRoot, &ds.LaidOutStructure)
	if err != nil {
		return fmt.Errorf("cannot create filesystem image writer: %v", err)
	}

	var noFilesToPreserve []string
	if err := fs.Write(mountpoint, noFilesToPreserve); err != nil {
		return fmt.Errorf("cannot create filesystem image: %v", err)
	}

	return nil
}

func writeNonFSContent(ds *gadget.OnDiskStructure, gadgetRoot string) error {
	f, err := os.OpenFile(ds.Node, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("cannot write bare content for %q: %v", ds.Node, err)
	}
	defer f.Close()

	// Laid out structures start relative to the beginning of the
	// volume, shift the structure offsets to 0, so that it starts
	// at the beginning of the partition
	l := gadget.ShiftStructureTo(ds.LaidOutStructure, 0)
	raw, err := gadget.NewRawStructureWriter(gadgetRoot, &l)
	if err != nil {
		return err
	}
	return raw.Write(f)
}
