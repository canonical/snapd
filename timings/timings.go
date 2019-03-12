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
	"time"
)

var timeNow = func() time.Time {
	return time.Now()
}

// Timings represents a tree of Timing measurements for a single execution of measured activity.
// A Timings tree object should be created at the beginning of the activity,
// followed by starting at least one Timing measurement, and then saved at the end of the activity.
//
// Calling Start on the Timings objects creates a Timing and starts new
// performance measurement. Measurement needs to be finished by calling Stop
// function on the Timing object.
// Nested measurements may be collected by calling Start on Timing objects. Similar
// to the above, nested measurements need to be finished by calling Stop on them.
//
// Typical usage:
//   troot := timings.New(map[string]string{"task-id": task.ID(), "change-id": task.Change().ID()})
//   t1 := troot.StartSpan("computation", "...")
//   ....
//   nestedTiming := t1.StartSpan("sub-computation", "...")
//   ....
//   nestedTiming.Stop()
//   t1.Stop()
//   troot.Save()
type Timings struct {
	tags    map[string]string
	timings []*Span
}

// Span represents a single performance measurement with optional nested measurements.
type Span struct {
	label, summary string
	start, stop    time.Time
	timings        []*Span
}

// New creates a Timings object. Tags provide extra information (such as "task-id" and "change-id")
// that can be used by the client when retrieving timings.
func New(tags map[string]string) *Timings {
	return &Timings{
		tags: tags,
	}
}

func startSpan(label, summary string) *Span {
	tmeas := &Span{
		label:   label,
		summary: summary,
		start:   timeNow(),
	}
	return tmeas
}

// Starts creates a Timing and initiates performance measurement.
// Measurement needs to be stopped by calling Stop on it.
func (t *Timings) StartSpan(label, summary string) *Span {
	tmeas := startSpan(label, summary)
	t.timings = append(t.timings, tmeas)
	return tmeas
}

// Start creates a new nested Timing and initiates performance measurement.
// Nested measurements need to be stopped by calling Stop on it.
func (t *Span) StartSpan(label, summary string) *Span {
	tmeas := startSpan(label, summary)
	t.timings = append(t.timings, tmeas)
	return tmeas
}

// Stops the measurement.
func (t *Span) Stop() {
	if t.stop.IsZero() {
		t.stop = timeNow()
	} // else - stopping already stopped timing is an error, but just ignore it
}
