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

var (
	fdestateReplaceRecoveryKey = fdestate.ReplaceRecoveryKey
)

type systemVolumesActionRequest struct {
	Action string `json:"action"`

	Keyslots []fdestate.KeyslotTarget `json:"keyslots,omitempty"`

	RecoveryKey    string   `json:"recovery-key"`
	ContainerRoles []string `json:"container-roles"`
	// KeyID is the recovery key id.
	KeyID string `json:"key-id"`
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
	case "replace-recovery-key":
		return postSystemVolumesActionReplaceRecoveryKey(c, &req)
	default:
		return BadRequest("unsupported system volumes action %q", req.Action)
	}
}

var fdeMgrGenerateRecoveryKey = (*fdestate.FDEManager).GenerateRecoveryKey

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

var fdeMgrCheckRecoveryKey = (*fdestate.FDEManager).CheckRecoveryKey

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
		// TODO:FDEM: distinguish between failure due to a bad key and an
		// actual internal error where snapd fails to do the check.
		return BadRequest("cannot find matching recovery key: %v", err)
	}

	return SyncResponse(nil)
}

func postSystemVolumesActionReplaceRecoveryKey(c *Command, req *systemVolumesActionRequest) Response {
	if req.KeyID == "" {
		return BadRequest("system volume action requires key-id to be provided")
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if len(req.Keyslots) == 0 {
		// target default-recovery key slots by default if no key slot targets are specified
		req.Keyslots = append(req.Keyslots,
			fdestate.KeyslotTarget{ContainerRole: "system-data", Name: "default-recovery"},
			fdestate.KeyslotTarget{ContainerRole: "system-save", Name: "default-recovery"},
		)
	}

	ts, err := fdestateReplaceRecoveryKey(st, req.KeyID, req.Keyslots)
	if err != nil {
		return BadRequest("cannot change recovery key: %v", err)
	}

	chg := st.NewChange("replace-recovery-key", "Replace recovery key")
	chg.AddAll(ts)

	st.EnsureBefore(0)

	return AsyncResponse(nil, chg.ID())
}
