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

package timings

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
)

// TimingJSON and rootTimingsJSON aid in marshalling of flattened timings into state.
type TimingJSON struct {
	Level    int           `json:"level,omitempty"`
	Label    string        `json:"label,omitempty"`
	Summary  string        `json:"summary,omitempty"`
	Duration time.Duration `json:"duration"`
}

type rootTimingsJSON struct {
	Tags          map[string]string `json:"tags,omitempty"`
	NestedTimings []*TimingJSON     `json:"timings,omitempty"`
	// start time of the first timing
	StartTime time.Time `json:"start-time"`
	// the most recent stop time of all timings
	StopTime time.Time `json:"stop-time"`
}

// TimingsInfo holds a set of related nested timings and the tags set when they were captured.
type TimingsInfo struct {
	Tags          map[string]string
	NestedTimings []*TimingJSON
	Duration      time.Duration
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
	var hasChangeID, hasTaskID bool
	if t.tags != nil {
		_, hasChangeID = t.tags["change-id"]
		_, hasTaskID = t.tags["task-id"]
	}

	// ensure timings which created a change, have the corresponding
	// change-id tag, but no task-id
	isEnsureWithChange := hasChangeID && !hasTaskID

	if len(t.timings) == 0 && !isEnsureWithChange {
		return nil
	}

	data := &rootTimingsJSON{
		Tags: t.tags,
	}
	if len(t.timings) > 0 {
		var maxStopTime time.Time
		flattenRecursive(data, t.timings, 0, &maxStopTime)
		if len(data.NestedTimings) == 0 && !hasChangeID {
			return nil
		}
		data.StartTime = t.timings[0].start
		data.StopTime = maxStopTime
	}

	return data
}

func flattenRecursive(data *rootTimingsJSON, timings []*Span, nestLevel int, maxStopTime *time.Time) {
	for _, tm := range timings {
		dur := timeDuration(tm.start, tm.stop)
		if dur >= DurationThreshold {
			data.NestedTimings = append(data.NestedTimings, &TimingJSON{
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

// A GetSaver helps storing Timings (ignoring their details).
type GetSaver interface {
	// GetMaybeTimings gets the saved timings.
	// It will not return an error if none were saved yet.
	GetMaybeTimings(timings interface{}) error
	// SaveTimings saves the given timings.
	SaveTimings(timings interface{})
}

// Save appends Timings data to a timings list in the GetSaver (usually
// state.State) and purges old timings, ensuring that up to MaxTimings
// are kept. Timings are only stored if their duration is greater than
// or equal to DurationThreshold.  If GetSaver is a state.State, it's
// responsibility of the caller to lock the state before calling this
// function.
func (t *Timings) Save(s GetSaver) {
	var stateTimings []*json.RawMessage
	mylog.Check(s.GetMaybeTimings(&stateTimings))

	data := t.flatten()
	if data == nil {
		return
	}
	serialized := mylog.Check2(json.Marshal(data))

	entryJSON := json.RawMessage(serialized)

	stateTimings = append(stateTimings, &entryJSON)
	if len(stateTimings) > MaxTimings {
		stateTimings = stateTimings[len(stateTimings)-MaxTimings:]
	}
	s.SaveTimings(stateTimings)
}

// Get returns timings for which filter predicate is true and filters
// out nested timings whose level is greater than maxLevel.
// Negative maxLevel value disables filtering by level.
// If GetSaver is a state.State, it's responsibility of the caller to
// lock the state before calling this function.
func Get(s GetSaver, maxLevel int, filter func(tags map[string]string) bool) ([]*TimingsInfo, error) {
	var stateTimings []rootTimingsJSON
	mylog.Check(s.GetMaybeTimings(&stateTimings))

	var result []*TimingsInfo
	for _, tm := range stateTimings {
		if !filter(tm.Tags) {
			continue
		}
		res := &TimingsInfo{
			Tags:     tm.Tags,
			Duration: timeDuration(tm.StartTime, tm.StopTime),
		}
		// negative maxLevel means no level filtering, take all nested timings
		if maxLevel < 0 {
			res.NestedTimings = tm.NestedTimings // there is always at least one nested timing - guaranteed by Save()
			result = append(result, res)
			continue
		}
		for _, nested := range tm.NestedTimings {
			if nested.Level <= maxLevel {
				res.NestedTimings = append(res.NestedTimings, nested)
			}
		}
		// maxLevel is >=0 here, so we always have at least level 0 timings when the loop finishes
		result = append(result, res)
	}
	return result, nil
}
