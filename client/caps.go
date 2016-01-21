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
)

// Capability holds information about a capability that a snap may request
// from a snappy system to do its job while running on it.
type Capability struct {
	// Name is a key that identifies the capability. It must be unique within
	// its context, which may be either a snap or a snappy runtime.
	Name string `json:"name"`
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
func (client *Client) Capabilities() (map[string]Capability, error) {
	var resultOk map[string]map[string]Capability

	if err := client.doSync("GET", "/2.0/capabilities", nil, &resultOk); err != nil {
		return nil, fmt.Errorf("cannot obtain capabilities: %s", err)
	}

	return resultOk["capabilities"], nil
}

// AddCapability adds one capability to the system
func (client *Client) AddCapability(c *Capability) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}

	var rsp interface{}
	if err := client.doSync("POST", "/2.0/capabilities", bytes.NewReader(b), &rsp); err != nil {
		return fmt.Errorf("cannot add capability: %s", err)
	}

	return nil
}

// RemoveCapability removes one capability from the system
func (client *Client) RemoveCapability(name string) error {
	var rsp interface{}
	if err := client.doSync("DELETE", fmt.Sprintf("/2.0/capabilities/%s", name), nil, &rsp); err != nil {
		return fmt.Errorf("cannot remove capability: %s", err)
	}

	return nil
}
