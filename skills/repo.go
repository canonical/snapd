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
	"errors"
	"sort"
	"sync"
)

// Repository stores all known snappy skills and slots and types.
type Repository struct {
	// Protects the internals from concurrent access.
	m     sync.Mutex
	types []Type
}

var (
	// ErrDuplicate is reported when type, skill or slot already exist.
	ErrDuplicate = errors.New("duplicate found")
)

// NewRepository creates an empty skill repository.
func NewRepository() *Repository {
	return &Repository{}
}

// AllTypes returns all skill types.
func (r *Repository) AllTypes() []Type {
	r.m.Lock()
	defer r.m.Unlock()

	types := make([]Type, len(r.types))
	copy(types, r.types)
	return types
}

// Type returns the type with a given name.
func (r *Repository) Type(typeName string) Type {
	r.m.Lock()
	defer r.m.Unlock()

	return r.unlockedType(typeName)
}

// AddType adds a skill type to the repository.
// NOTE: API exception, Type is an interface, so it cannot use simple types as arguments.
func (r *Repository) AddType(t Type) error {
	r.m.Lock()
	defer r.m.Unlock()

	typeName := t.Name()
	if err := ValidateName(typeName); err != nil {
		return err
	}
	if otherT := r.unlockedType(typeName); otherT != nil {
		return ErrDuplicate
	}
	r.types = append(r.types, t)
	sort.Sort(byTypeName(r.types))
	return nil
}

// Private unlocked APIs

func (r *Repository) unlockedType(typeName string) Type {
	// Assumption: r.types is sorted
	i := sort.Search(len(r.types), func(i int) bool { return r.types[i].Name() <= typeName })
	if i < len(r.types) && r.types[i].Name() == typeName {
		return r.types[i]
	}
	return nil
}

// Support for sort.Interface

type byTypeName []Type

func (c byTypeName) Len() int      { return len(c) }
func (c byTypeName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byTypeName) Less(i, j int) bool {
	return c[i].Name() < c[j].Name()
}
