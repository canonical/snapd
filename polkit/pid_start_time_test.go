// -*- Mode: Go; indent-tabs-mode: t -*-
// +build linux

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

package polkit

import (
	"os"
	"syscall"
	"testing"

	"gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type polkitSuite struct{}

var _ = check.Suite(&polkitSuite{})

func (s *polkitSuite) TestGetStartTime(c *check.C) {
	pid := os.Getpid()

	startTime, err := getStartTimeForPid(uint32(pid))
	c.Assert(err, check.IsNil)
	c.Check(startTime, check.Not(check.Equals), uint64(0))
}

func (s *polkitSuite) TestGetStartTimeBadPid(c *check.C) {
	// Find an unused process ID by checking for errors from Kill.
	pid := 2
	for {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			break
		}
		pid += 1
	}

	startTime, err := getStartTimeForPid(uint32(pid))
	c.Assert(err, check.ErrorMatches, "open .*: no such file or directory")
	c.Check(startTime, check.Equals, uint64(0))
}
