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
	"bytes"
	"fmt"
	"sort"
	"sync"

	"github.com/ubuntu-core/snappy/snap"
)

// Repository stores all known snappy skills and slots and types.
type Repository struct {
	// Protects the internals from concurrent access.
	m     sync.Mutex
	types map[string]Type
	// Indexed by [snapName][skillName]
	skills          map[string]map[string]*Skill
	slots           map[string]map[string]*Slot
	slotSkills      map[*Slot]map[*Skill]bool
	skillSlots      map[*Skill]map[*Slot]bool
	securityHelpers []securityHelper
}

// NewRepository creates an empty skill repository.
func NewRepository() *Repository {
	return &Repository{
		types:      make(map[string]Type),
		skills:     make(map[string]map[string]*Skill),
		slots:      make(map[string]map[string]*Slot),
		slotSkills: make(map[*Slot]map[*Skill]bool),
		skillSlots: make(map[*Skill]map[*Slot]bool),
		securityHelpers: []securityHelper{
			&appArmor{},
			&secComp{},
			&uDev{},
			&dBus{},
		},
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

// AllSkills returns all skills of the given type.
// If skillType is the empty string, all skills are returned.
func (r *Repository) AllSkills(skillType string) []*Skill {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Skill
	for _, skillsForSnap := range r.skills {
		for _, skill := range skillsForSnap {
			if skillType == "" || skill.Type == skillType {
				result = append(result, skill)
			}
		}
	}
	sort.Sort(bySkillSnapAndName(result))
	return result
}

// Skills returns the skills offered by the named snap.
func (r *Repository) Skills(snapName string) []*Skill {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Skill
	for _, skill := range r.skills[snapName] {
		result = append(result, skill)
	}
	sort.Sort(bySkillSnapAndName(result))
	return result
}

// Skill returns the specified skill from the named snap.
func (r *Repository) Skill(snapName, skillName string) *Skill {
	r.m.Lock()
	defer r.m.Unlock()

	return r.skills[snapName][skillName]
}

// AddSkill adds a skill to the repository.
// Skill names must be valid snap names, as defined by ValidateName.
// Skill name must be unique within a particular snap.
func (r *Repository) AddSkill(skill *Skill) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject snaps with invalid names
	if err := snap.ValidateName(skill.Snap); err != nil {
		return err
	}
	// Reject skill with invalid names
	if err := ValidateName(skill.Name); err != nil {
		return err
	}
	t := r.types[skill.Type]
	if t == nil {
		return fmt.Errorf("cannot add skill, skill type %q is not known", skill.Type)
	}
	// Reject skill that don't pass type-specific sanitization
	if err := t.SanitizeSkill(skill); err != nil {
		return fmt.Errorf("cannot add skill: %v", err)
	}
	if _, ok := r.skills[skill.Snap][skill.Name]; ok {
		return fmt.Errorf("cannot add skill, snap %q already has skill %q", skill.Snap, skill.Name)
	}
	if r.skills[skill.Snap] == nil {
		r.skills[skill.Snap] = make(map[string]*Skill)
	}
	r.skills[skill.Snap][skill.Name] = skill
	return nil
}

// RemoveSkill removes the named skill provided by a given snap.
// The removed skill must exist and must not be used anywhere.
func (r *Repository) RemoveSkill(snapName, skillName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such skill exists
	skill := r.skills[snapName][skillName]
	if skill == nil {
		return fmt.Errorf("cannot remove skill %q from snap %q, no such skill", skillName, snapName)
	}
	// Ensure that the skill is not used by any slot
	if len(r.skillSlots[skill]) > 0 {
		return fmt.Errorf("cannot remove skill %q from snap %q, it is still granted", skillName, snapName)
	}
	delete(r.skills[snapName], skillName)
	if len(r.skills[snapName]) == 0 {
		delete(r.skills, snapName)
	}
	return nil
}

// AllSlots returns all skill slots of the given type.
// If skillType is the empty string, all skill slots are returned.
func (r *Repository) AllSlots(skillType string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Slot
	for _, slotsForSnap := range r.slots {
		for _, slot := range slotsForSnap {
			if skillType == "" || slot.Type == skillType {
				result = append(result, slot)
			}
		}
	}
	sort.Sort(bySlotSnapAndName(result))
	return result
}

// Slots returns the skill slots offered by the named snap.
func (r *Repository) Slots(snapName string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Slot
	for _, slot := range r.slots[snapName] {
		result = append(result, slot)
	}
	sort.Sort(bySlotSnapAndName(result))
	return result
}

// Slot returns the specified skill slot from the named snap.
func (r *Repository) Slot(snapName, slotName string) *Slot {
	r.m.Lock()
	defer r.m.Unlock()

	return r.slots[snapName][slotName]
}

// AddSlot adds a new slot to the repository.
// Adding a slot with invalid name returns an error.
// Adding a slot that has the same name and snap name as another slot returns an error.
func (r *Repository) AddSlot(slot *Slot) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject snaps with invalid names
	if err := snap.ValidateName(slot.Snap); err != nil {
		return err
	}
	// Reject skill with invalid names
	if err := ValidateName(slot.Name); err != nil {
		return err
	}
	// TODO: ensure that apps are correct
	t := r.types[slot.Type]
	if t == nil {
		return fmt.Errorf("cannot add skill slot, skill type %q is not known", slot.Type)
	}
	if err := t.SanitizeSlot(slot); err != nil {
		return fmt.Errorf("cannot add slot: %v", err)
	}
	if _, ok := r.slots[slot.Snap][slot.Name]; ok {
		return fmt.Errorf("cannot add skill slot, snap %q already has slot %q", slot.Snap, slot.Name)
	}
	if r.slots[slot.Snap] == nil {
		r.slots[slot.Snap] = make(map[string]*Slot)
	}
	r.slots[slot.Snap][slot.Name] = slot
	return nil
}

// RemoveSlot removes a named slot from the given snap.
// Removing a slot that doesn't exist returns an error.
// Removing a slot that uses a skill returns an error.
func (r *Repository) RemoveSlot(snapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such slot exists
	slot := r.slots[snapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot remove skill slot %q from snap %q, no such slot", slotName, snapName)
	}
	// Ensure that the slot is not using any skills
	if len(r.slotSkills[slot]) > 0 {
		return fmt.Errorf("cannot remove slot %q from snap %q, it still uses granted skills", slotName, snapName)
	}
	delete(r.slots[snapName], slotName)
	if len(r.slots[snapName]) == 0 {
		delete(r.slots, snapName)
	}
	return nil
}

// Grant grants the named skill to the named slot of the given snap.
// The skill and the slot must have the same type.
func (r *Repository) Grant(skillSnapName, skillName, slotSnapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such skill exists
	skill := r.skills[skillSnapName][skillName]
	if skill == nil {
		return fmt.Errorf("cannot grant skill %q from snap %q, no such skill", skillName, skillSnapName)
	}
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot grant skill to slot %q from snap %q, no such slot", slotName, slotSnapName)
	}
	// Ensure that skill and slot are compatible
	if slot.Type != skill.Type {
		return fmt.Errorf(`cannot grant skill "%s:%s" (skill type %q) to "%s:%s" (skill type %q)`,
			skillSnapName, skillName, skill.Type, slotSnapName, slotName, slot.Type)
	}
	// Ensure that slot and skill are not connected yet
	if r.slotSkills[slot][skill] {
		// But if they are don't treat this as an error.
		return nil
	}
	// Grant the skill
	if r.slotSkills[slot] == nil {
		r.slotSkills[slot] = make(map[*Skill]bool)
	}
	if r.skillSlots[skill] == nil {
		r.skillSlots[skill] = make(map[*Slot]bool)
	}
	r.slotSkills[slot][skill] = true
	r.skillSlots[skill][slot] = true
	return nil
}

// Revoke revokes the named skill from the slot of the given snap.
//
// Revoke has three modes of operation that depend on the passed arguments:
//
// - If all the arguments are specified then Revoke() finds a specific skill
//   slot and a specific skill and revokes that skill from that skill slot. It is
//   an error if skill or skill slot cannot be found or if the grant does not
//   exist.
// - If skillSnapName and skillName are empty then Revoke() finds the specified
//   skill slot and revokes all the skills granted there. It is not an error if
//   there are no such skills but it is still an error if the skill slot does
//   not exist.
// - If skillSnapName, skillName and slotName are all empty then Revoke finds
//   the specified snap (designated by slotSnapName) and revokes all the skills
//   from all the skill slots found therein. It is not an error if there are no
//   such skills but it is still an error if the snap does not exist or has no
//   slots at all.
func (r *Repository) Revoke(skillSnapName, skillName, slotSnapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	switch {
	case skillSnapName == "" && skillName == "" && slotName == "":
		// Revoke everything from slotSnapName
		return r.revokeEverythingFromSnap(slotSnapName)
	case skillSnapName == "" && skillName == "":
		// Revoke everything from slotSnapName:slotName
		return r.revokeEverythingFromSkillSlot(slotSnapName, slotName)
	default:
		return r.revokeSkillFromSkillSlot(skillSnapName, skillName, slotSnapName, slotName)
	}

}

// revokeEverythingFromSnap finds a specific snap and revokes all the skills granted to all the slots therein.
func (r *Repository) revokeEverythingFromSnap(slotSnapName string) error {
	if _, ok := r.slots[slotSnapName]; !ok {
		return fmt.Errorf("cannot revoke skill from snap %q, no such snap", slotSnapName)
	}
	for _, slot := range r.slots[slotSnapName] {
		for skill := range r.slotSkills[slot] {
			r.revoke(skill, slot)
		}
	}
	return nil
}

// revokeEverythingFromSkillSlot finds a specific skill slot and revokes all the skills granted there.
func (r *Repository) revokeEverythingFromSkillSlot(slotSnapName, slotName string) error {
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot revoke skill from slot %q from snap %q, no such slot", slotName, slotSnapName)
	}
	for skill := range r.slotSkills[slot] {
		r.revoke(skill, slot)
	}
	return nil
}

// revokeSkillFromSkillSlot finds a specific skill slot and skill and revokes it.
func (r *Repository) revokeSkillFromSkillSlot(skillSnapName, skillName, slotSnapName, slotName string) error {
	// Ensure that such skill exists
	skill := r.skills[skillSnapName][skillName]
	if skill == nil {
		return fmt.Errorf("cannot revoke skill %q from snap %q, no such skill", skillName, skillSnapName)
	}
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot revoke skill from slot %q from snap %q, no such slot", slotName, slotSnapName)
	}
	// Ensure that slot and skill are connected
	if !r.slotSkills[slot][skill] {
		return fmt.Errorf("cannot revoke skill %q from snap %q from slot %q from snap %q, it is not granted",
			skillName, skillSnapName, slotName, slotSnapName)
	}
	r.revoke(skill, slot)
	return nil
}

// revoke revokes a specific skill from a specific skill slot.
func (r *Repository) revoke(skill *Skill, slot *Slot) {
	delete(r.slotSkills[slot], skill)
	if len(r.slotSkills[slot]) == 0 {
		delete(r.slotSkills, slot)
	}
	delete(r.skillSlots[skill], slot)
	if len(r.skillSlots[skill]) == 0 {
		delete(r.skillSlots, skill)
	}
}

// GrantedTo returns all the skills granted to a given snap.
func (r *Repository) GrantedTo(snapName string) map[*Slot][]*Skill {
	r.m.Lock()
	defer r.m.Unlock()

	result := make(map[*Slot][]*Skill)
	for _, slot := range r.slots[snapName] {
		for skill := range r.slotSkills[slot] {
			result[slot] = append(result[slot], skill)
		}
		sort.Sort(bySkillSnapAndName(result[slot]))
	}
	return result
}

// GrantedBy returns all of the skills granted by a given snap.
func (r *Repository) GrantedBy(snapName string) map[*Skill][]*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	result := make(map[*Skill][]*Slot)
	for _, skill := range r.skills[snapName] {
		for slot := range r.skillSlots[skill] {
			result[skill] = append(result[skill], slot)
		}
		sort.Sort(bySlotSnapAndName(result[skill]))
	}
	return result
}

// GrantsOf returns all of the slots that were granted the provided skill.
func (r *Repository) GrantsOf(snapName, skillName string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	skill := r.skills[snapName][skillName]
	if skill == nil {
		return nil
	}
	var result []*Slot
	for slot := range r.skillSlots[skill] {
		result = append(result, slot)
	}
	sort.Sort(bySlotSnapAndName(result))
	return result
}

// Support for sort.Interface

type bySkillSnapAndName []*Skill

func (c bySkillSnapAndName) Len() int      { return len(c) }
func (c bySkillSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySkillSnapAndName) Less(i, j int) bool {
	if c[i].Snap != c[j].Snap {
		return c[i].Snap < c[j].Snap
	}
	return c[i].Name < c[j].Name
}

type bySlotSnapAndName []*Slot

func (c bySlotSnapAndName) Len() int      { return len(c) }
func (c bySlotSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySlotSnapAndName) Less(i, j int) bool {
	if c[i].Snap != c[j].Snap {
		return c[i].Snap < c[j].Snap
	}
	return c[i].Name < c[j].Name
}

// LoadBuiltInTypes loads built-in skill types into the provided repository.
func LoadBuiltInTypes(repo *Repository) error {
	return nil
}

// SecuritySnippetsForSnap collects all of the snippets of a given security
// system that affect a given snap. The return value is indexed by app name
// within that snap.
func (r *Repository) SecuritySnippetsForSnap(snapName string, securitySystem SecuritySystem) (map[string][][]byte, error) {
	r.m.Lock()
	defer r.m.Unlock()

	return r.securitySnippetsForSnap(snapName, securitySystem)
}

func (r *Repository) securitySnippetsForSnap(snapName string, securitySystem SecuritySystem) (map[string][][]byte, error) {
	var snippets = make(map[string][][]byte)
	// Find all of the skills that affect this app because of skill consumption.
	for _, slot := range r.slots[snapName] {
		t := r.types[slot.Type]
		for skill := range r.slotSkills[slot] {
			snippet, err := t.SlotSecuritySnippet(skill, securitySystem)
			if err != nil {
				return nil, err
			}
			if snippet == nil {
				continue
			}
			for _, app := range slot.Apps {
				snippets[app] = append(snippets[app], snippet)
			}
		}
	}
	// Find all of the skills that affect this app because of skill offer.
	for _, skill := range r.skills[snapName] {
		t := r.types[skill.Type]
		snippet, err := t.SkillSecuritySnippet(skill, securitySystem)
		if err != nil {
			return nil, err
		}
		if snippet == nil {
			continue
		}
		for _, app := range skill.Apps {
			snippets[app] = append(snippets[app], snippet)
		}
	}
	return snippets, nil
}

// SecurityFilesForSnap returns the paths and contents of security files for a given snap.
func (r *Repository) SecurityFilesForSnap(snapName string) (map[string][]byte, error) {
	r.m.Lock()
	defer r.m.Unlock()

	buffers := make(map[string]*bytes.Buffer)
	for _, helper := range r.securityHelpers {
		if err := r.collectFilesFromSecurityHelper(snapName, helper, buffers); err != nil {
			return nil, err
		}
	}
	blobs := make(map[string][]byte)
	for name, buffer := range buffers {
		blobs[name] = buffer.Bytes()
	}
	return blobs, nil
}

func (r *Repository) collectFilesFromSecurityHelper(snapName string, helper securityHelper, buffers map[string]*bytes.Buffer) error {
	securitySystem := helper.securitySystem()
	appSnippets, err := r.securitySnippetsForSnap(snapName, securitySystem)
	if err != nil {
		return fmt.Errorf("cannot determine %s security snippets for snap %s: %v", securitySystem, snapName, err)
	}
	for appName, snippets := range appSnippets {
		writer := &bytes.Buffer{}
		path := helper.pathForApp(snapName, appName)
		doWrite := func(blob []byte) error {
			_, err = writer.Write(blob)
			if err != nil {
				return fmt.Errorf("cannot write %s file for snap %s (app %s): %v", securitySystem, snapName, appName, err)
			}
			return nil
		}
		if err := doWrite(helper.headerForApp(snapName, appName)); err != nil {
			return err
		}
		for _, snippet := range snippets {
			if err := doWrite(snippet); err != nil {
				return err
			}
		}
		if err := doWrite(helper.footerForApp(snapName, appName)); err != nil {
			return err
		}
		buffers[path] = writer
	}
	return nil
}
