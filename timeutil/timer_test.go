// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package timeutil_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/timeutil"
)

type timerSuite struct{}

var _ = Suite(&timerSuite{})

var _ timeutil.Timer = timeutil.StdlibTimer{}

func (s *timerSuite) TestAfterFuncExpiredC(c *C) {
	var timer timeutil.Timer = timeutil.AfterFunc(time.Second, func() {})
	c.Assert(timer, NotNil)
	c.Assert(timer.ExpiredC(), IsNil)
	active := timer.Stop()
	c.Assert(active, Equals, true)
}

func (s *timerSuite) TestAfter(c *C) {
	c.Assert(timeutil.After(time.Second), NotNil)
	before := time.Now()
	fired := <-timeutil.After(time.Nanosecond)
	c.Check(fired.After(before), Equals, true)
	c.Check(time.Now().After(fired), Equals, true)
}

func (s *timerSuite) TestNewTimerExpiredC(c *C) {
	before := time.Now()
	var timer timeutil.Timer = timeutil.NewTimer(time.Nanosecond)
	c.Assert(timer, NotNil)
	c.Assert(timer.ExpiredC(), NotNil)
	fired := <-timer.ExpiredC()
	after := time.Now()
	c.Check(before.Before(fired), Equals, true)
	c.Check(after.After(fired), Equals, true)
	active := timer.Reset(time.Second)
	c.Check(active, Equals, false)
	active = timer.Stop()
	c.Check(active, Equals, true)
}
