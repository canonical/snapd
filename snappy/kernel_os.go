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
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

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

func copyAll(src, dst string) error {
	if output, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot copy %q -> %q: %s (%s)", src, dst, err, output)
	}
	return nil
}

// extractKernelAssets extracts kernel/initrd/dtb data from the given
// Snap to a versionized bootloader directory so that the bootloader
// can use it.
func extractKernelAssets(s *snap.Info, flags LegacyInstallFlags, inter progress.Meter) error {
	if s.Type != snap.TypeKernel {
		return fmt.Errorf("cannot extract kernel assets from snap type %q", s.Type)
	}

	// sanity check that we have the new kernel format
	_, err := snap.ReadKernelInfo(s)
	if err != nil {
		return err
	}

	bootloader, err := findBootloader()
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

// SetNextBoot will schedule the given os or kernel snap to be used in
// the next boot
func SetNextBoot(s *snap.Info) error {
	if release.OnClassic {
		return nil
	}
	if s.Type != snap.TypeOS && s.Type != snap.TypeKernel {
		return nil
	}

	bootloader, err := findBootloader()
	if err != nil {
		return fmt.Errorf("cannot set next boot: %s", err)
	}

	var bootvar string
	switch s.Type {
	case snap.TypeOS:
		bootvar = "snappy_os"
	case snap.TypeKernel:
		bootvar = "snappy_kernel"
	}
	blobName := filepath.Base(s.MountFile())
	if err := bootloader.SetBootVar(bootvar, blobName); err != nil {
		return err
	}

	if err := bootloader.SetBootVar("snappy_mode", "try"); err != nil {
		return err
	}

	return nil
}

func kernelOrOsRebootRequired(s *snap.Info) bool {
	if s.Type != snap.TypeKernel && s.Type != snap.TypeOS {
		return false
	}

	bootloader, err := findBootloader()
	if err != nil {
		logger.Noticef("cannot get boot settings: %s", err)
		return false
	}

	var nextBoot, goodBoot string
	switch s.Type {
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

	squashfsName := filepath.Base(s.MountFile())
	if nextBootVer == squashfsName && goodBootVer != nextBootVer {
		return true
	}

	return false
}

func nameAndRevnoFromSnap(sn string) (string, snap.Revision) {
	name := strings.Split(sn, "_")[0]
	revnoNSuffix := strings.Split(sn, "_")[1]
	rev, err := snap.ParseRevision(strings.Split(revnoNSuffix, ".snap")[0])
	if err != nil {
		return "", snap.Revision{}
	}
	return name, rev
}

// SyncBoot synchronizes the active kernel and OS snap versions with
// the versions that actually booted. This is needed because a
// system may install "os=v2" but that fails to boot. The bootloader
// fallback logic will revert to "os=v1" but on the filesystem snappy
// still has the "active" version set to "v2" which is
// misleading. This code will check what kernel/os booted and set
// those versions active.
func SyncBoot() error {
	if release.OnClassic {
		return nil
	}
	bootloader, err := findBootloader()
	if err != nil {
		return fmt.Errorf("cannot run SyncBoot: %s", err)
	}

	kernelSnap, _ := bootloader.GetBootVar("snappy_kernel")
	osSnap, _ := bootloader.GetBootVar("snappy_os")

	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return fmt.Errorf("cannot run SyncBoot: %s", err)
	}

	overlord := &Overlord{}
	for _, snap := range []string{kernelSnap, osSnap} {
		name, revno := nameAndRevnoFromSnap(snap)
		found := FindSnapsByNameAndRevision(name, revno, installed)
		if len(found) != 1 {
			return fmt.Errorf("cannot SyncBoot, expected 1 snap %q (revision %s) found %d", snap, revno, len(found))
		}
		if err := overlord.SetActive(found[0], true, nil); err != nil {
			return fmt.Errorf("cannot SyncBoot, cannot make %s active: %s", found[0].Name(), err)
		}
	}

	return nil
}
