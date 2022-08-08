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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type servicesSuite struct {
	configcoreSuite
	serviceInstalled bool
}

var _ = Suite(&servicesSuite{})

func (s *servicesSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	s.systemctlOutput = func(args ...string) []byte {
		var output []byte
		if args[0] == "show" {
			if args[1] == "--property=ActiveState" {
				output = []byte("ActiveState=inactive")
			} else {
				if s.serviceInstalled {
					output = []byte(fmt.Sprintf("Id=%s\nType=daemon\nActiveState=inactive\nUnitFileState=enabled\nNames=%[1]s\nNeedDaemonReload=no\n", args[2]))
				} else {
					output = []byte(fmt.Sprintf("Id=%s\nType=\nActiveState=inactive\nUnitFileState=\nNames=%[1]s\nNeedDaemonReload=no\n", args[2]))
				}
			}
		}
		return output
	}

	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)
	s.serviceInstalled = true
	s.systemctlArgs = nil
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *servicesSuite) TestConfigureServiceInvalidValue(c *C) {
	err := configcore.Run(coreDev, &mockConf{
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
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "sshd.service"},
		{"unmask", "sshd.service"},
		{"--no-reload", "enable", "sshd.service"},
		{"daemon-reload"},
		{"start", "sshd.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabled(c *C) {
	err := configcore.SwitchDisableService("sshd.service", true, nil)
	c.Assert(err, IsNil)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "sshd.service"},
		{"--no-reload", "disable", "sshd.service"},
		{"mask", "sshd.service"},
		{"stop", "sshd.service"},
		{"show", "--property=ActiveState", "sshd.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabledIntegration(c *C) {
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/ssh"), 0755)
	c.Assert(err, IsNil)

	for _, service := range []struct {
		cfgName     string
		systemdName string
		installed   bool
	}{
		{"ssh", "ssh.service", true},  // no installed check for ssh
		{"ssh", "ssh.service", false}, // no installed check for ssh
		{"rsyslog", "rsyslog.service", true},
		{"rsyslog", "rsyslog.service", false},
		{"systemd-resolved", "systemd-resolved.service", true},
		{"systemd-resolved", "systemd-resolved.service", false},
	} {
		s.systemctlArgs = nil
		s.serviceInstalled = service.installed
		err := configcore.Run(coreDev, &mockConf{
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
		default:
			if service.installed {
				c.Check(s.systemctlArgs, DeepEquals, [][]string{
					{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srv},
					{"--no-reload", "disable", srv},
					{"mask", srv},
					{"stop", srv},
					{"show", "--property=ActiveState", srv},
				})
			} else {
				c.Check(s.systemctlArgs, DeepEquals, [][]string{
					{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srv},
				})
			}
		}
	}
}

func (s *servicesSuite) TestConfigureConsoleConfDisableFSOnly(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"service.console-conf.disable": true,
	})

	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	consoleConfDisabled := filepath.Join(tmpDir, "/var/lib/console-conf/complete")
	c.Check(consoleConfDisabled, testutil.FileEquals, "console-conf has been disabled by the snapd system configuration\n")
}

func (s *servicesSuite) TestConfigureConsoleConfEnabledFSOnly(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"service.console-conf.disable": false,
	})

	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	consoleConfDisabled := filepath.Join(tmpDir, "/var/lib/console-conf/complete")
	c.Check(consoleConfDisabled, testutil.FileAbsent)
}

func (s *servicesSuite) TestConfigureConsoleConfEnableNotAtRuntime(c *C) {
	// pretend that console-conf is disabled
	canary := filepath.Join(dirs.GlobalRootDir, "/var/lib/console-conf/complete")
	err := os.MkdirAll(filepath.Dir(canary), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(canary, nil, 0644)
	c.Assert(err, IsNil)

	// now enable it
	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.console-conf.disable": false,
		},
	})
	c.Assert(err, ErrorMatches, "cannot toggle console-conf at runtime, but only initially via gadget defaults")
}

func (s *servicesSuite) TestConfigureConsoleConfDisableNotAtRuntime(c *C) {
	// console-conf is not disabled, i.e. there is no
	// "/var/lib/console-conf/complete" file

	// now try to enable it
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.console-conf.disable": true,
		},
	})
	c.Assert(err, ErrorMatches, "cannot toggle console-conf at runtime, but only initially via gadget defaults")
}

func (s *servicesSuite) TestConfigureConsoleConfEnableAlreadyEnabledIsFine(c *C) {
	// Note that we have no
	//        /var/lib/console-conf/complete
	// file. So console-conf is already enabled
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.console-conf.disable": false,
		},
	})
	c.Assert(err, IsNil)
}

func (s *servicesSuite) TestConfigureConsoleConfDisableAlreadyDisabledIsFine(c *C) {
	// pretend that console-conf is disabled
	canary := filepath.Join(dirs.GlobalRootDir, "/var/lib/console-conf/complete")
	err := os.MkdirAll(filepath.Dir(canary), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(canary, nil, 0644)
	c.Assert(err, IsNil)

	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.console-conf.disable": true,
		},
	})
	c.Assert(err, IsNil)
}

func (s *servicesSuite) TestConfigureConsoleConfEnableDuringInstallMode(c *C) {
	mockProcCmdline := filepath.Join(c.MkDir(), "cmdline")
	err := ioutil.WriteFile(mockProcCmdline, []byte("snapd_recovery_mode=install snapd_recovery_system=20201212\n"), 0644)
	c.Assert(err, IsNil)
	restore := osutil.MockProcCmdline(mockProcCmdline)
	defer restore()

	err = configcore.Run(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.console-conf.disable": true,
		},
	})
	// no error because we are in install mode
	c.Assert(err, IsNil)
}

func (s *servicesSuite) TestConfigureServiceEnableIntegration(c *C) {
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/ssh"), 0755)
	c.Assert(err, IsNil)

	for _, service := range []struct {
		cfgName     string
		systemdName string
		installed   bool
	}{
		{"ssh", "ssh.service", true},  // no installed check for ssh
		{"ssh", "ssh.service", false}, // no installed check for ssh
		{"rsyslog", "rsyslog.service", true},
		{"rsyslog", "rsyslog.service", false},
		{"systemd-resolved", "systemd-resolved.service", true},
		{"systemd-resolved", "systemd-resolved.service", false},
	} {
		s.systemctlArgs = nil
		s.serviceInstalled = service.installed
		err := configcore.Run(coreDev, &mockConf{
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
				{"unmask", "sshd.service"},
				{"unmask", "ssh.service"},
				{"start", srv},
			})
			sshCanary := filepath.Join(dirs.GlobalRootDir, "/etc/ssh/sshd_not_to_be_run")
			_, err := os.Stat(sshCanary)
			c.Assert(err, ErrorMatches, ".* no such file or directory")
		default:
			if service.installed {
				c.Check(s.systemctlArgs, DeepEquals, [][]string{
					{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srv},
					{"unmask", srv},
					{"--no-reload", "enable", srv},
					{"daemon-reload"},
					{"start", srv},
				})
			} else {
				c.Check(s.systemctlArgs, DeepEquals, [][]string{
					{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", srv},
				})
			}
		}
	}
}

func (s *servicesSuite) TestConfigureServiceUnsupportedService(c *C) {
	err := configcore.Run(coreDev, &mockConf{
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
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", tmpDir, "mask", "rsyslog.service"},
	})
}
