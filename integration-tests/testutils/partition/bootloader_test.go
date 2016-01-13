// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package partition

import (
	"fmt"
	"testing"

	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type bootloaderTestSuite struct {
	filepathGlobCalls        map[string]int
	backFilepathGlob         func(string) ([]string, error)
	filepathGlobFail         bool
	filepathGlobReturnValues []string
}

var _ = check.Suite(&bootloaderTestSuite{})

func (s *bootloaderTestSuite) SetUpSuite(c *check.C) {
	s.backFilepathGlob = filepathGlob
	filepathGlob = s.fakeFilepathGlob
}

func (s *bootloaderTestSuite) TearDownSuite(c *check.C) {
	filepathGlob = s.backFilepathGlob
}

func (s *bootloaderTestSuite) SetUpTest(c *check.C) {
	s.filepathGlobCalls = make(map[string]int)
	s.filepathGlobFail = false
	s.filepathGlobReturnValues = nil
}

func (s *bootloaderTestSuite) fakeFilepathGlob(path string) (matches []string, err error) {
	if s.filepathGlobFail {
		err = fmt.Errorf("Error calling filepathGlob!!")
		return
	}
	s.filepathGlobCalls[path]++

	return s.filepathGlobReturnValues, nil
}

func (s *bootloaderTestSuite) TestBootDir(c *check.C) {
	c.Assert(BootDir("grub"), check.Equals, grubDir,
		check.Commentf("Expected BootDir of 'grub' to be "+grubDir))
	c.Assert(BootDir("uboot"), check.Equals, ubootDir,
		check.Commentf("Expected OtherPartition of 'uboot' to be "+ubootDir))
}

func (s *bootloaderTestSuite) TestBootSystemReturnsGlobError(c *check.C) {
	s.filepathGlobFail = true

	_, err := BootSystem()

	c.Assert(err, check.NotNil, check.Commentf("Expected error to be nil, %v", err))
}

func (s *bootloaderTestSuite) TestBootSystemCallsFilepathGlob(c *check.C) {
	BootSystem()

	p := bootBase + "/grub"
	calls := s.filepathGlobCalls[p]

	c.Assert(calls, check.Equals, 1,
		check.Commentf("Expected calls to filepath.Glob with path %s to be 1, %d found", p, calls))
}

func (s *bootloaderTestSuite) TestBootSystemForGrub(c *check.C) {
	s.filepathGlobReturnValues = []string{"a-grub-related-dir"}

	bootSystem, err := BootSystem()

	c.Assert(err, check.IsNil, check.Commentf("Unexpected error %v", err))
	c.Assert(bootSystem, check.Equals, "grub",
		check.Commentf("Expected grub boot system not found, %s", bootSystem))
}

func (s *bootloaderTestSuite) TestBootSystemForUBoot(c *check.C) {
	s.filepathGlobReturnValues = []string{}

	bootSystem, err := BootSystem()

	c.Assert(err, check.IsNil, check.Commentf("Unexpected error %v", err))
	c.Assert(bootSystem, check.Equals, "uboot",
		check.Commentf("Expected uboot boot system not found, %s", bootSystem))
}
