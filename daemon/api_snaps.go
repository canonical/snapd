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

	"github.com/ddkwork/golibrary/mylog"
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
		ReadAccess:  openAccess{},
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
	about := mylog.Check2(localSnapInfo(st, name))

	route := c.d.router.Get(c.Path)
	if route == nil {
		return InternalError("cannot find route for %q snap", name)
	}

	url := mylog.Check2(route.URL("name", name))

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
		url := mylog.Check2(route.URL("name", result.Name))
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
	mylog.Check(decoder.Decode(&inst))

	inst.ctx = r.Context()

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if user != nil {
		inst.userID = user.ID
	}

	vars := muxVars(r)
	inst.Snaps = []string{vars["name"]}
	mylog.Check(inst.validate())

	impl := inst.dispatch()
	if impl == nil {
		return BadRequest("unknown action %s", inst.Action)
	}

	msg, tsets := mylog.Check3(impl(&inst, st))

	chg := newChange(st, inst.Action+"-snap", msg, tsets, inst.Snaps)
	if len(tsets) == 0 {
		chg.SetStatus(state.DoneStatus)
	}

	if inst.SystemRestartImmediate {
		chg.Set("system-restart-immediate", true)
	}

	chg.Set("api-data", map[string]interface{}{"snap-names": inst.Snaps})

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
		_ := mylog.Check2(channel.Parse(ropt.Channel, "-"))
	}
	return nil
}

type snapInstruction struct {
	progress.NullMeter

	Action string `json:"action"`
	Amend  bool   `json:"amend"`
	snapRevisionOptions
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
	flags := mylog.Check2(inst.modeFlags())

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
		mylog.Check(options.Validate())

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
			mylog.Check2(time.Parse(time.RFC3339, inst.Time))
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
	mylog.Check(inst.validateSnapshotOptions())

	if inst.Action == "snapshot" {
		inst.cleanSnapshotOptions()
	}

	return inst.snapRevisionOptions.validate()
}

type snapInstructionResult struct {
	Summary  string
	Affected []string
	Tasksets []*state.TaskSet
	Result   map[string]interface{}
}

var (
	errDevJailModeConflict     = errors.New("cannot use devmode and jailmode flags together")
	errClassicDevmodeConflict  = errors.New("cannot use classic and devmode flags together")
	errUnaliasedPreferConflict = errors.New("cannot use unaliased and prefer flags together")
	errNoJailMode              = errors.New("this system cannot honour the jailmode flag")
)

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

	flags := mylog.Check2(inst.installFlags())

	var ckey string
	if inst.CohortKey == "" {
		logger.Noticef("Installing snap %q revision %s", inst.Snaps[0], inst.Revision)
	} else {
		ckey = strutil.ElliptLeft(inst.CohortKey, 10)
		logger.Noticef("Installing snap %q from cohort %q", inst.Snaps[0], ckey)
	}
	tset := mylog.Check2(snapstateInstall(inst.ctx, st, inst.Snaps[0], inst.revnoOpts(), inst.userID, flags))

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
	flags := mylog.Check2(inst.modeFlags())

	if inst.IgnoreValidation {
		flags.IgnoreValidation = true
	}
	if inst.IgnoreRunning {
		flags.IgnoreRunning = true
	}
	if inst.Amend {
		flags.Amend = true
	}
	mylog.Check(

		// we need refreshed snap-declarations to enforce refresh-control as best as we can
		assertstateRefreshSnapAssertions(st, inst.userID, nil))

	ts := mylog.Check2(snapstateUpdate(st, inst.Snaps[0], inst.revnoOpts(), inst.userID, flags))

	msg := fmt.Sprintf(i18n.G("Refresh %q snap"), inst.Snaps[0])
	if inst.Channel != "stable" && inst.Channel != "" {
		msg = fmt.Sprintf(i18n.G("Refresh %q snap from %q channel"), inst.Snaps[0], inst.Channel)
	}

	return msg, []*state.TaskSet{ts}, nil
}

func snapRemove(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	ts := mylog.Check2(snapstate.Remove(st, inst.Snaps[0], inst.Revision, &snapstate.RemoveFlags{Purge: inst.Purge}))

	msg := fmt.Sprintf(i18n.G("Remove %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapRevert(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	var ts *state.TaskSet

	flags := mylog.Check2(inst.modeFlags())

	if inst.Revision.Unset() {
		ts = mylog.Check2(snapstateRevert(st, inst.Snaps[0], flags, ""))
	} else {
		ts = mylog.Check2(snapstateRevertToRevision(st, inst.Snaps[0], inst.Revision, flags, ""))
	}

	msg := fmt.Sprintf(i18n.G("Revert %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapEnable(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if !inst.Revision.Unset() {
		return "", nil, errors.New("enable takes no revision")
	}
	ts := mylog.Check2(snapstate.Enable(st, inst.Snaps[0]))

	msg := fmt.Sprintf(i18n.G("Enable %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapDisable(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if !inst.Revision.Unset() {
		return "", nil, errors.New("disable takes no revision")
	}
	ts := mylog.Check2(snapstate.Disable(st, inst.Snaps[0]))

	msg := fmt.Sprintf(i18n.G("Disable %q snap"), inst.Snaps[0])
	return msg, []*state.TaskSet{ts}, nil
}

func snapSwitch(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	if !inst.Revision.Unset() {
		return "", nil, errors.New("switch takes no revision")
	}
	ts := mylog.Check2(snapstateSwitch(st, inst.Snaps[0], inst.revnoOpts()))

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
func snapHold(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	res := mylog.Check2(snapHoldMany(inst, st))

	return res.Summary, res.Tasksets, nil
}

// snapUnhold removes the hold on refreshes for one snap.
func snapUnhold(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
	res := mylog.Check2(snapUnholdMany(inst, st))

	return res.Summary, res.Tasksets, nil
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

	mediaType, params := mylog.Check3(mime.ParseMediaType(contentType))

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
	mylog.Check(decoder.Decode(&inst))

	// TODO: inst.Amend, etc?
	if inst.Channel != "" || !inst.Revision.Unset() || inst.DevMode || inst.JailMode || inst.CohortKey != "" || inst.LeaveCohort || inst.Prefer {
		return BadRequest("unsupported option provided for multi-snap operation")
	}
	mylog.Check(inst.validate())

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
	res := mylog.Check2(op(&inst, st))

	chg := newChange(st, inst.Action+"-snap", res.Summary, res.Tasksets, res.Affected)
	if len(res.Tasksets) == 0 {
		chg.SetStatus(state.DoneStatus)
	}

	if inst.SystemRestartImmediate {
		chg.Set("system-restart-immediate", true)
	}

	chg.Set("api-data", map[string]interface{}{"snap-names": res.Affected})

	ensureStateSoon(st)

	return AsyncResponse(res.Result, chg.ID())
}

type snapManyActionFunc func(*snapInstruction, *state.State) (*snapInstructionResult, error)

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

func snapInstallMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	for _, name := range inst.Snaps {
		if len(name) == 0 {
			return nil, fmt.Errorf(i18n.G("cannot install snap with empty name"))
		}
	}
	transaction := inst.Transaction
	installed, tasksets := mylog.Check3(snapstateInstallMany(st, inst.Snaps, nil, inst.userID, &snapstate.Flags{Transaction: transaction}))

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
	mylog.Check(assertstateRefreshSnapAssertions(st, inst.userID, opts))

	transaction := inst.Transaction
	// TODO: use a per-request context
	updated, tasksets := mylog.Check3(snapstateUpdateMany(context.TODO(), st, inst.Snaps, nil, inst.userID, &snapstate.Flags{
		IgnoreRunning: inst.IgnoreRunning,
		Transaction:   transaction,
	}))

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

func snapEnforceValidationSets(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	if len(inst.ValidationSets) > 0 && len(inst.Snaps) != 0 {
		return nil, fmt.Errorf("snap names cannot be specified with validation sets to enforce")
	}

	snaps, ignoreValidationSnaps := mylog.Check3(snapstate.InstalledSnaps(st))
	mylog.Check(

		// we need refreshed snap-declarations, this ensures that snap-declarations
		// and their prerequisite assertions are updated regularly; do not update all
		// validation-set assertions (this is implied by passing nil opts) - only
		// those requested via inst.ValidationSets will get updated by
		// assertstateTryEnforceValidationSets below.
		assertstateRefreshSnapAssertions(st, inst.userID, nil))

	var tss []*state.TaskSet
	var affected []string
	mylog.Check(assertstateTryEnforcedValidationSets(st, inst.ValidationSets, inst.userID, snaps, ignoreValidationSnaps))

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

func meetSnapConstraintsForEnforce(inst *snapInstruction, st *state.State, vErr *snapasserts.ValidationSetsValidationError) ([]*state.TaskSet, []string, error) {
	// Save the sequence numbers so we can pin them later when enforcing the sets again
	pinnedSeqs := make(map[string]int, len(inst.ValidationSets))
	for _, vsStr := range inst.ValidationSets {
		account, name, sequence := mylog.Check4(snapasserts.ParseValidationSet(vsStr))

		if sequence == 0 {
			continue
		}

		pinnedSeqs[fmt.Sprintf("%s/%s", account, name)] = sequence
	}

	return snapstateResolveValSetsEnforcementError(context.TODO(), st, vErr, pinnedSeqs, inst.userID)
}

func snapRemoveMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	flags := &snapstate.RemoveFlags{Purge: inst.Purge}
	removed, tasksets := mylog.Check3(snapstateRemoveMany(st, inst.Snaps, flags))

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
	found := mylog.Check2(allLocalSnapInfos(st, sel, wanted))

	results := make([]*json.RawMessage, len(found))

	sd := servicestate.NewStatusDecorator(progress.Null)
	for i, x := range found {
		name := x.info.InstanceName()
		rev := x.info.Revision

		url := mylog.Check2(route.URL("name", name))

		data := mylog.Check2(json.Marshal(webify(mapLocal(x, sd), url.String())))

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

func snapHoldMany(inst *snapInstruction, st *state.State) (res *snapInstructionResult, err error) {
	var msg string
	var tss []*state.TaskSet
	if len(inst.Snaps) == 0 {
		if inst.holdLevel() == snapstate.HoldGeneral {
			return nil, errors.New("holding general refreshes for all snaps is not supported")
		}
		patchValues := map[string]interface{}{"refresh.hold": inst.Time}
		ts := mylog.Check2(configstateConfigureInstalled(st, "core", patchValues, 0))

		tss = []*state.TaskSet{ts}
		msg = i18n.G("Hold auto-refreshes for all snaps")
	} else {
		holdLevel := inst.holdLevel()
		mylog.Check(snapstateHoldRefreshesBySystem(st, holdLevel, inst.Time, inst.Snaps))

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

func snapUnholdMany(inst *snapInstruction, st *state.State) (res *snapInstructionResult, err error) {
	var msg string
	var tss []*state.TaskSet

	if len(inst.Snaps) == 0 {
		patchValues := map[string]interface{}{"refresh.hold": nil}
		ts := mylog.Check2(configstateConfigureInstalled(st, "core", patchValues, 0))

		tss = []*state.TaskSet{ts}
		msg = i18n.G("Remove auto-refresh hold on all snaps")
	} else {
		mylog.Check(snapstateProceedWithRefresh(st, "system", inst.Snaps))

		msg = fmt.Sprintf(i18n.G("Remove refresh hold on %s"), strutil.Quoted(inst.Snaps))
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: inst.Snaps,
		Tasksets: tss,
	}, nil
}
