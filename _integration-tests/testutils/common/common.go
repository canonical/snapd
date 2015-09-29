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

	"gopkg.in/check.v1"

	"launchpad.net/snappy/_integration-tests/testutils/config"
)

const (
	// BaseAltPartitionPath is the path to the B system partition.
	BaseAltPartitionPath = "/writable/cache/system"
	needsRebootFile      = "/tmp/needs-reboot"
	channelCfgFile       = "/etc/system-image/channel.ini"
)

// Cfg is a struct that contains the configuration values passed from the
// host to the testbed.
var Cfg *config.Config

// SnappySuite is a structure used as a base test suite for all the snappy
// integration tests.
type SnappySuite struct {
	cleanupHandlers []func()
}

// SetUpSuite disables the snappy autopilot. It will run before all the
// integration suites.
func (s *SnappySuite) SetUpSuite(c *check.C) {
	ExecCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
	ExecCommand(c, "sudo", "systemctl", "disable", "snappy-autopilot.timer")
	var err error
	Cfg, err = config.ReadConfig(
		"_integration-tests/data/output/testconfig.json")
	c.Assert(err, check.IsNil, check.Commentf("Error reading config: %v", err))

	if !isInRebootProcess() {
		if Cfg.Update || Cfg.Rollback {
			switchSystemImageConf(c, Cfg.TargetRelease, Cfg.TargetChannel, "0")
			// Always use the installed snappy because we are updating from an old
			// image, so we should not use the snappy from the branch.
			output := ExecCommand(c, "sudo", "/usr/bin/snappy", "update")
			if output != "" {
				RebootWithMark(c, "setupsuite-update")
			}
		}
	} else if CheckRebootMark("setupsuite-update") {
		RemoveRebootMark(c)
		// Update was already executed. Update the config so it's not triggered again.
		Cfg.Update = false
		Cfg.Write()
		if Cfg.Rollback {
			ExecCommand(c, "sudo", "snappy", "rollback", "ubuntu-core")
			RebootWithMark(c, "setupsuite-rollback")
		}
	} else if CheckRebootMark("setupsuite-rollback") {
		RemoveRebootMark(c)
		// Rollback was already executed. Update the config so it's not triggered again.
		Cfg.Rollback = false
		Cfg.Write()
	}
}

// SetUpTest handles reboots and stores version information. It will run before
// all the integration tests. Before running a test, it will save the
// ubuntu-core version. If a reboot was requested by a previous test, it
// will skip all the following tests. If the suite is being called after the
// test bed was rebooted, it will resume the test that requested the reboot.
func (s *SnappySuite) SetUpTest(c *check.C) {
	if NeedsReboot() {
		contents, err := ioutil.ReadFile(needsRebootFile)
		c.Assert(err, check.IsNil, check.Commentf("Error reading needs-reboot file %v", err))
		c.Skip(fmt.Sprintf("****** Skipped %s during reboot caused by %s",
			c.TestName(), contents))
	} else {
		if CheckRebootMark("") {
			c.Logf("****** Running %s", c.TestName())
			SetSavedVersion(c, GetCurrentUbuntuCoreVersion(c))
		} else {
			if AfterReboot(c) {
				c.Logf("****** Resuming %s after reboot", c.TestName())
			} else {
				c.Skip(fmt.Sprintf("****** Skipped %s after reboot caused by %s",
					c.TestName(), os.Getenv("ADT_REBOOT_MARK")))
			}
		}
	}
	// clear slice
	s.cleanupHandlers = nil
}

// TearDownTest cleans up the channel.ini files in case they were changed by
// the test.
// It also runs the cleanup handlers
func (s *SnappySuite) TearDownTest(c *check.C) {
	if !NeedsReboot() && CheckRebootMark("") {
		// Only restore the channel config files if the reboot has been handled.
		m := make(map[string]string)
		m[channelCfgBackupFile()] = "/"
		m[channelCfgOtherBackupFile()] = BaseAltPartitionPath
		for backup, target := range m {
			if _, err := os.Stat(backup); err == nil {
				MakeWritable(c, target)
				defer MakeReadonly(c, target)
				original := filepath.Join(target, channelCfgFile)
				c.Logf("Restoring %s...", original)
				ExecCommand(c, "sudo", "mv", backup, original)
			}
		}
	}

	// run cleanup handlers and clear the slice
	for _, f := range s.cleanupHandlers {
		f()
	}
	s.cleanupHandlers = nil
}

// AddCleanup adds a new cleanup function to the test
func (s *SnappySuite) AddCleanup(f func()) {
	s.cleanupHandlers = append(s.cleanupHandlers, f)
}

func switchSystemImageConf(c *check.C, release, channel, version string) {
	targets := []string{"/", BaseAltPartitionPath}
	for _, target := range targets {
		file := filepath.Join(target, channelCfgFile)
		if _, err := os.Stat(file); err == nil {
			MakeWritable(c, target)
			defer MakeReadonly(c, target)
			replaceSystemImageValues(c, file, release, channel, version)
		}
	}
}

func replaceSystemImageValues(c *check.C, file, release, channel, version string) {
	c.Log("Switching the system image conf...")
	replaceRegex := map[string]string{
		release: `s#channel: ubuntu-core/.*/\(.*\)#channel: ubuntu-core/%s/\1#`,
		channel: `s#channel: ubuntu-core/\(.*\)/.*#channel: ubuntu-core/\1/%s#`,
		version: `s/build_number: .*/build_number: %s/`,
	}
	for value, regex := range replaceRegex {
		if value != "" {
			ExecCommand(c,
				"sudo", "sed", "-i", fmt.Sprintf(regex, value), file)
		}
	}
	// Leave the new file in the test log.
	ExecCommand(c, "cat", file)
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
	fmt.Print(stringOutput)
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

// GetCurrentVersion returns the version of the installed and active package.
func GetCurrentVersion(c *check.C, packageName string) string {
	output := ExecCommand(c, "snappy", "list")
	pattern := "(?mU)^" + packageName + " +(.*)$"
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(string(output))
	c.Assert(match, check.NotNil, check.Commentf("Version not found in %s", output))

	// match is like "ubuntu-core   2015-06-18 93        ubuntu"
	items := strings.Fields(match[0])
	return items[2]
}

// GetCurrentUbuntuCoreVersion returns the version number of the installed and
// active ubuntu-core.
func GetCurrentUbuntuCoreVersion(c *check.C) int {
	versionString := GetCurrentVersion(c, "ubuntu-core")
	version, err := strconv.Atoi(versionString)
	c.Assert(err, check.IsNil, check.Commentf("Error converting version to int %v", version))
	return version
}

// CallFakeUpdate calls snappy update after faking the current version
func CallFakeUpdate(c *check.C) string {
	c.Log("Preparing fake and calling update.")
	fakeAvailableUpdate(c)
	return ExecCommand(c, "sudo", "snappy", "update")
}

func fakeAvailableUpdate(c *check.C) {
	c.Log("Faking an available update...")
	currentVersion := GetCurrentUbuntuCoreVersion(c)
	switchChannelVersionWithBackup(c, currentVersion-1)
	SetSavedVersion(c, currentVersion-1)
}

func switchChannelVersionWithBackup(c *check.C, newVersion int) {
	m := make(map[string]string)
	m["/"] = channelCfgBackupFile()
	m[BaseAltPartitionPath] = channelCfgOtherBackupFile()
	for target, backup := range m {
		file := filepath.Join(target, channelCfgFile)
		if _, err := os.Stat(file); err == nil {
			MakeWritable(c, target)
			defer MakeReadonly(c, target)
			// Back up the file. It will be restored during the test tear down.
			ExecCommand(c, "cp", file, backup)
			replaceSystemImageValues(c, file, "", "", strconv.Itoa(newVersion))
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

// NeedsReboot returns True if a reboot has been requested by a test.
func NeedsReboot() bool {
	_, err := os.Stat(needsRebootFile)
	return err == nil
}

// BeforeReboot returns True if the test is running before the test bed has
// been rebooted, or after the test that requested the reboot handled it.
func BeforeReboot() bool {
	return CheckRebootMark("")
}

// AfterReboot returns True if the test is running after the test bed has been
// rebooted.
func AfterReboot(c *check.C) bool {
	// $ADT_REBOOT_MARK contains the reboot mark, if we have rebooted it'll be the test name
	return strings.HasPrefix(os.Getenv("ADT_REBOOT_MARK"), c.TestName())
}

// CheckRebootMark returns True if the reboot mark matches the string passed as
// argument.
func CheckRebootMark(mark string) bool {
	return os.Getenv("ADT_REBOOT_MARK") == mark
}

func isInRebootProcess() bool {
	return !CheckRebootMark("") || NeedsReboot()
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
	return ExecCommand(c, "sudo", "snappy", "install", packageName, "--allow-unauthenticated")
}

// RemoveSnap executes the required command to remove the specified snap
func RemoveSnap(c *check.C, packageName string) string {
	return ExecCommand(c, "sudo", "snappy", "remove", packageName)
}
