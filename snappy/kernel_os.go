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

package snappy

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
)

// dropVersionSuffix drops the kernel/initrd version suffix,
// e.g. "vmlinuz-4.1.0" -> "vmlinuz".
func dropVersionSuffix(name string) string {
	name = filepath.Base(name)
	return strings.SplitN(name, "-", 2)[0]
}

// override in tests
var findBootloader = partition.FindBootloader

// removeKernelAssets removes the unpacked kernel/initrd for the given
// kernel snap
func removeKernelAssets(s snap.PlaceInfo, inter interacter) error {
	bootloader, err := findBootloader()
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

// extractKernelAssets extracts kernel/initrd/dtb data from the given
// Snap to a versionized bootloader directory so that the bootloader
// can use it.
func extractKernelAssets(s *snap.Info, snapf snap.File, flags InstallFlags, inter progress.Meter) error {
	if s.Type != snap.TypeKernel {
		return fmt.Errorf("can not extract kernel assets from snap type %q", s.Type)
	}

	bootloader, err := findBootloader()
	if err != nil {
		return fmt.Errorf("can not extract kernel assets: %s", err)
	}

	// check if we are on a "grub" system. if so, no need to unpack
	// the kernel
	if oem, err := getGadget(); err == nil {
		if oem.Legacy.Gadget.Hardware.Bootloader == "grub" {
			return nil
		}
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

	for _, src := range []string{s.Legacy.Kernel, s.Legacy.Initrd} {
		if src == "" {
			continue
		}
		if err := snapf.Unpack(src, dstDir); err != nil {
			return err
		}
		src = filepath.Join(dstDir, src)
		dst := filepath.Join(dstDir, dropVersionSuffix(src))
		if err := os.Rename(src, dst); err != nil {
			return err
		}
		if err := dir.Sync(); err != nil {
			return err
		}
	}
	if s.Legacy.Dtbs != "" {
		src := filepath.Join(s.Legacy.Dtbs, "*")
		dst := dstDir
		if err := snapf.Unpack(src, dst); err != nil {
			return err
		}
	}

	return dir.Sync()
}

// setNextBoot will schedule the given os or kernel snap to be used in
// the next boot
func setNextBoot(s *Snap) error {
	if s.m.Type != snap.TypeOS && s.m.Type != snap.TypeKernel {
		return nil
	}

	bootloader, err := findBootloader()
	if err != nil {
		return fmt.Errorf("can not set next boot: %s", err)
	}

	var bootvar string
	switch s.m.Type {
	case snap.TypeOS:
		bootvar = "snappy_os"
	case snap.TypeKernel:
		bootvar = "snappy_kernel"
	}
	blobName := filepath.Base(s.Info().MountFile())
	if err := bootloader.SetBootVar(bootvar, blobName); err != nil {
		return err
	}

	if err := bootloader.SetBootVar("snappy_mode", "try"); err != nil {
		return err
	}

	return nil
}

func kernelOrOsRebootRequired(s *Snap) bool {
	if s.m.Type != snap.TypeKernel && s.m.Type != snap.TypeOS {
		return false
	}

	bootloader, err := findBootloader()
	if err != nil {
		logger.Noticef("can not get boot settings: %s", err)
		return false
	}

	var nextBoot, goodBoot string
	switch s.m.Type {
	case snap.TypeKernel:
		nextBoot = "snappy_kernel"
		goodBoot = "snappy_good_kernel"
	case snap.TypeOS:
		nextBoot = "snappy_os"
		goodBoot = "snappy_good_os"
	}

	nextBootVer, err := bootloader.GetBootVar(nextBoot)
	if err != nil {
		return false
	}
	goodBootVer, err := bootloader.GetBootVar(goodBoot)
	if err != nil {
		return false
	}

	squashfsName := filepath.Base(s.Info().MountFile())
	if nextBootVer == squashfsName && goodBootVer != nextBootVer {
		return true
	}

	return false
}

func nameAndRevnoFromSnap(snap string) (string, int) {
	name := strings.Split(snap, "_")[0]
	revnoNSuffix := strings.Split(snap, "_")[1]
	revno, err := strconv.Atoi(strings.Split(revnoNSuffix, ".snap")[0])
	if err != nil {
		return "", -1
	}
	return name, revno
}

// SyncBoot synchronizes the active kernel and OS snap versions with
// the versions that actually booted. This is needed because a
// system may install "os=v2" but that fails to boot. The bootloader
// fallback logic will revert to "os=v1" but on the filesystem snappy
// still has the "active" version set to "v2" which is
// misleading. This code will check what kernel/os booted and set
// those versions active.
func SyncBoot() error {
	bootloader, err := findBootloader()
	if err != nil {
		return fmt.Errorf("can not run SyncBoot: %s", err)
	}

	kernelSnap, _ := bootloader.GetBootVar("snappy_kernel")
	osSnap, _ := bootloader.GetBootVar("snappy_os")

	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return fmt.Errorf("failed to run SyncBoot: %s", err)
	}

	overlord := &Overlord{}
	for _, snap := range []string{kernelSnap, osSnap} {
		name, revno := nameAndRevnoFromSnap(snap)
		found := FindSnapsByNameAndRevision(name, revno, installed)
		if len(found) != 1 {
			return fmt.Errorf("can not SyncBoot, expected 1 snap %q (revno=%d) found %d", snap, revno, len(found))
		}
		if err := overlord.SetActive(found[0], true, nil); err != nil {
			return fmt.Errorf("can not SyncBoot, failed to make %s active: %s", found[0].Name(), err)
		}
	}

	return nil
}
