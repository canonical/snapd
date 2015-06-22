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
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"
)

const (
	origFilenamePattern = "boot/%s%s*"
	kernelFilename      = "vmlinuz"
	initrdFilename      = "initrd"
	destFilenamePrefix  = "snappy-selftest-"
)

type zeroSizeKernel struct{}
type zeroSizeInitrd struct{}

func (zeroSizeKernel) set(c *C) {
	commonSet(c, kernelFilename)
}

func (zeroSizeKernel) unset(c *C) {
	commonUnset(c, kernelFilename)
}

func (zeroSizeInitrd) set(c *C) {
	commonSet(c, initrdFilename)
}

func (zeroSizeInitrd) unset(c *C) {
	commonUnset(c, initrdFilename)
}

func commonSet(c *C, filename string) {
	filenamePattern := fmt.Sprintf(origFilenamePattern, "", filename)
	completePattern := filepath.Join(
		baseOtherPath,
		filenamePattern)
	oldKernelFilename := getSingleFilename(c, completePattern)
	newKernelFilename := fmt.Sprintf(
		"%s/boot/%s%s", baseOtherPath, destFilenamePrefix, filepath.Base(oldKernelFilename))

	renameFile(c, baseOtherPath, oldKernelFilename, newKernelFilename)
}

func commonUnset(c *C, filename string) {
	completePattern := filepath.Join(
		baseOtherPath,
		fmt.Sprintf(origFilenamePattern, destFilenamePrefix, filename))
	oldKernelFilename := getSingleFilename(c, completePattern)
	newKernelFilename := strings.Replace(oldKernelFilename, destFilenamePrefix, "", 1)

	renameFile(c, baseOtherPath, oldKernelFilename, newKernelFilename)
}

func renameFile(c *C, basePath, oldFilename, newFilename string) {
	makeWritable(c, basePath)
	execCommand(c, "sudo", "mv", oldFilename, newFilename)
	execCommand(c, "sudo", "touch", oldFilename)
	makeReadonly(c, basePath)
}

func getSingleFilename(c *C, pattern string) string {
	c.Logf("PAttern: %s", pattern)
	matches, err := filepath.Glob(pattern)

	c.Assert(err, IsNil, Commentf("Error: %v", err))
	c.Assert(len(matches), Equals, 1)

	return matches[0]
}

/*
func (s *FailoverSuite) TestZeroSizeKernel(c *C) {
	commonFailoverTest(c, zeroSizeKernel{})
}
*/

func (s *FailoverSuite) TestZeroSizeInitrd(c *C) {
	commonFailoverTest(c, zeroSizeInitrd{})
}
