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
	"fmt"
	"time"

	"github.com/snapcore/snapd/overlord/state"
)

// timingJson and rootTimingJson aid in marshalling of flattened timings into state.
type timingJson struct {
	Level    int           `json:"level"`
	Label    string        `json:"label,omitempty"`
	Summary  string        `json:"summary,omitempty"`
	Duration time.Duration `json:"duration"`
}

type rootTimingJson struct {
	Tags          map[string]string `json:"tags,omitempty"`
	NestedTimings []*timingJson     `json:"timings,omitempty"`
	// the duration between start time of the first timing and the most recent stop of any timing within the tree
	TotalDuration time.Duration `json:"total-duration"`
}

// Maximum number of timings to keep in state. It can be changed only while holding state lock.
var MaxTimings = 100

var timeDuration = func(start, end time.Time) time.Duration {
	return end.Sub(start)
}

// flatten flattens nested measurements into a single list within rootTimingJson.NestedTimings
// and calculates total duration.
func (t *Timings) flatten() interface{} {
	data := &rootTimingJson{
		Tags: t.tags,
	}
	var maxStopTime time.Time
	if len(t.timings) > 0 {
		flattenRecursive(data, t.timings, 0, &maxStopTime)
		data.TotalDuration = timeDuration(t.timings[0].start, maxStopTime)
	}
	return data
}

func flattenRecursive(data *rootTimingJson, timings []*Timing, nestLevel int, maxStopTime *time.Time) {
	for _, tm := range timings {
		data.NestedTimings = append(data.NestedTimings, &timingJson{
			Level:    nestLevel,
			Label:    tm.label,
			Summary:  tm.summary,
			Duration: timeDuration(tm.start, tm.stop),
		})
		if tm.stop.After(*maxStopTime) {
			*maxStopTime = tm.stop
		}
		if len(tm.timings) > 0 {
			flattenRecursive(data, tm.timings, nestLevel+1, maxStopTime)
		}
	}
}

// Save appends Timings data to the "timings" list in the state and purges old timings, ensuring
// that up to MaxTimings are kept.
// It's responsibility of the caller to lock the state before calling this function.
func (t *Timings) Save(st *state.State) error {
	var stateTimings []*json.RawMessage
	if err := st.Get("timings", &stateTimings); err != nil && err != state.ErrNoState {
		return err
	}

	serialized, err := json.Marshal(t.flatten())
	if err != nil {
		return fmt.Errorf("internal error: could not marshal timings: %v", err)
	}
	entryJSON := json.RawMessage(serialized)

	stateTimings = append(stateTimings, &entryJSON)
	if len(stateTimings) > MaxTimings {
		stateTimings = stateTimings[len(stateTimings)-MaxTimings:]
	}
	st.Set("timings", stateTimings)
	return nil
}
