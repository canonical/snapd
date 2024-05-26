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

package mkfs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil/shlex"
)

// MakeFunc defines a function signature that is used by all of the mkfs.<filesystem>
// functions supported in this package. This is done to allow them to be defined
// in the mkfsHandlers map
type MakeFunc func(imgFile, label, contentsRootDir string, deviceSize, sectorSize quantity.Size) error

var mkfsHandlers = map[string]MakeFunc{
	"vfat-16": mkfsVfat16,
	"vfat":    mkfsVfat32,
	"vfat-32": mkfsVfat32,
	"ext4":    mkfsExt4,
}

// Make creates a filesystem of given type and provided label in the device or
// file. The device size and sector size provides hints for additional tuning of
// the created filesystem.
func Make(typ, img, label string, deviceSize, sectorSize quantity.Size) error {
	return MakeWithContent(typ, img, label, "", deviceSize, sectorSize)
}

// MakeWithContent creates a filesystem of given type and provided label in the
// device or file. The filesystem is populated with contents of contentRootDir.
// The device size provides hints for additional tuning of the created
// filesystem.
func MakeWithContent(typ, img, label, contentRootDir string, deviceSize, sectorSize quantity.Size) error {
	h, ok := mkfsHandlers[typ]
	if !ok {
		return fmt.Errorf("cannot create unsupported filesystem %q", typ)
	}
	return h(img, label, contentRootDir, deviceSize, sectorSize)
}

// mkfsExt4 creates an EXT4 filesystem in given image file, with an optional
// filesystem label, and populates it with the contents of provided root
// directory.
func mkfsExt4(img, label, contentsRootDir string, deviceSize, sectorSize quantity.Size) error {
	// Originally taken from ubuntu-image
	// Switched to use mkfs defaults for https://bugs.launchpad.net/snappy/+bug/1878374
	// For caveats/requirements in case we need support for older systems:
	// https://github.com/snapcore/snapd/pull/6997#discussion_r293967140
	mkfsArgs := []string{"mkfs.ext4"}

	const size32MiB = 32 * quantity.SizeMiB
	if deviceSize != 0 && deviceSize <= size32MiB {
		// With the default block size of 4096 bytes, the minimal journal size
		// is 4M, meaning we loose a lot of usable space. Try to follow the
		// e2fsprogs upstream and use a 1k block size for smaller
		// filesystems, note that this may cause issues like
		// https://bugs.launchpad.net/ubuntu/+source/lvm2/+bug/1817097
		// if one migrates the filesystem to a device with a different
		// block size

		// though note if the sector size was specified (i.e. non-zero) and
		// larger than 1K, then we need to use that, since you can't create
		// a filesystem with a block-size smaller than the sector-size
		// see e2fsprogs source code:
		// https://github.com/tytso/e2fsprogs/blob/0d47f5ab05177c1861f16bb3644a47018e6be1d0/misc/mke2fs.c#L2151-L2156
		defaultSectorSize := 1 * quantity.SizeKiB
		if sectorSize > 1024 {
			defaultSectorSize = sectorSize
		}
		mkfsArgs = append(mkfsArgs, "-b", defaultSectorSize.String())
	}
	if contentsRootDir != "" {
		// mkfs.ext4 can populate the filesystem with contents of given
		// root directory
		// TODO: support e2fsprogs 1.42 without -d in Ubuntu 16.04
		mkfsArgs = append(mkfsArgs, "-d", contentsRootDir)
	}
	if label != "" {
		mkfsArgs = append(mkfsArgs, "-L", label)
	}
	mkfsArgs = append(mkfsArgs, img)

	var cmd *exec.Cmd
	if os.Geteuid() != 0 {
		// run through fakeroot so that files are owned by root
		fakerootFlags := os.Getenv("FAKEROOT_FLAGS")
		if fakerootFlags != "" {
			// When executing fakeroot from a classic confinement snap the location of
			// libfakeroot must be specified, or else it will be loaded from the host system
			flags := mylog.Check2(shlex.Split(fakerootFlags))

			if len(fakerootFlags) > 0 {
				fakerootArgs := append(flags, "--")
				mkfsArgs = append(fakerootArgs, mkfsArgs...)
			}
		}
		cmd = exec.Command("fakeroot", mkfsArgs...)
	} else {
		// no need to fake it if we're already root
		cmd = exec.Command(mkfsArgs[0], mkfsArgs[1:]...)
	}
	out := mylog.Check2(cmd.CombinedOutput())

	return nil
}

func mkfsVfat16(img, label, contentsRootDir string, deviceSize, sectorSize quantity.Size) error {
	return mkfsVfat(img, label, contentsRootDir, deviceSize, sectorSize, "16")
}

func mkfsVfat32(img, label, contentsRootDir string, deviceSize, sectorSize quantity.Size) error {
	return mkfsVfat(img, label, contentsRootDir, deviceSize, sectorSize, "32")
}

// mkfsVfat creates a VFAT filesystem in given image file, with an optional
// filesystem label, and populates it with the contents of provided root
// directory.
func mkfsVfat(img, label, contentsRootDir string, deviceSize, sectorSize quantity.Size, fatBits string) error {
	// 512B logical sector size by default, unless the specified sector size is
	// larger than 512, in which case use the sector size
	// mkfs.vfat will automatically increase the block size to the internal
	// sector size of the disk if the specified block size is too small, but
	// be paranoid and always set the block size to that of the sector size if
	// we know the sector size is larger than the default 512 (originally from
	// ubuntu-image). see dosfstools:
	// https://github.com/dosfstools/dosfstools/blob/e579a7df89bb3a6df08847d45c70c8ebfabca7d2/src/mkfs.fat.c#L1892-L1898
	defaultSectorSize := quantity.Size(512)
	if sectorSize > defaultSectorSize {
		defaultSectorSize = sectorSize
	}
	mkfsArgs := []string{
		// options taken from ubuntu-image, except the sector size
		"-S", defaultSectorSize.String(),
		// 1 sector per cluster
		"-s", "1",
		// 32b FAT size
		"-F", fatBits,
	}
	if label != "" {
		mkfsArgs = append(mkfsArgs, "-n", label)
	}
	mkfsArgs = append(mkfsArgs, img)

	cmd := exec.Command("mkfs.vfat", mkfsArgs...)
	out := mylog.Check2(cmd.CombinedOutput())

	// if there is no content to copy we are done now
	if contentsRootDir == "" {
		return nil
	}

	// mkfs.vfat does not know how to populate the filesystem with contents,
	// we need to do the work ourselves

	fis := mylog.Check2(os.ReadDir(contentsRootDir))

	if len(fis) == 0 {
		// nothing to copy to the image
		return nil
	}

	mcopyArgs := make([]string, 0, 4+len(fis))
	mcopyArgs = append(mcopyArgs,
		// recursive copy
		"-s",
		// image file
		"-i", img)
	for _, fi := range fis {
		mcopyArgs = append(mcopyArgs, filepath.Join(contentsRootDir, fi.Name()))
	}
	mcopyArgs = append(mcopyArgs,
		// place content at the / of the filesystem
		"::")

	cmd = exec.Command("mcopy", mcopyArgs...)
	cmd.Env = os.Environ()
	// skip mtools checks to avoid unnecessary warnings
	cmd.Env = append(cmd.Env, "MTOOLS_SKIP_CHECK=1")

	out = mylog.Check2(cmd.CombinedOutput())

	return nil
}
