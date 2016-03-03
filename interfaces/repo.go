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

package interfaces

import (
	"bytes"
	"fmt"
	"sort"
	"sync"

	"github.com/ubuntu-core/snappy/snap"
)

// Repository stores all known snappy slots and plugs and ifaces.
type Repository struct {
	// Protects the internals from concurrent access.
	m      sync.Mutex
	ifaces map[string]Interface
	// Indexed by [snapName][slotName]
	slots           map[string]map[string]*Slot
	plugs           map[string]map[string]*Plug
	plugSlots       map[*Plug]map[*Slot]bool
	slotPlugs       map[*Slot]map[*Plug]bool
	securityHelpers []securityHelper
}

// NewRepository creates an empty slot repository.
func NewRepository() *Repository {
	return &Repository{
		ifaces:    make(map[string]Interface),
		slots:     make(map[string]map[string]*Slot),
		plugs:     make(map[string]map[string]*Plug),
		plugSlots: make(map[*Plug]map[*Slot]bool),
		slotPlugs: make(map[*Slot]map[*Plug]bool),
		securityHelpers: []securityHelper{
			&appArmor{},
			&secComp{},
			&uDev{},
			&dBus{},
		},
	}
}

// Interface returns an interface with a given name.
func (r *Repository) Interface(interfaceName string) Interface {
	r.m.Lock()
	defer r.m.Unlock()

	return r.ifaces[interfaceName]
}

// AddInterface adds the provided interface to the repository.
func (r *Repository) AddInterface(i Interface) error {
	r.m.Lock()
	defer r.m.Unlock()

	interfaceName := i.Name()
	if err := ValidateName(interfaceName); err != nil {
		return err
	}
	if _, ok := r.ifaces[interfaceName]; ok {
		return fmt.Errorf("cannot add interface: %q, interface name is in use", interfaceName)
	}
	r.ifaces[interfaceName] = i
	return nil
}

// AllSlots returns all slots of the given interface.
// If interfaceName is the empty string, all slots are returned.
func (r *Repository) AllSlots(interfaceName string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Slot
	for _, slotsForSnap := range r.slots {
		for _, slot := range slotsForSnap {
			if interfaceName == "" || slot.Interface == interfaceName {
				result = append(result, slot)
			}
		}
	}
	sort.Sort(bySlotSnapAndName(result))
	return result
}

// Slots returns the slots offered by the named snap.
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

// Slot returns the specified slot from the named snap.
func (r *Repository) Slot(snapName, slotName string) *Slot {
	r.m.Lock()
	defer r.m.Unlock()

	return r.slots[snapName][slotName]
}

// AddSlot adds a slot to the repository.
// Slot names must be valid snap names, as defined by ValidateName.
// Slot name must be unique within a particular snap.
func (r *Repository) AddSlot(slot *Slot) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject snaps with invalid names
	if err := snap.ValidateName(slot.Snap); err != nil {
		return err
	}
	// Reject slot with invalid names
	if err := ValidateName(slot.Name); err != nil {
		return err
	}
	i := r.ifaces[slot.Interface]
	if i == nil {
		return fmt.Errorf("cannot add slot, interface %q is not known", slot.Interface)
	}
	// Reject slot that don't pass interface-specific sanitization
	if err := i.SanitizeSlot(slot); err != nil {
		return fmt.Errorf("cannot add slot: %v", err)
	}
	if _, ok := r.slots[slot.Snap][slot.Name]; ok {
		return fmt.Errorf("cannot add slot, snap %q already has slot %q", slot.Snap, slot.Name)
	}
	if r.slots[slot.Snap] == nil {
		r.slots[slot.Snap] = make(map[string]*Slot)
	}
	r.slots[slot.Snap][slot.Name] = slot
	return nil
}

// RemoveSlot removes the named slot provided by a given snap.
// The removed slot must exist and must not be used anywhere.
func (r *Repository) RemoveSlot(snapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such slot exists
	slot := r.slots[snapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot remove slot %q from snap %q, no such slot", slotName, snapName)
	}
	// Ensure that the slot is not used by any plug
	if len(r.slotPlugs[slot]) > 0 {
		return fmt.Errorf("cannot remove slot %q from snap %q, it is still connected", slotName, snapName)
	}
	delete(r.slots[snapName], slotName)
	if len(r.slots[snapName]) == 0 {
		delete(r.slots, snapName)
	}
	return nil
}

// AllPlugs returns all plugs of the given interface.
// If interfaceName is the empty string, all plugs are returned.
func (r *Repository) AllPlugs(interfaceName string) []*Plug {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Plug
	for _, plugsForSnap := range r.plugs {
		for _, plug := range plugsForSnap {
			if interfaceName == "" || plug.Interface == interfaceName {
				result = append(result, plug)
			}
		}
	}
	sort.Sort(byPlugSnapAndName(result))
	return result
}

// Plugs returns the plugs offered by the named snap.
func (r *Repository) Plugs(snapName string) []*Plug {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*Plug
	for _, plug := range r.plugs[snapName] {
		result = append(result, plug)
	}
	sort.Sort(byPlugSnapAndName(result))
	return result
}

// Plug returns the specified slot plug from the named snap.
func (r *Repository) Plug(snapName, plugName string) *Plug {
	r.m.Lock()
	defer r.m.Unlock()

	return r.plugs[snapName][plugName]
}

// AddPlug adds a new plug to the repository.
// Adding a plug with invalid name returns an error.
// Adding a plug that has the same name and snap name as another plug returns an error.
func (r *Repository) AddPlug(plug *Plug) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject snaps with invalid names
	if err := snap.ValidateName(plug.Snap); err != nil {
		return err
	}
	// Reject slot with invalid names
	if err := ValidateName(plug.Name); err != nil {
		return err
	}
	// TODO: ensure that apps are correct
	i := r.ifaces[plug.Interface]
	if i == nil {
		return fmt.Errorf("cannot add plug, interface %q is not known", plug.Interface)
	}
	if err := i.SanitizePlug(plug); err != nil {
		return fmt.Errorf("cannot add plug: %v", err)
	}
	if _, ok := r.plugs[plug.Snap][plug.Name]; ok {
		return fmt.Errorf("cannot add plug, snap %q already has plug %q", plug.Snap, plug.Name)
	}
	if r.plugs[plug.Snap] == nil {
		r.plugs[plug.Snap] = make(map[string]*Plug)
	}
	r.plugs[plug.Snap][plug.Name] = plug
	return nil
}

// RemovePlug removes a named plug from the given snap.
// Removing a plug that doesn't exist returns an error.
// Removing a plug that is connected to a slot returns an error.
func (r *Repository) RemovePlug(snapName, plugName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such plug exists
	plug := r.plugs[snapName][plugName]
	if plug == nil {
		return fmt.Errorf("cannot remove slot plug %q from snap %q, no such plug", plugName, snapName)
	}
	// Ensure that the plug is not using any slots
	if len(r.plugSlots[plug]) > 0 {
		return fmt.Errorf("cannot remove plug %q from snap %q, it is still connected", plugName, snapName)
	}
	delete(r.plugs[snapName], plugName)
	if len(r.plugs[snapName]) == 0 {
		delete(r.plugs, snapName)
	}
	return nil
}

// Connect establishes a connection between a slot and a plug.
// The slot and the plug must have the same interface.
func (r *Repository) Connect(slotSnapName, slotName, plugSnapName, plugName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot connect slot %q from snap %q, no such slot", slotName, slotSnapName)
	}
	// Ensure that such plug exists
	plug := r.plugs[plugSnapName][plugName]
	if plug == nil {
		return fmt.Errorf("cannot connect slot to plug %q from snap %q, no such plug", plugName, plugSnapName)
	}
	// Ensure that slot and plug are compatible
	if plug.Interface != slot.Interface {
		return fmt.Errorf(`cannot connect slot "%s:%s" (interface %q) to "%s:%s" (interface %q)`,
			slotSnapName, slotName, slot.Interface, plugSnapName, plugName, plug.Interface)
	}
	// Ensure that plug and slot are not connected yet
	if r.plugSlots[plug][slot] {
		// But if they are don't treat this as an error.
		return nil
	}
	// Connect the slot
	if r.plugSlots[plug] == nil {
		r.plugSlots[plug] = make(map[*Slot]bool)
	}
	if r.slotPlugs[slot] == nil {
		r.slotPlugs[slot] = make(map[*Plug]bool)
	}
	r.plugSlots[plug][slot] = true
	r.slotPlugs[slot][plug] = true
	return nil
}

// Disconnect disconnects the named slot from the plug of the given snap.
//
// Disconnect has three modes of operation that depend on the passed arguments:
//
// - If all the arguments are specified then Disconnect() finds a specific slot
//   plug and a specific slot and disconnects that slot from that slot plug. It is
//   an error if slot or slot plug cannot be found or if the connect does not
//   exist.
// - If slotSnapName and slotName are empty then Disconnect() finds the specified
//   slot plug and disconnects all the slots connected there. It is not an error if
//   there are no such slots but it is still an error if the slot plug does
//   not exist.
// - If slotSnapName, slotName and plugName are all empty then Disconnect finds
//   the specified snap (designated by plugSnapName) and disconnects all the slots
//   from all the plugs found therein. It is not an error if there are no
//   such slots but it is still an error if the snap does not exist or has no
//   plugs at all.
func (r *Repository) Disconnect(slotSnapName, slotName, plugSnapName, plugName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	switch {
	case slotSnapName == "" && slotName == "" && plugName == "":
		// Disconnect everything from plugSnapName
		return r.disconnectEverythingFromSnap(plugSnapName)
	case slotSnapName == "" && slotName == "":
		// Disconnect everything from plugSnapName:plugName
		return r.disconnectEverythingFromPlug(plugSnapName, plugName)
	default:
		return r.disconnectSlotFromPlug(slotSnapName, slotName, plugSnapName, plugName)
	}

}

// disconnectEverythingFromSnap finds a specific snap and disconnects all the slots connected to all the plugs therein.
func (r *Repository) disconnectEverythingFromSnap(plugSnapName string) error {
	if _, ok := r.plugs[plugSnapName]; !ok {
		return fmt.Errorf("cannot disconnect slot from snap %q, no such snap", plugSnapName)
	}
	for _, plug := range r.plugs[plugSnapName] {
		for slot := range r.plugSlots[plug] {
			r.disconnect(slot, plug)
		}
	}
	return nil
}

// disconnectEverythingFromPlug finds a specific slot plug and disconnects all the slots connected there.
func (r *Repository) disconnectEverythingFromPlug(plugSnapName, plugName string) error {
	// Ensure that such plug exists
	plug := r.plugs[plugSnapName][plugName]
	if plug == nil {
		return fmt.Errorf("cannot disconnect slot from plug %q from snap %q, no such plug", plugName, plugSnapName)
	}
	for slot := range r.plugSlots[plug] {
		r.disconnect(slot, plug)
	}
	return nil
}

// disconnectSlotFromPlug finds a specific slot plug and slot and disconnects it.
func (r *Repository) disconnectSlotFromPlug(slotSnapName, slotName, plugSnapName, plugName string) error {
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot disconnect slot %q from snap %q, no such slot", slotName, slotSnapName)
	}
	// Ensure that such plug exists
	plug := r.plugs[plugSnapName][plugName]
	if plug == nil {
		return fmt.Errorf("cannot disconnect slot from plug %q from snap %q, no such plug", plugName, plugSnapName)
	}
	// Ensure that plug and slot are connected
	if !r.plugSlots[plug][slot] {
		return fmt.Errorf("cannot disconnect slot %q from snap %q from plug %q from snap %q, it is not connected",
			slotName, slotSnapName, plugName, plugSnapName)
	}
	r.disconnect(slot, plug)
	return nil
}

// disconnect disconnects a slot from a plug.
func (r *Repository) disconnect(slot *Slot, plug *Plug) {
	delete(r.plugSlots[plug], slot)
	if len(r.plugSlots[plug]) == 0 {
		delete(r.plugSlots, plug)
	}
	delete(r.slotPlugs[slot], plug)
	if len(r.slotPlugs[slot]) == 0 {
		delete(r.slotPlugs, slot)
	}
}

// ConnectedPlugs returns all the slots connected to a given snap.
func (r *Repository) ConnectedPlugs(snapName string) map[*Plug][]*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	result := make(map[*Plug][]*Slot)
	for _, plug := range r.plugs[snapName] {
		for slot := range r.plugSlots[plug] {
			result[plug] = append(result[plug], slot)
		}
		sort.Sort(bySlotSnapAndName(result[plug]))
	}
	return result
}

// ConnectedSlots returns all of the slots connected by a given snap.
func (r *Repository) ConnectedSlots(snapName string) map[*Slot][]*Plug {
	r.m.Lock()
	defer r.m.Unlock()

	result := make(map[*Slot][]*Plug)
	for _, slot := range r.slots[snapName] {
		for plug := range r.slotPlugs[slot] {
			result[slot] = append(result[slot], plug)
		}
		sort.Sort(byPlugSnapAndName(result[slot]))
	}
	return result
}

// SlotConnections returns all of the plugs that are connected a given slot.
func (r *Repository) SlotConnections(snapName, slotName string) []*Plug {
	r.m.Lock()
	defer r.m.Unlock()

	slot := r.slots[snapName][slotName]
	if slot == nil {
		return nil
	}
	var result []*Plug
	for plug := range r.slotPlugs[slot] {
		result = append(result, plug)
	}
	sort.Sort(byPlugSnapAndName(result))
	return result
}

// PlugConnections returns all of the slots that are connected a given plug.
func (r *Repository) PlugConnections(snapName, plugName string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	plug := r.plugs[snapName][plugName]
	if plug == nil {
		return nil
	}
	var result []*Slot
	for slot := range r.plugSlots[plug] {
		result = append(result, slot)
	}
	sort.Sort(bySlotSnapAndName(result))
	return result
}

// Interfaces returns object holding a lists of all the slots and plugs and their connections.
func (r *Repository) Interfaces() *Interfaces {
	r.m.Lock()
	defer r.m.Unlock()

	ifaces := &Interfaces{}
	// Copy and flatten slots and plugs
	for _, slots := range r.slots {
		for _, slot := range slots {
			// Copy part of the data explicitly, leaving out attrs and apps.
			p := &Slot{
				Name:      slot.Name,
				Snap:      slot.Snap,
				Interface: slot.Interface,
				Label:     slot.Label,
			}
			// Add connection details
			for plug := range r.slotPlugs[slot] {
				p.Connections = append(p.Connections, PlugRef{
					Name: plug.Name,
					Snap: plug.Snap,
				})
			}
			sort.Sort(byPlugRef(p.Connections))
			ifaces.Slots = append(ifaces.Slots, p)
		}
	}
	for _, plugs := range r.plugs {
		for _, plug := range plugs {
			// Copy part of the data explicitly, leaving out attrs and apps.
			s := &Plug{
				Name:      plug.Name,
				Snap:      plug.Snap,
				Interface: plug.Interface,
				Label:     plug.Label,
			}
			// Add connection details
			for slot := range r.plugSlots[plug] {
				s.Connections = append(s.Connections, SlotRef{
					Name: slot.Name,
					Snap: slot.Snap,
				})
			}
			sort.Sort(bySlotRef(s.Connections))
			ifaces.Plugs = append(ifaces.Plugs, s)
		}
	}
	sort.Sort(bySlotSnapAndName(ifaces.Slots))
	sort.Sort(byPlugSnapAndName(ifaces.Plugs))
	return ifaces
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
	// Find all of the plugs that affect this snap because of slot connection.
	for _, plug := range r.plugs[snapName] {
		i := r.ifaces[plug.Interface]
		for slot := range r.plugSlots[plug] {
			snippet, err := i.PlugSecuritySnippet(slot, plug, securitySystem)
			if err != nil {
				return nil, err
			}
			if snippet == nil {
				continue
			}
			for _, app := range plug.Apps {
				snippets[app] = append(snippets[app], snippet)
			}
		}
	}
	// Find all of the slots that affect this snap because of plug connection
	for _, slot := range r.slots[snapName] {
		i := r.ifaces[slot.Interface]
		for plug := range r.slotPlugs[slot] {
			snippet, err := i.SlotSecuritySnippet(slot, plug, securitySystem)
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
