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
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/strutil"
)

var systemVolumesCmd = &Command{
	Path: "/v2/system-volumes",
	GET:  getSystemVolumes,
	POST: postSystemVolumesAction,
	Actions: []string{
		"generate-recovery-key", "check-recovery-key", "add-recovery-key", "replace-recovery-key",
		"replace-platform-key", "check-passphrase", "check-pin", "change-passphrase", "change-pin"},
	// anyone can enumerate key slots.
	ReadAccess: interfaceOpenAccess{Interfaces: []string{"snap-fde-control"}},
	WriteAccess: byActionAccess{
		ByAction: map[string]accessChecker{
			// anyone check passphrase/pin quality
			"check-passphrase": interfaceOpenAccess{Interfaces: []string{"snap-fde-control"}},
			"check-pin":        interfaceOpenAccess{Interfaces: []string{"snap-fde-control"}},
			// anyone can change passphrase or PIN given they know the old passphrase
			// TODO:FDEM: rate limiting is needed to avoid DA lockout.
			"change-passphrase": interfaceOpenAccess{Interfaces: []string{"snap-fde-control"}},
			"change-pin":        interfaceOpenAccess{Interfaces: []string{"snap-fde-control"}},
			// only root and admins (authenticated via Polkit) can do recovery key
			// related actions.
			"check-recovery-key": interfaceRootAccess{
				// firmware-updater-support is allowed so that a user can verify
				// their recovery key is valid before doing the firmware update
				// which might require entering their recovery key on next boot.
				Interfaces: []string{"snap-fde-control", "firmware-updater-support"},
				Polkit:     polkitActionManageFDE,
			},
			"generate-recovery-key": interfaceRootAccess{
				Interfaces: []string{"snap-fde-control"},
				Polkit:     polkitActionManageFDE,
			},
			"add-recovery-key": interfaceRootAccess{
				Interfaces: []string{"snap-fde-control"},
				Polkit:     polkitActionManageFDE,
			},
			"replace-recovery-key": interfaceRootAccess{
				Interfaces: []string{"snap-fde-control"},
				Polkit:     polkitActionManageFDE,
			},
			"replace-platform-key": interfaceRootAccess{
				Interfaces: []string{"snap-fde-control"},
				Polkit:     polkitActionManageFDE,
			},
		},
		// by default, all actions are only allowed for root.
		Default: rootAccess{},
	},
}

var fdeAddRecoveryKeyChangeKind = swfeats.RegisterChangeKind("fde-add-recovery-key")
var fdeReplaceRecoveryKeyChangeKind = swfeats.RegisterChangeKind("fde-replace-recovery-key")
var fdeReplacePlatformKeyChangeKind = swfeats.RegisterChangeKind("fde-replace-platform-key")
var fdeChangePassphraseChangeKind = swfeats.RegisterChangeKind("fde-change-passphrase")
var fdeChangePINChangeKind = swfeats.RegisterChangeKind("fde-change-pin")

var (
	fdestateAddRecoveryKey     = fdestate.AddRecoveryKey
	fdestateReplaceRecoveryKey = fdestate.ReplaceRecoveryKey
	fdestateReplacePlatformKey = fdestate.ReplacePlatformKey
	fdestateChangeAuth         = fdestate.ChangeAuth
	fdeMgrGenerateRecoveryKey  = (*fdestate.FDEManager).GenerateRecoveryKey
	fdeMgrCheckRecoveryKey     = (*fdestate.FDEManager).CheckRecoveryKey

	devicestateGetVolumeStructuresWithKeyslots = devicestate.GetVolumeStructuresWithKeyslots
)

func parseSystemVolumesOptionsFromURL(q url.Values) (opts *client.SystemVolumesOptions, err error) {
	opts = &client.SystemVolumesOptions{
		ContainerRoles: q["container-role"],
	}
	switch q.Get("by-container-role") {
	case "true", "false", "":
		opts.ByContainerRole = q.Get("by-container-role") == "true"
	default:
		return nil, errors.New(`"by-container-role" query parameter when used must be set to "true" or "false" or left unset`)
	}
	if len(opts.ContainerRoles) > 0 && opts.ByContainerRole {
		return nil, errors.New(`"container-role" query parameter conflicts with "by-container-role"`)
	}
	return opts, nil
}

func structureInfoFromVolumeStructure(structure *devicestate.VolumeStructureWithKeyslots) (*client.SystemVolumesStructureInfo, error) {
	structureInfo := &client.SystemVolumesStructureInfo{
		VolumeName: structure.VolumeName,
		Name:       structure.Name,
		Encrypted:  len(structure.Keyslots) > 0,
	}
	if structureInfo.Encrypted {
		structureInfo.Keyslots = make(map[string]client.KeyslotInfo, len(structure.Keyslots))
	}
	for _, keyslot := range structure.Keyslots {
		keyslotInfo := client.KeyslotInfo{
			Type: client.KeyslotType(keyslot.Type),
		}
		if keyslot.Type == fdestate.KeyslotTypePlatform {
			kd, err := keyslot.KeyData()
			if err != nil {
				return nil, err
			}
			keyslotInfo.PlatformName = kd.PlatformName()
			keyslotInfo.Roles = kd.Roles()
			keyslotInfo.AuthMode = kd.AuthMode()
		}
		structureInfo.Keyslots[keyslot.Name] = keyslotInfo
	}
	return structureInfo, nil
}

func getSystemVolumes(c *Command, r *http.Request, user *auth.UserState) Response {
	supported, respErr := systemVolumesAPISupportedLocking(c.d.overlord.State())
	if respErr != nil {
		return respErr
	}

	if !supported {
		return BadRequest("this action is not supported on this system")
	}

	opts, err := parseSystemVolumesOptionsFromURL(r.URL.Query())
	if err != nil {
		return BadRequest(err.Error())
	}

	structures, err := func() ([]devicestate.VolumeStructureWithKeyslots, error) {
		c.d.state.Lock()
		defer c.d.state.Unlock()

		return devicestateGetVolumeStructuresWithKeyslots(c.d.state)

	}()
	if err != nil {
		return InternalError("cannot get encryption information for gadget volumes: %v", err)
	}

	res := client.SystemVolumesResult{
		ByContainerRole: make(map[string]client.SystemVolumesStructureInfo),
	}
	for _, structure := range structures {
		if structure.Role == "" {
			// ignore structures without a role until other grouping
			// requires them.
			continue
		}
		switch {
		// conversion is done only on a match do as little key data loading
		// as possible since it is lazy loaded.
		case len(opts.ContainerRoles) > 0:
			if strutil.ListContains(opts.ContainerRoles, structure.Role) {
				structureInfo, err := structureInfoFromVolumeStructure(&structure)
				if err != nil {
					return InternalError("cannot convert volume structure: %v", err)
				}
				res.ByContainerRole[structure.Role] = *structureInfo
			}
		case opts.ByContainerRole:
			structureInfo, err := structureInfoFromVolumeStructure(&structure)
			if err != nil {
				return InternalError("cannot convert volume structure: %v", err)
			}
			res.ByContainerRole[structure.Role] = *structureInfo
		default:
			// all groupings, currently only by-container-role is supported.
			structureInfo, err := structureInfoFromVolumeStructure(&structure)
			if err != nil {
				return InternalError("cannot convert volume structure: %v", err)
			}
			res.ByContainerRole[structure.Role] = *structureInfo
		}
	}
	return SyncResponse(res)
}

type systemVolumesActionRequest struct {
	Action string `json:"action"`

	Keyslots []fdestate.KeyslotRef `json:"keyslots"`

	RecoveryKey    string   `json:"recovery-key"`
	ContainerRoles []string `json:"container-roles"`
	// KeyID is the recovery key id.
	KeyID string `json:"key-id"`

	client.PlatformKeyOptions
	client.ChangePassphraseOptions
	client.ChangePINOptions
}

func postSystemVolumesAction(c *Command, r *http.Request, user *auth.UserState) Response {
	supported, respErr := systemVolumesAPISupportedLocking(c.d.overlord.State())
	if respErr != nil {
		return respErr
	}

	if !supported {
		return BadRequest("this action is not supported on this system")
	}

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

	// Expand target keyslots that do not specify a container role.
	if len(req.Keyslots) != 0 {
		expanded := make([]fdestate.KeyslotRef, 0, len(req.Keyslots))
		exists := make(map[string]bool)
		for _, ref := range req.Keyslots {
			if len(ref.ContainerRole) == 0 {
				// Not specifying the container-role field implicitly means
				// targeting all system containers i.e. system-data and
				// system-save.
				expandedRefs := fdestate.ExpandSystemContainerRoles(ref.Name)
				for _, expandedRef := range expandedRefs {
					if exists[expandedRef.String()] {
						return BadRequest("invalid keyslots: duplicate implicit keyslot found %s", expandedRef.String())
					}
					expanded = append(expanded, expandedRef)
					exists[expandedRef.String()] = true
				}
			} else {
				if exists[ref.String()] {
					return BadRequest("invalid keyslots: duplicate keyslot found %s", ref.String())
				}
				expanded = append(expanded, ref)
				exists[ref.String()] = true
			}
		}
		req.Keyslots = expanded
	}

	switch req.Action {
	case "generate-recovery-key":
		return postSystemVolumesActionGenerateRecoveryKey(c)
	case "check-recovery-key":
		return postSystemVolumesActionCheckRecoveryKey(c, &req)
	case "add-recovery-key":
		return postSystemVolumesActionAddRecoveryKey(c, &req)
	case "replace-recovery-key":
		return postSystemVolumesActionReplaceRecoveryKey(c, &req)
	case "replace-platform-key":
		return postSystemVolumesActionReplacePlatformKey(c, &req)
	case "check-passphrase":
		return postSystemVolumesCheckPassphrase(&req)
	case "check-pin":
		return postSystemVolumesCheckPIN(&req)
	case "change-passphrase":
		return postSystemVolumesActionChangePassphrase(c, &req)
	case "change-pin":
		return postSystemVolumesActionChangePIN(c, &req)
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
		rkeyErr := &fdestate.InvalidRecoveryKeyError{
			Reason:  fdestate.InvalidRecoveryKeyReasonInvalidFormat,
			Message: fmt.Sprintf("cannot parse recovery key: %v", err),
		}
		return InvalidRecoveryKey(rkeyErr)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	fdemgr := c.d.overlord.FDEManager()
	if err := fdeMgrCheckRecoveryKey(fdemgr, rkey, req.ContainerRoles); err != nil {
		return errToResponse(err, nil, BadRequest, "%v")
	}

	return SyncResponse(nil)
}

func postSystemVolumesActionAddRecoveryKey(c *Command, req *systemVolumesActionRequest) Response {
	if req.KeyID == "" {
		return BadRequest("system volume action requires key-id to be provided")
	}
	if len(req.Keyslots) == 0 {
		return BadRequest("system volume action requires keyslots to be provided")
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	ts, err := fdestateAddRecoveryKey(st, req.KeyID, req.Keyslots)
	if err != nil {
		return errToResponse(err, nil, BadRequest, "cannot add recovery key: %v")
	}

	chg := newChange(st, fdeAddRecoveryKeyChangeKind, "Add recovery key", []*state.TaskSet{ts}, nil)

	st.EnsureBefore(0)

	return AsyncResponse(nil, chg.ID())
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
		return errToResponse(err, nil, BadRequest, "cannot replace recovery key: %v")
	}

	chg := newChange(st, fdeReplaceRecoveryKeyChangeKind, "Replace recovery key", []*state.TaskSet{ts}, nil)

	st.EnsureBefore(0)

	return AsyncResponse(nil, chg.ID())
}

func postSystemVolumesActionReplacePlatformKey(c *Command, req *systemVolumesActionRequest) Response {
	if req.AuthMode == "" {
		return BadRequest("system volume action requires auth-mode to be provided")
	}

	var volumesAuth *device.VolumesAuthOptions
	if req.AuthMode != device.AuthModeNone {
		volumesAuth = &device.VolumesAuthOptions{
			Mode:       req.AuthMode,
			PIN:        req.PIN,
			Passphrase: req.Passphrase,
			KDFType:    req.KDFType,
			KDFTime:    req.KDFTime,
		}
		if err := volumesAuth.Validate(); err != nil {
			return BadRequest("invalid platform key options: %v", err)
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	ts, err := fdestateReplacePlatformKey(st, volumesAuth, req.Keyslots)
	if err != nil {
		return errToResponse(err, nil, BadRequest, "cannot replace platform key: %v")
	}

	chg := newChange(st, fdeReplacePlatformKeyChangeKind, "Replace platform key", []*state.TaskSet{ts}, nil)

	st.EnsureBefore(0)

	return AsyncResponse(nil, chg.ID())
}

func postSystemVolumesCheckPassphrase(req *systemVolumesActionRequest) Response {
	if req.Passphrase == "" {
		return BadRequest("passphrase must be provided in request body for action %q", req.Action)
	}

	return postCheckAuthQuality(device.AuthModePassphrase, req.Passphrase)
}

func postSystemVolumesCheckPIN(req *systemVolumesActionRequest) Response {
	if req.PIN == "" {
		return BadRequest("pin must be provided in request body for action %q", req.Action)
	}

	return postCheckAuthQuality(device.AuthModePIN, req.PIN)
}

func postSystemVolumesActionChangePassphrase(c *Command, req *systemVolumesActionRequest) Response {
	if req.OldPassphrase == "" {
		return BadRequest("system volume action requires old-passphrase to be provided")
	}
	if req.NewPassphrase == "" {
		return BadRequest("system volume action requires new-passphrase to be provided")
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	ts, err := fdestateChangeAuth(st, device.AuthModePassphrase, req.OldPassphrase, req.NewPassphrase, req.Keyslots)
	if err != nil {
		return errToResponse(err, nil, BadRequest, "cannot change passphrase: %v")
	}

	chg := newChange(st, fdeChangePassphraseChangeKind, "Change passphrase", []*state.TaskSet{ts}, nil)

	st.EnsureBefore(0)

	return AsyncResponse(nil, chg.ID())
}

func postSystemVolumesActionChangePIN(c *Command, req *systemVolumesActionRequest) Response {
	if req.OldPIN == "" {
		return BadRequest("system volume action requires old-pin to be provided")
	}
	if req.NewPIN == "" {
		return BadRequest("system volume action requires new-pin to be provided")
	}

	if err := device.ValidatePIN(req.OldPIN); err != nil {
		return BadRequest("cannot change pin: invalid old-pin: %v", err)
	}
	if err := device.ValidatePIN(req.NewPIN); err != nil {
		return BadRequest("cannot change pin: invalid new-pin: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	ts, err := fdestateChangeAuth(st, device.AuthModePIN, req.OldPIN, req.NewPIN, req.Keyslots)
	if err != nil {
		return errToResponse(err, nil, BadRequest, "cannot change pin: %v")
	}

	chg := newChange(st, fdeChangePINChangeKind, "Change pin", []*state.TaskSet{ts}, nil)

	st.EnsureBefore(0)

	return AsyncResponse(nil, chg.ID())
}
