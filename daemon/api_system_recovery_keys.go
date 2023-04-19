// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

var systemRecoveryKeysCmd = &Command{
	Path:        "/v2/system-recovery-keys",
	GET:         getSystemRecoveryKeys,
	POST:        postSystemRecoveryKeys,
	ReadAccess:  rootAccess{},
	WriteAccess: rootAccess{},
}

func getSystemRecoveryKeys(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	keys, err := c.d.overlord.DeviceManager().EnsureRecoveryKeys()
	if err != nil {
		return InternalError(err.Error())
	}

	return SyncResponse(keys)
}

var deviceManagerRemoveRecoveryKeys = (*devicestate.DeviceManager).RemoveRecoveryKeys

type postSystemRecoveryKeysData struct {
	Action string `json:"action"`
}

func postSystemRecoveryKeys(c *Command, r *http.Request, user *auth.UserState) Response {

	var postData postSystemRecoveryKeysData

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postData); err != nil {
		return BadRequest("cannot decode recovery keys action data from request body: %v", err)
	}
	if decoder.More() {
		return BadRequest("spurious content after recovery keys action")
	}
	switch postData.Action {
	case "":
		return BadRequest("missing recovery keys action")
	default:
		return BadRequest("unsupported recovery keys action %q", postData.Action)
	case "remove":
		// only currently supported action
	}
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	err := deviceManagerRemoveRecoveryKeys(c.d.overlord.DeviceManager())
	if err != nil {
		return InternalError(err.Error())
	}
	return SyncResponse(nil)
}
