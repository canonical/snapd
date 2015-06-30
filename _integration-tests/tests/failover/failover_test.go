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

package failover

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "../common"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&failoverSuite{})

type failoverSuite struct {
	SnappySuite
}

const (
	baseOtherPath  = "/writable/cache/system"
	channelCfgFile = "/etc/system-image/channel.ini"
)

// The types that implement this interface can be used in the test logic
type failer interface {
	// Sets the failure conditions
	set(c *C)
	// Unsets the failure conditions
	unset(c *C)
}

// This is the logic common to all the failover tests. Each of them has define a
// type implementing the failer interface and call this function with an instance
// of it
func commonFailoverTest(c *C, f failer) {
	currentVersion := GetCurrentVersion(c)

	if AfterReboot(c) {
		RemoveRebootMark(c)
		f.unset(c)
		c.Assert(GetSavedVersion(c), Equals, currentVersion)
	} else {
		switchChannelVersion(c, currentVersion, currentVersion-1)
		SetSavedVersion(c, currentVersion-1)
		CallUpdate(c)
		f.set(c)
		Reboot(c)
	}
}

func switchChannelVersion(c *C, oldVersion, newVersion int) {
	targets := []string{"/", baseOtherPath}
	for _, target := range targets {
		file := filepath.Join(target, channelCfgFile)
		if _, err := os.Stat(file); err == nil {
			makeWritable(c, target)
			ExecCommand(c,
				"sudo", "sed", "-i",
				fmt.Sprintf(
					"s/build_number: %d/build_number: %d/g",
					oldVersion, newVersion),
				file)
			makeReadonly(c, target)
		}
	}
}

func makeWritable(c *C, path string) {
	ExecCommand(c, "sudo", "mount", "-o", "remount,rw", path)
}

func makeReadonly(c *C, path string) {
	ExecCommand(c, "sudo", "mount", "-o", "remount,ro", path)
}
