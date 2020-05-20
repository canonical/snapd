// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type logControlSuite struct {
	configcoreSuite

	mockSysctlPath string
	restores       []func()
	mockSysctl     *testutil.MockCmd
}

var _ = Suite(&logControlSuite{})

func (s *logControlSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.restores = append(s.restores, release.MockOnClassic(false))

	s.mockSysctl = testutil.MockCommand(c, "sysctl", "")
	s.restores = append(s.restores, func() { s.mockSysctl.Restore() })

	s.mockSysctlPath = filepath.Join(dirs.GlobalRootDir, "/etc/sysctl.d/10-console-messages.conf")
	c.Assert(os.MkdirAll(filepath.Dir(s.mockSysctlPath), 0755), IsNil)
}

func (s *logControlSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	for _, f := range s.restores {
		f()
	}
}

func (s *logControlSuite) TestConfigureLogControlIntegration(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.printk.console-loglevel": "2",
		},
	})
	c.Assert(err, IsNil)

	c.Check(s.mockSysctlPath, testutil.FileEquals, "kernel.printk = 2 4 1 7\n")
	c.Check(s.mockSysctl.Calls(), DeepEquals, [][]string{
		{"sysctl", "-w", "kernel.printk=2"},
	})
}

func (s *logControlSuite) TestConfigureLoglevelUnderRange(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.printk.console-loglevel": "-1",
		},
	})
	c.Check(osutil.FileExists(s.mockSysctlPath), Equals, false)
	c.Assert(err, ErrorMatches, `loglevel must be a number between 0 and 7, not "-1"`)
}

func (s *logControlSuite) TestConfigureLoglevelOverRange(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.printk.console-loglevel": "8",
		},
	})
	c.Check(osutil.FileExists(s.mockSysctlPath), Equals, false)
	c.Assert(err, ErrorMatches, `loglevel must be a number between 0 and 7, not "8"`)
}

func (s *logControlSuite) TestConfigureLevelRejected(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.printk.console-loglevel": "invalid",
		},
	})
	c.Check(osutil.FileExists(s.mockSysctlPath), Equals, false)
	c.Assert(err, ErrorMatches, `loglevel must be a number between 0 and 7, not "invalid"`)
}

func (s *logControlSuite) TestConfigureLogControlIntegrationNoSetting(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf:  map[string]interface{}{},
	})
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(s.mockSysctlPath), Equals, false)
}

func (s *logControlSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"system.printk.console-loglevel": "4",
	})

	tmpDir := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "/etc/sysctl.d/"), 0755), IsNil)
	c.Assert(configcore.FilesystemOnlyApply(tmpDir, conf, nil), IsNil)

	networkSysctlPath := filepath.Join(tmpDir, "/etc/sysctl.d/10-console-messages.conf")
	c.Check(networkSysctlPath, testutil.FileEquals, "kernel.printk = 4 4 1 7\n")

	// sysctl was not executed
	c.Check(s.mockSysctl.Calls(), HasLen, 0)
}
