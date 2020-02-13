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
	"errors"
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	//"github.com/snapcore/snapd/dirs"
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

	rootdir string
}

func (s *baseBootenvTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	s.rootdir = c.MkDir()
}

type bootenvTestSuite struct {
	baseBootenvTestSuite

	b *bootloadertest.MockBootloader
}

var _ = Suite(&bootenvTestSuite{})

func (s *bootenvTestSuite) SetUpTest(c *C) {
	s.baseBootenvTestSuite.SetUpTest(c)

	s.b = bootloadertest.Mock("mocky", c.MkDir())
}

func (s *bootenvTestSuite) TestForceBootloader(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	got, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	c.Check(got, Equals, s.b)
}

func (s *bootenvTestSuite) TestForceBootloaderError(c *C) {
	myErr := errors.New("zap")
	bootloader.ForceError(myErr)
	defer bootloader.ForceError(nil)

	got, err := bootloader.Find("", nil)
	c.Assert(err, Equals, myErr)
	c.Check(got, IsNil)
}

func (s *bootenvTestSuite) TestInstallBootloaderConfigNoConfig(c *C) {
	err := bootloader.InstallBootConfig(c.MkDir(), s.rootdir, nil)
	c.Assert(err, ErrorMatches, `cannot find boot config in.*`)
}

func (s *bootenvTestSuite) TestInstallBootloaderConfig(c *C) {
	for _, t := range []struct {
		gadgetFile, systemFile string
		opts                   *bootloader.Options
	}{
		{"grub.conf", "/boot/grub/grub.cfg", nil},
		{"uboot.conf", "/boot/uboot/uboot.env", nil},
		{"androidboot.conf", "/boot/androidboot/androidboot.env", nil},
		{"lk.conf", "/boot/lk/snapbootsel.bin", nil},
		{"grub-recovery.conf", "/EFI/ubuntu/grub.cfg", &bootloader.Options{Recovery: true}},
	} {
		mockGadgetDir := c.MkDir()
		err := ioutil.WriteFile(filepath.Join(mockGadgetDir, t.gadgetFile), nil, 0644)
		c.Assert(err, IsNil)
		err = bootloader.InstallBootConfig(mockGadgetDir, s.rootdir, t.opts)
		c.Assert(err, IsNil)
		fn := filepath.Join(s.rootdir, t.systemFile)
		c.Check(osutil.FileExists(fn), Equals, true, Commentf("boot config missing for %s", t.gadgetFile))
	}
}
