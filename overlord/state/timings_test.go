// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package state_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type timingsSuite struct {
	testutil.BaseTest
	st *state.State
}

var _ = Suite(&timingsSuite{})

func (s *timingsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.st = state.New(nil)

	s.mockDurationThreshold(0)
}

func (s *timingsSuite) mockDurationThreshold(threshold time.Duration) {
	oldDurationThreshold := timings.DurationThreshold
	timings.DurationThreshold = threshold
	restore := func() {
		timings.DurationThreshold = oldDurationThreshold
	}
	s.AddCleanup(restore)
}

func (s *timingsSuite) TestTagTimingsWithChange(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	chg := s.st.NewChange("change", "...")
	task := s.st.NewTask("kind", "...")
	task.SetStatus(state.DoingStatus)
	chg.AddTask(task)

	timing := timings.New(nil)
	state.TagTimingsWithChange(timing, chg)
	timing.Save(s.st)

	tims := mylog.Check2(timings.Get(s.st, 1, func(tags map[string]string) bool { return true }))

	c.Assert(tims, HasLen, 1)
	c.Check(tims[0].NestedTimings, HasLen, 0)
	c.Check(tims[0].Tags, DeepEquals, map[string]string{
		"change-id": chg.ID(),
	})
}

func (s *timingsSuite) TestTimingsForTask(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	task := s.st.NewTask("kind", "...")
	task.SetStatus(state.DoingStatus)
	chg := s.st.NewChange("change", "...")
	chg.AddTask(task)

	troot := state.TimingsForTask(task)
	span := troot.StartSpan("foo", "bar")
	span.Stop()
	troot.Save(s.st)

	tims := mylog.Check2(timings.Get(s.st, 1, func(tags map[string]string) bool { return true }))

	c.Assert(tims, HasLen, 1)
	c.Check(tims[0].NestedTimings, HasLen, 1)
	c.Check(tims[0].Tags, DeepEquals, map[string]string{
		"change-id":   chg.ID(),
		"task-id":     task.ID(),
		"task-kind":   "kind",
		"task-status": "Doing",
	})
}
