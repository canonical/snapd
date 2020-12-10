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
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/strutil"
)

var api = []*Command{
	rootCmd,
	sysInfoCmd,
	loginCmd,
	logoutCmd,
	appIconCmd,
	findCmd,
	snapsCmd,
	snapCmd,
	snapFileCmd,
	snapDownloadCmd,
	snapConfCmd,
	interfacesCmd,
	assertsCmd,
	assertsFindManyCmd,
	stateChangeCmd,
	stateChangesCmd,
	createUserCmd,
	buyCmd,
	readyToBuyCmd,
	snapctlCmd,
	usersCmd,
	sectionsCmd,
	aliasesCmd,
	appsCmd,
	logsCmd,
	warningsCmd,
	debugPprofCmd,
	debugCmd,
	snapshotCmd,
	snapshotExportCmd,
	connectionsCmd,
	modelCmd,
	cohortsCmd,
	serialModelCmd,
	systemsCmd,
	systemsActionCmd,
	routineConsoleConfStartCmd,
	systemRecoveryKeysCmd,
}

var (
	// see daemon.go:canAccess for details how the access is controlled
	snapsCmd = &Command{
		Path:     "/v2/snaps",
		UserOK:   true,
		PolkitOK: "io.snapcraft.snapd.manage",
		GET:      getSnapsInfo,
		POST:     postSnaps,
	}

	snapCmd = &Command{
		Path:     "/v2/snaps/{name}",
		UserOK:   true,
		PolkitOK: "io.snapcraft.snapd.manage",
		GET:      getSnapInfo,
		POST:     postSnap,
	}
)

// UserFromRequest extracts user information from request and return the respective user in state, if valid
// It requires the state to be locked
func UserFromRequest(st *state.State, req *http.Request) (*auth.UserState, error) {
	// extract macaroons data from request
	header := req.Header.Get("Authorization")
	if header == "" {
		return nil, auth.ErrInvalidAuth
	}

	authorizationData := strings.SplitN(header, " ", 2)
	if len(authorizationData) != 2 || authorizationData[0] != "Macaroon" {
		return nil, fmt.Errorf("authorization header misses Macaroon prefix")
	}

	var macaroon string
	var discharges []string
	for _, field := range strutil.CommaSeparatedList(authorizationData[1]) {
		if strings.HasPrefix(field, `root="`) {
			macaroon = strings.TrimSuffix(field[6:], `"`)
		}
		if strings.HasPrefix(field, `discharge="`) {
			discharges = append(discharges, strings.TrimSuffix(field[11:], `"`))
		}
	}

	if macaroon == "" {
		return nil, fmt.Errorf("invalid authorization header")
	}

	user, err := auth.CheckMacaroon(st, macaroon, discharges)
	return user, err
}

var muxVars = mux.Vars

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

	return SyncResponse(result, nil)
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

func getStore(c *Command) snapstate.StoreService {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	return snapstate.Store(st, nil)
}

// plural!
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

	return SyncResponse(results, &Meta{Sources: []string{"local"}})
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

// licenseData holds details about the snap license, and may be
// marshaled back as an error when the license agreement is pending,
// and is expected as input to accept (or not) that license
// agreement. As such, its field names are part of the API.
type licenseData struct {
	Intro   string `json:"intro"`
	License string `json:"license"`
	Agreed  bool   `json:"agreed"`
}

func (*licenseData) Error() string {
	return "license agreement required"
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
	DevMode          bool `json:"devmode"`
	JailMode         bool `json:"jailmode"`
	Classic          bool `json:"classic"`
	IgnoreValidation bool `json:"ignore-validation"`
	IgnoreRunning    bool `json:"ignore-running"`
	Unaliased        bool `json:"unaliased"`
	Purge            bool `json:"purge,omitempty"`
	// dropping support temporarely until flag confusion is sorted,
	// this isn't supported by client atm anyway
	LeaveOld bool         `json:"temp-dropped-leave-old"`
	License  *licenseData `json:"license"`
	Snaps    []string     `json:"snaps"`
	Users    []string     `json:"users"`

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

var (
	snapstateInstall           = snapstate.Install
	snapstateInstallPath       = snapstate.InstallPath
	snapstateRefreshCandidates = snapstate.RefreshCandidates
	snapstateTryPath           = snapstate.TryPath
	snapstateUpdate            = snapstate.Update
	snapstateUpdateMany        = snapstate.UpdateMany
	snapstateInstallMany       = snapstate.InstallMany
	snapstateRemoveMany        = snapstate.RemoveMany
	snapstateRevert            = snapstate.Revert
	snapstateRevertToRevision  = snapstate.RevertToRevision
	snapstateSwitch            = snapstate.Switch

	snapshotList    = snapshotstate.List
	snapshotCheck   = snapshotstate.Check
	snapshotForget  = snapshotstate.Forget
	snapshotRestore = snapshotstate.Restore
	snapshotSave    = snapshotstate.Save
	snapshotExport  = snapshotstate.Export
	snapshotImport  = snapshotstate.Import

	assertstateRefreshSnapDeclarations = assertstate.RefreshSnapDeclarations
)

func ensureStateSoonImpl(st *state.State) {
	st.EnsureBefore(0)
}

var ensureStateSoon = ensureStateSoonImpl

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

func snapUpdateMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	// we need refreshed snap-declarations to enforce refresh-control as best as we can, this also ensures that snap-declarations and their prerequisite assertions are updated regularly
	if err := assertstateRefreshSnapDeclarations(st, inst.userID); err != nil {
		return nil, err
	}

	// TODO: use a per-request context
	updated, tasksets, err := snapstateUpdateMany(context.TODO(), st, inst.Snaps, inst.userID, nil)
	if err != nil {
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
	if err = assertstateRefreshSnapDeclarations(st, inst.userID); err != nil {
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

func snapshotMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	setID, snapshotted, ts, err := snapshotSave(st, inst.Snaps, inst.Users)
	if err != nil {
		return nil, err
	}

	var msg string
	if len(inst.Snaps) == 0 {
		msg = i18n.G("Snapshot all snaps")
	} else {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Snapshot snaps %s"), strutil.Quoted(inst.Snaps))
	}

	return &snapInstructionResult{
		Summary:  msg,
		Affected: snapshotted,
		Tasksets: []*state.TaskSet{ts},
		Result:   map[string]interface{}{"set-id": setID},
	}, nil
}

type snapActionFunc func(*snapInstruction, *state.State) (string, []*state.TaskSet, error)
type snapsActionFunc func(*snapInstruction, *state.State) (*snapInstructionResult, error)

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

func (inst *snapInstruction) dispatchForMany() (op snapsActionFunc) {
	switch inst.Action {
	case "refresh":
		op = snapUpdateMany
	case "install":
		op = snapInstallMany
	case "remove":
		op = snapRemoveMany
	case "snapshot":
		op = snapshotMany
	}
	return op
}

func (inst *snapInstruction) errToResponse(err error) Response {
	if len(inst.Snaps) == 0 {
		return errToResponse(err, nil, BadRequest, "cannot %s: %v", inst.Action)
	}

	return errToResponse(err, inst.Snaps, BadRequest, "cannot %s %s: %v", inst.Action, strutil.Quoted(inst.Snaps))
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

	ensureStateSoon(state)

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

func newChange(st *state.State, kind, summary string, tsets []*state.TaskSet, snapNames []string) *state.Change {
	chg := st.NewChange(kind, summary)
	for _, ts := range tsets {
		chg.AddAll(ts)
	}
	if snapNames != nil {
		chg.Set("snap-names", snapNames)
	}
	return chg
}

const maxReadBuflen = 1024 * 1024

func trySnap(st *state.State, r *http.Request, user *auth.UserState, trydir string, flags snapstate.Flags) Response {
	st.Lock()
	defer st.Unlock()

	if !filepath.IsAbs(trydir) {
		return BadRequest("cannot try %q: need an absolute path", trydir)
	}
	if !osutil.IsDirectory(trydir) {
		return BadRequest("cannot try %q: not a snap directory", trydir)
	}

	// the developer asked us to do this with a trusted snap dir
	info, err := unsafeReadSnapInfo(trydir)
	if _, ok := err.(snap.NotSnapError); ok {
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindNotSnap,
			},
			Status: 400,
		}, nil)
	}
	if err != nil {
		return BadRequest("cannot read snap info for %s: %s", trydir, err)
	}

	tset, err := snapstateTryPath(st, info.InstanceName(), trydir, flags)
	if err != nil {
		return errToResponse(err, []string{info.InstanceName()}, BadRequest, "cannot try %s: %s", trydir)
	}

	msg := fmt.Sprintf(i18n.G("Try %q snap from %s"), info.InstanceName(), trydir)
	chg := newChange(st, "try-snap", msg, []*state.TaskSet{tset}, []string{info.InstanceName()})
	chg.Set("api-data", map[string]string{"snap-name": info.InstanceName()})

	ensureStateSoon(st)

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

func isTrue(form *multipart.Form, key string) bool {
	value := form.Value[key]
	if len(value) == 0 {
		return false
	}
	b, err := strconv.ParseBool(value[0])
	if err != nil {
		return false
	}

	return b
}

func snapsOp(c *Command, r *http.Request, user *auth.UserState) Response {
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

	chg.Set("api-data", map[string]interface{}{"snap-names": res.Affected})

	return AsyncResponse(res.Result, &Meta{Change: chg.ID()})
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
		return snapsOp(c, r, user)
	}

	if !strings.HasPrefix(contentType, "multipart/") {
		return BadRequest("unknown content type: %s", contentType)
	}

	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("cannot find route for change")
	}

	// POSTs to sideload snaps must be a multipart/form-data file upload.
	form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(maxReadBuflen)
	if err != nil {
		return BadRequest("cannot read POST form: %v", err)
	}

	dangerousOK := isTrue(form, "dangerous")
	flags, err := modeFlags(isTrue(form, "devmode"), isTrue(form, "jailmode"), isTrue(form, "classic"))
	if err != nil {
		return BadRequest(err.Error())
	}

	if len(form.Value["action"]) > 0 && form.Value["action"][0] == "try" {
		if len(form.Value["snap-path"]) == 0 {
			return BadRequest("need 'snap-path' value in form")
		}
		return trySnap(c.d.overlord.State(), r, user, form.Value["snap-path"][0], flags)
	}
	flags.RemoveSnapPath = true

	flags.Unaliased = isTrue(form, "unaliased")
	flags.IgnoreRunning = isTrue(form, "ignore-running")

	// find the file for the "snap" form field
	var snapBody multipart.File
	var origPath string
out:
	for name, fheaders := range form.File {
		if name != "snap" {
			continue
		}
		for _, fheader := range fheaders {
			snapBody, err = fheader.Open()
			origPath = fheader.Filename
			if err != nil {
				return BadRequest(`cannot open uploaded "snap" file: %v`, err)
			}
			defer snapBody.Close()

			break out
		}
	}
	defer form.RemoveAll()

	if snapBody == nil {
		return BadRequest(`cannot find "snap" file field in provided multipart/form-data payload`)
	}

	// we are in charge of the tempfile life cycle until we hand it off to the change
	changeTriggered := false
	// if you change this prefix, look for it in the tests
	// also see localInstallCleanup in snapstate/snapmgr.go
	tmpf, err := ioutil.TempFile(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix)
	if err != nil {
		return InternalError("cannot create temporary file: %v", err)
	}

	tempPath := tmpf.Name()

	defer func() {
		if !changeTriggered {
			os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(tmpf, snapBody); err != nil {
		return InternalError("cannot copy request into temporary file: %v", err)
	}
	tmpf.Sync()

	if len(form.Value["snap-path"]) > 0 {
		origPath = form.Value["snap-path"][0]
	}

	var instanceName string

	if len(form.Value["name"]) > 0 {
		// caller has specified desired instance name
		instanceName = form.Value["name"][0]
		if err := snap.ValidateInstanceName(instanceName); err != nil {
			return BadRequest(err.Error())
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var snapName string
	var sideInfo *snap.SideInfo

	if !dangerousOK {
		si, err := snapasserts.DeriveSideInfo(tempPath, assertstate.DB(st))
		switch {
		case err == nil:
			snapName = si.RealName
			sideInfo = si
		case asserts.IsNotFound(err):
			// with devmode we try to find assertions but it's ok
			// if they are not there (implies --dangerous)
			if !isTrue(form, "devmode") {
				msg := "cannot find signatures with metadata for snap"
				if origPath != "" {
					msg = fmt.Sprintf("%s %q", msg, origPath)
				}
				return BadRequest(msg)
			}
			// TODO: set a warning if devmode
		default:
			return BadRequest(err.Error())
		}
	}

	if snapName == "" {
		// potentially dangerous but dangerous or devmode params were set
		info, err := unsafeReadSnapInfo(tempPath)
		if err != nil {
			return BadRequest("cannot read snap file: %v", err)
		}
		snapName = info.SnapName()
		sideInfo = &snap.SideInfo{RealName: snapName}
	}

	if instanceName != "" {
		requestedSnapName := snap.InstanceSnap(instanceName)
		if requestedSnapName != snapName {
			return BadRequest(fmt.Sprintf("instance name %q does not match snap name %q", instanceName, snapName))
		}
	} else {
		instanceName = snapName
	}

	msg := fmt.Sprintf(i18n.G("Install %q snap from file"), instanceName)
	if origPath != "" {
		msg = fmt.Sprintf(i18n.G("Install %q snap from file %q"), instanceName, origPath)
	}

	tset, _, err := snapstateInstallPath(st, sideInfo, tempPath, instanceName, "", flags)
	if err != nil {
		return errToResponse(err, []string{snapName}, InternalError, "cannot install snap file: %v")
	}

	chg := newChange(st, "install-snap", msg, []*state.TaskSet{tset}, []string{instanceName})
	chg.Set("api-data", map[string]string{"snap-name": instanceName})

	ensureStateSoon(st)

	// only when the unlock succeeds (as opposed to panicing) is the handoff done
	// but this is good enough
	changeTriggered = true

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

func unsafeReadSnapInfoImpl(snapPath string) (*snap.Info, error) {
	// Condider using DeriveSideInfo before falling back to this!
	snapf, err := snapfile.Open(snapPath)
	if err != nil {
		return nil, err
	}
	return snap.ReadInfoFromSnapFile(snapf, nil)
}

var unsafeReadSnapInfo = unsafeReadSnapInfoImpl
