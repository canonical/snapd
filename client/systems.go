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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"

	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/snap"
)

// SystemModelData contains information about the model
type SystemModelData struct {
	// Model as the model assertion
	Model string `json:"model,omitempty"`
	// BrandID corresponds to brand-id in the model assertion
	BrandID string `json:"brand-id,omitempty"`
	// DisplayName is human friendly name, corresponds to display-name in
	// the model assertion
	DisplayName string `json:"display-name,omitempty"`
}

type System struct {
	// Current is true when the system running now was installed from that
	// recovery seed
	Current bool `json:"current,omitempty"`
	// Label of the recovery system
	Label string `json:"label,omitempty"`
	// Model information
	Model SystemModelData `json:"model,omitempty"`
	// Brand information
	Brand snap.StoreAccount `json:"brand,omitempty"`
	// Actions available for this system
	Actions []SystemAction `json:"actions,omitempty"`
}

type SystemAction struct {
	// Title is a user presentable action description
	Title string `json:"title,omitempty"`
	// Mode given action can be executed in
	Mode string `json:"mode,omitempty"`
}

// ListSystems list all systems available for seeding or recovery.
func (client *Client) ListSystems() ([]System, error) {
	type systemsResponse struct {
		Systems []System `json:"systems,omitempty"`
	}

	var rsp systemsResponse

	if _, err := client.doSync("GET", "/v2/systems", nil, nil, nil, &rsp); err != nil {
		return nil, xerrors.Errorf("cannot list recovery systems: %v", err)
	}
	return rsp.Systems, nil
}

// DoSystemAction issues a request to perform an action using the given seed
// system and its mode.
func (client *Client) DoSystemAction(systemLabel string, action *SystemAction) error {
	if systemLabel == "" {
		return fmt.Errorf("cannot request an action without the system")
	}
	if action == nil {
		return fmt.Errorf("cannot request an action without one")
	}
	// deeper verification is done by the backend

	req := struct {
		Action string `json:"action"`
		*SystemAction
	}{
		Action:       "do",
		SystemAction: action,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&req); err != nil {
		return err
	}
	if _, err := client.doSync("POST", "/v2/systems/"+systemLabel, nil, nil, &body, nil); err != nil {
		return xerrors.Errorf("cannot request system action: %v", err)
	}
	return nil
}

// RebootToSystem issues a request to reboot into system with the
// given label and the given mode.
//
// When called without a systemLabel and without a mode it will just
// trigger a regular reboot.
//
// When called without a systemLabel but with a mode it will use
// the current system to enter the given mode.
//
// Note that "recover" and "run" modes are only available for the
// current system.
func (client *Client) RebootToSystem(systemLabel, mode string) error {
	// verification is done by the backend

	req := struct {
		Action string `json:"action"`
		Mode   string `json:"mode"`
	}{
		Action: "reboot",
		Mode:   mode,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&req); err != nil {
		return err
	}
	if _, err := client.doSync("POST", "/v2/systems/"+systemLabel, nil, nil, &body, nil); err != nil {
		if systemLabel != "" {
			return xerrors.Errorf("cannot request system reboot into %q: %v", systemLabel, err)
		}
		return xerrors.Errorf("cannot request system reboot: %v", err)
	}
	return nil
}

type SystemDetails struct {
	// First part is designed to look like `client.System` - the
	// only difference is how the model is represented
	Current bool                   `json:"current,omitempty"`
	Label   string                 `json:"label,omitempty"`
	Model   map[string]interface{} `json:"model,omitempty"`
	Brand   snap.StoreAccount      `json:"brand,omitempty"`
	Actions []SystemAction         `json:"actions,omitempty"`

	// Volumes contains the volumes defined from the gadget snap
	Volumes map[string]*gadget.Volume `json:"volumes,omitempty"`

	// TODO: add EncryptionSupportInfo here too
}

func (client *Client) SystemDetails(seedLabel string) (*SystemDetails, error) {
	var rsp SystemDetails

	if _, err := client.doSync("GET", "/v2/systems/"+seedLabel, nil, nil, nil, &rsp); err != nil {
		return nil, xerrors.Errorf("cannot get details for system %q: %v", seedLabel, err)
	}
	return &rsp, nil
}
