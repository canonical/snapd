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
	sys "syscall"
	"time"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/lockfile"
	"github.com/ubuntu-core/snappy/snappy"
)

func isAutoUpdateRunning() bool {
	unitName := "snappy-autopilot"
	bs, err := exec.Command("systemctl", "show", "--property=SubState", unitName).CombinedOutput()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(bs)) == "SubState=running"
}

// withMutexAndRetry runs the given function with a filelock mutex and provides
// automatic re-try and helpful messages if the lock is already taken
//
// FIXME: This function is a left over from the old CLI and a major
// red flag. No client-side locks should exist.
func withMutexAndRetry(f func() error) error {
	if sys.Getuid() != 0 {
		return snappy.ErrNeedRoot
	}
	for {
		err := lockfile.WithLock(dirs.SnapLockFile, f)
		// if already locked, auto-retry
		if err == lockfile.ErrAlreadyLocked {
			var msg string
			if isAutoUpdateRunning() {
				// FIXME: we could even do a
				//    journalctl -u snappy-autopilot
				// here

				// TRANSLATORS: please keep each line under 80 characters.
				msg = i18n.G(
					`Snappy is updating your system in the background. This may take some minutes.
Will try again in %d seconds...
Press ctrl-c to cancel.
`)
			} else {
				msg = i18n.G(
					`Another snappy is running, will try again in %d seconds...
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
