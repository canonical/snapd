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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
)

var snapctlCmd = &Command{
	Path:        "/v2/snapctl",
	POST:        runSnapctl,
	WriteAccess: snapAccess{},
}

var ctlcmdRun = ctlcmd.Run

func runSnapctl(c *Command, r *http.Request, user *auth.UserState) Response {
	var snapctlPostData client.SnapCtlPostData
	mylog.Check(jsonutil.DecodeWithNumber(r.Body, &snapctlPostData))

	if len(snapctlPostData.Args) == 0 {
		return BadRequest("snapctl cannot run without args")
	}

	ucred := mylog.Check2(ucrednetGet(r.RemoteAddr))

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

	stdout, stderr := mylog.Check3(ctlcmdRun(context, snapctlPostData.Args, ucred.Uid))

	if context != nil && context.IsEphemeral() {
		context.Lock()
		defer context.Unlock()
		mylog.Check(context.Done())

	}

	result := map[string]string{
		"stdout": string(stdout),
		"stderr": string(stderr),
	}

	return SyncResponse(result)
}
