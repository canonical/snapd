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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
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
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"service.ssh.disable": "xxx",
		},
	}))
	c.Check(err, ErrorMatches, `option "service.ssh.disable" has invalid value "xxx"`)
}

func (s *servicesSuite) TestConfigureServiceNotDisabled(c *C) {
	mylog.Check(configcore.SwitchDisableService("sshd.service", false, nil))

	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "sshd.service"},
		{"unmask", "sshd.service"},
		{"--no-reload", "enable", "sshd.service"},
		{"daemon-reload"},
		{"start", "sshd.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabled(c *C) {
	mylog.Check(configcore.SwitchDisableService("sshd.service", true, nil))

	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "sshd.service"},
		{"--no-reload", "disable", "sshd.service"},
		{"mask", "sshd.service"},
		{"stop", "sshd.service"},
		{"show", "--property=ActiveState", "sshd.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabledIntegration(c *C) {
	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/ssh"), 0755))


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
		mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				fmt.Sprintf("service.%s.disable", service.cfgName): true,
			},
		}))

		srv := service.systemdName
		switch service.cfgName {
		case "ssh":
			sshCanary := filepath.Join(dirs.GlobalRootDir, "/etc/ssh/sshd_not_to_be_run")
			_ := mylog.Check2(os.Stat(sshCanary))

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
	modeenvContent := `mode=run
recovery_system=20200202
`
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapModeenvFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapModeenvFile, []byte(modeenvContent), 0644), IsNil)

	// pretend that console-conf is disabled
	canary := filepath.Join(dirs.GlobalRootDir, "/var/lib/console-conf/complete")
	mylog.Check(os.MkdirAll(filepath.Dir(canary), 0755))

	mylog.Check(os.WriteFile(canary, nil, 0644))

	mylog.

		// now enable it
		Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"service.console-conf.disable": false,
			},
		}))
	c.Assert(err, ErrorMatches, "cannot toggle console-conf at runtime, but only initially via gadget defaults")
}

func (s *servicesSuite) TestConfigureConsoleConfDisableNotAtRuntime(c *C) {
	modeenvContent := `mode=run
recovery_system=20200202
`
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapModeenvFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapModeenvFile, []byte(modeenvContent), 0644), IsNil)
	mylog.

		// console-conf is not disabled, i.e. there is no
		// "/var/lib/console-conf/complete" file
		Check(

			// now try to enable it
			configcore.FilesystemOnlyRun(coreDev, &mockConf{
				state: s.state,
				conf: map[string]interface{}{
					"service.console-conf.disable": true,
				},
			}))
	c.Assert(err, ErrorMatches, "cannot toggle console-conf at runtime, but only initially via gadget defaults")
}

func (s *servicesSuite) TestConfigureConsoleConfEnableAlreadyEnabledIsFine(c *C) {
	modeenvContent := `mode=run
recovery_system=20200202
`
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapModeenvFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapModeenvFile, []byte(modeenvContent), 0644), IsNil)
	mylog.

		// Note that we have no
		//        /var/lib/console-conf/complete
		// file. So console-conf is already enabled
		Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"service.console-conf.disable": false,
			},
		}))

}

func (s *servicesSuite) TestConfigureConsoleConfDisableAlreadyDisabledIsFine(c *C) {
	// pretend that console-conf is disabled
	canary := filepath.Join(dirs.GlobalRootDir, "/var/lib/console-conf/complete")
	mylog.Check(os.MkdirAll(filepath.Dir(canary), 0755))

	mylog.Check(os.WriteFile(canary, nil, 0644))


	modeenvContent := `mode=run
recovery_system=20200202
`
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapModeenvFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapModeenvFile, []byte(modeenvContent), 0644), IsNil)
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.console-conf.disable": true,
		},
	}))

}

func (s *servicesSuite) TestConfigureConsoleConfEnableDuringInstallMode(c *C) {
	modeenvContent := `mode=install
recovery_system=20200202
`
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapModeenvFile), 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapModeenvFile, []byte(modeenvContent), 0644), IsNil)
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.console-conf.disable": true,
		},
	}))
	// no error because we are in install mode

}

func (s *servicesSuite) TestConfigureServiceEnableIntegration(c *C) {
	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/ssh"), 0755))


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
		mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				fmt.Sprintf("service.%s.disable", service.cfgName): false,
			},
		}))


		srv := service.systemdName
		switch service.cfgName {
		case "ssh":
			c.Check(s.systemctlArgs, DeepEquals, [][]string{
				{"unmask", "sshd.service"},
				{"unmask", "ssh.service"},
				{"start", srv},
			})
			sshCanary := filepath.Join(dirs.GlobalRootDir, "/etc/ssh/sshd_not_to_be_run")
			_ := mylog.Check2(os.Stat(sshCanary))
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
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.snapd.disable": true,
		},
	}))


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

func (s *servicesSuite) TestConfigureNetworkSSHListenAddressFailsOnNonCore20(c *C) {
	mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"service.ssh.listen-address": ":8022",
		},
	}))
	c.Assert(err, ErrorMatches, "cannot set ssh listen address configuration on systems older than UC20")
}

func (s *servicesSuite) TestConfigureNetworkSSHListenAdressFailsWrongRange(c *C) {
	for _, invalidPort := range []int{0, 65536, -1, 99999} {
		mylog.Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"service.ssh.listen-address": fmt.Sprintf(":%v", invalidPort),
			},
		}))
		c.Check(err, ErrorMatches, fmt.Sprintf("cannot validate ssh configuration: port %v must be in the range 1-65535", invalidPort))
	}
}

func (s *servicesSuite) TestConfigureNetworkSSHListenAdressFailsWrongAddr(c *C) {
	for _, tc := range []struct {
		confStr string
		errStr  string
	}{
		// strange chars
		{"x!", `cannot validate ssh configuration: invalid hostname "x!"`},
		// invalid ports
		{"x:x", `cannot validate ssh configuration: port must be a number: strconv.Atoi: parsing "x": invalid syntax`},
		{"x:123456", "cannot validate ssh configuration: port 123456 must be in the range 1-65535"},
		// too long
		{"1234567890123456789012345678901234567890123456789012345678901234567890", `cannot validate ssh configuration: invalid hostname "1234567890123456789012345678901234567890123456789012345678901234567890"`},
		// mixing good/bad also rejected
		{"valid-hostname,invalid!one", `cannot validate ssh configuration: invalid hostname "invalid!one"`},
	} {
		mylog.Check(configcore.FilesystemOnlyRun(core20Dev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"service.ssh.listen-address": tc.confStr,
			},
		}))
		c.Check(err, ErrorMatches, tc.errStr, Commentf(tc.confStr))
	}
}

func (s *servicesSuite) TestConfigureNetworkValid(c *C) {
	sshListenCfg := filepath.Join(dirs.GlobalRootDir, "/etc/ssh/sshd_config.d/listen.conf")

	for _, tc := range []struct {
		confStr string
		sshConf string
	}{
		// valid hostnames/IPs
		{"host", "ListenAddress host\n"},
		{"10.0.2.2", "ListenAddress 10.0.2.2\n"},
		{"::1", "ListenAddress ::1\n"},
		{"[::1]:8022", "ListenAddress [::1]:8022\n"},
		{"2001", "ListenAddress 2001\n"},
		{"2001.net", "ListenAddress 2001.net\n"},
		// port only
		{":9022", "ListenAddress 0.0.0.0:9022\nListenAddress [::]:9022\n"},
		// multiple ones
		{"host1,host2", "ListenAddress host1\nListenAddress host2\n"},
	} {
		mylog.Check(configcore.FilesystemOnlyRun(core20Dev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"service.ssh.listen-address": tc.confStr,
			},
		}))

		c.Check(sshListenCfg, testutil.FileEquals, tc.sshConf)
	}
}

func (s *servicesSuite) TestSamePortNoChange(c *C) {
	mylog.Check(configcore.FilesystemOnlyRun(core20Dev, &mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"service.ssh.listen-address": ":8022",
		},
		changes: map[string]interface{}{
			"service.ssh.listen-address": ":8022",
		},
	}))

	c.Check(s.systemctlArgs, HasLen, 0)
}

func (s *servicesSuite) TestConfigureNetworkIntegrationSSHListenAddress(c *C) {
	mylog.Check(configcore.FilesystemOnlyRun(core20Dev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"service.ssh.listen-address": ":8022",
		},
	}))


	sshListenCfg := filepath.Join(dirs.GlobalRootDir, "/etc/ssh/sshd_config.d/listen.conf")
	c.Check(sshListenCfg, testutil.FileEquals, "ListenAddress 0.0.0.0:8022\nListenAddress [::]:8022\n")
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"reload-or-restart", "ssh.service"},
	})
	mylog.

		// disable port again
		Check(configcore.FilesystemOnlyRun(core20Dev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"service.ssh.listen-address": ":8022",
			},
			changes: map[string]interface{}{
				"service.ssh.listen-address": "",
			},
		}))

	c.Check(sshListenCfg, testutil.FileAbsent)
}

func (s *servicesSuite) TestConfigureNetworkIntegrationSSHListenAddressMulti(c *C) {
	mylog.Check(configcore.FilesystemOnlyRun(core20Dev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"service.ssh.listen-address": ":8022,192.168.99.4:9922",
		},
	}))


	sshListenCfg := filepath.Join(dirs.GlobalRootDir, "/etc/ssh/sshd_config.d/listen.conf")
	c.Check(sshListenCfg, testutil.FileEquals, "ListenAddress 0.0.0.0:8022\nListenAddress [::]:8022\nListenAddress 192.168.99.4:9922\n")
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"reload-or-restart", "ssh.service"},
	})
}
