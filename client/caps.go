// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	errPrefix := "cannot obtain capabilities"
	var rsp response
	if err := client.do("GET", "/1.0/capabilities", nil, &rsp); err != nil {
		return nil, err
	}
	switch rsp.Type {
	case "error":
		return nil, rsp.processErrorResponse()
	case "sync":
		var resultOk map[string]map[string]Capability
		if err := json.Unmarshal(rsp.Result, &resultOk); err != nil {
			return nil, fmt.Errorf("%s: failed to unmarshal response: %q", errPrefix, err)
		}
		return resultOk["capabilities"], nil
	default:
		return nil, fmt.Errorf("%s: expected sync response, got %s", errPrefix, rsp.Type)
	}
}

// AddCapability adds one capability to the system
func (client *Client) AddCapability(c *Capability) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	var rsp response
	if err := client.do("POST", "/1.0/capabilities", bytes.NewReader(b), &rsp); err != nil {
		return err
	}
	switch rsp.Type {
	case "error":
		return rsp.processErrorResponse()
	case "sync":
		return nil
	default:
		return fmt.Errorf("cannot obtain capabilities: expected sync response, got %q", rsp.Type)
	}
}
