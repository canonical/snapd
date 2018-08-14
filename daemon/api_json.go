// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces"
)

// plugJSON aids in marshaling snap.PlugInfo into JSON.
type plugJSON struct {
	Snap      string                 `json:"snap"`
	Name      string                 `json:"plug"`
	Interface string                 `json:"interface,omitempty"`
	Attrs     map[string]interface{} `json:"attrs,omitempty"`
	Apps      []string               `json:"apps,omitempty"`
	Label     string                 `json:"label,omitempty"`
	// Connections are synthesized, they are not on the original type.
	Connections []interfaces.SlotRef `json:"connections,omitempty"`
}

// slotJSON aids in marshaling snap.SlotInfo into JSON.
type slotJSON struct {
	Snap      string                 `json:"snap"`
	Name      string                 `json:"slot"`
	Interface string                 `json:"interface,omitempty"`
	Attrs     map[string]interface{} `json:"attrs,omitempty"`
	Apps      []string               `json:"apps,omitempty"`
	Label     string                 `json:"label,omitempty"`
	// Connections are synthesized, they are not on the original type.
	Connections []interfaces.PlugRef `json:"connections,omitempty"`
}

// interfaceJSON aids in marshaling interfaces.Info into JSON.
type interfaceJSON struct {
	Name    string      `json:"name,omitempty"`
	Summary string      `json:"summary,omitempty"`
	DocURL  string      `json:"doc-url,omitempty"`
	Plugs   []*plugJSON `json:"plugs,omitempty"`
	Slots   []*slotJSON `json:"slots,omitempty"`
}

// interfaceAction is an action performed on the interface system.
type interfaceAction struct {
	Action string     `json:"action"`
	Plugs  []plugJSON `json:"plugs,omitempty"`
	Slots  []slotJSON `json:"slots,omitempty"`
}
