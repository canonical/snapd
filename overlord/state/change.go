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

package state

import (
	"encoding/json"
)

// Change represents a tracked modification to the system state.
//
// The Change provides both the justification for individual tasks
// to be performed and the grouping of them.
//
// As an example, if an administrator requests an interface connection,
// multiple hooks might be individually run to accomplish the task. The
// Change summary would reflect the request for an interface connection,
// while the individual Task values would track the running of
// the hooks themselves.
type Change struct {
	id string
}

func newChange(id, kind, summary string) *Change {
	return &Change{id: id}
}

type marshalledChange struct {
	ID string `json:"id"`
}

// MarshalJSON makes Change a json.Marshaller
func (c *Change) MarshalJSON() ([]byte, error) {
	return json.Marshal(marshalledChange{
		ID: c.id,
	})
}

// UnmarshalJSON makes Change a json.Unmarshaller
func (c *Change) UnmarshalJSON(data []byte) error {
	var unmarshalled marshalledChange
	err := json.Unmarshal(data, &unmarshalled)
	if err != nil {
		return err
	}
	c.id = unmarshalled.ID
	return nil
}

// ID returns the individual random key for the change.
func (c *Change) ID() string {
	return c.id
}
