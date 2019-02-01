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

package measure

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Measure is a single timing measurement for a given "action"
//
type Measure struct {
	// TODO: we may want to add more fancy things like json
	//       output, tags etc but YAGNI for now
	action     string
	start, end time.Time
}

// New creates a new timing measure for the given action
func New(action string) *Measure {
	return &Measure{action: action, start: time.Now()}
}

// Done indicates that the given measure is finished
func (m *Measure) Done() {
	if m.end.IsZero() {
		m.end = time.Now()
		addMeasure(m)
	}
}

// maximum amount of measures to keep in memory
const maxSize = 100

// use something fancy like a ringbuffer here, see
// https://github.com/zyga/snapd/commit/ce3f289c86b783486349b21c386f976133ff69aa
// for some nice work in this area
var allMeasures []string

var mu sync.Mutex

// addMeasure is an internal helper
func addMeasure(m *Measure) {
	mu.Lock()
	defer mu.Unlock()

	msg := fmt.Sprintf("%s took %v", m.action, m.end.Sub(m.start))
	allMeasures = append(allMeasures, msg)
	if len(allMeasures) > maxSize {
		allMeasures = allMeasures[len(allMeasures)-maxSize:]
	}
}

func WriteAll(w io.Writer) {
	for _, m := range allMeasures {
		// FIXME: add timestaamps
		fmt.Fprintln(w, m)
	}
}
