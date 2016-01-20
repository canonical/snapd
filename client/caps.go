// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"strings"
)

// CapabilityID is a pair of names (snap, capability) that identifies a capability.
type CapabilityID struct {
	// SnapName is the name of a snap.
	SnapName string `json:"snap"`
	// CapabilityName is the name of a capability local to the snap.
	CapName string `json:"capability"`
}

// Capability holds information about a capability that a snap may request
// from a snappy system to do its job while running on it.
type Capability struct {
	ID CapabilityID `json:"id"`
	// Label provides an optional title for the capability to help a human tell
	// which physical device this capability is referring to. It might say
	// "Front USB", or "Green Serial Port", for example.
	Label string `json:"label"`
	// Type defines the type of this capability. The capability type defines
	// the behavior allowed and expected from providers and consumers of that
	// capability, and also which information should be exchanged by these
	// parties.
	Type string `json:"type"`
	// Attrs are key-value pairs that provide type-specific capability details.
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Capabilities returns the capabilities currently available for snaps to consume.
func (client *Client) Capabilities() (map[CapabilityID]Capability, error) {
	var response map[string]map[string]Capability

	if err := client.doSync("GET", "/1.0/capabilities", nil, &response); err != nil {
		return nil, fmt.Errorf("cannot obtain capabilities: %s", err)
	}
	result := make(map[CapabilityID]Capability)
	for capIDString, cap := range response["capabilities"] {
		split := strings.SplitN(capIDString, ".", 2)
		if len(split) != 2 {
			return nil, fmt.Errorf("invalid capability identifier: %q", capIDString)
		}
		result[CapabilityID{split[0], split[1]}] = cap
	}

	return result, nil
}

// AddCapability adds one capability to the system
func (client *Client) AddCapability(c *Capability) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}

	var rsp interface{}
	if err := client.doSync("POST", "/1.0/capabilities", bytes.NewReader(b), &rsp); err != nil {
		return fmt.Errorf("cannot add capability: %s", err)
	}

	return nil
}

// RemoveCapability removes one capability from the system
func (client *Client) RemoveCapability(ID CapabilityID) error {
	var rsp interface{}
	if err := client.doSync("DELETE", fmt.Sprintf("/1.0/capabilities/%s.%s", ID.SnapName, ID.CapName), nil, &rsp); err != nil {
		return fmt.Errorf("cannot remove capability: %s", err)
	}

	return nil
}
