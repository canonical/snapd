// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/config"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/partition"
	"github.com/ubuntu-core/snappy/testutil"
)

const (
	// BaseAltPartitionPath is the path to the B system partition.
	BaseAltPartitionPath = "/writable/cache/system"
	// NeedsRebootFile is the file that a test writes in order to request a reboot.
	NeedsRebootFile = "/tmp/needs-reboot"
	channelCfgFile  = "/etc/system-image/channel.ini"
	// FormatSkipDuringReboot is the reason used to skip the pending tests when a test requested
	// a reboot.
	FormatSkipDuringReboot = "****** Skipped %s during reboot caused by %s"
	// FormatSkipAfterReboot is the reason used to skip already ran tests after a reboot requested
	// by a test.
	FormatSkipAfterReboot = "****** Skipped %s after reboot caused by %s"
)

// Cfg is a struct that contains the configuration values passed from the
// host to the testbed.
var Cfg *config.Config

// SnappySuite is a structure used as a base test suite for all the snappy
// integration tests.
type SnappySuite struct {
	testutil.BaseTest
}

// SetUpSuite disables the snappy autopilot. It will run before all the
// integration suites.
func (s *SnappySuite) SetUpSuite(c *check.C) {
	var err error
	Cfg, err = config.ReadConfig(
		"integration-tests/data/output/testconfig.json")
	c.Assert(err, check.IsNil, check.Commentf("Error reading config: %v", err))

	if !IsInRebootProcess() {
		if Cfg.Update || Cfg.Rollback {
			// Always use the installed snappy because we are updating from an old
			// image, so we should not use the snappy from the branch.
			output := cli.ExecCommand(c, "sudo", "/usr/bin/snappy", "update")
			expected := "(?ms)" +
				".*" +
				"^Reboot to use the new ubuntu-core\\.\n"
			c.Assert(output, check.Matches, expected)
			RebootWithMark(c, "setupsuite-update")
		}
	} else if CheckRebootMark("setupsuite-update") {
		RemoveRebootMark(c)
		// Update was already executed. Update the config so it's not triggered again.
		Cfg.Update = false
		Cfg.Write()
		if Cfg.Rollback {
			cli.ExecCommand(c, "sudo", "snappy", "rollback", "ubuntu-core")
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
	s.BaseTest.SetUpTest(c)

	if NeedsReboot() {
		contents, err := ioutil.ReadFile(NeedsRebootFile)
		c.Assert(err, check.IsNil, check.Commentf("Error reading needs-reboot file %v", err))
		c.Skip(fmt.Sprintf(FormatSkipDuringReboot, c.TestName(), contents))
	} else {
		if CheckRebootMark("") {
			c.Logf("****** Running %s", c.TestName())
			SetSavedVersion(c, GetCurrentUbuntuCoreVersion(c))
		} else {
			if AfterReboot(c) {
				c.Logf("****** Resuming %s after reboot", c.TestName())
				p, err := partition.CurrentPartition()
				c.Assert(err, check.IsNil, check.Commentf("Error getting the current boot partition: %v", err))
				c.Logf(fmt.Sprintf("Rebooted to partition %s", p))
			} else {
				c.Skip(fmt.Sprintf(FormatSkipAfterReboot, c.TestName(), os.Getenv("ADT_REBOOT_MARK")))
			}
		}
	}
}

// TearDownTest cleans up the channel.ini files in case they were changed by
// the test.
// It also runs the cleanup handlers
func (s *SnappySuite) TearDownTest(c *check.C) {
	if !IsInRebootProcess() {
		// Only restore the channel config files if the reboot has been handled.
		m := make(map[string]string)
		m[channelCfgBackupFile()] = "/"
		m[channelCfgOtherBackupFile()] = BaseAltPartitionPath
		for backup, target := range m {
			if _, err := os.Stat(backup); err == nil {
				partition.MakeWritable(c, target)
				defer partition.MakeReadonly(c, target)
				original := filepath.Join(target, channelCfgFile)
				c.Logf("Restoring %s...", original)
				cli.ExecCommand(c, "sudo", "mv", backup, original)
			}
		}
	}

	s.BaseTest.TearDownTest(c)
}

func switchSystemImageConf(c *check.C, release, channel, version string) {
	targets := []string{"/", BaseAltPartitionPath}
	for _, target := range targets {
		file := filepath.Join(target, channelCfgFile)
		if _, err := os.Stat(file); err == nil {
			partition.MakeWritable(c, target)
			defer partition.MakeReadonly(c, target)
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
			cli.ExecCommand(c,
				"sudo", "sed", "-i", fmt.Sprintf(regex, value), file)
		}
	}
	// Leave the new file in the test log.
	cli.ExecCommand(c, "cat", file)
}

func channelCfgBackupFile() string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), "channel.ini")
}

func channelCfgOtherBackupFile() string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), "channel.ini.other")
}

// GetCurrentVersion returns the version of the installed and active package.
func GetCurrentVersion(c *check.C, packageName string) string {
	output := cli.ExecCommand(c, "snappy", "list")
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
	return cli.ExecCommand(c, "sudo", "snappy", "update")
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
			partition.MakeWritable(c, target)
			defer partition.MakeReadonly(c, target)
			// Back up the file. It will be restored during the test tear down.
			cli.ExecCommand(c, "cp", file, backup)
			replaceSystemImageValues(c, file, "", "", strconv.Itoa(newVersion))
		}
	}
}

// Reboot requests a reboot using the test name as the mark.
func Reboot(c *check.C) {
	RebootWithMark(c, c.TestName())
}

// RebootWithMark requests a reboot using a specified mark.
func RebootWithMark(c *check.C, mark string) {
	c.Log("Preparing reboot with mark " + mark)
	err := ioutil.WriteFile(NeedsRebootFile, []byte(mark), 0777)
	c.Assert(err, check.IsNil, check.Commentf("Error writing needs-reboot file: %v", err))
	mode, err := partition.Mode()
	c.Assert(err, check.IsNil, check.Commentf("Error getting the bootloader mode: %v", err))
	if mode == "try" {
		p, err := partition.NextBootPartition()
		c.Assert(err, check.IsNil, check.Commentf("Error getting the next boot partition: %v", err))
		c.Logf("Will reboot in try mode to partition %s", p)
	}
}

// NeedsReboot returns True if a reboot has been requested by a test.
func NeedsReboot() bool {
	_, err := os.Stat(NeedsRebootFile)
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

// IsInRebootProcess returns True if the suite needs to execute a reboot or has just rebooted.
func IsInRebootProcess() bool {
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
	return cli.ExecCommand(c, "sudo", "https_proxy="+os.Getenv("https_proxy"),
		"snappy", "install", packageName, "--allow-unauthenticated")
}

// RemoveSnap executes the required command to remove the specified snap
func RemoveSnap(c *check.C, packageName string) string {
	return cli.ExecCommand(c, "sudo", "snappy", "remove", packageName)
}
