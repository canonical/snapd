// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type sysctlSuite struct {
	configcoreSuite

	mockSysctlConfPath string
}

var _ = Suite(&sysctlSuite{})

func (s *sysctlSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.mockSysctlConfPath = filepath.Join(dirs.GlobalRootDir, "/etc/sysctl.d/99-snapd.conf")
	c.Assert(os.MkdirAll(filepath.Dir(s.mockSysctlConfPath), 0755), IsNil)
}

func (s *sysctlSuite) TearDownTest(c *C) {
	s.configcoreSuite.TearDownTest(c)
	dirs.SetRootDir("/")
}

func (s *sysctlSuite) TestConfigureSysctlIntegration(c *C) {
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "2",
		},
	}))

	c.Check(s.mockSysctlConfPath, testutil.FileEquals, "kernel.printk = 2 4 1 7\n")
	c.Check(s.systemdSysctlArgs, DeepEquals, [][]string{
		{"--prefix", "kernel.printk"},
	})
	s.systemdSysctlArgs = nil
	mylog.

		// Unset console-loglevel and restore default vaule
		Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.kernel.printk.console-loglevel": "",
			},
		}))

	c.Check(osutil.FileExists(s.mockSysctlConfPath), Equals, false)
	c.Check(s.systemdSysctlArgs, DeepEquals, [][]string{
		{"--prefix", "kernel.printk"},
	})
}

func (s *sysctlSuite) TestConfigureLoglevelUnderRange(c *C) {
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "-1",
		},
	}))
	c.Check(osutil.FileExists(s.mockSysctlConfPath), Equals, false)
	c.Assert(err, ErrorMatches, `console-loglevel must be a number between 0 and 7, not: -1`)
}

func (s *sysctlSuite) TestConfigureLoglevelOverRange(c *C) {
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "8",
		},
	}))
	c.Check(osutil.FileExists(s.mockSysctlConfPath), Equals, false)
	c.Assert(err, ErrorMatches, `console-loglevel must be a number between 0 and 7, not: 8`)
}

func (s *sysctlSuite) TestConfigureLevelRejected(c *C) {
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "invalid",
		},
	}))
	c.Check(osutil.FileExists(s.mockSysctlConfPath), Equals, false)
	c.Assert(err, ErrorMatches, `console-loglevel must be a number between 0 and 7, not: invalid`)
}

func (s *sysctlSuite) TestConfigureSysctlIntegrationNoSetting(c *C) {
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf:  map[string]interface{}{},
	}))

	c.Check(osutil.FileExists(s.mockSysctlConfPath), Equals, false)
}

func (s *sysctlSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"system.kernel.printk.console-loglevel": "4",
	})

	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	networkSysctlPath := filepath.Join(tmpDir, "/etc/sysctl.d/99-snapd.conf")
	c.Check(networkSysctlPath, testutil.FileEquals, "kernel.printk = 4 4 1 7\n")

	// systemd-sysctl was not executed
	c.Check(s.systemdSysctlArgs, HasLen, 0)
}
