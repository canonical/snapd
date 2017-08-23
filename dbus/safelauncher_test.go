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

package dbus_test

import (
	"testing"

	"github.com/snapcore/snapd/dbus"

	"fmt"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type safeLauncherSuite struct {
	errCnt   int
	args     [][]string
	errors   []error
	outs     [][]byte
	launcher *dbus.SafeLauncher

	restoreXdgOpen func()
}

var _ = Suite(&safeLauncherSuite{})

func (s *safeLauncherSuite) myXdgOpen(args ...string) (err error) {
	s.args = append(s.args, args)
	if s.errCnt < len(s.errors) {
		err = s.errors[s.errCnt]
	}
	s.errCnt++
	return err
}

func (s *safeLauncherSuite) SetUpTest(c *C) {
	s.restoreXdgOpen = dbus.MockXdgOpenCommand(s.myXdgOpen)
	s.errCnt = 0
	s.args = nil
	s.errors = nil
	s.outs = nil
	s.launcher = &dbus.SafeLauncher{}
}

func (s *safeLauncherSuite) TearDownTest(c *C) {
	s.restoreXdgOpen()
}

func (s *safeLauncherSuite) TestOpenURLWithNotAllowedScheme(c *C) {
	err := s.launcher.OpenURL("tel://049112233445566")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "Supplied URL scheme \"tel\" is not allowed")
	c.Assert(s.args, IsNil)

	err = s.launcher.OpenURL("aabbccdd0011")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "Supplied URL scheme \"\" is not allowed")
	c.Assert(s.args, IsNil)
}

func (s *safeLauncherSuite) TestOpenURLWithAllowedSchemeHTTP(c *C) {
	err := s.launcher.OpenURL("http://snapcraft.io")
	c.Assert(err, IsNil)
	c.Assert(s.args, DeepEquals, [][]string{{"http://snapcraft.io"}})
}

func (s *safeLauncherSuite) TestOpenURLWithAllowedSchemeHTTPS(c *C) {
	err := s.launcher.OpenURL("https://snapcraft.io")
	c.Assert(err, IsNil)
	c.Assert(s.args, DeepEquals, [][]string{{"https://snapcraft.io"}})
}

func (s *safeLauncherSuite) TestOpenURLWithAllowedSchemeMailto(c *C) {
	err := s.launcher.OpenURL("mailto:foo@bar.org")
	c.Assert(err, IsNil)
	c.Assert(s.args, DeepEquals, [][]string{{"mailto:foo@bar.org"}})
}

func (s *safeLauncherSuite) TestOpenURLWithFailingXdgOpen(c *C) {
	dbus.MockXdgOpenCommand(func(args ...string) error {
		return fmt.Errorf("failed")
	})
	err := s.launcher.OpenURL("https://snapcraft.io")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "Can not open supplied URL")
}
