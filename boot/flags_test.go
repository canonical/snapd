// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package boot_test

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type bootFlagsSuite struct {
	baseBootenvSuite
}

var _ = Suite(&bootFlagsSuite{})

func (s *bootFlagsSuite) TestBootFlagsFamilyClassic(c *C) {
	classicDev := boottest.MockDevice("")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))
	defer bootloader.ForceError(nil)

	_, err := boot.NextBootFlags(classicDev)
	c.Assert(err, ErrorMatches, "cannot get boot flags on non-UC20 device")

	err = boot.SetNextBootFlags(classicDev, "", []string{"foo"})
	c.Assert(err, ErrorMatches, "cannot get boot flags on non-UC20 device")

	_, err = boot.BootFlags(classicDev)
	c.Assert(err, ErrorMatches, "cannot get boot flags on non-UC20 device")
}

func (s *bootFlagsSuite) TestBootFlagsFamilyUC16(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))
	defer bootloader.ForceError(nil)

	_, err := boot.NextBootFlags(coreDev)
	c.Assert(err, ErrorMatches, "cannot get boot flags on non-UC20 device")

	err = boot.SetNextBootFlags(coreDev, "", []string{"foo"})
	c.Assert(err, ErrorMatches, "cannot get boot flags on non-UC20 device")

	_, err = boot.BootFlags(coreDev)
	c.Assert(err, ErrorMatches, "cannot get boot flags on non-UC20 device")
}

func setupRealGrub(c *C, rootDir, baseDir string, opts *bootloader.Options) bootloader.Bootloader {
	if rootDir == "" {
		rootDir = dirs.GlobalRootDir
	}
	grubCfg := filepath.Join(rootDir, baseDir, "grub.cfg")
	err := os.MkdirAll(filepath.Dir(grubCfg), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(grubCfg, nil, 0644)
	c.Assert(err, IsNil)

	genv := grubenv.NewEnv(filepath.Join(rootDir, baseDir, "grubenv"))
	err = genv.Save()
	c.Assert(err, IsNil)

	grubBl, err := bootloader.Find(rootDir, opts)
	c.Assert(err, IsNil)
	c.Assert(grubBl.Name(), Equals, "grub")

	return grubBl
}

func (s *bootFlagsSuite) TestInitramfsActiveBootFlagsUC20InstallModeHappy(c *C) {
	dir := c.MkDir()

	dirs.SetRootDir(dir)
	defer func() { dirs.SetRootDir("") }()

	blDir := boot.InitramfsUbuntuSeedDir

	setupRealGrub(c, blDir, "EFI/ubuntu", &bootloader.Options{Role: bootloader.RoleRecovery})

	flags, err := boot.InitramfsActiveBootFlags(boot.ModeInstall)
	c.Assert(err, IsNil)
	c.Assert(flags, HasLen, 0)

	// if we set some flags via ubuntu-image customizations then we get them
	// back

	err = boot.SetImageBootFlags([]string{"factory"}, blDir)
	c.Assert(err, IsNil)

	flags, err = boot.InitramfsActiveBootFlags(boot.ModeInstall)
	c.Assert(err, IsNil)
	c.Assert(flags, DeepEquals, []string{"factory"})
}

func (s *bootFlagsSuite) TestSetImageBootFlagsVerification(c *C) {
	dir := c.MkDir()

	dirs.SetRootDir(dir)
	defer func() { dirs.SetRootDir("") }()

	longVal := "longer-than-256-char-value"
	for i := 0; i < 256; i++ {
		longVal += "X"
	}

	r := boot.MockAdditionalBootFlags([]string{longVal})
	defer r()

	blDir := boot.InitramfsUbuntuSeedDir

	setupRealGrub(c, blDir, "EFI/ubuntu", &bootloader.Options{Role: bootloader.RoleRecovery})

	flags, err := boot.InitramfsActiveBootFlags(boot.ModeInstall)
	c.Assert(err, IsNil)
	c.Assert(flags, HasLen, 0)

	err = boot.SetImageBootFlags([]string{"not-a-real-flag"}, blDir)
	c.Assert(err, ErrorMatches, "flag \"not-a-real-flag\" is not allowed")

	err = boot.SetImageBootFlags([]string{longVal}, blDir)
	c.Assert(err, ErrorMatches, "internal error: boot flags too large to fit inside bootenv value")
}

func (s *bootFlagsSuite) TestInitramfsActiveBootFlagsUC20RecoverModeNoop(c *C) {
	dir := c.MkDir()

	dirs.SetRootDir(dir)
	defer func() { dirs.SetRootDir("") }()

	blDir := boot.InitramfsUbuntuSeedDir

	// create a grubenv to ensure that we don't return any values from there
	grubBl := setupRealGrub(c, blDir, "EFI/ubuntu", &bootloader.Options{Role: bootloader.RoleRecovery})

	// also create the modeenv to make sure we don't peek there either
	m := boot.Modeenv{
		Mode:      boot.ModeRun,
		BootFlags: []string{},
	}

	err := os.MkdirAll(boot.InitramfsWritableDir, 0755)
	c.Assert(err, IsNil)

	err = m.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	flags, err := boot.InitramfsActiveBootFlags(boot.ModeRecover)
	c.Assert(err, IsNil)
	c.Assert(flags, HasLen, 0)

	err = grubBl.SetBootVars(map[string]string{"snapd_boot_flags": "factory"})
	c.Assert(err, IsNil)

	m.BootFlags = []string{"modeenv-boot-flag"}
	err = m.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// still no flags since we are in recovery mode
	flags, err = boot.InitramfsActiveBootFlags(boot.ModeRecover)
	c.Assert(err, IsNil)
	c.Assert(flags, HasLen, 0)
}

func (s *bootFlagsSuite) TestInitramfsActiveBootFlagsUC20RRunModeHappy(c *C) {
	dir := c.MkDir()

	dirs.SetRootDir(dir)
	defer func() { dirs.SetRootDir("") }()

	// setup a basic empty modeenv
	m := boot.Modeenv{
		Mode:      boot.ModeRun,
		BootFlags: []string{},
	}

	err := os.MkdirAll(boot.InitramfsWritableDir, 0755)
	c.Assert(err, IsNil)

	err = m.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	flags, err := boot.InitramfsActiveBootFlags(boot.ModeRun)
	c.Assert(err, IsNil)
	c.Assert(flags, HasLen, 0)

	m.BootFlags = []string{"factory", "other-flag"}
	err = m.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// now some flags after we set them in the modeenv
	flags, err = boot.InitramfsActiveBootFlags(boot.ModeRun)
	c.Assert(err, IsNil)
	c.Assert(flags, DeepEquals, []string{"factory", "other-flag"})
}

func (s *bootFlagsSuite) TestInitramfsSetBootFlags(c *C) {
	tt := []struct {
		flags       []string
		expFlags    []string
		expFlagFile string
	}{
		{
			flags:       []string{"factory"},
			expFlags:    []string{"factory"},
			expFlagFile: "factory",
		},
		{
			flags:       []string{"factory", "unknown-new-flag"},
			expFlags:    []string{"factory", "unknown-new-flag"},
			expFlagFile: "factory,unknown-new-flag",
		},
		{
			flags:       []string{"", "", "", "factory"},
			expFlags:    []string{"factory"},
			expFlagFile: "factory",
		},
		{
			flags:    []string{},
			expFlags: []string{},
		},
	}

	uc20Dev := boottest.MockUC20Device("run", nil)

	for _, t := range tt {
		err := boot.InitramfsSetBootFlags(t.flags)
		c.Assert(err, IsNil)
		c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "boot-flags"), testutil.FileEquals, t.expFlagFile)

		// also read the flags as if from user space to make sure they match
		flags, err := boot.BootFlags(uc20Dev)
		c.Assert(err, IsNil)
		c.Assert(flags, DeepEquals, t.expFlags)
	}
}

func (s *bootFlagsSuite) TestUserspaceBootFlagsUC20(c *C) {
	tt := []struct {
		beforeFlags []string
		flags       []string
		expFlags    []string
		err         string
	}{
		{
			beforeFlags: []string{},
			flags:       []string{"factory"},
			expFlags:    []string{"factory"},
		},
		{
			flags: []string{"factory", "new-unsupported-flag"},
			err:   "flag \"new-unsupported-flag\" is not allowed",
		},
		{
			flags: []string{""},
			err:   "flag \"\" is not allowed",
		},
		{
			beforeFlags: []string{},
			flags:       []string{},
		},
		{
			beforeFlags: []string{"factory"},
			flags:       []string{},
		},
		{
			beforeFlags: []string{"foobar"},
			flags:       []string{"factory"},
			expFlags:    []string{"factory"},
		},
	}

	uc20Dev := boottest.MockUC20Device("run", nil)

	m := boot.Modeenv{
		Mode:      boot.ModeInstall,
		BootFlags: []string{},
	}

	for _, t := range tt {
		m.BootFlags = t.beforeFlags
		err := m.WriteTo("")
		c.Assert(err, IsNil)

		err = boot.SetNextBootFlags(uc20Dev, "", t.flags)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err)
			continue
		}

		c.Assert(err, IsNil)

		// re-read modeenv
		m2, err := boot.ReadModeenv("")
		c.Assert(err, IsNil)
		c.Assert(m2.BootFlags, DeepEquals, t.expFlags)

		// get the next boot flags with NextBootFlags and compare with expected
		flags, err := boot.NextBootFlags(uc20Dev)
		c.Assert(flags, DeepEquals, t.expFlags)
	}
}
