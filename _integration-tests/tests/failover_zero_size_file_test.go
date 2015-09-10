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
	"os"
	"path/filepath"
	"strings"

	. "launchpad.net/snappy/_integration-tests/testutils/common"
	"launchpad.net/snappy/_integration-tests/testutils/partition"

	"gopkg.in/check.v1"
)

const (
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

func (zeroSizeKernel) set(c *check.C) {
	commonSet(c, BaseAltPartitionPath, origBootFilenamePattern, kernelFilename)
}

func (zeroSizeKernel) unset(c *check.C) {
	commonUnset(c, BaseAltPartitionPath, origBootFilenamePattern, kernelFilename)
}

func (zeroSizeInitrd) set(c *check.C) {
	if classicKernelFiles(c) {
		commonSet(c, BaseAltPartitionPath, origBootFilenamePattern, initrdFilename)
	} else {
		boot, err := partition.BootSystem()
		c.Assert(err, check.IsNil, check.Commentf("Error getting the boot system: %s", err))
		dir := partition.BootDir(boot)

		bootFileNamePattern := newKernelFilenamePattern(c, boot, true)
		commonSet(c, dir, bootFileNamePattern, initrdFilename)
	}
}

func (zeroSizeInitrd) unset(c *check.C) {
	if classicKernelFiles(c) {
		commonUnset(c, BaseAltPartitionPath, origBootFilenamePattern, initrdFilename)
	} else {
		boot, err := partition.BootSystem()
		c.Assert(err, check.IsNil, check.Commentf("Error getting the boot system: %s", err))
		dir := partition.BootDir(boot)

		bootFileNamePattern := newKernelFilenamePattern(c, boot, false)
		commonUnset(c, dir, bootFileNamePattern, initrdFilename)
	}
}

func (zeroSizeSystemd) set(c *check.C) {
	commonSet(c, BaseAltPartitionPath, origSystemdFilenamePattern, systemdFilename)
}

func (zeroSizeSystemd) unset(c *check.C) {
	commonUnset(c, BaseAltPartitionPath, origSystemdFilenamePattern, systemdFilename)
}

func commonSet(c *check.C, baseOtherPath, origPattern, filename string) {
	filenamePattern := fmt.Sprintf(origPattern, "", filename)
	completePattern := filepath.Join(
		baseOtherPath,
		filenamePattern)
	oldFilename := getSingleFilename(c, completePattern)
	filenameSuffix := fmt.Sprintf(
		strings.Replace(origPattern, "*", "", 1), destFilenamePrefix, filepath.Base(oldFilename))
	newFilename := fmt.Sprintf(
		"%s/%s", baseOtherPath, filenameSuffix)

	renameFile(c, baseOtherPath, oldFilename, newFilename, true)
}

func commonUnset(c *check.C, baseOtherPath, origPattern, filename string) {
	completePattern := filepath.Join(
		baseOtherPath,
		fmt.Sprintf(origPattern, destFilenamePrefix, filename))
	oldFilename := getSingleFilename(c, completePattern)
	newFilename := strings.Replace(oldFilename, destFilenamePrefix, "", 1)

	renameFile(c, baseOtherPath, oldFilename, newFilename, false)
}

func renameFile(c *check.C, basePath, oldFilename, newFilename string, keepOld bool) {
	// Only need to make writable and revert for BaseAltPartitionPath,
	// kernel files' boot directory is writable
	if basePath == BaseAltPartitionPath {
		MakeWritable(c, basePath)
		defer MakeReadonly(c, basePath)
	}

	ExecCommand(c, "sudo", "mv", oldFilename, newFilename)

	if keepOld {
		ExecCommand(c, "sudo", "touch", oldFilename)
		mode := getFileMode(c, newFilename)
		ExecCommand(c, "sudo", "chmod", fmt.Sprintf("%o", mode), oldFilename)
	}
}

func getFileMode(c *check.C, filePath string) os.FileMode {
	info, err := os.Stat(filePath)
	c.Check(err, check.IsNil, check.Commentf("Error getting Stat of %s", filePath))

	return info.Mode()
}

func getSingleFilename(c *check.C, pattern string) string {
	matches, err := filepath.Glob(pattern)

	c.Assert(err, check.IsNil, check.Commentf("Error: %v", err))
	c.Assert(len(matches), check.Equals, 1,
		check.Commentf("%d files matching %s, 1 expected", len(matches), pattern))

	return matches[0]
}

func classicKernelFiles(c *check.C) bool {
	initrdClassicFilenamePattern := fmt.Sprintf("/boot/%s*-generic", initrdFilename)
	matches, err := filepath.Glob(initrdClassicFilenamePattern)

	c.Assert(err, check.IsNil, check.Commentf("Error: %v", err))

	return len(matches) == 1
}

// newKernelFilenamePattern returns the filename pattern to modify files
// in the partition declared in the boot config file.
//
// After the update, the config file is already changed to point to the new partition.
// If we are on a and update, the config file would point to b
// and this function would return "b/%s%s*"
// If we are not in an update process (ie. we are unsetting the failover conditions)
// we want to change the files in the other partition
func newKernelFilenamePattern(c *check.C, bootSystem string, afterUpdate bool) string {
	var actualPartition string
	part, err := partition.CurrentPartition()
	c.Assert(err, check.IsNil, check.Commentf("Error getting the current partition: %s", err))
	if afterUpdate {
		actualPartition = part
	} else {
		actualPartition = partition.OtherPartition(part)
	}
	return filepath.Join(actualPartition, "%s%s*")
}

/*
TODO: uncomment when bug https://bugs.launchpad.net/snappy/+bug/1467553 is fixed
(fgimenez 20150729)

func (s *failoverSuite) TestZeroSizeKernel(c *check.C) {
	commonFailoverTest(c, zeroSizeKernel{})
}
*/

func (s *failoverSuite) TestZeroSizeInitrd(c *check.C) {
	// Skip if on uboot due to https://bugs.launchpad.net/snappy/+bug/1480248
	// (fgimenez 20150731)
	boot, err := partition.BootSystem()
	c.Assert(err, check.IsNil, check.Commentf("Error getting the boot system: %s", err))
	if boot == "uboot" {
		c.Skip("Failover for empty initrd not working in uboot")
	}
	commonFailoverTest(c, zeroSizeInitrd{})
}

func (s *failoverSuite) TestZeroSizeSystemd(c *check.C) {
	commonFailoverTest(c, zeroSizeSystemd{})
}
