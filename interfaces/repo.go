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

// Repository stores all known snappy plugs and slots and ifaces.
type Repository struct {
	// Protects the internals from concurrent access.
	m      sync.Mutex
	ifaces map[string]Interface
	// Indexed by [snapName][plugName]
	plugs           map[string]map[string]*Plug
	slots           map[string]map[string]*Slot
	slotPlugs       map[*Slot]map[*Plug]bool
	plugSlots       map[*Plug]map[*Slot]bool
	securityHelpers []securityHelper
}

// NewRepository creates an empty plug repository.
func NewRepository() *Repository {
	return &Repository{
		ifaces:    make(map[string]Interface),
		plugs:     make(map[string]map[string]*Plug),
		slots:     make(map[string]map[string]*Slot),
		slotPlugs: make(map[*Slot]map[*Plug]bool),
		plugSlots: make(map[*Plug]map[*Slot]bool),
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

// Plug returns the specified plug from the named snap.
func (r *Repository) Plug(snapName, plugName string) *Plug {
	r.m.Lock()
	defer r.m.Unlock()

	return r.plugs[snapName][plugName]
}

// AddPlug adds a plug to the repository.
// Plug names must be valid snap names, as defined by ValidateName.
// Plug name must be unique within a particular snap.
func (r *Repository) AddPlug(plug *Plug) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Reject snaps with invalid names
	if err := snap.ValidateName(plug.Snap); err != nil {
		return err
	}
	// Reject plug with invalid names
	if err := ValidateName(plug.Name); err != nil {
		return err
	}
	i := r.ifaces[plug.Interface]
	if i == nil {
		return fmt.Errorf("cannot add plug, interface %q is not known", plug.Interface)
	}
	// Reject plug that don't pass interface-specific sanitization
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

// RemovePlug removes the named plug provided by a given snap.
// The removed plug must exist and must not be used anywhere.
func (r *Repository) RemovePlug(snapName, plugName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such plug exists
	plug := r.plugs[snapName][plugName]
	if plug == nil {
		return fmt.Errorf("cannot remove plug %q from snap %q, no such plug", plugName, snapName)
	}
	// Ensure that the plug is not used by any slot
	if len(r.plugSlots[plug]) > 0 {
		return fmt.Errorf("cannot remove plug %q from snap %q, it is still connected", plugName, snapName)
	}
	delete(r.plugs[snapName], plugName)
	if len(r.plugs[snapName]) == 0 {
		delete(r.plugs, snapName)
	}
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

// Slot returns the specified plug slot from the named snap.
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
	// Reject plug with invalid names
	if err := ValidateName(slot.Name); err != nil {
		return err
	}
	// TODO: ensure that apps are correct
	i := r.ifaces[slot.Interface]
	if i == nil {
		return fmt.Errorf("cannot add slot, interface %q is not known", slot.Interface)
	}
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

// RemoveSlot removes a named slot from the given snap.
// Removing a slot that doesn't exist returns an error.
// Removing a slot that is connected to a plug returns an error.
func (r *Repository) RemoveSlot(snapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such slot exists
	slot := r.slots[snapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot remove plug slot %q from snap %q, no such slot", slotName, snapName)
	}
	// Ensure that the slot is not using any plugs
	if len(r.slotPlugs[slot]) > 0 {
		return fmt.Errorf("cannot remove slot %q from snap %q, it is still connected", slotName, snapName)
	}
	delete(r.slots[snapName], slotName)
	if len(r.slots[snapName]) == 0 {
		delete(r.slots, snapName)
	}
	return nil
}

// Connect establishes a connection between a plug and a slot.
// The plug and the slot must have the same interface.
func (r *Repository) Connect(plugSnapName, plugName, slotSnapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Ensure that such plug exists
	plug := r.plugs[plugSnapName][plugName]
	if plug == nil {
		return fmt.Errorf("cannot connect plug %q from snap %q, no such plug", plugName, plugSnapName)
	}
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot connect plug to slot %q from snap %q, no such slot", slotName, slotSnapName)
	}
	// Ensure that plug and slot are compatible
	if slot.Interface != plug.Interface {
		return fmt.Errorf(`cannot connect plug "%s:%s" (interface %q) to "%s:%s" (interface %q)`,
			plugSnapName, plugName, plug.Interface, slotSnapName, slotName, slot.Interface)
	}
	// Ensure that slot and plug are not connected yet
	if r.slotPlugs[slot][plug] {
		// But if they are don't treat this as an error.
		return nil
	}
	// Connect the plug
	if r.slotPlugs[slot] == nil {
		r.slotPlugs[slot] = make(map[*Plug]bool)
	}
	if r.plugSlots[plug] == nil {
		r.plugSlots[plug] = make(map[*Slot]bool)
	}
	r.slotPlugs[slot][plug] = true
	r.plugSlots[plug][slot] = true
	slot.Connections = append(slot.Connections, PlugRef{plug.Snap, plug.Name})
	plug.Connections = append(plug.Connections, SlotRef{slot.Snap, slot.Name})
	return nil
}

// Disconnect disconnects the named plug from the slot of the given snap.
//
// Disconnect has three modes of operation that depend on the passed arguments:
//
// - If all the arguments are specified then Disconnect() finds a specific slot
//   and a specific plug and disconnects that plug from that plug slot. It is
//   an error if plug or plug slot cannot be found or if the connect does not
//   exist.
// - If plugSnapName and plugName are empty then Disconnect() finds the specified
//   slot and disconnects all the plugs connected there. It is not an error if
//   there are no such plugs but it is still an error if the plug slot does
//   not exist.
// - If plugSnapName, plugName and slotName are all empty then Disconnect finds
//   the specified snap (designated by slotSnapName) and disconnects all the plugs
//   from all the slots found therein. It is not an error if there are no
//   such plugs but it is still an error if the snap does not exist or has no
//   slots at all.
func (r *Repository) Disconnect(plugSnapName, plugName, slotSnapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	switch {
	case plugSnapName == "" && plugName == "" && slotName == "":
		// Disconnect everything from slotSnapName
		return r.disconnectEverythingFromSnap(slotSnapName)
	case plugSnapName == "" && plugName == "":
		// Disconnect everything from slotSnapName:slotName
		return r.disconnectEverythingFromSlot(slotSnapName, slotName)
	default:
		return r.disconnectPlugFromSlot(plugSnapName, plugName, slotSnapName, slotName)
	}

}

// disconnectEverythingFromSnap finds a specific snap and disconnects all the plugs connected to all the slots therein.
func (r *Repository) disconnectEverythingFromSnap(slotSnapName string) error {
	if _, ok := r.slots[slotSnapName]; !ok {
		return fmt.Errorf("cannot disconnect plug from snap %q, no such snap", slotSnapName)
	}
	for _, slot := range r.slots[slotSnapName] {
		for plug := range r.slotPlugs[slot] {
			r.disconnect(plug, slot)
		}
	}
	return nil
}

// disconnectEverythingFromSlot finds a specific plug slot and disconnects all the plugs connected there.
func (r *Repository) disconnectEverythingFromSlot(slotSnapName, slotName string) error {
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot disconnect plug from slot %q from snap %q, no such slot", slotName, slotSnapName)
	}
	for plug := range r.slotPlugs[slot] {
		r.disconnect(plug, slot)
	}
	return nil
}

// disconnectPlugFromSlot finds a specific plug slot and plug and disconnects it.
func (r *Repository) disconnectPlugFromSlot(plugSnapName, plugName, slotSnapName, slotName string) error {
	// Ensure that such plug exists
	plug := r.plugs[plugSnapName][plugName]
	if plug == nil {
		return fmt.Errorf("cannot disconnect plug %q from snap %q, no such plug", plugName, plugSnapName)
	}
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("cannot disconnect plug from slot %q from snap %q, no such slot", slotName, slotSnapName)
	}
	// Ensure that slot and plug are connected
	if !r.slotPlugs[slot][plug] {
		return fmt.Errorf("cannot disconnect plug %q from snap %q from slot %q from snap %q, it is not connected",
			plugName, plugSnapName, slotName, slotSnapName)
	}
	r.disconnect(plug, slot)
	return nil
}

// disconnect disconnects a plug from a slot.
func (r *Repository) disconnect(plug *Plug, slot *Slot) {
	delete(r.slotPlugs[slot], plug)
	if len(r.slotPlugs[slot]) == 0 {
		delete(r.slotPlugs, slot)
	}
	delete(r.plugSlots[plug], slot)
	if len(r.plugSlots[plug]) == 0 {
		delete(r.plugSlots, plug)
	}
	for i, plugRef := range slot.Connections {
		if plugRef.Snap == plug.Snap && plugRef.Name == plug.Name {
			slot.Connections[i] = slot.Connections[len(slot.Connections)-1]
			slot.Connections = slot.Connections[:len(slot.Connections)-1]
			if len(slot.Connections) == 0 {
				slot.Connections = nil
			}
			break
		}
	}
	for i, slotRef := range plug.Connections {
		if slotRef.Snap == slot.Snap && slotRef.Name == slot.Name {
			plug.Connections[i] = plug.Connections[len(plug.Connections)-1]
			plug.Connections = plug.Connections[:len(plug.Connections)-1]
			if len(plug.Connections) == 0 {
				plug.Connections = nil
			}
			break
		}
	}
}

// Interfaces returns object holding a lists of all the plugs and slots and their connections.
func (r *Repository) Interfaces() *Interfaces {
	r.m.Lock()
	defer r.m.Unlock()

	ifaces := &Interfaces{}
	// Copy and flatten plugs and slots
	for _, plugs := range r.plugs {
		for _, plug := range plugs {
			// Copy part of the data explicitly, leaving out attrs and apps.
			p := &Plug{
				Name:        plug.Name,
				Snap:        plug.Snap,
				Interface:   plug.Interface,
				Label:       plug.Label,
				Connections: append([]SlotRef(nil), plug.Connections...),
			}
			sort.Sort(bySlotRef(p.Connections))
			ifaces.Plugs = append(ifaces.Plugs, p)
		}
	}
	for _, slots := range r.slots {
		for _, slot := range slots {
			// Copy part of the data explicitly, leaving out attrs and apps.
			s := &Slot{
				Name:        slot.Name,
				Snap:        slot.Snap,
				Interface:   slot.Interface,
				Label:       slot.Label,
				Connections: append([]PlugRef(nil), slot.Connections...),
			}
			sort.Sort(byPlugRef(s.Connections))
			ifaces.Slots = append(ifaces.Slots, s)
		}
	}
	sort.Sort(byPlugSnapAndName(ifaces.Plugs))
	sort.Sort(bySlotSnapAndName(ifaces.Slots))
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
	// Find all of the slots that affect this snap because of plug connection.
	for _, slot := range r.slots[snapName] {
		iface := r.ifaces[slot.Interface]
		// Add the static snippet for the slot
		snippet, err := iface.PermanentSlotSnippet(slot, securitySystem)
		if err != nil {
			return nil, err
		}
		if snippet != nil {
			for _, app := range slot.Apps {
				snippets[app] = append(snippets[app], snippet)
			}
		}
		// Add connection-specific snippet specific to each plug
		for plug := range r.slotPlugs[slot] {
			snippet, err := iface.ConnectedSlotSnippet(plug, slot, securitySystem)
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
	// Find all of the plugs that affect this snap because of slot connection
	for _, plug := range r.plugs[snapName] {
		iface := r.ifaces[plug.Interface]
		// Add the static snippet for the plug
		snippet, err := iface.PermanentPlugSnippet(plug, securitySystem)
		if err != nil {
			return nil, err
		}
		if snippet != nil {
			for _, app := range plug.Apps {
				snippets[app] = append(snippets[app], snippet)
			}
		}
		// Add connection-specific snippet specific to each slot
		for slot := range r.plugSlots[plug] {
			snippet, err := iface.ConnectedPlugSnippet(plug, slot, securitySystem)
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
	snapVersion, snapOrigin, snapApps, err := ActiveSnapMetaData(snapName)
	if err != nil {
		return fmt.Errorf("cannot determine meta-data for snap %s: %v", snapName, err)
	}
	appSnippets, err := r.securitySnippetsForSnap(snapName, securitySystem)
	if err != nil {
		return fmt.Errorf("cannot determine %s security snippets for snap %s: %v", securitySystem, snapName, err)
	}
	for _, appName := range snapApps {
		// NOTE: this explicitly iterates over all apps, even if they have no granted skills.
		// This way after revoking a skill permission are updated to reflect that.
		snippets := appSnippets[appName]
		writer := &bytes.Buffer{}
		path := helper.pathForApp(snapName, snapVersion, snapOrigin, appName)
		doWrite := func(blob []byte) error {
			_, err = writer.Write(blob)
			if err != nil {
				return fmt.Errorf("cannot write %s file for snap %s (app %s): %v", securitySystem, snapName, appName, err)
			}
			return nil
		}
		if err := doWrite(helper.headerForApp(snapName, snapVersion, snapOrigin, appName)); err != nil {
			return err
		}
		for _, snippet := range snippets {
			if err := doWrite(snippet); err != nil {
				return err
			}
		}
		if err := doWrite(helper.footerForApp(snapName, snapVersion, snapOrigin, appName)); err != nil {
			return err
		}
		buffers[path] = writer
	}
	return nil
}
