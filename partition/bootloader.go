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

package partition

import (
	"fmt"
	"os"
	"path/filepath"

	"launchpad.net/snappy/helpers"
)

const (
	// bootloader variable used to denote which rootfs to boot from
	bootloaderRootfsVar = "snappy_ab"

	// bootloader variable used to determine if boot was successful.
	// Set to value of either bootloaderBootmodeTry (when attempting
	// to boot a new rootfs) or bootloaderBootmodeSuccess (to denote
	// that the boot of the new rootfs was successful).
	bootloaderBootmodeVar = "snappy_mode"

	// Initial and final values
	bootloaderBootmodeTry     = "try"
	bootloaderBootmodeSuccess = "regular"

	// textual description in hardware.yaml for AB systems
	bootloaderSystemAB = "system-AB"
)

type bootloaderName string

type bootLoader interface {
	// Name of the bootloader
	Name() bootloaderName

	// Switch bootloader configuration so that the "other" root
	// filesystem partition will be used on next boot.
	ToggleRootFS() error

	// Hook function called before system-image starts downloading
	// and applying archives that allows files to be copied between
	// partitions.
	SyncBootFiles() error

	// Install any hardware-specific files that system-image
	// downloaded.
	HandleAssets() error

	// Return the value of the specified bootloader variable
	GetBootVar(name string) (string, error)

	// Return the 1-character name corresponding to the
	// rootfs currently being used.
	GetRootFSName() string

	// Return the 1-character name corresponding to the
	// other rootfs.
	GetOtherRootFSName() string

	// Return the 1-character name corresponding to the
	// rootfs that will be used on _next_ boot.
	//
	// XXX: Note the distinction between this method and
	// GetOtherRootFSName(): the latter corresponds to the other
	// partition, whereas the value returned by this method is
	// queried directly from the bootloader.
	GetNextBootRootFSName() (string, error)

	// Update the bootloader configuration to mark the
	// currently-booted rootfs as having booted successfully.
	MarkCurrentBootSuccessful() error
}

// Factory method that returns a new bootloader for the given partition
var getBootloader = getBootloaderImpl

func getBootloaderImpl(p *Partition) (bootloader bootLoader, err error) {
	// try uboot
	if uboot := newUboot(p); uboot != nil {
		return uboot, nil
	}

	// no, try grub
	if grub := newGrub(p); grub != nil {
		return grub, nil
	}

	// no, weeeee
	return nil, ErrBootloader
}

type bootloaderType struct {
	partition *Partition

	// each rootfs partition has a corresponding u-boot directory named
	// from the last character of the partition name ('a' or 'b').
	currentRootfs string
	otherRootfs   string

	// full path to rootfs-specific assets on boot partition
	currentBootPath string
	otherBootPath   string

	// FIXME: this should /boot if possible
	// the dir that the bootloader lives in (e.g. /boot/uboot)
	bootloaderDir string
}

func newBootLoader(partition *Partition, bootloaderDir string) *bootloaderType {
	// FIXME: is this the right thing to do? i.e. what should we do
	//        on a single partition system?
	if partition.otherRootPartition() == nil {
		return nil
	}

	// full label of the system {system-a,system-b}
	currentLabel := partition.rootPartition().name
	otherLabel := partition.otherRootPartition().name

	// single letter description of the rootfs {a,b}
	currentRootfs := string(currentLabel[len(currentLabel)-1])
	otherRootfs := string(otherLabel[len(otherLabel)-1])

	return &bootloaderType{
		partition: partition,

		currentRootfs: currentRootfs,
		otherRootfs:   otherRootfs,

		// the paths that the kernel/initramfs are loaded, e.g.
		// /boot/uboot/a
		currentBootPath: filepath.Join(bootloaderDir, currentRootfs),
		otherBootPath:   filepath.Join(bootloaderDir, otherRootfs),

		// the base bootloader dir, e.g. /boot/uboot or /boot/grub
		bootloaderDir: bootloaderDir,
	}
}

// Return true if the next boot will use the other rootfs
// partition.
func isNextBootOther(bootloader bootLoader) bool {
	value, err := bootloader.GetBootVar(bootloaderBootmodeVar)
	if err != nil {
		return false
	}

	if value != bootloaderBootmodeTry {
		return false
	}

	fsname, err := bootloader.GetNextBootRootFSName()
	if err != nil {
		return false
	}

	if fsname == bootloader.GetOtherRootFSName() {
		return true
	}

	return false
}

// FIXME:
// - transition from old grub to /boot/grub/{a,b}, i.e. create dirs if missing
// - populate kernel if missing
// - rewrite grub.cfg for new static one
//   (plus support adding grub flags e.g. for azure)
// - ensure rollback still works
func (b *bootloaderType) SyncBootFiles() (err error) {
	srcDir := b.currentBootPath
	destDir := b.otherBootPath

	return helpers.RSyncWithDelete(srcDir, destDir)
}

// FIXME:
// - if this fails it will never be re-tried because the "other" patition
//   is updated to revision-N in /etc/system-image/channel.ini
//   so the system only downloads from revision-N onwards even though the
//   complete update was not applied (i.e. kernel missing)
func (b *bootloaderType) HandleAssets() (err error) {
	// check if we have anything, if there is no hardware yaml, there is nothing
	// to process.
	hardware, err := b.partition.hardwareSpec()
	if err == ErrNoHardwareYaml {
		return nil
	} else if err != nil {
		return err
	}
	// ensure to remove the file if there are no errors
	defer func() {
		if err == nil {
			os.Remove(b.partition.hardwareSpecFile)
		}
	}()

	/*
		// validate bootloader
		if hardware.Bootloader != b.Name() {
			return fmt.Errorf(
				"bootloader is of type %s but hardware spec requires %s",
				b.Name(),
				hardware.Bootloader)
		}
	*/

	// validate partition layout
	if b.partition.dualRootPartitions() && hardware.PartitionLayout != bootloaderSystemAB {
		return fmt.Errorf("hardware spec requires dual root partitions")
	}

	// ensure we have the destdir
	destDir := b.otherBootPath
	if err := os.MkdirAll(destDir, dirMode); err != nil {
		return err
	}

	// install kernel+initrd
	for _, file := range []string{hardware.Kernel, hardware.Initrd} {

		if file == "" {
			continue
		}

		// expand path
		path := filepath.Join(b.partition.cacheDir(), file)

		if !helpers.FileExists(path) {
			return fmt.Errorf("can not find file %s", path)
		}

		// ensure we remove the dir later
		defer func() {
			if err == nil {
				os.RemoveAll(filepath.Dir(path))
			}
		}()

		if err := runCommand("/bin/cp", path, destDir); err != nil {
			return err
		}
	}

	// TODO: look at the OEM package for dtb changes too once that is
	//       fully speced

	// install .dtb files
	dtbSrcDir := filepath.Join(b.partition.cacheDir(), hardware.DtbDir)
	// ensure there is a DtbDir specified
	if hardware.DtbDir != "" && helpers.FileExists(dtbSrcDir) {
		// ensure we cleanup the source dir
		defer func() {
			if err == nil {
				os.RemoveAll(dtbSrcDir)
			}
		}()

		dtbDestDir := filepath.Join(destDir, "dtbs")
		if err := os.MkdirAll(dtbDestDir, dirMode); err != nil {
			return err
		}

		files, err := filepath.Glob(filepath.Join(dtbSrcDir, "*"))
		if err != nil {
			return err
		}

		for _, file := range files {
			if err := runCommand("/bin/cp", file, dtbDestDir); err != nil {
				return err
			}
		}
	}

	flashAssetsDir := b.partition.flashAssetsDir()

	if helpers.FileExists(flashAssetsDir) {
		// FIXME: we don't currently do anything with the
		// MLO + uImage files since they are not specified in
		// the hardware spec. So for now, just remove them.

		if err := os.RemoveAll(flashAssetsDir); err != nil {
			return err
		}
	}

	return err
}
