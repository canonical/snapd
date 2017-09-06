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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/corecfg"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type piCfgSuite struct {
	mockConfigPath string

	mockSystemctl *testutil.MockCmd
}

var _ = Suite(&piCfgSuite{})

var mockConfigTxt = `
# For more options and information see
# http://www.raspberrypi.org/documentation/configuration/config-txt.md
#hdmi_group=1
# uncomment this if your display has a black border of unused pixels visible
# and your display can output without overscan
#disable_overscan=1
unrelated_options=are-kept`

func (s *piCfgSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)

	s.mockConfigPath = filepath.Join(dirs.GlobalRootDir, "/boot/uboot/config.txt")
	err := os.MkdirAll(filepath.Dir(s.mockConfigPath), 0755)
	c.Assert(err, IsNil)
	s.mockConfig(c, mockConfigTxt)

	s.mockSystemctl = testutil.MockCommand(c, "systemctl", "")
}

func (s *piCfgSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	s.mockSystemctl.Restore()
}

func (s *piCfgSuite) mockConfig(c *C, txt string) {
	err := ioutil.WriteFile(s.mockConfigPath, []byte(txt), 0644)
	c.Assert(err, IsNil)
}

func (s *piCfgSuite) checkMockConfig(c *C, expected string) {
	newContent, err := ioutil.ReadFile(s.mockConfigPath)
	c.Assert(err, IsNil)
	c.Check(string(newContent), Equals, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigUncommentExisting(c *C) {
	err := corecfg.UpdatePiConfig(s.mockConfigPath, map[string]string{"disable_overscan": "1"})
	c.Assert(err, IsNil)

	expected := strings.Replace(mockConfigTxt, "#disable_overscan=1", "disable_overscan=1", -1)
	s.checkMockConfig(c, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigCommentExisting(c *C) {
	s.mockConfig(c, mockConfigTxt+"\navoid_warnings=1\n")

	err := corecfg.UpdatePiConfig(s.mockConfigPath, map[string]string{"avoid_warnings": ""})
	c.Assert(err, IsNil)

	expected := mockConfigTxt + "\n" + "#avoid_warnings=1"
	s.checkMockConfig(c, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigAddNewOption(c *C) {
	err := corecfg.UpdatePiConfig(s.mockConfigPath, map[string]string{"framebuffer_depth": "16"})
	c.Assert(err, IsNil)

	expected := mockConfigTxt + "\n" + "framebuffer_depth=16"
	s.checkMockConfig(c, expected)

	// add again, verify its not added twice but updated
	err = corecfg.UpdatePiConfig(s.mockConfigPath, map[string]string{"framebuffer_depth": "32"})
	c.Assert(err, IsNil)
	expected = mockConfigTxt + "\n" + "framebuffer_depth=32"
	s.checkMockConfig(c, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigNoChangeUnset(c *C) {
	// ensure we cannot write to the dir to test that we really
	// do not update the file
	err := os.Chmod(filepath.Dir(s.mockConfigPath), 0500)
	c.Assert(err, IsNil)
	defer os.Chmod(filepath.Dir(s.mockConfigPath), 0755)

	err = corecfg.UpdatePiConfig(s.mockConfigPath, map[string]string{"hdmi_group": ""})
	c.Assert(err, IsNil)
}

func (s *piCfgSuite) TestConfigurePiConfigNoChangeSet(c *C) {
	// ensure we cannot write to the dir to test that we really
	// do not update the file
	err := os.Chmod(filepath.Dir(s.mockConfigPath), 0500)
	c.Assert(err, IsNil)
	defer os.Chmod(filepath.Dir(s.mockConfigPath), 0755)

	err = corecfg.UpdatePiConfig(s.mockConfigPath, map[string]string{"unrelated_options": "cannot-be-set"})
	c.Assert(err, ErrorMatches, `cannot set unsupported configuration value "unrelated_options"`)
}

func (s *piCfgSuite) TestConfigurePiConfigIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapctl := testutil.MockCommand(c, "snapctl", fmt.Sprintf(`
if [ "$1" = "get" ] && [ "$2" = "pi-config.disable-overscan" ]; then
    echo "1"
fi
`))
	defer mockSnapctl.Restore()

	err := corecfg.Run()
	c.Assert(err, IsNil)

	expected := strings.Replace(mockConfigTxt, "#disable_overscan=1", "disable_overscan=1", -1)
	s.checkMockConfig(c, expected)

	// run again with the inverse result and ensure we are back
	// as before
	mockSnapctl = testutil.MockCommand(c, "snapctl", fmt.Sprintf(`
if [ "$1" = "get" ] && [ "$2" = "pi-config.disable-overscan" ]; then
    echo ""
fi
`))
	defer mockSnapctl.Restore()

	err = corecfg.Run()
	c.Assert(err, IsNil)

	s.checkMockConfig(c, mockConfigTxt)

}
