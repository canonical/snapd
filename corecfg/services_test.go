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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/corecfg"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type servicesSuite struct {
	systemctlArgs [][]string
}

var _ = Suite(&servicesSuite{})

func (s *servicesSuite) SetUpSuite(c *C) {
	systemd.SystemctlCmd = func(args ...string) ([]byte, error) {
		s.systemctlArgs = append(s.systemctlArgs, args[:])
		output := []byte("ActiveState=inactive")
		return output, nil
	}
}

func (s *servicesSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)
	s.systemctlArgs = nil
}

func (s *servicesSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *servicesSuite) TestConfigureServiceInvalidValue(c *C) {
	err := corecfg.SwitchDisableService("ssh", "xxx")
	c.Check(err, ErrorMatches, `option "ssh.service" has invalid value "xxx"`)
}

func (s *servicesSuite) TestConfigureServiceNotDisabled(c *C) {
	err := corecfg.SwitchDisableService("ssh", "false")
	c.Assert(err, IsNil)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "enable", "ssh.service"},
		{"start", "ssh.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabled(c *C) {
	err := corecfg.SwitchDisableService("ssh", "true")
	c.Assert(err, IsNil)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "disable", "ssh.service"},
		{"stop", "ssh.service"},
		{"show", "--property=ActiveState", "ssh.service"},
	})
}

func (s *servicesSuite) TestConfigureServiceDisabledIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, srvName := range []string{"ssh", "rsyslog"} {
		srv := fmt.Sprintf("%s.service", srvName)

		s.systemctlArgs = nil
		mockSnapctl := testutil.MockCommand(c, "snapctl", fmt.Sprintf(`
if [ "$1" = "get" ] && [ "$2" = "service.%s.disable" ]; then
    echo "true"
fi
`, srvName))
		defer mockSnapctl.Restore()

		err := corecfg.Run()
		c.Assert(err, IsNil)
		c.Check(mockSnapctl.Calls(), Not(HasLen), 0)
		c.Check(s.systemctlArgs, DeepEquals, [][]string{
			{"--version"},
			{"--root", dirs.GlobalRootDir, "disable", srv},
			{"stop", srv},
			{"show", "--property=ActiveState", srv},
		})
	}
}

func (s *servicesSuite) TestConfigureServiceEnableIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, srvName := range []string{"ssh", "rsyslog"} {
		srv := fmt.Sprintf("%s.service", srvName)

		s.systemctlArgs = nil
		mockSnapctl := testutil.MockCommand(c, "snapctl", fmt.Sprintf(`
if [ "$1" = "get" ] && [ "$2" = "service.%s.disable" ]; then
    echo "false"
fi
`, srvName))
		defer mockSnapctl.Restore()

		err := corecfg.Run()
		c.Assert(err, IsNil)
		c.Check(mockSnapctl.Calls(), Not(HasLen), 0)
		c.Check(s.systemctlArgs, DeepEquals, [][]string{
			{"--version"},
			{"--root", dirs.GlobalRootDir, "enable", srv},
			{"start", srv},
		})
	}
}

func (s *servicesSuite) TestConfigureServiceUnsupportedService(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.systemctlArgs = nil
	mockSnapctl := testutil.MockCommand(c, "snapctl", `
if [ "$1" = "get" ] && [ "$2" = "service.snapd.disable" ]; then
    echo "true"
fi
`)
	defer mockSnapctl.Restore()

	err := corecfg.Run()
	c.Assert(err, IsNil)

	// ensure nothing gets enabled/disabled when an unsupported
	// service is set for disable
	c.Check(mockSnapctl.Calls(), Not(HasLen), 0)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--version"},
	})
}
