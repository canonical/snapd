// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2024 Canonical Ltd
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
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
)

var (
	// see daemon.go:canAccess for details how the access is controlled
	rootCmd = &Command{
		Path:       "/",
		GET:        tbd,
		ReadAccess: openAccess{},
	}

	sysInfoCmd = &Command{
		Path:       "/v2/system-info",
		GET:        sysInfo,
		ReadAccess: openAccess{},
	}

	stateChangeCmd = &Command{
		Path:        "/v2/changes/{id}",
		GET:         getChange,
		POST:        abortChange,
		ReadAccess:  interfaceOpenAccess{Interface: "snap-refresh-observe"},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}

	stateChangesCmd = &Command{
		Path:       "/v2/changes",
		GET:        getChanges,
		ReadAccess: interfaceOpenAccess{Interface: "snap-refresh-observe"},
	}

	warningsCmd = &Command{
		Path:        "/v2/warnings",
		GET:         getWarnings,
		POST:        ackWarnings,
		ReadAccess:  openAccess{},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}
)

var (
	buildID     = "unknown"
	systemdVirt = ""
)

func init() {
	// cache the build-id on startup to ensure that changes in
	// the underlying binary do not affect us
	if bid, err := osutil.MyBuildID(); err == nil {
		buildID = bid
	}
	// cache systemd-detect-virt output as it's unlikely to change :-)
	if buf, _, err := osutil.RunSplitOutput("systemd-detect-virt"); err == nil {
		systemdVirt = string(bytes.TrimSpace(buf))
	}
}

func tbd(c *Command, r *http.Request, user *auth.UserState) Response {
	return SyncResponse([]string{"TBD"})
}

func sysInfo(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	snapMgr := c.d.overlord.SnapManager()
	deviceMgr := c.d.overlord.DeviceManager()
	st.Lock()
	defer st.Unlock()
	tr := config.NewTransaction(st)
	nextRefresh := snapMgr.NextRefresh()
	lastRefresh, _ := snapMgr.LastRefresh()
	refreshHold, _ := snapMgr.EffectiveRefreshHold()
	refreshScheduleStr, legacySchedule, err := snapMgr.RefreshSchedule()
	if err != nil {
		return InternalError("cannot get refresh schedule: %s", err)
	}
	users, err := auth.Users(st)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return InternalError("cannot get user auth data: %s", err)
	}

	refreshInfo := client.RefreshInfo{
		Last: formatRefreshTime(lastRefresh),
		Hold: formatRefreshTime(refreshHold),
		Next: formatRefreshTime(nextRefresh),
	}
	if !legacySchedule {
		refreshInfo.Timer = refreshScheduleStr
	} else {
		refreshInfo.Schedule = refreshScheduleStr
	}

	m := map[string]interface{}{
		"series":         release.Series,
		"version":        c.d.Version,
		"build-id":       buildID,
		"os-release":     release.ReleaseInfo,
		"on-classic":     release.OnClassic,
		"managed":        len(users) > 0,
		"kernel-version": osutil.KernelVersion(),
		"locations": map[string]interface{}{
			"snap-mount-dir": dirs.SnapMountDir,
			"snap-bin-dir":   dirs.SnapBinariesDir,
		},
		"refresh":      refreshInfo,
		"architecture": arch.DpkgArchitecture(),
		"system-mode":  deviceMgr.SystemMode(devicestate.SysAny),
		"features":     features.All(tr),
	}
	if systemdVirt != "" {
		m["virtualization"] = systemdVirt
	}

	// NOTE: Right now we don't have a good way to differentiate if we
	// only have partial confinement (ala AppArmor disabled and Seccomp
	// enabled) or no confinement at all. Once we have a better system
	// in place how we can dynamically retrieve these information from
	// snapd we will use this here.
	if sandbox.ForceDevMode() {
		m["confinement"] = "partial"
	} else {
		m["confinement"] = "strict"
	}

	// Convey richer information about features of available security backends.
	if features := sandboxFeatures(c.d.overlord.InterfaceManager().Repository().Backends()); features != nil {
		m["sandbox-features"] = features
	}

	return SyncResponse(m)
}

func formatRefreshTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Truncate(time.Minute).Format(time.RFC3339)
}

func sandboxFeatures(backends []interfaces.SecurityBackend) map[string][]string {
	result := make(map[string][]string, len(backends)+1)
	for _, backend := range backends {
		features := backend.SandboxFeatures()
		if len(features) > 0 {
			sort.Strings(features)
			result[string(backend.Name())] = features
		}
	}

	// Add information about supported confinement types as a fake backend
	features := make([]string, 1, 3)
	features[0] = "devmode"
	if !sandbox.ForceDevMode() {
		features = append(features, "strict")
	}
	if dirs.SupportsClassicConfinement() {
		features = append(features, "classic")
	}
	sort.Strings(features)
	result["confinement-options"] = features

	return result
}

func getChange(c *Command, r *http.Request, user *auth.UserState) Response {
	chID := muxVars(r)["id"]
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chg := state.Change(chID)
	if chg == nil {
		return NotFound("cannot find change with id %q", chID)
	}

	return SyncResponse(change2changeInfo(chg))
}

func getChanges(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	qselect := query.Get("select")
	if qselect == "" {
		qselect = "in-progress"
	}
	var filter func(*state.Change) bool
	switch qselect {
	case "all":
		filter = func(*state.Change) bool { return true }
	case "in-progress":
		filter = func(chg *state.Change) bool { return !chg.IsReady() }
	case "ready":
		filter = func(chg *state.Change) bool { return chg.IsReady() }
	default:
		return BadRequest("select should be one of: all,in-progress,ready")
	}

	if wantedName := query.Get("for"); wantedName != "" {
		outerFilter := filter
		filter = func(chg *state.Change) bool {
			if !outerFilter(chg) {
				return false
			}

			var snapNames []string
			if err := chg.Get("snap-names", &snapNames); err != nil {
				logger.Noticef("Cannot get snap-name for change %v", chg.ID())
				return false
			}

			for _, name := range snapNames {
				// due to
				// https://bugs.launchpad.net/snapd/+bug/1880560
				// the snap-names in service-control changes
				// could have included <snap>.<app>
				snapName, _ := snap.SplitSnapApp(name)
				if snapName == wantedName {
					return true
				}
			}
			return false
		}
	}

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chgs := state.Changes()
	chgInfos := make([]*changeInfo, 0, len(chgs))
	for _, chg := range chgs {
		if !filter(chg) {
			continue
		}
		chgInfos = append(chgInfos, change2changeInfo(chg))
	}
	return SyncResponse(chgInfos)
}

func abortChange(c *Command, r *http.Request, user *auth.UserState) Response {
	chID := muxVars(r)["id"]
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chg := state.Change(chID)
	if chg == nil {
		return NotFound("cannot find change with id %q", chID)
	}

	var reqData struct {
		Action string `json:"action"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reqData); err != nil {
		return BadRequest("cannot decode data from request body: %v", err)
	}

	if reqData.Action != "abort" {
		return BadRequest("change action %q is unsupported", reqData.Action)
	}

	if chg.IsReady() {
		return BadRequest("cannot abort change %s with nothing pending", chID)
	}

	// flag the change
	chg.Abort()

	// actually ask to proceed with the abort
	ensureStateSoon(state)

	return SyncResponse(change2changeInfo(chg))
}

type changeInfo struct {
	ID      string      `json:"id"`
	Kind    string      `json:"kind"`
	Summary string      `json:"summary"`
	Status  string      `json:"status"`
	Tasks   []*taskInfo `json:"tasks,omitempty"`
	Ready   bool        `json:"ready"`
	Err     string      `json:"err,omitempty"`

	SpawnTime time.Time  `json:"spawn-time,omitempty"`
	ReadyTime *time.Time `json:"ready-time,omitempty"`

	Data map[string]*json.RawMessage `json:"data,omitempty"`
}

type taskInfo struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`
	Summary  string           `json:"summary"`
	Status   string           `json:"status"`
	Log      []string         `json:"log,omitempty"`
	Progress taskInfoProgress `json:"progress"`

	SpawnTime time.Time  `json:"spawn-time,omitempty"`
	ReadyTime *time.Time `json:"ready-time,omitempty"`
}

type taskInfoProgress struct {
	Label string `json:"label"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

func change2changeInfo(chg *state.Change) *changeInfo {
	status := chg.Status()
	chgInfo := &changeInfo{
		ID:      chg.ID(),
		Kind:    chg.Kind(),
		Summary: chg.Summary(),
		Status:  status.String(),
		Ready:   status.Ready(),

		SpawnTime: chg.SpawnTime(),
	}
	readyTime := chg.ReadyTime()
	if !readyTime.IsZero() {
		chgInfo.ReadyTime = &readyTime
	}
	if err := chg.Err(); err != nil {
		chgInfo.Err = err.Error()
	}

	tasks := chg.Tasks()
	taskInfos := make([]*taskInfo, len(tasks))
	for j, t := range tasks {
		label, done, total := t.Progress()

		taskInfo := &taskInfo{
			ID:      t.ID(),
			Kind:    t.Kind(),
			Summary: t.Summary(),
			Status:  t.Status().String(),
			Log:     t.Log(),
			Progress: taskInfoProgress{
				Label: label,
				Done:  done,
				Total: total,
			},
			SpawnTime: t.SpawnTime(),
		}
		readyTime := t.ReadyTime()
		if !readyTime.IsZero() {
			taskInfo.ReadyTime = &readyTime
		}
		taskInfos[j] = taskInfo
	}
	chgInfo.Tasks = taskInfos

	var data map[string]*json.RawMessage
	if chg.Get("api-data", &data) == nil {
		chgInfo.Data = data
	}

	return chgInfo
}

var (
	stateOkayWarnings    = (*state.State).OkayWarnings
	stateAllWarnings     = (*state.State).AllWarnings
	statePendingWarnings = (*state.State).PendingWarnings
)

func getWarnings(c *Command, r *http.Request, _ *auth.UserState) Response {
	query := r.URL.Query()
	var all bool
	sel := query.Get("select")
	switch sel {
	case "all":
		all = true
	case "pending", "":
		all = false
	default:
		return BadRequest("invalid select parameter: %q", sel)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var ws []*state.Warning
	if all {
		ws = stateAllWarnings(st)
	} else {
		ws, _ = statePendingWarnings(st)
	}
	if len(ws) == 0 {
		// no need to confuse the issue
		return SyncResponse([]state.Warning{})
	}

	return SyncResponse(ws)
}

func ackWarnings(c *Command, r *http.Request, _ *auth.UserState) Response {
	defer r.Body.Close()
	var op struct {
		Action    string    `json:"action"`
		Timestamp time.Time `json:"timestamp"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&op); err != nil {
		return BadRequest("cannot decode request body into warnings operation: %v", err)
	}
	if op.Action != "okay" {
		return BadRequest("unknown warning action %q", op.Action)
	}
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	n := stateOkayWarnings(st, op.Timestamp)

	return SyncResponse(n)
}
