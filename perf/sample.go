// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package perf

import (
	"time"
)

// Sample contains timing information about an activity in snapd.
//
// Activities are described by free-form name and may be associated with any
// combination of task, change, snap name, manager name, etc.
type Sample struct {
	// ID contains the identifier of a sample.
	ID uint64 `json:"id"`
	// StartTime contains the time of the start of the activity.
	StartTime time.Time `json:"start-time"`
	// EndTime contains the time of the start of the activity.
	EndTime time.Time `json:"end-time"`
	// Kind is a coarse group of activities, see the constants below.
	Kind string `json:"kind"`
	// Summary contains the short textual description of the activity.
	Summary string `json:"summary"`

	// TaskID contains the identifier of the task, if any.
	TaskID string `json:"task-id,omitempty"`
	// ChangeID contains the identifier of the change, if any.
	ChangeID string `json:"change-id,omitempty"`
	// SnapName contains the identifier of a snap, if any.
	SnapName string `json:"snap-name,omitempty"`
	// ManagerID contains the identifier of a overlord manager, if any.
	ManagerID string `json:"manager,omitempty"`
	// MiscID contains manager-specific identifier, if any.
	// The interface manager stores the name of the security backend here.
	MiscID string `json:"misc-id,omitempty"`
}

const (
	// KindStartup groups activities performed during snapd startup.
	KindStartup = "startup"
	// KindAPI groups activities performed during API snapd request/response.
	KindAPI = "api"
	// KindTaskHandler groups activities performed by the snapd task manager.
	KindTaskHandler = "task-handler"
	// KindSnapRun groups activities performed during snap-{run,confine,exec} chain.
	KindSnapRun = "snap-run"
)

// Duration returns the duration of the activity.
func (s Sample) Duration() time.Duration {
	return s.EndTime.Sub(s.StartTime)
}
