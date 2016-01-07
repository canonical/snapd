// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
)

func MakeFakeUpdateForSnap(c *check.C, sourceDir, targetDir string) {
	// make a fake update snap
	fakeUpdateDir := c.MkDir()
	defer cli.ExecCommand(c, "sudo", "rm", "-rf", fakeUpdateDir)

	files, err := filepath.Glob(filepath.Join(sourceDir, "*"))
	c.Assert(err, check.IsNil)
	for _, m := range files {
		cli.ExecCommand(c, "sudo", "cp", "-a", m, fakeUpdateDir)
	}

	// fake new version
	cli.ExecCommand(c, "sudo", "sed", "-i", `s/version:\(.*\)/version:\1+fake1/`, filepath.Join(fakeUpdateDir, "meta/package.yaml"))
	helpers.ChDir(targetDir, func() error {
		cli.ExecCommand(c, "sudo", "snappy", "build", "--squashfs", fakeUpdateDir)
		return nil
	})
}
