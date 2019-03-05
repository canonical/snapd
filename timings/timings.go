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
	"fmt"
	"time"
)

// Subject is a type of the activity being measured; available subjects are defined below.
type Subject string

const (
	// execution of ensure loop
	Ensure Subject = "ensure"
	// acitivity related to task
	Task = "task"
)

// Timings represents a tree of measures concerning given Subject (activity).
// Calling Start on the Timings objects returns a root Measure and starts a new performance measurement. Measurement needs to
// be finished by calling Stop function on the Measure object.
// Nested measures may be collected by calling StartNested on Measure objects. Similiar to the above, nested measurements need
// to be finished by calling Stop on them.
//
// Typical usagage:
//   timing := timings.New(timings.Ensure)
//   measure := timing.Start("computation", "...")
//   ....
//   nested := measure.StartNested("sub-computation", "...")
//   ....
//   nested.Stop()
//   measure.Stop()
type Timings struct {
	subject Subject
	meta    map[string]string

	// root measurement
	m *Measure
}

// Measure represents a single measure with optional nested measurements.
type Measure struct {
	label, summary string
	start, stop    time.Time
	nested         []*Measure
}

// New creates a Timings object.
func New(subject Subject, metaInfo map[string]string) *Timings {
	return &Timings{
		subject: subject,
		meta:    metaInfo,
	}
}

// Starts creates and returns root Measure and initiates performance measurement.
// Measurement needs to be stopped by calling Stop on it.
func (t *Timings) Start(label, summary string) *Measure {
	if t.m != nil {
		panic(fmt.Sprintf("timing %q already started", label))
	}
	t.m = &Measure{
		label:   label,
		summary: summary,
		start:   time.Now(),
	}
	return t.m
}

// StartNested creates a new nested Measure and initiates performance measurement.
// Nested measure needs to be stopped by calling Stop on it.
func (m *Measure) StartNested(label, summary string) *Measure {
	meas := &Measure{
		label:   label,
		summary: summary,
		start:   time.Now(),
	}
	m.nested = append(m.nested, meas)
	return meas
}

// Stops the measurement.
func (m *Measure) Stop() {
	if !m.stop.IsZero() {
		panic(fmt.Sprintf("measure %q already stopped", m.label))
	}
	m.stop = time.Now()
}
