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
	"os"
	"os/exec"
	"time"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
)

// rebootArgsPath is used so we can mock the path easily in tests
var rebootArgsPath = "/run/systemd/reboot-param"
var bootloaderFind = bootloader.Find

type RebootAction int

const (
	RebootReboot RebootAction = iota
	RebootHalt
	RebootPoweroff
)

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

var (
	shutdownMsg = i18n.G("reboot scheduled to update the system")
	haltMsg     = i18n.G("system halt scheduled")
	poweroffMsg = i18n.G("system poweroff scheduled")

	// testingRebootItself is set to true when we want to unit
	// test the Reboot function.
	testingRebootItself = false
)

func getRebootArguments(rebootInfo *RebootInfo) (string, error) {
	if rebootInfo == nil {
		return "", nil
	}

	bl, err := bootloaderFind("", rebootInfo.BootloaderOptions)
	if err != nil {
		return "", fmt.Errorf("cannot resolve bootloader: %v", err)
	}
	if rbl, ok := bl.(bootloader.RebootBootloader); ok {
		return rbl.GetRebootArguments()
	}
	return "", nil
}

func Reboot(action RebootAction, rebootDelay time.Duration, rebootInfo *RebootInfo) error {
	if osutil.IsTestBinary() && !testingRebootItself {
		panic("Reboot must be mocked in tests")
	}

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
	rebArgs, err := getRebootArguments(rebootInfo)
	if err != nil {
		return err
	}
	if rebArgs != "" {
		if err := osutil.AtomicWriteFile(rebootArgsPath,
			[]byte(rebArgs+"\n"), 0644, 0); err != nil {
			return err
		}
	}

	cmd := exec.Command("shutdown", arg, fmt.Sprintf("+%d", mins), msg)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(rebootArgsPath)
		return osutil.OutputErr(out, err)
	}
	return nil
}
