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
	origKernelfilenamePattern = "boot/%svmlinuz*"
	destKernelFilenamePrefix  = "snappy-selftest-"
)

type zeroSizeKernel struct{}

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

func renameFile(c *C, basePath, oldFilename, newFilename string) {
	makeWritable(c, basePath)
	execCommand(c, "sudo", "mv", oldFilename, newFilename)
	makeReadonly(c, basePath)
}

func getSingleFilename(c *C, pattern string) string {
	matches, err := filepath.Glob(pattern)

	c.Assert(err, IsNil, Commentf("Error: %v", err))
	c.Check(len(matches), Equals, 1)

	return matches[0]
}

/*
func (s *FailoverSuite) TestZeroSizeKernel(c *C) {
	commonFailoverTest(c, zeroSizeKernel{})
}
*/
