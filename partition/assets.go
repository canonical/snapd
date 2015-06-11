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
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

// Representation of the yaml in the hardwareSpecFile
type hardwareSpecType struct {
	Kernel          string         `yaml:"kernel"`
	Initrd          string         `yaml:"initrd"`
	DtbDir          string         `yaml:"dtbs"`
	PartitionLayout string         `yaml:"partition-layout"`
	Bootloader      bootloaderName `yaml:"bootloader"`
}

var (
	// ErrNoHardwareYaml is returned when no hardware yaml is found in
	// the update, this means that there is nothing to process with regards
	// to device parts.
	ErrNoHardwareYaml = errors.New("no hardware.yaml")

	// Declarative specification of the type of system which specifies such
	// details as:
	//
	// - the location of initrd+kernel within the system-image archive.
	// - the location of hardware-specific .dtb files within the
	//   system-image archive.
	// - the type of bootloader that should be used for this system.
	// - expected system partition layout (single or dual rootfs's).
	hardwareSpecFileReal = filepath.Join(cacheDir, "hardware.yaml")

	// useful to override in the tests
	hardwareSpecFile = hardwareSpecFileReal

	// Directory that _may_ get automatically created on unpack that
	// contains updated hardware-specific boot assets (such as initrd,
	// kernel)
	assetsDir = filepath.Join(cacheDir, "assets")

	// Directory that _may_ get automatically created on unpack that
	// contains updated hardware-specific assets that require flashing
	// to the disk (such as uBoot, MLO)
	flashAssetsDir = filepath.Join(cacheDir, "flashtool-assets")
)

func readHardwareSpec() (*hardwareSpecType, error) {
	var h hardwareSpecType

	data, err := ioutil.ReadFile(hardwareSpecFile)
	// if hardware.yaml does not exist it just means that there was no
	// device part in the update.
	if os.IsNotExist(err) {
		return nil, ErrNoHardwareYaml
	} else if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal([]byte(data), &h); err != nil {
		return nil, err
	}

	return &h, nil
}
