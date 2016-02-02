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
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"

	"gopkg.in/check.v1"
)

func replaceWithZeroSizeFile(c *check.C, path string) {
	mode := getFileMode(c, path)
	cli.ExecCommand(c, "sudo", "rm", path)
	cli.ExecCommand(c, "sudo", "touch", path)
	cli.ExecCommand(c, "sudo", "chmod", fmt.Sprintf("%o", mode), path)
}

func getFileMode(c *check.C, filePath string) os.FileMode {
	info, err := os.Stat(filePath)
	c.Check(err, check.IsNil, check.Commentf("Error getting Stat of %s", filePath))

	return info.Mode()
}

/*
TODO: uncomment when bug https://bugs.launchpad.net/snappy/+bug/1467553 is fixed
(fgimenez 20150729)

func (s *failoverSuite) TestZeroSizeKernel(c *check.C) {
  breakSnap := func(snapPath string) error {
		fullPath, error := filepath.EvalSymlinks(filepath.Join(snapPath, "vmlinuz"))
    if error != nil {
        return error
    }
		replaceWithZeroSizeFile(c, fullPath)
		return nil
	}
  // FIXME get the kernel snap name from the system:
  // https://bugs.launchpad.net/snappy/+bug/1532245
	s.testUpdateToBrokenVersion(c, "canonical-linux-pc.canonical", breakSnap)
}
*/

func (s *failoverSuite) TestZeroSizeInitrd(c *check.C) {
	breakSnap := func(snapPath string) error {
		fullPath, error := filepath.EvalSymlinks(filepath.Join(snapPath, "initrd.img"))
		if error != nil {
			return error
		}
		replaceWithZeroSizeFile(c, fullPath)
		return nil
	}
	// FIXME get the kernel snap name from the system:
	// https://bugs.launchpad.net/snappy/+bug/1532245
	s.testUpdateToBrokenVersion(c, "canonical-pc-linux.canonical", breakSnap)
}

func (s *failoverSuite) TestZeroSizeSystemd(c *check.C) {
	breakSnap := func(snapPath string) error {
		fullPath := filepath.Join(snapPath, "lib", "systemd", "systemd")
		replaceWithZeroSizeFile(c, fullPath)
		return nil
	}
	s.testUpdateToBrokenVersion(c, "ubuntu-core.canonical", breakSnap)
}
