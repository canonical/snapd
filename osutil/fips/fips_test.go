// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package fips_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/fips"
	"github.com/snapcore/snapd/testutil"
)

type fipsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&fipsSuite{})

func (s *fipsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func mockFipsEnabledWithContent(c *C, root, content string) {
	f := filepath.Join(root, "/proc/sys/crypto/fips_enabled")
	err := os.MkdirAll(filepath.Dir(f), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(f, []byte(content), 0444)
	c.Assert(err, IsNil)
}

func (s *fipsSuite) TestFIPSIsEnabled(c *C) {
	mockFipsEnabledWithContent(c, dirs.GlobalRootDir, "1\n")

	res, err := fips.IsEnabled()
	c.Assert(err, IsNil)
	c.Check(res, Equals, true)
}

func (s *fipsSuite) TestFIPSIsDisabled(c *C) {
	mockFipsEnabledWithContent(c, dirs.GlobalRootDir, "0\n")

	res, err := fips.IsEnabled()
	c.Assert(err, IsNil)
	c.Check(res, Equals, false)
}

func (s *fipsSuite) TestFIPSFilePresentButWeirdContent(c *C) {
	mockFipsEnabledWithContent(c, dirs.GlobalRootDir, "\n")

	res, err := fips.IsEnabled()
	c.Assert(err, IsNil)
	c.Check(res, Equals, false)
}

func (s *fipsSuite) TestFIPSNoFile(c *C) {
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/proc/sys/crypto/fips_enabled"), testutil.FileAbsent)
	res, err := fips.IsEnabled()
	c.Assert(err, IsNil)
	c.Check(res, Equals, false)
}

func (s *fipsSuite) TestFIPSFileNotReadable(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("test cannot be executed by root")
	}

	mockFipsEnabledWithContent(c, dirs.GlobalRootDir, "\n")

	err := os.Chmod(filepath.Join(dirs.GlobalRootDir, "/proc/sys/crypto/fips_enabled"), 0o000)
	c.Assert(err, IsNil)

	res, err := fips.IsEnabled()
	c.Assert(err, ErrorMatches, ".*/proc/sys/crypto/fips_enabled: permission denied")
	c.Check(res, Equals, false)
}

func Test(t *testing.T) { TestingT(t) }
