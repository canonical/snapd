// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type piCfgSuite struct {
	configcoreSuite

	mockConfigPath string
}

var _ = Suite(&piCfgSuite{})

var mockConfigTxt = `
# For more options and information see
# http://www.raspberrypi.org/documentation/configuration/config-txt.md
#hdmi_group=1
# uncomment this if your display has a black border of unused pixels visible
# and your display can output without overscan
#disable_overscan=1
unrelated_options=are-kept
#gpu_mem_512=true
`

func (s *piCfgSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)

	s.mockConfigPath = filepath.Join(dirs.GlobalRootDir, "/boot/uboot/config.txt")
	err := os.MkdirAll(filepath.Dir(s.mockConfigPath), 0755)
	c.Assert(err, IsNil)
	s.mockConfig(c, mockConfigTxt)
}

func (s *piCfgSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *piCfgSuite) mockConfig(c *C, txt string) {
	err := ioutil.WriteFile(s.mockConfigPath, []byte(txt), 0644)
	c.Assert(err, IsNil)
}

func (s *piCfgSuite) checkMockConfig(c *C, expected string) {
	c.Check(s.mockConfigPath, testutil.FileEquals, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigUncommentExisting(c *C) {
	err := configcore.UpdatePiConfig(s.mockConfigPath, map[string]string{"disable_overscan": "1"})
	c.Assert(err, IsNil)

	expected := strings.Replace(mockConfigTxt, "#disable_overscan=1", "disable_overscan=1", -1)
	s.checkMockConfig(c, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigCommentExisting(c *C) {
	s.mockConfig(c, mockConfigTxt+"avoid_warnings=1\n")

	err := configcore.UpdatePiConfig(s.mockConfigPath, map[string]string{"avoid_warnings": ""})
	c.Assert(err, IsNil)

	expected := mockConfigTxt + "#avoid_warnings=1\n"
	s.checkMockConfig(c, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigAddNewOption(c *C) {
	err := configcore.UpdatePiConfig(s.mockConfigPath, map[string]string{"framebuffer_depth": "16"})
	c.Assert(err, IsNil)

	expected := mockConfigTxt + "framebuffer_depth=16\n"
	s.checkMockConfig(c, expected)

	// add again, verify its not added twice but updated
	err = configcore.UpdatePiConfig(s.mockConfigPath, map[string]string{"framebuffer_depth": "32"})
	c.Assert(err, IsNil)
	expected = mockConfigTxt + "framebuffer_depth=32\n"
	s.checkMockConfig(c, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigNoChangeUnset(c *C) {
	// ensure we cannot write to the dir to test that we really
	// do not update the file
	err := os.Chmod(filepath.Dir(s.mockConfigPath), 0500)
	c.Assert(err, IsNil)
	defer os.Chmod(filepath.Dir(s.mockConfigPath), 0755)

	err = configcore.UpdatePiConfig(s.mockConfigPath, map[string]string{"hdmi_group": ""})
	c.Assert(err, IsNil)
}

func (s *piCfgSuite) TestConfigurePiConfigNoChangeSet(c *C) {
	// ensure we cannot write to the dir to test that we really
	// do not update the file
	err := os.Chmod(filepath.Dir(s.mockConfigPath), 0500)
	c.Assert(err, IsNil)
	defer os.Chmod(filepath.Dir(s.mockConfigPath), 0755)

	err = configcore.UpdatePiConfig(s.mockConfigPath, map[string]string{"unrelated_options": "cannot-be-set"})
	c.Assert(err, ErrorMatches, `cannot set unsupported configuration value "unrelated_options"`)
}

func (s *piCfgSuite) TestConfigurePiConfigIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.disable-overscan": 1,
		},
	})
	c.Assert(err, IsNil)

	expected := strings.Replace(mockConfigTxt, "#disable_overscan=1", "disable_overscan=1", -1)
	s.checkMockConfig(c, expected)

	err = configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.disable-overscan": "",
		},
	})
	c.Assert(err, IsNil)

	s.checkMockConfig(c, mockConfigTxt)
}

func (s *piCfgSuite) TestConfigurePiConfigRegression(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.gpu-mem-512": true,
		},
	})
	c.Assert(err, IsNil)
	expected := strings.Replace(mockConfigTxt, "#gpu_mem_512=true", "gpu_mem_512=true", -1)
	s.checkMockConfig(c, expected)
}
