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

	"launchpad.net/snappy/priv"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/snappy"
)

type cmdUpdate struct {
	DisableGC  bool `long:"no-gc" description:"Do not clean up old versions of the package."`
	AutoReboot bool `long:"automatic-reboot" description:"Reboot if necessary to be on the latest running system."`
}

func init() {
	var cmdUpdateData cmdUpdate
	_, _ = parser.AddCommand("update",
		"Update all installed parts",
		"Ensures system is running with latest parts",
		&cmdUpdateData)
}

const (
	shutdownCmd     = "/sbin/shutdown"
	shutdownTimeout = "+10"
	shutdownMsg     = "snappy autopilot triggered a reboot to boot into an up to date system" +
		"-- temprorarily disable the reboot by running 'shutdown -c'"
)

func (x *cmdUpdate) Execute(args []string) (err error) {
	privMutex := priv.New()
	if err := privMutex.TryLock(); err != nil {
		return err
	}
	defer privMutex.Unlock()

	// FIXME: handle (more?) args
	flags := snappy.DoInstallGC
	if x.DisableGC {
		flags = 0
	}

	updates, err := snappy.ListUpdates()
	if err != nil {
		return err
	}

	for _, part := range updates {
		pbar := progress.NewTextProgress(part.Name())

		fmt.Printf("Installing %s (%s)\n", part.Name(), part.Version())
		if _, err := part.Install(pbar, flags); err != nil {
			return err
		}
		if err := snappy.GarbageCollect(part.Name(), flags); err != nil {
			return err
		}
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
			fmt.Println("Rebooting to satisfy updates for", strings.Join(rebootTriggers, ", "))
			cmd := exec.Command(shutdownCmd, shutdownTimeout, "-r", shutdownMsg)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to auto reboot: %s", out)
			}
		}
	}

	return nil
}
