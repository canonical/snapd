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

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

var snapshotCmd = &Command{
	// TODO: also support /v2/snapshots/<id>
	Path:        "/v2/snapshots",
	GET:         listSnapshots,
	POST:        changeSnapshots,
	ReadAccess:  openAccess{},
	WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
}

var snapshotExportCmd = &Command{
	Path:       "/v2/snapshots/{id}/export",
	GET:        getSnapshotExport,
	ReadAccess: authenticatedAccess{},
}

var (
	snapshotList    = snapshotstate.List
	snapshotCheck   = snapshotstate.Check
	snapshotForget  = snapshotstate.Forget
	snapshotRestore = snapshotstate.Restore
	snapshotSave    = snapshotstate.Save
	snapshotExport  = snapshotstate.Export
	snapshotImport  = snapshotstate.Import
)

func listSnapshots(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	var setID uint64
	if sid := query.Get("set"); sid != "" {
		setID = mylog.Check2(strconv.ParseUint(sid, 10, 64))
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	sets := mylog.Check2(snapshotList(context.TODO(), st, setID, strutil.CommaSeparatedList(r.URL.Query().Get("snaps"))))

	return SyncResponse(sets)
}

// A snapshotAction is used to request an operation on a snapshot
// keep this in sync with client/snapshotAction...
type snapshotAction struct {
	SetID  uint64   `json:"set"`
	Action string   `json:"action"`
	Snaps  []string `json:"snaps,omitempty"`
	Users  []string `json:"users,omitempty"`
}

func (action snapshotAction) String() string {
	// verb of snapshot #N [for snaps %q] [for users %q]
	var snaps string
	var users string
	if len(action.Snaps) > 0 {
		snaps = " for snaps " + strutil.Quoted(action.Snaps)
	}
	if len(action.Users) > 0 {
		users = " for users " + strutil.Quoted(action.Users)
	}
	return fmt.Sprintf("%s of snapshot set #%d%s%s", strings.Title(action.Action), action.SetID, snaps, users)
}

func changeSnapshots(c *Command, r *http.Request, user *auth.UserState) Response {
	contentType := r.Header.Get("Content-Type")
	if contentType == client.SnapshotExportMediaType {
		return doSnapshotImport(c, r, user)
	}

	var action snapshotAction
	decoder := json.NewDecoder(r.Body)
	mylog.Check(decoder.Decode(&action))

	if decoder.More() {
		return BadRequest("extra content found after snapshot operation")
	}

	if action.SetID == 0 {
		return BadRequest("snapshot operation requires snapshot set ID")
	}

	if action.Action == "" {
		return BadRequest("snapshot operation requires action")
	}

	var affected []string
	var ts *state.TaskSet

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	switch action.Action {
	case "check":
		affected, ts = mylog.Check3(snapshotCheck(st, action.SetID, action.Snaps, action.Users))
	case "restore":
		affected, ts = mylog.Check3(snapshotRestore(st, action.SetID, action.Snaps, action.Users))
	case "forget":
		if len(action.Users) != 0 {
			return BadRequest(`snapshot "forget" operation cannot specify users`)
		}
		affected, ts = mylog.Check3(snapshotForget(st, action.SetID, action.Snaps))
	default:
		return BadRequest("unknown snapshot operation %q", action.Action)
	}

	switch err {
	case nil:
		// woo
	case client.ErrSnapshotSetNotFound, client.ErrSnapshotSnapsNotFound:
		return NotFound("%v", err)
	default:
		return InternalError("%v", err)
	}

	chg := newChange(st, action.Action+"-snapshot", action.String(), []*state.TaskSet{ts}, affected)
	chg.Set("api-data", map[string]interface{}{"snap-names": affected})
	ensureStateSoon(st)

	return AsyncResponse(nil, chg.ID())
}

// getSnapshotExport streams an archive containing an export of existing snapshots.
//
// The snapshots are re-packaged into a single uncompressed tar archive and
// internally contain multiple zip files.
func getSnapshotExport(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	vars := muxVars(r)
	sid := vars["id"]
	setID := mylog.Check2(strconv.ParseUint(sid, 10, 64))

	export := mylog.Check2(snapshotExport(context.TODO(), st, setID))

	// init (size calculation) can be slow so drop the lock
	st.Unlock()
	mylog.Check(export.Init())
	st.Lock()

	return &snapshotExportResponse{SnapshotExport: export, setID: setID, st: st}
}

func doSnapshotImport(c *Command, r *http.Request, user *auth.UserState) Response {
	defer r.Body.Close()

	expectedSize := mylog.Check2(strconv.ParseInt(r.Header.Get("Content-Length"), 10, 64))

	// ensure we don't read more than we expect
	limitedBodyReader := io.LimitReader(r.Body, expectedSize)

	// XXX: check that we have enough space to import the compressed snapshots
	st := c.d.overlord.State()
	setID, snapNames := mylog.Check3(snapshotImport(context.TODO(), st, limitedBodyReader))

	result := map[string]interface{}{"set-id": setID, "snaps": snapNames}
	return SyncResponse(result)
}

func snapshotMany(inst *snapInstruction, st *state.State) (*snapInstructionResult, error) {
	setID, snapshotted, ts := mylog.Check4(snapshotSave(st, inst.Snaps, inst.Users, inst.SnapshotOptions))

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
