// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"errors"
	"net/http"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var snapFileCmd = &Command{
	Path:       "/v2/snaps/{name}/file",
	GET:        getSnapFile,
	ReadAccess: openAccess{},
}

func getSnapFile(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	name := vars["name"]

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	var info *snap.Info
	mylog.Check(snapstate.Get(st, name, &snapst))
	if err == nil {
		info = mylog.Check2(snapst.CurrentInfo())
	}

	if !snapst.Active {
		return BadRequest("cannot download file of inactive snap %q", name)
	}
	if snapst.TryMode {
		return BadRequest("cannot download file for try-mode snap %q", name)
	}

	return fileResponse(info.MountFile())
}
