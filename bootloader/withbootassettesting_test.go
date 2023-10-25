// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build withbootassetstesting

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

package bootloader_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/snapdenv"
)

type withbootasetstestingTestSuite struct {
	baseBootenvTestSuite
}

var _ = Suite(&withbootasetstestingTestSuite{})

func (s *withbootasetstestingTestSuite) testInjects(c *C, role bootloader.Role) {
	markerFile := "bootassetstesting"
	grubCfgAsset := "grub.cfg"
	if role == bootloader.RoleRecovery {
		markerFile = "recoverybootassetstesting"
		grubCfgAsset = "grub-recovery.cfg"
	}
	snippetName := grubCfgAsset + ":static-cmdline"

	d := c.MkDir()
	c.Assert(os.WriteFile(filepath.Join(d, markerFile), []byte("with-bootassetstesting\n"), 0644), IsNil)
	restore := bootloader.MockMaybeInjectOsReadlink(func(_ string) (string, error) {
		return filepath.Join(d, "foo"), nil
	})
	defer restore()
	restore = snapdenv.MockTesting(true)
	defer restore()
	restore = assets.MockSnippetsForEdition(snippetName, []assets.ForEditions{
		{FirstEdition: 2, Snippet: []byte(`foo bar baz`)},
	})
	defer restore()
	restore = assets.MockInternal(grubCfgAsset, []byte(`# Snapd-Boot-Config-Edition: 5
set snapd_static_cmdline_args='foo bar baz'
this is mocked grub-recovery.conf
`))
	defer restore()

	bootloader.MaybeInjectTestingBootloaderAssets(role)

	bumped := assets.Internal(grubCfgAsset)
	c.Check(string(bumped), Equals, `# Snapd-Boot-Config-Edition: 6
set snapd_static_cmdline_args='foo bar baz with-bootassetstesting'
this is mocked grub-recovery.conf
`)
	cmdline := bootloader.StaticCommandLineForGrubAssetEdition(grubCfgAsset, 6)
	c.Check(cmdline, Equals, `foo bar baz with-bootassetstesting`)
}

func (s *withbootasetstestingTestSuite) TestInjectsRun(c *C) {
	s.testInjects(c, bootloader.RoleRunMode)
}

func (s *withbootasetstestingTestSuite) TestInjectsRecovery(c *C) {
	s.testInjects(c, bootloader.RoleRecovery)
}

func (s *withbootasetstestingTestSuite) testNoMarker(c *C, role bootloader.Role) {
	grubCfgAsset := "grub.cfg"
	if role == bootloader.RoleRecovery {
		grubCfgAsset = "grub-recovery.cfg"
	}
	snippetName := grubCfgAsset + ":static-cmdline"

	d := c.MkDir()
	restore := bootloader.MockMaybeInjectOsReadlink(func(_ string) (string, error) {
		return filepath.Join(d, "foo"), nil
	})
	defer restore()
	restore = snapdenv.MockTesting(true)
	defer restore()
	restore = assets.MockSnippetsForEdition(snippetName, []assets.ForEditions{
		{FirstEdition: 2, Snippet: []byte(`foo bar baz`)},
	})
	defer restore()
	grubCfg := `# Snapd-Boot-Config-Edition: 5
set snapd_static_cmdline_args='foo bar baz'
this is mocked grub-recovery.conf
`
	restore = assets.MockInternal(grubCfgAsset, []byte(grubCfg))
	defer restore()

	bootloader.MaybeInjectTestingBootloaderAssets(bootloader.RoleRunMode)

	notBumped := assets.Internal(grubCfgAsset)
	c.Check(string(notBumped), Equals, grubCfg)
	cmdline := bootloader.StaticCommandLineForGrubAssetEdition(grubCfgAsset, 5)
	c.Check(cmdline, Equals, `foo bar baz`)
}

func (s *withbootasetstestingTestSuite) TestNoMarkerRun(c *C) {
	s.testInjects(c, bootloader.RoleRunMode)
}

func (s *withbootasetstestingTestSuite) TestNoMarkerRecovery(c *C) {
	s.testInjects(c, bootloader.RoleRecovery)
}
