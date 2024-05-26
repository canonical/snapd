// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package snapdenv_test

import (
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
)

type UASuite struct {
	restore func()
}

var _ = Suite(&UASuite{})

func (s *UASuite) SetUpTest(c *C) {
	s.restore = snapdenv.MockUserAgent("-")
}

func (s *UASuite) TearDownTest(c *C) {
	s.restore()
}

func (s *UASuite) TestUserAgent(c *C) {
	snapdenv.SetUserAgentFromVersion("10", nil)
	ua := snapdenv.UserAgent()
	c.Check(strings.HasPrefix(ua, "snapd/10 "), Equals, true)

	snapdenv.SetUserAgentFromVersion("10", nil, "extraProd")
	ua = snapdenv.UserAgent()
	c.Check(strings.Contains(ua, "extraProd"), Equals, true)
	c.Check(strings.Contains(ua, "devmode"), Equals, false)

	devmode := false
	probeForceDevMode := func() bool { return devmode }

	snapdenv.SetUserAgentFromVersion("10", probeForceDevMode, "extraProd")
	ua = snapdenv.UserAgent()
	c.Check(strings.Contains(ua, "devmode"), Equals, false)

	devmode = true
	snapdenv.SetUserAgentFromVersion("10", probeForceDevMode, "extraProd")
	ua = snapdenv.UserAgent()
	c.Check(strings.Contains(ua, "devmode"), Equals, true)
}

func (s *UASuite) TestUserAgentWSL(c *C) {
	defer testutil.Backup(&release.OnWSL)()

	release.OnWSL = false
	snapdenv.SetUserAgentFromVersion("10", nil)
	ua := snapdenv.UserAgent()
	c.Check(strings.Contains(ua, "wsl"), Equals, false)

	release.OnWSL = true
	snapdenv.SetUserAgentFromVersion("10", nil)
	ua = snapdenv.UserAgent()
	c.Check(strings.Contains(ua, "wsl"), Equals, true)
}

func (s *UASuite) TestStripUnsafeRunes(c *C) {
	// Validity check, strings like that are not modified
	for _, unchanged := range []string{
		"abc-xyz-ABC-XYZ-0-9",
		".", "-", "_",
		"4.4.0-62-generic",
		"4.8.6-x86_64-linode78",
	} {
		c.Check(snapdenv.StripUnsafeRunes(unchanged), Equals, unchanged, Commentf("%q", unchanged))
	}
	for _, t := range []struct{ orig, changed string }{
		{"space bar", "spacebar"},
		{"~;+()[]", ""}, // most punctuation goes away
	} {
		c.Check(snapdenv.StripUnsafeRunes(t.orig), Equals, t.changed)
	}
}

func (s *UASuite) TestSanitizeKernelVersion(c *C) {
	// Ensure that it is not too long (at most 25 runes)
	const in = "this-is-a-very-long-thing-that-pretends-to-be-a-kernel-version-string"
	const out = "this-is-a-very-long-thing"
	c.Check(snapdenv.SanitizeKernelVersion(in), Equals, out)
}
