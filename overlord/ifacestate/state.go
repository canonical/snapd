// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package ifacestate implements the manager and state aspects
// responsible for the maintenance of interfaces the system.
package ifacestate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/overlord/state"
)

// Connection identifies both end points of a connection.
type Connection struct {
	Plug interfaces.PlugRef
	Slot interfaces.SlotRef
}

// String returns the text representation of a connection.
func (conn Connection) String() string {
	return fmt.Sprintf("%s:%s %s:%s", conn.Plug.Snap, conn.Plug.Name,
		conn.Slot.Snap, conn.Slot.Name)
}

// MarshalJSON marshals connection to JSON
func (conn Connection) MarshalJSON() ([]byte, error) {
	return json.Marshal(conn.String())
}

// MarshalText marshals connection to text.
func (conn Connection) MarshalText() ([]byte, error) {
	return []byte(conn.String()), nil
}

// UnmarshalJSON unmarshals connection from the JSON.
func (conn *Connection) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return conn.UnmarshalText([]byte(s))
}

// UnmarshalText unmarshals connection from text.
func (conn *Connection) UnmarshalText(data []byte) error {
	s := string(data)
	parts := strings.SplitN(s, " ", 2)
	if len(parts) != 2 {
		return fmt.Errorf("malformed connection: %q", s)
	}
	plugParts := strings.SplitN(parts[0], ":", 2)
	slotParts := strings.SplitN(parts[1], ":", 2)
	if len(plugParts) != 2 || len(slotParts) != 2 {
		return fmt.Errorf("malformed connection: %q", s)
	}
	*conn = Connection{
		Plug: interfaces.PlugRef{Snap: plugParts[0], Name: plugParts[1]},
		Slot: interfaces.SlotRef{Snap: slotParts[0], Name: slotParts[1]},
	}
	return nil
}

// ConnState holds the state associated with a given connection.
type ConnState struct {
	Auto bool `json:"auto,omitempty"`
	// TODO: other meta data
}

// Connections describe the state of a set of connections.
type Connections map[Connection]ConnState

// Load loads connections from a given state.
func (conns *Connections) Load(s *state.State) error {
	if err := s.Get("conns", conns); err != nil && err != state.ErrNoState {
		return err
	}
	return nil
}

// Store stores connections in a given state.
func (conns *Connections) Store(s *state.State) {
	s.Set("conns", conns)
}

// MarshalJSON marshals connections to JSON.
func (conns Connections) MarshalJSON() ([]byte, error) {
	cns := make(map[string]ConnState, len(conns))
	for conn, state := range conns {
		cns[conn.String()] = state
	}
	return json.Marshal(cns)
}

// UnmarshalJSON unmarshals connections from JSON.
func (conns *Connections) UnmarshalJSON(data []byte) error {
	var cns map[string]ConnState
	if err := json.Unmarshal(data, &cns); err != nil {
		return err
	}
	for msg, connState := range cns {
		var cn Connection
		if err := cn.UnmarshalText([]byte(msg)); err != nil {
			return err
		}
		if *conns == nil {
			*conns = make(Connections)
		}
		(*conns)[cn] = connState
	}
	return nil
}
