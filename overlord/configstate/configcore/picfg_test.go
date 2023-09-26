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
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
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
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)

	s.mockConfigPath = filepath.Join(dirs.GlobalRootDir, "/boot/uboot/config.txt")
	err := os.MkdirAll(filepath.Dir(s.mockConfigPath), 0755)
	c.Assert(err, IsNil)
	s.mockConfig(c, mockConfigTxt)
}

func (s *piCfgSuite) mockConfig(c *C, txt string) {
	err := os.WriteFile(s.mockConfigPath, []byte(txt), 0644)
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
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.disable-overscan": 1,
		},
	})
	c.Assert(err, IsNil)

	expected := strings.Replace(mockConfigTxt, "#disable_overscan=1", "disable_overscan=1", -1)
	s.checkMockConfig(c, expected)

	err = configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.disable-overscan": "",
		},
	})
	c.Assert(err, IsNil)

	s.checkMockConfig(c, mockConfigTxt)
}

func (s *piCfgSuite) TestConfigurePiConfigRegression(c *C) {
	err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.gpu-mem-512": true,
		},
	})
	c.Assert(err, IsNil)
	expected := strings.Replace(mockConfigTxt, "#gpu_mem_512=true", "gpu_mem_512=true", -1)
	s.checkMockConfig(c, expected)
}

func (s *piCfgSuite) TestUpdateConfigUC20RunMode(c *C) {
	uc20DevRunMode := mockDev{
		mode: "run",
		uc20: true,
	}

	// write default config at both the uc18 style runtime location and uc20 run
	// mode location to show that we only modify the uc20 one
	piCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "config.txt")
	uc18PiCfg := filepath.Join(dirs.GlobalRootDir, "/boot/uboot/config.txt")

	err := os.MkdirAll(filepath.Dir(piCfg), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Dir(uc18PiCfg), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(piCfg, []byte(mockConfigTxt), 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(uc18PiCfg, []byte(mockConfigTxt), 0644)
	c.Assert(err, IsNil)

	// apply the config
	err = configcore.FilesystemOnlyRun(uc20DevRunMode, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.gpu-mem-512": true,
		},
	})
	c.Assert(err, IsNil)

	// make sure that the original pi config.txt in /boot/uboot/config.txt
	// didn't change
	c.Check(uc18PiCfg, testutil.FileEquals, mockConfigTxt)

	// but the real one did change*
	expected := strings.Replace(mockConfigTxt, "#gpu_mem_512=true", "gpu_mem_512=true", -1)
	c.Check(piCfg, testutil.FileEquals, expected)
}

func (s *piCfgSuite) testUpdateConfigUC20NonRunMode(c *C, mode string) {
	uc20DevMode := mockDev{
		mode: mode,
		uc20: true,
	}

	piCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "config.txt")

	err := os.MkdirAll(filepath.Dir(piCfg), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(piCfg, []byte(mockConfigTxt), 0644)
	c.Assert(err, IsNil)

	// apply the config
	err = configcore.FilesystemOnlyRun(uc20DevMode, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.gpu-mem-512": true,
		},
	})
	c.Assert(err, IsNil)

	// the config.txt didn't change at all
	c.Check(piCfg, testutil.FileEquals, mockConfigTxt)
}

func (s *piCfgSuite) TestUpdateConfigUC20RecoverModeDoesNothing(c *C) {
	s.testUpdateConfigUC20NonRunMode(c, "recover")
}

func (s *piCfgSuite) TestUpdateConfigUC20InstallModeDoesNothing(c *C) {
	s.testUpdateConfigUC20NonRunMode(c, "install")
}

func (s *piCfgSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"pi-config.gpu-mem-512": true,
	})

	tmpDir := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "/boot/uboot"), 0755), IsNil)

	// write default config
	piCfg := filepath.Join(tmpDir, "/boot/uboot/config.txt")
	c.Assert(os.WriteFile(piCfg, []byte(mockConfigTxt), 0644), IsNil)

	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	expected := strings.Replace(mockConfigTxt, "#gpu_mem_512=true", "gpu_mem_512=true", -1)
	c.Check(piCfg, testutil.FileEquals, expected)
}

func (s *piCfgSuite) TestConfigurePiConfigSkippedOnAvnetKernel(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	avnetDev := mockDev{classic: false, kernel: "avnet-avt-iiotg20-kernel"}

	err := configcore.FilesystemOnlyRun(avnetDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.disable-overscan": 1,
		},
	})
	c.Assert(err, IsNil)

	c.Check(logbuf.String(), testutil.Contains, "ignoring pi-config settings: configuration cannot be applied: boot measures config.txt")
	// change was ignored
	s.checkMockConfig(c, mockConfigTxt)
}

func (s *piCfgSuite) TestConfigurePiConfigSkippedOnWrongMode(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	uc20DevInstallMode := mockDev{
		classic: false,
		mode:    "install",
		uc20:    true,
	}

	err := configcore.FilesystemOnlyRun(uc20DevInstallMode, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"pi-config.disable-overscan": 1,
		},
	})
	c.Assert(err, IsNil)

	c.Check(logbuf.String(), testutil.Contains, "ignoring pi-config settings: configuration cannot be applied: unsupported system mode")
	// change was ignored
	s.checkMockConfig(c, mockConfigTxt)
}

func (s *piCfgSuite) TestConfigurePiConfigSkippedOnIgnoreHeader(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	tests := []struct {
		header       string
		shouldIgnore bool
	}{
		// ignored
		{"# Snapd-Edit: no", true},
		{"#    Snapd-Edit:     no   ", true},
		{"# snapd-edit: No", true},
		{"# SNAPD-EDIT: NO", true},
		// not ignored
		{"# Snapd-Edit: noAND THEN random words", false},
		{"not first line \n# SNAPD-EDIT: NO", false},
		{"# random things and then SNAPD-EDIT: NO", false},
	}

	for _, tc := range tests {
		mockConfigWithHeader := tc.header + mockConfigTxt
		s.mockConfig(c, mockConfigWithHeader)
		err := configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"pi-config.disable-overscan": 1,
			},
		})
		c.Assert(err, IsNil)

		if tc.shouldIgnore {
			c.Check(logbuf.String(), testutil.Contains, "ignoring pi-config settings: configuration cannot be applied: no-editing header found")
			// change was ignored
			s.checkMockConfig(c, mockConfigWithHeader)
		} else {
			c.Check(logbuf.String(), HasLen, 0)
			expected := strings.Replace(mockConfigWithHeader, "#disable_overscan=1", "disable_overscan=1", -1)
			s.checkMockConfig(c, expected)
		}

		logbuf.Reset()
	}
}
