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
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/config"
	"github.com/snapcore/snapd/integration-tests/testutils/partition"
	"github.com/snapcore/snapd/testutil"
)

const (
	// NeedsRebootFile is the file that a test writes in order to request a reboot.
	NeedsRebootFile = "/tmp/needs-reboot"
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
	Cfg, err = config.ReadConfig(config.DefaultFileName)
	c.Assert(err, check.IsNil, check.Commentf("Error reading config: %v", err))

	if !IsInRebootProcess() {
		if Cfg.Update || Cfg.Rollback {
			// TODO handle updates to a different release and channel.
			// Always use the installed snappy because we are updating from an old
			// image, so we should not use the snappy from the branch.
			output := cli.ExecCommand(c, "sudo", "/usr/bin/snappy", "update")
			expected := "(?ms)" +
				".*" +
				fmt.Sprintf("^Reboot to use %s version .*\\.\n", partition.OSSnapName(c))
			c.Assert(output, check.Matches, expected)
			RebootWithMark(c, "setupsuite-update")
		}
	} else if CheckRebootMark("setupsuite-update") {
		RemoveRebootMark(c)
		// Update was already executed. Update the config so it's not triggered again.
		Cfg.Update = false
		Cfg.Write()
		if Cfg.Rollback {
			cli.ExecCommand(c, "sudo", "snappy", "rollback", partition.OSSnapName(c))
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
			if version := GetCurrentUbuntuCoreVersion(c); version != "" {
				SetSavedVersion(c, version)
			}
		} else {
			if AfterReboot(c) {
				c.Logf("****** Resuming %s after reboot", c.TestName())
			} else {
				c.Skip(fmt.Sprintf(FormatSkipAfterReboot, c.TestName(), os.Getenv("ADT_REBOOT_MARK")))
			}
		}
	}
}

// GetCurrentVersion returns the version of the installed and active package.
func GetCurrentVersion(c *check.C, packageName string) string {
	output := cli.ExecCommand(c, "snap", "list")
	pattern := "(?mU)^" + packageName + " +(.*)$"
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(string(output))
	c.Assert(match, check.NotNil, check.Commentf("Version of %s not found in %s", packageName, output))

	// match is like "ubuntu-core   2015-06-18 93        ubuntu"
	items := strings.Fields(match[0])
	return items[2]
}

// GetCurrentUbuntuCoreVersion returns the version number of the installed and
// active ubuntu-core.
func GetCurrentUbuntuCoreVersion(c *check.C) string {
	if snap := partition.OSSnapName(c); snap != "" {
		return GetCurrentVersion(c, snap)
	}
	return ""
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
func SetSavedVersion(c *check.C, version string) {
	versionFile := getVersionFile()
	err := ioutil.WriteFile(versionFile, []byte(version), 0777)
	c.Assert(err, check.IsNil, check.Commentf("Error writing version file %s with %s", versionFile, version))
}

// GetSavedVersion returns the saved version number.
func GetSavedVersion(c *check.C) string {
	versionFile := getVersionFile()
	contents, err := ioutil.ReadFile(versionFile)
	c.Assert(err, check.IsNil, check.Commentf("Error reading version file %s", versionFile))

	return string(contents)
}

func getVersionFile() string {
	return filepath.Join(os.Getenv("ADT_ARTIFACTS"), "version")
}

// InstallSnap executes the required command to install the specified snap
func InstallSnap(c *check.C, packageName string) string {
	cli.ExecCommand(c, "sudo", "snap", "install", packageName)
	out := cli.ExecCommand(c, "snap", "list")
	return out
}

// RemoveSnap executes the required command to remove the specified snap
func RemoveSnap(c *check.C, packageName string) string {
	cli.ExecCommand(c, "sudo", "snap", "remove", packageName)
	out := cli.ExecCommand(c, "snap", "list")
	return out
}
