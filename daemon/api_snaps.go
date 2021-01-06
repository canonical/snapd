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
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/strutil"
)

var (
	// see daemon.go:canAccess for details how the access is controlled
	snapsCmd = &Command{
		Path:     "/v2/snaps",
		UserOK:   true,
		PolkitOK: "io.snapcraft.snapd.manage",
		GET:      getSnapsInfo,
		POST:     postSnaps,
	}
)

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

	chg.Set("api-data", map[string]interface{}{"snap-names": res.Affected})

	return AsyncResponse(res.Result, &Meta{Change: chg.ID()})
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
