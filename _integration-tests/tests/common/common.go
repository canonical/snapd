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

	. "gopkg.in/check.v1"
)

type CommonSuite struct{}

func ExecCommand(c *C, cmds ...string) []byte {
	fmt.Println(strings.Join(cmds, " "))

	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil, Commentf("Error: %v", string(output)))
	return output
}

func ExecCommandToFile(c *C, filename string, cmds ...string) {
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	outfile, err := os.Create(filename)
	c.Assert(err, IsNil, Commentf("Error creating output file %s", filename))

	defer outfile.Close()
	cmd.Stdout = outfile

	err = cmd.Run()
	c.Assert(err, IsNil, Commentf("Error executing command '%v': %v", cmds, err))
}

func GetCurrentVersion(c *C) int {
	output := ExecCommand(c, "snappy", "list")
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

func CallUpdate(c *C) {
	c.Log("Calling snappy update...")
	ExecCommand(c, "sudo", "snappy", "update")
}

func Reboot(c *C) {
	RebootWithMark(c, c.TestName())
}

func RebootWithMark(c *C, mark string) {
	ExecCommand(c, "sudo", "/tmp/autopkgtest-reboot", mark)
}

func BeforeReboot(c *C) bool {
	return checkRebootMark(c, "")
}

func AfterReboot(c *C) bool {
	// $ADT_REBOOT_MARK contains the reboot mark, if we have rebooted it'll be the test name
	return checkRebootMark(c, c.TestName())
}

func checkRebootMark(c *C, mark string) bool {
	return os.Getenv("ADT_REBOOT_MARK") == mark
}

func RemoveRebootMark(c *C) {
	os.Setenv("ADT_REBOOT_MARK", "")
}

func SetSavedVersion(c *C, version int) {
	versionFile := getVersionFile()
	err := ioutil.WriteFile(versionFile, []byte(strconv.Itoa(version)), 0777)
	c.Assert(err, IsNil, Commentf("Error writing version file %s with %s", versionFile, version))
}

func GetSavedVersion(c *C) int {
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

func (s *CommonSuite) SetUpSuite(c *C) {
	ExecCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
	ExecCommand(c, "sudo", "systemctl", "disable", "snappy-autopilot.timer")
}

func (s *CommonSuite) SetUpTest(c *C) {
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
