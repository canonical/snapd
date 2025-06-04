// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2025 Canonical Ltd
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

// PreinstallErrorInfo represents the information contained within
// a preinstall check error, including its kind, message, and optionally
// arguments and recovery actions.
type PreinstallErrorInfo struct {
	Kind    string                     `json:"kind"`
	Message string                     `json:"message"`
	Args    map[string]json.RawMessage `json:"args,omitempty"`
	Actions []string                   `json:"actions,omitempty"`
}
