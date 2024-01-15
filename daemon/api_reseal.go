// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/devicestate"
)

var sealCmd = &Command{
	Path:        "/v2/reseal",
	POST:        postReseal,
	WriteAccess: rootAccess{},
}

func postReseal(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	chg := devicestate.Reseal(st)
	ensureStateSoon(st)
	return AsyncResponse(nil, chg.ID())
}
