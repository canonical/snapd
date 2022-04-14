// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package syscheck_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/syscheck"
)

type cgroupSuite struct{}

var _ = Suite(&cgroupSuite{})

func (s *cgroupSuite) TestBadCgroupProbeHappy(c *C) {
	defer cgroup.MockVersion(cgroup.V1, nil)()

	c.Check(syscheck.CheckCgroup(), IsNil)
}

func (s *cgroupSuite) TestBadCgroupProbeUnknown(c *C) {
	defer cgroup.MockVersion(cgroup.Unknown, nil)()

	c.Check(syscheck.CheckCgroup(), ErrorMatches, "snapd could not determine cgroup version")
}

func (s *cgroupSuite) TestBadCgroupProbeErr(c *C) {
	defer cgroup.MockVersion(cgroup.Unknown, errors.New("nada"))()

	c.Check(syscheck.CheckCgroup(), ErrorMatches, "snapd could not probe cgroup version: nada")
}
