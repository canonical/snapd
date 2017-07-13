// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package corecfg

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/systemd"
)

var (
	Stdout = os.Stdout
	Stderr = os.Stderr
)

// ensureSupportInterface checks that the system has the core-support
// interface. An error is returned if this is not the case
func ensureSupportInterface() error {
	_, err := systemd.SystemctlCmd("--version")
	return err
}

func snapctlGet(key string) (string, error) {
	raw, err := exec.Command("snapctl", "get", key).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cannot run snapctl: %s", osutil.OutputErr(raw, err))
	}

	output := strings.TrimRight(string(raw), "\n")
	return output, nil
}

func Run() error {
	// see if it makes sense to run at all
	if release.OnClassic {
		return fmt.Errorf("cannot run core-configure on classic distribution")
	}
	if err := ensureSupportInterface(); err != nil {
		return fmt.Errorf("cannot run systemctl - core-support interface seems disconnected: %v", err)
	}

	// handle the various core config options:
	// service.*.disable
	if err := handleServiceDisableConfiguration(); err != nil {
		return err
	}
	// system.power-key-action
	if err := handlePowerButtonConfiguration(); err != nil {
		return err
	}
	// pi-config.*
	if err := handlePiConfiguration(); err != nil {
		return err
	}

	return nil
}
