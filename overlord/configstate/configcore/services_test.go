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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type servicesSuite struct {
	configcoreSuite
}

var _ = Suite(&servicesSuite{})

func (s *servicesSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)
	s.systemctlArgs = nil
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *servicesSuite) TestConfigureServiceInvalidValue(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"service.ssh.disable": "xxx",
		},
	})
	c.Check(err, ErrorMatches, `option "service.ssh.disable" has invalid value "xxx"`)
}

func (s *servicesSuite) TestConfigureServiceNotDisabled(c *C) {
	err := configcore.SwitchDisableService("sshd.service", false, nil)
	c.Assert(err, IsNil)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "unmask", "sshd.service"},
		{"--root", dirs.GlobalRootDir, "enable", "sshd.service"},
		{"start", "sshd.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabled(c *C) {
	err := configcore.SwitchDisableService("sshd.service", true, nil)
	c.Assert(err, IsNil)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "disable", "sshd.service"},
		{"--root", dirs.GlobalRootDir, "mask", "sshd.service"},
		{"stop", "sshd.service"},
		{"show", "--property=ActiveState", "sshd.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabledIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/ssh"), 0755)
	c.Assert(err, IsNil)

	for _, service := range []struct {
		cfgName     string
		systemdName string
	}{
		{"ssh", "ssh.service"},
		{"rsyslog", "rsyslog.service"},
		{"console-conf", "getty@*"},
	} {
		s.systemctlArgs = nil

		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				fmt.Sprintf("service.%s.disable", service.cfgName): true,
			},
		})
		c.Assert(err, IsNil)
		srv := service.systemdName
		switch service.cfgName {
		case "ssh":
			sshCanary := filepath.Join(dirs.GlobalRootDir, "/etc/ssh/sshd_not_to_be_run")
			_, err := os.Stat(sshCanary)
			c.Assert(err, IsNil)
			c.Check(s.systemctlArgs, DeepEquals, [][]string{
				{"stop", srv},
				{"show", "--property=ActiveState", srv},
			})
		case "console-conf":
			consoleConfCanary := filepath.Join(dirs.GlobalRootDir, "/var/lib/console-conf/complete")
			_, err := os.Stat(consoleConfCanary)
			c.Assert(err, IsNil)
			c.Check(s.systemctlArgs, DeepEquals, [][]string{
				{"restart", srv, "--all"},
				{"restart", "serial-getty@*", "--all"},
				{"restart", "serial-console-conf@*", "--all"},
				{"restart", "console-conf@*", "--all"},
			})
		default:
			c.Check(s.systemctlArgs, DeepEquals, [][]string{
				{"--root", dirs.GlobalRootDir, "disable", srv},
				{"--root", dirs.GlobalRootDir, "mask", srv},
				{"stop", srv},
				{"show", "--property=ActiveState", srv},
			})
		}
	}
}

func (s *servicesSuite) TestConfigureConsoleConfEnableIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// pretend that console-conf is disabled
	canary := filepath.Join(dirs.GlobalRootDir, "/var/lib/console-conf/complete")
	err := os.MkdirAll(filepath.Dir(canary), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(canary, nil, 0644)
	c.Assert(err, IsNil)

	// now enable it
	err = configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.console-conf.disable": false,
		},
	})
	c.Assert(err, IsNil)
	// ensure it got fully enabled
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"restart", "getty@*", "--all"},
		{"restart", "serial-getty@*", "--all"},
		{"restart", "serial-console-conf@*", "--all"},
		{"restart", "console-conf@*", "--all"},
	})
	// and that the canary file is no longer there
	c.Assert(canary, testutil.FileAbsent)
}

func (s *servicesSuite) TestConfigureConsoleConfEnableAlreadyEnabled(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// Note that we have no
	//        /var/lib/console-conf/complete
	// file. So console-conf is already enabled
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.console-conf.disable": false,
		},
	})
	c.Assert(err, IsNil)
	// because it was not enabled before no need to restart anything
	c.Check(s.systemctlArgs, HasLen, 0)
}

func (s *servicesSuite) TestConfigureServiceEnableIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/ssh"), 0755)
	c.Assert(err, IsNil)

	for _, service := range []struct {
		cfgName     string
		systemdName string
	}{
		{"ssh", "ssh.service"},
		{"rsyslog", "rsyslog.service"},
	} {
		s.systemctlArgs = nil
		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				fmt.Sprintf("service.%s.disable", service.cfgName): false,
			},
		})

		c.Assert(err, IsNil)
		srv := service.systemdName
		switch service.cfgName {
		case "ssh":
			c.Check(s.systemctlArgs, DeepEquals, [][]string{
				{"--root", dirs.GlobalRootDir, "unmask", "sshd.service"},
				{"--root", dirs.GlobalRootDir, "unmask", "ssh.service"},
				{"start", srv},
			})
			sshCanary := filepath.Join(dirs.GlobalRootDir, "/etc/ssh/sshd_not_to_be_run")
			_, err := os.Stat(sshCanary)
			c.Assert(err, ErrorMatches, ".* no such file or directory")
		default:
			c.Check(s.systemctlArgs, DeepEquals, [][]string{
				{"--root", dirs.GlobalRootDir, "unmask", srv},
				{"--root", dirs.GlobalRootDir, "enable", srv},
				{"start", srv},
			})
		}
	}
}

func (s *servicesSuite) TestConfigureServiceUnsupportedService(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.snapd.disable": true,
		},
	})
	c.Assert(err, IsNil)

	// ensure nothing gets enabled/disabled when an unsupported
	// service is set for disable
	c.Check(s.systemctlArgs, IsNil)
}

func (s *servicesSuite) TestFilesystemOnlyApply(c *C) {
	tmpDir := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "etc", "ssh"), 0755), IsNil)

	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"service.ssh.disable":     "true",
		"service.rsyslog.disable": "true",
	})
	c.Assert(configcore.FilesystemOnlyApply(tmpDir, conf, nil), IsNil)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", tmpDir, "mask", "rsyslog.service"},
	})
}
