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
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/progress"

	"gopkg.in/yaml.v2"
)

var (
	errNoSnapToConfig   = errors.New("configuring an invalid snappy package")
	errNoSnapToActivate = errors.New("activating an invalid snappy package")
)

func wrapConfig(pkgName string, conf interface{}) ([]byte, error) {
	configWrap := map[string]map[string]interface{}{
		"config": map[string]interface{}{
			pkgName: conf,
		},
	}

	return yaml.Marshal(configWrap)
}

var newPartMap = newPartMapImpl

func newPartMapImpl() (map[string]Part, error) {
	repo := NewMetaLocalRepository()
	all, err := repo.All()
	if err != nil {
		return nil, err
	}

	m := make(map[string]Part, 2*len(all))
	for _, part := range all {
		m[FullName(part)] = part
		m[BareName(part)] = part
	}

	return m, nil
}

// OemConfig checks for an oem snap and if found applies the configuration
// set there to the system
func oemConfig() error {
	oem, err := getOem()
	if err != nil || oem == nil {
		return err
	}

	partMap, err := newPartMap()
	if err != nil {
		return err
	}

	pb := progress.MakeProgressBar()
	for _, pkgName := range oem.OEM.Software.BuiltIn {
		part, ok := partMap[pkgName]
		if !ok {
			return errNoSnapToActivate
		}
		snap, ok := part.(*SnapPart)
		if !ok {
			return errNoSnapToActivate
		}
		snap.activate(false, pb)
	}

	for pkgName, conf := range oem.Config {
		snap, ok := partMap[pkgName]
		if !ok {
			// We want to error early as this is a disparity and oem snap
			// packaging error.
			return errNoSnapToConfig
		}

		configData, err := wrapConfig(pkgName, conf)
		if err != nil {
			return err
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

// NOTE: if you change stampFile, update the condition in
// ubuntu-snappy.firstboot.service to match
var stampFile = "/var/lib/snappy/firstboot/stamp"

func stampFirstBoot() error {
	// filepath.Dir instead of firstbootDir directly to ease testing
	stampDir := filepath.Dir(stampFile)

	if _, err := os.Stat(stampDir); os.IsNotExist(err) {
		if err := os.MkdirAll(stampDir, 0755); err != nil {
			return err
		}
	}

	return helpers.AtomicWriteFile(stampFile, []byte{}, 0644, 0)
}

var globs = []string{"/sys/class/net/eth*", "/sys/class/net/en*"}
var ethdir = "/etc/network/interfaces.d"
var ifup = "/sbin/ifup"

func enableFirstEther() error {
	oem, _ := getOem()
	if oem != nil && oem.OEM.SkipIfupProvisioning {
		return nil
	}

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

	if err := helpers.AtomicWriteFile(ethfile, []byte(data), 0644, 0); err != nil {
		return err
	}

	ifup := exec.Command(ifup, eth)
	ifup.Stdout = os.Stdout
	ifup.Stderr = os.Stderr
	if err := ifup.Run(); err != nil {
		return err
	}

	return nil
}

func firstBootHasRun() bool {
	return helpers.FileExists(stampFile)
}
