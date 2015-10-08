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

package partition

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/check.v1"

	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/wait"
)

const (
	// beginning of the string returned by lsof -V when the partition
	// is not being used, in this case the exit status of lsof is 1
	lsofNotUsed = "lsof: no file system use located"
	// custom pattern to be returned by the check functions
	lsofNotBeingWritten = "volume-is-idle"
)

var (
	execCommand     = cli.ExecCommandErr
	waitForFunction = wait.ForFunction
)

// MakeWritable remounts a path with read and write permissions.
func MakeWritable(c *check.C, path string) (err error) {
	return commonMount(c, path, "remount,rw")
}

// MakeReadonly remounts a path with only read permissions.
func MakeReadonly(c *check.C, path string) (err error) {
	return commonMount(c, path, "remount,ro")
}

func commonMount(c *check.C, path, mountOption string) (err error) {
	err = waitForFunction(c, lsofNotBeingWritten, checkPathBusyFunc(path))

	if err != nil {
		return
	}

	execCommand("sudo", "mount", "-o", mountOption, path)
	return
}

func checkPathBusyFunc(path string) func() (string, error) {
	return func() (result string, err error) {
		// lsof exit status is 1 for unused partitions
		var info string
		if info, err = execCommand("lsof", "-V", path); err != nil {
			// check if the output matches lsofNotUsed
			if !strings.HasPrefix(info, lsofNotUsed) {
				return info, err
			}
		}
		reader := strings.NewReader(info)
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if match, _ := regexp.MatchString("^[0-9]+w$", fields[3]); match {
				fmt.Printf("match! %s", fields[3])
				return fields[3], nil
			}
		}
		return lsofNotBeingWritten, nil
	}
}
