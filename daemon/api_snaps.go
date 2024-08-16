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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts/snapasserts"
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
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-refresh-observe"}},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}

	snapsCmd = &Command{
		Path:        "/v2/snaps",
		GET:         getSnapsInfo,
		POST:        postSnaps,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-refresh-observe"}},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}
)

func getSnapInfo(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	name := vars["name"]

	st := c.d.overlord.State()
	about, err := localSnapInfo(st, name)
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

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if user != nil {
		inst.userID = user.ID
	}

	vars := muxVars(r)
	inst.Snaps = []string{vars["name"]}

	if len(inst.CompsRaw) > 0 {
		// must be a string slice for /v2/snaps/<snap>
		if err := inst.setCompsFromRawList(); err != nil {
			return BadRequest("%s", err)
		}
	}

	if err := inst.validate(); err != nil {
		return BadRequest("%s", err)
	}

	impl := inst.dispatch()
	if impl == nil {
		return BadRequest("unknown action %s", inst.Action)
	}

	msg, tsets, err := impl(r.Context(), &inst, st)
	if err != nil {
		return inst.errToResponse(err)
	}

	chg := newChange(st, inst.Action+"-snap", msg, tsets, inst.Snaps)
	if len(tsets) == 0 {
		chg.SetStatus(state.DoneStatus)
	}

	if inst.SystemRestartImmediate {
		chg.Set("system-restart-immediate", true)
	}

	apiData := map[string]interface{}{}
	if len(inst.CompsForSnaps) > 0 {
		apiData["components"] = inst.CompsForSnaps
		// TODO:COMPS: in install case we might want "snap-names" set
		// if we installed the snap too
	} else {
		apiData["snap-names"] = inst.Snaps
	}

	chg.Set("api-data", apiData)

	ensureStateSoon(st)

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
	CompsRaw               json.RawMessage                  `json:"components"`
	CompsForSnaps          map[string][]string              `json:"-"`
	DevMode                bool                             `json:"devmode"`
	JailMode               bool                             `json:"jailmode"`
	Classic                bool                             `json:"classic"`
	IgnoreValidation       bool                             `json:"ignore-validation"`
	IgnoreRunning          bool                             `json:"ignore-running"`
	Unaliased              bool                             `json:"unaliased"`
	Prefer                 bool                             `json:"prefer"`
	Purge                  bool                             `json:"purge,omitempty"`
	SystemRestartImmediate bool                             `json:"system-restart-immediate"`
	Transaction            client.TransactionType           `json:"transaction"`
	Snaps                  []string                         `json:"snaps"`
	Users                  []string                         `json:"users"`
	SnapshotOptions        map[string]*snap.SnapshotOptions `json:"snapshot-options"`
	ValidationSets         []string                         `json:"validation-sets"`
	QuotaGroupName         string                           `json:"quota-group"`
	Time                   string                           `json:"time"`
	HoldLevel              string                           `json:"hold-level"`

	// The fields below should not be unmarshalled into. Do not export them.
	userID int
}

func (inst *snapInstruction) setCompsFromRawList() error {
	compsList := []string{}
	if err := json.Unmarshal(inst.CompsRaw, &compsList); err != nil {
		return err
	}
	inst.CompsForSnaps = make(map[string][]string, len(compsList))
	inst.CompsForSnaps[inst.Snaps[0]] = compsList
	return nil
}

func (inst *snapInstruction) setCompsFromRawMap() error {
	return json.Unmarshal(inst.CompsRaw, &inst.CompsForSnaps)
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
	if inst.Prefer {
		flags.Prefer = true
	}
	flags.QuotaGroupName = inst.QuotaGroupName

	return flags, nil
}

func (inst *snapInstruction) holdLevel() snapstate.HoldLevel {
	switch inst.HoldLevel {
	case "auto-refresh":
		return snapstate.HoldAutoRefresh
	case "general":
		return snapstate.HoldGeneral
	default:
		panic("not validated hold level")
	}
}

// cleanSnapshotOptions cleans the snapshot options.
//
// With default marshalling, some permutations of valid JSON definitions of snapshot-options e.g.
//   - `"snapshot-options": { "snap1": {} }`
//   - `"snapshot-options": { "snap1": {exclude: []} }`
//
// results in a pointer to SnapshotOptions object with a nil or zero length exclusion list
// which in turn will be marshalled to JSON as `options: {}` when we rather want it omitted.
// The cleaning step ensures that we only populate map entries for snapshot options that contain
// usable content that we want to be marshalled downstream.
func (inst *snapInstruction) cleanSnapshotOptions() {
	for name, options := range inst.SnapshotOptions {
		if options.Unset() {
			delete(inst.SnapshotOptions, name)
		}
	}
}

func (inst *snapInstruction) validateSnapshotOptions() error {
	if inst.SnapshotOptions == nil {
		return nil
	}
	if inst.Action != "snapshot" {
		return fmt.Errorf("snapshot-options can only be specified for snapshot action")
	}
	for name, options := range inst.SnapshotOptions {
		if !strutil.ListContains(inst.Snaps, name) {
			return fmt.Errorf("cannot use snapshot-options for snap %q that is not listed in snaps", name)
		}
		if err := options.Validate(); err != nil {
			return fmt.Errorf("invalid snapshot-options for snap %q: %v", name, err)
		}
	}

	return nil
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
	switch inst.Transaction {
	case "":
	case client.TransactionPerSnap, client.TransactionAllSnaps:
		if inst.Action != "install" && inst.Action != "refresh" {
			return fmt.Errorf(`transaction type is unsupported for %q actions`, inst.Action)
		}
	default:
		return fmt.Errorf("invalid value for transaction type: %s", inst.Transaction)
	}
	if inst.QuotaGroupName != "" && inst.Action != "install" {
		return fmt.Errorf("quota-group can only be specified on install")
	}

	if inst.Action == "hold" {
		if inst.Time == "" {
			return errors.New("hold action requires a non-empty time value")
		} else if inst.Time != "forever" {
			if _, err := time.Parse(time.RFC3339, inst.Time); err != nil {
				return fmt.Errorf(`hold action requires time to be "forever" or in RFC3339 format: %v`, err)
			}
		}
		if inst.HoldLevel == "" {
			return errors.New("hold action requires a non-empty hold-level value")
		} else if !(inst.HoldLevel == "auto-refresh" || inst.HoldLevel == "general") {
			return errors.New(`hold action requires hold-level to be either "auto-refresh" or "general"`)
		}
	}

	if inst.Action != "hold" {
		if inst.Time != "" {
			return errors.New(`time can only be specified for the "hold" action`)
		}
		if inst.HoldLevel != "" {
			return errors.New(`hold-level can only be specified for the "hold" action`)
		}
	}

	if inst.Unaliased && inst.Prefer {
		return errUnaliasedPreferConflict
	}
	if inst.Prefer && inst.Action != "install" {
		return fmt.Errorf("the prefer flag can only be specified on install")
	}

	if err := inst.validateSnapshotOptions(); err != nil {
		return err
	}

	if inst.Action == "snapshot" {
		inst.cleanSnapshotOptions()
	}

	if len(inst.CompsRaw) > 0 && inst.Action != "remove" {
		// TODO:COMPS: allow install too
		return fmt.Errorf("%q action is not supported for components", inst.Action)
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
var errUnaliasedPreferConflict = errors.New("cannot use unaliased and prefer flags together")
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

func snapInstall(ctx context.Context, inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if len(inst.Snaps[0]) == 0 {
		return "", nil, errors.New(i18n.G("cannot install snap with empty name"))
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
	tset, err := snapstateInstall(ctx, st, inst.Snaps[0], inst.revnoOpts(), inst.userID, flags)
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

func snapUpdate(_ context.Context, inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
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

func snapRemove(_ context.Context, inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if len(inst.CompsForSnaps) > 0 {
		return removeSnapComponents(inst, st)
	} else {
		return removeSnap(inst, st)
	}
}

func removeSnap(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	ts, err := snapstate.Remove(st, inst.Snaps[0], inst.Revision, &snapstate.RemoveFlags{Purge: inst.Purge})
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Remove %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func removeSnapComponents(inst *snapInstruction, st *state.State) (msg string, allTaskSets []*state.TaskSet, err error) {
	compsMsg := make([]string, 0, len(inst.CompsForSnaps))
	for snap, comps := range inst.CompsForSnaps {
		// We call from here only when we remove components, not the
		// full snap, so we need to refresh the security profiles.
		tss, err := snapstateRemoveComponents(st, snap, comps,
			snapstate.RemoveComponentsOpts{RefreshProfile: true})
		if err != nil {
			return "", nil, err
		}
		allTaskSets = append(allTaskSets, tss...)
		compsMsg = append(compsMsg, fmt.Sprintf(i18n.G("%v for %q snap"), comps, snap))
	}

	msg = fmt.Sprintf(i18n.G("Remove component(s) %s"), strings.Join(compsMsg, ", "))
	return msg, allTaskSets, nil
}

func snapRevert(_ context.Context, inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	var ts *state.TaskSet

	flags, err := inst.modeFlags()
	if err != nil {
		return "", nil, err
	}

	if inst.Revision.Unset() {
		ts, err = snapstateRevert(st, inst.Snaps[0], flags, "")
	} else {
		ts, err = snapstateRevertToRevision(st, inst.Snaps[0], inst.Revision, flags, "")
	}
	if err != nil {
		return "", nil, err
	}

	msg := fmt.Sprintf(i18n.G("Revert %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapEnable(_ context.Context, inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
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

func snapDisable(_ context.Context, inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
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

func snapSwitch(_ context.Context, inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
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

// snapHold holds refreshes for one snap.
func snapHold(ctx context.Context, inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	res, err := snapHoldMany(ctx, inst, st)
	if err != nil {
		return "", nil, err
	}

	return res.Summary, res.Tasksets, nil
}

// snapUnhold removes the hold on refreshes for one snap.
func snapUnhold(ctx context.Context, inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	res, err := snapUnholdMany(ctx, inst, st)
	if err != nil {
		return "", nil, err
	}

	return res.Summary, res.Tasksets, nil
}

type snapActionFunc func(context.Context, *snapInstruction, *state.State) (string, []*state.TaskSet, error)

var snapInstructionDispTable = map[string]snapActionFunc{
	"install": snapInstall,
	"refresh": snapUpdate,
	"remove":  snapRemove,
	"revert":  snapRevert,
	"enable":  snapEnable,
	"disable": snapDisable,
	"switch":  snapSwitch,
	"hold":    snapHold,
	"unhold":  snapUnhold,
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

	return sideloadOrTrySnap(r.Context(), c, r.Body, params["boundary"], user)
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
	if inst.Channel != "" || !inst.Revision.Unset() || inst.DevMode || inst.JailMode || inst.CohortKey != "" || inst.LeaveCohort || inst.Prefer {
		return BadRequest("unsupported option provided for multi-snap operation")
	}
	if len(inst.CompsRaw) > 0 {
		// must be a map of snaps to components for /v2/snaps
		if err := inst.setCompsFromRawMap(); err != nil {
			return BadRequest("%s", err)
		}
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

	res, err := op(r.Context(), &inst, st)
	if err != nil {
		return inst.errToResponse(err)
	}

	chg := newChange(st, inst.Action+"-snap", res.Summary, res.Tasksets, res.Affected)
	if len(res.Tasksets) == 0 {
		chg.SetStatus(state.DoneStatus)
	}

	if inst.SystemRestartImmediate {
		chg.Set("system-restart-immediate", true)
	}

	apiData := map[string]interface{}{}
	if len(res.Affected) > 0 {
		apiData["snap-names"] = res.Affected
	}
	if len(inst.CompsForSnaps) > 0 {
		apiData["components"] = inst.CompsForSnaps
	}

	chg.Set("api-data", apiData)

	ensureStateSoon(st)

	return AsyncResponse(res.Result, chg.ID())
}

type snapManyActionFunc func(context.Context, *snapInstruction, *state.State) (*snapInstructionResult, error)

func (inst *snapInstruction) dispatchForMany() (op snapManyActionFunc) {
	switch inst.Action {
	case "refresh":
		if len(inst.ValidationSets) > 0 {
			op = snapEnforceValidationSets
		} else {
			op = snapUpdateMany
		}
	case "install":
		op = snapInstallMany
	case "remove":
		op = snapRemoveMany
	case "snapshot":
		// see api_snapshots.go
		op = snapshotMany
	case "hold":
		op = snapHoldMany
	case "unhold":
		op = snapUnholdMany
	}
	return op
}

func snapInstallMany(_ context.Context, inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	for _, name := range inst.Snaps {
		if len(name) == 0 {
			return nil, fmt.Errorf(i18n.G("cannot install snap with empty name"))
		}
	}
	transaction := inst.Transaction
	// TODO use per request context passed in snap instruction
	installed, tasksets, err := snapstateInstallMany(st, inst.Snaps, nil, inst.userID, &snapstate.Flags{Transaction: transaction})
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

func snapUpdateMany(ctx context.Context, inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
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

	transaction := inst.Transaction
	updated, tasksets, err := snapstateUpdateMany(ctx, st, inst.Snaps, nil, inst.userID, &snapstate.Flags{
		IgnoreRunning: inst.IgnoreRunning,
		Transaction:   transaction,
	})
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

func snapEnforceValidationSets(ctx context.Context, inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	if len(inst.ValidationSets) > 0 && len(inst.Snaps) != 0 {
		return nil, fmt.Errorf("snap names cannot be specified with validation sets to enforce")
	}

	snaps, ignoreValidationSnaps, err := snapstate.InstalledSnaps(st)
	if err != nil {
		return nil, err
	}

	// we need refreshed snap-declarations, this ensures that snap-declarations
	// and their prerequisite assertions are updated regularly; do not update all
	// validation-set assertions (this is implied by passing nil opts) - only
	// those requested via inst.ValidationSets will get updated by
	// assertstateTryEnforceValidationSets below.
	if err := assertstateRefreshSnapAssertions(st, inst.userID, nil); err != nil {
		return nil, err
	}

	var tss []*state.TaskSet
	var affected []string
	err = assertstateTryEnforcedValidationSets(st, inst.ValidationSets, inst.userID, snaps, ignoreValidationSnaps)
	if err != nil {
		vErr, ok := err.(*snapasserts.ValidationSetsValidationError)
		if !ok {
			return nil, err
		}

		tss, affected, err = meetSnapConstraintsForEnforce(ctx, inst, st, vErr)
		if err != nil {
			return nil, err
		}
	}

	summary := fmt.Sprintf("Enforce validation sets %s", strutil.Quoted(inst.ValidationSets))
	if len(affected) != 0 {
		summary = fmt.Sprintf("%s for snaps %s", summary, strutil.Quoted(affected))
	}

	return &snapInstructionResult{
		Summary:  summary,
		Affected: affected,
		Tasksets: tss,
	}, nil
}

func meetSnapConstraintsForEnforce(ctx context.Context, inst *snapInstruction, st *state.State, vErr *snapasserts.ValidationSetsValidationError) ([]*state.TaskSet, []string, error) {
	// Save the sequence numbers so we can pin them later when enforcing the sets again
	pinnedSeqs := make(map[string]int, len(inst.ValidationSets))

	trackedSets, err := assertstate.ValidationSets(st)
	if err != nil {
		return nil, nil, err
	}

	// make sure to re-pin the already existing validation sets that were
	// considered when creating this enforcement error
	for key := range vErr.Sets {
		tr, ok := trackedSets[key]

		// new validation sets won't be found in the already tracked sets
		if !ok {
			continue
		}

		// ignore any that are not pinned
		if tr.PinnedAt == 0 {
			continue
		}

		pinnedSeqs[key] = tr.PinnedAt
	}

	// also pin new validation sets that are not yet tracked
	for _, vsStr := range inst.ValidationSets {
		account, name, sequence, err := snapasserts.ParseValidationSet(vsStr)
		if err != nil {
			return nil, nil, err
		}

		if sequence == 0 {
			continue
		}

		pinnedSeqs[fmt.Sprintf("%s/%s", account, name)] = sequence
	}

	return snapstateResolveValSetsEnforcementError(ctx, st, vErr, pinnedSeqs, inst.userID)
}

func snapRemoveMany(_ context.Context, inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	if len(inst.Snaps) == 0 && len(inst.CompsForSnaps) == 0 {
		return nil, fmt.Errorf("cannot remove zero snaps")
	}

	var compsTaskSets, snapsTaskSets []*state.TaskSet
	var removed []string
	var msg, snapsMsg, compsMsg string
	var err error
	if len(inst.CompsForSnaps) > 0 {
		for snap := range inst.CompsForSnaps {
			if strutil.ListContains(inst.Snaps, snap) {
				return nil, fmt.Errorf(i18n.G("unexpected request to remove some components and also the full snap (which would remove all components) for %q"), snap)
			}
		}
		compsMsg, compsTaskSets, err = removeSnapComponents(inst, st)
		if err != nil {
			return nil, err
		}
	}
	if len(inst.Snaps) > 0 {
		flags := &snapstate.RemoveFlags{Purge: inst.Purge}
		removed, snapsTaskSets, err = snapstateRemoveMany(st, inst.Snaps, flags)
		if err != nil {
			return nil, err
		}
		switch len(inst.Snaps) {
		case 1:
			snapsMsg = fmt.Sprintf(i18n.G("Remove snap %q"), inst.Snaps[0])
		default:
			quoted := strutil.Quoted(inst.Snaps)
			// TRANSLATORS: the %s is a comma-separated list of quoted snap names
			snapsMsg = fmt.Sprintf(i18n.G("Remove snaps %s"), quoted)
		}
	}

	tasksets := make([]*state.TaskSet, 0, len(compsTaskSets)+len(snapsTaskSets))
	tasksets = append(tasksets, compsTaskSets...)
	tasksets = append(tasksets, snapsTaskSets...)
	if snapsMsg == "" {
		msg = compsMsg
	} else if compsMsg == "" {
		msg = snapsMsg
	} else {
		msg = fmt.Sprintf("%s - %s", snapsMsg, compsMsg)
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
	var sel snapSelect
	switch query.Get("select") {
	case "":
		sel = snapSelectNone
	case "all":
		sel = snapSelectAll
	case "enabled":
		sel = snapSelectEnabled
	case "refresh-inhibited":
		sel = snapSelectRefreshInhibited
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

	st := c.d.overlord.State()
	found, err := allLocalSnapInfos(st, sel, wanted)
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

func snapHoldMany(_ context.Context, inst *snapInstruction, st *state.State) (res *snapInstructionResult, err error) {
	var msg string
	var tss []*state.TaskSet
	if len(inst.Snaps) == 0 {
		if inst.holdLevel() == snapstate.HoldGeneral {
			return nil, errors.New("holding general refreshes for all snaps is not supported")
		}
		patchValues := map[string]interface{}{"refresh.hold": inst.Time}
		ts, err := configstateConfigureInstalled(st, "core", patchValues, 0)
		if err != nil {
			return nil, err
		}

		tss = []*state.TaskSet{ts}
		msg = i18n.G("Hold auto-refreshes for all snaps")
	} else {
		holdLevel := inst.holdLevel()
		if err := snapstateHoldRefreshesBySystem(st, holdLevel, inst.Time, inst.Snaps); err != nil {
			return nil, err
		}
		msgFmt := i18n.G("Hold general refreshes for %s")
		if holdLevel == snapstate.HoldAutoRefresh {
			msgFmt = i18n.G("Hold auto-refreshes for %s")
		}
		msg = fmt.Sprintf(msgFmt, strutil.Quoted(inst.Snaps))
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: inst.Snaps,
		Tasksets: tss,
	}, nil
}

func snapUnholdMany(_ context.Context, inst *snapInstruction, st *state.State) (res *snapInstructionResult, err error) {
	var msg string
	var tss []*state.TaskSet

	if len(inst.Snaps) == 0 {
		patchValues := map[string]interface{}{"refresh.hold": nil}
		ts, err := configstateConfigureInstalled(st, "core", patchValues, 0)
		if err != nil {
			return nil, err
		}

		tss = []*state.TaskSet{ts}
		msg = i18n.G("Remove auto-refresh hold on all snaps")
	} else {
		if err := snapstateProceedWithRefresh(st, "system", inst.Snaps); err != nil {
			return nil, err
		}

		msg = fmt.Sprintf(i18n.G("Remove refresh hold on %s"), strutil.Quoted(inst.Snaps))
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: inst.Snaps,
		Tasksets: tss,
	}, nil
}
