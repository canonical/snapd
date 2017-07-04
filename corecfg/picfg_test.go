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

package corecfg_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/corecfg"
)

type piCfgSuite struct{}

var _ = Suite(&piCfgSuite{})

var mockConfigTxt = `
# For more options and information see
# http://www.raspberrypi.org/documentation/configuration/config-txt.md
# Some settings may impact device functionality. See link above for details
# uncomment if you get no picture on HDMI for a default "safe" mode
#hdmi_safe=1
# uncomment this if your display has a black border of unused pixels visible
# and your display can output without overscan
#disable_overscan=1
unrelated_options=are-kept`

func mockConfig(c *C, txt string) string {
	mockConfig := filepath.Join(c.MkDir(), "config.txt")
	err := ioutil.WriteFile(mockConfig, []byte(txt), 0644)
	c.Assert(err, IsNil)
	return mockConfig
}

func checkMockConfig(c *C, mockConfig, expected string) {
	newContent, err := ioutil.ReadFile(mockConfig)
	c.Assert(err, IsNil)
	c.Check(string(newContent), Equals, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigUncommentExisting(c *C) {
	mockConfig := mockConfig(c, mockConfigTxt)

	err := corecfg.UpdatePiConfig(mockConfig, map[string]string{"disable_overscan": "1"})
	c.Assert(err, IsNil)

	expected := strings.Replace(mockConfigTxt, "#disable_overscan=1", "disable_overscan=1", -1)
	checkMockConfig(c, mockConfig, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigCommentExisting(c *C) {
	mockConfig := mockConfig(c, mockConfigTxt+"\navoid_warnings=1\n")

	err := corecfg.UpdatePiConfig(mockConfig, map[string]string{"avoid_warnings": ""})
	c.Assert(err, IsNil)

	expected := mockConfigTxt + "\n" + "#avoid_warnings=1"
	checkMockConfig(c, mockConfig, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigAddNewOption(c *C) {
	mockConfig := mockConfig(c, mockConfigTxt)

	err := corecfg.UpdatePiConfig(mockConfig, map[string]string{"framebuffer_depth": "16"})
	c.Assert(err, IsNil)

	expected := mockConfigTxt + "\n" + "framebuffer_depth=16"
	checkMockConfig(c, mockConfig, expected)

	// add again, verify its not added twice but updated
	err = corecfg.UpdatePiConfig(mockConfig, map[string]string{"framebuffer_depth": "32"})
	expected = mockConfigTxt + "\n" + "framebuffer_depth=32"
	checkMockConfig(c, mockConfig, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigNoChangeUnset(c *C) {
	mockConfig := mockConfig(c, mockConfigTxt)

	st1, err := os.Stat(mockConfig)
	c.Assert(err, IsNil)

	err = corecfg.UpdatePiConfig(mockConfig, map[string]string{"hdmi_safe": ""})
	c.Assert(err, IsNil)

	st2, err := os.Stat(mockConfig)
	c.Assert(err, IsNil)

	c.Check(st1.ModTime(), DeepEquals, st2.ModTime())
}

func (s *piCfgSuite) TestConfigurePiConfigNoChangeSet(c *C) {
	mockConfig := mockConfig(c, mockConfigTxt)

	st1, err := os.Stat(mockConfig)
	c.Assert(err, IsNil)

	err = corecfg.UpdatePiConfig(mockConfig, map[string]string{"unrelated_options": "are-kept"})
	c.Assert(err, IsNil)

	st2, err := os.Stat(mockConfig)
	c.Assert(err, IsNil)

	c.Check(st1.ModTime(), DeepEquals, st2.ModTime())
}
