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
	"fmt"
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
	theStore := snapstate.Store(st, nil)
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

type debugTimings struct {
	ChangeID string `json:"change-id"`
	// total duration of the activity - present for ensure and startup timings only
	TotalDuration  time.Duration             `json:"total-duration,omitempty"`
	EnsureTimings  []*timings.TimingJSON     `json:"ensure-timings,omitempty"`
	StartupTimings []*timings.TimingJSON     `json:"startup-timings,omitempty"`
	ChangeTimings  map[string]*changeTimings `json:"change-timings,omitempty"`
}

func collectChangeTimings(st *state.State, changeID string) (map[string]*changeTimings, error) {
	chg := st.Change(changeID)
	if chg == nil {
		return nil, fmt.Errorf("cannot find change: %v", changeID)
	}

	// collect "timings" for tasks of given change
	stateTimings, err := timings.Get(st, -1, func(tags map[string]string) bool { return tags["change-id"] == changeID })
	if err != nil {
		return nil, fmt.Errorf("cannot get timings of change %s: %v", changeID, err)
	}

	doingTimingsByTask := make(map[string][]*timings.TimingJSON)
	undoingTimingsByTask := make(map[string][]*timings.TimingJSON)
	for _, tm := range stateTimings {
		taskID := tm.Tags["task-id"]
		if status, ok := tm.Tags["task-status"]; ok {
			switch {
			case status == state.DoingStatus.String():
				doingTimingsByTask[taskID] = tm.NestedTimings
			case status == state.UndoingStatus.String():
				undoingTimingsByTask[taskID] = tm.NestedTimings
			default:
				return nil, fmt.Errorf("unexpected task status %q for timing of task %s", status, taskID)
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
	return m, nil
}

func collectEnsureTimings(st *state.State, ensureTag string, allEnsures bool) ([]*debugTimings, error) {
	ensures, err := timings.Get(st, -1, func(tags map[string]string) bool {
		return tags["ensure"] == ensureTag
	})
	if err != nil {
		return nil, fmt.Errorf("cannot get timings of ensure %s: %v", ensureTag, err)
	}
	if len(ensures) == 0 {
		return nil, fmt.Errorf("cannot find ensure: %v", ensureTag)
	}

	// If allEnsures is true, then report all activities of given ensure, otherwise just the latest
	first := len(ensures) - 1
	if allEnsures {
		first = 0
	}
	var responseData []*debugTimings
	var changeTimings map[string]*changeTimings
	for _, ensureTm := range ensures[first:] {
		ensureChangeID := ensureTm.Tags["change-id"]
		// change is optional for ensure timings
		if ensureChangeID != "" {
			// ignore an error when getting a change, it may no longer be present in the state
			changeTimings, _ = collectChangeTimings(st, ensureChangeID)
		}
		debugTm := &debugTimings{
			ChangeID:      ensureChangeID,
			ChangeTimings: changeTimings,
			EnsureTimings: ensureTm.NestedTimings,
			TotalDuration: ensureTm.Duration,
		}
		responseData = append(responseData, debugTm)
	}

	return responseData, nil
}

func collectStartupTimings(st *state.State, startupTag string, allStarts bool) ([]*debugTimings, error) {
	starts, err := timings.Get(st, -1, func(tags map[string]string) bool {
		return tags["startup"] == startupTag
	})
	if err != nil {
		return nil, fmt.Errorf("cannot get timings of startup %s: %v", startupTag, err)
	}
	if len(starts) == 0 {
		return nil, fmt.Errorf("cannot find startup: %v", startupTag)
	}

	// If allStarts is true, then report all activities of given startup, otherwise just the latest
	first := len(starts) - 1
	if allStarts {
		first = 0
	}
	var responseData []*debugTimings
	for _, startTm := range starts[first:] {
		debugTm := &debugTimings{
			StartupTimings: startTm.NestedTimings,
			TotalDuration:  startTm.Duration,
		}
		responseData = append(responseData, debugTm)
	}

	return responseData, nil
}

func getChangeTimings(st *state.State, changeID, ensureTag, startupTag string, all bool) Response {
	// If ensure tag was passed by the client, find its related changes;
	// we can have many ensure executions and their changes in the responseData array.
	if ensureTag != "" {
		responseData, err := collectEnsureTimings(st, ensureTag, all)
		if err != nil {
			return BadRequest(err.Error())
		}
		return SyncResponse(responseData, nil)
	}

	if startupTag != "" {
		responseData, err := collectStartupTimings(st, startupTag, all)
		if err != nil {
			return BadRequest(err.Error())
		}
		return SyncResponse(responseData, nil)
	}

	// timings for single change ID
	changeTimings, err := collectChangeTimings(st, changeID)
	if err != nil {
		return BadRequest(err.Error())
	}

	responseData := []*debugTimings{
		{
			ChangeID:      changeID,
			ChangeTimings: changeTimings,
		},
	}
	return SyncResponse(responseData, nil)
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
		ensureTag := query.Get("ensure")
		startupTag := query.Get("startup")
		all := query.Get("all")
		return getChangeTimings(st, chgID, ensureTag, startupTag, all == "true")
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
