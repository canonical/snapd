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

package main_test

import (
	"io"
	"os"
	"strconv"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	repair "github.com/snapcore/snapd/cmd/snap-repair"
)

func (r *repairSuite) TestStatusNoStatusFdEnv(c *C) {
	for _, s := range []string{"done", "skip", "retry"} {
		mylog.Check(repair.ParseArgs([]string{s}))
		c.Check(err, ErrorMatches, "cannot find SNAP_REPAIR_STATUS_FD environment")
	}
}

func (r *repairSuite) TestStatusBadStatusFD(c *C) {
	for _, s := range []string{"done", "skip", "retry"} {
		os.Setenv("SNAP_REPAIR_STATUS_FD", "123456789")
		defer os.Unsetenv("SNAP_REPAIR_STATUS_FD")
		mylog.Check(repair.ParseArgs([]string{s}))
		c.Check(err, ErrorMatches, `write <snap-repair-status-fd>: bad file descriptor`)
	}
}

func (r *repairSuite) TestStatusUnparsableStatusFD(c *C) {
	for _, s := range []string{"done", "skip", "retry"} {
		os.Setenv("SNAP_REPAIR_STATUS_FD", "xxx")
		defer os.Unsetenv("SNAP_REPAIR_STATUS_FD")
		mylog.Check(repair.ParseArgs([]string{s}))
		c.Check(err, ErrorMatches, `cannot parse SNAP_REPAIR_STATUS_FD environment: strconv.*: parsing "xxx": invalid syntax`)
	}
}

func (r *repairSuite) TestStatusHappy(c *C) {
	for _, s := range []string{"done", "skip", "retry"} {
		rp, wp := mylog.Check3(os.Pipe())

		defer rp.Close()
		defer wp.Close()

		fd, e := syscall.Dup(int(wp.Fd()))
		c.Assert(e, IsNil)
		wp.Close()

		os.Setenv("SNAP_REPAIR_STATUS_FD", strconv.Itoa(fd))
		defer os.Unsetenv("SNAP_REPAIR_STATUS_FD")
		mylog.Check(repair.ParseArgs([]string{s}))
		c.Check(err, IsNil)

		status := mylog.Check2(io.ReadAll(rp))

		c.Check(string(status), Equals, s+"\n")
	}
}
