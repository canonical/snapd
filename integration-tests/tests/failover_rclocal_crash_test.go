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
	"path/filepath"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"

	"gopkg.in/check.v1"
)

func (s *failoverSuite) TestRCLocalCrash(c *check.C) {
	breakSnap := func(snapPath string) error {
		targetFile := filepath.Join(snapPath, "etc", "rc.local")
		cli.ExecCommand(c, "sudo", "chmod", "a+xw", targetFile)
		cli.ExecCommandToFile(c, targetFile,
			"sudo", "echo", "#!bin/sh\nprintf c > /proc/sysrq-trigger")
		return nil
	}
	s.testUpdateToBrokenVersion(c, "ubuntu-core.canonical", breakSnap)
}
