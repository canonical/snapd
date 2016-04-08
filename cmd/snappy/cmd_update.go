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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snappy"
)

type cmdUpdate struct {
	DisableGC  bool `long:"no-gc"`
	AutoReboot bool `long:"automatic-reboot"`
	Positional struct {
		PackageName string `positional-arg-name:"package name"`
	} `positional-args:"yes"`
}

func init() {
	arg, err := parser.AddCommand("update",
		i18n.G("Update all installed parts"),
		i18n.G("Ensures system is running with latest parts"),
		&cmdUpdate{})
	if err != nil {
		logger.Panicf("Unable to update: %v", err)
	}
	addOptionDescription(arg, "no-gc", i18n.G("Do not clean up old versions of the package."))
	addOptionDescription(arg, "automatic-reboot", i18n.G("Reboot if necessary to be on the latest running system."))
	addOptionDescription(arg, "package name", i18n.G("The Package to update"))
}

const (
	shutdownCmd     = "/sbin/shutdown"
	shutdownTimeout = "+10"
)

// TRANSLATORS: please keep this under 80 characters if possible
var shutdownMsg = i18n.G("Snappy needs to reboot to finish an update. Defer with 'sudo shutdown -c'.")

func (x *cmdUpdate) Execute(args []string) (err error) {
	return withMutexAndRetry(x.doUpdate)
}

func (x *cmdUpdate) doUpdate() error {
	// FIXME: handle (more?) args
	flags := snappy.DoInstallGC
	if x.DisableGC {
		flags = 0
	}

	var err error
	var updates []*snappy.Snap
	if x.Positional.PackageName != "" {
		updates, err = snappy.Update(x.Positional.PackageName, flags, progress.MakeProgressBar())
	} else {
		updates, err = snappy.UpdateAll(flags, progress.MakeProgressBar())
	}
	if err != nil {
		return err
	}

	if len(updates) > 0 {
		showVerboseList(updates, os.Stdout)
	}

	if x.AutoReboot {
		installed, err := snappy.ListInstalled()
		if err != nil {
			return err
		}

		var rebootTriggers []string
		for _, part := range installed {
			if part.NeedsReboot() {
				rebootTriggers = append(rebootTriggers, part.Name())
			}
		}

		if len(rebootTriggers) != 0 {
			// TRANSLATORS: the %s shows a comma separated list
			//              of package names
			fmt.Printf(i18n.G("Rebooting to satisfy updates for %s\n"), strings.Join(rebootTriggers, ", "))
			cmd := exec.Command(shutdownCmd, shutdownTimeout, "-r", shutdownMsg)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to auto reboot: %s", out)
			}
		}
	}

	return nil
}
