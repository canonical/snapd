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
	"fmt"
	"net/http"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/strutil"
)

var systemVolumesCmd = &Command{
	Path:        "/v2/system-volumes",
	GET:         getSystemVolumes,
	POST:        postSystemVolumesAction,
	ReadAccess:  rootAccess{},
	WriteAccess: rootAccess{},
}

var (
	fdestateReplaceRecoveryKey = fdestate.ReplaceRecoveryKey
	fdeMgrGetKeyslots          = (*fdestate.FDEManager).GetKeyslots
	fdeMgrGenerateRecoveryKey  = (*fdestate.FDEManager).GenerateRecoveryKey
	fdeMgrCheckRecoveryKey     = (*fdestate.FDEManager).CheckRecoveryKey
	snapstateGadgetInfo        = snapstate.GadgetInfo
)

type systemVolumesResponse struct {
	ByContainerRole map[string]volumeInfo `json:"by-container-role,omitempty"`
}

type volumeInfo struct {
	ContainerRole string
	VolumeName    string                 `json:"volume-name"`
	Name          string                 `json:"name"`
	Encrypted     bool                   `json:"encrypted"`
	Keyslots      map[string]keyslotInfo `json:"keyslots,omitempty"`
}

type keyslotInfo struct {
	Type fdestate.KeyslotType `json:"type"`
	// only for platform key slots
	Roles        []string        `json:"roles,omitempty"`
	PlatformName string          `json:"platform-name,omitempty"`
	AuthMode     device.AuthMode `json:"auth-mode,omitempty"`
}

func getSystemVolumes(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	containerRoles := query["container-role"]
	byContainerRole := query.Get("by-container-role") == "true"
	if len(containerRoles) > 0 && byContainerRole {
		return BadRequest(`query parameter "container-role" conflicts with "by-container-role"`)
	}

	volumes, err := getAllVolumes(c.d.overlord.State(), c.d.overlord.FDEManager())
	if err != nil {
		return InternalError(err.Error())
	}

	res := systemVolumesResponse{
		ByContainerRole: make(map[string]volumeInfo),
	}
	for _, volume := range volumes {
		switch {
		case len(containerRoles) > 0:
			if strutil.ListContains(containerRoles, volume.ContainerRole) {
				res.ByContainerRole[volume.ContainerRole] = volume
			}
		case byContainerRole:
			res.ByContainerRole[volume.ContainerRole] = volume
		default:
			// all groupings, currently only by-container-role is supported.
			res.ByContainerRole[volume.ContainerRole] = volume
		}
	}

	return SyncResponse(res)
}

func getAllVolumes(st *state.State, fdemgr *fdestate.FDEManager) ([]volumeInfo, error) {
	st.Lock()
	defer st.Unlock()

	gadgetInfo, err := getCurrentGadgetInfo(st)
	if err != nil {
		return nil, fmt.Errorf("failed to get gadget info: %v", err)
	}

	keyslots, _, err := fdeMgrGetKeyslots(fdemgr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get key slots: %v", err)
	}
	keyslotsByContainerRole := make(map[string][]fdestate.Keyslot)
	for _, keyslot := range keyslots {
		keyslotsByContainerRole[keyslot.ContainerRole] = append(keyslotsByContainerRole[keyslot.ContainerRole], keyslot)
	}

	var volumes []volumeInfo
	for _, gv := range gadgetInfo.Volumes {
		for _, gs := range gv.Structure {
			if gs.Role == "" {
				continue
			}

			containerKeyslots := keyslotsByContainerRole[gs.Role]
			encrypted := len(containerKeyslots) != 0
			volume := volumeInfo{
				ContainerRole: gs.Role,
				VolumeName:    gv.Name,
				Name:          gs.Name,
				Encrypted:     encrypted,
			}
			if encrypted {
				volume.Keyslots = make(map[string]keyslotInfo, len(containerKeyslots))
			}

			for _, keyslot := range containerKeyslots {
				data := keyslotInfo{
					Type: keyslot.Type,
				}
				if keyslot.Type == fdestate.KeyslotTypePlatform {
					kd, err := keyslot.KeyData()
					if err != nil {
						return nil, err
					}
					data.PlatformName = kd.PlatformName()
					data.Roles = kd.Roles()
					data.AuthMode = kd.AuthMode()
				}
				volume.Keyslots[keyslot.Name] = data
			}
			volumes = append(volumes, volume)
		}
	}

	return volumes, nil
}

func getCurrentGadgetInfo(st *state.State) (*gadget.Info, error) {
	deviceCtx, err := devicestate.DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, err
	}
	gadgetInfo, err := snapstateGadgetInfo(st, deviceCtx)
	if err != nil {
		return nil, err
	}
	gadgetDir := gadgetInfo.MountDir()

	model := deviceCtx.Model()

	info, err := gadget.ReadInfo(gadgetDir, model)
	if err != nil {
		return nil, err
	}
	return info, nil
}

type systemVolumesActionRequest struct {
	Action string `json:"action"`

	Keyslots []fdestate.KeyslotRef `json:"keyslots"`

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

	ts, err := fdestateReplaceRecoveryKey(st, req.KeyID, req.Keyslots)
	if err != nil {
		return BadRequest("cannot replace recovery key: %v", err)
	}

	chg := st.NewChange("replace-recovery-key", "Replace recovery key")
	chg.AddAll(ts)

	st.EnsureBefore(0)

	return AsyncResponse(nil, chg.ID())
}
