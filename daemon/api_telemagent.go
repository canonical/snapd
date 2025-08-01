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
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
)

var (
	// see daemon.go:canAccess for details how the access is controlled
	deviceSessionCmd = &Command{
		Path:        "/v2/devicesession",
		GET:         getDeviceSession,
		ReadAccess:  openAccess{}, //TODO: this is open for now just for testing, but the goal is to make it protected
	}


)

func getDeviceSession(c *Command, r *http.Request, user *auth.UserState) Response {

	st := c.d.state
	st.Lock()
	defer st.Unlock()
	
	device, err := devicestatetest.Device(st)
	if err != nil {
		return SyncResponse(err)
	}


	return SyncResponse([]string{device.SessionMacaroon})
}

