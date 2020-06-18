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
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
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

func (s *bootenvTestSuite) TestInstallBootloaderConfigFromGadget(c *C) {
	for _, t := range []struct {
		name                string
		gadgetFile, sysFile string
		gadgetFileContent   []byte
		opts                *bootloader.Options
	}{
		{name: "grub", gadgetFile: "grub.conf", sysFile: "/boot/grub/grub.cfg"},
		// traditional uboot.env - the uboot.env file needs to be non-empty
		{name: "uboot.env", gadgetFile: "uboot.conf", sysFile: "/boot/uboot/uboot.env", gadgetFileContent: []byte{1}},
		// boot.scr in place of uboot.env means we create the boot.sel file
		{
			name:       "uboot boot.scr",
			gadgetFile: "uboot.conf",
			sysFile:    "/uboot/ubuntu/boot.sel",
			opts:       &bootloader.Options{NoSlashBoot: true},
		},
		{name: "androidboot", gadgetFile: "androidboot.conf", sysFile: "/boot/androidboot/androidboot.env"},
		{name: "lk", gadgetFile: "lk.conf", sysFile: "/boot/lk/snapbootsel.bin"},
	} {
		mockGadgetDir := c.MkDir()
		rootDir := c.MkDir()
		err := ioutil.WriteFile(filepath.Join(mockGadgetDir, t.gadgetFile), t.gadgetFileContent, 0644)
		c.Assert(err, IsNil)
		err = bootloader.InstallBootConfig(mockGadgetDir, rootDir, t.opts)
		c.Assert(err, IsNil, Commentf("installing boot config for %s", t.name))
		fn := filepath.Join(rootDir, t.sysFile)
		c.Assert(fn, testutil.FilePresent, Commentf("boot config missing for %s at %s", t.name, t.sysFile))
	}
}

func (s *bootenvTestSuite) TestInstallBootloaderConfigFromAssets(c *C) {
	for _, t := range []struct {
		name                string
		gadgetFile, sysFile string
		gadgetFileContent   []byte
		assetContent        []byte
		assetName           string
		opts                *bootloader.Options
		err                 string
	}{
		{
			name:       "grub",
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub-recovery.cfg",
			assetContent:      []byte("hello assets"),
			opts: &bootloader.Options{
				Recovery: true,
			},
		}, {
			name:              "grub with non empty gadget file",
			gadgetFile:        "grub.conf",
			gadgetFileContent: []byte("not so empty"),
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub-recovery.cfg",
			assetContent:      []byte("hello assets"),
			opts: &bootloader.Options{
				Recovery: true,
			},
		}, {
			name:       "grub with default asset",
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			opts: &bootloader.Options{
				Recovery: true,
			},
		}, {
			name:       "grub missing asset",
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			opts: &bootloader.Options{
				Recovery: true,
			},
			assetName: "grub-recovery.cfg",
			// // no asset content
			// assetContent: nil,
			err: `internal error: no boot asset for "grub-recovery.cfg"`,
		},
	} {
		mockGadgetDir := c.MkDir()
		rootDir := c.MkDir()
		fn := filepath.Join(rootDir, t.sysFile)
		err := ioutil.WriteFile(filepath.Join(mockGadgetDir, t.gadgetFile), t.gadgetFileContent, 0644)
		c.Assert(err, IsNil)
		var restoreAsset func()
		if t.assetName != "" {
			restoreAsset = assets.MockInternal(t.assetName, t.assetContent)
		}
		err = bootloader.InstallBootConfig(mockGadgetDir, rootDir, t.opts)
		if t.err == "" {
			c.Assert(err, IsNil, Commentf("installing boot config for %s", t.name))
			if t.assetContent != nil {
				// mocked asset content
				c.Assert(fn, testutil.FileEquals, string(t.assetContent))
			} else {
				// predefined content, make sure edition marker exists
				c.Assert(fn, testutil.FileContains, "# Snapd-Boot-Config-Edition:")
			}
		} else {
			c.Assert(err, ErrorMatches, t.err)
			c.Assert(fn, testutil.FileAbsent)
		}
		if restoreAsset != nil {
			restoreAsset()
		}
	}
}
