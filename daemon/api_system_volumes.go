// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/secboot/keys"
)

var systemVolumesCmd = &Command{
	Path:        "/v2/system-volumes",
	POST:        postSystemVolumesAction,
	WriteAccess: rootAccess{},
}

type systemVolumesActionRequest struct {
	Action string `json:"action"`

	RecoveryKey    string   `json:"recovery-key"`
	ContainerRoles []string `json:"container-roles"`
}

func postSystemVolumesAction(c *Command, r *http.Request, user *auth.UserState) Response {
	contentType := r.Header.Get("Content-Type")

	switch contentType {
	case "application/json":
		return postSystemVolumesActionJSON(c, r)
	default:
		return BadRequest("unexpected content type: %q", contentType)
	}
}

func postSystemVolumesActionJSON(c *Command, r *http.Request) Response {
	var req systemVolumesActionRequest

	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&req); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}

	if decoder.More() {
		return BadRequest("extra content found in request body")
	}

	switch req.Action {
	case "generate-recovery-key":
		return postSystemVolumesActionGenerateRecoveryKey(c)
	case "check-recovery-key":
		return postSystemVolumesActionCheckRecoveryKey(c, &req)
	default:
		return BadRequest("unsupported system volumes action %q", req.Action)
	}
}

var fdeMgrGenerateRecoveryKey = func(fdemgr *fdestate.FDEManager) (rkey keys.RecoveryKey, keyID string, err error) {
	return fdemgr.GenerateRecoveryKey()
}

func postSystemVolumesActionGenerateRecoveryKey(c *Command) Response {
	fdemgr := c.d.overlord.FDEManager()

	rkey, keyID, err := fdeMgrGenerateRecoveryKey(fdemgr)
	if err != nil {
		return InternalError(err.Error())
	}

	return SyncResponse(map[string]string{
		"recovery-key": rkey.String(),
		"key-id":       keyID,
	})
}

var fdeMgrCheckRecoveryKey = func(fdemgr *fdestate.FDEManager, rkey keys.RecoveryKey, containerRoles []string) (err error) {
	return fdemgr.CheckRecoveryKey(rkey, containerRoles)
}

func postSystemVolumesActionCheckRecoveryKey(c *Command, req *systemVolumesActionRequest) Response {
	if req.RecoveryKey == "" {
		return BadRequest("system volume action requires recovery-key to be provided")
	}

	rkey, err := keys.ParseRecoveryKey(req.RecoveryKey)
	if err != nil {
		return BadRequest("cannot parse recovery key: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	fdemgr := c.d.overlord.FDEManager()
	if err := fdeMgrCheckRecoveryKey(fdemgr, rkey, req.ContainerRoles); err != nil {
		return BadRequest("cannot find matching recovery key: %v", err)
	}

	return SyncResponse(nil)
}
