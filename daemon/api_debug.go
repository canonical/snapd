// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
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

package daemon

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/timings"
)

var debugCmd = &Command{
	Path:   "/v2/debug",
	UserOK: true,
	GET:    getDebug,
	POST:   postDebug,
}

type debugAction struct {
	Action  string `json:"action"`
	Message string `json:"message"`
	Params  struct {
		ChgID string `json:"chg-id"`
	} `json:"params"`
}

type ConnectivityStatus struct {
	Connectivity bool     `json:"connectivity"`
	Unreachable  []string `json:"unreachable,omitempty"`
}

func getBaseDeclaration(st *state.State) Response {
	bd, err := assertstate.BaseDeclaration(st)
	if err != nil {
		return InternalError("cannot get base declaration: %s", err)
	}
	return SyncResponse(map[string]interface{}{
		"base-declaration": string(asserts.Encode(bd)),
	}, nil)
}

func checkConnectivity(st *state.State) Response {
	theStore := snapstate.Store(st)
	st.Unlock()
	defer st.Lock()
	checkResult, err := theStore.ConnectivityCheck()
	if err != nil {
		return InternalError("cannot run connectivity check: %v", err)
	}
	status := ConnectivityStatus{Connectivity: true}
	for host, reachable := range checkResult {
		if !reachable {
			status.Connectivity = false
			status.Unreachable = append(status.Unreachable, host)
		}
	}
	sort.Strings(status.Unreachable)

	return SyncResponse(status, nil)
}

type changeTimings struct {
	DoingTime      time.Duration         `json:"doing-time,omitempty"`
	UndoingTime    time.Duration         `json:"undoing-time,omitempty"`
	DoingTimings   []*timings.TimingJSON `json:"doing-timings,omitempty"`
	UndoingTimings []*timings.TimingJSON `json:"undoing-timings,omitempty"`
}

func getChangeTimings(st *state.State, changeID string) Response {
	chg := st.Change(changeID)
	if chg == nil {
		return BadRequest("cannot find change: %v", changeID)
	}

	doingTimingsByTask := make(map[string][]*timings.TimingJSON)
	undoingTimingsByTask := make(map[string][]*timings.TimingJSON)

	// collect "timings" for tasks of given change
	stateTimings, err := timings.Get(st, -1, func(tags map[string]string) bool { return tags["change-id"] == changeID })
	if err != nil {
		return InternalError("cannot get timings of change %s: %v", changeID, err)
	}
	for _, tm := range stateTimings {
		taskID := tm.Tags["task-id"]
		if status, ok := tm.Tags["task-status"]; ok {
			switch {
			case status == state.DoingStatus.String():
				doingTimingsByTask[taskID] = tm.NestedTimings
			case status == state.UndoingStatus.String():
				undoingTimingsByTask[taskID] = tm.NestedTimings
			default:
				return InternalError("unexpected task status %q for timing of task %s", status, taskID)
			}
		}
	}

	m := map[string]*changeTimings{}
	for _, t := range chg.Tasks() {
		m[t.ID()] = &changeTimings{
			DoingTime:      t.DoingTime(),
			UndoingTime:    t.UndoingTime(),
			DoingTimings:   doingTimingsByTask[t.ID()],
			UndoingTimings: undoingTimingsByTask[t.ID()],
		}
	}
	return SyncResponse(m, nil)
}

func getDebug(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	aspect := query.Get("aspect")
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	switch aspect {
	case "base-declaration":
		return getBaseDeclaration(st)
	case "connectivity":
		return checkConnectivity(st)
	case "model":
		model, err := c.d.overlord.DeviceManager().Model()
		if err != nil {
			return InternalError("cannot get model: %v", err)
		}
		return SyncResponse(map[string]interface{}{
			"model": string(asserts.Encode(model)),
		}, nil)
	case "change-timings":
		chgID := query.Get("change-id")
		return getChangeTimings(st, chgID)
	default:
		return BadRequest("unknown debug aspect %q", aspect)
	}
}

func postDebug(c *Command, r *http.Request, user *auth.UserState) Response {
	var a debugAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&a); err != nil {
		return BadRequest("cannot decode request body into a debug action: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	switch a.Action {
	case "add-warning":
		st.Warnf("%v", a.Message)
		return SyncResponse(true, nil)
	case "unshow-warnings":
		st.UnshowAllWarnings()
		return SyncResponse(true, nil)
	case "ensure-state-soon":
		ensureStateSoon(st)
		return SyncResponse(true, nil)
	case "get-base-declaration":
		return getBaseDeclaration(st)
	case "can-manage-refreshes":
		return SyncResponse(devicestate.CanManageRefreshes(st), nil)
	case "connectivity":
		return checkConnectivity(st)
	default:
		return BadRequest("unknown debug action: %v", a.Action)
	}
}
