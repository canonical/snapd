// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package boot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// RemoveKernelAssets removes the unpacked kernel/initrd for the given
// kernel snap.
func RemoveKernelAssets(s snap.PlaceInfo, inter progress.Meter) error {
	bootloader, err := partition.FindBootloader()
	if err != nil {
		return fmt.Errorf("no not remove kernel assets: %s", err)
	}

	// remove the kernel blob
	blobName := filepath.Base(s.MountFile())
	dstDir := filepath.Join(bootloader.Dir(), blobName)
	if err := os.RemoveAll(dstDir); err != nil {
		return err
	}

	return nil
}

func copyAll(src, dst string) error {
	if output, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot copy %q -> %q: %s (%s)", src, dst, err, output)
	}
	return nil
}

// ExtractKernelAssets extracts kernel/initrd/dtb data from the given
// kernel snap, if required, to a versioned bootloader directory so
// that the bootloader can use it.
func ExtractKernelAssets(s *snap.Info, inter progress.Meter) error {
	if s.Type != snap.TypeKernel {
		return fmt.Errorf("cannot extract kernel assets from snap type %q", s.Type)
	}

	bootloader, err := partition.FindBootloader()
	if err != nil {
		return fmt.Errorf("cannot extract kernel assets: %s", err)
	}

	if bootloader.Name() == "grub" {
		return nil
	}

	// now do the kernel specific bits
	blobName := filepath.Base(s.MountFile())
	dstDir := filepath.Join(bootloader.Dir(), blobName)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	dir, err := os.Open(dstDir)
	if err != nil {
		return err
	}
	defer dir.Close()

	for _, src := range []string{
		filepath.Join(s.MountDir(), "kernel.img"),
		filepath.Join(s.MountDir(), "initrd.img"),
	} {
		if err := copyAll(src, dstDir); err != nil {
			return err
		}
		if err := dir.Sync(); err != nil {
			return err
		}
	}

	srcDir := filepath.Join(s.MountDir(), "dtbs")
	if osutil.IsDirectory(srcDir) {
		if err := copyAll(srcDir, dstDir); err != nil {
			return err
		}
	}

	return dir.Sync()
}

// SetNextBoot will schedule the given OS or kernel snap to be used in
// the next boot
func SetNextBoot(s *snap.Info) error {
	if release.OnClassic {
		return nil
	}
	if s.Type != snap.TypeOS && s.Type != snap.TypeKernel {
		return nil
	}

	bootloader, err := partition.FindBootloader()
	if err != nil {
		return fmt.Errorf("cannot set next boot: %s", err)
	}

	var bootvar string
	switch s.Type {
	case snap.TypeOS:
		bootvar = "snap_try_core"
	case snap.TypeKernel:
		bootvar = "snap_try_kernel"
	}
	blobName := filepath.Base(s.MountFile())
	if err := bootloader.SetBootVar(bootvar, blobName); err != nil {
		return err
	}

	if err := bootloader.SetBootVar("snap_mode", "try"); err != nil {
		return err
	}

	return nil
}

// KernelOrOsRebootRequired returns whether a reboot is required to swith to the given OS or kernel snap.
func KernelOrOsRebootRequired(s *snap.Info) bool {
	if s.Type != snap.TypeKernel && s.Type != snap.TypeOS {
		return false
	}

	bootloader, err := partition.FindBootloader()
	if err != nil {
		logger.Noticef("cannot get boot settings: %s", err)
		return false
	}

	var nextBoot, goodBoot string
	switch s.Type {
	case snap.TypeKernel:
		nextBoot = "snap_try_kernel"
		goodBoot = "snap_kernel"
	case snap.TypeOS:
		nextBoot = "snap_try_core"
		goodBoot = "snap_core"
	}

	nextBootVer, err := bootloader.GetBootVar(nextBoot)
	if err != nil {
		return false
	}
	goodBootVer, err := bootloader.GetBootVar(goodBoot)
	if err != nil {
		return false
	}

	squashfsName := filepath.Base(s.MountFile())
	if nextBootVer == squashfsName && goodBootVer != nextBootVer {
		return true
	}

	return false
}
