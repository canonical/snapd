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
	"regexp"
	"strings"

	"github.com/snapcore/snapd/snap"
)

// BeforePreparePlug sanitizes a plug with a given snapd interface.
func BeforePreparePlug(iface Interface, plugInfo *snap.PlugInfo) error {
	if iface.Name() != plugInfo.Interface {
		return fmt.Errorf("cannot sanitize plug %q (interface %q) using interface %q",
			PlugRef{Snap: plugInfo.Snap.InstanceName(), Name: plugInfo.Name}, plugInfo.Interface, iface.Name())
	}
	var err error
	if iface, ok := iface.(PlugSanitizer); ok {
		err = iface.BeforePreparePlug(plugInfo)
	}
	return err
}

func BeforeConnectPlug(iface Interface, plug *ConnectedPlug) error {
	if iface.Name() != plug.plugInfo.Interface {
		return fmt.Errorf("cannot sanitize connection for plug %q (interface %q) using interface %q",
			PlugRef{Snap: plug.plugInfo.Snap.InstanceName(), Name: plug.plugInfo.Name}, plug.plugInfo.Interface, iface.Name())
	}
	var err error
	if iface, ok := iface.(ConnPlugSanitizer); ok {
		err = iface.BeforeConnectPlug(plug)
	}
	return err
}

// ByName returns an Interface for the given interface name. Note that in order for
// this to work properly, the package "interfaces/builtin" must also eventually be
// imported to populate the full list of interfaces.
var ByName = func(name string) (iface Interface, err error) {
	panic("ByName is unset, import interfaces/builtin to initialize this")
}

// PlugRef is a reference to a plug.
type PlugRef struct {
	Snap string `json:"snap"`
	Name string `json:"plug"`
}

// String returns the "snap:plug" representation of a plug reference.
func (ref PlugRef) String() string {
	return fmt.Sprintf("%s:%s", ref.Snap, ref.Name)
}

// SortsBefore returns true when plug should be sorted before the other
func (ref PlugRef) SortsBefore(other PlugRef) bool {
	if ref.Snap != other.Snap {
		return ref.Snap < other.Snap
	}
	return ref.Name < other.Name
}

// Sanitize slot with a given snapd interface.
func BeforePrepareSlot(iface Interface, slotInfo *snap.SlotInfo) error {
	if iface.Name() != slotInfo.Interface {
		return fmt.Errorf("cannot sanitize slot %q (interface %q) using interface %q",
			SlotRef{Snap: slotInfo.Snap.InstanceName(), Name: slotInfo.Name}, slotInfo.Interface, iface.Name())
	}
	var err error
	if iface, ok := iface.(SlotSanitizer); ok {
		err = iface.BeforePrepareSlot(slotInfo)
	}
	return err
}

// SlotRef is a reference to a slot.
type SlotRef struct {
	Snap string `json:"snap"`
	Name string `json:"slot"`
}

// String returns the "snap:slot" representation of a slot reference.
func (ref SlotRef) String() string {
	return fmt.Sprintf("%s:%s", ref.Snap, ref.Name)
}

// SortsBefore returns true when slot should be sorted before the other
func (ref SlotRef) SortsBefore(other SlotRef) bool {
	if ref.Snap != other.Snap {
		return ref.Snap < other.Snap
	}
	return ref.Name < other.Name
}

// Interfaces holds information about a list of plugs, slots and their connections.
type Interfaces struct {
	Plugs       []*snap.PlugInfo
	Slots       []*snap.SlotInfo
	Connections []*ConnRef
}

// Info holds information about a given interface and its instances.
type Info struct {
	Name    string
	Summary string
	DocURL  string
	Plugs   []*snap.PlugInfo
	Slots   []*snap.SlotInfo
}

// ConnRef holds information about plug and slot reference that form a particular connection.
type ConnRef struct {
	PlugRef PlugRef
	SlotRef SlotRef
}

// NewConnRef creates a connection reference for given plug and slot
func NewConnRef(plug *snap.PlugInfo, slot *snap.SlotInfo) *ConnRef {
	return &ConnRef{
		PlugRef: PlugRef{Snap: plug.Snap.InstanceName(), Name: plug.Name},
		SlotRef: SlotRef{Snap: slot.Snap.InstanceName(), Name: slot.Name},
	}
}

// ID returns a string identifying a given connection.
func (conn *ConnRef) ID() string {
	return fmt.Sprintf("%s:%s %s:%s", conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name)
}

// SortsBefore returns true when connection should be sorted before the other
func (conn *ConnRef) SortsBefore(other *ConnRef) bool {
	if conn.PlugRef != other.PlugRef {
		return conn.PlugRef.SortsBefore(other.PlugRef)
	}
	return conn.SlotRef.SortsBefore(other.SlotRef)
}

// ParseConnRef parses an ID string
func ParseConnRef(id string) (*ConnRef, error) {
	var conn ConnRef
	parts := strings.SplitN(id, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed connection identifier: %q", id)
	}
	plugParts := strings.Split(parts[0], ":")
	slotParts := strings.Split(parts[1], ":")
	if len(plugParts) != 2 || len(slotParts) != 2 {
		return nil, fmt.Errorf("malformed connection identifier: %q", id)
	}
	conn.PlugRef.Snap = plugParts[0]
	conn.PlugRef.Name = plugParts[1]
	conn.SlotRef.Snap = slotParts[0]
	conn.SlotRef.Name = slotParts[1]
	return &conn, nil
}

// Interface describes a group of interchangeable capabilities with common features.
// Interfaces act as a contract between system builders, application developers
// and end users.
type Interface interface {
	// Unique and public name of this interface.
	Name() string

	// AutoConnect returns whether plug and slot should be
	// implicitly auto-connected assuming there will be an
	// unambiguous connection candidate and declaration-based checks
	// allow.
	AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool
}

// ConnPlugSanitizer can be implemented by Interfaces that have reasons to sanitize
// their plugs specifically before a connection is performed.
type ConnPlugSanitizer interface {
	BeforeConnectPlug(plug *ConnectedPlug) error
}

// PlugSanitizer can be implemented by Interfaces that have reasons to sanitize their plugs.
type PlugSanitizer interface {
	BeforePreparePlug(plug *snap.PlugInfo) error
}

// SlotSanitizer can be implemented by Interfaces that have reasons to sanitize their slots.
type SlotSanitizer interface {
	BeforePrepareSlot(slot *snap.SlotInfo) error
}

// StaticInfo describes various static-info of a given interface.
//
// The Summary must be a one-line string of length suitable for listing views.
// The DocURL can point to website (e.g. a forum thread) that goes into more
// depth and documents the interface in detail.
type StaticInfo struct {
	Summary string
	DocURL  string

	// ImplicitOnCore controls if a slot is automatically added to core (non-classic) systems.
	ImplicitOnCore bool
	// ImplicitOnClassic controls if a slot is automatically added to classic systems.
	ImplicitOnClassic bool

	// AffectsPlugOnRefresh tells if refreshing of a snap with a slot of this interface
	// is disruptive for the snap on the plug side (when the interface is connected),
	// meaning that a refresh of the slot-side affects snap(s) on the plug side
	// due to e.g. namespace changes which require freezing and thawing of the
	// running processes. This flag is consulted when computing snaps affected
	// by refresh for auto-refresh gating with gate-auto-refresh hooks.
	// TODO: if we change the snap-update-ns logic to avoid the freezeing/thawing
	// if there are no changes, there are interfaces like appstream-metadata or
	// system-packages-doc that could get the flag set back to false.
	AffectsPlugOnRefresh bool

	// BaseDeclarationPlugs defines optional plug-side rules in the
	// base-declaration assertion relevant for this interface. See
	// interfaces/builtin/README.md, especially "Base declaration policy
	// patterns".
	BaseDeclarationPlugs string
	// BaseDeclarationSlots defines an optional slot-side rules in the
	// base-declaration assertion relevant for this interface. See
	// interfaces/builtin/README.md, especially "Base declaration policy
	// patterns".
	BaseDeclarationSlots string

	// AppArmorUnconfinedPlugs results in the snap that plugs this interface
	// being granted the AppArmor unconfined profile mode
	AppArmorUnconfinedPlugs bool
	// Similarly, AppArmorUnconfinedSlots results in the snap that slots this interface
	// being granted the AppArmor unconfined profile mode
	AppArmorUnconfinedSlots bool
}

// PermanentPlugServiceSnippets will return the set of snippets for the systemd
// service unit that should be generated for a snap with the specified plug.
// The list returned is not unique, callers must de-duplicate themselves.
// The plug is provided because the snippet may depend on plug attributes for
// example. The plug is sanitized before the snippets are returned.
func PermanentPlugServiceSnippets(iface Interface, plug *snap.PlugInfo) (snips []string, err error) {
	// sanitize the plug first
	err = BeforePreparePlug(iface, plug)
	if err != nil {
		return nil, err
	}

	type serviceSnippetPlugger interface {
		ServicePermanentPlug(plug *snap.PlugInfo) []string
	}
	if iface, ok := iface.(serviceSnippetPlugger); ok {
		snips = iface.ServicePermanentPlug(plug)
	}
	return snips, nil
}

// StaticInfoOf returns the static-info of the given interface.
func StaticInfoOf(iface Interface) (si StaticInfo) {
	type metaDataProvider interface {
		StaticInfo() StaticInfo
	}
	if iface, ok := iface.(metaDataProvider); ok {
		si = iface.StaticInfo()
	}
	return si
}

// Specification describes interactions between backends and interfaces.
type Specification interface {
	// AddPermanentSlot records side-effects of having a slot.
	AddPermanentSlot(iface Interface, slot *snap.SlotInfo) error
	// AddPermanentPlug records side-effects of having a plug.
	AddPermanentPlug(iface Interface, plug *snap.PlugInfo) error
	// AddConnectedSlot records side-effects of having a connected slot.
	AddConnectedSlot(iface Interface, plug *ConnectedPlug, slot *ConnectedSlot) error
	// AddConnectedPlug records side-effects of having a connected plug.
	AddConnectedPlug(iface Interface, plug *ConnectedPlug, slot *ConnectedSlot) error
}

// SecuritySystem is a name of a security system.
type SecuritySystem string

const (
	// SecurityAppArmor identifies the apparmor security system.
	SecurityAppArmor SecuritySystem = "apparmor"
	// SecuritySecComp identifies the seccomp security system.
	SecuritySecComp SecuritySystem = "seccomp"
	// SecurityDBus identifies the DBus security system.
	SecurityDBus SecuritySystem = "dbus"
	// SecurityUDev identifies the UDev security system.
	SecurityUDev SecuritySystem = "udev"
	// SecurityMount identifies the mount security system.
	SecurityMount SecuritySystem = "mount"
	// SecurityKMod identifies the kernel modules security system.
	SecurityKMod SecuritySystem = "kmod"
	// SecuritySystemd identifies the systemd services security system.
	SecuritySystemd SecuritySystem = "systemd"
	// SecurityPolkit identifies the polkit security system.
	SecurityPolkit SecuritySystem = "polkit"
)

var isValidBusName = regexp.MustCompile(`^[a-zA-Z_-][a-zA-Z0-9_-]*(\.[a-zA-Z_-][a-zA-Z0-9_-]*)+$`).MatchString

// ValidateDBusBusName checks if a string conforms to
// https://dbus.freedesktop.org/doc/dbus-specification.html#message-protocol-names
func ValidateDBusBusName(busName string) error {
	if len(busName) == 0 {
		return fmt.Errorf("DBus bus name must be set")
	} else if len(busName) > 255 {
		return fmt.Errorf("DBus bus name is too long (must be <= 255)")
	}

	if !isValidBusName(busName) {
		return fmt.Errorf("invalid DBus bus name: %q", busName)
	}
	return nil
}
