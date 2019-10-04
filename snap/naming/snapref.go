// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package naming

// A SnapRef references a snap by name and/or id.
type SnapRef interface {
	SnapName() string
	ID() string
}

// Snap references a snap by name only.
type Snap string

func (s Snap) SnapName() string {
	return string(s)
}

func (s Snap) ID() string {
	return ""
}

type snapRef struct {
	name string
	id   string
}

// NewSnapRef returns a reference to the snap with given name and id.
func NewSnapRef(name, id string) SnapRef {
	return &snapRef{name: name, id: id}
}

func (r *snapRef) SnapName() string {
	return r.name
}

func (r *snapRef) ID() string {
	return r.id
}

// SameSnap returns whether the two arguments refer to the same snap.
// If ids are not available for both it will fallback to names.
func SameSnap(snapRef1, snapRef2 SnapRef) bool {
	id1 := snapRef1.ID()
	id2 := snapRef2.ID()
	if id1 != "" && id2 != "" {
		return id1 == id2
	}
	return snapRef1.SnapName() == snapRef2.SnapName()
}

// SnapSet can hold a set of references to snaps.
type SnapSet struct {
	byID   map[string]SnapRef
	byName map[string]SnapRef
}

// NewSnapSet builds a snap set with the given references.
func NewSnapSet(refs []SnapRef) *SnapSet {
	sz := len(refs) + 2
	s := &SnapSet{
		byID:   make(map[string]SnapRef, sz),
		byName: make(map[string]SnapRef, sz),
	}
	for _, r := range refs {
		s.Add(r)
	}
	return s
}

// Empty returns whether the snap set is empty.
func (s *SnapSet) Empty() bool {
	return len(s.byID) == 0 && len(s.byName) == 0
}

// Lookup finds the reference in the set matching the given one if any.
func (s *SnapSet) Lookup(which SnapRef) SnapRef {
	whichID := which.ID()
	name := which.SnapName()
	if whichID != "" {
		if ref := s.byID[whichID]; ref != nil {
			return ref
		}
	}
	ref := s.byName[name]
	if ref == nil || (ref.ID() != "" && whichID != "") {
		return nil
	}
	return ref
}

// Contains returns whether the set has a matching reference already.
func (s *SnapSet) Contains(ref SnapRef) bool {
	return s.Lookup(ref) != nil
}

// Add adds one reference to the set.
// Already added ids or names will be ignored. The assumption is that
// a SnapSet is populated with distinct snaps.
func (s *SnapSet) Add(ref SnapRef) {
	if s.Contains(ref) {
		// nothing to do
		return
	}
	if id := ref.ID(); id != "" {
		s.byID[id] = ref
	}
	if name := ref.SnapName(); name != "" {
		s.byName[name] = ref
	}
}
