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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/systemd"

	"gopkg.in/yaml.v2"
)

var (
	errNoSnapToConfig = errors.New("configuring an invalid snappy package")
)

func wrapConfig(pkgName string, conf interface{}) ([]byte, error) {
	configWrap := map[string]map[string]interface{}{
		"config": map[string]interface{}{
			pkgName: conf,
		},
	}

	return yaml.Marshal(configWrap)
}

var activeSnapByName = ActiveSnapByName
var activeSnapsByType = ActiveSnapsByType

// OemConfig checks for an oem snap and if found applies the configuration
// set there to the system
func oemConfig() error {
	oemSnap, err := activeSnapsByType(pkg.TypeOem)
	if err != nil {
		return err
	}

	if len(oemSnap) < 1 {
		return nil
	}

	snap, ok := oemSnap[0].(Configuration)
	if !ok {
		return ErrNoOemConfiguration
	}

	for pkgName, conf := range snap.OemConfig() {
		configData, err := wrapConfig(pkgName, conf)
		if err != nil {
			return err
		}

		snap := activeSnapByName(pkgName)
		if snap == nil {
			// We want to error early as this is a disparity and oem snap
			// packaging error.
			return errNoSnapToConfig
		}

		if _, err := snap.Config(configData); err != nil {
			return err
		}
	}

	return nil
}

// FirstBoot checks whether it's the first boot, and if so enables the
// first ethernet device and runs oemConfig (as well as flagging that
// it run)
func FirstBoot() error {
	if firstBootHasRun() {
		return ErrNotFirstBoot
	}
	defer stampFirstBoot()
	defer enableFirstEther()

	return oemConfig()
}

const firstbootDir = "/var/lib/snappy/firstboot"

var stampFile = filepath.Join(firstbootDir, "stamp")

func stampFirstBoot() error {
	// filepath.Dir instead of firstbootDir directly to ease testing
	stampDir := filepath.Dir(stampFile)

	if _, err := os.Stat(stampDir); os.IsNotExist(err) {
		if err := os.MkdirAll(stampDir, 0755); err != nil {
			return err
		}
	}

	return ioutil.WriteFile(stampFile, []byte{}, 0644)
}

var globs = []string{"/sys/class/net/eth*", "/sys/class/net/en*"}
var ethdir = "/etc/network/interfaces.d"

func enableFirstEther() error {
	var eths []string
	for _, glob := range globs {
		eths, _ = filepath.Glob(glob)
		if len(eths) != 0 {
			break
		}
	}
	if len(eths) == 0 {
		return nil
	}
	eth := filepath.Base(eths[0])
	ethfile := filepath.Join(ethdir, eth)
	data := fmt.Sprintf("allow-hotplug %[1]s\niface %[1]s inet dhcp\n", eth)

	if err := helpers.AtomicWriteFile(ethfile, []byte(data), 0644); err != nil {
		return err
	}

	if _, err := systemd.SystemctlCmd("restart", "networking", "--no-block"); err != nil {
		return err
	}

	return nil
}

func firstBootHasRun() bool {
	return helpers.FileExists(stampFile)
}
