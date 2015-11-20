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

package caps

import (
	"fmt"
	"sort"
	"sync"
)

// Repository stores all known snappy capabilities and types.
type Repository struct {
	m sync.Mutex // protects the internals from concurrent access. If contention gets high, switch to a RWMutex
	// Map of capabilities, indexed by Capability.Name
	caps map[string]*Capability
	// A slice of types that are recognized and accepted
	types []Type
}

// NewRepository creates an empty capability repository
func NewRepository() *Repository {
	return &Repository{
		caps:  make(map[string]*Capability),
		types: make([]Type, 0),
	}
}

// Add a capability to the repository.
// Capability names must be valid snap names, as defined by ValidateName, and
// must be unique within the repository.  An error is returned if this
// constraint is violated.
func (r *Repository) Add(cap *Capability) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject capabilities with invalid names
	if err := ValidateName(cap.Name); err != nil {
		return err
	}
	// Reject capabilities with duplicate names
	if _, ok := r.caps[cap.Name]; ok {
		return fmt.Errorf("cannot add capability %q: name already exists", cap.Name)
	}
	// Reject capabilities with unknown types
	if !r.HasType(cap.Type) {
		return fmt.Errorf("cannot add capability %q: type %q is unknown", cap.Name, cap.Type)
	}
	// Reject capabilities that don't pass type-specific validation
	if err := cap.Type.Validate(cap); err != nil {
		return err
	}
	r.caps[cap.Name] = cap
	return nil
}

// HasType checks if the repository contains the given type.
func (r *Repository) HasType(t Type) bool {
	for _, tt := range r.types {
		if tt == t {
			return true
		}
	}
	return false
}

// AddType adds a capability type to the repository.
// It's an error to add the same capability type more than once.
func (r *Repository) AddType(t Type) error {
	r.m.Lock()
	defer r.m.Unlock()

	if err := ValidateName(t.String()); err != nil {
		return err
	}
	for _, otherT := range r.types {
		if t == otherT {
			return fmt.Errorf("cannot add type %q: name already exists", t)
		}
	}
	r.types = append(r.types, t)
	return nil
}

// Remove removes the capability with the provided name.
// Removing a capability that doesn't exist returns a NotFoundError.
func (r *Repository) Remove(name string) error {
	r.m.Lock()
	defer r.m.Unlock()

	_, ok := r.caps[name]
	if ok {
		delete(r.caps, name)
		return nil
	}
	return &NotFoundError{"remove", name}
}

// Names returns all capability names in the repository in lexicographical order.
func (r *Repository) Names() []string {
	r.m.Lock()
	defer r.m.Unlock()

	keys := make([]string, len(r.caps))
	i := 0
	for key := range r.caps {
		keys[i] = key
		i++
	}
	sort.Strings(keys)
	return keys
}

// TypeNames returns all type names in the repository in lexicographical order.
func (r *Repository) TypeNames() []string {
	r.m.Lock()
	defer r.m.Unlock()

	types := make([]string, len(r.types))
	for i, t := range r.types {
		types[i] = t.String()
	}
	sort.Strings(types)
	return types
}

type byName []Capability

func (c byName) Len() int           { return len(c) }
func (c byName) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c byName) Less(i, j int) bool { return c[i].Name < c[j].Name }

// All returns all capabilities ordered by name.
func (r *Repository) All() []Capability {
	r.m.Lock()
	defer r.m.Unlock()

	caps := make([]Capability, len(r.caps))
	i := 0
	for _, capability := range r.caps {
		caps[i] = *capability
		i++
	}
	sort.Sort(byName(caps))
	return caps
}
