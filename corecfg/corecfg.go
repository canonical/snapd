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

// services that can be disabled
var services = []string{"ssh", "rsyslog"}

// coreSupportAvailable checks that the system has the core-support
// interface. An error is returned if this is not the case
func coreSupportAvailable() error {
	_, err := systemd.SystemctlCmd("--version")
	return err
}

func snapctlGet(key string) ([]byte, error) {
	output, err := exec.Command("snapctl", "get", key).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("Cannot run snapctl: %s", osutil.OutputErr(output, err))
	}
	return output, nil
}

func Run() error {
	if release.OnClassic {
		return fmt.Errorf("cannot run core-configure on classic distribution")
	}

	// see if it makes sense to run at all
	if err := coreSupportAvailable(); err != nil {
		return fmt.Errorf("Cannot run systemctl - is core-support available: %s\n", err)
	}

	// service handling
	for _, service := range services {
		output, err := snapctlGet(fmt.Sprintf("service.%s.disable", service))
		if err != nil {
			return err
		}
		if output != nil {
			if err := switchDisableService(service, string(output)); err != nil {
				return err
			}
		}
	}

	// system.power-key-action
	output, err := snapctlGet("system.power-key-action")
	if err != nil {
		return err
	}
	if output != nil {
		switchHandlePowerKey(string(output))
	}

	piConfig := os.Getenv("TEST_UBOOT_CONFIG")
	if piConfig == "" {
		piConfig = "/boot/uboot/config.txt"
	}
	if osutil.FileExists(piConfig) {
		// snapctl can actually give us the whole dict in
		// JSON, in a single call; use that instead of this.
		config := map[string]string{}
		for key := range piConfigKeys {
			value, err := snapctlGet(fmt.Sprintf("pi-config.%s", strings.Replace(key, "_", "-", -1)))
			if err != nil {
				return err
			}
			config[key] = string(value)
		}
		if err := updatePiConfig(piConfig, config); err != nil {
			return err
		}
	}

	return nil
}
