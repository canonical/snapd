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

// The swfeats package implements registries for features
// not tracked in other places
package swfeats

import (
	"fmt"
	"strings"
)

var (
	changeReg *ChangeKindRegistry = newChangeKindRegistry()
	ensureReg *EnsureRegistry     = newEnsureRegistry()
)

// ChangeKindRegistry contains the set of all change kind strings
// along with all their possible variants if the change kind is
// a template
type ChangeKindRegistry struct {
	changes map[string][]string
}

func newChangeKindRegistry() *ChangeKindRegistry {
	return &ChangeKindRegistry{changes: make(map[string][]string)}
}

// Add a change kind string to the registry
func RegChangeKind(kind string) string {
	if _, ok := changeReg.changes[kind]; !ok {
		changeReg.changes[kind] = make([]string, 0)
	}
	return kind
}

// AddVariants attaches the list of variants to the already
// registered change kind template. If the template is not
// present or does not contain exactly one string placeholder,
// then the method fails and returns false
func AddChangeKindVariants(kind string, values []string) bool {
	if strings.Count(kind, "%s") != 1 {
		return false
	}
	if _, ok := changeReg.changes[kind]; !ok {
		return false
	}
	changeReg.changes[kind] = values
	return true
}

// KnownChangeKinds retrieves the complete list of all registered
// change kinds, including their variants, if present
func KnownChangeKinds() []string {
	kinds := make([]string, 0, len(changeReg.changes))
	for key, values := range changeReg.changes {
		if len(values) == 0 {
			kinds = append(kinds, key)
			continue
		}
		for _, value := range values {
			kinds = append(kinds, fmt.Sprintf(key, value))
		}
	}
	return kinds
}

// EnsureRegistry contains the set of all ensure helper
// functions and their manager
type EnsureRegistry struct {
	ensures map[EnsureEntry]any
}

// EnsureEntry represents a single ensure helper function
// by containing manager and function name
type EnsureEntry struct {
	Manager  string `json:"manager"`
	Function string `json:"function"`
}

func newEnsureRegistry() *EnsureRegistry {
	return &EnsureRegistry{ensures: make(map[EnsureEntry]any)}
}

// Add a ensure helper function to the registry
func RegEnsure(manager, function string) {
	ensureReg.ensures[EnsureEntry{Manager: manager, Function: function}] = nil
}

// KnownEnsures retrieves the complete list of ensure
// helper functions from the registry
func KnownEnsures() []EnsureEntry {
	ensures := make([]EnsureEntry, 0, len(ensureReg.ensures))
	for k := range ensureReg.ensures {
		ensures = append(ensures, k)
	}
	return ensures
}
