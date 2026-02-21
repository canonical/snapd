// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package polkit

import (
	"bytes"
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/polkit/validate"
	"github.com/snapcore/snapd/snap"
)

type Policy []byte
type Rule []byte

// Specification keeps all the polkit policies.
type Specification struct {
	policyFiles map[string]Policy
	ruleFiles   map[string]Rule
}

// AddPolicy adds a polkit policy file to install.
func (spec *Specification) AddPolicy(nameSuffix string, content Policy) error {
	if old, ok := spec.policyFiles[nameSuffix]; ok && !bytes.Equal(old, content) {
		return fmt.Errorf("internal error: polkit policy content for %q re-defined with different content", nameSuffix)
	}
	if spec.policyFiles == nil {
		spec.policyFiles = make(map[string]Policy)
	}
	spec.policyFiles[nameSuffix] = content
	return nil
}

// Policies returns a map of polkit policies added to the Specification.
func (spec *Specification) Policies() map[string]Policy {
	if spec.policyFiles == nil {
		return nil
	}
	result := make(map[string]Policy, len(spec.policyFiles))
	for k, v := range spec.policyFiles {
		result[k] = make(Policy, len(v))
		copy(result[k], v)
	}
	return result
}

// AddRule adds a polkit rule file to install.
func (spec *Specification) AddRule(nameSuffix string, content Rule) error {
	if err := validate.ValidateRuleNameSuffix(nameSuffix); err != nil {
		return err
	}
	if old, ok := spec.ruleFiles[nameSuffix]; ok && !bytes.Equal(old, content) {
		return fmt.Errorf("internal error: polkit rule content for %q re-defined with different content", nameSuffix)
	}
	if spec.ruleFiles == nil {
		spec.ruleFiles = make(map[string]Rule)
	}
	spec.ruleFiles[nameSuffix] = content
	return nil
}

// Rules returns a map of polkit rules added to the Specification.
// This maps from rule name suffixes (without the ".rules" suffix)
// to the content required to be installed.
func (spec *Specification) Rules() map[string]Rule {
	if spec.ruleFiles == nil {
		return nil
	}
	result := make(map[string]Rule, len(spec.ruleFiles))
	for k, v := range spec.ruleFiles {
		result[k] = make(Rule, len(v))
		copy(result[k], v)
	}
	return result
}

// Implementation of methods required by interfaces.Specification

// ConnectedPlugDefiner can be implemented by interfaces that need to add polkit policies for connected plugs.
type ConnectedPlugDefiner interface {
	PolkitConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}

// AddConnectedPlug records polkit-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if iface, ok := iface.(ConnectedPlugDefiner); ok {
		return iface.PolkitConnectedPlug(spec, plug, slot)
	}
	return nil
}

// ConnectedSlotDefiner can be implemented by interfaces that need to add polkit policies for connected slots.
type ConnectedSlotDefiner interface {
	PolkitConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
}

// AddConnectedSlot records polkit-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if iface, ok := iface.(ConnectedSlotDefiner); ok {
		return iface.PolkitConnectedSlot(spec, plug, slot)
	}
	return nil
}

// PermanentPlugDefiner can be implemented by interfaces that need to add permanent polkit policies for plugs.
type PermanentPlugDefiner interface {
	PolkitPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
}

// AddPermanentPlug records polkit-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	if iface, ok := iface.(PermanentPlugDefiner); ok {
		return iface.PolkitPermanentPlug(spec, plug)
	}
	return nil
}

// PermanentSlotDefiner can be implemented by interfaces that need to add permanent polkit policies for slots.
type PermanentSlotDefiner interface {
	PolkitPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
}

// AddPermanentSlot records polkit-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	if iface, ok := iface.(PermanentSlotDefiner); ok {
		return iface.PolkitPermanentSlot(spec, slot)
	}
	return nil
}
