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

package tests

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	. "gopkg.in/check.v1"
)

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
	currentVersion := getCurrentVersion(c)

	if afterReboot(c) {
		removeRebootMark(c)
		f.unset(c)
		c.Assert(getSavedVersion(c), Equals, currentVersion)
	} else {
		switchChannelVersion(c, currentVersion, currentVersion-1)
		setSavedVersion(c, currentVersion-1)

		callUpdate(c)
		f.set(c)
		reboot(c)
	}
}

func reboot(c *C) {
	// This will write the name of the current test as a reboot mark
	execCommand(c, "sudo", "/tmp/autopkgtest-reboot", c.TestName())
}

func removeRebootMark(c *C) {
	err := os.Setenv("ADT_REBOOT_MARK", "")
	c.Assert(err, IsNil, Commentf("Error unsetting ADT_REBOOT_MARK"))
}

func afterReboot(c *C) bool {
	// $ADT_REBOOT_MARK contains the reboot mark, if we have rebooted it'll be the test name
	return os.Getenv("ADT_REBOOT_MARK") == c.TestName()
}

func getCurrentVersion(c *C) int {
	output := execCommand(c, "snappy", "list")
	pattern := "(?mU)^ubuntu-core (.*)$"
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(string(output))
	c.Assert(match, NotNil, Commentf("Version not found in %s", output))

	// match is like "ubuntu-core   2015-06-18 93        ubuntu"
	items := strings.Fields(match[0])
	version, err := strconv.Atoi(items[2])
	c.Assert(err, IsNil, Commentf("Error converting version to int %v", version))
	return version
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
			execCommand(c,
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
	execCommand(c, "sudo", "snappy", "update")
}

func makeWritable(c *C, path string) {
	execCommand(c, "sudo", "mount", "-o", "remount,rw", path)
}

func makeReadonly(c *C, path string) {
	execCommand(c, "sudo", "mount", "-o", "remount,ro", path)
}
