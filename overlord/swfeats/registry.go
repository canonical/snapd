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
	ChangeReg *ChangeKindRegistry = newChangeKindRegistry()
	EnsureReg *EnsureRegistry     = newEnsureRegistry()
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
func (r *ChangeKindRegistry) Add(kind string) string {
	r.changes[kind] = make([]string, 0)
	return kind
}

// AddVariants attaches the list of variants to the already
// registered change kind template. If the template is not
// present or does not contain exactly one string placeholder,
// then the method fails and returns false
func (r *ChangeKindRegistry) AddVariants(kind string, values []string) bool {
	if strings.Count(kind, "%s") != 1 {
		return false
	}
	if _, ok := r.changes[kind]; !ok {
		return false
	}
	r.changes[kind] = values
	return true
}

// KnownChangeKinds retrieves the complete list of all registered
// change kinds, including their variants, if present
func (r *ChangeKindRegistry) KnownChangeKinds() []string {
	kinds := make([]string, 0, len(r.changes))
	for key, values := range r.changes {
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
func (r *EnsureRegistry) Add(manager, function string) {
	r.ensures[EnsureEntry{Manager: manager, Function: function}] = nil
}

// KnownEnsures retrieves the complete list of ensure
// helper functions from the registry
func (r *EnsureRegistry) KnownEnsures() []EnsureEntry {
	ensures := make([]EnsureEntry, 0, len(r.ensures))
	for k := range r.ensures {
		ensures = append(ensures, k)
	}
	return ensures
}
