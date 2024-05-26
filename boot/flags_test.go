// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
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

	_ := mylog.Check2(boot.NextBootFlags(classicDev))
	c.Assert(err, ErrorMatches, `cannot get boot flags on pre-UC20 device`)
	mylog.Check(boot.SetNextBootFlags(classicDev, "", []string{"foo"}))
	c.Assert(err, ErrorMatches, `cannot get boot flags on pre-UC20 device`)

	_ = mylog.Check2(boot.BootFlags(classicDev))
	c.Assert(err, ErrorMatches, `cannot get boot flags on pre-UC20 device`)
}

func (s *bootFlagsSuite) TestBootFlagsFamilyUC16(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	// make bootloader.Find fail but shouldn't matter
	bootloader.ForceError(errors.New("broken bootloader"))
	defer bootloader.ForceError(nil)

	_ := mylog.Check2(boot.NextBootFlags(coreDev))
	c.Assert(err, ErrorMatches, `cannot get boot flags on pre-UC20 device`)
	mylog.Check(boot.SetNextBootFlags(coreDev, "", []string{"foo"}))
	c.Assert(err, ErrorMatches, `cannot get boot flags on pre-UC20 device`)

	_ = mylog.Check2(boot.BootFlags(coreDev))
	c.Assert(err, ErrorMatches, `cannot get boot flags on pre-UC20 device`)
}

func setupRealGrub(c *C, rootDir, baseDir string, opts *bootloader.Options) bootloader.Bootloader {
	if rootDir == "" {
		rootDir = dirs.GlobalRootDir
	}
	grubCfg := filepath.Join(rootDir, baseDir, "grub.cfg")
	mylog.Check(os.MkdirAll(filepath.Dir(grubCfg), 0755))

	mylog.Check(os.WriteFile(grubCfg, nil, 0644))


	genv := grubenv.NewEnv(filepath.Join(rootDir, baseDir, "grubenv"))
	mylog.Check(genv.Save())


	grubBl := mylog.Check2(bootloader.Find(rootDir, opts))

	c.Assert(grubBl.Name(), Equals, "grub")

	return grubBl
}

func (s *bootFlagsSuite) TestInitramfsActiveBootFlagsUC20InstallModeHappy(c *C) {
	dir := c.MkDir()

	dirs.SetRootDir(dir)
	defer func() { dirs.SetRootDir("") }()

	blDir := boot.InitramfsUbuntuSeedDir

	setupRealGrub(c, blDir, "EFI/ubuntu", &bootloader.Options{Role: bootloader.RoleRecovery})

	flags := mylog.Check2(boot.InitramfsActiveBootFlags(boot.ModeInstall, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")))

	c.Assert(flags, HasLen, 0)
	mylog.

		// if we set some flags via ubuntu-image customizations then we get them
		// back
		Check(boot.SetBootFlagsInBootloader([]string{"factory"}, blDir))


	flags = mylog.Check2(boot.InitramfsActiveBootFlags(boot.ModeInstall, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")))

	c.Assert(flags, DeepEquals, []string{"factory"})
}

func (s *bootFlagsSuite) TestInitramfsActiveBootFlagsUC20FactoryResetModeHappy(c *C) {
	// FactoryReset and Install run identical code, as their condition match pretty closely
	// so this unit test is to reconfirm that we expect same behavior as we see in the unit
	// test for install mode.
	dir := c.MkDir()

	dirs.SetRootDir(dir)
	defer func() { dirs.SetRootDir("") }()

	blDir := boot.InitramfsUbuntuSeedDir

	setupRealGrub(c, blDir, "EFI/ubuntu", &bootloader.Options{Role: bootloader.RoleRecovery})

	flags := mylog.Check2(boot.InitramfsActiveBootFlags(boot.ModeFactoryReset, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")))

	c.Assert(flags, HasLen, 0)
	mylog.

		// if we set some flags via ubuntu-image customizations then we get them
		// back
		Check(boot.SetBootFlagsInBootloader([]string{"factory"}, blDir))


	flags = mylog.Check2(boot.InitramfsActiveBootFlags(boot.ModeFactoryReset, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")))

	c.Assert(flags, DeepEquals, []string{"factory"})
}

func (s *bootFlagsSuite) TestSetImageBootFlagsVerification(c *C) {
	longVal := "longer-than-256-char-value"
	for i := 0; i < 256; i++ {
		longVal += "X"
	}

	r := boot.MockAdditionalBootFlags([]string{longVal})
	defer r()

	blVars := make(map[string]string)
	mylog.Check(boot.SetImageBootFlags([]string{"not-a-real-flag"}, blVars))
	c.Assert(err, ErrorMatches, `unknown boot flags \[not-a-real-flag\] not allowed`)
	mylog.Check(boot.SetImageBootFlags([]string{longVal}, blVars))
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
	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), 0755))

	mylog.Check(m.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")))


	flags := mylog.Check2(boot.InitramfsActiveBootFlags(boot.ModeRecover, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")))

	c.Assert(flags, HasLen, 0)
	mylog.Check(grubBl.SetBootVars(map[string]string{"snapd_boot_flags": "factory"}))


	m.BootFlags = []string{"modeenv-boot-flag"}
	mylog.Check(m.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")))


	// still no flags since we are in recovery mode
	flags = mylog.Check2(boot.InitramfsActiveBootFlags(boot.ModeRecover, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")))

	c.Assert(flags, HasLen, 0)
}

func (s *bootFlagsSuite) testInitramfsActiveBootFlagsUC20RRunModeHappy(c *C, flagsDir string) {
	dir := c.MkDir()

	dirs.SetRootDir(dir)
	defer func() { dirs.SetRootDir("") }()

	// setup a basic empty modeenv
	m := boot.Modeenv{
		Mode:      boot.ModeRun,
		BootFlags: []string{},
	}
	mylog.Check(os.MkdirAll(flagsDir, 0755))

	mylog.Check(m.WriteTo(flagsDir))


	flags := mylog.Check2(boot.InitramfsActiveBootFlags(boot.ModeRun, flagsDir))

	c.Assert(flags, HasLen, 0)

	m.BootFlags = []string{"factory", "other-flag"}
	mylog.Check(m.WriteTo(flagsDir))


	// now some flags after we set them in the modeenv
	flags = mylog.Check2(boot.InitramfsActiveBootFlags(boot.ModeRun, flagsDir))

	c.Assert(flags, DeepEquals, []string{"factory", "other-flag"})
}

func (s *bootFlagsSuite) TestInitramfsActiveBootFlagsUC20RRunModeHappy(c *C) {
	s.testInitramfsActiveBootFlagsUC20RRunModeHappy(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	s.testInitramfsActiveBootFlagsUC20RRunModeHappy(c, c.MkDir())
}

func (s *bootFlagsSuite) TestInitramfsSetBootFlags(c *C) {
	tt := []struct {
		flags               []string
		expFlags            []string
		expFlagFile         string
		bootFlagsErr        string
		bootFlagsErrUnknown bool
	}{
		{
			flags:       []string{"factory"},
			expFlags:    []string{"factory"},
			expFlagFile: "factory",
		},
		{
			flags:               []string{"factory", "unknown-new-flag"},
			expFlagFile:         "factory,unknown-new-flag",
			expFlags:            []string{"factory"},
			bootFlagsErr:        `unknown boot flags \[unknown-new-flag\] not allowed`,
			bootFlagsErrUnknown: true,
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
		mylog.Check(boot.InitramfsExposeBootFlagsForSystem(t.flags))

		c.Assert(filepath.Join(dirs.SnapRunDir, "boot-flags"), testutil.FileEquals, t.expFlagFile)

		// also read the flags as if from user space to make sure they match
		flags := mylog.Check2(boot.BootFlags(uc20Dev))
		if t.bootFlagsErr != "" {
			c.Assert(err, ErrorMatches, t.bootFlagsErr)
			if t.bootFlagsErrUnknown {
				c.Assert(boot.IsUnknownBootFlagError(err), Equals, true)
			}
		} else {

		}
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
			err:   `unknown boot flags \[new-unsupported-flag\] not allowed`,
		},
		{
			flags: []string{""},
			err:   `unknown boot flags \[\"\"\] not allowed`,
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
		mylog.Check(m.WriteTo(""))

		mylog.Check(boot.SetNextBootFlags(uc20Dev, "", t.flags))
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err)
			continue
		}



		// re-read modeenv
		m2 := mylog.Check2(boot.ReadModeenv(""))

		c.Assert(m2.BootFlags, DeepEquals, t.expFlags)

		// get the next boot flags with NextBootFlags and compare with expected
		flags := mylog.Check2(boot.NextBootFlags(uc20Dev))
		c.Assert(flags, DeepEquals, t.expFlags)

	}
}

func (s *bootFlagsSuite) TestRunModeRootfs(c *C) {
	uc20Dev := boottest.MockUC20Device("run", nil)
	classicModesDev := boottest.MockClassicWithModesDevice("run", nil)

	tt := []struct {
		mode               string
		dev                snap.Device
		createExpDirs      bool
		expDirs            []string
		noExpDirRootPrefix bool
		degradedJSON       string
		err                string
		comment            string
	}{
		{
			mode:    boot.ModeRun,
			dev:     uc20Dev,
			expDirs: []string{"/run/mnt/data", ""},
			comment: "run mode",
		},
		{
			mode:    boot.ModeRun,
			dev:     classicModesDev,
			expDirs: []string{"/run/mnt/data", ""},
			comment: "run mode (classic)",
		},
		{
			mode:    boot.ModeInstall,
			dev:     uc20Dev,
			comment: "install mode before partition creation",
		},
		{
			mode:    boot.ModeInstall,
			dev:     classicModesDev,
			comment: "install mode before partition creation (classic)",
		},
		{
			mode:    boot.ModeFactoryReset,
			dev:     uc20Dev,
			comment: "factory-reset mode before partition is recreated",
		},
		{
			mode:    boot.ModeFactoryReset,
			dev:     uc20Dev,
			comment: "factory-reset mode before partition is recreated (classic)",
		},
		{
			mode:          boot.ModeInstall,
			dev:           uc20Dev,
			expDirs:       []string{"/run/mnt/ubuntu-data"},
			createExpDirs: true,
			comment:       "install mode after partition creation",
		},
		{
			mode:          boot.ModeInstall,
			dev:           classicModesDev,
			expDirs:       []string{"/run/mnt/ubuntu-data"},
			createExpDirs: true,
			comment:       "install mode after partition creation (classic)",
		},
		{
			mode:          boot.ModeFactoryReset,
			dev:           uc20Dev,
			expDirs:       []string{"/run/mnt/ubuntu-data"},
			createExpDirs: true,
			comment:       "factory-reset mode after partition creation",
		},
		{
			mode:          boot.ModeFactoryReset,
			dev:           classicModesDev,
			expDirs:       []string{"/run/mnt/ubuntu-data"},
			createExpDirs: true,
			comment:       "factory-reset mode after partition creation (classic)",
		},
		{
			mode: boot.ModeRecover,
			dev:  uc20Dev,
			degradedJSON: `
			{
				"ubuntu-data": {
					"mount-state": "mounted",
					"mount-location": "/host/ubuntu-data"
				}
			}
			`,
			expDirs:            []string{"/host/ubuntu-data"},
			noExpDirRootPrefix: true,
			comment:            "recover degraded.json default mounted location",
		},
		{
			mode: boot.ModeRecover,
			dev:  uc20Dev,
			degradedJSON: `
			{
				"ubuntu-data": {
					"mount-state": "mounted",
					"mount-location": "/host/elsewhere/ubuntu-data"
				}
			}
			`,
			expDirs:            []string{"/host/elsewhere/ubuntu-data"},
			noExpDirRootPrefix: true,
			comment:            "recover degraded.json alternative mounted location",
		},
		{
			mode: boot.ModeRecover,
			dev:  uc20Dev,
			degradedJSON: `
			{
				"ubuntu-data": {
					"mount-state": "error-mounting"
				}
			}
			`,
			comment: "recover degraded.json error-mounting",
		},
		{
			mode: boot.ModeRecover,
			dev:  uc20Dev,
			degradedJSON: `
			{
				"ubuntu-data": {
					"mount-state": "mounted-untrusted"
				}
			}
			`,
			comment: "recover degraded.json mounted-untrusted",
		},
		{
			mode: boot.ModeRecover,
			dev:  uc20Dev,
			degradedJSON: `
			{
				"ubuntu-data": {
					"mount-state": "absent-but-optional"
				}
			}
			`,
			comment: "recover degraded.json absent-but-optional",
		},
		{
			mode: boot.ModeRecover,
			dev:  uc20Dev,
			degradedJSON: `
			{
				"ubuntu-data": {
					"mount-state": "new-wild-unknown-state"
				}
			}
			`,
			comment: "recover degraded.json new-wild-unknown-state",
		},
		{
			mode:    "",
			dev:     uc20Dev,
			err:     "system mode is unsupported",
			comment: "unsupported system mode",
		},
	}
	for _, t := range tt {
		comment := Commentf(t.comment)
		if t.degradedJSON != "" {
			rootdir := c.MkDir()
			dirs.SetRootDir(rootdir)
			defer func() { dirs.SetRootDir("") }()

			degradedJSON := filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json")
			mylog.Check(os.MkdirAll(dirs.SnapBootstrapRunDir, 0755))
			c.Assert(err, IsNil, comment)
			mylog.Check(os.WriteFile(degradedJSON, []byte(t.degradedJSON), 0644))
			c.Assert(err, IsNil, comment)
		}

		if t.createExpDirs {
			for _, dir := range t.expDirs {
				mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, dir), 0755))
				c.Assert(err, IsNil, comment)
			}
		}

		dataMountDirs := mylog.Check2(boot.HostUbuntuDataForMode(t.mode, t.dev.Model()))
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
			c.Assert(dataMountDirs, IsNil)
			continue
		}
		c.Assert(err, IsNil, comment)

		if t.expDirs != nil && !t.noExpDirRootPrefix {
			// prefix all the dirs in expDirs with dirs.GlobalRootDir for easier
			// test case writing above
			prefixedDir := make([]string, len(t.expDirs))
			for i, dir := range t.expDirs {
				prefixedDir[i] = filepath.Join(dirs.GlobalRootDir, dir)
			}
			c.Assert(dataMountDirs, DeepEquals, prefixedDir, comment)
		} else {
			c.Assert(dataMountDirs, DeepEquals, t.expDirs, comment)
		}
	}
}
