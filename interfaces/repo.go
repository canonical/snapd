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
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/snapcore/snapd/snap"
)

// Repository stores all known snappy plugs and slots and ifaces.
type Repository struct {
	// Protects the internals from concurrent access.
	m      sync.Mutex
	ifaces map[string]Interface
	// Indexed by [snapName][plugName]
	plugs map[string]map[string]*snap.PlugInfo
	slots map[string]map[string]*snap.SlotInfo
	// given a slot and a plug, are they connected?
	slotPlugs map[*snap.SlotInfo]map[*snap.PlugInfo]*Connection
	// given a plug and a slot, are they connected?
	plugSlots map[*snap.PlugInfo]map[*snap.SlotInfo]*Connection
	backends  map[SecuritySystem]SecurityBackend
}

// NewRepository creates an empty plug repository.
func NewRepository() *Repository {
	repo := &Repository{
		ifaces:    make(map[string]Interface),
		plugs:     make(map[string]map[string]*snap.PlugInfo),
		slots:     make(map[string]map[string]*snap.SlotInfo),
		slotPlugs: make(map[*snap.SlotInfo]map[*snap.PlugInfo]*Connection),
		plugSlots: make(map[*snap.PlugInfo]map[*snap.SlotInfo]*Connection),
		backends:  make(map[SecuritySystem]SecurityBackend),
	}

	return repo
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
	if err := snap.ValidateInterfaceName(interfaceName); err != nil {
		return err
	}
	if _, ok := r.ifaces[interfaceName]; ok {
		return fmt.Errorf("cannot add interface: %q, interface name is in use", interfaceName)
	}
	r.ifaces[interfaceName] = i
	return nil
}

// AllInterfaces returns all the interfaces added to the repository, ordered by name.
func (r *Repository) AllInterfaces() []Interface {
	r.m.Lock()
	defer r.m.Unlock()

	ifaces := make([]Interface, 0, len(r.ifaces))
	for _, iface := range r.ifaces {
		ifaces = append(ifaces, iface)
	}
	sort.Sort(byInterfaceName(ifaces))
	return ifaces
}

// InfoOptions describes options for Info.
//
// Names: return just this subset if non-empty.
// Doc: return documentation.
// Plugs: return information about plugs.
// Slots: return information about slots.
// Connected: only consider interfaces with at least one connection.
type InfoOptions struct {
	Names     []string
	Doc       bool
	Plugs     bool
	Slots     bool
	Connected bool
}

func (r *Repository) interfaceInfo(iface Interface, opts *InfoOptions) *Info {
	// NOTE: InfoOptions.Connected is handled by Info
	si := StaticInfoOf(iface)
	ifaceName := iface.Name()
	ii := &Info{
		Name:    ifaceName,
		Summary: si.Summary,
	}
	if opts != nil && opts.Doc {
		// Collect documentation URL
		ii.DocURL = si.DocURL
	}
	if opts != nil && opts.Plugs {
		// Collect all plugs of this interface type.
		for _, snapName := range sortedSnapNamesWithPlugs(r.plugs) {
			for _, plugName := range sortedPlugNames(r.plugs[snapName]) {
				plugInfo := r.plugs[snapName][plugName]
				if plugInfo.Interface == ifaceName {
					ii.Plugs = append(ii.Plugs, plugInfo)
				}
			}
		}
	}
	if opts != nil && opts.Slots {
		// Collect all slots of this interface type.
		for _, snapName := range sortedSnapNamesWithSlots(r.slots) {
			for _, slotName := range sortedSlotNames(r.slots[snapName]) {
				slotInfo := r.slots[snapName][slotName]
				if slotInfo.Interface == ifaceName {
					ii.Slots = append(ii.Slots, slotInfo)
				}
			}
		}
	}
	return ii
}

// Info returns information about interfaces in the system.
//
// If names is empty then all interfaces are considered. Query options decide
// which data to return but can also skip interfaces without connections. See
// the documentation of InfoOptions for details.
func (r *Repository) Info(opts *InfoOptions) []*Info {
	r.m.Lock()
	defer r.m.Unlock()

	// If necessary compute the set of interfaces with any connections.
	var connected map[string]bool
	if opts != nil && opts.Connected {
		connected = make(map[string]bool)
		for _, plugMap := range r.slotPlugs {
			for plug, conn := range plugMap {
				if conn != nil {
					connected[plug.Interface] = true
				}
			}
		}
		for _, slotMap := range r.plugSlots {
			for slot, conn := range slotMap {
				if conn != nil {
					connected[slot.Interface] = true
				}
			}
		}
	}

	// If weren't asked about specific interfaces then query every interface.
	var names []string
	if opts == nil || len(opts.Names) == 0 {
		for _, iface := range r.ifaces {
			name := iface.Name()
			if connected == nil || connected[name] {
				// Optionally filter out interfaces without connections.
				names = append(names, name)
			}
		}
	} else {
		names = make([]string, len(opts.Names))
		copy(names, opts.Names)
	}
	sort.Strings(names)

	// Query each interface we are interested in.
	infos := make([]*Info, 0, len(names))
	for _, name := range names {
		if iface, ok := r.ifaces[name]; ok {
			if connected == nil || connected[name] {
				infos = append(infos, r.interfaceInfo(iface, opts))
			}
		}
	}
	return infos
}

// AddBackend adds the provided security backend to the repository.
func (r *Repository) AddBackend(backend SecurityBackend) error {
	r.m.Lock()
	defer r.m.Unlock()

	name := backend.Name()
	if _, ok := r.backends[name]; ok {
		return fmt.Errorf("cannot add backend %q, security system name is in use", name)
	}
	r.backends[name] = backend
	return nil
}

// AllPlugs returns all plugs of the given interface.
// If interfaceName is the empty string, all plugs are returned.
func (r *Repository) AllPlugs(interfaceName string) []*snap.PlugInfo {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*snap.PlugInfo
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
func (r *Repository) Plugs(snapName string) []*snap.PlugInfo {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*snap.PlugInfo
	for _, plug := range r.plugs[snapName] {
		result = append(result, plug)
	}
	sort.Sort(byPlugSnapAndName(result))
	return result
}

// Plug returns the specified plug from the named snap.
func (r *Repository) Plug(snapName, plugName string) *snap.PlugInfo {
	r.m.Lock()
	defer r.m.Unlock()

	return r.plugs[snapName][plugName]
}

// AddPlug adds a plug to the repository.
// Plug names must be valid snap names, as defined by ValidateName.
// Plug name must be unique within a particular snap.
func (r *Repository) AddPlug(plug *snap.PlugInfo) error {
	r.m.Lock()
	defer r.m.Unlock()

	snapName := plug.Snap.InstanceName()

	// Reject snaps with invalid names
	if err := snap.ValidateName(snapName); err != nil {
		return err
	}
	// Reject plugs with invalid names
	if err := snap.ValidatePlugName(plug.Name); err != nil {
		return err
	}
	i := r.ifaces[plug.Interface]
	if i == nil {
		return fmt.Errorf("cannot add plug, interface %q is not known", plug.Interface)
	}
	if _, ok := r.plugs[snapName][plug.Name]; ok {
		return fmt.Errorf("snap %q has plugs conflicting on name %q", snapName, plug.Name)
	}
	if _, ok := r.slots[snapName][plug.Name]; ok {
		return fmt.Errorf("snap %q has plug and slot conflicting on name %q", snapName, plug.Name)
	}
	if r.plugs[snapName] == nil {
		r.plugs[snapName] = make(map[string]*snap.PlugInfo)
	}
	r.plugs[snapName][plug.Name] = plug
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
func (r *Repository) AllSlots(interfaceName string) []*snap.SlotInfo {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*snap.SlotInfo
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
func (r *Repository) Slots(snapName string) []*snap.SlotInfo {
	r.m.Lock()
	defer r.m.Unlock()

	var result []*snap.SlotInfo
	for _, slot := range r.slots[snapName] {
		result = append(result, slot)
	}
	sort.Sort(bySlotSnapAndName(result))
	return result
}

// Slot returns the specified slot from the named snap.
func (r *Repository) Slot(snapName, slotName string) *snap.SlotInfo {
	r.m.Lock()
	defer r.m.Unlock()

	return r.slots[snapName][slotName]
}

// AddSlot adds a new slot to the repository.
// Adding a slot with invalid name returns an error.
// Adding a slot that has the same name and snap name as another slot returns an error.
func (r *Repository) AddSlot(slot *snap.SlotInfo) error {
	r.m.Lock()
	defer r.m.Unlock()

	snapName := slot.Snap.InstanceName()

	// Reject snaps with invalid names
	if err := snap.ValidateName(snapName); err != nil {
		return err
	}
	// Reject slots with invalid names
	if err := snap.ValidateSlotName(slot.Name); err != nil {
		return err
	}
	// TODO: ensure that apps are correct
	i := r.ifaces[slot.Interface]
	if i == nil {
		return fmt.Errorf("cannot add slot, interface %q is not known", slot.Interface)
	}
	if _, ok := r.slots[snapName][slot.Name]; ok {
		return fmt.Errorf("snap %q has slots conflicting on name %q", snapName, slot.Name)
	}
	if _, ok := r.plugs[snapName][slot.Name]; ok {
		return fmt.Errorf("snap %q has plug and slot conflicting on name %q", snapName, slot.Name)
	}
	if r.slots[snapName] == nil {
		r.slots[snapName] = make(map[string]*snap.SlotInfo)
	}
	r.slots[snapName][slot.Name] = slot
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
		return fmt.Errorf("cannot remove slot %q from snap %q, no such slot", slotName, snapName)
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

// ResolveConnect resolves potentially missing plug or slot names and returns a
// fully populated connection reference.
func (r *Repository) ResolveConnect(plugSnapName, plugName, slotSnapName, slotName string) (*ConnRef, error) {
	r.m.Lock()
	defer r.m.Unlock()

	if plugSnapName == "" {
		return nil, fmt.Errorf("cannot resolve connection, plug snap name is empty")
	}
	if plugName == "" {
		return nil, fmt.Errorf("cannot resolve connection, plug name is empty")
	}
	// Ensure that such plug exists
	plug := r.plugs[plugSnapName][plugName]
	if plug == nil {
		return nil, fmt.Errorf("snap %q has no plug named %q", plugSnapName, plugName)
	}

	if slotSnapName == "" {
		// Use the core snap if the slot-side snap name is empty
		switch {
		case r.slots["snapd"] != nil:
			slotSnapName = "snapd"
		case r.slots["core"] != nil:
			slotSnapName = "core"
		case r.slots["ubuntu-core"] != nil:
			slotSnapName = "ubuntu-core"
		default:
			// XXX: perhaps this should not be an error and instead it should
			// silently assume "core" now?
			return nil, fmt.Errorf("cannot resolve connection, slot snap name is empty")
		}
	}
	if slotName == "" {
		// Find the unambiguous slot that satisfies plug requirements
		var candidates []string
		for candidateSlotName, candidateSlot := range r.slots[slotSnapName] {
			// TODO: use some smarter matching (e.g. against $attrs)
			if candidateSlot.Interface == plug.Interface {
				candidates = append(candidates, candidateSlotName)
			}
		}
		switch len(candidates) {
		case 0:
			return nil, fmt.Errorf("snap %q has no %q interface slots", slotSnapName, plug.Interface)
		case 1:
			slotName = candidates[0]
		default:
			sort.Strings(candidates)
			return nil, fmt.Errorf("snap %q has multiple %q interface slots: %s", slotSnapName, plug.Interface, strings.Join(candidates, ", "))
		}
	}

	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return nil, fmt.Errorf("snap %q has no slot named %q", slotSnapName, slotName)
	}
	// Ensure that plug and slot are compatible
	if slot.Interface != plug.Interface {
		return nil, fmt.Errorf("cannot connect %s:%s (%q interface) to %s:%s (%q interface)",
			plugSnapName, plugName, plug.Interface, slotSnapName, slotName, slot.Interface)
	}
	return NewConnRef(plug, slot), nil
}

// ResolveDisconnect resolves potentially missing plug or slot names and
// returns a list of fully populated connection references that can be
// disconnected.
//
// It can be used in two different ways:
// 1: snap disconnect <snap>:<plug> <snap>:<slot>
// 2: snap disconnect <snap>:<plug or slot>
//
// In the first case the referenced plug and slot must be connected.  In the
// second case any matching connection are returned but it is not an error if
// there are no connections.
//
// In both cases the snap name can be omitted to implicitly refer to the core
// snap. If there's no core snap it is simply assumed to be called "core" to
// provide consistent error messages.
func (r *Repository) ResolveDisconnect(plugSnapName, plugName, slotSnapName, slotName string) ([]*ConnRef, error) {
	r.m.Lock()
	defer r.m.Unlock()

	coreSnapName, _ := r.guessCoreSnapName()
	if coreSnapName == "" {
		// This is not strictly speaking true BUT when there's no core snap the
		// produced error messages are consistent to when the is a core snap
		// and it has the modern form.
		coreSnapName = "core"
	}

	// There are two allowed forms (see snap disconnect --help)
	switch {
	// 1: <snap>:<plug> <snap>:<slot>
	// Return exactly one plug/slot or an error if it doesn't exist.
	case plugName != "" && slotName != "":
		// The snap name can be omitted to implicitly refer to the core snap.
		if plugSnapName == "" {
			plugSnapName = coreSnapName
		}
		// Ensure that such plug exists
		plug := r.plugs[plugSnapName][plugName]
		if plug == nil {
			return nil, fmt.Errorf("snap %q has no plug named %q", plugSnapName, plugName)
		}
		// The snap name can be omitted to implicitly refer to the core snap.
		if slotSnapName == "" {
			slotSnapName = coreSnapName
		}
		// Ensure that such slot exists
		slot := r.slots[slotSnapName][slotName]
		if slot == nil {
			return nil, fmt.Errorf("snap %q has no slot named %q", slotSnapName, slotName)
		}
		// Ensure that slot and plug are connected
		if r.slotPlugs[slot][plug] == nil {
			return nil, fmt.Errorf("cannot disconnect %s:%s from %s:%s, it is not connected",
				plugSnapName, plugName, slotSnapName, slotName)
		}
		return []*ConnRef{NewConnRef(plug, slot)}, nil
	// 2: <snap>:<plug or slot> (through 1st pair)
	// Return a list of connections involving specified plug or slot.
	case plugName != "" && slotName == "" && slotSnapName == "":
		// The snap name can be omitted to implicitly refer to the core snap.
		if plugSnapName == "" {
			plugSnapName = coreSnapName
		}
		return r.connected(plugSnapName, plugName)
	// 2: <snap>:<plug or slot> (through 2nd pair)
	// Return a list of connections involving specified plug or slot.
	case plugSnapName == "" && plugName == "" && slotName != "":
		// The snap name can be omitted to implicitly refer to the core snap.
		if slotSnapName == "" {
			slotSnapName = coreSnapName
		}
		return r.connected(slotSnapName, slotName)
	default:
		return nil, fmt.Errorf("allowed forms are <snap>:<plug> <snap>:<slot> or <snap>:<plug or slot>")
	}
}

// slotValidator can be implemented by Interfaces that need to validate the slot before the security is lifted.
type slotValidator interface {
	BeforeConnectSlot(slot *ConnectedSlot) error
}

// plugValidator can be implemented by Interfaces that need to validate the plug before the security is lifted.
type plugValidator interface {
	BeforeConnectPlug(plug *ConnectedPlug) error
}

type PolicyFunc func(*ConnectedPlug, *ConnectedSlot) (bool, error)

// Connect establishes a connection between a plug and a slot.
// The plug and the slot must have the same interface.
// When connections are reloaded policyCheck is null (we don't check policy again).
func (r *Repository) Connect(ref *ConnRef, plugDynamicAttrs, slotDynamicAttrs map[string]interface{}, policyCheck PolicyFunc) (*Connection, error) {
	r.m.Lock()
	defer r.m.Unlock()

	plugSnapName := ref.PlugRef.Snap
	plugName := ref.PlugRef.Name
	slotSnapName := ref.SlotRef.Snap
	slotName := ref.SlotRef.Name

	// Ensure that such plug exists
	plug := r.plugs[plugSnapName][plugName]
	if plug == nil {
		return nil, &UnknownPlugSlotError{Msg: fmt.Sprintf("cannot connect plug %q from snap %q: no such plug", plugName, plugSnapName)}
	}
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return nil, &UnknownPlugSlotError{fmt.Sprintf("cannot connect slot %q from snap %q: no such slot", slotName, slotSnapName)}
	}
	// Ensure that plug and slot are compatible
	if slot.Interface != plug.Interface {
		return nil, fmt.Errorf(`cannot connect plug "%s:%s" (interface %q) to "%s:%s" (interface %q)`,
			plugSnapName, plugName, plug.Interface, slotSnapName, slotName, slot.Interface)
	}

	iface, ok := r.ifaces[plug.Interface]
	if !ok {
		return nil, fmt.Errorf("internal error: unknown interface %q", plug.Interface)
	}

	cplug := NewConnectedPlug(plug, plugDynamicAttrs)
	cslot := NewConnectedSlot(slot, slotDynamicAttrs)

	// policyCheck is null when reloading connections
	if policyCheck != nil {
		if i, ok := iface.(plugValidator); ok {
			if err := i.BeforeConnectPlug(cplug); err != nil {
				return nil, fmt.Errorf("cannot connect plug %q of snap %q: %s", plug.Name, plug.Snap.InstanceName(), err)
			}
		}
		if i, ok := iface.(slotValidator); ok {
			if err := i.BeforeConnectSlot(cslot); err != nil {
				return nil, fmt.Errorf("cannot connect slot %q of snap %q: %s", slot.Name, slot.Snap.InstanceName(), err)
			}
		}

		// autoconnect policy checker returns false to indicate disallowed auto-connection, but it's not an error.
		ok, err := policyCheck(cplug, cslot)
		if err != nil || !ok {
			return nil, err
		}
	}

	// Connect the plug
	if r.slotPlugs[slot] == nil {
		r.slotPlugs[slot] = make(map[*snap.PlugInfo]*Connection)
	}
	if r.plugSlots[plug] == nil {
		r.plugSlots[plug] = make(map[*snap.SlotInfo]*Connection)
	}

	conn := &Connection{Plug: cplug, Slot: cslot}
	r.slotPlugs[slot][plug] = conn
	r.plugSlots[plug][slot] = conn
	return conn, nil
}

// Disconnect disconnects the named plug from the slot of the given snap.
//
// Disconnect() finds a specific slot and a specific plug and disconnects that
// plug from that slot. It is an error if plug or slot cannot be found or if
// the connect does not exist.
func (r *Repository) Disconnect(plugSnapName, plugName, slotSnapName, slotName string) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Sanity check
	if plugSnapName == "" {
		return fmt.Errorf("cannot disconnect, plug snap name is empty")
	}
	if plugName == "" {
		return fmt.Errorf("cannot disconnect, plug name is empty")
	}
	if slotSnapName == "" {
		return fmt.Errorf("cannot disconnect, slot snap name is empty")
	}
	if slotName == "" {
		return fmt.Errorf("cannot disconnect, slot name is empty")
	}

	// Ensure that such plug exists
	plug := r.plugs[plugSnapName][plugName]
	if plug == nil {
		return fmt.Errorf("snap %q has no plug named %q", plugSnapName, plugName)
	}
	// Ensure that such slot exists
	slot := r.slots[slotSnapName][slotName]
	if slot == nil {
		return fmt.Errorf("snap %q has no slot named %q", slotSnapName, slotName)
	}
	// Ensure that slot and plug are connected
	if r.slotPlugs[slot][plug] == nil {
		return fmt.Errorf("cannot disconnect %s:%s from %s:%s, it is not connected",
			plugSnapName, plugName, slotSnapName, slotName)
	}
	r.disconnect(plug, slot)
	return nil
}

// Connected returns references for all connections that are currently
// established with the provided plug or slot.
func (r *Repository) Connected(snapName, plugOrSlotName string) ([]*ConnRef, error) {
	r.m.Lock()
	defer r.m.Unlock()

	return r.connected(snapName, plugOrSlotName)
}

func (r *Repository) connected(snapName, plugOrSlotName string) ([]*ConnRef, error) {
	if snapName == "" {
		snapName, _ = r.guessCoreSnapName()
		if snapName == "" {
			return nil, fmt.Errorf("internal error: cannot obtain core snap name while computing connections")
		}
	}
	var conns []*ConnRef
	if plugOrSlotName == "" {
		return nil, fmt.Errorf("plug or slot name is empty")
	}
	// Check if plugOrSlotName actually maps to anything
	if r.plugs[snapName][plugOrSlotName] == nil && r.slots[snapName][plugOrSlotName] == nil {
		return nil, fmt.Errorf("snap %q has no plug or slot named %q", snapName, plugOrSlotName)
	}
	// Collect all the relevant connections

	if plug, ok := r.plugs[snapName][plugOrSlotName]; ok {
		for slotInfo := range r.plugSlots[plug] {
			connRef := NewConnRef(plug, slotInfo)
			conns = append(conns, connRef)
		}
	}

	if slot, ok := r.slots[snapName][plugOrSlotName]; ok {
		for plugInfo := range r.slotPlugs[slot] {
			connRef := NewConnRef(plugInfo, slot)
			conns = append(conns, connRef)
		}
	}

	return conns, nil
}

func (r *Repository) Connections(snapName string) ([]*ConnRef, error) {
	r.m.Lock()
	defer r.m.Unlock()

	if snapName == "" {
		snapName, _ = r.guessCoreSnapName()
		if snapName == "" {
			return nil, fmt.Errorf("internal error: cannot obtain core snap name while computing connections")
		}
	}

	var conns []*ConnRef
	for _, plugInfo := range r.plugs[snapName] {
		for slotInfo := range r.plugSlots[plugInfo] {
			connRef := NewConnRef(plugInfo, slotInfo)
			conns = append(conns, connRef)
		}
	}
	for _, slotInfo := range r.slots[snapName] {
		for plugInfo := range r.slotPlugs[slotInfo] {
			connRef := NewConnRef(plugInfo, slotInfo)
			conns = append(conns, connRef)
		}
	}

	return conns, nil
}

// coreSnapName returns the name of the core snap if one exists
func (r *Repository) guessCoreSnapName() (string, error) {
	switch {
	case r.slots["snapd"] != nil:
		return "snapd", nil
	case r.slots["core"] != nil:
		return "core", nil
	case r.slots["ubuntu-core"] != nil:
		return "ubuntu-core", nil
	default:
		return "", fmt.Errorf("cannot guess the name of the core snap")
	}
}

// DisconnectAll disconnects all provided connection references.
func (r *Repository) DisconnectAll(conns []*ConnRef) {
	r.m.Lock()
	defer r.m.Unlock()

	for _, conn := range conns {
		plug := r.plugs[conn.PlugRef.Snap][conn.PlugRef.Name]
		slot := r.slots[conn.SlotRef.Snap][conn.SlotRef.Name]
		if plug != nil && slot != nil {
			r.disconnect(plug, slot)
		}
	}
}

// disconnect disconnects a plug from a slot.
func (r *Repository) disconnect(plug *snap.PlugInfo, slot *snap.SlotInfo) {
	delete(r.slotPlugs[slot], plug)
	if len(r.slotPlugs[slot]) == 0 {
		delete(r.slotPlugs, slot)
	}
	delete(r.plugSlots[plug], slot)
	if len(r.plugSlots[plug]) == 0 {
		delete(r.plugSlots, plug)
	}
}

// Backends returns all the security backends.
func (r *Repository) Backends() []SecurityBackend {
	r.m.Lock()
	defer r.m.Unlock()

	result := make([]SecurityBackend, 0, len(r.backends))
	for _, backend := range r.backends {
		result = append(result, backend)
	}
	sort.Sort(byBackendName(result))
	return result
}

// Interfaces returns object holding a lists of all the plugs and slots and their connections.
func (r *Repository) Interfaces() *Interfaces {
	r.m.Lock()
	defer r.m.Unlock()

	ifaces := &Interfaces{}

	// Copy and flatten plugs and slots
	for _, plugs := range r.plugs {
		for _, plugInfo := range plugs {
			ifaces.Plugs = append(ifaces.Plugs, plugInfo)
		}
	}
	for _, slots := range r.slots {
		for _, slotInfo := range slots {
			ifaces.Slots = append(ifaces.Slots, slotInfo)
		}
	}

	for plug, slots := range r.plugSlots {
		for slot := range slots {
			ifaces.Connections = append(ifaces.Connections, NewConnRef(plug, slot))
		}
	}

	sort.Sort(byPlugSnapAndName(ifaces.Plugs))
	sort.Sort(bySlotSnapAndName(ifaces.Slots))
	sort.Sort(byConnRef(ifaces.Connections))
	return ifaces
}

// SnapSpecification returns the specification of a given snap in a given security system.
func (r *Repository) SnapSpecification(securitySystem SecuritySystem, snapName string) (Specification, error) {
	r.m.Lock()
	defer r.m.Unlock()

	backend := r.backends[securitySystem]
	if backend == nil {
		return nil, fmt.Errorf("cannot handle interfaces of snap %q, security system %q is not known", snapName, securitySystem)
	}

	spec := backend.NewSpecification()

	// slot side
	for _, slotInfo := range r.slots[snapName] {
		iface := r.ifaces[slotInfo.Interface]
		if err := spec.AddPermanentSlot(iface, slotInfo); err != nil {
			return nil, err
		}
		for _, conn := range r.slotPlugs[slotInfo] {
			if err := spec.AddConnectedSlot(iface, conn.Plug, conn.Slot); err != nil {
				return nil, err
			}
		}
	}
	// plug side
	for _, plugInfo := range r.plugs[snapName] {
		iface := r.ifaces[plugInfo.Interface]
		if err := spec.AddPermanentPlug(iface, plugInfo); err != nil {
			return nil, err
		}
		for _, conn := range r.plugSlots[plugInfo] {
			if err := spec.AddConnectedPlug(iface, conn.Plug, conn.Slot); err != nil {
				return nil, err
			}
		}
	}
	return spec, nil
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
	if snapInfo.Broken != "" {
		return fmt.Errorf("snap is broken: %s", snapInfo.Broken)
	}
	err := snap.Validate(snapInfo)
	if err != nil {
		return err
	}

	r.m.Lock()
	defer r.m.Unlock()

	snapName := snapInfo.InstanceName()

	if r.plugs[snapName] != nil || r.slots[snapName] != nil {
		return fmt.Errorf("cannot register interfaces for snap %q more than once", snapName)
	}

	for plugName, plugInfo := range snapInfo.Plugs {
		if _, ok := r.ifaces[plugInfo.Interface]; !ok {
			continue
		}
		if r.plugs[snapName] == nil {
			r.plugs[snapName] = make(map[string]*snap.PlugInfo)
		}
		r.plugs[snapName][plugName] = plugInfo
	}

	for slotName, slotInfo := range snapInfo.Slots {
		if _, ok := r.ifaces[slotInfo.Interface]; !ok {
			continue
		}
		if r.slots[snapName] == nil {
			r.slots[snapName] = make(map[string]*snap.SlotInfo)
		}
		r.slots[snapName][slotName] = slotInfo
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
		if len(r.plugSlots[plug]) > 0 {
			return fmt.Errorf("cannot remove connected plug %s.%s", snapName, plugName)
		}
	}
	for slotName, slot := range r.slots[snapName] {
		if len(r.slotPlugs[slot]) > 0 {
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
// The return value is a list of names that were affected.
func (r *Repository) DisconnectSnap(snapName string) ([]string, error) {
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

	result := make([]string, 0, len(seen))
	for info := range seen {
		result = append(result, info.InstanceName())
	}
	sort.Strings(result)
	return result, nil
}

// AutoConnectCandidateSlots finds and returns viable auto-connection candidates
// for a given plug.
func (r *Repository) AutoConnectCandidateSlots(plugSnapName, plugName string, policyCheck func(*ConnectedPlug, *ConnectedSlot) (bool, error)) []*snap.SlotInfo {
	r.m.Lock()
	defer r.m.Unlock()

	plugInfo := r.plugs[plugSnapName][plugName]
	if plugInfo == nil {
		return nil
	}

	var candidates []*snap.SlotInfo
	for _, slotsForSnap := range r.slots {
		for _, slotInfo := range slotsForSnap {
			if slotInfo.Interface != plugInfo.Interface {
				continue
			}
			iface := slotInfo.Interface

			// declaration based checks disallow
			ok, err := policyCheck(NewConnectedPlug(plugInfo, nil), NewConnectedSlot(slotInfo, nil))
			if !ok || err != nil {
				continue
			}

			if r.ifaces[iface].AutoConnect(plugInfo, slotInfo) {
				candidates = append(candidates, slotInfo)
			}
		}
	}
	return candidates
}

// AutoConnectCandidatePlugs finds and returns viable auto-connection candidates
// for a given slot.
func (r *Repository) AutoConnectCandidatePlugs(slotSnapName, slotName string, policyCheck func(*ConnectedPlug, *ConnectedSlot) (bool, error)) []*snap.PlugInfo {
	r.m.Lock()
	defer r.m.Unlock()

	slotInfo := r.slots[slotSnapName][slotName]
	if slotInfo == nil {
		return nil
	}

	var candidates []*snap.PlugInfo
	for _, plugsForSnap := range r.plugs {
		for _, plugInfo := range plugsForSnap {
			if slotInfo.Interface != plugInfo.Interface {
				continue
			}
			iface := slotInfo.Interface

			// declaration based checks disallow
			ok, err := policyCheck(NewConnectedPlug(plugInfo, nil), NewConnectedSlot(slotInfo, nil))
			if !ok || err != nil {
				continue
			}

			if r.ifaces[iface].AutoConnect(plugInfo, slotInfo) {
				candidates = append(candidates, plugInfo)
			}
		}
	}
	return candidates
}
