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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func HasRun() bool {
	return osutil.FileExists(dirs.SnapFirstBootStamp)
}

var (
	// ErrNotFirstBoot is an error that indicates that the first boot has already
	// run
	ErrNotFirstBoot = errors.New("this is not your first boot")
)

// FirstBoot checks whether it's the first boot, and if so enables the
// first ethernet device and runs gadgetConfig (as well as flagging that
// it run)
func FirstBoot() error {
	if HasRun() {
		return ErrNotFirstBoot
	}
	defer stampFirstBoot()

	return enableFirstEther()
}

func stampFirstBoot() error {
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
var ethdir = "/etc/network/interfaces.d"
var ifup = "/sbin/ifup"

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

	if err := osutil.AtomicWriteFile(ethfile, []byte(data), 0644, 0); err != nil {
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
