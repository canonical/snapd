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
	"os"
	"path"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
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

// Tests for LoadProfiles()

func (s *appArmorSuite) TestLoadProfilesRunsAppArmorParserReplace(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	err := apparmor.LoadProfiles([]string{"/path/to/snap.samba.smbd"}, dirs.AppArmorCacheDir, 0)
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", "--cache-loc=/var/cache/apparmor", "--quiet", "/path/to/snap.samba.smbd"},
	})
}

func (s *appArmorSuite) TestLoadProfilesMany(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	err := apparmor.LoadProfiles([]string{"/path/to/snap.samba.smbd", "/path/to/another.profile"}, dirs.AppArmorCacheDir, 0)
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", "--cache-loc=/var/cache/apparmor", "--quiet", "/path/to/snap.samba.smbd", "/path/to/another.profile"},
	})
}

func (s *appArmorSuite) TestLoadProfilesNone(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	err := apparmor.LoadProfiles([]string{}, dirs.AppArmorCacheDir, 0)
	c.Assert(err, IsNil)
	c.Check(cmd.Calls(), HasLen, 0)
}

func (s *appArmorSuite) TestLoadProfilesReportsErrors(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", "exit 42")
	defer cmd.Restore()
	err := apparmor.LoadProfiles([]string{"/path/to/snap.samba.smbd"}, dirs.AppArmorCacheDir, 0)
	c.Assert(err.Error(), Equals, `cannot load apparmor profiles: exit status 42
apparmor_parser output:
`)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", "--cache-loc=/var/cache/apparmor", "--quiet", "/path/to/snap.samba.smbd"},
	})
}

func (s *appArmorSuite) TestLoadProfilesRunsAppArmorParserReplaceWithSnapdDebug(c *C) {
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	err := apparmor.LoadProfiles([]string{"/path/to/snap.samba.smbd"}, dirs.AppArmorCacheDir, 0)
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", "--cache-loc=/var/cache/apparmor", "/path/to/snap.samba.smbd"},
	})
}

// Tests for Profile.Unload()

func (s *appArmorSuite) TestUnloadProfilesMany(c *C) {
	err := apparmor.UnloadProfiles([]string{"/path/to/snap.samba.smbd", "/path/to/another.profile"}, dirs.AppArmorCacheDir)
	c.Assert(err, IsNil)
}

func (s *appArmorSuite) TestUnloadProfilesNone(c *C) {
	err := apparmor.UnloadProfiles([]string{}, dirs.AppArmorCacheDir)
	c.Assert(err, IsNil)
}

func (s *appArmorSuite) TestUnloadRemovesCachedProfile(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	err := os.MkdirAll(dirs.AppArmorCacheDir, 0755)
	c.Assert(err, IsNil)

	fname := filepath.Join(dirs.AppArmorCacheDir, "profile")
	ioutil.WriteFile(fname, []byte("blob"), 0600)
	err = apparmor.UnloadProfiles([]string{"profile"}, dirs.AppArmorCacheDir)
	c.Assert(err, IsNil)
	_, err = os.Stat(fname)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *appArmorSuite) TestUnloadRemovesCachedProfileInForest(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	err := os.MkdirAll(dirs.AppArmorCacheDir, 0755)
	c.Assert(err, IsNil)
	// mock the forest subdir and features file
	subdir := filepath.Join(dirs.AppArmorCacheDir, "deadbeef.0")
	err = os.MkdirAll(subdir, 0700)
	c.Assert(err, IsNil)
	features := filepath.Join(subdir, ".features")
	ioutil.WriteFile(features, []byte("blob"), 0644)

	fname := filepath.Join(subdir, "profile")
	ioutil.WriteFile(fname, []byte("blob"), 0600)
	err = apparmor.UnloadProfiles([]string{"profile"}, dirs.AppArmorCacheDir)
	c.Assert(err, IsNil)
	_, err = os.Stat(fname)
	c.Check(os.IsNotExist(err), Equals, true)
	c.Check(osutil.FileExists(features), Equals, true)
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
snap.pi2-piglow.background (enforce)
snap.pi2-piglow.foreground (enforce)
webbrowser-app (enforce)
webbrowser-app//oxide_helper (enforce)
`), 0600)
	profiles, err := apparmor.LoadedProfiles()
	c.Assert(err, IsNil)
	c.Check(profiles, DeepEquals, []string{
		"snap.pi2-piglow.background",
		"snap.pi2-piglow.foreground",
	})
}

func (s *appArmorSuite) TestLoadedApparmorProfilesHandlesParsingErrors(c *C) {
	ioutil.WriteFile(s.profilesFilename, []byte("broken stuff here\n"), 0600)
	profiles, err := apparmor.LoadedProfiles()
	c.Assert(err, ErrorMatches, "newline in format does not match input")
	c.Check(profiles, IsNil)
	ioutil.WriteFile(s.profilesFilename, []byte("truncated"), 0600)
	profiles, err = apparmor.LoadedProfiles()
	c.Assert(err, ErrorMatches, `syntax error, expected: name \(mode\)`)
	c.Check(profiles, IsNil)
}

func (s *appArmorSuite) TestValidateFreeFromAAREUnhappy(c *C) {
	var testCases = []string{"a?", "*b", "c[c", "dd]", "e{", "f}", "g^", `h"`, "f\000", "g\x00"}

	for _, s := range testCases {
		c.Check(apparmor.ValidateNoAppArmorRegexp(s), ErrorMatches, ".* contains a reserved apparmor char from .*", Commentf("%q is not raising an error", s))
	}
}

func (s *appArmorSuite) TestValidateFreeFromAAREhappy(c *C) {
	var testCases = []string{"foo", "BaR", "b-z", "foo+bar", "b00m!", "be/ep", "a%b", "a&b", "a(b", "a)b", "a=b", "a#b", "a~b", "a'b", "a_b", "a,b", "a;b", "a>b", "a<b", "a|b"}

	for _, s := range testCases {
		c.Check(apparmor.ValidateNoAppArmorRegexp(s), IsNil, Commentf("%q raised an error but shouldn't", s))
	}
}

func (s *appArmorSuite) TestMaybeSetNumberOfJobs(c *C) {
	var cpus int
	restore := apparmor.MockRuntimeNumCPU(func() int {
		return cpus
	})
	defer restore()

	cpus = 10
	c.Check(apparmor.MaybeSetNumberOfJobs(), Equals, "-j8")

	cpus = 2
	c.Check(apparmor.MaybeSetNumberOfJobs(), Equals, "-j1")

	cpus = 1
	c.Check(apparmor.MaybeSetNumberOfJobs(), Equals, "")
}
