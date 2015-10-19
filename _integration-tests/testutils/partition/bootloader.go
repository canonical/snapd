// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	bootBase        = "/boot"
	ubootDir        = bootBase + "/uboot"
	grubDir         = bootBase + "/grub"
	ubootConfigFile = ubootDir + "/snappy-system.txt"
	grubConfigFile  = grubDir + "/grubenv"
)

var (
	// dependency aliasing
	filepathGlob = filepath.Glob
	// BootSystem proxies bootSystem
	BootSystem = bootSystem

	configFiles = map[string]string{"uboot": ubootConfigFile, "grub": grubConfigFile}
)

// bootSystem returns the name of the boot system, grub or uboot.
func bootSystem() (string, error) {
	matches, err := filepathGlob(bootBase + "/grub")
	if err != nil {
		return "", err
	}
	if len(matches) == 1 {
		return "grub", nil
	}
	return "uboot", nil
}

// BootDir returns the directory used by the boot system.
func BootDir(bootSystem string) string {
	if bootSystem == "grub" {
		return grubDir
	}
	return ubootDir
}

// NextBootPartition returns the partition the system will use on the next boot
// if we are upgrading. In grub systems it is the partition pointed in the boot
// config file. For uboot systems the boot config file does not change, so that
// we take the other partition in that case
func NextBootPartition() (partition string, err error) {
	m, err := Mode()
	if err != nil {
		return
	}
	if m != "try" {
		return "", errors.New("Snappy is not in try mode")
	}
	snappyab, err := confValue("snappy_ab")
	if err != nil || snappyab == "" {
		return
	}
	system, err := BootSystem()
	if err != nil {
		return
	}
	if system == "grub" {
		// in grub based systems, the boot config file is changed before
		// the update has been applied
		partition = snappyab
	} else {
		// in uboot based systems, the boot config file is not changed until
		// the update has been applied
		partition = OtherPartition(snappyab)
	}
	return
}

// Mode returns the current bootloader mode, regular or try.
func Mode() (mode string, err error) {
	return confValue("snappy_mode")
}

func confValue(key string) (partition string, err error) {
	system, err := BootSystem()
	if err != nil {
		return
	}

	bootConfigFile := configFiles[system]

	file, err := os.Open(bootConfigFile)
	if err != nil {
		return
	}

	defer file.Close()

	reader := bufio.NewReader(file)
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), key) {
			fields := strings.Split(scanner.Text(), "=")
			if len(fields) > 1 {
				partition = fields[1]
			}
			return
		}
	}
	return
}

// OtherPartition returns the backup partition, a or b.
func OtherPartition(current string) string {
	if current == "a" {
		return "b"
	}
	return "a"
}

// CurrentPartition returns the current partition, a or b.
func CurrentPartition() (partition string, err error) {
	partition, err = confValue("snappy_ab")
	if err != nil {
		return
	}
	m, err := Mode()
	if err != nil {
		return
	}
	if m == "try" {
		var system string
		system, err = BootSystem()
		if err != nil {
			return
		}
		if system == "grub" {
			// in grub based systems, the boot config file is changed before
			// the update has been applied
			partition = OtherPartition(partition)
		}
	}
	return
}
