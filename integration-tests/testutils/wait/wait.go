// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
)

var (
	// dependency aliasing
	execCommand = cli.ExecCommand
	// ForCommand dep alias
	ForCommand = forCommand
	// ForFunction dep alias
	ForFunction    = forFunction
	maxWaitRetries = 100
	interval       = time.Duration(100)
)

// ForActiveService uses ForCommand to check for an active service
func ForActiveService(c *check.C, serviceName string) (err error) {
	return ForCommand(c, "ActiveState=active\n", "systemctl", "show", "-p", "ActiveState", serviceName)
}

// ForInactiveService uses ForCommand to check for an inactive service
func ForInactiveService(c *check.C, serviceName string) (err error) {
	return ForCommand(c, "ActiveState=inactive\n", "systemctl", "show", "-p", "ActiveState", serviceName)
}

// ForServerOnPort uses ForCommand to check for process listening on the given port
func ForServerOnPort(c *check.C, protocol string, port int) (err error) {
	return ForCommand(c, fmt.Sprintf(`(?sU)^.*%s .*:%d\s*(0\.0\.0\.0|::):\*\s*LISTEN.*`, protocol, port),
		"netstat", "-tapn")
}

// forCommand uses ForFunction to check for the execCommand output
func forCommand(c *check.C, outputPattern string, cmds ...string) (err error) {
	return ForFunction(c, outputPattern, func() (string, error) { return execCommand(c, cmds...), nil })
}

// forFunction keeps trying to execute the given function to get an output that
// matches the given pattern until it is obtained or the maximun number of
// retries is executed
func forFunction(c *check.C, outputPattern string, inputFunc func() (string, error)) (err error) {
	re := regexp.MustCompile(outputPattern)

	output, err := inputFunc()
	if err != nil {
		return
	}

	if match := re.FindString(output); match != "" {
		return
	}

	checkInterval := time.Millisecond * interval
	var retries int

	ticker := time.NewTicker(checkInterval)
	tickChan := ticker.C

	for {
		select {
		case <-tickChan:
			output, err = inputFunc()
			if err != nil {
				ticker.Stop()
				return
			}
			if match := re.FindString(output); match != "" {
				ticker.Stop()
				return
			}
			retries++
			if retries >= maxWaitRetries {
				ticker.Stop()
				return fmt.Errorf("Pattern not found in function output")
			}
		}
	}
}
