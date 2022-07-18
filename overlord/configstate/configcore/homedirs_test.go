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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type homedirsSuite struct {
	configcoreSuite
}

var _ = Suite(&homedirsSuite{})

func (s *homedirsSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	etcDir := filepath.Join(dirs.GlobalRootDir, "/etc/")
	err := os.MkdirAll(etcDir, 0755)
	c.Assert(err, IsNil)
	s.AddCleanup(func() {
		err := os.RemoveAll(etcDir)
		c.Assert(err, IsNil)
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
		{"/home/error", `cannot get directory info for "/home/error/".*`},
		{"/home/missing", `path "/home/missing/" does not exist`},
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

func (s *homedirsSuite) TestConfigureWriteFailure(c *C) {
	restore := configcore.MockWriteFile(func(path string, contents []byte, mode os.FileMode) error {
		return errors.New("some write error")
	})
	defer restore()
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	})
	c.Assert(err, ErrorMatches, "some write error")
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
	c.Check(err, ErrorMatches, "tunable error")
	c.Check(homedirs, DeepEquals, []string{"/home/existingDir/one", "/home/existingDir/two"})
}

func (s *homedirsSuite) TestConfigureApparmorReloadFailure(c *C) {
	restore := configcore.MockApparmorReloadAllSnapProfiles(func() error {
		return errors.New("reload error")
	})
	defer restore()
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	})
	c.Assert(err, ErrorMatches, "reload error")
}

func (s *homedirsSuite) TestConfigureHomedirsHappy(c *C) {
	reloadProfilesCallCount := 0
	restore := configcore.MockApparmorReloadAllSnapProfiles(func() error {
		reloadProfilesCallCount++
		return nil
	})
	defer restore()

	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"homedirs": "/home/existingDir",
		},
	})
	c.Check(err, IsNil)
	c.Check(reloadProfilesCallCount, Equals, 1)
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
	c.Check(err, IsNil)
	c.Check(passedHomeDirs, HasLen, 0)
}
