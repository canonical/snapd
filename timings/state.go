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

package timings

import (
	"encoding/json"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
)

// timingJson and rootTimingsJson aid in marshalling of flattened timings into state.
type timingJson struct {
	Level    int           `json:"level,omitempty"`
	Label    string        `json:"label,omitempty"`
	Summary  string        `json:"summary,omitempty"`
	Duration time.Duration `json:"duration"`
}

type rootTimingsJson struct {
	Tags          map[string]string `json:"tags,omitempty"`
	NestedTimings []*timingJson     `json:"timings,omitempty"`
	// start time of the first timing
	StartTime time.Time `json:"start-time"`
	// the most recent stop time of all timings
	StopTime time.Time `json:"stop-time"`
}

// Maximum number of timings to keep in state. It can be changed only while holding state lock.
var MaxTimings = 100

// Duration threshold - timings below the threshold will not be saved in the state.
// It can be changed only while holding state lock.
var DurationThreshold = 5 * time.Millisecond

var timeDuration = func(start, end time.Time) time.Duration {
	return end.Sub(start)
}

// flatten flattens nested measurements into a single list within rootTimingJson.NestedTimings
// and calculates total duration.
func (t *Timings) flatten() interface{} {
	data := &rootTimingsJson{
		Tags: t.tags,
	}
	var maxStopTime time.Time
	if len(t.timings) > 0 {
		flattenRecursive(data, t.timings, 0, &maxStopTime)
		if len(data.NestedTimings) == 0 {
			return nil
		}
		data.StartTime = t.timings[0].start
		data.StopTime = maxStopTime
	}
	return data
}

func flattenRecursive(data *rootTimingsJson, timings []*Span, nestLevel int, maxStopTime *time.Time) {
	for _, tm := range timings {
		dur := timeDuration(tm.start, tm.stop)
		if dur >= DurationThreshold {
			data.NestedTimings = append(data.NestedTimings, &timingJson{
				Level:    nestLevel,
				Label:    tm.label,
				Summary:  tm.summary,
				Duration: dur,
			})
		}
		if tm.stop.After(*maxStopTime) {
			*maxStopTime = tm.stop
		}
		if len(tm.timings) > 0 {
			flattenRecursive(data, tm.timings, nestLevel+1, maxStopTime)
		}
	}
}

// Save appends Timings data to the "timings" list in the state and purges old timings, ensuring
// that up to MaxTimings are kept. Timings are only stored in state if their duration is greater
// than or equal to DurationThreshold.
// It's responsibility of the caller to lock the state before calling this function.
func (t *Timings) Save(st *state.State) {
	var stateTimings []*json.RawMessage
	if err := st.Get("timings", &stateTimings); err != nil && err != state.ErrNoState {
		logger.Noticef("could not get timings data from the state: %v", err)
		return
	}

	data := t.flatten()
	if data == nil {
		return
	}
	serialized, err := json.Marshal(data)
	if err != nil {
		logger.Noticef("could not marshal timings: %v", err)
		return
	}
	entryJSON := json.RawMessage(serialized)

	stateTimings = append(stateTimings, &entryJSON)
	if len(stateTimings) > MaxTimings {
		stateTimings = stateTimings[len(stateTimings)-MaxTimings:]
	}
	st.Set("timings", stateTimings)
}
