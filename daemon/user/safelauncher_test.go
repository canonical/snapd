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

package user_test

import (
	"testing"

	"github.com/snapcore/snapd/daemon/user"

	"fmt"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type safeLauncherSuite struct {
	i      int
	args   [][]string
	errors []error
	outs   [][]byte
}

var _ = Suite(&safeLauncherSuite{})

func (s *safeLauncherSuite) myXdgOpen(args ...string) (err error) {
	s.args = append(s.args, args)
	if s.i < len(s.errors) {
		err = s.errors[s.i]
	}
	s.i++
	return err
}

func (s *safeLauncherSuite) SetUpTest(c *C) {
	user.XdgOpenCommand = s.myXdgOpen
	s.i = 0
	s.args = nil
	s.errors = nil
	s.outs = nil
}

func (s *safeLauncherSuite) TestOpenURLWithNotAllowedScheme(c *C) {
	launcher := &user.SafeLauncher{}
	err := launcher.OpenURL("tel://049112233445566")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "Supplied URL scheme \"tel\" is not allowed")
	c.Assert(s.args, IsNil)

	err = launcher.OpenURL("aabbccdd0011")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "Supplied URL scheme \"\" is not allowed")
	c.Assert(s.args, IsNil)
}

func (s *safeLauncherSuite) TestOpenURLWithAllowedSchemeHTTP(c *C) {
	launcher := &user.SafeLauncher{}
	err := launcher.OpenURL("http://snapcraft.io")
	c.Assert(err, IsNil)
	c.Assert(s.args, DeepEquals, [][]string{{"http://snapcraft.io"}})
}

func (s *safeLauncherSuite) TestOpenURLWithAllowedSchemeHTTPS(c *C) {
	launcher := &user.SafeLauncher{}
	err := launcher.OpenURL("https://snapcraft.io")
	c.Assert(err, IsNil)
	c.Assert(s.args, DeepEquals, [][]string{{"https://snapcraft.io"}})
}

func (s *safeLauncherSuite) TestOpenURLWithAllowedSchemeMailto(c *C) {
	launcher := &user.SafeLauncher{}
	err := launcher.OpenURL("mailto:foo@bar.org")
	c.Assert(err, IsNil)
	c.Assert(s.args, DeepEquals, [][]string{{"mailto:foo@bar.org"}})
}

func (s *safeLauncherSuite) TestOpenURLWithFailingXdgOpen(c *C) {
	user.XdgOpenCommand = func(args ...string) error {
		return fmt.Errorf("failed")
	}
	launcher := &user.SafeLauncher{}
	err := launcher.OpenURL("https://snapcraft.io")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "Can not open supplied URL")
}
