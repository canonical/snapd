// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2023 Canonical Ltd
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
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/timings"
)

var debugCmd = &Command{
	Path:        "/v2/debug",
	GET:         getDebug,
	POST:        postDebug,
	ReadAccess:  openAccess{},
	WriteAccess: rootAccess{},
}

type debugAction struct {
	Action  string `json:"action"`
	Message string `json:"message"`
	Params  struct {
		ChgID string `json:"chg-id"`

		RecoverySystemLabel string `json:"recovery-system-label"`
	} `json:"params"`
	Snaps []string `json:"snaps"`
}

type connectivityStatus struct {
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
	})

}

func checkConnectivity(st *state.State) Response {
	theStore := snapstate.Store(st, nil)
	st.Unlock()
	defer st.Lock()
	checkResult, err := theStore.ConnectivityCheck()
	if err != nil {
		return InternalError("cannot run connectivity check: %v", err)
	}
	status := connectivityStatus{Connectivity: true}
	for host, reachable := range checkResult {
		if !reachable {
			status.Connectivity = false
			status.Unreachable = append(status.Unreachable, host)
		}
	}
	sort.Strings(status.Unreachable)

	return SyncResponse(status)
}

type changeTimings struct {
	Status         string                `json:"status,omitempty"`
	Kind           string                `json:"kind,omitempty"`
	Summary        string                `json:"summary,omitempty"`
	Lane           int                   `json:"lane,omitempty"`
	ReadyTime      time.Time             `json:"ready-time,omitempty"`
	DoingTime      time.Duration         `json:"doing-time,omitempty"`
	UndoingTime    time.Duration         `json:"undoing-time,omitempty"`
	DoingTimings   []*timings.TimingJSON `json:"doing-timings,omitempty"`
	UndoingTimings []*timings.TimingJSON `json:"undoing-timings,omitempty"`
}

type debugTimings struct {
	ChangeID string `json:"change-id"`
	// total duration of the activity - present for ensure and startup timings only
	TotalDuration  time.Duration         `json:"total-duration,omitempty"`
	EnsureTimings  []*timings.TimingJSON `json:"ensure-timings,omitempty"`
	StartupTimings []*timings.TimingJSON `json:"startup-timings,omitempty"`
	// ChangeTimings are indexed by task id
	ChangeTimings map[string]*changeTimings `json:"change-timings,omitempty"`
}

// minLane determines the lowest lane number for the task
func minLane(t *state.Task) int {
	lanes := t.Lanes()
	minLane := lanes[0]
	for _, l := range lanes[1:] {
		if l < minLane {
			minLane = l
		}
	}
	return minLane
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
			Kind:           t.Kind(),
			Status:         t.Status().String(),
			Summary:        t.Summary(),
			Lane:           minLane(t),
			ReadyTime:      t.ReadyTime(),
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
		return SyncResponse(responseData)
	}

	if startupTag != "" {
		responseData, err := collectStartupTimings(st, startupTag, all)
		if err != nil {
			return BadRequest(err.Error())
		}
		return SyncResponse(responseData)
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
	return SyncResponse(responseData)
}

func getGadgetDiskMapping(st *state.State) Response {
	deviceCtx, err := devicestate.DeviceCtx(st, nil, nil)
	if err != nil {
		return InternalError("cannot get device context: %v", err)
	}
	gadgetInfo, err := snapstate.GadgetInfo(st, deviceCtx)
	if err != nil {
		return InternalError("cannot get gadget info: %v", err)
	}
	gadgetDir := gadgetInfo.MountDir()

	mod := deviceCtx.Model()

	info, err := gadget.ReadInfoAndValidate(gadgetDir, mod, nil)
	if err != nil {
		return InternalError("cannot get all disk volume device traits: cannot read gadget: %v", err)
	}

	// TODO: allow passing in encrypted options info here

	// allow implicit system-data on pre-uc20 only
	optsMap := map[string]*gadget.DiskVolumeValidationOptions{}
	for vol := range info.Volumes {
		optsMap[vol] = &gadget.DiskVolumeValidationOptions{
			AllowImplicitSystemData: mod.Grade() == asserts.ModelGradeUnset,
		}
	}

	res, err := gadget.AllDiskVolumeDeviceTraits(info.Volumes, optsMap)
	if err != nil {
		return InternalError("cannot get all disk volume device traits: %v", err)
	}

	return SyncResponse(res)
}

func getDisks(st *state.State) Response {

	disks, err := disks.AllPhysicalDisks()
	if err != nil {
		return InternalError("cannot get all physical disks: %v", err)
	}
	vols := make([]*gadget.OnDiskVolume, 0, len(disks))
	for _, d := range disks {
		vol, err := gadget.OnDiskVolumeFromDisk(d)
		if err != nil {
			return InternalError("cannot get on disk volume for device %s: %v", d.KernelDeviceNode(), err)
		}
		vols = append(vols, vol)
	}

	return SyncResponse(vols)
}

func createRecovery(st *state.State, label string) Response {
	if label == "" {
		return BadRequest("cannot create a recovery system with no label")
	}
	chg, err := devicestate.CreateRecoverySystem(st, label, devicestate.CreateRecoverySystemOptions{
		TestSystem: true,
	})
	if err != nil {
		return InternalError("cannot create recovery system %q: %v", label, err)
	}
	ensureStateSoon(st)
	return AsyncResponse(nil, chg.ID())
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
		})

	case "change-timings":
		chgID := query.Get("change-id")
		ensureTag := query.Get("ensure")
		startupTag := query.Get("startup")
		all := query.Get("all")
		return getChangeTimings(st, chgID, ensureTag, startupTag, all == "true")
	case "seeding":
		return getSeedingInfo(st)
	case "gadget-disk-mapping":
		return getGadgetDiskMapping(st)
	case "disks":
		return getDisks(st)
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
		return SyncResponse(true)
	case "unshow-warnings":
		st.UnshowAllWarnings()
		return SyncResponse(true)
	case "ensure-state-soon":
		ensureStateSoon(st)
		return SyncResponse(true)
	case "can-manage-refreshes":
		return SyncResponse(devicestate.CanManageRefreshes(st))
	case "prune":
		opTime, err := c.d.overlord.DeviceManager().StartOfOperationTime()
		if err != nil {
			return BadRequest("cannot get start of operation time: %s", err)
		}
		st.Prune(opTime, 0, 0, 0)
		return SyncResponse(true)
	case "stacktraces":
		return getStacktraces()
	case "create-recovery-system":
		return createRecovery(st, a.Params.RecoverySystemLabel)
	case "migrate-home":
		return migrateHome(st, a.Snaps)
	default:
		return BadRequest("unknown debug action: %v", a.Action)
	}
}
