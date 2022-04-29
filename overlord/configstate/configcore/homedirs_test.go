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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/sandbox/apparmor"
)

type mockedFileInfo struct {
	isDir bool
}

func (f *mockedFileInfo) Name() string {
	return ""
}

func (f *mockedFileInfo) Size() int64 {
	return 0
}

func (f *mockedFileInfo) Mode() os.FileMode {
	return 0
}

func (f *mockedFileInfo) ModTime() time.Time {
	return time.Unix(0, 0)
}

func (f *mockedFileInfo) IsDir() bool {
	return f.isDir
}

func (f *mockedFileInfo) Sys() interface{} {
	return nil
}

type homedirsSuite struct {
	configcoreSuite
}

var _ = Suite(&homedirsSuite{})

func (s *homedirsSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	stateDir := dirs.SnapdStateDir(dirs.GlobalRootDir)
	err := os.MkdirAll(stateDir, 0755)
	c.Assert(err, IsNil)
	s.AddCleanup(func() {
		err := os.RemoveAll(stateDir)
		c.Assert(err, IsNil)
	})

	etcDir := filepath.Join(dirs.GlobalRootDir, "/etc/")
	err = os.MkdirAll(etcDir, 0755)
	c.Assert(err, IsNil)
	s.AddCleanup(func() {
		err := os.RemoveAll(etcDir)
		c.Assert(err, IsNil)
	})

	restore := configcore.MockStat(func(path string) (os.FileInfo, error) {
		if strings.HasPrefix(path, "/home/existingDir") {
			return &mockedFileInfo{isDir: true}, nil
		} else if strings.HasPrefix(path, "/home/existingFile") {
			return &mockedFileInfo{isDir: false}, nil
		} else {
			return nil, errors.New("stat failed")
		}
	})
	s.AddCleanup(restore)

}

func (s *homedirsSuite) TestValidationUnhappy(c *C) {
	for _, testData := range []struct {
		homedirs      string
		expectedError string
	}{
		{"./here", `path "\./here" is not absolute`},
		// empty path in list
		{",/home", `path "" is not absolute`},
		{"/somewhere/else", `path "/somewhere/else/" unsupported: must start with one of: /home/`},
		{"/home/foo[12]", `home path invalid: "/home/foo\[12\]" contains a reserved apparmor char.*`},
		{"/lib/homes", `path "/lib/homes/" uses reserved root directory "/lib/"`},
		{"/home/missing", `cannot get directory info for "/home/missing/".*`},
		{"/home/existingFile", `path "/home/existingFile/" is not a directory`},
	} {
		err := configcore.Run(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"homedirs": testData.homedirs,
			},
		})
		c.Assert(err, ErrorMatches, testData.expectedError, Commentf("%v", testData.homedirs))
	}
}

func (s *homedirsSuite) TestConfigureOpenFailure(c *C) {
	var systemInfoPath string
	restore := configcore.MockOpenFile(func(path string, flags int, mode os.FileMode) (*os.File, error) {
		systemInfoPath = path
		return nil, errors.New("open failure")
	})
	defer restore()
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	})
	expectedPath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "system.info")
	c.Check(systemInfoPath, Equals, expectedPath)
	c.Assert(err, ErrorMatches, "open failure")
}

func (s *homedirsSuite) TestConfigureWriteFailure(c *C) {
	restore := configcore.MockOpenFile(func(path string, flags int, mode os.FileMode) (*os.File, error) {
		file, err := os.OpenFile(path, flags, mode)
		// But now close the file so that the write will fail
		file.Close()
		return file, err
	})
	defer restore()
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	})
	c.Assert(err, ErrorMatches, "write .*system.info: file already closed")
}

func (s *homedirsSuite) TestConfigureApparmorTunableFailure(c *C) {
	var homedirs []string
	restore := configcore.MockApparmorUpdateHomedirsTunable(func(paths []string) error {
		homedirs = paths
		return errors.New("tunable error")
	})
	defer restore()
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "/home/existingDir/one,/home/existingDir/two",
		},
	})
	c.Check(homedirs, DeepEquals, []string{"/home/existingDir/one", "/home/existingDir/two"})
	c.Assert(err, ErrorMatches, "tunable error")
}

func (s *homedirsSuite) TestConfigureApparmorReloadFailure(c *C) {
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
	restore := configcore.MockApparmorLoadProfiles(func(paths []string, cacheDir string, flags apparmor.AaParserFlags) error {
		passedProfiles = paths
		return errors.New("reload error")
	})
	defer restore()
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	})
	c.Check(passedProfiles, DeepEquals, profiles)
	c.Assert(err, ErrorMatches, "reload error")
}

func (s *homedirsSuite) TestConfigureHomedirsHappy(c *C) {
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
	restore := configcore.MockApparmorSnapConfineDistroProfilePath(func() string {
		return snapConfineProfile
	})
	defer restore()
	profiles = append(profiles, snapConfineProfile)

	var passedProfiles []string
	var passedCacheDir string
	var passedFlags apparmor.AaParserFlags
	restore = configcore.MockApparmorLoadProfiles(func(paths []string, cacheDir string, flags apparmor.AaParserFlags) error {
		passedProfiles = paths
		passedCacheDir = cacheDir
		passedFlags = flags
		return nil
	})
	defer restore()

	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	})
	c.Check(passedProfiles, DeepEquals, profiles)
	c.Check(passedCacheDir, Equals, filepath.Join(dirs.GlobalRootDir, "/var/cache/apparmor"))
	c.Check(passedFlags, Equals, apparmor.SkipReadCache)
	c.Assert(err, IsNil)
}

func (s *homedirsSuite) TestConfigureHomedirsEmptyHappy(c *C) {
	var passedHomeDirs []string
	restore := configcore.MockApparmorUpdateHomedirsTunable(func(paths []string) error {
		passedHomeDirs = paths
		return nil
	})
	defer restore()
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "",
		},
	})
	c.Check(passedHomeDirs, HasLen, 0)
	c.Assert(err, IsNil)
}
