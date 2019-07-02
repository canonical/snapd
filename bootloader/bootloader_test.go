// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package bootloader_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone
`

type baseBootenvTestSuite struct {
	testutil.BaseTest
}

func (s *baseBootenvTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

type bootenvTestSuite struct {
	baseBootenvTestSuite

	b *boottest.MockBootloader
}

var _ = Suite(&bootenvTestSuite{})

func (s *bootenvTestSuite) SetUpTest(c *C) {
	s.baseBootenvTestSuite.SetUpTest(c)

	s.b = boottest.NewMockBootloader("mocky", c.MkDir())
}

func (s *bootenvTestSuite) TestForceBootloader(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	got, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Check(got, Equals, s.b)
}

func (s *bootenvTestSuite) TestMarkBootSuccessfulAllSnap(c *C) {
	s.b.BootVars["snap_mode"] = "trying"
	s.b.BootVars["snap_try_core"] = "os1"
	s.b.BootVars["snap_try_kernel"] = "k1"
	err := bootloader.MarkBootSuccessful(s.b)
	c.Assert(err, IsNil)

	expected := map[string]string{
		// cleared
		"snap_mode":       "",
		"snap_try_kernel": "",
		"snap_try_core":   "",
		// updated
		"snap_kernel": "k1",
		"snap_core":   "os1",
	}
	c.Assert(s.b.BootVars, DeepEquals, expected)

	// do it again, verify its still valid
	err = bootloader.MarkBootSuccessful(s.b)
	c.Assert(err, IsNil)
	c.Assert(s.b.BootVars, DeepEquals, expected)
}

func (s *bootenvTestSuite) TestMarkBootSuccessfulKKernelUpdate(c *C) {
	s.b.BootVars["snap_mode"] = "trying"
	s.b.BootVars["snap_core"] = "os1"
	s.b.BootVars["snap_kernel"] = "k1"
	s.b.BootVars["snap_try_core"] = ""
	s.b.BootVars["snap_try_kernel"] = "k2"
	err := bootloader.MarkBootSuccessful(s.b)
	c.Assert(err, IsNil)
	c.Assert(s.b.BootVars, DeepEquals, map[string]string{
		// cleared
		"snap_mode":       "",
		"snap_try_kernel": "",
		"snap_try_core":   "",
		// unchanged
		"snap_core": "os1",
		// updated
		"snap_kernel": "k2",
	})
}

func (s *bootenvTestSuite) TestInstallBootloaderConfigNoConfig(c *C) {
	err := bootloader.InstallBootConfig(c.MkDir())
	c.Assert(err, ErrorMatches, `cannot find boot config in.*`)
}

func (s *bootenvTestSuite) TestInstallBootloaderConfig(c *C) {
	for _, t := range []struct{ gadgetFile, systemFile string }{
		{"grub.conf", "/boot/grub/grub.cfg"},
		{"uboot.conf", "/boot/uboot/uboot.env"},
		{"androidboot.conf", "/boot/androidboot/androidboot.env"},
	} {
		mockGadgetDir := c.MkDir()
		err := ioutil.WriteFile(filepath.Join(mockGadgetDir, t.gadgetFile), nil, 0644)
		c.Assert(err, IsNil)
		err = bootloader.InstallBootConfig(mockGadgetDir)
		c.Assert(err, IsNil)
		fn := filepath.Join(dirs.GlobalRootDir, t.systemFile)
		c.Assert(osutil.FileExists(fn), Equals, true)
	}
}
