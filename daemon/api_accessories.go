// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2024 Canonical Ltd
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
)

var (
	accessoriesChangeCmd = &Command{
		Path: "/v2/accessories/changes/{id}",
		GET:  getAccessoriesChange,
		// TODO: expand this to other accessories APIs as they appear
		ReadAccess: interfaceOpenAccess{Interfaces: []string{"snap-themes-control"}},
	}
)

var allowedAccessoriesChanges = map[string]bool{
	"install-themes": true,
}

func getAccessoriesChange(c *Command, r *http.Request, user *auth.UserState) Response {
	chID := muxVars(r)["id"]
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()
	chg := state.Change(chID)

	// Only return information about theme install changes
	if chg == nil || !allowedAccessoriesChanges[chg.Kind()] {
		return NotFound("cannot find change with id %q", chID)
	}

	return SyncResponse(change2changeInfo(chg))
}
