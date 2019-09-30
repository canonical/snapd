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

package selinux_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/selinux"
)

type selinuxBasicSuite struct{}

var _ = Suite(&selinuxBasicSuite{})

func (s *selinuxBasicSuite) TestProbeNone(c *C) {
	restore := selinux.MockIsEnabled(func() (bool, error) { return false, nil })
	defer restore()

	level, status := selinux.ProbeSELinux()
	c.Assert(level, Equals, selinux.Unsupported)
	c.Assert(status, Equals, "")

	c.Assert(selinux.ProbedLevel(), Equals, level)
	c.Assert(selinux.Summary(), Equals, status)
}

func (s *selinuxBasicSuite) TestProbeEnforcingHappy(c *C) {
	restore := selinux.MockIsEnabled(func() (bool, error) { return true, nil })
	defer restore()
	restore = selinux.MockSELinuxIsEnforcing(func() (bool, error) { return true, nil })
	defer restore()

	level, status := selinux.ProbeSELinux()
	c.Assert(level, Equals, selinux.Enforcing)
	c.Assert(status, Equals, "SELinux is enabled and in enforcing mode")

	c.Assert(selinux.ProbedLevel(), Equals, level)
	c.Assert(selinux.Summary(), Equals, status)
}

func (s *selinuxBasicSuite) TestProbeEnabledError(c *C) {
	restore := selinux.MockIsEnabled(func() (bool, error) { return true, errors.New("so much fail") })
	defer restore()

	level, status := selinux.ProbeSELinux()
	c.Assert(level, Equals, selinux.Unsupported)
	c.Assert(status, Equals, "so much fail")

	c.Assert(selinux.ProbedLevel(), Equals, level)
	c.Assert(selinux.Summary(), Equals, status)
}

func (s *selinuxBasicSuite) TestProbeEnforcingError(c *C) {
	restore := selinux.MockIsEnabled(func() (bool, error) { return true, nil })
	defer restore()
	restore = selinux.MockSELinuxIsEnforcing(func() (bool, error) { return true, errors.New("so much fail") })
	defer restore()

	level, status := selinux.ProbeSELinux()
	c.Assert(level, Equals, selinux.Unsupported)
	c.Assert(status, Equals, "SELinux is enabled, but status cannot be determined: so much fail")

	c.Assert(selinux.ProbedLevel(), Equals, level)
	c.Assert(selinux.Summary(), Equals, status)
}

func (s *selinuxBasicSuite) TestProbePermissive(c *C) {
	restore := selinux.MockIsEnabled(func() (bool, error) { return true, nil })
	defer restore()
	restore = selinux.MockSELinuxIsEnforcing(func() (bool, error) { return false, nil })
	defer restore()

	level, status := selinux.ProbeSELinux()
	c.Assert(level, Equals, selinux.Permissive)
	c.Assert(status, Equals, "SELinux is enabled but in permissive mode")

	c.Assert(selinux.ProbedLevel(), Equals, level)
	c.Assert(selinux.Summary(), Equals, status)
}
