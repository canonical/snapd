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

package client

import (
	"bytes"
	"encoding/json"
)

// Skill represents a capacity offered by a snap.
type Skill struct {
	Name  string                 `json:"name"`
	Snap  string                 `json:"snap"`
	Type  string                 `json:"type,omitempty"`
	Attrs map[string]interface{} `json:"attrs,omitempty"`
	Apps  []string               `json:"apps,omitempty"`
	Label string                 `json:"label,omitempty"`
}

// Slot represents the potential of a given snap to use a skill.
type Slot struct {
	Name  string                 `json:"name"`
	Snap  string                 `json:"snap"`
	Type  string                 `json:"type,omitempty"`
	Attrs map[string]interface{} `json:"attrs,omitempty"`
	Apps  []string               `json:"apps,omitempty"`
	Label string                 `json:"label,omitempty"`
}

// SkillGrants represents a single skill and slots that are using it.
type SkillGrants struct {
	Skill
	GrantedTo []Slot `json:"granted_to"`
}

// SkillAction represents an action performed on the skill system.
type SkillAction struct {
	Action string `json:"action"`
	Skill  *Skill `json:"skill,omitempty"`
	Slot   *Slot  `json:"slot,omitempty"`
}

// AllSkills returns information about all the skills and their grants.
func (client *Client) AllSkills() (grants []SkillGrants, err error) {
	err = client.doSync("GET", "/2.0/skills", nil, nil, &grants)
	return
}

// performSkillAction performs a single action on the skill system.
func (client *Client) performSkillAction(sa *SkillAction) error {
	b, err := json.Marshal(sa)
	if err != nil {
		return err
	}
	var rsp interface{}
	if err := client.doSync("POST", "/2.0/skills", nil, bytes.NewReader(b), &rsp); err != nil {
		return err
	}
	return nil
}

// Grant grants the named skill to the named slot of the given snap.
// The skill and the slot must have the same type.
func (client *Client) Grant(skillSnapName, skillName, slotSnapName, slotName string) error {
	return client.performSkillAction(&SkillAction{
		Action: "grant",
		Skill: &Skill{
			Snap: skillSnapName,
			Name: skillName,
		},
		Slot: &Slot{
			Snap: slotSnapName,
			Name: slotName,
		},
	})
}

// Revoke revokes the named skill from the slot of the given snap.
func (client *Client) Revoke(skillSnapName, skillName, slotSnapName, slotName string) error {
	return client.performSkillAction(&SkillAction{
		Action: "revoke",
		Skill: &Skill{
			Snap: skillSnapName,
			Name: skillName,
		},
		Slot: &Slot{
			Snap: slotSnapName,
			Name: slotName,
		},
	})
}

// AddSkill adds a skill to the system.
func (client *Client) AddSkill(skill *Skill) error {
	return client.performSkillAction(&SkillAction{
		Action: "add-skill",
		Skill:  skill,
	})
}

// RemoveSkill removes a skill from the system.
func (client *Client) RemoveSkill(snapName, skillName string) error {
	return client.performSkillAction(&SkillAction{
		Action: "remove-skill",
		Skill: &Skill{
			Snap: snapName,
			Name: skillName,
		},
	})
}

// AddSlot adds a slot to the system.
func (client *Client) AddSlot(slot *Slot) error {
	return client.performSkillAction(&SkillAction{
		Action: "add-slot",
		Slot:   slot,
	})
}

// RemoveSlot removes a slot from the system.
func (client *Client) RemoveSlot(snapName, slotName string) error {
	return client.performSkillAction(&SkillAction{
		Action: "remove-slot",
		Slot: &Slot{
			Snap: snapName,
			Name: slotName,
		},
	})
}
