// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package boot

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
)

func getRebootParamPathRuntime() string {
	return "/run/systemd/reboot-param"
}

// Get absolute dirs via variables so we can mock in tests
var GetRebootParamPath = getRebootParamPathRuntime

type RebootAction int

func (a RebootAction) String() string {
	switch a {
	case RebootReboot:
		return "system reboot"
	case RebootHalt:
		return "system halt"
	case RebootPoweroff:
		return "system poweroff"
	default:
		panic(fmt.Sprintf("unknown reboot action %d", a))
	}
}

const (
	RebootReboot RebootAction = iota
	RebootHalt
	RebootPoweroff
)

var (
	shutdownMsg = i18n.G("reboot scheduled to update the system")
	haltMsg     = i18n.G("system halt scheduled")
	poweroffMsg = i18n.G("system poweroff scheduled")
)

func Reboot(action RebootAction, rebootDelay time.Duration) error {
	if rebootDelay < 0 {
		rebootDelay = 0
	}
	mins := int64(rebootDelay / time.Minute)
	var arg, msg string
	switch action {
	case RebootReboot:
		arg = "-r"
		msg = shutdownMsg
	case RebootHalt:
		arg = "--halt"
		msg = haltMsg
	case RebootPoweroff:
		arg = "--poweroff"
		msg = poweroffMsg
	default:
		return fmt.Errorf("unknown reboot action: %v", action)
	}

	// Use reboot arguments if required by the bootloader
	// TODO: find dynamically the root/role?
	bl, err := bootloader.Find("", &bootloader.Options{Role: bootloader.RoleRunMode})
	if err == nil {
		if rebArgBl, ok := bl.(bootloader.RebootArgumentsBootloader); ok {
			rebArgs := rebArgBl.GetRebootArguments()
			if rebArgs != "" {
				if err := osutil.AtomicWriteFile(GetRebootParamPath(),
					[]byte(rebArgs+"\n"), 0644, 0); err != nil {
					return err
				}
			}
		}
	}

	cmd := exec.Command("shutdown", arg, fmt.Sprintf("+%d", mins), msg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(out, err)
	}
	return nil
}
