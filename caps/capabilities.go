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
)

// Type is the name of a capability type.
type Type string

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
}

// Repository stores all known snappy capabilities and types
type Repository struct {
	// Map of capabilities, indexed by Capability.Name
	caps map[string]*Capability
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
	return &Repository{make(map[string]*Capability)}
}

// Add a capability to the repository.
// Capability names must be unique within the repository.
// An error is returned if this constraint is violated.
func (r *Repository) Add(cap *Capability) error {
	if err := ValidateName(cap.Name); err != nil {
		return err
	}
	if _, ok := r.caps[cap.Name]; ok {
		return fmt.Errorf("cannot add capability %q: name already exists", cap.Name)
	}
	r.caps[cap.Name] = cap
	return nil
}

// Remove removes the capability with the provided name.
// Removing a capability that doesn't exist silently does nothing
func (r *Repository) Remove(name string) error {
	_, ok := r.caps[name]
	if ok {
		delete(r.caps, name)
		return nil
	}
	return &NotFoundError{"remove", name}
}

// Names returns all capability names in the repository in lexicographical order.
func (r *Repository) Names() []string {
	keys := make([]string, len(r.caps))
	i := 0
	for key := range r.caps {
		keys[i] = key
		i++
	}
	sort.Strings(keys)
	return keys
}

// String representation of a capability
func (c Capability) String() string {
	return c.Name
}

type byName []Capability

func (c byName) Len() int {
	return len(c)
}

func (c byName) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

func (c byName) Less(i, j int) bool {
	return c[i].Name < c[j].Name
}

// All capabilities, sorted by name
func (r *Repository) All() []Capability {
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
