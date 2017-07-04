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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/corecfg"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
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
	s.systemctlArgs = nil
}

func (s *servicesSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *servicesSuite) TestConfigureServiceInvalidValue(c *C) {
	err := corecfg.SwitchDisableService("ssh", "xxx")
	c.Check(err, ErrorMatches, `Invalid value "xxx" provided for option "ssh.service"`)
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
