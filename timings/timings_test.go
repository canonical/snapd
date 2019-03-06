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

package timings_test

import (
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func TestTimings(t *testing.T) { TestingT(t) }

type timingsSuite struct {
	testutil.BaseTest
	st       *state.State
	duration time.Duration
}

var _ = Suite(&timingsSuite{})

func (s *timingsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.st = state.New(nil)

	s.duration = 0
	// Increase duration by 1 millisecond on each call
	s.BaseTest.AddCleanup(timings.MockTimeDuration(func(start, end time.Time) time.Duration {
		c.Check(start.Before(end), Equals, true)
		s.duration += time.Millisecond
		return s.duration
	}))
}

func (s *timingsSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *timingsSuite) TestSave(c *C) {
	// two timings, with 2 nested measures
	for i := 0; i < 2; i++ {
		timing := timings.New(timings.Task, map[string]string{"id": "3", "change-id": "12"})
		meas := timing.Start(fmt.Sprintf("doing something-%d", i), "...")
		nested := meas.StartNested("nested measurement", "...")
		nestedMore := nested.StartNested("nested more", "...")
		nestedMore.Stop()
		nested.Stop()
		meas.Stop()
		timing.Save(s.st)
	}

	s.st.Lock()
	defer s.st.Unlock()

	var stateTimings []interface{}
	c.Assert(s.st.Get("timings", &stateTimings), IsNil)

	c.Assert(stateTimings, DeepEquals, []interface{}{
		map[string]interface{}{
			"level":    float64(0),
			"label":    "doing something-0",
			"duration": float64(1000000),
			"subject":  "task",
			"meta":     map[string]interface{}{"change-id": "12", "id": "3"},
			"nested": []interface{}{
				map[string]interface{}{
					"level":    float64(1),
					"label":    "nested measurement",
					"summary":  "...",
					"duration": float64(2000000)},
				map[string]interface{}{
					"level":    float64(2),
					"label":    "nested more",
					"summary":  "...",
					"duration": float64(3000000)},
			}},
		map[string]interface{}{
			"level":    float64(0),
			"label":    "doing something-1",
			"duration": float64(4000000),
			"subject":  "task",
			"meta":     map[string]interface{}{"change-id": "12", "id": "3"},
			"nested": []interface{}{
				map[string]interface{}{
					"level":    float64(1),
					"label":    "nested measurement",
					"summary":  "...",
					"duration": float64(5000000)},
				map[string]interface{}{
					"level":    float64(2),
					"label":    "nested more",
					"summary":  "...",
					"duration": float64(6000000)},
			}}})
}

func (s *timingsSuite) TestPurge(c *C) {
	// Create 10 timings
	for i := 0; i < 10; i++ {
		timing := timings.New(timings.Ensure, nil)
		meas := timing.Start(fmt.Sprintf("%d", i), "...")
		meas.Stop()
		timing.Save(s.st)
	}

	s.st.Lock()
	defer s.st.Unlock()

	var stateTimings []interface{}
	c.Assert(s.st.Get("timings", &stateTimings), IsNil)
	c.Check(stateTimings, HasLen, 10)

	// purge 3 times, consecutive purging does nothing if limit not reached; two most recent timings expected after purge.
	for i := 0; i < 3; i++ {
		s.st.Unlock()
		c.Assert(timings.Purge(s.st, 2), IsNil)
		s.st.Lock()

		c.Assert(s.st.Get("timings", &stateTimings), IsNil)
		c.Assert(stateTimings, DeepEquals, []interface{}{
			map[string]interface{}{
				"level":    float64(0),
				"label":    "8",
				"duration": float64(9000000),
				"subject":  "ensure"},
			map[string]interface{}{
				"level":    float64(0),
				"label":    "9",
				"duration": float64(10000000),
				"subject":  "ensure"}})
	}
}
