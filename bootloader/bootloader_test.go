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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
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
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
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
			opts:       &bootloader.Options{Role: bootloader.RoleRecovery},
		},
		{name: "androidboot", gadgetFile: "androidboot.conf", sysFile: "/boot/androidboot/androidboot.env"},
		{name: "lk", gadgetFile: "lk.conf", sysFile: "/boot/lk/snapbootsel.bin", opts: &bootloader.Options{PrepareImageTime: true}},
		{
			name:       "piboot",
			gadgetFile: "piboot.conf",
			sysFile:    "/boot/piboot/piboot.conf",
		},
	} {
		mockGadgetDir := c.MkDir()
		rootDir := c.MkDir()
		err := os.WriteFile(filepath.Join(mockGadgetDir, t.gadgetFile), t.gadgetFileContent, 0644)
		c.Assert(err, IsNil)
		err = bootloader.InstallBootConfig(mockGadgetDir, rootDir, t.opts)
		c.Assert(err, IsNil, Commentf("installing boot config for %s", t.name))
		fn := filepath.Join(rootDir, t.sysFile)
		c.Assert(fn, testutil.FilePresent, Commentf("boot config missing for %s at %s", t.name, t.sysFile))
	}
}

func (s *bootenvTestSuite) TestInstallBootloaderConfigFromAssets(c *C) {
	recoveryOpts := &bootloader.Options{
		Role: bootloader.RoleRecovery,
	}
	systemBootOpts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	defaultRecoveryGrubAsset := assets.Internal("grub-recovery.cfg")
	c.Assert(defaultRecoveryGrubAsset, NotNil)
	defaultGrubAsset := assets.Internal("grub.cfg")
	c.Assert(defaultGrubAsset, NotNil)

	for _, t := range []struct {
		name                string
		gadgetFile, sysFile string
		gadgetFileContent   []byte
		sysFileContent      []byte
		assetContent        []byte
		assetName           string
		err                 string
		opts                *bootloader.Options
	}{
		{
			name:       "recovery grub",
			opts:       recoveryOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub-recovery.cfg",
			assetContent:      []byte("hello assets"),
			// boot config from assets
			sysFileContent: []byte("hello assets"),
		}, {
			name:              "recovery grub with non empty gadget file",
			opts:              recoveryOpts,
			gadgetFile:        "grub.conf",
			gadgetFileContent: []byte("not so empty"),
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub-recovery.cfg",
			assetContent:      []byte("hello assets"),
			// boot config from assets
			sysFileContent: []byte("hello assets"),
		}, {
			name:       "recovery grub with default asset",
			opts:       recoveryOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			sysFileContent:    defaultRecoveryGrubAsset,
		}, {
			name:       "recovery grub missing asset",
			opts:       recoveryOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub-recovery.cfg",
			// no asset content
			err: `internal error: no boot asset for "grub-recovery.cfg"`,
		}, {
			name:       "system-boot grub",
			opts:       systemBootOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub.cfg",
			assetContent:      []byte("hello assets"),
			sysFileContent:    []byte("hello assets"),
		}, {
			name:       "system-boot grub with default asset",
			opts:       systemBootOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			sysFileContent:    defaultGrubAsset,
		},
	} {
		mockGadgetDir := c.MkDir()
		rootDir := c.MkDir()
		fn := filepath.Join(rootDir, t.sysFile)
		err := os.WriteFile(filepath.Join(mockGadgetDir, t.gadgetFile), t.gadgetFileContent, 0644)
		c.Assert(err, IsNil)
		var restoreAsset func()
		if t.assetName != "" {
			restoreAsset = assets.MockInternal(t.assetName, t.assetContent)
		}
		err = bootloader.InstallBootConfig(mockGadgetDir, rootDir, t.opts)
		if t.err == "" {
			c.Assert(err, IsNil, Commentf("installing boot config for %s", t.name))
			// mocked asset content
			c.Assert(fn, testutil.FileEquals, string(t.sysFileContent))
		} else {
			c.Assert(err, ErrorMatches, t.err)
			c.Assert(fn, testutil.FileAbsent)
		}
		if restoreAsset != nil {
			restoreAsset()
		}
	}
}

func (s *bootenvTestSuite) TestBootloaderFindPresentNonNilError(c *C) {
	rootdir := c.MkDir()
	// add a mock bootloader to the list of bootloaders that Find() uses
	mockBl := bootloadertest.Mock("mock", rootdir)
	restore := bootloader.MockAddBootloaderToFind(func(dir string, opts *bootloader.Options) bootloader.Bootloader {
		c.Assert(dir, Equals, rootdir)
		return mockBl
	})
	defer restore()

	// make us find our bootloader
	mockBl.MockedPresent = true

	bl, err := bootloader.Find(rootdir, nil)
	c.Assert(err, IsNil)
	c.Assert(bl, NotNil)
	c.Assert(bl.Name(), Equals, "mock")
	c.Assert(bl, DeepEquals, mockBl)

	// now make finding our bootloader a fatal error, this time we will get the
	// error back
	mockBl.PresentErr = fmt.Errorf("boom")
	_, err = bootloader.Find(rootdir, nil)
	c.Assert(err, ErrorMatches, "bootloader \"mock\" found but not usable: boom")
}

func (s *bootenvTestSuite) TestBootloaderFindBadOptions(c *C) {
	_, err := bootloader.Find("", &bootloader.Options{
		PrepareImageTime: true,
		Role:             bootloader.RoleRunMode,
	})
	c.Assert(err, ErrorMatches, "internal error: cannot use run mode bootloader at prepare-image time")

	_, err = bootloader.Find("", &bootloader.Options{
		NoSlashBoot: true,
		Role:        bootloader.RoleSole,
	})
	c.Assert(err, ErrorMatches, "internal error: bootloader.RoleSole doesn't expect NoSlashBoot set")
}

func (s *bootenvTestSuite) TestBootloaderFind(c *C) {
	for _, tc := range []struct {
		name    string
		sysFile string
		opts    *bootloader.Options
		expName string
	}{
		{name: "grub", sysFile: "/boot/grub/grub.cfg", expName: "grub"},
		{
			// native run partition layout
			name: "grub", sysFile: "/EFI/ubuntu/grub.cfg",
			opts:    &bootloader.Options{Role: bootloader.RoleRunMode, NoSlashBoot: true},
			expName: "grub",
		},
		{
			// recovery layout
			name: "grub", sysFile: "/EFI/ubuntu/grub.cfg",
			opts:    &bootloader.Options{Role: bootloader.RoleRecovery},
			expName: "grub",
		},

		// traditional uboot.env - the uboot.env file needs to be non-empty
		{name: "uboot.env", sysFile: "/boot/uboot/uboot.env", expName: "uboot"},
		// boot.sel uboot variant
		{
			name:    "uboot boot.scr",
			sysFile: "/uboot/ubuntu/boot.sel",
			opts:    &bootloader.Options{Role: bootloader.RoleRunMode, NoSlashBoot: true},
			expName: "uboot",
		},
		{name: "androidboot", sysFile: "/boot/androidboot/androidboot.env", expName: "androidboot"},
		// lk is detected differently based on runtime/prepare-image
		{name: "lk", sysFile: "/dev/disk/by-partlabel/snapbootsel", expName: "lk"},
		{
			name: "lk", sysFile: "/boot/lk/snapbootsel.bin",
			expName: "lk", opts: &bootloader.Options{PrepareImageTime: true},
		},
	} {
		c.Logf("tc: %v", tc.name)
		rootDir := c.MkDir()
		err := os.MkdirAll(filepath.Join(rootDir, filepath.Dir(tc.sysFile)), 0755)
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(rootDir, tc.sysFile), nil, 0644)
		c.Assert(err, IsNil)
		bl, err := bootloader.Find(rootDir, tc.opts)
		c.Assert(err, IsNil)
		c.Assert(bl, NotNil)
		c.Check(bl.Name(), Equals, tc.expName)
	}
}

func (s *bootenvTestSuite) TestBootloaderForGadget(c *C) {
	for _, tc := range []struct {
		name       string
		gadgetFile string
		opts       *bootloader.Options
		expName    string
	}{
		{name: "grub", gadgetFile: "grub.conf", expName: "grub"},
		{name: "grub", gadgetFile: "grub.conf", opts: &bootloader.Options{Role: bootloader.RoleRunMode, NoSlashBoot: true}, expName: "grub"},
		{name: "grub", gadgetFile: "grub.conf", opts: &bootloader.Options{Role: bootloader.RoleRecovery}, expName: "grub"},
		{name: "uboot", gadgetFile: "uboot.conf", expName: "uboot"},
		{name: "androidboot", gadgetFile: "androidboot.conf", expName: "androidboot"},
		{name: "lk", gadgetFile: "lk.conf", expName: "lk"},
	} {
		c.Logf("tc: %v", tc.name)
		gadgetDir := c.MkDir()
		rootDir := c.MkDir()
		err := os.MkdirAll(filepath.Join(rootDir, filepath.Dir(tc.gadgetFile)), 0755)
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(gadgetDir, tc.gadgetFile), nil, 0644)
		c.Assert(err, IsNil)
		bl, err := bootloader.ForGadget(gadgetDir, rootDir, tc.opts)
		c.Assert(err, IsNil)
		c.Assert(bl, NotNil)
		c.Check(bl.Name(), Equals, tc.expName)
	}
}

func (s *bootenvTestSuite) TestBootFileWithPath(c *C) {
	a := bootloader.NewBootFile("", "some/path", bootloader.RoleRunMode)
	b := a.WithPath("other/path")
	c.Assert(a.Path, Equals, "some/path")
	c.Assert(b.Path, Equals, "other/path")
}
