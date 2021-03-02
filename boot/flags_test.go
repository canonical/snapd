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
	. "gopkg.in/check.v1"
)

type bootFlagsSuite struct {
	baseBootenvSuite
}

var _ = Suite(&bootFlagsSuite{})

func (s *bootFlagsSuite) TestNextBootFlagsClassic(c *C) {
	classicDev := boottest.MockDevice("")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))
	defer bootloader.ForceError(nil)

	_, err := boot.NextBootFlags(classicDev)
	c.Assert(err, ErrorMatches, "cannot get boot flags on non-UC20 device")
}

func (s *bootFlagsSuite) TestNextBootFlagsUC16(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))
	defer bootloader.ForceError(nil)

	_, err := boot.NextBootFlags(coreDev)
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

func (s *bootFlagsSuite) TestNextBootFlagsUC20Happy(c *C) {

	tt := []struct {
		mode      string
		blDirFunc func() string
		baseDir   string
		blOpts    *bootloader.Options
	}{
		{
			mode:    boot.ModeRun,
			baseDir: "boot/grub",
		},
		{
			mode:      boot.ModeRecover,
			blDirFunc: func() string { return boot.InitramfsUbuntuSeedDir },
			baseDir:   "EFI/ubuntu",
			blOpts:    &bootloader.Options{Role: bootloader.RoleRecovery},
		},
		{
			mode:      boot.ModeInstall,
			blDirFunc: func() string { return boot.InitramfsUbuntuSeedDir },
			baseDir:   "EFI/ubuntu",
			blOpts:    &bootloader.Options{Role: bootloader.RoleRecovery},
		},
	}
	for _, t := range tt {
		dir := c.MkDir()

		dirs.SetRootDir(dir)
		defer func() { dirs.SetRootDir("") }()

		// mock a grubenv bootloader on the specified dir, this allows us to
		// more specifically test that the right options are passed down from
		// NextBootFlags to find the right bootloader

		blDir := dirs.GlobalRootDir
		if t.blDirFunc != nil {
			blDir = t.blDirFunc()
		}

		grubBl := setupRealGrub(c, blDir, t.baseDir, t.blOpts)

		coreDev := boottest.MockDevice("some-snap@" + t.mode)

		flags, err := boot.NextBootFlags(coreDev)
		c.Assert(err, IsNil)
		c.Assert(flags, HasLen, 0)

		err = grubBl.SetBootVars(map[string]string{"snapd_next_boot_flags": "factory"})
		c.Assert(err, IsNil)

		// if we set some flags then we get them back
		flags, err = boot.NextBootFlags(coreDev)
		c.Assert(err, IsNil)
		c.Assert(flags, DeepEquals, []string{"factory"})

		err = grubBl.SetBootVars(map[string]string{"snapd_next_boot_flags": "some-other,factory,"})
		c.Assert(err, IsNil)

		flags, err = boot.NextBootFlags(coreDev)
		c.Assert(err, IsNil)
		c.Assert(flags, DeepEquals, []string{"some-other", "factory"})

		// Now assign some boot flags with SetNextBootFlags
		err = boot.SetNextBootFlags(coreDev, []string{"other-thing", "under_score", "numbers123"})
		c.Assert(err, IsNil)

		flags, err = boot.NextBootFlags(coreDev)
		c.Assert(err, IsNil)
		c.Assert(flags, DeepEquals, []string{"other-thing", "under_score", "numbers123"})

		// we can also clear the flags too
		err = boot.SetNextBootFlags(coreDev, nil)
		c.Assert(err, IsNil)

		flags, err = boot.NextBootFlags(coreDev)
		c.Assert(err, IsNil)
		c.Assert(flags, HasLen, 0)
	}
}

func (s *bootFlagsSuite) TestSetNextBootFlagsUC20Unhappy(c *C) {
	dir := c.MkDir()
	dirs.SetRootDir(dir)
	defer func() { dirs.SetRootDir("") }()

	_ = setupRealGrub(c, "", "boot/grub", nil)

	coreDev := boottest.MockDevice("some-snap@run")

	tt := []struct {
		err   string
		flags []string
	}{
		{
			`cannot set boot flags: invalid flag "..."`,
			[]string{"..."},
		},
		{
			`cannot set boot flags: invalid flag ".*`,
			[]string{"#$%#$^#$@#@@%^^"},
		},
		{
			`cannot set boot flags: invalid flag "👈👈👈👈"`,
			[]string{"👈👈👈👈"},
		},
		{
			`cannot set boot flags: combined serialized length \(305\) is too long`,
			[]string{
				"00000000000000000000000000000000000000000000000000",
				"00000000000000000000000000000000000000000000000000",
				"00000000000000000000000000000000000000000000000000",
				"00000000000000000000000000000000000000000000000000",
				"00000000000000000000000000000000000000000000000000",
				"00000000000000000000000000000000000000000000000000",
			},
		},
		{
			flags: []string{},
		},
	}

	for _, t := range tt {
		err := boot.SetNextBootFlags(coreDev, t.flags)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err)
			continue
		}
		c.Assert(err, IsNil)

		flags, err := boot.NextBootFlags(coreDev)
		c.Assert(err, IsNil)
		c.Assert(flags, DeepEquals, t.flags)
	}
}
