// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
	"net/http"
	"slices"

	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	snapctlCmd = &Command{
		Path:        "/v2/snapctl",
		POST:        runSnapctl,
		WriteAccess: snapAccess{},
	}
)

var ctlcmdRun = ctlcmd.Run

func runSnapctl(c *Command, r *http.Request, user *auth.UserState) Response {
	var snapctlPostData client.SnapCtlPostData

	if err := jsonutil.DecodeWithNumber(r.Body, &snapctlPostData); err != nil {
		return BadRequest("cannot decode snapctl request: %s", err)
	}

	if len(snapctlPostData.Args) == 0 {
		return BadRequest("snapctl cannot run without args")
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %s", err)
	}

	// Ignore missing context error to allow 'snapctl -h' without a context;
	// Actual context is validated later by get/set.
	context, _ := c.d.overlord.HookManager().Context(snapctlPostData.ContextID)

	// Make the data read from stdin available for the hook via the
	// context. If no context was found, calls to ensureContext() make sure
	// we return with error before stdin is actually used.
	// TODO: use a forwarded stdin here
	if snapctlPostData.Stdin != nil && context != nil {
		context.Lock()
		context.Set("stdin", snapctlPostData.Stdin)
		context.Unlock()
	}

	var features []string
	if header := r.Header.Get("X-Snapctl-Features"); header != "" {
		features = strings.Split(header, ",")
	}

	if slices.Contains(features, "async") {
		decoder := json.NewDecoder(r.Body)
		var inst snapInstruction
		if err := decoder.Decode(&inst); err != nil {
			return BadRequest("cannot decode request body into snap instruction: %v", err)
		}

		st := c.d.overlord.State()
		st.Lock()
		defer st.Unlock()

		if err := inst.validate(); err != nil {
			return BadRequest("%s", err)
		}

		impl := inst.dispatch()
		if impl == nil {
			return BadRequest("unknown action %s", inst.Action)
		}

		res, err := impl(r.Context(), &inst, st)
		if err != nil {
			return inst.errToResponse(err)
		}

		changeKind, ok := changeKind(inst.Action)
		if !ok {
			return BadRequest("unknown action %s", inst.Action)
		}

		chg := newChange(st, changeKind, res.Summary, res.Tasksets, res.Affected)
		if len(res.Tasksets) == 0 {
			chg.SetStatus(state.DoneStatus)
		}

		return AsyncResponse(nil, chg.ID())
	}

	stdout, stderr, err := ctlcmdRun(context, snapctlPostData.Args, ucred.Uid, features)
	if err != nil {
		if e, ok := err.(*ctlcmd.UnsuccessfulError); ok {
			result := map[string]any{
				"stdout":    string(stdout),
				"stderr":    string(stderr),
				"exit-code": e.ExitCode,
			}
			return &apiError{
				Status:  200,
				Message: e.Error(),
				Kind:    client.ErrorKindUnsuccessful,
				Value:   result,
			}
		}
		if e, ok := err.(*ctlcmd.ForbiddenCommandError); ok {
			return Forbidden(e.Error())
		}
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			stdout = []byte(e.Error())
		} else {
			return BadRequest("snapctl: %s", err)
		}
	}

	if context != nil && context.IsEphemeral() {
		context.Lock()
		defer context.Unlock()
		if err := context.Done(); err != nil {
			return BadRequest(i18n.G("set failed: %v"), err)
		}
	}

	result := map[string]string{
		"stdout": string(stdout),
		"stderr": string(stderr),
	}

	return SyncResponse(result)
}
