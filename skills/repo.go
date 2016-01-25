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

// AllTypes returns all skill types known to the repository.
func (r *Repository) AllTypes() []Type {
	r.m.Lock()
	defer r.m.Unlock()

	return append([]Type(nil), r.types...)
}

// Type returns a type with a given name.
func (r *Repository) Type(typeName string) Type {
	r.m.Lock()
	defer r.m.Unlock()

	return r.unlockedType(typeName)
}

// AddType adds the provided skill type to the repository.
// NOTE: API exception, Type is an interface, so it cannot use simple types as arguments.
func (r *Repository) AddType(t Type) error {
	r.m.Lock()
	defer r.m.Unlock()

	typeName := t.Name()
	if err := ValidateName(typeName); err != nil {
		return err
	}
	if i, found := r.unlockedTypeIndex(typeName); !found {
		r.types = append(r.types[:i], append([]Type{t}, r.types[i:]...)...)
		return nil
	}
	return ErrDuplicate
}

// Private unlocked APIs

func (r *Repository) unlockedType(typeName string) Type {
	if i, found := r.unlockedTypeIndex(typeName); found {
		return r.types[i]
	}
	return nil
}

func (r *Repository) unlockedTypeIndex(typeName string) (int, bool) {
	// Assumption: r.types is sorted
	i := sort.Search(len(r.types), func(i int) bool { return r.types[i].Name() >= typeName })
	if i < len(r.types) && r.types[i].Name() == typeName {
		return i, true
	}
	return i, false
}
