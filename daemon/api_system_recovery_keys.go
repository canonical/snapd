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
	"errors"
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

var systemRecoveryKeysCmd = &Command{
	Path:        "/v2/system-recovery-keys",
	GET:         getSystemRecoveryKeys,
	POST:        postSystemRecoveryKeys,
	Actions:     []string{"remove"},
	ReadAccess:  rootAccess{},
	WriteAccess: rootAccess{},
}

// systemVolumesAPISupported returns true if this system should use the new
// FDE/recovery key APIs.
//
// TODO: usage of this function and the routes we support should be reviewed for
// UC 26
func systemVolumesAPISupported(st *state.State) (bool, *apiError) {
	deviceCtx, err := snapstate.DeviceCtx(st, nil, nil)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return false, BadRequest("cannot use this API prior to device having a model")
		}
		return false, InternalError(err.Error())
	}

	supported, err := install.CheckHybridQuestingRelease(deviceCtx.Model())
	if err != nil {
		return false, InternalError(err.Error())
	}

	return supported, nil
}

func systemVolumesAPISupportedLocking(st *state.State) (bool, *apiError) {
	st.Lock()
	defer st.Unlock()
	return systemVolumesAPISupported(st)
}

func getSystemRecoveryKeys(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	supported, respErr := systemVolumesAPISupported(st)
	if respErr != nil {
		return respErr
	}

	// systems that support the new APIs should use those
	if supported {
		return BadRequest("this action is not supported on 25.10+ classic systems")
	}

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

	supported, respErr := systemVolumesAPISupported(st)
	if respErr != nil {
		return respErr
	}

	// systems that support the new APIs should use those
	if supported {
		return BadRequest("this action is not supported on 25.10+ classic systems")
	}

	err := deviceManagerRemoveRecoveryKeys(c.d.overlord.DeviceManager())
	if err != nil {
		return InternalError(err.Error())
	}
	return SyncResponse(nil)
}
