// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package wait

import (
	"fmt"
	"regexp"
	"time"

	check "gopkg.in/check.v1"

	"launchpad.net/snappy/_integration-tests/testutils/common"
)

var (
	// dependency aliasing
	execCommand = common.ExecCommand
	// ForCommand dep alias
	ForCommand     = forCommand
	maxWaitRetries = 100
)

// ForActiveService uses ForCommand to check for an active service
func ForActiveService(c *check.C, serviceName string) (err error) {
	return ForCommand(c, "ActiveState=active\n", "systemctl", "show", "-p", "ActiveState", serviceName)
}

// forCommand keeps trying to execute the given command to get an output that
// matches the given pattern until it is obtained or the maximun number of
// retries is executed
func forCommand(c *check.C, outputPattern string, cmds ...string) (err error) {
	output := execCommand(c, cmds...)

	re := regexp.MustCompile(outputPattern)

	if match := re.FindString(output); match != "" {
		return
	}

	checkInterval := time.Millisecond * 100
	var retries int

	ticker := time.NewTicker(checkInterval)
	tickChan := ticker.C

	for {
		select {
		case <-tickChan:
			output = execCommand(c, cmds...)
			if match := re.FindString(output); match != "" {
				ticker.Stop()
				return
			}
			retries++
			if retries >= maxWaitRetries {
				ticker.Stop()
				return fmt.Errorf("Pattern not found in command output")
			}
		}
	}
}
