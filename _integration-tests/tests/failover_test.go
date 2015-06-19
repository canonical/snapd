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
	baseOtherPath             = "/writable/cache/system"
	origKernelfilenamePattern = "boot/%svmlinuz*"
	destKernelFilenamePrefix  = "snappy-selftest-"
	channelCfgFile            = "/etc/system-image/channel.ini"
	deadlockService           = `[Unit]
Before=sysinit.target
DefaultDependencies=no

[Service]
Type=oneshot
ExecStartPre=-/bin/sh -c "echo 'DEBUG: $(date): deadlocked system' >/dev/console"
ExecStartPre=-/bin/sh -c "echo 'DEBUG: $(date): deadlocked system' >/dev/ttyS0"
ExecStart=/bin/systemctl start deadlock.service
RemainAfterExit=yes

[Install]
RequiredBy=sysinit.target
`
	rebootService = `[Unit]
DefaultDependencies=no
Description=Hack to force reboot if booting did not finish after 90s

[Service]
Type=oneshot
ExecStartPre=/bin/sleep 90
ExecStart=-/bin/sh -c 'if ! systemctl is-active default.target; then wall "EMERGENCY REBOOT"; reboot -f; fi'

[Install]
RequiredBy=sysinit.target
`
	baseSystemdPath          = "/lib/systemd/system"
	systemdTargetRequiresDir = "sysinit.target.requires"
)

// The types that implement this interface can be used in the test logic
type failer interface {
	// Sets the failure conditions
	set(c *C)
	// Unsets the failure conditions
	unset(c *C)
}

type zeroSizeKernel struct{}
type sysrqCrashRCLocal struct{}
type systemdDependencyLoop struct{}

func (zeroSizeKernel) set(c *C) {
	completePattern := filepath.Join(
		baseOtherPath,
		fmt.Sprintf(origKernelfilenamePattern, ""))
	oldKernelFilename := getSingleFilename(c, completePattern)
	newKernelFilename := fmt.Sprintf(
		"%s/%s%s", baseOtherPath, destKernelFilenamePrefix, filepath.Base(oldKernelFilename))

	renameFile(c, baseOtherPath, oldKernelFilename, newKernelFilename)
	execCommand(c, "sudo", "touch", oldKernelFilename)
}

func (zeroSizeKernel) unset(c *C) {
	completePattern := filepath.Join(
		baseOtherPath,
		fmt.Sprintf(origKernelfilenamePattern, destKernelFilenamePrefix))
	oldKernelFilename := getSingleFilename(c, completePattern)
	newKernelFilename := strings.Replace(oldKernelFilename, destKernelFilenamePrefix, "", 1)

	renameFile(c, baseOtherPath, oldKernelFilename, newKernelFilename)
}

func (sysrqCrashRCLocal) set(c *C) {
	makeWritable(c, baseOtherPath)
	targetFile := fmt.Sprintf("%s/etc/rc.local", baseOtherPath)
	execCommand(c, "sudo", "chmod", "a+xw", targetFile)
	execCommandToFile(c, targetFile,
		"sudo", "echo", "#bin/sh\nprintf c > /proc/sysrq-trigger")
	makeReadonly(c, baseOtherPath)
}

func (sysrqCrashRCLocal) unset(c *C) {
	makeWritable(c, baseOtherPath)
	execCommand(c, "sudo", "rm", fmt.Sprintf("%s/etc/rc.local", baseOtherPath))
	makeReadonly(c, baseOtherPath)
}

func renameFile(c *C, basePath, oldFilename, newFilename string) {
	makeWritable(c, basePath)
	execCommand(c, "sudo", "mv", oldFilename, newFilename)
	makeReadonly(c, basePath)
}

func (systemdDependencyLoop) set(c *C) {
	installService(c, "deadlock", deadlockService, baseOtherPath)
	installService(c, "emerg-reboot", rebootService, baseOtherPath)
}

func (systemdDependencyLoop) unset(c *C) {
	unInstallService(c, "deadlock", baseOtherPath)
	unInstallService(c, "emerg-reboot", baseOtherPath)
}

func installService(c *C, serviceName, serviceCfg, basePath string) {
	makeWritable(c, basePath)

	// Create service file
	serviceFile := fmt.Sprintf("%s%s/%s.service", basePath, baseSystemdPath, serviceName)
	execCommand(c, "sudo", "chmod", "a+w", fmt.Sprintf("%s%s", basePath, baseSystemdPath))
	execCommandToFile(c, serviceFile, "sudo", "echo", serviceCfg)

	// Create requires directory
	requiresDirPart := fmt.Sprintf("%s/%s", baseSystemdPath, systemdTargetRequiresDir)
	requiresDir := fmt.Sprintf("%s%s", basePath, requiresDirPart)
	execCommand(c, "sudo", "mkdir", "-p", requiresDir)

	// Symlink from the requires dir to the service file (with chroot for being
	// usable in the other partition)
	execCommand(c, "sudo", "chroot", basePath, "ln", "-s",
		fmt.Sprintf("%s/%s.service", baseSystemdPath, serviceName),
		fmt.Sprintf("%s/%s.service", requiresDirPart, serviceName),
	)

	makeReadonly(c, basePath)
}

func unInstallService(c *C, serviceName, basePath string) {
	makeWritable(c, basePath)

	// Disable the service
	execCommand(c, "sudo", "chroot", basePath,
		"systemctl", "disable", fmt.Sprintf("%s.service", serviceName))

	// Remove the service file
	execCommand(c, "sudo", "rm",
		fmt.Sprintf("%s%s/%s.service", basePath, baseSystemdPath, serviceName))

	// Remove the requires symlink
	execCommand(c, "sudo", "rm",
		fmt.Sprintf("%s%s/%s/%s.service", basePath, baseSystemdPath, systemdTargetRequiresDir, serviceName))

	makeReadonly(c, basePath)
}

/*
func (s *FailoverSuite) TestZeroSizeKernel(c *C) {
	commonFailoverTest(c, zeroSizeKernel{})
}

func (s *FailoverSuite) TestSysrqCrashRCLocal(c *C) {
	commonFailoverTest(c, sysrqCrashRCLocal{})
}
*/

func (s *FailoverSuite) TestSystemdDependencyLoop(c *C) {
	commonFailoverTest(c, systemdDependencyLoop{})
}

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
	err := os.Unsetenv("ADT_REBOOT_MARK")
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

func getSingleFilename(c *C, pattern string) string {
	matches, err := filepath.Glob(pattern)

	c.Assert(err, IsNil, Commentf("Error: %v", err))
	c.Check(len(matches), Equals, 1)

	return matches[0]
}
