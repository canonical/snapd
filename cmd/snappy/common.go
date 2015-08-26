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
	"os/exec"
	"strings"
	"time"

	"launchpad.net/snappy/i18n"
	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/priv"

	"github.com/jessevdk/go-flags"
)

const snappyLockFile = "/run/snappy.lock"

func isAutoPilotRunning() bool {
	unitName := "snappy-autopilot"
	bs, err := exec.Command("systemctl", "show", "--property=SubState", unitName).CombinedOutput()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(bs)) == "SubState=running"
}

// withMutex runs the given function with a filelock mutex and provides
// automatic re-try and helpful messages if the lock is already taken
func withMutex(f func() error) error {
	for {
		err := priv.WithMutex(snappyLockFile, f)
		// if already locked, auto-retry
		if err == priv.ErrAlreadyLocked {
			var msg string
			if isAutoPilotRunning() {
				// FIXME: we could even do a
				//    journalctl -u snappy-autopilot
				// here
				msg = i18n.G(
					`The snappy autopilot is updating your system in the background. This may
take some minutes. Will try again in %d seconds...
Press ctrl-c to cancel.
`)
			} else {
				msg = i18n.G(
					`Another snappy is running in the background, will try again in %d seconds...
Press ctrl-c to cancel.
`)
			}
			// wait a wee bit
			wait := 5
			fmt.Printf(msg, wait)
			time.Sleep(time.Duration(wait) * time.Second)
			continue
		}

		return err
	}
}

// addOptionDescription will try to find the given longName in the
// options and arguments of the given Command and add a description
//
// if the longName is not found it will panic
func addOptionDescription(arg *flags.Command, longName, description string) {
	for _, opt := range arg.Options() {
		if opt.LongName == longName {
			opt.Description = description
			return
		}
	}
	for _, opt := range arg.Args() {
		if opt.Name == longName {
			opt.Description = description
			return
		}
	}

	logger.Panicf("can not set option description for %#v", longName)
}
