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

package configcore_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/testutil"
)

type homedirsSuite struct {
	configcoreSuite
}

var _ = Suite(&homedirsSuite{})

func (s *homedirsSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	etcDir := filepath.Join(dirs.GlobalRootDir, "/etc/")
	mylog.Check(os.MkdirAll(etcDir, 0755))

	s.AddCleanup(func() {
		mylog.Check(os.RemoveAll(etcDir))

	})

	// Tests might create this file. Since its presence is checked by the
	// implementation code, we remove it after each test, to make sure that
	// tests don't influence each other.
	configPath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "system-params")
	s.AddCleanup(func() {
		mylog.Check(os.Remove(configPath))
	})

	restore := configcore.MockDirExists(func(path string) (exists bool, isDir bool, err error) {
		switch {
		case strings.HasPrefix(path, "/home/existingDir"):
			return true, true, nil
		case strings.HasPrefix(path, "/home/existingFile"):
			return true, false, nil
		case strings.HasPrefix(path, "/home/missing"):
			return false, false, nil
		default:
			return false, false, errors.New("stat failed")
		}
	})
	s.AddCleanup(restore)

	// Mock full apparmor support by default for the tests here
	s.AddCleanup(apparmor.MockLevel(apparmor.Full))
}

func (s *homedirsSuite) TestValidationUnhappy(c *C) {
	for _, testData := range []struct {
		homedirs      string
		expectedError string
	}{
		{"./here", `path "\./here" is not absolute`},
		// empty path in list
		{",/home", `path "" is not absolute`},
		{"/home/foo[12]", `home path invalid: "/home/foo\[12\]" contains a reserved apparmor char.*`},
		{"/lib/homes", `path "/lib/homes/" uses reserved root directory "/lib/"`},
		{"/home/error", `cannot get directory info for "/home/error/".*`},
		{"/home/missing", `path "/home/missing/" does not exist`},
		{"/home/existingFile", `path "/home/existingFile/" is not a directory`},
		// combine a valid path with an invalid one
		{"/home/existingDir,/boot/invalid", `path "/boot/invalid/" uses reserved root directory "/boot/"`},
	} {
		mylog.Check(configcore.FilesystemOnlyRun(classicDev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"homedirs": testData.homedirs,
			},
		}))
		c.Assert(err, ErrorMatches, testData.expectedError, Commentf("%v", testData.homedirs))
	}
}

func (s *homedirsSuite) TestConfigureUnchangedConfig(c *C) {
	tunableUpdated := false
	restore := configcore.MockApparmorUpdateHomedirsTunable(func(paths []string) error {
		tunableUpdated = true
		return nil
	})
	defer restore()
	mylog.Check(configcore.FilesystemOnlyRun(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
		changes: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	}))

	c.Check(tunableUpdated, Equals, false)
}

func (s *homedirsSuite) TestConfigureApparmorTunableFailure(c *C) {
	var homedirs []string
	restore := configcore.MockApparmorUpdateHomedirsTunable(func(paths []string) error {
		homedirs = paths
		return errors.New("tunable error")
	})
	defer restore()
	mylog.Check(configcore.FilesystemOnlyRun(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"homedirs": "/home/existingDir/one,/home/existingDir/two",
		},
	}))
	c.Check(err, ErrorMatches, "tunable error")
	c.Check(homedirs, DeepEquals, []string{
		filepath.Join(dirs.GlobalRootDir, "/home/existingDir/one"),
		filepath.Join(dirs.GlobalRootDir, "/home/existingDir/two"), filepath.Join(dirs.GlobalRootDir, "/home"),
	})
}

func (s *homedirsSuite) TestConfigureApparmorReloadFailure(c *C) {
	restore := configcore.MockApparmorReloadAllSnapProfiles(func() error {
		return errors.New("reload error")
	})
	defer restore()
	mylog.Check(configcore.FilesystemOnlyRun(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	}))
	c.Assert(err, ErrorMatches, "reload error")
}

func (s *homedirsSuite) TestConfigureApparmorUnsupported(c *C) {
	// Currently the homedir option will act more or less as a no-op on
	// systems that do not have apparmor support. So let's test that
	// both unsupported and unusable will return no error, as it should be
	// a no-op.

	// let's mock this to ensure we can track whether this was called, as we don't
	// want this called when unsupported.
	var reloadProfilesCalled bool
	restore := configcore.MockApparmorReloadAllSnapProfiles(func() error {
		reloadProfilesCalled = true
		return nil
	})
	defer restore()

	// always update
	restore = configcore.MockEnsureFileState(func(string, osutil.FileState) error {
		return nil
	})
	defer restore()

	for _, testData := range []struct {
		level          apparmor.LevelType
		updateProfiles bool
	}{
		{apparmor.Unknown, false},
		{apparmor.Unsupported, false},
		{apparmor.Unusable, false},
		{apparmor.Partial, true},
		{apparmor.Full, true},
	} {
		// initialize test by mocking the aa level and reseting the boolean
		resetAA := apparmor.MockLevel(testData.level)
		reloadProfilesCalled = false
		mylog.Check(configcore.FilesystemOnlyRun(classicDev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"homedirs": "/home/existingDir",
			},
		}))
		c.Check(err, IsNil)
		c.Check(reloadProfilesCalled, Equals, testData.updateProfiles, Commentf("%v", testData.level.String()))
		resetAA()
	}
}

func (s *homedirsSuite) TestConfigureHomedirsHappy(c *C) {
	reloadProfilesCallCount := 0
	restore := configcore.MockApparmorReloadAllSnapProfiles(func() error {
		reloadProfilesCallCount++
		return nil
	})
	defer restore()

	var tunableHomedirs []string
	restore = configcore.MockApparmorUpdateHomedirsTunable(func(paths []string) error {
		tunableHomedirs = paths
		return nil
	})
	defer restore()

	var setupSnapConfineSnippetsCalls int
	restore = configcore.MockApparmorSetupSnapConfineSnippets(func() (bool, error) {
		setupSnapConfineSnippetsCalls++
		return false, nil
	})
	defer restore()
	mylog.Check(configcore.FilesystemOnlyRun(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	}))
	c.Check(err, IsNil)

	// Check that the config file has been written
	configPath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "system-params")
	contents := mylog.Check2(os.ReadFile(configPath))

	c.Check(string(contents), Equals, "homedirs=/home/existingDir\n")

	// Check that the AppArmor tunables have been written...
	c.Check(tunableHomedirs, DeepEquals, []string{
		filepath.Join(dirs.GlobalRootDir, "/home/existingDir"),
		filepath.Join(dirs.GlobalRootDir, "/home"),
	})
	// ...and that profiles have been reloaded
	c.Check(reloadProfilesCallCount, Equals, 1)
	// and finally that snap-confine snippets was called
	c.Check(setupSnapConfineSnippetsCalls, Equals, 1)
}

func (s *homedirsSuite) TestConfigureHomedirsEmptyHappy(c *C) {
	var passedHomeDirs []string
	restore := configcore.MockApparmorUpdateHomedirsTunable(func(paths []string) error {
		passedHomeDirs = paths
		return nil
	})
	defer restore()
	restore = configcore.MockApparmorSetupSnapConfineSnippets(func() (bool, error) {
		return false, nil
	})
	defer restore()
	mylog.Check(configcore.FilesystemOnlyRun(classicDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "",
		},
	}))
	c.Check(err, IsNil)
	c.Check(passedHomeDirs, HasLen, 0)
}

func (s *homedirsSuite) TestConfigureHomedirsNotOnCore(c *C) {
	reloadProfilesCallCount := 0
	restore := configcore.MockApparmorReloadAllSnapProfiles(func() error {
		reloadProfilesCallCount++
		return nil
	})
	defer restore()

	var tunableHomedirs []string
	restore = configcore.MockApparmorUpdateHomedirsTunable(func(paths []string) error {
		tunableHomedirs = paths
		return nil
	})
	defer restore()

	var setupSnapConfineSnippetsCalls int
	restore = configcore.MockApparmorSetupSnapConfineSnippets(func() (bool, error) {
		setupSnapConfineSnippetsCalls++
		return false, nil
	})
	defer restore()
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	}))
	c.Check(err, ErrorMatches, `configuration of homedir locations on Ubuntu Core is currently unsupported. Please report a bug if you need it`)

	// Verify config file doesn't exist
	c.Check(dirs.SnapSystemParamsUnder(dirs.GlobalRootDir), testutil.FileAbsent)

	// And that nothing happened.
	c.Check(tunableHomedirs, HasLen, 0)
	c.Check(reloadProfilesCallCount, Equals, 0)
	c.Check(setupSnapConfineSnippetsCalls, Equals, 0)
}

func (s *homedirsSuite) TestupdateHomedirsConfig(c *C) {
	config := "/home/homeDir1,/home/homeDirs/homeDir1///,/home/homeDir2/,/home/homeTest/users/"
	expectedHomeDirs := []string{
		filepath.Join(dirs.GlobalRootDir, "/home/homeDir1"), filepath.Join(dirs.GlobalRootDir, "/home/homeDirs/homeDir1"),
		filepath.Join(dirs.GlobalRootDir, "/home/homeDir2"), filepath.Join(dirs.GlobalRootDir, "/home/homeTest/users"), filepath.Join(dirs.GlobalRootDir, "/home"),
	}
	configcore.UpdateHomedirsConfig(config, nil)
	c.Check(dirs.SnapHomeDirs(), DeepEquals, expectedHomeDirs)
}
