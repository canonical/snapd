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
	"github.com/snapcore/snapd/overlord/state"
)

func NewForTask(task *state.Task) (*Timings, *Timing) {
	tags := map[string]string{"id": task.ID()}
	if chg := task.Change(); chg != nil {
		tags["change-id"] = chg.ID()
	}
	t := New(tags)
	return t, t.Start(task.Kind(), task.Summary())
}

func (t *Timing) Run(label, summary string, f func(nestedTiming *Timing)) {
	meas := t.Start(label, summary)
	f(meas)
	meas.Stop()
}
