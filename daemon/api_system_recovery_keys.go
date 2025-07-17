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

// newRecoveryKeyAPISupported returns true if this system should use the new
// FDE/recovery key APIs.
//
// TODO: usage of this function and the routes we support should be reviewed for
// core26
func newRecoveryKeyAPISupported(st *state.State) (bool, error) {
	deviceCtx, err := snapstate.DeviceCtx(st, nil, nil)

	// TODO: if we don't yet have a model, assume that it should be supported?
	// is that the right thing to do? unsure if this is a realistic scenario,
	// but it does happen in tests
	if err != nil {
		return true, nil
	}

	supported, err := install.PreinstallCheckSupported(deviceCtx.Model())
	if err != nil {
		return false, err
	}

	return supported, nil
}

func newRecoveryKeyAPISupportedLocking(st *state.State) (bool, error) {
	st.Lock()
	defer st.Unlock()
	return newRecoveryKeyAPISupported(st)
}

func getSystemRecoveryKeys(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	supported, err := newRecoveryKeyAPISupported(st)
	if err != nil {
		return InternalError(err.Error())
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

	supported, err := newRecoveryKeyAPISupported(st)
	if err != nil {
		return InternalError(err.Error())
	}

	// systems that support the new APIs should use those
	if supported {
		return BadRequest("this action is not supported on 25.10+ classic systems")
	}

	err = deviceManagerRemoveRecoveryKeys(c.d.overlord.DeviceManager())
	if err != nil {
		return InternalError(err.Error())
	}
	return SyncResponse(nil)
}
