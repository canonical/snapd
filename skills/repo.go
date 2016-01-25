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
	m      sync.Mutex
	types  []Type
	skills []*Skill
	slots  []*Slot
}

var (
	// ErrDuplicate is reported when type, skill or slot already exist.
	ErrDuplicate = errors.New("duplicate found")
	// ErrSkillNotFound is reported when skill cannot be looked up.
	ErrSkillNotFound = errors.New("skill not found")
	// ErrTypeNotFound is reported when skill type cannot found.
	ErrTypeNotFound = errors.New("skill type not found")
	// ErrSlotNotFound is reported when slot cannot be found.
	ErrSlotNotFound = errors.New("slot not found")
	// ErrSlotBusy is reported when operation cannot be performed when a slot is occupied.
	ErrSlotBusy = errors.New("slot is occupied")
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

// AllSkills returns all skills of the given type.
// If skillType is the empty string, all skills are returned.
func (r *Repository) AllSkills(skillType string) []*Skill {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Skill
	if skillType == "" {
		result = make([]*Skill, len(r.skills))
		copy(result, r.skills)
	} else {
		result = make([]*Skill, 0)
		for _, skill := range r.skills {
			if skill.Type == skillType {
				result = append(result, skill)
			}
		}
	}
	return result
}

// Skills returns the skills offered by the named snap.
func (r *Repository) Skills(snapName string) []*Skill {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Skill
	for _, skill := range r.skills {
		if skill.Snap == snapName {
			result = append(result, skill)
		}
	}
	return result
}

// Skill returns the specified skill from the named snap.
func (r *Repository) Skill(snapName, skillName string) *Skill {
	r.m.Lock()
	defer r.m.Unlock()

	return r.unlockedSkill(snapName, skillName)
}

// AddSkill adds a skill to the repository.
// Skill names must be valid snap names, as defined by ValidateName.
// Skill name must be unique within a particular snap.
func (r *Repository) AddSkill(snapName, skillName, typeName, label string, attrs map[string]interface{}) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject skill with invalid names
	if err := ValidateName(snapName); err != nil {
		return err
	}
	if err := ValidateName(skillName); err != nil {
		return err
	}
	// TODO: ensure that given snap really exists

	t := r.unlockedType(typeName)
	if t == nil {
		return ErrTypeNotFound
	}
	if r.unlockedSkill(snapName, skillName) != nil {
		return ErrDuplicate
	}
	skill := &Skill{
		Name:  skillName,
		Snap:  snapName,
		Type:  typeName,
		Attrs: attrs,
		Label: label,
	}
	// Reject skill that don't pass type-specific sanitization
	if err := t.Sanitize(skill); err != nil {
		return err
	}
	r.skills = append(r.skills, skill)
	sort.Sort(bySkillSnapAndName(r.skills))
	return nil
}

// RemoveSkill removes the named skill provided by a given snap.
// Removing a skill that doesn't exist returns a ErrSkillNotFound.
// Removing a skill that is granted returns ErrSkillBusy.
func (r *Repository) RemoveSkill(snapName, skillName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// TODO: Ensure that the skill is not used anywhere
	for i, skill := range r.skills {
		if skill.Snap == snapName && skill.Name == skillName {
			r.skills = append(r.skills[:i], r.skills[i+1:]...)
			return nil
		}
	}
	return ErrSkillNotFound
}

// AllSlots returns all skill slots of the given type.
// If skillType is the empty string, all skill slots are returned.
func (r *Repository) AllSlots(skillType string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Slot
	if skillType == "" {
		result = make([]*Slot, len(r.slots))
		copy(result, r.slots)
	} else {
		result = make([]*Slot, 0)
		for _, slot := range r.slots {
			if slot.Type == skillType {
				result = append(result, slot)
			}
		}
	}
	return result
}

// Slots returns the skill slots offered by the named snap.
func (r *Repository) Slots(snapName string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Slot
	for _, slot := range r.slots {
		// NOTE: can be done faster; r.slots is sorted by (Slot.Snap, Slot.Name).
		if slot.Snap == snapName {
			result = append(result, slot)
		}
	}
	return result
}

// Slot returns the specified skill slot from the named snap.
func (r *Repository) Slot(snapName, slotName string) *Slot {
	r.m.Lock()
	defer r.m.Unlock()

	return r.unlockedSlot(snapName, slotName)
}

// AddSlot adds a new slot to the repository.
// Adding a slot with invalid name returns an error.
// Adding a slot that has the same name and snap name as another slot returns ErrDuplicate.
func (r *Repository) AddSlot(snapName, slotName, typeName, label string, attrs map[string]interface{}, apps []string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject skill with invalid names
	if err := ValidateName(slotName); err != nil {
		return err
	}
	// TODO: ensure the snap is correct
	// TODO: ensure that apps are correct
	if r.unlockedType(typeName) == nil {
		return ErrTypeNotFound
	}
	if i, found := r.unlockedSlotIndex(snapName, slotName); !found {
		slot := &Slot{
			Name:  slotName,
			Snap:  snapName,
			Type:  typeName,
			Attrs: attrs,
			Apps:  apps,
			Label: label,
		}
		// Insert the slot at the right index
		r.slots = append(r.slots[:i], append([]*Slot{slot}, r.slots[i:]...)...)
		return nil
	}
	return ErrDuplicate
}

// RemoveSlot removes a named slot from the given snap.
// Removing a slot that doesn't exist returns ErrSlotNotFound.
// Removing a slot that uses a skill returns ErrSlotBusy.
func (r *Repository) RemoveSlot(snapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	if i, found := r.unlockedSlotIndex(snapName, slotName); found {
		// TODO: return ErrSlotBusy if slot is occupied by at least one capability.
		r.slots = append(r.slots[:i], r.slots[i+1:]...)
		return nil
	}
	return ErrSlotNotFound
}

// Private unlocked APIs

func (r *Repository) unlockedType(typeName string) Type {
	// Assumption: r.types is sorted
	i := sort.Search(len(r.types), func(i int) bool { return r.types[i].Name() >= typeName })
	if i < len(r.types) && r.types[i].Name() == typeName {
		return r.types[i]
	}
	return nil
}

func (r *Repository) unlockedSkill(snapName, skillName string) *Skill {
	// Assumption: r.skills is sorted
	i := sort.Search(len(r.skills), func(i int) bool {
		if r.skills[i].Snap != snapName {
			return r.skills[i].Snap >= snapName
		}
		return r.skills[i].Name >= skillName
	})
	if i < len(r.skills) && r.skills[i].Snap == snapName && r.skills[i].Name == skillName {
		return r.skills[i]
	}
	return nil
}

// unlockedSlot returns a slot given snap and slot name.
func (r *Repository) unlockedSlot(snapName, slotName string) *Slot {
	i, found := r.unlockedSlotIndex(snapName, slotName)
	if found {
		return r.slots[i]
	}
	return nil
}

// unlockedSlotIndex returns the index of a slot given snap and slot name.
// If the slot is found, the found return value is true. Otherwise the index can
// be used as a place where the slot should be inserted.
func (r *Repository) unlockedSlotIndex(snapName, slotName string) (index int, found bool) {
	// Assumption: r.slots is sorted
	i := sort.Search(len(r.slots), func(i int) bool {
		if r.slots[i].Snap != snapName {
			return r.slots[i].Snap >= snapName
		}
		return r.slots[i].Name >= slotName
	})
	if i < len(r.slots) && r.slots[i].Snap == snapName && r.slots[i].Name == slotName {
		return i, true
	}
	return i, false
}

// Support for sort.Interface

type byTypeName []Type

func (c byTypeName) Len() int      { return len(c) }
func (c byTypeName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byTypeName) Less(i, j int) bool {
	return c[i].Name() < c[j].Name()
}

type bySkillSnapAndName []*Skill

func (c bySkillSnapAndName) Len() int      { return len(c) }
func (c bySkillSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySkillSnapAndName) Less(i, j int) bool {
	if c[i].Snap != c[j].Snap {
		return c[i].Snap < c[j].Snap
	}
	return c[i].Name < c[j].Name
}
