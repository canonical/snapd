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
	"regexp"
	"sort"
	"sync"
)

// Type is the name of a capability type.
type Type string

// String returns a string representation for the capability type.
func (t Type) String() string {
	return string(t)
}

// Capability holds information about a capability that a snap may request
// from a snappy system to do its job while running on it.
type Capability struct {
	// Name is a key that identifies the capability. It must be unique within
	// its context, which may be either a snap or a snappy runtime.
	Name string
	// Label provides an optional title for the capability to help a human tell
	// which physical device this capability is referring to. It might say
	// "Front USB", or "Green Serial Port", for example.
	Label string
	// Type defines the type of this capability. The capability type defines
	// the behavior allowed and expected from providers and consumers of that
	// capability, and also which information should be exchanged by these
	// parties.
	Type Type
	// Attrs are key-value pairs that provide type-specific capability details.
	Attrs map[string]string
}

// Repository stores all known snappy capabilities and types
type Repository struct {
	m sync.Mutex // protects the internals from concurrent access. If contention gets high, switch to a RWMutex
	// Map of capabilities, indexed by Capability.Name
	caps map[string]*Capability
	// A slice of types that are recognized and accepted
	types []Type
}

// NotFoundError means that a capability was not found
type NotFoundError struct {
	what, name string
}

const (
	// FileType is a basic capability vaguely expressing access to a specific
	// file. This single capability  type is here just to help bootstrap
	// the capability concept before we get to load capability interfaces
	// from YAML.
	FileType Type = "file"
)

var builtInTypes = [...]Type{
	FileType,
}

// Regular expression describing correct identifiers
var validName = regexp.MustCompile("^[a-z]([a-z0-9-]+[a-z0-9])?$")

// ValidateName checks if a string as a capability name
func ValidateName(name string) error {
	valid := validName.MatchString(name)
	if !valid {
		return fmt.Errorf("%q is not a valid snap name", name)
	}
	return nil
}

// NewRepository creates an empty capability repository
func NewRepository() *Repository {
	return &Repository{
		caps:  make(map[string]*Capability),
		types: make([]Type, 0),
	}
}

// LoadBuiltInTypes adds all built-in types to the repository
// If any of the additions fail the function returns the error and stops.
func LoadBuiltInTypes(r *Repository) error {
	for _, t := range builtInTypes {
		if err := r.AddType(t); err != nil {
			return err
		}
	}
	return nil
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
	for _, t := range r.types {
		if cap.Type == t {
			goto typeFound
		}
	}
	return fmt.Errorf("cannot add capability %q: type %q is unknown", cap.Name, cap.Type)
typeFound:
	// Reject capabilities that don't pass type-specific validation
	if err := cap.Type.Validate(cap); err != nil {
		return err
	}
	r.caps[cap.Name] = cap
	return nil
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
// Removing a capability that doesn't exist silently does nothing
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

// String representation of a capability.
func (c Capability) String() string {
	return c.Name
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

func (e *NotFoundError) Error() string {
	switch e.what {
	case "remove":
		return fmt.Sprintf("can't remove capability %q, no such capability", e.name)
	default:
		panic(fmt.Sprintf("unexpected what: %q", e.what))
	}
}

// Validate if a capability is correct according to the given type
func (t Type) Validate(c *Capability) error {
	if t != c.Type {
		return fmt.Errorf("capability is not of type %q", t)
	}
	return nil
}
