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
	"os"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/snap"
)

var systemsCmd = &Command{
	Path:       "/v2/systems",
	GET:        getAllSystems,
	ReadAccess: authenticatedAccess{},
	// this is awkward, we want the postSystemsAction function to be used
	// when the label is empty too, but the router will not handle the request
	// for /v2/systems with the systemsActionCmd and instead handles it through
	// this command, so we need to set the POST for this command to essentially
	// forward to that one
	POST:        postSystemsAction,
	WriteAccess: rootAccess{},
}

var systemsActionCmd = &Command{
	Path:       "/v2/systems/{label}",
	GET:        getSystemDetails,
	ReadAccess: rootAccess{},

	POST:        postSystemsAction,
	WriteAccess: rootAccess{},
}

type systemsResponse struct {
	Systems []client.System `json:"systems,omitempty"`
}

func getAllSystems(c *Command, r *http.Request, user *auth.UserState) Response {
	var rsp systemsResponse

	seedSystems, err := c.d.overlord.DeviceManager().Systems()
	if err != nil {
		if err == devicestate.ErrNoSystems {
			// no systems available
			return SyncResponse(&rsp)
		}

		return InternalError(err.Error())
	}

	rsp.Systems = make([]client.System, 0, len(seedSystems))

	for _, ss := range seedSystems {
		// untangle the model

		actions := make([]client.SystemAction, 0, len(ss.Actions))
		for _, sa := range ss.Actions {
			actions = append(actions, client.SystemAction{
				Title: sa.Title,
				Mode:  sa.Mode,
			})
		}

		rsp.Systems = append(rsp.Systems, client.System{
			Current:               ss.Current,
			DefaultRecoverySystem: ss.DefaultRecoverySystem,
			Label:                 ss.Label,
			Model: client.SystemModelData{
				Model:       ss.Model.Model(),
				BrandID:     ss.Model.BrandID(),
				DisplayName: ss.Model.DisplayName(),
			},
			Brand: snap.StoreAccount{
				ID:          ss.Brand.AccountID(),
				Username:    ss.Brand.Username(),
				DisplayName: ss.Brand.DisplayName(),
				Validation:  ss.Brand.Validation(),
			},
			Actions: actions,
		})
	}
	return SyncResponse(&rsp)
}

// wrapped for unit tests
var deviceManagerSystemAndGadgetAndEncryptionInfo = func(dm *devicestate.DeviceManager, systemLabel string) (*devicestate.System, *gadget.Info, *install.EncryptionSupportInfo, error) {
	return dm.SystemAndGadgetAndEncryptionInfo(systemLabel)
}

func storageEncryption(encInfo *install.EncryptionSupportInfo) *client.StorageEncryption {
	if encInfo.Disabled {
		return &client.StorageEncryption{
			Support: client.StorageEncryptionSupportDisabled,
		}
	}
	storageEnc := &client.StorageEncryption{
		StorageSafety: string(encInfo.StorageSafety),
		Type:          string(encInfo.Type),
	}
	required := (encInfo.StorageSafety == asserts.StorageSafetyEncrypted)
	switch {
	case encInfo.Available:
		storageEnc.Support = client.StorageEncryptionSupportAvailable
	case !encInfo.Available && required:
		storageEnc.Support = client.StorageEncryptionSupportDefective
		storageEnc.UnavailableReason = encInfo.UnavailableErr.Error()
	case !encInfo.Available && !required:
		storageEnc.Support = client.StorageEncryptionSupportUnavailable
		storageEnc.UnavailableReason = encInfo.UnavailableWarning
	}

	return storageEnc
}

var (
	devicestateInstallFinish                 = devicestate.InstallFinish
	devicestateInstallSetupStorageEncryption = devicestate.InstallSetupStorageEncryption
)

func getSystemDetails(c *Command, r *http.Request, user *auth.UserState) Response {
	wantedSystemLabel := muxVars(r)["label"]

	deviceMgr := c.d.overlord.DeviceManager()

	sys, gadgetInfo, encryptionInfo, err := deviceManagerSystemAndGadgetAndEncryptionInfo(deviceMgr, wantedSystemLabel)
	if err != nil {
		return InternalError(err.Error())
	}

	rsp := client.SystemDetails{
		Current: sys.Current,
		Label:   sys.Label,
		Brand: snap.StoreAccount{
			ID:          sys.Brand.AccountID(),
			Username:    sys.Brand.Username(),
			DisplayName: sys.Brand.DisplayName(),
			Validation:  sys.Brand.Validation(),
		},
		// no body: we expect models to have empty bodies
		Model:             sys.Model.Headers(),
		Volumes:           gadgetInfo.Volumes,
		StorageEncryption: storageEncryption(encryptionInfo),
	}
	for _, sa := range sys.Actions {
		rsp.Actions = append(rsp.Actions, client.SystemAction{
			Title: sa.Title,
			Mode:  sa.Mode,
		})
	}

	return SyncResponse(rsp)
}

type systemActionRequest struct {
	Action string `json:"action"`

	client.SystemAction
	client.InstallSystemOptions
}

func postSystemsAction(c *Command, r *http.Request, user *auth.UserState) Response {
	var req systemActionRequest
	systemLabel := muxVars(r)["label"]

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		return BadRequest("cannot decode request body into system action: %v", err)
	}
	if decoder.More() {
		return BadRequest("extra content found in request body")
	}
	switch req.Action {
	case "do":
		return postSystemActionDo(c, systemLabel, &req)
	case "reboot":
		return postSystemActionReboot(c, systemLabel, &req)
	case "install":
		return postSystemActionInstall(c, systemLabel, &req)
	default:
		return BadRequest("unsupported action %q", req.Action)
	}
}

// XXX: should deviceManager return more sensible errors here? e.g.:
// UnsupportedActionError{systemLabel, mode}, SystemDoesNotExistError{systemLabel}
func handleSystemActionErr(err error, systemLabel string) Response {
	if os.IsNotExist(err) {
		return NotFound("requested seed system %q does not exist", systemLabel)
	}
	if err == devicestate.ErrUnsupportedAction {
		return BadRequest("requested action is not supported by system %q", systemLabel)
	}
	return InternalError(err.Error())
}

// wrapped for unit tests
var deviceManagerReboot = func(dm *devicestate.DeviceManager, systemLabel, mode string) error {
	return dm.Reboot(systemLabel, mode)
}

func postSystemActionReboot(c *Command, systemLabel string, req *systemActionRequest) Response {
	dm := c.d.overlord.DeviceManager()
	if err := deviceManagerReboot(dm, systemLabel, req.Mode); err != nil {
		return handleSystemActionErr(err, systemLabel)
	}
	return SyncResponse(nil)
}

func postSystemActionDo(c *Command, systemLabel string, req *systemActionRequest) Response {
	if systemLabel == "" {
		return BadRequest("system action requires the system label to be provided")
	}
	if req.Mode == "" {
		return BadRequest("system action requires the mode to be provided")
	}

	sa := devicestate.SystemAction{
		Title: req.Title,
		Mode:  req.Mode,
	}
	if err := c.d.overlord.DeviceManager().RequestSystemAction(systemLabel, sa); err != nil {
		return handleSystemActionErr(err, systemLabel)
	}
	return SyncResponse(nil)
}

func postSystemActionInstall(c *Command, systemLabel string, req *systemActionRequest) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	switch req.Step {
	case client.InstallStepSetupStorageEncryption:
		chg, err := devicestateInstallSetupStorageEncryption(st, systemLabel, req.OnVolumes)
		if err != nil {
			return BadRequest("cannot setup storage encryption for install from %q: %v", systemLabel, err)
		}
		ensureStateSoon(st)
		return AsyncResponse(nil, chg.ID())
	case client.InstallStepFinish:
		chg, err := devicestateInstallFinish(st, systemLabel, req.OnVolumes)
		if err != nil {
			return BadRequest("cannot finish install for %q: %v", systemLabel, err)
		}
		ensureStateSoon(st)
		return AsyncResponse(nil, chg.ID())
	default:
		return BadRequest("unsupported install step %q", req.Step)
	}
}
