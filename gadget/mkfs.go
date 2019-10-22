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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

// MkfsExt4 creates an EXT4 filesystem in given image file, with an optional
// filesystem label, and populates it with the contents of provided root
// directory.
func MkfsExt4(img, label, contentsRootDir string) error {
	// taken from ubuntu-image
	mkfsArgs := []string{
		"mkfs.ext4",
		// default usage type
		"-T", "default",
		// disable metadata checksum, which were unsupported in Ubuntu
		// 16.04 and Ubuntu Core 16 systems and would lead to a boot
		// failure if enabled
		"-O", "-metadata_csum",
		// allow uninitialized block groups
		"-O", "uninit_bg",
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
	// run through fakeroot so that files are owned by root
	cmd := exec.Command("fakeroot", mkfsArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return osutil.OutputErr(out, err)
	}
	return nil
}

// MkfsVfat creates a VFAT filesystem in given image file, with an optional
// filesystem label, and populates it with the contents of provided root
// directory.
func MkfsVfat(img, label, contentsRootDir string) error {
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
