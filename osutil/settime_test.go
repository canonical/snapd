// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package osutil_test

import (
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

type settimeTestSuite struct{}

var _ = Suite(&settimeTestSuite{})

func (s *settimeTestSuite) TestSetTime(c *C) {
	timeIn := mylog.Check2(time.Parse("Mon Jan 2 15:04:05 -0700 MST 2006", "Mon Jan 2 15:04:05 -0700 MST 2006"))


	r := osutil.MockSyscallSettimeofday(func(t *syscall.Timeval) error {
		c.Assert(int64(t.Sec), Equals, timeIn.Unix())
		c.Assert(int64(t.Usec), Equals, timeIn.UnixNano()/1000%1000)
		return nil
	})
	defer r()
	mylog.Check(osutil.SetTime(timeIn))

}
