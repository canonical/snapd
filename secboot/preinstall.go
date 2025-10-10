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
package secboot

import (
	"encoding/json"
)

// PreinstallErrorDetails describes an individual error detected during a
// preinstall check. It includes the error kind, a human-readable message,
// and optional structured arguments and suggested recovery actions.
type PreinstallErrorDetails struct {
	Kind    string                     `json:"kind"`
	Message string                     `json:"message"`
	Args    map[string]json.RawMessage `json:"args,omitempty"`
	Actions []string                   `json:"actions,omitempty"`
}

// PreinstallAction describes an action with optional arguments that may
// be requested to resolve a preinstall check error.
type PreinstallAction struct {
	Action string                     `json:"action"`
	Args   map[string]json.RawMessage `json:"args,omitempty"`
}
