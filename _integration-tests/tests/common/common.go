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

package common

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	check "gopkg.in/check.v1"
)

const (
	needsRebootFile = "/tmp/needs-reboot"
)

// SnappySuite is a structure used as a base test suite for all the snappy
// integration tests.
type SnappySuite struct{}

// SetUpSuite disables the snappy autopilot. It will run before all the
// integration suites.
func (s *SnappySuite) SetUpSuite(c *check.C) {
	ExecCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
	ExecCommand(c, "sudo", "systemctl", "disable", "snappy-autopilot.timer")
}

// SetUpTest handles reboots and stores version information. It will run before
// all the integration tests. Before running a test, it will save the
// ubuntu-core version. If a reboot was requested by a previous test, it
// will skip all the following tests. If the suite is being called after the
// test bed was rebooted, it will resume the test that requested the reboot.
func (s *SnappySuite) SetUpTest(c *check.C) {
	if needsReboot() {
		contents, err := ioutil.ReadFile(needsRebootFile)
		c.Assert(err, check.IsNil, check.Commentf("Error reading needs-reboot file %v", err))
		c.Skip(fmt.Sprintf("****** Skipped %s during reboot caused by %s",
			c.TestName(), contents))
	} else {

		afterReboot := os.Getenv("ADT_REBOOT_MARK")

		if afterReboot == "" {
			c.Logf("****** Running %s", c.TestName())
			SetSavedVersion(c, GetCurrentVersion(c))
		} else {
			if afterReboot == c.TestName() {
				c.Logf("****** Resuming %s after reboot", c.TestName())
			} else {
				c.Skip(fmt.Sprintf("****** Skipped %s after reboot caused by %s",
					c.TestName(), afterReboot))
			}
		}
	}
}

// ExecCommand executes a shell command and returns a string with the output
// of the command. In case of error, it will fail the test.
func ExecCommand(c *check.C, cmds ...string) string {
	fmt.Println(strings.Join(cmds, " "))
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	output, err := cmd.CombinedOutput()
	stringOutput := string(output)
	c.Assert(err, check.IsNil, check.Commentf("Error: %v", stringOutput))
	return stringOutput
}

// ExecCommandToFile executes a shell command and saves the output of the
// command to a file. In case of error, it will fail the test.
func ExecCommandToFile(c *check.C, filename string, cmds ...string) {
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	outfile, err := os.Create(filename)
	c.Assert(err, check.IsNil, check.Commentf("Error creating output file %s", filename))

	defer outfile.Close()
	cmd.Stdout = outfile

	err = cmd.Run()
	c.Assert(err, check.IsNil, check.Commentf("Error executing command '%v': %v", cmds, err))
}

// GetCurrentVersion returns the version number of the installed and active
// ubuntu-core.
func GetCurrentVersion(c *check.C) int {
	output := ExecCommand(c, "snappy", "list")
	pattern := "(?mU)^ubuntu-core (.*)$"
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(string(output))
	c.Assert(match, check.NotNil, check.Commentf("Version not found in %s", output))

	// match is like "ubuntu-core   2015-06-18 93        ubuntu"
	items := strings.Fields(match[0])
	version, err := strconv.Atoi(items[2])
	c.Assert(err, check.IsNil, check.Commentf("Error converting version to int %v", version))
	return version
}

// CallUpdate executes an snappy update.
func CallUpdate(c *check.C) {
	c.Log("Calling snappy update...")
	ExecCommand(c, "sudo", "snappy", "update")
}

// Reboot requests a reboot using the test name as the mark.
func Reboot(c *check.C) {
	RebootWithMark(c, c.TestName())
}

// RebootWithMark requests a reboot using a specified mark.
func RebootWithMark(c *check.C, mark string) {
	c.Log("Preparing reboot with mark " + mark)
	err := ioutil.WriteFile(needsRebootFile, []byte(mark), 0777)
	c.Assert(err, check.IsNil, check.Commentf("Error writing needs-reboot file: %v", err))
}

func needsReboot() bool {
	_, err := os.Stat(needsRebootFile)
	return err == nil
}

// BeforeReboot returns True if the test is running before the test bed has
// been rebooted, or after the test that requested the reboot handled it.
func BeforeReboot() bool {
	return checkRebootMark("")
}

// AfterReboot returns True if the test is running after the test bed has been
// rebooted.
func AfterReboot(c *check.C) bool {
	// $ADT_REBOOT_MARK contains the reboot mark, if we have rebooted it'll be the test name
	return checkRebootMark(c.TestName())
}

func checkRebootMark(mark string) bool {
	return os.Getenv("ADT_REBOOT_MARK") == mark
}

// RemoveRebootMark removes the reboot mark to signal that the reboot has been
// handled.
func RemoveRebootMark(c *check.C) {
	os.Setenv("ADT_REBOOT_MARK", "")
}

// SetSavedVersion saves a version number into a file so it can be used on
// tests after reboots.
func SetSavedVersion(c *check.C, version int) {
	versionFile := getVersionFile()
	err := ioutil.WriteFile(versionFile, []byte(strconv.Itoa(version)), 0777)
	c.Assert(err, check.IsNil, check.Commentf("Error writing version file %s with %s", versionFile, version))
}

// GetSavedVersion returns the saved version number.
func GetSavedVersion(c *check.C) int {
	versionFile := getVersionFile()
	contents, err := ioutil.ReadFile(versionFile)
	c.Assert(err, check.IsNil, check.Commentf("Error reading version file %s", versionFile))

	version, err := strconv.Atoi(string(contents))
	c.Assert(err, check.IsNil, check.Commentf("Error converting version %v", contents))

	return version
}

func getVersionFile() string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), "version")
}
