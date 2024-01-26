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
	"encoding/json"
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
)

var sealCmd = &Command{
	Path:        "/v2/system-reseal",
	POST:        postReseal,
	WriteAccess: rootAccess{},
}

type resealData struct {
	Reboot bool `json:"reboot"`
}

func postReseal(c *Command, r *http.Request, user *auth.UserState) Response {
	var data resealData
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&data); err != nil {
		return BadRequest("cannot decode request body into a resealData: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	chg := devicestate.Reseal(st, data.Reboot)
	ensureStateSoon(st)
	return AsyncResponse(nil, chg.ID())
}
