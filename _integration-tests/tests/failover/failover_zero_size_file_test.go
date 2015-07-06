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

package failover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "../common"

	. "gopkg.in/check.v1"
)

const (
	// TODO: take into account arch for the boot path pattern
	origBootFilenamePattern    = "boot/%s%s*"
	origSystemdFilenamePattern = "lib/systemd/%s%s"
	kernelFilename             = "vmlinuz"
	initrdFilename             = "initrd"
	systemdFilename            = "systemd"
	destFilenamePrefix         = "snappy-selftest-"
)

type zeroSizeKernel struct{}
type zeroSizeInitrd struct{}
type zeroSizeSystemd struct{}

func (zeroSizeKernel) set(c *C) {
	commonSet(c, origBootFilenamePattern, kernelFilename)
}

func (zeroSizeKernel) unset(c *C) {
	commonUnset(c, origBootFilenamePattern, kernelFilename)
}

func (zeroSizeInitrd) set(c *C) {
	commonSet(c, origBootFilenamePattern, initrdFilename)
}

func (zeroSizeInitrd) unset(c *C) {
	commonUnset(c, origBootFilenamePattern, initrdFilename)
}

func (zeroSizeSystemd) set(c *C) {
	commonSet(c, origSystemdFilenamePattern, systemdFilename)
}

func (zeroSizeSystemd) unset(c *C) {
	commonUnset(c, origSystemdFilenamePattern, systemdFilename)
}

func commonSet(c *C, origPattern, filename string) {
	filenamePattern := fmt.Sprintf(origPattern, "", filename)
	completePattern := filepath.Join(
		baseOtherPath,
		filenamePattern)
	oldFilename := getSingleFilename(c, completePattern)
	filenameSuffix := fmt.Sprintf(
		strings.Replace(origPattern, "*", "", 1), destFilenamePrefix, filepath.Base(oldFilename))
	newFilename := fmt.Sprintf(
		"%s/%s", baseOtherPath, filenameSuffix)

	renameFile(c, baseOtherPath, oldFilename, newFilename)
}

func commonUnset(c *C, origPattern, filename string) {
	completePattern := filepath.Join(
		baseOtherPath,
		fmt.Sprintf(origPattern, destFilenamePrefix, filename))
	oldFilename := getSingleFilename(c, completePattern)
	newFilename := strings.Replace(oldFilename, destFilenamePrefix, "", 1)

	renameFile(c, baseOtherPath, oldFilename, newFilename)
}

func renameFile(c *C, basePath, oldFilename, newFilename string) {
	makeWritable(c, basePath)
	ExecCommand(c, "sudo", "mv", oldFilename, newFilename)
	ExecCommand(c, "sudo", "touch", oldFilename)

	mode := getFileMode(newFilename)
	ExecCommand(c, "sudo", "chmod", fmt.Sprintf("%o", mode), oldFilename)
	makeReadonly(c, basePath)
}

func getFileMode(filePath string) os.FileMode {
	info, _ := os.Stat(filePath)
	return info.Mode()
}

func getSingleFilename(c *C, pattern string) string {
	matches, err := filepath.Glob(pattern)

	c.Assert(err, IsNil, Commentf("Error: %v", err))
	c.Assert(len(matches), Equals, 1)

	return matches[0]
}

/*
func (s *failoverSuite) TestZeroSizeKernel(c *C) {
	commonFailoverTest(c, zeroSizeKernel{})
}
*/

func (s *failoverSuite) TestZeroSizeInitrd(c *C) {
	commonFailoverTest(c, zeroSizeInitrd{})
}

func (s *failoverSuite) TestZeroSizeSystemd(c *C) {
	commonFailoverTest(c, zeroSizeSystemd{})
}
