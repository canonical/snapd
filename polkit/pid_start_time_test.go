// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

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
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ddkwork/golibrary/mylog"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type polkitSuite struct{}

var _ = check.Suite(&polkitSuite{})

func (s *polkitSuite) TestGetStartTime(c *check.C) {
	pid := os.Getpid()

	startTime := mylog.Check2(getStartTimeForPid(int32(pid)))
	c.Assert(err, check.IsNil)
	c.Check(startTime, check.Not(check.Equals), uint64(0))
}

func (s *polkitSuite) TestGetStartTimeBadPid(c *check.C) {
	// Find an unused process ID by checking for errors from Kill.
	pid := 2
	for {
		if mylog.Check(syscall.Kill(pid, 0)); err == syscall.ESRCH {
			break
		}
		pid += 1
	}

	startTime := mylog.Check2(getStartTimeForPid(int32(pid)))
	c.Assert(err, check.ErrorMatches, "open .*: no such file or directory")
	c.Check(startTime, check.Equals, uint64(0))
}

func (s *polkitSuite) TestProcStatParsing(c *check.C) {
	filename := filepath.Join(c.MkDir(), "stat")
	contents := []byte("18433 (cat) R 9732 18433 9732 34818 18433 4194304 96 0 1 0 0 0 0 0 20 0 1 0 123104764 7602176 182 18446744073709551615 94902526107648 94902526138492 140734457666896 0 0 0 0 0 0 0 0 0 17 5 0 0 0 0 0 94902528236168 94902528237760 94902542680064 140734457672267 140734457672287 140734457672287 140734457675759 0")
	mylog.Check(os.WriteFile(filename, contents, 0644))
	c.Assert(err, check.IsNil)

	startTime := mylog.Check2(getStartTimeForProcStatFile(filename))
	c.Assert(err, check.IsNil)
	c.Check(startTime, check.Equals, uint64(123104764))
}
