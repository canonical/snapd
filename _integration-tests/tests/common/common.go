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
	// BaseOtherPath is the path to the B system partition.
	BaseOtherPath   = "/writable/cache/system"
	needsRebootFile = "/tmp/needs-reboot"
	channelCfgFile  = "/etc/system-image/channel.ini"
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
		if checkRebootMark("") {
			c.Logf("****** Running %s", c.TestName())
			SetSavedVersion(c, GetCurrentVersion(c))
		} else {
			if AfterReboot(c) {
				c.Logf("****** Resuming %s after reboot", c.TestName())
			} else {
				c.Skip(fmt.Sprintf("****** Skipped %s after reboot caused by %s",
					c.TestName(), os.Getenv("ADT_REBOOT_MARK")))
			}
		}
	}
}

// TearDownTest cleans up the channel.ini files in case they were changed by
// the test.
func (s *SnappySuite) TearDownTest(c *check.C) {
	if !needsReboot() && checkRebootMark("") {
		// Only restore the channel config files if the reboot has been handled.
		m := make(map[string]string)
		m[channelCfgBackupFile()] = "/"
		m[channelCfgOtherBackupFile()] = BaseOtherPath
		for backup, target := range m {
			if _, err := os.Stat(backup); err == nil {
				MakeWritable(c, target)
				defer MakeReadonly(c, target)
				original := filepath.Join(target, channelCfgFile)
				c.Log(fmt.Sprintf("Restoring %s...", original))
				os.Rename(backup, original)
			}
		}
	}
}

func channelCfgBackupFile() string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), "channel.ini")
}

func channelCfgOtherBackupFile() string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), "channel.ini.other")
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

// CallUpdate executes an snappy update. If there is no update available, the
// channel version will be modified to fake an update.
func CallUpdate(c *check.C) {
	c.Log("Calling snappy update...")
	output := ExecCommand(c, "sudo", "snappy", "update")
	// XXX Instead of trying the update, we should have a command to tell us
	// if there is an available update. --elopio - 2015-07-01
	if output == "" {
		c.Log("There is no update available.")
		fakeAvailableUpdate(c)
		ExecCommand(c, "sudo", "snappy", "update")
	}
}

func fakeAvailableUpdate(c *check.C) {
	c.Log("Faking an available update...")
	currentVersion := GetCurrentVersion(c)
	switchChannelVersion(c, currentVersion, currentVersion-1)
	SetSavedVersion(c, currentVersion-1)
}

func switchChannelVersion(c *check.C, oldVersion, newVersion int) {
	m := make(map[string]string)
	m["/"] = channelCfgBackupFile()
	m[BaseOtherPath] = channelCfgOtherBackupFile()
	for target, backup := range m {
		file := filepath.Join(target, channelCfgFile)
		if _, err := os.Stat(file); err == nil {
			MakeWritable(c, target)
			defer MakeReadonly(c, target)
			// Back up the file. It will be restored during the test tear down.
			ExecCommand(c, "cp", file, backup)
			ExecCommand(c,
				"sudo", "sed", "-i",
				fmt.Sprintf(
					"s/build_number: %d/build_number: %d/g",
					oldVersion, newVersion),
				file)
		}
	}
}

// MakeWritable remounts a path with read and write permissions.
func MakeWritable(c *check.C, path string) {
	ExecCommand(c, "sudo", "mount", "-o", "remount,rw", path)
}

// MakeReadonly remounts a path with only read permissions.
func MakeReadonly(c *check.C, path string) {
	ExecCommand(c, "sudo", "mount", "-o", "remount,ro", path)
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

// InstallSnap executes the required command to install the specified snap
func InstallSnap(c *check.C, packageName string) string {
	return ExecCommand(c, "sudo", "snappy", "install", packageName)
}

// RemoveSnap executes the required command to remove the specified snap
func RemoveSnap(c *check.C, packageName string) string {
	return ExecCommand(c, "sudo", "snappy", "remove", packageName)
}
