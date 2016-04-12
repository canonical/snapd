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
	"strings"
	"sync"

	"github.com/ubuntu-core/snappy/snap"
)

// Repository stores all known snappy plugs and slots and ifaces.
type Repository struct {
	// Protects the internals from concurrent access.
	m      sync.Mutex
	ifaces map[string]Interface
	// Indexed by [snapName][plugName]
	plugs     map[string]map[string]*Plug
	slots     map[string]map[string]*Slot
	slotPlugs map[*Slot]map[*Plug]bool
	plugSlots map[*Plug]map[*Slot]bool
}

// NewRepository creates an empty plug repository.
func NewRepository() *Repository {
	return &Repository{
		ifaces:    make(map[string]Interface),
		plugs:     make(map[string]map[string]*Plug),
		slots:     make(map[string]map[string]*Slot),
		slotPlugs: make(map[*Slot]map[*Plug]bool),
		plugSlots: make(map[*Plug]map[*Slot]bool),
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
	if err := snap.ValidateName(plug.Snap.Name()); err != nil {
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
	if _, ok := r.plugs[plug.Snap.Name()][plug.Name]; ok {
		return fmt.Errorf("cannot add plug, snap %q already has plug %q", plug.Snap.Name(), plug.Name)
	}
	if r.plugs[plug.Snap.Name()] == nil {
		r.plugs[plug.Snap.Name()] = make(map[string]*Plug)
	}
	r.plugs[plug.Snap.Name()][plug.Name] = plug
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
	if err := snap.ValidateName(slot.Snap.Name()); err != nil {
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
	if _, ok := r.slots[slot.Snap.Name()][slot.Name]; ok {
		return fmt.Errorf("cannot add slot, snap %q already has slot %q", slot.Snap.Name(), slot.Name)
	}
	if r.slots[slot.Snap.Name()] == nil {
		r.slots[slot.Snap.Name()] = make(map[string]*Slot)
	}
	r.slots[slot.Snap.Name()][slot.Name] = slot
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
	slot.Connections = append(slot.Connections, PlugRef{plug.Snap.Name(), plug.Name})
	plug.Connections = append(plug.Connections, SlotRef{slot.Snap.Name(), slot.Name})
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
		if plugRef.Snap == plug.Snap.Name() && plugRef.Name == plug.Name {
			slot.Connections[i] = slot.Connections[len(slot.Connections)-1]
			slot.Connections = slot.Connections[:len(slot.Connections)-1]
			if len(slot.Connections) == 0 {
				slot.Connections = nil
			}
			break
		}
	}
	for i, slotRef := range plug.Connections {
		if slotRef.Snap == slot.Snap.Name() && slotRef.Name == slot.Name {
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
			p := &Plug{
				PlugInfo:    plug.PlugInfo,
				Connections: append([]SlotRef(nil), plug.Connections...),
			}
			sort.Sort(bySlotRef(p.Connections))
			ifaces.Plugs = append(ifaces.Plugs, p)
		}
	}
	for _, slots := range r.slots {
		for _, slot := range slots {
			s := &Slot{
				SlotInfo:    slot.SlotInfo,
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
			for appName := range slot.Apps {
				snippets[appName] = append(snippets[appName], snippet)
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
			for appName := range slot.Apps {
				snippets[appName] = append(snippets[appName], snippet)
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
			for appName := range plug.Apps {
				snippets[appName] = append(snippets[appName], snippet)
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
			for appName := range plug.Apps {
				snippets[appName] = append(snippets[appName], snippet)
			}
		}
	}
	return snippets, nil
}

// BadInterfacesError is returned when some snap interfaces could not be registered.
// Those interfaces not mentioned in the error were successfully registered.
type BadInterfacesError struct {
	snap   string
	issues map[string]string // slot or plug name => message
}

func (e *BadInterfacesError) Error() string {
	inverted := make(map[string][]string)
	for name, reason := range e.issues {
		inverted[reason] = append(inverted[reason], name)
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "snap %q has bad plugs or slots: ", e.snap)
	reasons := make([]string, 0, len(inverted))
	for reason := range inverted {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		names := inverted[reason]
		sort.Strings(names)
		for i, name := range names {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(name)
		}
		fmt.Fprintf(&buf, " (%s); ", reason)
	}
	return strings.TrimSuffix(buf.String(), "; ")
}

// AddSnap adds plugs and slots declared by the given snap to the repository.
//
// This function can be used to implement snap install or, when used along with
// RemoveSnap, snap upgrade.
//
// AddSnap doesn't change existing plugs/slots. The caller is responsible for
// ensuring that the snap is not present in the repository in any way prior to
// calling this function. If this constraint is violated then no changes are
// made and an error is returned.
//
// Each added plug/slot is validated according to the corresponding interface.
// Unknown interfaces and plugs/slots that don't validate are not added.
// Information about those failures are returned to the caller.
func (r *Repository) AddSnap(snapInfo *snap.Info) error {
	r.m.Lock()
	defer r.m.Unlock()

	snapName := snapInfo.Name()

	if r.plugs[snapName] != nil || r.slots[snapName] != nil {
		return fmt.Errorf("cannot register interfaces for snap %q more than once", snapName)
	}

	bad := BadInterfacesError{
		snap:   snapName,
		issues: make(map[string]string),
	}

	for plugName, plugInfo := range snapInfo.Plugs {
		iface, ok := r.ifaces[plugInfo.Interface]
		if !ok {
			bad.issues[plugName] = "unknown interface"
			continue
		}
		plug := &Plug{PlugInfo: plugInfo}
		if err := iface.SanitizePlug(plug); err != nil {
			bad.issues[plugName] = err.Error()
			continue
		}
		if r.plugs[snapName] == nil {
			r.plugs[snapName] = make(map[string]*Plug)
		}
		r.plugs[snapName][plugName] = plug
	}

	for slotName, slotInfo := range snapInfo.Slots {
		iface, ok := r.ifaces[slotInfo.Interface]
		if !ok {
			bad.issues[slotName] = "unknown interface"
			continue
		}
		slot := &Slot{SlotInfo: slotInfo}
		if err := iface.SanitizeSlot(slot); err != nil {
			bad.issues[slotName] = err.Error()
			continue
		}
		if r.slots[snapName] == nil {
			r.slots[snapName] = make(map[string]*Slot)
		}
		r.slots[snapName][slotName] = slot
	}

	if len(bad.issues) > 0 {
		return &bad
	}
	return nil
}

// RemoveSnap removes all the plugs and slots associated with a given snap.
//
// This function can be used to implement snap removal or, when used along with
// AddSnap, snap upgrade.
//
// RemoveSnap does not remove connections. The caller is responsible for
// ensuring that connections are broken before calling this method. If this
// constraint is violated then no changes are made and an error is returned.
func (r *Repository) RemoveSnap(snapName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	for plugName, plug := range r.plugs[snapName] {
		if len(plug.Connections) > 0 {
			return fmt.Errorf("cannot remove connected plug %s.%s", snapName, plugName)
		}
	}
	for slotName, slot := range r.slots[snapName] {
		if len(slot.Connections) > 0 {
			return fmt.Errorf("cannot remove connected slot %s.%s", snapName, slotName)
		}
	}

	for _, plug := range r.plugs[snapName] {
		delete(r.plugSlots, plug)
	}
	delete(r.plugs, snapName)
	for _, slot := range r.slots[snapName] {
		delete(r.slotPlugs, slot)
	}
	delete(r.slots, snapName)

	return nil
}

// DisconnectSnap disconnects all the connections to and from a given snap.
//
// The return value is a list of snap.Info's that were affected.
func (r *Repository) DisconnectSnap(snapName string) ([]*snap.Info, error) {
	r.m.Lock()
	defer r.m.Unlock()

	seen := make(map[*snap.Info]bool)

	for _, plug := range r.plugs[snapName] {
		for slot := range r.plugSlots[plug] {
			r.disconnect(plug, slot)
			seen[plug.Snap] = true
			seen[slot.Snap] = true
		}
	}

	for _, slot := range r.slots[snapName] {
		for plug := range r.slotPlugs[slot] {
			r.disconnect(plug, slot)
			seen[plug.Snap] = true
			seen[slot.Snap] = true
		}
	}

	result := make([]*snap.Info, 0, len(seen))
	for info := range seen {
		result = append(result, info)
	}
	return result, nil
}

// AutoConnectCandidates finds and returns viable auto-connection candidates
// for a given plug.
func (r *Repository) AutoConnectCandidates(plugSnapName, plugName string) []*Slot {
	r.m.Lock()
	defer r.m.Unlock()

	plug := r.plugs[plugSnapName][plugName]
	if plug == nil {
		return nil
	}
	if r.ifaces[plug.Interface].AutoConnect() == false {
		return nil
	}
	var candidates []*Slot
	for _, slotsForSnap := range r.slots {
		for _, slot := range slotsForSnap {
			if slot.Snap.Type == snap.TypeOS && slot.Interface == plug.Interface {
				candidates = append(candidates, slot)
			}
		}
	}
	return candidates
}
