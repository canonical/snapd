// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!excludereboots

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

package tests

import (
	"fmt"
	"path/filepath"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"

	"gopkg.in/check.v1"
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
	systemdTargetRequiresDir = "sysinit.target.requires"
)

func installService(c *check.C, serviceName, serviceCfg, servicesPath string) {
	// Create service file.
	cli.ExecCommand(c, "sudo", "chmod", "a+w", servicesPath)
	serviceFileName := fmt.Sprintf("%s.service", serviceName)
	serviceFilePath := filepath.Join(servicesPath, serviceFileName)
	cli.ExecCommandToFile(c, serviceFilePath, "sudo", "echo", serviceCfg)

	// Create requires directory.
	requiresDir := filepath.Join(servicesPath, systemdTargetRequiresDir)
	cli.ExecCommand(c, "sudo", "mkdir", "-p", requiresDir)

	// Symlink from the requires dir to the service file.
	cli.ExecCommand(c, "sudo", "ln", "-s", serviceFilePath,
		filepath.Join(requiresDir, serviceFileName),
	)
}

func (s *failoverSuite) TestSystemdDependencyLoop(c *check.C) {
	c.Skip("port to snapd")

	breakSnap := func(snapPath string) error {
		servicesPath := filepath.Join(snapPath, "lib", "systemd", "system")
		installService(c, "deadlock", deadlockService, servicesPath)
		installService(c, "emerg-reboot", rebootService, servicesPath)
		return nil
	}
	s.testUpdateToBrokenVersion(c, "ubuntu-core.canonical", breakSnap)
}
