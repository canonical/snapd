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
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

var snapshotCmd = &Command{
	// TODO: also support /v2/snapshots/<id>
	Path:     "/v2/snapshots",
	UserOK:   true,
	PolkitOK: "io.snapcraft.snapd.manage",
	GET:      listSnapshots,
	POST:     changeSnapshots,
}

func listSnapshots(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	var setID uint64
	if sid := query.Get("set"); sid != "" {
		var err error
		setID, err = strconv.ParseUint(sid, 10, 64)
		if err != nil {
			return BadRequest("'set', if given, must be a positive base 10 number; got %q", sid)
		}
	}

	sets, err := snapshotList(context.TODO(), setID, strutil.CommaSeparatedList(r.URL.Query().Get("snaps")))
	if err != nil {
		return InternalError("%v", err)
	}
	return SyncResponse(sets, nil)
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
	var action snapshotAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&action); err != nil {
		return BadRequest("cannot decode request body into snapshot operation: %v", err)
	}
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
	var err error

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	switch action.Action {
	case "check":
		affected, ts, err = snapshotCheck(st, action.SetID, action.Snaps, action.Users)
	case "restore":
		affected, ts, err = snapshotRestore(st, action.SetID, action.Snaps, action.Users)
	case "forget":
		if len(action.Users) != 0 {
			return BadRequest(`snapshot "forget" operation cannot specify users`)
		}
		affected, ts, err = snapshotForget(st, action.SetID, action.Snaps)
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

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}
