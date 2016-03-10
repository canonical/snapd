// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package apparmor_test

import (
	"io/ioutil"
	"path"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/interfaces/apparmor"
	"github.com/ubuntu-core/snappy/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type appArmorSuite struct {
	testutil.BaseTest
	profilesFilename string
}

var _ = Suite(&appArmorSuite{})

func (s *appArmorSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// Mock the list of profiles in the running kernel
	s.profilesFilename = path.Join(c.MkDir(), "profiles")
	apparmor.MockProfilesPath(&s.BaseTest, s.profilesFilename)
}

// Tests for LoadProfile()

func (s *appArmorSuite) TestLoadProfileRunsAppArmorParserReplace(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", 0)
	defer cmd.Restore()
	err := apparmor.LoadProfile("foo.snap")
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, []string{
		"--replace foo.snap",
	})
}

func (s *appArmorSuite) TestLoadProfileReportsErrors(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", 42)
	defer cmd.Restore()
	err := apparmor.LoadProfile("foo.snap")
	c.Assert(err, ErrorMatches, "exit status 42")
}

// Tests for Profile.Unload()

func (s *appArmorSuite) TestUnloadProfileRunsAppArmorParserRemove(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", 0)
	defer cmd.Restore()
	profile := apparmor.Profile{Name: "foo.snap"}
	err := profile.Unload()
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, []string{"--remove foo.snap"})
}

func (s *appArmorSuite) TestUnloadProfileReportsErrors(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", 42)
	defer cmd.Restore()
	profile := apparmor.Profile{Name: "foo.snap"}
	err := profile.Unload()
	c.Assert(err, ErrorMatches, "exit status 42")
}

// Tests for LoadedProfiles()

func (s *appArmorSuite) TestLoadedApparmorProfilesReturnsErrorOnMissingFile(c *C) {
	profiles, err := apparmor.LoadedProfiles()
	c.Assert(err, ErrorMatches, "open .*: no such file or directory")
	c.Check(profiles, IsNil)
}

func (s *appArmorSuite) TestLoadedApparmorProfilesCanParseEmptyFile(c *C) {
	ioutil.WriteFile(s.profilesFilename, []byte(""), 0600)
	profiles, err := apparmor.LoadedProfiles()
	c.Assert(err, IsNil)
	c.Check(profiles, HasLen, 0)
}

func (s *appArmorSuite) TestLoadedApparmorProfilesParsesAndFiltersData(c *C) {
	ioutil.WriteFile(s.profilesFilename, []byte(
		// The output contains some of the snappy-specific elements
		// and some non-snappy elements pulled from Ubuntu 16.04 desktop
		//
		// The pi2-piglow.{background,foreground}.snap entries are the only
		// ones that should be reported by the function.
		`/sbin/dhclient (enforce)
/usr/bin/ubuntu-core-launcher (enforce)
/usr/bin/ubuntu-core-launcher (enforce)
/usr/lib/NetworkManager/nm-dhcp-client.action (enforce)
/usr/lib/NetworkManager/nm-dhcp-helper (enforce)
/usr/lib/connman/scripts/dhclient-script (enforce)
/usr/lib/lightdm/lightdm-guest-session (enforce)
/usr/lib/lightdm/lightdm-guest-session//chromium (enforce)
/usr/lib/telepathy/telepathy-* (enforce)
/usr/lib/telepathy/telepathy-*//pxgsettings (enforce)
/usr/lib/telepathy/telepathy-*//sanitized_helper (enforce)
pi2-piglow.background.snap (enforce)
pi2-piglow.foreground.snap (enforce)
webbrowser-app (enforce)
webbrowser-app//oxide_helper (enforce)
`), 0600)
	profiles, err := apparmor.LoadedProfiles()
	c.Assert(err, IsNil)
	c.Check(profiles, DeepEquals, []apparmor.Profile{
		{"pi2-piglow.background.snap", "enforce"},
		{"pi2-piglow.foreground.snap", "enforce"},
	})
}
