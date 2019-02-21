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
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var snapBlobCmd = &Command{
	Path:     "/v2/snaps/{name}/blob",
	UserOK:   true,
	PolkitOK: "io.snapcraft.snapd.manage",
	GET:      getSnapBlob,
}

func getSnapBlob(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	name := vars["name"]

	st := c.d.overlord.State()
	st.Lock()
	var snapst snapstate.SnapState
	var info *snap.Info
	err := snapstate.Get(st, name, &snapst)
	if err == nil {
		info, err = snapst.CurrentInfo()
	}
	st.Unlock()
	switch err {
	case nil:
		// ok
	case state.ErrNoState:
		return SnapNotFound(name, err)
	default:
		return InternalError("cannot download blob for snap %q: %v", name, err)
	}
	if !snapst.Active {
		return BadRequest("cannot download blob of inactive snap %q", name)
	}
	if snapst.TryMode {
		return BadRequest("cannot download blob for try-mode snap %q", name)
	}

	return FileResponse(info.MountFile())
}
