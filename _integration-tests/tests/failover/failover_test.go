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
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	. "../common"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type FailoverSuite struct {
	CommonSuite
}

var _ = Suite(&FailoverSuite{})

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
		removeRebootMark(c)
		f.unset(c)
		c.Assert(getSavedVersion(c), Equals, currentVersion)
	} else {
		switchChannelVersion(c, currentVersion, currentVersion-1)
		setSavedVersion(c, currentVersion-1)

		callUpdate(c)
		f.set(c)
		Reboot(c)
	}
}

func reboot(c *C) {
	// This will write the name of the current test as a reboot mark
	ExecCommand(c, "sudo", "/tmp/autopkgtest-reboot", c.TestName())
}

func removeRebootMark(c *C) {
	ExecCommand(c, "unset", "ADT_REBOOT_MARK")
}

func afterReboot(c *C) bool {
	// $ADT_REBOOT_MARK contains the reboot mark, if we have rebooted it'll be the test name
	return os.Getenv("ADT_REBOOT_MARK") == c.TestName()
}

func setSavedVersion(c *C, version int) {
	versionFile := getVersionFile()
	err := ioutil.WriteFile(versionFile, []byte(strconv.Itoa(version)), 0777)
	c.Assert(err, IsNil, Commentf("Error writing version file %s with %s", versionFile, version))
}

func getSavedVersion(c *C) int {
	versionFile := getVersionFile()
	contents, err := ioutil.ReadFile(versionFile)
	c.Assert(err, IsNil, Commentf("Error reading version file %s", versionFile))

	version, err := strconv.Atoi(string(contents))
	c.Assert(err, IsNil, Commentf("Error converting version %v", contents))

	return version
}

func getVersionFile() string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), "version")
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

func callUpdate(c *C) {
	c.Log("Calling snappy update...")
	ExecCommand(c, "sudo", "snappy", "update")
}

func makeWritable(c *C, path string) {
	ExecCommand(c, "sudo", "mount", "-o", "remount,rw", path)
}

func makeReadonly(c *C, path string) {
	ExecCommand(c, "sudo", "mount", "-o", "remount,ro", path)
}
