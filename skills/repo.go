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

package skills

import (
	"fmt"
	"sync"
)

// Repository stores all known snappy skills and slots and types.
type Repository struct {
	// Protects the internals from concurrent access.
	m     sync.Mutex
	types map[string]Type
}

// NewRepository creates an empty skill repository.
func NewRepository() *Repository {
	return &Repository{
		types: make(map[string]Type),
	}
}

// Type returns a type with a given name.
func (r *Repository) Type(typeName string) Type {
	r.m.Lock()
	defer r.m.Unlock()

	return r.types[typeName]
}

// AddType adds the provided skill type to the repository.
func (r *Repository) AddType(t Type) error {
	r.m.Lock()
	defer r.m.Unlock()

	typeName := t.Name()
	if err := ValidateName(typeName); err != nil {
		return err
	}
	if _, ok := r.types[typeName]; ok {
		return fmt.Errorf("cannot add skill type: %q, type name is in use", typeName)
	}
	r.types[typeName] = t
	return nil
}
