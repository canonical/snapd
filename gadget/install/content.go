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
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/mkfs"
)

var contentMountpoint string

var mkfsImpl = mkfs.Make

func init() {
	contentMountpoint = filepath.Join(dirs.SnapRunDir, "gadget-install")
}

// makeFilesystem creates a filesystem on the on-disk structure, according
// to the filesystem type defined in the gadget. If sectorSize is specified,
// that sector size is used when creating the filesystem, otherwise if it is
// zero, automatic values are used instead.
func makeFilesystem(ds *gadget.OnDiskStructure, sectorSize quantity.Size) error {
	if !ds.HasFilesystem() {
		return fmt.Errorf("internal error: on disk structure for partition %s has no filesystem", ds.Node)
	}
	logger.Debugf("create %s filesystem on %s with label %q", ds.VolumeStructure.Filesystem, ds.Node, ds.VolumeStructure.Label)
	if err := mkfsImpl(ds.VolumeStructure.Filesystem, ds.Node, ds.VolumeStructure.Label, ds.Size, sectorSize); err != nil {
		return err
	}
	return udevTrigger(ds.Node)
}

// writeContent populates the given on-disk structure, according to the contents
// defined in the gadget.
func writeContent(ds *gadget.OnDiskStructure, observer gadget.ContentObserver) error {
	if ds.HasFilesystem() {
		return writeFilesystemContent(ds, observer)
	}
	return fmt.Errorf("cannot write non-filesystem structures during install")
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

func writeFilesystemContent(ds *gadget.OnDiskStructure, observer gadget.ContentObserver) (err error) {
	mountpoint := filepath.Join(contentMountpoint, strconv.Itoa(ds.DiskIndex))
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
	fs, err := gadget.NewMountedFilesystemWriter(&ds.LaidOutStructure, observer)
	if err != nil {
		return fmt.Errorf("cannot create filesystem image writer: %v", err)
	}

	var noFilesToPreserve []string
	if err := fs.Write(mountpoint, noFilesToPreserve); err != nil {
		return fmt.Errorf("cannot create filesystem image: %v", err)
	}

	return nil
}
