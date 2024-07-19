// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2024 Canonical Ltd
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
	"fmt"
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var (
	snapIconCmd = &Command{
		Path:       "/v2/icons/{name}/icon",
		GET:        snapIconGet,
		ReadAccess: openAccess{},
	}

	snapAppIconCmd = &Command{
		Path:       "/v2/icons/{snap}/icon/{app}",
		GET:        snapAppIconGet,
		ReadAccess: openAccess{},
	}
)

func snapIconGet(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	name := vars["name"]

	return iconGet(c.d.overlord.State(), name)
}

func iconGet(st *state.State, name string) Response {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, name, &snapst)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return SnapNotFound(name, err)
		}
		return InternalError("cannot consult state: %v", err)
	}
	sideInfo := snapst.CurrentSideInfo()
	if sideInfo == nil {
		return NotFound("snap has no current revision")
	}

	icon := snapIcon(snap.MinimalPlaceInfo(name, sideInfo.Revision))

	if icon == "" {
		return NotFound("local snap has no icon")
	}

	return fileResponse(icon)
}

func snapAppIconGet(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	snap := vars["snap"]
	app := vars["app"]

	snapInfo, err := localSnapInfo(c.d.overlord.State(), snap)
	if err != nil {
		if errors.Is(err, errNoSnap) {
			return SnapNotFound(snap, err)
		}
		return InternalError(fmt.Sprintf("%v", err))
	}

	for _, appInfo := range snapInfo.info.Apps {
		if appInfo.Name != app {
			continue
		}

		iconPath, err := iconPathFromDesktopFile(appInfo.DesktopFile())
		if err != nil {
			return NotFound("cannot find icon for app %q of snap %q", app, snap)
		}

		return fileResponse(iconPath)
	}

	return AppNotFound("snap %q has no app %q", snap, app)
}
