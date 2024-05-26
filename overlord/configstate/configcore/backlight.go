// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package configcore

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.system.disable-backlight-service"] = true
}

func validateBacklightServiceSettings(tr ConfGetter) error {
	return validateBoolFlag(tr, "system.disable-backlight-service")
}

type backlightSysdLogger struct{}

func (l *backlightSysdLogger) Notify(status string) {
	fmt.Fprintf(Stderr, "sysd: %s\n", status)
}

// systemd-backlight service has no installation config. It's started when needed via udev.
// So, systemctl enable/disable/start/stop are invalid commands. After masking/unmasking
// the service, rebooting is required for making new setting work
func handleBacklightServiceConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	var sysd systemd.Systemd
	const serviceName = "systemd-backlight@.service"
	if opts != nil {
		sysd = systemd.NewEmulationMode(opts.RootDir)
	} else {
		sysd = systemd.NewUnderRoot(dirs.GlobalRootDir, systemd.SystemMode, &backlightSysdLogger{})
	}
	output := mylog.Check2(coreCfg(tr, "system.disable-backlight-service"))

	if output != "" {
		switch output {
		case "true":
			return sysd.Mask(serviceName)
		case "false":
			return sysd.Unmask(serviceName)
		default:
			return fmt.Errorf("unsupported disable-backlight-service option: %q", output)
		}
	}
	return nil
}
