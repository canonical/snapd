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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"launchpad.net/snappy/helpers"

	"github.com/mvo5/goconfigparser"
)

const (
	bootloaderUbootDirReal        = "/boot/uboot"
	bootloaderUbootConfigFileReal = "/boot/uboot/uEnv.txt"

	// File created by u-boot itself when
	// bootloaderBootmodeTry == "try" which the
	// successfully booted system must remove to flag to u-boot that
	// this partition is "good".
	bootloaderUbootStampFileReal = "/boot/uboot/snappy-stamp.txt"

	// the main uEnv.txt u-boot config file sources this snappy
	// boot-specific config file.
	bootloaderUbootEnvFileReal = "/boot/uboot/snappy-system.txt"
)

// var to make it testable
var (
	bootloaderUbootDir        = bootloaderUbootDirReal
	bootloaderUbootConfigFile = bootloaderUbootConfigFileReal
	bootloaderUbootStampFile  = bootloaderUbootStampFileReal
	bootloaderUbootEnvFile    = bootloaderUbootEnvFileReal
	atomicFileUpdate          = atomicFileUpdateImpl
)

const bootloaderNameUboot bootloaderName = "u-boot"

type uboot struct {
	bootloaderType
}

// Stores a Name and a Value to be added as a name=value pair in a file.
// TODO convert to map
type configFileChange struct {
	Name  string
	Value string
}

// newUboot create a new Grub bootloader object
func newUboot(partition *Partition) bootLoader {
	if !helpers.FileExists(bootloaderUbootConfigFile) {
		return nil
	}

	b := newBootLoader(partition, bootloaderUbootDir)
	if b == nil {
		return nil
	}
	u := uboot{bootloaderType: *b}

	return &u
}

func (u *uboot) Name() bootloaderName {
	return bootloaderNameUboot
}

// ToggleRootFS make the U-Boot bootloader switch rootfs's.
//
// Approach:
//
// - Assume the device's installed version of u-boot supports
//   CONFIG_SUPPORT_RAW_INITRD (that allows u-boot to boot a
//   standard initrd+kernel on the fat32 disk partition).
// - Copy the "other" rootfs's kernel+initrd to the boot partition,
//   renaming them in the process to ensure the next boot uses the
//   correct versions.
func (u *uboot) ToggleRootFS(otherRootfs string) (err error) {

	// If the file exists, update it. Otherwise create it.
	//
	// The file _should_ always exist, but since it's on a writable
	// partition, it's possible the admin removed it by mistake. So
	// recreate to allow the system to boot!
	changes := []configFileChange{
		configFileChange{Name: bootloaderRootfsVar,
			Value: string(otherRootfs),
		},
		configFileChange{Name: bootloaderBootmodeVar,
			Value: bootloaderBootmodeTry,
		},
	}

	return modifyNameValueFile(bootloaderUbootEnvFile, changes)
}

func (u *uboot) GetBootVar(name string) (value string, err error) {
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadFile(bootloaderUbootEnvFile); err != nil {
		return "", nil
	}

	return cfg.Get("", name)
}

func (u *uboot) GetNextBootRootFSName() (label string, err error) {
	value, err := u.GetBootVar(bootloaderRootfsVar)
	if err != nil {
		// should never happen
		return "", err
	}

	return value, nil
}

// FIXME: put into utils package
func readLines(path string) (lines []string, err error) {

	file, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

// FIXME: put into utils package
func writeLines(lines []string, path string) (err error) {

	file, err := os.Create(path)

	if err != nil {
		return err
	}

	defer func() {
		e := file.Close()
		if err == nil {
			err = e
		}
	}()

	writer := bufio.NewWriter(file)

	for _, line := range lines {
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	return file.Sync()
}

func (u *uboot) MarkCurrentBootSuccessful(currentRootfs string) (err error) {
	changes := []configFileChange{
		configFileChange{Name: bootloaderBootmodeVar,
			Value: bootloaderBootmodeSuccess,
		},
		configFileChange{Name: bootloaderRootfsVar,
			Value: string(currentRootfs),
		},
	}

	if err := modifyNameValueFile(bootloaderUbootEnvFile, changes); err != nil {
		return err
	}

	return os.RemoveAll(bootloaderUbootStampFile)
}

// Write lines to file atomically. File does not have to preexist.
// FIXME: put into utils package
func atomicFileUpdateImpl(file string, lines []string) (err error) {
	tmpFile := fmt.Sprintf("%s.NEW", file)

	// XXX: if go switches to use aio_fsync, we need to open the dir for writing
	dir, err := os.Open(filepath.Dir(file))
	if err != nil {
		return err
	}
	defer dir.Close()

	if err := writeLines(lines, tmpFile); err != nil {
		return err
	}

	// atomic update
	if err := os.Rename(tmpFile, file); err != nil {
		return err
	}

	return dir.Sync()
}

// Rewrite the specified file, applying the specified set of changes.
// Lines not in the changes slice are left alone.
// If the original file does not contain any of the name entries (from
// the corresponding configFileChange objects), those entries are
// appended to the file.
//
// FIXME: put into utils package
// FIXME: improve logic
func modifyNameValueFile(file string, changes []configFileChange) (err error) {
	var updated []configFileChange

	lines, err := readLines(file)
	if err != nil {
		return err
	}

	var new []string
	// we won't write to a file if we don't need to.
	updateNeeded := false

	for _, line := range lines {
		for _, change := range changes {
			if strings.HasPrefix(line, fmt.Sprintf("%s=", change.Name)) {
				value := strings.SplitN(line, "=", 2)[1]
				// updated is used later to see if you had the originally requested
				// value.
				updated = append(updated, change)
				if value != change.Value {
					line = fmt.Sprintf("%s=%s", change.Name, change.Value)
					updateNeeded = true
				}
			}
		}
		new = append(new, line)
	}

	lines = new

	for _, change := range changes {
		got := false
		for _, update := range updated {
			if update.Name == change.Name {
				got = true
				break
			}
		}

		if !got {
			updateNeeded = true

			// name/value pair did not exist in original
			// file, so append
			lines = append(lines, fmt.Sprintf("%s=%s",
				change.Name, change.Value))
		}
	}

	if updateNeeded {
		return atomicFileUpdate(file, lines)
	}

	return nil
}

func (u *uboot) BootDir() string {
	return bootloaderUbootDir
}
