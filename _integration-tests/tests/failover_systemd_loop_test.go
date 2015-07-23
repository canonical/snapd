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

	. "launchpad.net/snappy/_integration-tests/common"

	check "gopkg.in/check.v1"
)

const (
	deadlockService = `[Unit]
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
Description=Hack to force reboot if booting did not finish after 20s

[Service]
Type=oneshot
ExecStartPre=/bin/sleep 20
ExecStart=-/bin/sh -c 'if ! systemctl is-active default.target; then wall "EMERGENCY REBOOT"; reboot -f; fi'

[Install]
RequiredBy=sysinit.target
`
	baseSystemdPath          = "/lib/systemd/system"
	systemdTargetRequiresDir = "sysinit.target.requires"
)

type systemdDependencyLoop struct{}

func (systemdDependencyLoop) set(c *check.C) {
	installService(c, "deadlock", deadlockService, BaseOtherPath)
	installService(c, "emerg-reboot", rebootService, BaseOtherPath)
}

func (systemdDependencyLoop) unset(c *check.C) {
	unInstallService(c, "deadlock", BaseOtherPath)
	unInstallService(c, "emerg-reboot", BaseOtherPath)
}

func installService(c *check.C, serviceName, serviceCfg, basePath string) {
	MakeWritable(c, basePath)
	defer MakeReadonly(c, basePath)

	// Create service file
	serviceFile := fmt.Sprintf("%s%s/%s.service", basePath, baseSystemdPath, serviceName)
	ExecCommand(c, "sudo", "chmod", "a+w", fmt.Sprintf("%s%s", basePath, baseSystemdPath))
	ExecCommandToFile(c, serviceFile, "sudo", "echo", serviceCfg)

	// Create requires directory
	requiresDirPart := fmt.Sprintf("%s/%s", baseSystemdPath, systemdTargetRequiresDir)
	requiresDir := fmt.Sprintf("%s%s", basePath, requiresDirPart)
	ExecCommand(c, "sudo", "mkdir", "-p", requiresDir)

	// Symlink from the requires dir to the service file (with chroot for being
	// usable in the other partition)
	ExecCommand(c, "sudo", "chroot", basePath, "ln", "-s",
		fmt.Sprintf("%s/%s.service", baseSystemdPath, serviceName),
		fmt.Sprintf("%s/%s.service", requiresDirPart, serviceName),
	)
}

func unInstallService(c *check.C, serviceName, basePath string) {
	MakeWritable(c, basePath)
	defer MakeReadonly(c, basePath)

	// Disable the service
	ExecCommand(c, "sudo", "chroot", basePath,
		"systemctl", "disable", fmt.Sprintf("%s.service", serviceName))

	// Remove the service file
	ExecCommand(c, "sudo", "rm",
		fmt.Sprintf("%s%s/%s.service", basePath, baseSystemdPath, serviceName))

	// Remove the requires symlink
	ExecCommand(c, "sudo", "rm",
		fmt.Sprintf("%s%s/%s/%s.service", basePath, baseSystemdPath, systemdTargetRequiresDir, serviceName))
}

func (s *failoverSuite) TestSystemdDependencyLoop(c *check.C) {
	commonFailoverTest(c, systemdDependencyLoop{})
}
