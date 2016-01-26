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
	grants map[*Slot][]*Skill
}

var (
	// ErrTypeNotFound is reported when skill type cannot found.
	ErrTypeNotFound = errors.New("skill type not found")
	// ErrDuplicateType is reported when type with duplicate name is being added to a repository.
	ErrDuplicateType = errors.New("duplicate type name")
	// ErrTypeMismatch is reported when skill and slot types are different.
	ErrTypeMismatch = errors.New("skill type doesn't match slot type")
	// ErrSkillNotFound is reported when skill cannot be looked up.
	ErrSkillNotFound = errors.New("skill not found")
	// ErrDuplicateSkill is reported when skill with duplicate name is being added to a repository.
	ErrDuplicateSkill = errors.New("duplicate skill name")
	// ErrSkillBusy is reported when operation cannot be performed while a skill is granted.
	ErrSkillBusy = errors.New("skill is busy")
	// ErrSlotNotFound is reported when slot cannot be found.
	ErrSlotNotFound = errors.New("slot not found")
	// ErrDuplicateSlot is reported when slot with duplicate name is being added to a repository.
	ErrDuplicateSlot = errors.New("duplicate slot name")
	// ErrSlotBusy is reported when operation cannot be performed when a slot is occupied.
	ErrSlotBusy = errors.New("slot is occupied")
	// ErrSkillNotGranted is reported when a skill is being revoked but it was not granted.
	ErrSkillNotGranted = errors.New("skill not granted")
	// ErrSkillAlreadyGranted is reported when a skill is being granted to the same slot again.
	ErrSkillAlreadyGranted = errors.New("skill already granted")
)

// NewRepository creates an empty skill repository.
func NewRepository() *Repository {
	return &Repository{
		grants: make(map[*Slot][]*Skill),
	}
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
	return ErrDuplicateType
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
	if i, found := r.unlockedSkillIndex(snapName, skillName); !found {
		r.skills = append(r.skills[:i], append([]*Skill{skill}, r.skills[i:]...)...)
		return nil
	}
	return ErrDuplicateSkill
}

// RemoveSkill removes the named skill provided by a given snap.
// Removing a skill that doesn't exist returns a ErrSkillNotFound.
// Removing a skill that is granted returns ErrSkillBusy.
func (r *Repository) RemoveSkill(snapName, skillName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	var i int
	var found bool

	// Ensure that such skill exists
	if i, found = r.unlockedSkillIndex(snapName, skillName); !found {
		return ErrSkillNotFound
	}
	// Ensure that the skill is not busy
	for _, skills := range r.grants {
		if _, found := searchSkill(skills, snapName, skillName); found {
			return ErrSkillBusy
		}
	}
	// Remove the skill
	r.skills = append(r.skills[:i], r.skills[i+1:]...)
	return nil
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
// Adding a slot that has the same name and snap name as another slot returns ErrDuplicateSlot.
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
	return ErrDuplicateSlot
}

// RemoveSlot removes a named slot from the given snap.
// Removing a slot that doesn't exist returns ErrSlotNotFound.
// Removing a slot that uses a skill returns ErrSlotBusy.
func (r *Repository) RemoveSlot(snapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	var i int
	var found bool

	// Ensure that such slot exists
	if i, found = r.unlockedSlotIndex(snapName, slotName); !found {
		return ErrSlotNotFound
	}
	// Ensure that the slot is not busy
	for slot := range r.grants {
		if slot.Snap == snapName && slot.Name == slotName {
			return ErrSlotBusy
		}
	}
	// Remove the slot
	r.slots = append(r.slots[:i], r.slots[i+1:]...)
	return nil
}

// Grant grants the named skill to the named slot of the given snap.
// The skill and the slot must have the same type.
func (r *Repository) Grant(skillSnapName, skillName, slotSnapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	var i int
	var found bool

	// Ensure that such skill exists
	skill := r.unlockedSkill(skillSnapName, skillName)
	if skill == nil {
		return ErrSkillNotFound
	}
	// Ensure that such slot exists
	slot := r.unlockedSlot(slotSnapName, slotName)
	if slot == nil {
		return ErrSlotNotFound
	}
	// Ensure that skill and slot are compatible
	if slot.Type != skill.Type {
		return ErrTypeMismatch
	}
	// Ensure that slot and skill are not connected yet
	slotGrants := r.grants[slot]
	if i, found = searchSkill(slotGrants, skillSnapName, skillName); found {
		return ErrSkillAlreadyGranted
	}
	// Grant the skill
	r.grants[slot] = append(slotGrants[:i], append([]*Skill{skill}, slotGrants[i:]...)...)
	return nil
}

// Revoke revokes the named skill from the slot of the given snap.
func (r *Repository) Revoke(skillSnapName, skillName, slotSnapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	var i int
	var found bool

	// Ensure that such skill exists
	skill := r.unlockedSkill(skillSnapName, skillName)
	if skill == nil {
		return ErrSkillNotFound
	}
	// Ensure that such slot exists
	slot := r.unlockedSlot(slotSnapName, slotName)
	if slot == nil {
		return ErrSlotNotFound
	}
	// Ensure that slot and skill are connected
	slotGrants := r.grants[slot]
	if i, found = searchSkill(slotGrants, skillSnapName, skillName); !found {
		return ErrSkillNotGranted
	}
	r.grants[slot] = append(slotGrants[:i], slotGrants[i+1:]...)
	return nil
}

// GrantedTo returns all the skills granted to a given snap.
func (r *Repository) GrantedTo(snapName string) map[*Slot][]*Skill {
	r.m.Lock()
	defer r.m.Unlock()

	result := make(map[*Slot][]*Skill)
	for slot, skills := range r.grants {
		if slot.Snap == snapName && len(skills) > 0 {
			result[slot] = make([]*Skill, len(skills))
			copy(result[slot], skills)
		}
	}
	return result
}

// GrantedBy returns all of the skills granted by a given snap.
func (r *Repository) GrantedBy(snapName string) map[*Skill][]*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	result := make(map[*Skill][]*Slot)
	for slot, skills := range r.grants {
		for _, skill := range skills {
			if skill.Snap == snapName {
				result[skill] = append(result[skill], slot)
			}
		}
	}
	return result
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

func (r *Repository) unlockedSkill(snapName, skillName string) *Skill {
	if i, found := r.unlockedSkillIndex(snapName, skillName); found {
		return r.skills[i]
	}
	return nil
}

func (r *Repository) unlockedSkillIndex(snapName, skillName string) (int, bool) {
	return searchSkill(r.skills, snapName, skillName)
}

func searchSkill(skills []*Skill, snapName, skillName string) (int, bool) {
	// Assumption: skills is sorted
	i := sort.Search(len(skills), func(i int) bool {
		if skills[i].Snap != snapName {
			return skills[i].Snap >= snapName
		}
		return skills[i].Name >= skillName
	})
	if i < len(skills) && skills[i].Snap == snapName && skills[i].Name == skillName {
		return i, true
	}
	return i, false
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
