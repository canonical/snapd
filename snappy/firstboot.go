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

	"launchpad.net/snappy/logger"

	"gopkg.in/yaml.v2"
)

const oemConfigRemovable = "removable"

var (
	errNoConf         = errors.New("no conf")
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
var installedSnapsByType = InstalledSnapsByType

// OemConfig checks for an oem snap and if found applies the configuration
// set there to the system flagging that it run so it is effectively only
// run once
func OemConfig() error {
	if firstBootHasRun() {
		return ErrNotFirstBoot
	}
	defer stampFirstBoot()

	oemSnap, err := installedSnapsByType(SnapTypeOem)
	if err != nil {
		return logger.LogError(err)
	}

	if len(oemSnap) < 1 {
		return errors.New("no oem snap")
	}

	snap, ok := oemSnap[0].(Configuration)
	if !ok {
		return errors.New("no config")
	}

	for pkgName, conf := range snap.OemConfig() {
		fmt.Println(pkgName)
		configData, err := wrapConfig(pkgName, conf)
		if err != nil {
			return logger.LogError(err)
		}

		snap := activeSnapByName(pkgName)
		if snap == nil {
			return errNoSnapToConfig
		}

		if _, err := snap.Config(configData); err != nil {
			return logger.LogError(err)
		}
	}

	return nil
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

func firstBootHasRun() bool {
	_, err := os.Stat(stampFile)

	// TODO support all the variations of this file being misplaced.
	return err == nil
}
