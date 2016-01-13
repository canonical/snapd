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
	// Protects the internals from concurrent access.
	m sync.Mutex
	// Map of capabilities, indexed by name
	caps map[string]Capability
	// Map of capability types, indexed by name
	types map[string]Type
}

// NewRepository creates an empty capability repository
func NewRepository() *Repository {
	return &Repository{
		caps:  make(map[string]Capability),
		types: make(map[string]Type),
	}
}

// MakeCap creates a new capability from a specification.
// The capability is *not* added to the repository, use Add() for that.
func (r *Repository) MakeCap(name, label, typeName string, attrs map[string]string) (Capability, error) {
	r.m.Lock()
	defer r.m.Unlock()

	if t, ok := r.types[typeName]; ok {
		return t.Make(name, label, attrs)
	}
	return nil, fmt.Errorf("unknown capability type: %q", typeName)
}

// MakeCapFromInfo creates a new capability from capability info.
// The capability is *not* added to the repository, use Add() for that.
func (r *Repository) MakeCapFromInfo(info *CapabilityInfo) (Capability, error) {
	return r.MakeCap(info.Name, info.Label, info.TypeName, info.AttrMap)
}

// Add a capability to the repository.
// Capability names must be valid snap names, as defined by ValidateName, and
// must be unique within the repository.  An error is returned if this
// constraint is violated.
func (r *Repository) Add(cap Capability) error {
	r.m.Lock()
	defer r.m.Unlock()

	name := cap.Name()
	typeName := cap.TypeName()

	// Reject capabilities with invalid names
	if err := ValidateName(name); err != nil {
		return err
	}
	// Reject capabilities with duplicate names
	if _, ok := r.caps[name]; ok {
		return fmt.Errorf("cannot add capability %q: name already exists", name)
	}
	// Reject capabilities with unknown types
	if _, ok := r.types[typeName]; !ok {
		return fmt.Errorf("cannot add capability %q: type %q is unknown", name, typeName)
	}
	// Reject capabilities that don't pass type-specific validation
	if err := cap.Validate(); err != nil {
		return err
	}
	r.caps[name] = cap
	return nil
}

// Capability finds and returns the Capability with the given name or nil if it
// is not found.
func (r *Repository) Capability(name string) Capability {
	r.m.Lock()
	defer r.m.Unlock()

	return r.caps[name]
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

type byName []Capability

func (c byName) Len() int           { return len(c) }
func (c byName) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c byName) Less(i, j int) bool { return c[i].Name() < c[j].Name() }

// All returns all capabilities ordered by name.
func (r *Repository) All() []Capability {
	r.m.Lock()
	defer r.m.Unlock()

	caps := make([]Capability, len(r.caps))
	i := 0
	for _, capability := range r.caps {
		caps[i] = capability
		i++
	}
	sort.Sort(byName(caps))
	return caps
}

// Caps returns a shallow copy of the map of capabilities.
func (r *Repository) Caps() map[string]Capability {
	r.m.Lock()
	defer r.m.Unlock()

	caps := make(map[string]Capability, len(r.caps))
	for k, v := range r.caps {
		caps[k] = v
	}
	return caps
}

// AddType adds a capability type to the repository.
// It's an error to add the same capability type more than once.
func (r *Repository) AddType(t Type) error {
	r.m.Lock()
	defer r.m.Unlock()

	typeName := t.Name()
	if err := ValidateName(typeName); err != nil {
		return err
	}
	if _, ok := r.types[typeName]; ok {
		return fmt.Errorf("cannot add type %q: name already exists", typeName)
	}
	r.types[typeName] = t
	return nil
}

// Type finds and returns the Type with the given name or nil if it is not
// found.
func (r *Repository) Type(name string) Type {
	r.m.Lock()
	defer r.m.Unlock()

	return r.types[name]
}

// TypeNames returns all type names in the repository in lexicographical order.
func (r *Repository) TypeNames() []string {
	r.m.Lock()
	defer r.m.Unlock()

	keys := make([]string, len(r.types))
	i := 0
	for key := range r.types {
		keys[i] = key
		i++
	}
	sort.Strings(keys)
	return keys
}
