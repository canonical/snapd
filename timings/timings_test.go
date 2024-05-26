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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func TestTimings(t *testing.T) { TestingT(t) }

type timingsSuite struct {
	testutil.BaseTest
	st       *state.State
	duration time.Duration
	fakeTime time.Time
}

var _ = Suite(&timingsSuite{})

func (s *timingsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.st = state.New(nil)
	s.duration = 0

	s.mockTimeNow(c)
	s.mockDurationThreshold(0)
}

func (s *timingsSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *timingsSuite) mockDuration(c *C) {
	// Increase duration by 1 millisecond on each call
	s.BaseTest.AddCleanup(timings.MockTimeDuration(func(start, end time.Time) time.Duration {
		c.Check(start.Before(end), Equals, true)
		s.duration += time.Millisecond
		return s.duration
	}))
}

func (s *timingsSuite) mockDurationThreshold(threshold time.Duration) {
	oldDurationThreshold := timings.DurationThreshold
	timings.DurationThreshold = threshold
	restore := func() {
		timings.DurationThreshold = oldDurationThreshold
	}
	s.AddCleanup(restore)
}

func (s *timingsSuite) mockTimeNow(c *C) {
	t := mylog.Check2(time.Parse(time.RFC3339, "2019-03-11T09:01:00.0Z"))

	s.fakeTime = t
	// Increase fakeTime by 1 millisecond on each call, and report it as current time
	s.BaseTest.AddCleanup(timings.MockTimeNow(func() time.Time {
		s.fakeTime = s.fakeTime.Add(time.Millisecond)
		return s.fakeTime
	}))
}

func (s *timingsSuite) TestSave(c *C) {
	s.mockDuration(c)

	s.st.Lock()
	defer s.st.Unlock()

	// two timings, with 2 nested measures
	for i := 0; i < 2; i++ {
		timing := timings.New(map[string]string{"task": "3"})
		timing.AddTag("change", "12")
		meas := timing.StartSpan(fmt.Sprintf("doing something-%d", i), "...")
		nested := meas.StartSpan("nested measurement", "...")
		var called bool
		timings.Run(nested, "nested more", "...", func(span timings.Measurer) {
			called = true
		})
		c.Check(called, Equals, true)
		nested.Stop()
		meas.Stop()
		timing.Save(s.st)
	}

	var stateTimings []interface{}
	c.Assert(s.st.Get("timings", &stateTimings), IsNil)

	c.Assert(stateTimings, DeepEquals, []interface{}{
		map[string]interface{}{
			"tags":       map[string]interface{}{"change": "12", "task": "3"},
			"start-time": "2019-03-11T09:01:00.001Z",
			"stop-time":  "2019-03-11T09:01:00.006Z",
			"timings": []interface{}{
				map[string]interface{}{
					"label":    "doing something-0",
					"summary":  "...",
					"duration": float64(1000000),
				},
				map[string]interface{}{
					"level":    float64(1),
					"label":    "nested measurement",
					"summary":  "...",
					"duration": float64(2000000),
				},
				map[string]interface{}{
					"level":    float64(2),
					"label":    "nested more",
					"summary":  "...",
					"duration": float64(3000000),
				},
			},
		},
		map[string]interface{}{
			"tags":       map[string]interface{}{"change": "12", "task": "3"},
			"start-time": "2019-03-11T09:01:00.007Z",
			"stop-time":  "2019-03-11T09:01:00.012Z",
			"timings": []interface{}{
				map[string]interface{}{
					"label":    "doing something-1",
					"summary":  "...",
					"duration": float64(4000000),
				},
				map[string]interface{}{
					"level":    float64(1),
					"label":    "nested measurement",
					"summary":  "...",
					"duration": float64(5000000),
				},
				map[string]interface{}{
					"level":    float64(2),
					"label":    "nested more",
					"summary":  "...",
					"duration": float64(6000000),
				},
			},
		},
	})
}

func (s *timingsSuite) TestSaveNoTimings(c *C) {
	s.mockDuration(c)

	s.st.Lock()
	defer s.st.Unlock()

	timing := timings.New(nil)
	timing.Save(s.st)

	var stateTimings []interface{}
	c.Assert(s.st.Get("timings", &stateTimings), testutil.ErrorIs, state.ErrNoState)
}

func (s *timingsSuite) TestDuration(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	timing := timings.New(nil)
	meas := timing.StartSpan("foo", "...")                   // time now = 1
	nested := meas.StartSpan("nested", "...")                // time now = 2
	nested.Stop()                                            // time now = 3 -> duration = 1
	nestedSibling := meas.StartSpan("nested sibling", "...") // time now = 4
	nestedSibling.Stop()                                     // time now = 5 -> duration = 1
	meas.Stop()
	timing.Save(s.st)

	var stateTimings []interface{}
	c.Assert(s.st.Get("timings", &stateTimings), IsNil)

	c.Assert(stateTimings, DeepEquals, []interface{}{
		map[string]interface{}{
			"start-time": "2019-03-11T09:01:00.001Z",
			"stop-time":  "2019-03-11T09:01:00.006Z",
			"timings": []interface{}{
				map[string]interface{}{
					"label":    "foo",
					"summary":  "...",
					"duration": float64(5000000),
				},
				map[string]interface{}{
					"level":    float64(1),
					"label":    "nested",
					"summary":  "...",
					"duration": float64(1000000),
				},
				map[string]interface{}{
					"level":    float64(1),
					"label":    "nested sibling",
					"summary":  "...",
					"duration": float64(1000000),
				},
			},
		},
	})
}

func (s *timingsSuite) testDurationThreshold(c *C, threshold time.Duration, expected interface{}) {
	s.mockDurationThreshold(threshold)

	s.st.Lock()
	defer s.st.Unlock()

	timing := timings.New(nil)
	meas := timing.StartSpan("main", "...")
	nested := meas.StartSpan("nested", "...")
	nestedMore := nested.StartSpan("nested more", "...")
	nestedMore.Stop()
	nested.Stop()

	meas.Stop()
	timing.Save(s.st)

	var stateTimings []interface{}
	if expected == nil {
		c.Assert(s.st.Get("timings", &stateTimings), testutil.ErrorIs, state.ErrNoState)
		c.Assert(stateTimings, IsNil)
		return
	}
	c.Assert(s.st.Get("timings", &stateTimings), IsNil)
	c.Check(stateTimings, DeepEquals, expected)
}

func (s *timingsSuite) TestDurationThresholdAll(c *C) {
	s.testDurationThreshold(c, 0, []interface{}{
		map[string]interface{}{
			"start-time": "2019-03-11T09:01:00.001Z",
			"stop-time":  "2019-03-11T09:01:00.006Z",
			"timings": []interface{}{
				map[string]interface{}{
					"label":    "main",
					"summary":  "...",
					"duration": float64(5000000),
				},
				map[string]interface{}{
					"level":    float64(1),
					"label":    "nested",
					"summary":  "...",
					"duration": float64(3000000),
				},
				map[string]interface{}{
					"level":    float64(2),
					"label":    "nested more",
					"summary":  "...",
					"duration": float64(1000000),
				},
			},
		},
	})
}

func (s *timingsSuite) TestDurationThreshold(c *C) {
	s.testDurationThreshold(c, 3000000, []interface{}{
		map[string]interface{}{
			"start-time": "2019-03-11T09:01:00.001Z",
			"stop-time":  "2019-03-11T09:01:00.006Z",
			"timings": []interface{}{
				map[string]interface{}{
					"label":    "main",
					"summary":  "...",
					"duration": float64(5000000),
				},
				map[string]interface{}{
					"level":    float64(1),
					"label":    "nested",
					"summary":  "...",
					"duration": float64(3000000),
				},
			},
		},
	})
}

func (s *timingsSuite) TestDurationThresholdRootOnly(c *C) {
	s.testDurationThreshold(c, 4000000, []interface{}{
		map[string]interface{}{
			"start-time": "2019-03-11T09:01:00.001Z",
			"stop-time":  "2019-03-11T09:01:00.006Z",
			"timings": []interface{}{
				map[string]interface{}{
					"label":    "main",
					"summary":  "...",
					"duration": float64(5000000),
				},
			},
		},
	})
}

func (s *timingsSuite) TestDurationThresholdNone(c *C) {
	s.testDurationThreshold(c, time.Hour, nil)
}

func (s *timingsSuite) TestPurgeOnSave(c *C) {
	oldMaxTimings := timings.MaxTimings
	timings.MaxTimings = 3
	defer func() {
		timings.MaxTimings = oldMaxTimings
	}()

	s.st.Lock()
	defer s.st.Unlock()

	// Create lots of timings
	for i := 0; i < 10; i++ {
		t := timings.New(map[string]string{"number": fmt.Sprintf("%d", i)})
		m := t.StartSpan("...", "...")
		m.Stop()
		t.Save(s.st)
	}

	var stateTimings []interface{}
	c.Assert(s.st.Get("timings", &stateTimings), IsNil)

	// excess timings got dropped
	c.Assert(stateTimings, HasLen, 3)
	c.Check(stateTimings[0].(map[string]interface{})["tags"], DeepEquals, map[string]interface{}{"number": "7"})
	c.Check(stateTimings[1].(map[string]interface{})["tags"], DeepEquals, map[string]interface{}{"number": "8"})
	c.Check(stateTimings[2].(map[string]interface{})["tags"], DeepEquals, map[string]interface{}{"number": "9"})
}

func (s *timingsSuite) TestGet(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// three timings, with 2 nested measures
	for i := 0; i < 3; i++ {
		timing := timings.New(map[string]string{"foo": fmt.Sprintf("%d", i)})
		meas := timing.StartSpan(fmt.Sprintf("doing something-%d", i), "...")
		nested := meas.StartSpan("nested measurement", "...")
		nested.Stop()
		meas.Stop()
		timing.Save(s.st)
	}

	none := mylog.Check2(timings.Get(s.st, 999, func(tags map[string]string) bool { return false }))

	c.Check(none, HasLen, 0)

	tm := mylog.Check2(timings.Get(s.st, -1, func(tags map[string]string) bool {
		return tags["foo"] == "1"
	}))

	c.Check(tm, DeepEquals, []*timings.TimingsInfo{
		{
			Tags:     map[string]string{"foo": "1"},
			Duration: 3000000,
			NestedTimings: []*timings.TimingJSON{
				{Level: 0, Label: "doing something-1", Summary: "...", Duration: 3000000},
				{Level: 1, Label: "nested measurement", Summary: "...", Duration: 1000000},
			},
		},
	})

	tmOnlyLevel0 := mylog.Check2(timings.Get(s.st, 0, func(tags map[string]string) bool { return true }))

	c.Check(tmOnlyLevel0, DeepEquals, []*timings.TimingsInfo{
		{
			Tags:     map[string]string{"foo": "0"},
			Duration: 3000000,
			NestedTimings: []*timings.TimingJSON{
				{Level: 0, Label: "doing something-0", Summary: "...", Duration: 3000000},
			},
		},
		{
			Tags:     map[string]string{"foo": "1"},
			Duration: 3000000,
			NestedTimings: []*timings.TimingJSON{
				{Level: 0, Label: "doing something-1", Summary: "...", Duration: 3000000},
			},
		},
		{
			Tags:     map[string]string{"foo": "2"},
			Duration: 3000000,
			NestedTimings: []*timings.TimingJSON{
				{Level: 0, Label: "doing something-2", Summary: "...", Duration: 3000000},
			},
		},
	})
}
