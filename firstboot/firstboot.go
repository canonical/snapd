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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
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

var globs = []string{"/sys/class/net/eth*", "/sys/class/net/en*"}
var nplandir = "/etc/netplan"
var enableConfig = []string{"netplan", "apply"}

func EnableFirstEther() error {
	// ensure that udev is ready and we have the net stuff
	if output, err := exec.Command("udevadm", "settle").CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}

	var eths []string
	for _, glob := range globs {
		eths, _ = filepath.Glob(glob)
		if len(eths) != 0 {
			break
		}
	}
	if len(eths) == 0 {
		logger.Noticef("no network interfaces found")
		return nil
	}
	eth := filepath.Base(eths[0])
	ethfile := filepath.Join(nplandir, "00firstboot-"+eth+".yaml")
	data := fmt.Sprintf("network:\n version: 2\n ethernets:\n  %s:\n   dhcp4: true\n", eth)

	if err := osutil.AtomicWriteFile(ethfile, []byte(data), 0644, 0); err != nil {
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
