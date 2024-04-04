// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/testutil"
)

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
	dirs.SetRootDir("")
	s.AddCleanup(apparmor.MockFeatures(nil, nil, nil, nil))
}

// Tests for LoadProfiles()

func (s *appArmorSuite) TestLoadProfilesRunsAppArmorParserReplace(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	restore := apparmor.MockParserSearchPath(cmd.BinDir())
	defer restore()
	err := apparmor.LoadProfiles([]string{"/path/to/snap.samba.smbd"}, apparmor.CacheDir, 0)
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", dirs.GlobalRootDir), "--quiet", "/path/to/snap.samba.smbd"},
	})
}

func (s *appArmorSuite) TestLoadProfilesMany(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	restore := apparmor.MockParserSearchPath(cmd.BinDir())
	defer restore()
	err := apparmor.LoadProfiles([]string{"/path/to/snap.samba.smbd", "/path/to/another.profile"}, apparmor.CacheDir, 0)
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", dirs.GlobalRootDir), "--quiet", "/path/to/snap.samba.smbd", "/path/to/another.profile"},
	})
}

func (s *appArmorSuite) TestLoadProfilesNone(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	restore := apparmor.MockParserSearchPath(cmd.BinDir())
	defer restore()
	err := apparmor.LoadProfiles([]string{}, apparmor.CacheDir, 0)
	c.Assert(err, IsNil)
	c.Check(cmd.Calls(), HasLen, 0)
}

func (s *appArmorSuite) TestLoadProfilesReportsErrors(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	cmd := testutil.MockCommand(c, "apparmor_parser", "exit 42")
	defer cmd.Restore()
	restore := apparmor.MockParserSearchPath(cmd.BinDir())
	defer restore()
	err := apparmor.LoadProfiles([]string{"/path/to/snap.samba.smbd"}, apparmor.CacheDir, 0)
	c.Assert(err.Error(), Equals, `cannot load apparmor profiles: exit status 42
apparmor_parser output:
`)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", dirs.GlobalRootDir), "--quiet", "/path/to/snap.samba.smbd"},
	})
}

func (s *appArmorSuite) TestLoadProfilesReportsErrorWithZeroExitStatus(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	cmd := testutil.MockCommand(c, "apparmor_parser", "echo parser error; exit 0")
	defer cmd.Restore()
	restore := apparmor.MockParserSearchPath(cmd.BinDir())
	defer restore()
	err := apparmor.LoadProfiles([]string{"/path/to/snap.samba.smbd"}, apparmor.CacheDir, 0)
	c.Assert(err.Error(), Equals, `cannot load apparmor profiles: exit status 0 with parser error
apparmor_parser output:
parser error
`)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", dirs.GlobalRootDir), "--quiet", "/path/to/snap.samba.smbd"},
	})
}

func (s *appArmorSuite) TestLoadProfilesRunsAppArmorParserReplaceWithSnapdDebug(c *C) {
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	restore := apparmor.MockParserSearchPath(cmd.BinDir())
	defer restore()
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	err := apparmor.LoadProfiles([]string{"/path/to/snap.samba.smbd"}, apparmor.CacheDir, 0)
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", dirs.GlobalRootDir), "/path/to/snap.samba.smbd"},
	})
}

// Tests for Profile.RemoveCachedProfiles()

func (s *appArmorSuite) TestRemoveCachedProfilesMany(c *C) {
	err := apparmor.RemoveCachedProfiles([]string{"/path/to/snap.samba.smbd", "/path/to/another.profile"}, apparmor.CacheDir)
	c.Assert(err, IsNil)
}

func (s *appArmorSuite) TestRemoveCachedProfilesNone(c *C) {
	err := apparmor.RemoveCachedProfiles([]string{}, apparmor.CacheDir)
	c.Assert(err, IsNil)
}

func (s *appArmorSuite) TestRemoveCachedProfiles(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	restore := apparmor.MockParserSearchPath(cmd.BinDir())
	defer restore()

	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	err := os.MkdirAll(apparmor.CacheDir, 0755)
	c.Assert(err, IsNil)

	fname := filepath.Join(apparmor.CacheDir, "profile")
	os.WriteFile(fname, []byte("blob"), 0600)
	err = apparmor.RemoveCachedProfiles([]string{"profile"}, apparmor.CacheDir)
	c.Assert(err, IsNil)
	_, err = os.Stat(fname)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *appArmorSuite) TestRemoveCachedProfilesInForest(c *C) {
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	restore := apparmor.MockParserSearchPath(cmd.BinDir())
	defer restore()

	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	err := os.MkdirAll(apparmor.CacheDir, 0755)
	c.Assert(err, IsNil)
	// mock the forest subdir and features file
	subdir := filepath.Join(apparmor.CacheDir, "deadbeef.0")
	err = os.MkdirAll(subdir, 0700)
	c.Assert(err, IsNil)
	features := filepath.Join(subdir, ".features")
	os.WriteFile(features, []byte("blob"), 0644)

	fname := filepath.Join(subdir, "profile")
	os.WriteFile(fname, []byte("blob"), 0600)
	err = apparmor.RemoveCachedProfiles([]string{"profile"}, apparmor.CacheDir)
	c.Assert(err, IsNil)
	_, err = os.Stat(fname)
	c.Check(os.IsNotExist(err), Equals, true)
	c.Check(osutil.FileExists(features), Equals, true)
}

func (s *appArmorSuite) TestReloadAllSnapProfilesFailure(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Create a couple of empty profiles
	err := os.MkdirAll(dirs.SnapAppArmorDir, 0755)
	defer func() {
		os.RemoveAll(dirs.SnapAppArmorDir)
	}()
	c.Assert(err, IsNil)
	var profiles []string
	for _, profile := range []string{"app1", "second_app"} {
		path := filepath.Join(dirs.SnapAppArmorDir, profile)
		f, err := os.Create(path)
		f.Close()
		c.Assert(err, IsNil)
		profiles = append(profiles, path)
	}

	var passedProfiles []string
	restore := apparmor.MockLoadProfiles(func(paths []string, cacheDir string, flags apparmor.AaParserFlags) error {
		passedProfiles = paths
		return errors.New("reload error")
	})
	defer restore()
	err = apparmor.ReloadAllSnapProfiles()
	c.Check(passedProfiles, DeepEquals, profiles)
	c.Assert(err, ErrorMatches, "reload error")
}

func (s *appArmorSuite) TestReloadAllSnapProfilesHappy(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Create a couple of empty profiles
	err := os.MkdirAll(dirs.SnapAppArmorDir, 0755)
	defer func() {
		os.RemoveAll(dirs.SnapAppArmorDir)
	}()
	c.Assert(err, IsNil)
	var profiles []string
	for _, profile := range []string{"first", "second", "third"} {
		path := filepath.Join(dirs.SnapAppArmorDir, profile)
		f, err := os.Create(path)
		f.Close()
		c.Assert(err, IsNil)
		profiles = append(profiles, path)
	}

	const snapConfineProfile = "/etc/apparmor.d/some.where.snap-confine"
	restore := apparmor.MockSnapConfineDistroProfilePath(func() string {
		return snapConfineProfile
	})
	defer restore()
	profiles = append(profiles, snapConfineProfile)

	var passedProfiles []string
	var passedCacheDir string
	var passedFlags apparmor.AaParserFlags
	restore = apparmor.MockLoadProfiles(func(paths []string, cacheDir string, flags apparmor.AaParserFlags) error {
		passedProfiles = paths
		passedCacheDir = cacheDir
		passedFlags = flags
		return nil
	})
	defer restore()

	err = apparmor.ReloadAllSnapProfiles()
	c.Check(passedProfiles, DeepEquals, profiles)
	c.Check(passedCacheDir, Equals, filepath.Join(dirs.GlobalRootDir, "/var/cache/apparmor"))
	c.Check(passedFlags, Equals, apparmor.SkipReadCache)
	c.Assert(err, IsNil)
}

// Tests for LoadedProfiles()

func (s *appArmorSuite) TestLoadedApparmorProfilesReturnsErrorOnMissingFile(c *C) {
	profiles, err := apparmor.LoadedProfiles()
	c.Assert(err, ErrorMatches, "open .*: no such file or directory")
	c.Check(profiles, IsNil)
}

func (s *appArmorSuite) TestLoadedApparmorProfilesCanParseEmptyFile(c *C) {
	os.WriteFile(s.profilesFilename, []byte(""), 0600)
	profiles, err := apparmor.LoadedProfiles()
	c.Assert(err, IsNil)
	c.Check(profiles, HasLen, 0)
}

func (s *appArmorSuite) TestLoadedApparmorProfilesParsesAndFiltersData(c *C) {
	os.WriteFile(s.profilesFilename, []byte(
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
	os.WriteFile(s.profilesFilename, []byte("broken stuff here\n"), 0600)
	profiles, err := apparmor.LoadedProfiles()
	c.Assert(err, ErrorMatches, "newline in format does not match input")
	c.Check(profiles, IsNil)
	os.WriteFile(s.profilesFilename, []byte("truncated"), 0600)
	profiles, err = apparmor.LoadedProfiles()
	c.Assert(err, ErrorMatches, `syntax error, expected: name \(mode\)`)
	c.Check(profiles, IsNil)
}

func (s *appArmorSuite) TestMaybeSetNumberOfJobs(c *C) {
	var cpus int
	restore := apparmor.MockRuntimeNumCPU(func() int {
		return cpus
	})
	defer restore()

	cpus = 10
	c.Check(apparmor.NumberOfJobsParam(), Equals, "-j8")

	cpus = 2
	c.Check(apparmor.NumberOfJobsParam(), Equals, "-j1")

	cpus = 1
	c.Check(apparmor.NumberOfJobsParam(), Equals, "-j1")
}

func (s *appArmorSuite) TestSnapConfineDistroProfilePath(c *C) {
	baseDir := c.MkDir()
	restore := testutil.Backup(&apparmor.ConfDir)
	apparmor.ConfDir = filepath.Join(baseDir, "/a/b/c")
	defer restore()

	for _, testData := range []struct {
		existingFiles []string
		expectedPath  string
	}{
		{[]string{}, ""},
		{[]string{"/a/b/c/usr.lib.snapd.snap-confine.real"}, "/a/b/c/usr.lib.snapd.snap-confine.real"},
		{[]string{"/a/b/c/usr.lib.snapd.snap-confine"}, "/a/b/c/usr.lib.snapd.snap-confine"},
		{[]string{"/a/b/c/usr.libexec.snapd.snap-confine"}, "/a/b/c/usr.libexec.snapd.snap-confine"},
		{
			[]string{"/a/b/c/usr.lib.snapd.snap-confine.real", "/a/b/c/usr.lib.snapd.snap-confine"},
			"/a/b/c/usr.lib.snapd.snap-confine.real",
		},
	} {
		// Remove leftovers from the previous iteration
		err := os.RemoveAll(baseDir)
		c.Assert(err, IsNil)

		existingFiles := testData.existingFiles
		for _, path := range existingFiles {
			fullPath := filepath.Join(baseDir, path)
			err := os.MkdirAll(filepath.Dir(fullPath), 0755)
			c.Assert(err, IsNil)
			err = os.WriteFile(fullPath, []byte("I'm an ELF binary"), 0755)
			c.Assert(err, IsNil)
		}
		var expectedPath string
		if testData.expectedPath != "" {
			expectedPath = filepath.Join(baseDir, testData.expectedPath)
		}
		path := apparmor.SnapConfineDistroProfilePath()
		c.Check(path, Equals, expectedPath, Commentf("Existing: %q", existingFiles))
	}
}

func (s *appArmorSuite) TestSetupSnapConfineSnippetsNoSnippets(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	restore := osutil.MockIsHomeUsingRemoteFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()
	restore = apparmor.MockLoadHomedirs(func() ([]string, error) { return nil, nil })
	defer restore()

	// No features, no remote file system, no Overlay, no homedirs
	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	c.Check(err, IsNil)
	c.Check(wasChanged, Equals, false)

	// Because overlay/remote file system is not used there are no local
	// policy files but the directory was created.
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)
}

func (s *appArmorSuite) writeSystemParams(c *C, homedirs []string) {
	sspPath := dirs.SnapSystemParamsUnder(dirs.GlobalRootDir)
	conents := fmt.Sprintf("homedirs=%s\n", strings.Join(homedirs, ","))

	c.Assert(os.MkdirAll(path.Dir(sspPath), 0755), IsNil)
	c.Assert(os.WriteFile(sspPath, []byte(conents), 0644), IsNil)
}

func (s *appArmorSuite) TestSetupSnapConfineSnippetsHomedirs(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	restore := osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()
	restore = osutil.MockIsHomeUsingRemoteFS(func() (bool, error) { return false, nil })
	defer restore()

	// Setup the system-params which is read by loadHomedirs in SetupSnapConfineSnippets
	// to verify it's correctly read and loaded.
	s.writeSystemParams(c, []string{"/mnt/foo", "/mnt/bar"})

	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	c.Check(err, IsNil)
	c.Check(wasChanged, Equals, true)

	// Homedirs was specified, so we expect an entry for each homedir in a
	// snippet 'homedirs'
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "homedirs")
	fi, err := files[0].Info()
	c.Assert(err, IsNil)
	c.Assert(fi.Mode().Perm(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	c.Assert(filepath.Join(apparmor.SnapConfineAppArmorDir, files[0].Name()),
		testutil.FileContains, `"/mnt/foo/" -> "/tmp/snap.rootfs_*/mnt/foo/",`)
	c.Assert(filepath.Join(apparmor.SnapConfineAppArmorDir, files[0].Name()),
		testutil.FileContains, `"/mnt/bar/" -> "/tmp/snap.rootfs_*/mnt/bar/",`)
}

func (s *appArmorSuite) TestSetupSnapConfineGeneratedPolicyWithHomedirsLoadError(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	log, restore := logger.MockLogger()
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()
	restore = osutil.MockIsHomeUsingRemoteFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = apparmor.MockLoadHomedirs(func() ([]string, error) { return nil, fmt.Errorf("failed to load") })
	defer restore()

	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	c.Check(err, IsNil)
	c.Check(wasChanged, Equals, false)

	// Probing apparmor_parser capabilities failed, so nothing gets written
	// to the snap-confine policy directory
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)

	// But an error was logged
	c.Assert(log.String(), testutil.Contains, "cannot determine if any homedirs are set: failed to load")
}

func (s *appArmorSuite) TestSetupSnapConfineSnippetsBPF(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	restore := osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()
	restore = osutil.MockIsHomeUsingRemoteFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = apparmor.MockLoadHomedirs(func() ([]string, error) { return nil, nil })
	defer restore()

	// Pretend apparmor_parser supports bpf capability
	restore = apparmor.MockFeatures(nil, nil, []string{"cap-bpf"}, nil)
	defer restore()

	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	c.Check(err, IsNil)
	c.Check(wasChanged, Equals, true)

	// Capability bpf is supported by the parser, so an extra policy file
	// for snap-confine is present
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "cap-bpf")
	fi, err := files[0].Info()
	c.Assert(err, IsNil)
	c.Assert(fi.Mode().Perm(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	c.Assert(filepath.Join(apparmor.SnapConfineAppArmorDir, files[0].Name()),
		testutil.FileContains, "capability bpf,")
}

func (s *appArmorSuite) TestSetupSnapConfineGeneratedPolicyWithBPFProbeError(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	log, restore := logger.MockLogger()
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()
	restore = osutil.MockIsHomeUsingRemoteFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = apparmor.MockLoadHomedirs(func() ([]string, error) { return nil, nil })
	defer restore()

	// Probing for apparmor_parser features failed
	restore = apparmor.MockFeatures(nil, nil, nil, fmt.Errorf("mock probe error"))
	defer restore()

	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	c.Check(err, IsNil)
	c.Check(wasChanged, Equals, false)

	// Probing apparmor_parser capabilities failed, so nothing gets written
	// to the snap-confine policy directory
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)

	// But an error was logged
	c.Assert(log.String(), testutil.Contains, "cannot determine apparmor_parser features: mock probe error")
}

func (s *appArmorSuite) TestSetupSnapConfineSnippetsOverlay(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Make it appear as if overlay workaround was needed.
	restore := osutil.MockIsRootWritableOverlay(func() (string, error) { return "/upper", nil })
	defer restore()
	restore = osutil.MockIsHomeUsingRemoteFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = apparmor.MockLoadHomedirs(func() ([]string, error) { return nil, nil })
	defer restore()

	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	c.Check(err, IsNil)
	c.Check(wasChanged, Equals, true)

	// Because overlay is being used, we have the extra policy file.
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "overlay-root")
	fi, err := files[0].Info()
	c.Assert(err, IsNil)
	c.Assert(fi.Mode().Perm(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	// The policy allows upperdir access.
	data, err := os.ReadFile(filepath.Join(apparmor.SnapConfineAppArmorDir, files[0].Name()))
	c.Assert(err, IsNil)
	c.Assert(string(data), testutil.Contains, "\"/upper/{,**/}\" r,")
}

func (s *appArmorSuite) TestSetupSnapConfineSnippetsRemoteFS(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Make it appear as if remote file system workaround was needed.
	restore := osutil.MockIsHomeUsingRemoteFS(func() (bool, error) { return true, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()
	restore = apparmor.MockLoadHomedirs(func() ([]string, error) { return nil, nil })
	defer restore()

	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	c.Check(err, IsNil)
	c.Check(wasChanged, Equals, true)

	// Because remote file system is being used, we have the extra policy file.
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "nfs-support")
	fi, err := files[0].Info()
	c.Assert(err, IsNil)
	c.Assert(fi.Mode().Perm(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	// The policy allows network access.
	fn := filepath.Join(apparmor.SnapConfineAppArmorDir, files[0].Name())
	c.Assert(fn, testutil.FileContains, "network inet,")
	c.Assert(fn, testutil.FileContains, "network inet6,")
}

// Test behavior when isHomeUsingRemoteFS fails.
func (s *appArmorSuite) TestSetupSnapConfineGeneratedPolicyError1(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	log, restore := logger.MockLogger()
	defer restore()

	// Make it appear as if remote file system detection was broken.
	restore = osutil.MockIsHomeUsingRemoteFS(func() (bool, error) { return false, fmt.Errorf("broken") })
	defer restore()

	// Make it appear as if overlay was not used.
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// No homedirs
	restore = apparmor.MockLoadHomedirs(func() ([]string, error) { return nil, nil })
	defer restore()

	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	// NOTE: Errors in determining remote file system are non-fatal to prevent
	// snapd from failing to operate. A warning message is logged but system
	// operates as if remote file system was not active.
	c.Check(err, IsNil)
	c.Check(wasChanged, Equals, false)

	// While other stuff failed we created the policy directory and didn't
	// write any files to it.
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Check(err, IsNil)
	c.Check(files, HasLen, 0)

	// But an error was logged
	c.Check(log.String(), testutil.Contains, "cannot determine if remote file system is in use: broken")
}

// Test behavior when MkdirAll fails
func (s *appArmorSuite) TestSetupSnapConfineGeneratedPolicyError2(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Create a file where we would expect to find the local policy.
	err := os.RemoveAll(filepath.Dir(apparmor.SnapConfineAppArmorDir))
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Dir(apparmor.SnapConfineAppArmorDir), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(apparmor.SnapConfineAppArmorDir, []byte(""), 0644)
	c.Assert(err, IsNil)

	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	c.Check(err, ErrorMatches, fmt.Sprintf(`cannot create snap-confine policy directory: mkdir %s: not a directory`,
		apparmor.SnapConfineAppArmorDir))
	c.Check(wasChanged, Equals, false)
}

// Test behavior when EnsureDirState fails
func (s *appArmorSuite) TestSetupSnapConfineGeneratedPolicyError3(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Make it appear as if remote file system workaround was not needed.
	restore := osutil.MockIsHomeUsingRemoteFS(func() (bool, error) { return false, nil })
	defer restore()

	// Make it appear as if overlay was not used.
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// No homedirs
	restore = apparmor.MockLoadHomedirs(func() ([]string, error) { return nil, nil })
	defer restore()

	// Create the snap-confine directory and put a file. Because the file name
	// matches the glob generated-* snapd will attempt to remove it but because
	// the directory is not writable, that operation will fail.
	err := os.MkdirAll(apparmor.SnapConfineAppArmorDir, 0755)
	c.Assert(err, IsNil)
	f := filepath.Join(apparmor.SnapConfineAppArmorDir, "generated-test")
	err = os.WriteFile(f, []byte("spurious content"), 0644)
	c.Assert(err, IsNil)
	err = os.Chmod(apparmor.SnapConfineAppArmorDir, 0555)
	c.Assert(err, IsNil)

	// Make the directory writable for cleanup.
	defer os.Chmod(apparmor.SnapConfineAppArmorDir, 0755)

	wasChanged, err := apparmor.SetupSnapConfineSnippets()
	c.Check(err.Error(), testutil.Contains, "cannot synchronize snap-confine policy")
	c.Check(wasChanged, Equals, false)

	// The policy directory was unchanged.
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
}

func (s *appArmorSuite) TestRemoveSnapConfineSnippets(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Create the snap-confine directory and put a few files.
	err := os.MkdirAll(apparmor.SnapConfineAppArmorDir, 0755)
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(apparmor.SnapConfineAppArmorDir, "cap-test"), []byte("foo"), 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(apparmor.SnapConfineAppArmorDir, "my-file"), []byte("foo"), 0644), IsNil)

	err = apparmor.RemoveSnapConfineSnippets()
	c.Check(err, IsNil)

	// The files were removed
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)
}

func (s *appArmorSuite) TestRemoveSnapConfineSnippetsNoSnippets(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Create the snap-confine directory and let it do nothing.
	err := os.MkdirAll(apparmor.SnapConfineAppArmorDir, 0755)
	c.Assert(err, IsNil)

	err = apparmor.RemoveSnapConfineSnippets()
	c.Check(err, IsNil)

	// Nothing happens
	files, err := os.ReadDir(apparmor.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)
}
