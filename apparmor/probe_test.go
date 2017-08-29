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

package apparmor_test

import (
	. "gopkg.in/check.v1"
	"testing"

	"github.com/snapcore/snapd/apparmor"
)

func Test(t *testing.T) {
	TestingT(t)
}

type probeSuite struct{}

var _ = Suite(&probeSuite{})

func (s *probeSuite) TestMockProbeNone(c *C) {
	restore := apparmor.MockFeatureLevel(apparmor.None)
	defer restore()

	ks := apparmor.ProbeKernel()
	c.Assert(ks.IsEnabled(), Equals, false)
	c.Assert(ks.SupportsFeature("dbus"), Equals, false)
	c.Assert(ks.SupportsFeature("file"), Equals, false)

	level, summary := ks.Evaluate()
	c.Assert(level, Equals, apparmor.None)
	c.Assert(summary, Equals, "apparmor is not enabled")
}

func (s *probeSuite) TestMockProbePartial(c *C) {
	restore := apparmor.MockFeatureLevel(apparmor.Partial)
	defer restore()

	ks := apparmor.ProbeKernel()
	c.Assert(ks.IsEnabled(), Equals, true)
	c.Assert(ks.SupportsFeature("dbus"), Equals, false)
	c.Assert(ks.SupportsFeature("file"), Equals, true)

	level, summary := ks.Evaluate()
	c.Assert(level, Equals, apparmor.Partial)
	c.Assert(summary, Equals, "apparmor is enabled but some features are missing: dbus, mount, namespaces, ptrace, signal")
}

func (s *probeSuite) TestMockProbeFull(c *C) {
	restore := apparmor.MockFeatureLevel(apparmor.Full)
	defer restore()

	ks := apparmor.ProbeKernel()
	c.Assert(ks.IsEnabled(), Equals, true)
	c.Assert(ks.SupportsFeature("dbus"), Equals, true)
	c.Assert(ks.SupportsFeature("file"), Equals, true)

	level, summary := ks.Evaluate()
	c.Assert(level, Equals, apparmor.Full)
	c.Assert(summary, Equals, "apparmor is enabled and all features are available")
}
