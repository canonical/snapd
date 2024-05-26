// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/timeutil"
)

type humanSuite struct {
	beforeDSTbegins, afterDSTbegins, beforeDSTends, afterDSTends time.Time
}

var _ = check.Suite(&humanSuite{})

func (s *humanSuite) SetUpSuite(c *check.C) {
	loc := mylog.Check2(time.LoadLocation("Europe/London"))
	c.Assert(err, check.IsNil)

	s.beforeDSTbegins = time.Date(2017, 3, 26, 0, 59, 0, 0, loc)
	// note this is actually 2:01am DST
	s.afterDSTbegins = time.Date(2017, 3, 26, 1, 1, 0, 0, loc)

	// apparently no way to straight out initialise a time inside the DST overlap
	s.beforeDSTends = time.Date(2017, 10, 29, 0, 59, 0, 0, loc).Add(60 * time.Minute)
	s.afterDSTends = time.Date(2017, 10, 29, 1, 1, 0, 0, loc)

	// validity check
	c.Check(s.beforeDSTbegins.Format("MST"), check.Equals, s.afterDSTends.Format("MST"))
	c.Check(s.beforeDSTbegins.Format("MST"), check.Equals, "GMT")
	c.Check(s.afterDSTbegins.Format("MST"), check.Equals, s.beforeDSTends.Format("MST"))
	c.Check(s.afterDSTbegins.Format("MST"), check.Equals, "BST")

	// “The month, day, hour, min, sec, and nsec values may be outside their
	//  usual ranges and will be normalized during the conversion.”
	// so you can always add or subtract 1 from a day and it'll just work \o/
	c.Check(time.Date(2017, -1, -1, -1, -1, -1, 0, loc), check.DeepEquals, time.Date(2016, 10, 29, 22, 58, 59, 0, loc))
	c.Check(time.Date(2017, 13, 32, 25, 61, 63, 0, loc), check.DeepEquals, time.Date(2018, 2, 2, 2, 2, 3, 0, loc))
}

func (s *humanSuite) TestHumanTimeDST(c *check.C) {
	c.Check(timeutil.HumanTimeSince(s.beforeDSTbegins, s.afterDSTbegins, 300), check.Equals, "today at 00:59 GMT")
	c.Check(timeutil.HumanTimeSince(s.beforeDSTends, s.afterDSTends, 300), check.Equals, "today at 01:59 BST")
	c.Check(timeutil.HumanTimeSince(s.beforeDSTbegins, s.afterDSTends, 300), check.Equals, "217 days ago, at 00:59 GMT")
}

func (s *humanSuite) TestHumanTimeDSTMore(c *check.C) {
	loc := mylog.Check2(time.LoadLocation("Europe/London"))
	c.Assert(err, check.IsNil)
	d0 := time.Date(2018, 3, 23, 13, 14, 15, 0, loc)
	df := time.Date(2018, 3, 25, 13, 14, 15, 0, loc)
	c.Check(timeutil.HumanTimeSince(d0, df, 300), check.Equals, "2 days ago, at 13:14 GMT")
	c.Check(timeutil.HumanTimeSince(df, d0, 300), check.Equals, "in 2 days, at 13:14 BST")
}

func (*humanSuite) TestHuman(c *check.C) {
	now := time.Now()

	for i, expected := range []string{
		"2 days ago, at ", "yesterday at ", "today at ", "tomorrow at ", "in 2 days, at ",
	} {
		t := now.AddDate(0, 0, i-2)
		timePart := t.Format("15:04 MST")
		c.Check(timeutil.Human(t), check.Equals, expected+timePart)
	}

	// two outside of the 60-day cutoff:
	d1 := now.AddDate(0, -3, 0)
	d2 := now.AddDate(0, 3, 0)
	c.Check(timeutil.Human(d1), check.Equals, d1.Format("2006-01-02"))
	c.Check(timeutil.Human(d2), check.Equals, d2.Format("2006-01-02"))
}
