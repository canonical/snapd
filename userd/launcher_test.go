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

package userd_test

import (
	"testing"

	"github.com/godbus/dbus"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/userd"
)

func Test(t *testing.T) { TestingT(t) }

type launcherSuite struct {
	launcher *userd.Launcher

	mockXdgOpen *testutil.MockCmd
}

var _ = Suite(&launcherSuite{})

func (s *launcherSuite) SetUpTest(c *C) {
	s.launcher = &userd.Launcher{}
	s.mockXdgOpen = testutil.MockCommand(c, "xdg-open", "")
}

func (s *launcherSuite) TearDownTest(c *C) {
	s.mockXdgOpen.Restore()
}

func (s *launcherSuite) TestOpenURLWithNotAllowedScheme(c *C) {
	for _, t := range []struct {
		url        string
		errMatcher string
	}{
		{"tel://049112233445566", "Supplied URL scheme \"tel\" is not allowed"},
		{"aabbccdd0011", "Supplied URL scheme \"\" is not allowed"},
		{"invälid:%url", dbus.ErrMsgInvalidArg.Error()},
	} {
		err := s.launcher.OpenURL(t.url)
		c.Assert(err, ErrorMatches, t.errMatcher)
		c.Assert(s.mockXdgOpen.Calls(), IsNil)
	}
}

func (s *launcherSuite) TestOpenURLWithAllowedSchemeHappy(c *C) {
	for _, schema := range []string{"http", "https", "mailto"} {
		err := s.launcher.OpenURL(schema + "://snapcraft.io")
		c.Assert(err, IsNil)
		c.Assert(s.mockXdgOpen.Calls(), DeepEquals, [][]string{
			{"xdg-open", schema + "://snapcraft.io"},
		})
		s.mockXdgOpen.ForgetCalls()
	}
}

func (s *launcherSuite) TestOpenURLWithFailingXdgOpen(c *C) {
	cmd := testutil.MockCommand(c, "xdg-open", "false")
	defer cmd.Restore()

	err := s.launcher.OpenURL("https://snapcraft.io")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "cannot open supplied URL")
}
