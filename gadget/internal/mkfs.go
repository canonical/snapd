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

package internal

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
)

type MkfsFunc func(imgFile, label, contentsRootDir string, deviceSize quantity.Size) error

var (
	mkfsHandlers = map[string]MkfsFunc{
		"vfat": mkfsVfat,
		"ext4": mkfsExt4,
	}
)

// Mkfs creates a filesystem of given type and provided label in the device or
// file. The device size provides hints for additional tuning of the created
// filesystem.
func Mkfs(typ, img, label string, deviceSize quantity.Size) error {
	return MkfsWithContent(typ, img, label, "", deviceSize)
}

// Mkfs creates a filesystem of given type and provided label in the device or
// file. The filesystem is populated with contents of contentRootDir. The device
// size provides hints for additional tuning of the created filesystem.
func MkfsWithContent(typ, img, label, contentRootDir string, deviceSize quantity.Size) error {
	h, ok := mkfsHandlers[typ]
	if !ok {
		return fmt.Errorf("cannot create unsupported filesystem %q", typ)
	}
	return h(img, label, contentRootDir, deviceSize)
}

// mkfsExt4 creates an EXT4 filesystem in given image file, with an optional
// filesystem label, and populates it with the contents of provided root
// directory.
func mkfsExt4(img, label, contentsRootDir string, deviceSize quantity.Size) error {
	// Originally taken from ubuntu-image
	// Switched to use mkfs defaults for https://bugs.launchpad.net/snappy/+bug/1878374
	// For caveats/requirements in case we need support for older systems:
	// https://github.com/snapcore/snapd/pull/6997#discussion_r293967140
	mkfsArgs := []string{"mkfs.ext4"}
	const size32MiB = 32 * quantity.SizeMiB
	if deviceSize != 0 && deviceSize <= size32MiB {
		// With the default of 4096 bytes, the minimal journal size is
		// 4M, meaning we loose a lot of usable space. Try to follow the
		// e2fsprogs upstream and use a 1k block size for smaller
		// filesystems, note that this may cause issues like
		// https://bugs.launchpad.net/ubuntu/+source/lvm2/+bug/1817097
		// if one migrates the filesystem to a device with a different
		// block size
		mkfsArgs = append(mkfsArgs, "-b", "1024")
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
		cmd = exec.Command("fakeroot", mkfsArgs...)
	} else {
		// no need to fake it if we're already root
		cmd = exec.Command(mkfsArgs[0], mkfsArgs[1:]...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return osutil.OutputErr(out, err)
	}
	return nil
}

// mkfsVfat creates a VFAT filesystem in given image file, with an optional
// filesystem label, and populates it with the contents of provided root
// directory.
func mkfsVfat(img, label, contentsRootDir string, deviceSize quantity.Size) error {
	// taken from ubuntu-image
	mkfsArgs := []string{
		// 512B logical sector size
		"-S", "512",
		// 1 sector per cluster
		"-s", "1",
		// 32b FAT size
		"-F", "32",
	}
	if label != "" {
		mkfsArgs = append(mkfsArgs, "-n", label)
	}
	mkfsArgs = append(mkfsArgs, img)

	cmd := exec.Command("mkfs.vfat", mkfsArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return osutil.OutputErr(out, err)
	}

	// if there is no content to copy we are done now
	if contentsRootDir == "" {
		return nil
	}

	// mkfs.vfat does not know how to populate the filesystem with contents,
	// we need to do the work ourselves

	fis, err := ioutil.ReadDir(contentsRootDir)
	if err != nil {
		return fmt.Errorf("cannot list directory contents: %v", err)
	}
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

	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot populate vfat filesystem with contents: %v", osutil.OutputErr(out, err))
	}
	return nil
}
