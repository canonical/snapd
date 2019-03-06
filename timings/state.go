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
	timingJson
	MeasuredSubject string            `json:"subject"`
	Meta            map[string]string `json:"meta,omitempty"`
	NestedTimings   []*timingJson     `json:"nested,omitempty"`
}

var timeDuration = func(start, end time.Time) time.Duration {
	return end.Sub(start)
}

// flatten flattens nested measurements into a single list within rootTimingJson.NestedTImings.
func (t *Timings) flatten() interface{} {
	data := &rootTimingJson{
		timingJson: timingJson{
			Level:    0,
			Label:    t.m.label,
			Duration: timeDuration(t.m.start, t.m.stop),
		},
		MeasuredSubject: string(t.subject),
		Meta:            t.meta,
	}
	flattenRecursive(data, t.m.nested, 1)
	return data
}

func flattenRecursive(data *rootTimingJson, measures []*Measure, nestLevel int) {
	for _, m := range measures {
		data.NestedTimings = append(data.NestedTimings, &timingJson{
			Level:    nestLevel,
			Label:    m.label,
			Summary:  m.summary,
			Duration: timeDuration(m.start, m.stop),
		})
		if len(m.nested) > 0 {
			flattenRecursive(data, m.nested, nestLevel+1)
		}
	}
}

// Save appends Timings data to the "timings" list in the state.
func (t *Timings) Save(st *state.State) error {
	st.Lock()
	defer st.Unlock()

	var timings []*json.RawMessage
	if err := st.Get("timings", &timings); err != nil && err != state.ErrNoState {
		return err
	}

	serialized, err := json.Marshal(t.flatten())
	if err != nil {
		return fmt.Errorf("internal error: could not marshal value: %v", err)
	}
	entryJSON := json.RawMessage(serialized)

	timings = append(timings, &entryJSON)
	st.Set("timings", timings)
	return nil
}

// Purge removes excess timings from the "timings" list in the state (starting from the oldest),
// ensuring that up to maxTimings is kept.
func Purge(st *state.State, maxTimings int) error {
	st.Lock()
	defer st.Unlock()

	var timings []*json.RawMessage
	err := st.Get("timings", &timings)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}
	if len(timings) < maxTimings {
		return nil
	}

	timings = timings[(len(timings) - maxTimings):]
	st.Set("timings", timings)
	return nil
}
