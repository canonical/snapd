// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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

	"github.com/mvo5/uboot-go/uenv"
	"gopkg.in/check.v1"
)

const (
	bootBase        = "/boot"
	ubootDir        = bootBase + "/uboot"
	grubDir         = bootBase + "/grub"
	ubootConfigFile = ubootDir + "/uboot.env"
	grubConfigFile  = grubDir + "/grubenv"
)

var (
	// dependency aliasing
	filepathGlob = filepath.Glob
	// BootSystem proxies bootSystem
	BootSystem = bootSystem

	confValue = getConfValue

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

// Mode returns the current bootloader mode, regular or try.
func Mode() (mode string, err error) {
	return confValue("snappy_mode")
}

// OSSnapName returns the name of the OS snap.
func OSSnapName(c *check.C) string {
	snappyOS, err := snappyOS()
	c.Assert(err, check.IsNil, check.Commentf("Error getting the name of the OS snap: %s", err))
	front := strings.Split(snappyOS, "_")[0]
	return strings.Split(front, ".")[0]
}

// snappyOS returns the name of the OS snap in the form name.developer_version.snap
// This is a workaround for https://bugs.launchpad.net/snappy/+bug/1532245
func snappyOS() (string, error) {
	return confValue("snappy_os")
}

// SnappyKernel returns the name of the Kernel snap in the form name.developer_version.snap
// This is a workaround for https://bugs.launchpad.net/snappy/+bug/1532245
func SnappyKernel() (string, error) {
	return confValue("snappy_kernel")
}

func getConfValue(key string) (string, error) {
	system, err := BootSystem()
	if err != nil {
		return "", err
	}

	var value string
	if system == "grub" {
		value, err = getGrubConfValue(key)
	} else if system == "uboot" {
		value, err = getUbootConfValue(key)
	} else {
		panic(fmt.Sprintf("unknown boot system: %s", system))
	}
	return value, err
}

func getGrubConfValue(key string) (string, error) {
	bootConfigFile := configFiles["grub"]
	file, err := os.Open(bootConfigFile)
	if err != nil {
		return "", err
	}

	defer file.Close()

	reader := bufio.NewReader(file)
	scanner := bufio.NewScanner(reader)

	var value string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), key) {
			fields := strings.Split(scanner.Text(), "=")
			if len(fields) > 1 {
				value = fields[1]
			}
			break
		}
	}
	return value, nil
}

func getUbootConfValue(key string) (string, error) {
	bootConfigFile := configFiles["uboot"]
	env, err := uenv.Open(bootConfigFile)
	if err != nil {
		return "", err
	}

	return env.Get(key), nil
}
