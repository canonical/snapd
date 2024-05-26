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
	"strings"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/mkfs"
)

var mkfsImpl = mkfs.Make

type mkfsParams struct {
	Type       string
	Device     string
	Label      string
	Size       quantity.Size
	SectorSize quantity.Size
}

// makeFilesystem creates a filesystem on the on-disk structure, according
// to the filesystem type defined in the gadget. If sectorSize is specified,
// that sector size is used when creating the filesystem, otherwise if it is
// zero, automatic values are used instead.
func makeFilesystem(params mkfsParams) error {
	logger.Debugf("create %s filesystem on %s with label %q", params.Type, params.Device, params.Label)
	mylog.Check(mkfsImpl(params.Type, params.Device, params.Label, params.Size, params.SectorSize))

	return udevTrigger(params.Device)
}

// mountFilesystem mounts the filesystem on a given device with
// filesystem type fs under the provided mount point directory.
func mountFilesystem(fsDevice, fs, mountpoint string) error {
	mylog.Check(os.MkdirAll(mountpoint, 0755))
	mylog.Check(sysMount(fsDevice, mountpoint, fs, 0, ""))

	return nil
}

func unmountWithFallbackToLazy(mntPt, operationMsg string) error {
	mylog.Check(sysUnmount(mntPt, 0))

	// lazy umount on error, see LP:2025402

	return nil
}

// writeContent populates the given on-disk filesystem structure with a
// corresponding filesystem device, according to the contents defined in the
// gadget.
func writeFilesystemContent(laidOut *gadget.LaidOutStructure, fsDevice string, observer gadget.ContentObserver) (err error) {
	mountpoint := filepath.Join(dirs.SnapRunDir, "gadget-install", strings.ReplaceAll(strings.Trim(fsDevice, "/"), "/", "-"))
	mylog.Check(os.MkdirAll(mountpoint, 0755))

	// temporarily mount the filesystem
	logger.Debugf("mounting %q in %q (fs type %q)", fsDevice, mountpoint, laidOut.Filesystem())
	mylog.Check(sysMount(fsDevice, mountpoint, laidOut.Filesystem(), 0, ""))

	defer func() {
		errUnmount := unmountWithFallbackToLazy(mountpoint, "writing filesystem content")
		if err == nil && errUnmount != nil {
			mylog.Check(fmt.Errorf("cannot unmount %v after writing filesystem content: %v", fsDevice, errUnmount))
		}
	}()
	fs := mylog.Check2(gadget.NewMountedFilesystemWriter(nil, laidOut, observer))

	var noFilesToPreserve []string
	mylog.Check(fs.Write(mountpoint, noFilesToPreserve))

	return nil
}
