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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type servicesSuite struct {
	configcoreSuite
	testutil.BaseTest
}

var _ = Suite(&servicesSuite{})

func (s *servicesSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)
	s.systemctlArgs = nil
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *servicesSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	s.BaseTest.TearDownTest(c)
}

func (s *servicesSuite) TestConfigureServiceInvalidValue(c *C) {
	err := configcore.SwitchDisableService("ssh", "xxx")
	c.Check(err, ErrorMatches, `option "ssh.service" has invalid value "xxx"`)
}

func (s *servicesSuite) TestConfigureServiceNotDisabled(c *C) {
	err := configcore.SwitchDisableService("ssh", "false")
	c.Assert(err, IsNil)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "unmask", "ssh.service"},
		{"--root", dirs.GlobalRootDir, "enable", "ssh.service"},
		{"start", "ssh.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabled(c *C) {
	err := configcore.SwitchDisableService("ssh", "true")
	c.Assert(err, IsNil)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "disable", "ssh.service"},
		{"--root", dirs.GlobalRootDir, "mask", "ssh.service"},
		{"stop", "ssh.service"},
		{"show", "--property=ActiveState", "ssh.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabledIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, srvName := range []string{"ssh", "rsyslog", "snapd.autoimport"} {
		s.systemctlArgs = nil

		err := configcore.Run(&mockConf{
			conf: map[string]interface{}{
				fmt.Sprintf("service.%s.disable", srvName): true,
			},
		})
		c.Assert(err, IsNil)
		srv := fmt.Sprintf("%s.service", srvName)
		c.Check(s.systemctlArgs, DeepEquals, [][]string{
			{"--root", dirs.GlobalRootDir, "disable", srv},
			{"--root", dirs.GlobalRootDir, "mask", srv},
			{"stop", srv},
			{"show", "--property=ActiveState", srv},
		})
	}
}

func (s *servicesSuite) TestConfigureServiceEnableIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, srvName := range []string{"ssh", "rsyslog", "snapd.autoimport"} {
		s.systemctlArgs = nil
		err := configcore.Run(&mockConf{
			conf: map[string]interface{}{
				fmt.Sprintf("service.%s.disable", srvName): false,
			},
		})

		c.Assert(err, IsNil)
		srv := fmt.Sprintf("%s.service", srvName)
		c.Check(s.systemctlArgs, DeepEquals, [][]string{
			{"--root", dirs.GlobalRootDir, "unmask", srv},
			{"--root", dirs.GlobalRootDir, "enable", srv},
			{"start", srv},
		})
	}
}

func (s *servicesSuite) TestConfigureServiceUnsupportedService(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := configcore.Run(&mockConf{
		conf: map[string]interface{}{
			"service.snapd.disable": true,
		},
	})
	c.Assert(err, IsNil)

	// ensure nothing gets enabled/disabled when an unsupported
	// service is set for disable
	c.Check(s.systemctlArgs, IsNil)
}
