// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/strutil"
)

var (
	// see daemon.go:canAccess for details how the access is controlled
	snapCmd = &Command{
		Path:        "/v2/snaps/{name}",
		GET:         getSnapInfo,
		POST:        postSnap,
		ReadAccess:  openAccess{},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}

	snapsCmd = &Command{
		Path:        "/v2/snaps",
		GET:         getSnapsInfo,
		POST:        postSnaps,
		ReadAccess:  openAccess{},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}
)

func getSnapInfo(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	name := vars["name"]

	about, err := localSnapInfo(c.d.overlord.State(), name)
	if err != nil {
		if err == errNoSnap {
			return SnapNotFound(name, err)
		}

		return InternalError("%v", err)
	}

	route := c.d.router.Get(c.Path)
	if route == nil {
		return InternalError("cannot find route for %q snap", name)
	}

	url, err := route.URL("name", name)
	if err != nil {
		return InternalError("cannot build URL for %q snap: %v", name, err)
	}

	sd := servicestate.NewStatusDecorator(progress.Null)

	result := webify(mapLocal(about, sd), url.String())

	return SyncResponse(result)
}

func webify(result *client.Snap, resource string) *client.Snap {
	if result.Icon == "" || strings.HasPrefix(result.Icon, "http") {
		return result
	}
	result.Icon = ""

	route := appIconCmd.d.router.Get(appIconCmd.Path)
	if route != nil {
		url, err := route.URL("name", result.Name)
		if err == nil {
			result.Icon = url.String()
		}
	}

	return result
}

func postSnap(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("cannot find route for change")
	}

	decoder := json.NewDecoder(r.Body)
	var inst snapInstruction
	if err := decoder.Decode(&inst); err != nil {
		return BadRequest("cannot decode request body into snap instruction: %v", err)
	}
	inst.ctx = r.Context()

	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	if user != nil {
		inst.userID = user.ID
	}

	vars := muxVars(r)
	inst.Snaps = []string{vars["name"]}

	if err := inst.validate(); err != nil {
		return BadRequest("%s", err)
	}

	impl := inst.dispatch()
	if impl == nil {
		return BadRequest("unknown action %s", inst.Action)
	}

	msg, tsets, err := impl(&inst, state)
	if err != nil {
		return inst.errToResponse(err)
	}

	chg := newChange(state, inst.Action+"-snap", msg, tsets, inst.Snaps)
	if inst.SystemRestartImmediate {
		chg.Set("system-restart-immediate", true)
	}

	ensureStateSoon(state)

	return AsyncResponse(nil, chg.ID())
}

type snapRevisionOptions struct {
	Channel  string        `json:"channel"`
	Revision snap.Revision `json:"revision"`

	CohortKey   string `json:"cohort-key"`
	LeaveCohort bool   `json:"leave-cohort"`
}

func (ropt *snapRevisionOptions) validate() error {
	if ropt.CohortKey != "" {
		if ropt.LeaveCohort {
			return fmt.Errorf("cannot specify both cohort-key and leave-cohort")
		}
		if !ropt.Revision.Unset() {
			return fmt.Errorf("cannot specify both cohort-key and revision")
		}
	}

	if ropt.Channel != "" {
		_, err := channel.Parse(ropt.Channel, "-")
		if err != nil {
			return err
		}
	}
	return nil
}

type snapInstruction struct {
	progress.NullMeter

	Action string `json:"action"`
	Amend  bool   `json:"amend"`
	snapRevisionOptions
	DevMode                bool     `json:"devmode"`
	JailMode               bool     `json:"jailmode"`
	Classic                bool     `json:"classic"`
	IgnoreValidation       bool     `json:"ignore-validation"`
	IgnoreRunning          bool     `json:"ignore-running"`
	Unaliased              bool     `json:"unaliased"`
	Purge                  bool     `json:"purge,omitempty"`
	SystemRestartImmediate bool     `json:"system-restart-immediate"`
	Snaps                  []string `json:"snaps"`
	Users                  []string `json:"users"`

	// The fields below should not be unmarshalled into. Do not export them.
	userID int
	ctx    context.Context
}

func (inst *snapInstruction) revnoOpts() *snapstate.RevisionOptions {
	return &snapstate.RevisionOptions{
		Channel:     inst.Channel,
		Revision:    inst.Revision,
		CohortKey:   inst.CohortKey,
		LeaveCohort: inst.LeaveCohort,
	}
}

func (inst *snapInstruction) modeFlags() (snapstate.Flags, error) {
	return modeFlags(inst.DevMode, inst.JailMode, inst.Classic)
}

func (inst *snapInstruction) installFlags() (snapstate.Flags, error) {
	flags, err := inst.modeFlags()
	if err != nil {
		return snapstate.Flags{}, err
	}
	if inst.Unaliased {
		flags.Unaliased = true
	}
	if inst.IgnoreRunning {
		flags.IgnoreRunning = true
	}
	if inst.IgnoreValidation {
		flags.IgnoreValidation = true
	}

	return flags, nil
}

func (inst *snapInstruction) validate() error {
	if inst.CohortKey != "" {
		if inst.Action != "install" && inst.Action != "refresh" && inst.Action != "switch" {
			return fmt.Errorf("cohort-key can only be specified for install, refresh, or switch")
		}
	}
	if inst.LeaveCohort {
		if inst.Action != "refresh" && inst.Action != "switch" {
			return fmt.Errorf("leave-cohort can only be specified for refresh or switch")
		}
	}
	if inst.Action == "install" {
		for _, snapName := range inst.Snaps {
			// FIXME: alternatively we could simply mutate *inst
			//        and s/ubuntu-core/core/ ?
			if snapName == "ubuntu-core" {
				return fmt.Errorf(`cannot install "ubuntu-core", please use "core" instead`)
			}
		}
	}

	return inst.snapRevisionOptions.validate()
}

type snapInstructionResult struct {
	Summary  string
	Affected []string
	Tasksets []*state.TaskSet
	Result   map[string]interface{}
}

var errDevJailModeConflict = errors.New("cannot use devmode and jailmode flags together")
var errClassicDevmodeConflict = errors.New("cannot use classic and devmode flags together")
var errNoJailMode = errors.New("this system cannot honour the jailmode flag")

func modeFlags(devMode, jailMode, classic bool) (snapstate.Flags, error) {
	flags := snapstate.Flags{}
	devModeOS := sandbox.ForceDevMode()
	switch {
	case jailMode && devModeOS:
		return flags, errNoJailMode
	case jailMode && devMode:
		return flags, errDevJailModeConflict
	case devMode && classic:
		return flags, errClassicDevmodeConflict
	}
	// NOTE: jailmode and classic are allowed together. In that setting,
	// jailmode overrides classic and the app gets regular (non-classic)
	// confinement.
	flags.JailMode = jailMode
	flags.Classic = classic
	flags.DevMode = devMode
	return flags, nil
}

func snapInstall(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if len(inst.Snaps[0]) == 0 {
		return "", nil, fmt.Errorf(i18n.G("cannot install snap with empty name"))
	}

	flags, err := inst.installFlags()
	if err != nil {
		return "", nil, err
	}

	var ckey string
	if inst.CohortKey == "" {
		logger.Noticef("Installing snap %q revision %s", inst.Snaps[0], inst.Revision)
	} else {
		ckey = strutil.ElliptLeft(inst.CohortKey, 10)
		logger.Noticef("Installing snap %q from cohort %q", inst.Snaps[0], ckey)
	}
	tset, err := snapstateInstall(inst.ctx, st, inst.Snaps[0], inst.revnoOpts(), inst.userID, flags)
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Install %q snap"), inst.Snaps[0])
	if inst.Channel != "stable" && inst.Channel != "" {
		msg += fmt.Sprintf(" from %q channel", inst.Channel)
	}
	if inst.CohortKey != "" {
		msg += fmt.Sprintf(" from %q cohort", ckey)
	}
	return msg, []*state.TaskSet{tset}, nil
}

func snapUpdate(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	// TODO: bail if revision is given (and != current?), *or* behave as with install --revision?
	flags, err := inst.modeFlags()
	if err != nil {
		return "", nil, err
	}
	if inst.IgnoreValidation {
		flags.IgnoreValidation = true
	}
	if inst.IgnoreRunning {
		flags.IgnoreRunning = true
	}
	if inst.Amend {
		flags.Amend = true
	}

	// we need refreshed snap-declarations to enforce refresh-control as best as we can
	if err = assertstateRefreshSnapAssertions(st, inst.userID, nil); err != nil {
		return "", nil, err
	}

	ts, err := snapstateUpdate(st, inst.Snaps[0], inst.revnoOpts(), inst.userID, flags)
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Refresh %q snap"), inst.Snaps[0])
	if inst.Channel != "stable" && inst.Channel != "" {
		msg = fmt.Sprintf(i18n.G("Refresh %q snap from %q channel"), inst.Snaps[0], inst.Channel)
	}

	return msg, []*state.TaskSet{ts}, nil
}

func snapRemove(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	ts, err := snapstate.Remove(st, inst.Snaps[0], inst.Revision, &snapstate.RemoveFlags{Purge: inst.Purge})
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Remove %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapRevert(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	var ts *state.TaskSet

	flags, err := inst.modeFlags()
	if err != nil {
		return "", nil, err
	}

	if inst.Revision.Unset() {
		ts, err = snapstateRevert(st, inst.Snaps[0], flags)
	} else {
		ts, err = snapstateRevertToRevision(st, inst.Snaps[0], inst.Revision, flags)
	}
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Revert %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapEnable(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if !inst.Revision.Unset() {
		return "", nil, errors.New("enable takes no revision")
	}
	ts, err := snapstate.Enable(st, inst.Snaps[0])
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Enable %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapDisable(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if !inst.Revision.Unset() {
		return "", nil, errors.New("disable takes no revision")
	}
	ts, err := snapstate.Disable(st, inst.Snaps[0])
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Disable %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapSwitch(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if !inst.Revision.Unset() {
		return "", nil, errors.New("switch takes no revision")
	}
	ts, err := snapstateSwitch(st, inst.Snaps[0], inst.revnoOpts())
	if err != nil {
		return "", nil, err
	}

	var msg string
	switch {
	case inst.LeaveCohort && inst.Channel != "":
		msg = fmt.Sprintf(i18n.G("Switch %q snap to channel %q and away from cohort"), inst.Snaps[0], inst.Channel)
	case inst.LeaveCohort:
		msg = fmt.Sprintf(i18n.G("Switch %q snap away from cohort"), inst.Snaps[0])
	case inst.CohortKey == "" && inst.Channel != "":
		msg = fmt.Sprintf(i18n.G("Switch %q snap to channel %q"), inst.Snaps[0], inst.Channel)
	case inst.CohortKey != "" && inst.Channel == "":
		msg = fmt.Sprintf(i18n.G("Switch %q snap to cohort %q"), inst.Snaps[0], strutil.ElliptLeft(inst.CohortKey, 10))
	default:
		msg = fmt.Sprintf(i18n.G("Switch %q snap to channel %q and cohort %q"), inst.Snaps[0], inst.Channel, strutil.ElliptLeft(inst.CohortKey, 10))
	}
	return msg, []*state.TaskSet{ts}, nil
}

type snapActionFunc func(*snapInstruction, *state.State) (string, []*state.TaskSet, error)

var snapInstructionDispTable = map[string]snapActionFunc{
	"install": snapInstall,
	"refresh": snapUpdate,
	"remove":  snapRemove,
	"revert":  snapRevert,
	"enable":  snapEnable,
	"disable": snapDisable,
	"switch":  snapSwitch,
}

func (inst *snapInstruction) dispatch() snapActionFunc {
	if len(inst.Snaps) != 1 {
		logger.Panicf("dispatch only handles single-snap ops; got %d", len(inst.Snaps))
	}
	return snapInstructionDispTable[inst.Action]
}

func (inst *snapInstruction) errToResponse(err error) *apiError {
	if len(inst.Snaps) == 0 {
		return errToResponse(err, nil, BadRequest, "cannot %s: %v", inst.Action)
	}

	return errToResponse(err, inst.Snaps, BadRequest, "cannot %s %s: %v", inst.Action, strutil.Quoted(inst.Snaps))
}

func postSnaps(c *Command, r *http.Request, user *auth.UserState) Response {
	contentType := r.Header.Get("Content-Type")

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return BadRequest("cannot parse content type: %v", err)
	}

	if mediaType == "application/json" {
		charset := strings.ToUpper(params["charset"])
		if charset != "" && charset != "UTF-8" {
			return BadRequest("unknown charset in content type: %s", contentType)
		}
		return snapOpMany(c, r, user)
	}

	if !strings.HasPrefix(contentType, "multipart/") {
		return BadRequest("unknown content type: %s", contentType)
	}

	return sideloadOrTrySnap(c, r.Body, params["boundary"], user)
}

func snapOpMany(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("cannot find route for change")
	}

	decoder := json.NewDecoder(r.Body)
	var inst snapInstruction
	if err := decoder.Decode(&inst); err != nil {
		return BadRequest("cannot decode request body into snap instruction: %v", err)
	}

	// TODO: inst.Amend, etc?
	if inst.Channel != "" || !inst.Revision.Unset() || inst.DevMode || inst.JailMode || inst.CohortKey != "" || inst.LeaveCohort || inst.Purge {
		return BadRequest("unsupported option provided for multi-snap operation")
	}
	if err := inst.validate(); err != nil {
		return BadRequest("%v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if user != nil {
		inst.userID = user.ID
	}

	op := inst.dispatchForMany()
	if op == nil {
		return BadRequest("unsupported multi-snap operation %q", inst.Action)
	}
	res, err := op(&inst, st)
	if err != nil {
		return inst.errToResponse(err)
	}

	var chg *state.Change
	if len(res.Tasksets) == 0 {
		chg = st.NewChange(inst.Action+"-snap", res.Summary)
		chg.SetStatus(state.DoneStatus)
	} else {
		chg = newChange(st, inst.Action+"-snap", res.Summary, res.Tasksets, res.Affected)
		ensureStateSoon(st)
	}

	if inst.SystemRestartImmediate {
		chg.Set("system-restart-immediate", true)
	}

	chg.Set("api-data", map[string]interface{}{"snap-names": res.Affected})

	return AsyncResponse(res.Result, chg.ID())
}

type snapManyActionFunc func(*snapInstruction, *state.State) (*snapInstructionResult, error)

func (inst *snapInstruction) dispatchForMany() (op snapManyActionFunc) {
	switch inst.Action {
	case "refresh":
		op = snapUpdateMany
	case "install":
		op = snapInstallMany
	case "remove":
		op = snapRemoveMany
	case "snapshot":
		// see api_snapshots.go
		op = snapshotMany
	}
	return op
}

func snapInstallMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	for _, name := range inst.Snaps {
		if len(name) == 0 {
			return nil, fmt.Errorf(i18n.G("cannot install snap with empty name"))
		}
	}
	installed, tasksets, err := snapstateInstallMany(st, inst.Snaps, inst.userID)
	if err != nil {
		return nil, err
	}

	var msg string
	switch len(inst.Snaps) {
	case 0:
		return nil, fmt.Errorf("cannot install zero snaps")
	case 1:
		msg = fmt.Sprintf(i18n.G("Install snap %q"), inst.Snaps[0])
	default:
		quoted := strutil.Quoted(inst.Snaps)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Install snaps %s"), quoted)
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: installed,
		Tasksets: tasksets,
	}, nil
}

func snapUpdateMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	// we need refreshed snap-declarations to enforce refresh-control as best as
	// we can, this also ensures that snap-declarations and their prerequisite
	// assertions are updated regularly; update validation sets assertions only
	// if refreshing all snaps (no snap names explicitly requested).
	opts := &assertstate.RefreshAssertionsOptions{
		IsRefreshOfAllSnaps: len(inst.Snaps) == 0,
	}
	if err := assertstateRefreshSnapAssertions(st, inst.userID, opts); err != nil {
		return nil, err
	}

	// TODO: use a per-request context
	updated, tasksets, err := snapstateUpdateMany(context.TODO(), st, inst.Snaps, inst.userID, nil)
	if err != nil {
		if opts.IsRefreshOfAllSnaps {
			if err := assertstateRestoreValidationSetsTracking(st); err != nil && !errors.Is(err, state.ErrNoState) {
				return nil, err
			}
		}
		return nil, err
	}

	var msg string
	switch len(updated) {
	case 0:
		if len(inst.Snaps) != 0 {
			// TRANSLATORS: the %s is a comma-separated list of quoted snap names
			msg = fmt.Sprintf(i18n.G("Refresh snaps %s: no updates"), strutil.Quoted(inst.Snaps))
		} else {
			msg = i18n.G("Refresh all snaps: no updates")
		}
	case 1:
		msg = fmt.Sprintf(i18n.G("Refresh snap %q"), updated[0])
	default:
		quoted := strutil.Quoted(updated)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Refresh snaps %s"), quoted)
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: updated,
		Tasksets: tasksets,
	}, nil
}

func snapRemoveMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	removed, tasksets, err := snapstateRemoveMany(st, inst.Snaps)
	if err != nil {
		return nil, err
	}

	var msg string
	switch len(inst.Snaps) {
	case 0:
		return nil, fmt.Errorf("cannot remove zero snaps")
	case 1:
		msg = fmt.Sprintf(i18n.G("Remove snap %q"), inst.Snaps[0])
	default:
		quoted := strutil.Quoted(inst.Snaps)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Remove snaps %s"), quoted)
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: removed,
		Tasksets: tasksets,
	}, nil
}

// query many snaps
func getSnapsInfo(c *Command, r *http.Request, user *auth.UserState) Response {

	if shouldSearchStore(r) {
		logger.Noticef("Jumping to \"find\" to better support legacy request %q", r.URL)
		return searchStore(c, r, user)
	}

	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}

	query := r.URL.Query()
	var all bool
	sel := query.Get("select")
	switch sel {
	case "all":
		all = true
	case "enabled", "":
		all = false
	default:
		return BadRequest("invalid select parameter: %q", sel)
	}
	var wanted map[string]bool
	if ns := query.Get("snaps"); len(ns) > 0 {
		nsl := strutil.CommaSeparatedList(ns)
		wanted = make(map[string]bool, len(nsl))
		for _, name := range nsl {
			wanted[name] = true
		}
	}

	found, err := allLocalSnapInfos(c.d.overlord.State(), all, wanted)
	if err != nil {
		return InternalError("cannot list local snaps! %v", err)
	}

	results := make([]*json.RawMessage, len(found))

	sd := servicestate.NewStatusDecorator(progress.Null)
	for i, x := range found {
		name := x.info.InstanceName()
		rev := x.info.Revision

		url, err := route.URL("name", name)
		if err != nil {
			logger.Noticef("Cannot build URL for snap %q revision %s: %v", name, rev, err)
			continue
		}

		data, err := json.Marshal(webify(mapLocal(x, sd), url.String()))
		if err != nil {
			return InternalError("cannot serialize snap %q revision %s: %v", name, rev, err)
		}
		raw := json.RawMessage(data)
		results[i] = &raw
	}

	return &findResponse{
		Results: results,
		Sources: []string{"local"},
	}
}

func shouldSearchStore(r *http.Request) bool {
	// we should jump to the old behaviour iff q is given, or if
	// sources is given and either empty or contains the word
	// 'store'.  Otherwise, local results only.

	query := r.URL.Query()

	if _, ok := query["q"]; ok {
		logger.Debugf("use of obsolete \"q\" parameter: %q", r.URL)
		return true
	}

	if src, ok := query["sources"]; ok {
		logger.Debugf("use of obsolete \"sources\" parameter: %q", r.URL)
		if len(src) == 0 || strings.Contains(src[0], "store") {
			return true
		}
	}

	return false
}
