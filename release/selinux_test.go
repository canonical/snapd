// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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

package release_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/release"
)

type selinuxSuite struct{}

var _ = Suite(&selinuxSuite{})

func (s *selinuxSuite) TestProbeNone(c *C) {
	restore := release.MockSELinuxIsEnabled(func() (bool, error) { return false, nil })
	defer restore()

	level, status := release.ProbeSELinux()
	c.Assert(level, Equals, release.NoSELinux)
	c.Assert(status, Equals, "")

	c.Assert(release.SELinuxLevel(), Equals, level)
	c.Assert(release.SELinuxSummary(), Equals, status)
}

func (s *selinuxSuite) TestProbeEnforcingHappy(c *C) {
	restore := release.MockSELinuxIsEnabled(func() (bool, error) { return true, nil })
	defer restore()
	restore = release.MockSELinuxIsEnforcing(func() (bool, error) { return true, nil })
	defer restore()

	level, status := release.ProbeSELinux()
	c.Assert(level, Equals, release.SELinuxEnforcing)
	c.Assert(status, Equals, "SELinux is enabled and in enforcing mode")

	c.Assert(release.SELinuxLevel(), Equals, level)
	c.Assert(release.SELinuxSummary(), Equals, status)
}

func (s *selinuxSuite) TestProbeEnabledError(c *C) {
	restore := release.MockSELinuxIsEnabled(func() (bool, error) { return true, errors.New("so much fail") })
	defer restore()

	level, status := release.ProbeSELinux()
	c.Assert(level, Equals, release.NoSELinux)
	c.Assert(status, Equals, "so much fail")

	c.Assert(release.SELinuxLevel(), Equals, level)
	c.Assert(release.SELinuxSummary(), Equals, status)
}

func (s *selinuxSuite) TestProbeEnforcingError(c *C) {
	restore := release.MockSELinuxIsEnabled(func() (bool, error) { return true, nil })
	defer restore()
	restore = release.MockSELinuxIsEnforcing(func() (bool, error) { return true, errors.New("so much fail") })
	defer restore()

	level, status := release.ProbeSELinux()
	c.Assert(level, Equals, release.NoSELinux)
	c.Assert(status, Equals, "SELinux is enabled, but status cannot be determined: so much fail")

	c.Assert(release.SELinuxLevel(), Equals, level)
	c.Assert(release.SELinuxSummary(), Equals, status)
}

func (s *selinuxSuite) TestProbePermissive(c *C) {
	restore := release.MockSELinuxIsEnabled(func() (bool, error) { return true, nil })
	defer restore()
	restore = release.MockSELinuxIsEnforcing(func() (bool, error) { return false, nil })
	defer restore()

	level, status := release.ProbeSELinux()
	c.Assert(level, Equals, release.SELinuxPermissive)
	c.Assert(status, Equals, "SELinux is enabled but in permissive mode")

	c.Assert(release.SELinuxLevel(), Equals, level)
	c.Assert(release.SELinuxSummary(), Equals, status)
}
