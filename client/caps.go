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

// Assignment holds information about capability assignment.
type Assignment struct {
	// SnapName holds the name of the snap that got the assignment.
	SnapName string `json:"snap"`
	// SlotName holds the name of the "slot" within the snap that got the assignment.
	SlotName string `json:"slot"`
}

func (a Assignment) String() string {
	return fmt.Sprintf("%s:%s", a.SnapName, a.SlotName)
}

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
	// Assignment describes assignment of this capability to a snap
	Assignment *Assignment `json:"assignment,omitempty"`
}

// Capabilities returns the capabilities currently available for snaps to consume.
func (client *Client) Capabilities() (map[string]Capability, error) {
	const errPrefix = "cannot obtain capabilities"
	var rsp response
	if err := client.do("GET", "/1.0/capabilities", nil, &rsp); err != nil {
		return nil, fmt.Errorf("%s: failed to communicate with server: %s", errPrefix, err)
	}
	if err := rsp.err(); err != nil {
		return nil, err
	}
	if rsp.Type != "sync" {
		return nil, fmt.Errorf("%s: expected sync response, got %q", errPrefix, rsp.Type)
	}
	var resultOk map[string]map[string]Capability
	if err := json.Unmarshal(rsp.Result, &resultOk); err != nil {
		return nil, fmt.Errorf("%s: failed to unmarshal response: %v", errPrefix, err)
	}
	return resultOk["capabilities"], nil
}

// AddCapability adds one capability to the system
func (client *Client) AddCapability(c *Capability) error {
	errPrefix := "cannot add capability"
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	var rsp response
	if err := client.do("POST", "/1.0/capabilities", bytes.NewReader(b), &rsp); err != nil {
		return err
	}
	if err := rsp.err(); err != nil {
		return err
	}
	if rsp.Type != "sync" {
		return fmt.Errorf("%s: expected sync response, got %q", errPrefix, rsp.Type)
	}
	return nil
}

// RemoveCapability removes one capability from the system
func (client *Client) RemoveCapability(name string) error {
	errPrefix := "cannot remove capability"
	var rsp response
	if err := client.do("DELETE", fmt.Sprintf("/1.0/capabilities/%s", name), nil, &rsp); err != nil {
		return err
	}
	if err := rsp.err(); err != nil {
		return err
	}
	if rsp.Type != "sync" {
		return fmt.Errorf("%s: expected sync response, got %q", errPrefix, rsp.Type)
	}
	return nil
}

// AssignCapability assigns a capability to a slot within a snap
func (client *Client) AssignCapability(capName, snapName, slotName string) error {
	const errPrefix = "cannot assign capability"
	url := fmt.Sprintf("/1.0/capabilities/%s", capName)
	var rsp response
	if err := client.do("GET", url, nil, &rsp); err != nil {
		return err
	}
	if err := rsp.err(); err != nil {
		return err
	}
	if rsp.Type != "sync" {
		return fmt.Errorf("%s: expected sync response, got %q", errPrefix, rsp.Type)
	}
	var cap Capability
	if err := json.Unmarshal(rsp.Result, &cap); err != nil {
		return fmt.Errorf("%s: failed to unmarshal response: %v", errPrefix, err)
	}
	cap.Assignments = append(cap.Assignments, Assignment{
		SnapName: snapName,
		SlotName: slotName,
	})
	b, err := json.Marshal(cap)
	if err != nil {
		return err
	}
	if err := client.do("POST", url, bytes.NewReader(b), &rsp); err != nil {
		return err
	}
	if err := rsp.err(); err != nil {
		return err
	}
	if rsp.Type != "sync" {
		return fmt.Errorf("%s: expected sync response, got %q", errPrefix, rsp.Type)
	}
	return nil
}
