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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var aliasesCmd = &Command{
	Path:        "/v2/aliases",
	GET:         getAliases,
	POST:        changeAliases,
	ReadAccess:  openAccess{},
	WriteAccess: authenticatedAccess{},
}

// aliasAction is an action performed on aliases
type aliasAction struct {
	Action string `json:"action"`
	Snap   string `json:"snap"`
	App    string `json:"app"`
	Alias  string `json:"alias"`
	// old now unsupported api
	Aliases []string `json:"aliases"`
}

func changeAliases(c *Command, r *http.Request, user *auth.UserState) Response {
	var a aliasAction
	decoder := json.NewDecoder(r.Body)
	mylog.Check(decoder.Decode(&a))

	if len(a.Aliases) != 0 {
		return BadRequest("cannot interpret request, snaps can no longer be expected to declare their aliases")
	}

	var taskset *state.TaskSet

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	switch a.Action {
	default:
		return BadRequest("unsupported alias action: %q", a.Action)
	case "alias":
		taskset = mylog.Check2(snapstate.Alias(st, a.Snap, a.App, a.Alias))
	case "unalias":
		if a.Alias == a.Snap {
			// Do What I mean:
			// check if a snap is referred/intended
			// or just an alias
			var snapst snapstate.SnapState
			mylog.Check(snapstate.Get(st, a.Snap, &snapst))
			if err != nil && !errors.Is(err, state.ErrNoState) {
				return InternalError("%v", err)
			}
			if errors.Is(err, state.ErrNoState) { // not a snap
				a.Snap = ""
			}
		}
		if a.Snap != "" {
			a.Alias = ""
			taskset = mylog.Check2(snapstate.DisableAllAliases(st, a.Snap))
		} else {
			taskset, a.Snap = mylog.Check3(snapstate.RemoveManualAlias(st, a.Alias))
		}
	case "prefer":
		taskset = mylog.Check2(snapstate.Prefer(st, a.Snap))
	}

	var summary string
	switch a.Action {
	case "alias":
		summary = fmt.Sprintf(i18n.G("Setup alias %q => %q for snap %q"), a.Alias, a.App, a.Snap)
	case "unalias":
		if a.Alias != "" {
			summary = fmt.Sprintf(i18n.G("Remove manual alias %q for snap %q"), a.Alias, a.Snap)
		} else {
			summary = fmt.Sprintf(i18n.G("Disable all aliases for snap %q"), a.Snap)
		}
	case "prefer":
		summary = fmt.Sprintf(i18n.G("Prefer aliases of snap %q"), a.Snap)
	}

	change := newChange(st, a.Action, summary, []*state.TaskSet{taskset}, []string{a.Snap})
	st.EnsureBefore(0)

	return AsyncResponse(nil, change.ID())
}

type aliasStatus struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Manual  string `json:"manual,omitempty"`
	Auto    string `json:"auto,omitempty"`
}

// getAliases produces a response with a map snap -> alias -> aliasStatus
func getAliases(c *Command, r *http.Request, user *auth.UserState) Response {
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	res := make(map[string]map[string]aliasStatus)

	allStates := mylog.Check2(snapstate.All(state))

	for snapName, snapst := range allStates {
		if len(snapst.Aliases) != 0 {
			snapAliases := make(map[string]aliasStatus)
			res[snapName] = snapAliases
			autoDisabled := snapst.AutoAliasesDisabled
			for alias, aliasTarget := range snapst.Aliases {
				aliasStatus := aliasStatus{
					Manual: aliasTarget.Manual,
					Auto:   aliasTarget.Auto,
				}
				status := "auto"
				tgt := aliasTarget.Effective(autoDisabled)
				if tgt == "" {
					status = "disabled"
					tgt = aliasTarget.Auto
				} else if aliasTarget.Manual != "" {
					status = "manual"
				}
				aliasStatus.Status = status
				aliasStatus.Command = snap.JoinSnapApp(snapName, tgt)
				snapAliases[alias] = aliasStatus
			}
		}
	}

	return SyncResponse(res)
}
