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

package firstboot

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func HasRun() bool {
	return osutil.FileExists(dirs.SnapFirstBootStamp)
}

func StampFirstBoot() error {
	// filepath.Dir instead of firstbootDir directly to ease testing
	stampDir := filepath.Dir(dirs.SnapFirstBootStamp)

	if _, err := os.Stat(stampDir); os.IsNotExist(err) {
		if err := os.MkdirAll(stampDir, 0755); err != nil {
			return err
		}
	}

	return osutil.AtomicWriteFile(dirs.SnapFirstBootStamp, []byte{}, 0644, 0)
}

var netplanConfigFile = "/etc/netplan/00-initial-config.yaml"
var enableConfig = []string{"netplan", "apply"}

var netplanConfigData = `
network:
 version: 2
 ethernets:
   all:
    match:
     name: "*"
    dhcp4: true
`

// InitialNetworkConfig writes and applies a netplan config that
// enables dhcp on all wired interfaces. In the long run this should
// be run as part of the config-changed hook and read the snap's
// config to determine the netplan config to write.
func InitialNetworkConfig() error {
	if err := osutil.AtomicWriteFile(netplanConfigFile, []byte(netplanConfigData), 0644, 0); err != nil {
		return err
	}

	enable := exec.Command(enableConfig[0], enableConfig[1:]...)
	enable.Stdout = os.Stdout
	enable.Stderr = os.Stderr
	if err := enable.Run(); err != nil {
		return err
	}

	return nil
}
