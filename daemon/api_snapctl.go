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
	"net/http"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
)

var (
	snapctlCmd = &Command{
		Path:   "/v2/snapctl",
		SnapOK: true,
		POST:   runSnapctl,
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

	_, uid, _, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %s", err)
	}

	// Ignore missing context error to allow 'snapctl -h' without a context;
	// Actual context is validated later by get/set.
	context, _ := c.d.overlord.HookManager().Context(snapctlPostData.ContextID)

	// make the data read from stdin available for the hook
	// TODO: use a forwarded stdin here
	if snapctlPostData.Stdin != nil {
		context.Lock()
		context.Set("stdin", snapctlPostData.Stdin)
		context.Unlock()
	}

	stdout, stderr, err := ctlcmdRun(context, snapctlPostData.Args, uid)
	if err != nil {
		if e, ok := err.(*ctlcmd.UnsuccessfulError); ok {
			result := map[string]interface{}{
				"stdout":    string(stdout),
				"stderr":    string(stderr),
				"exit-code": e.ExitCode,
			}
			return &resp{
				Type: ResponseTypeError,
				Result: &errorResult{
					Message: e.Error(),
					Kind:    client.ErrorKindUnsuccessful,
					Value:   result,
				},
				Status: 200,
			}
		}
		if e, ok := err.(*ctlcmd.ForbiddenCommandError); ok {
			return Forbidden(e.Error())
		}
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			stdout = []byte(e.Error())
		} else {
			return BadRequest("error running snapctl: %s", err)
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

	return SyncResponse(result, nil)
}
