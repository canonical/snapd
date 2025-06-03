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

package swfeats

import (
	"fmt"
	"strings"
)

var (
	ChangeReg = newChangeRegistry()
	EnsureReg = newEnsureRegistry()
)

type ChangeRegistry struct {
	changes map[string][]string
}

func newChangeRegistry() *ChangeRegistry {
	return &ChangeRegistry{changes: make(map[string][]string)}
}

func (r *ChangeRegistry) NewChangeKind(kind string) string {
	r.changes[kind] = nil
	return kind
}

func (r *ChangeRegistry) AddPossibleValues(kind string, values []string) bool {
	if strings.Count(kind, "%s") != 1 {
		return false
	}
	if _, ok := r.changes[kind]; !ok {
		return false
	}
	r.changes[kind] = values
	return true
}

func (r *ChangeRegistry) KnownChangeKinds() []string {
	kinds := make([]string, 0, len(r.changes))
	for key, values := range r.changes {
		if values == nil {
			kinds = append(kinds, key)
			continue
		}
		for _, value := range values {
			kinds = append(kinds, fmt.Sprintf(key, value))
		}
	}
	return kinds
}

type EnsureRegistry struct {
	ensures map[EnsureEntry]any
}

type EnsureEntry struct {
	Manager  string `json:"manager"`
	Function string `json:"function"`
}

func newEnsureRegistry() *EnsureRegistry {
	return &EnsureRegistry{ensures: make(map[EnsureEntry]any)}
}

func (r *EnsureRegistry) NewEnsure(manager, function string) {
	r.ensures[EnsureEntry{Manager: manager, Function: function}] = nil
}

func (r *EnsureRegistry) KnownEnsures() []EnsureEntry {
	ensures := make([]EnsureEntry, 0, len(r.ensures))
	for k := range r.ensures {
		ensures = append(ensures, k)
	}
	return ensures
}
