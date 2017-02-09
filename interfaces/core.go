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

// Plug represents the potential of a given snap to connect to a slot.
type Plug struct {
	*snap.PlugInfo
	Connections []SlotRef `json:"connections,omitempty"`
}

// Ref returns reference to a plug
func (plug *Plug) Ref() PlugRef {
	return PlugRef{Snap: plug.Snap.Name(), Name: plug.Name}
}

// PlugRef is a reference to a plug.
type PlugRef struct {
	Snap string `json:"snap"`
	Name string `json:"plug"`
}

// String returns the "snap:plug" representation of a plug reference.
func (ref *PlugRef) String() string {
	return fmt.Sprintf("%s:%s", ref.Snap, ref.Name)
}

// Slot represents a capacity offered by a snap.
type Slot struct {
	*snap.SlotInfo
	Connections []PlugRef `json:"connections,omitempty"`
}

// Ref returns reference to a slot
func (slot *Slot) Ref() SlotRef {
	return SlotRef{Snap: slot.Snap.Name(), Name: slot.Name}
}

// SlotRef is a reference to a slot.
type SlotRef struct {
	Snap string `json:"snap"`
	Name string `json:"slot"`
}

// String returns the "snap:slot" representation of a slot reference.
func (ref *SlotRef) String() string {
	return fmt.Sprintf("%s:%s", ref.Snap, ref.Name)
}

// Interfaces holds information about a list of plugs and slots, and their connections.
type Interfaces struct {
	Plugs []*Plug `json:"plugs"`
	Slots []*Slot `json:"slots"`
}

// ConnRef holds information about plug and slot reference that form a particular connection.
type ConnRef struct {
	PlugRef PlugRef
	SlotRef SlotRef
}

// ID returns a string identifying a given connection.
func (conn *ConnRef) ID() string {
	return fmt.Sprintf("%s:%s %s:%s", conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name)
}

// ParseConnRef parses an ID string
func ParseConnRef(id string) (ConnRef, error) {
	var conn ConnRef
	parts := strings.SplitN(id, " ", 2)
	if len(parts) != 2 {
		return conn, fmt.Errorf("malformed connection identifier: %q", id)
	}
	plugParts := strings.Split(parts[0], ":")
	slotParts := strings.Split(parts[1], ":")
	if len(plugParts) != 2 || len(slotParts) != 2 {
		return conn, fmt.Errorf("malformed connection identifier: %q", id)
	}
	conn.PlugRef.Snap = plugParts[0]
	conn.PlugRef.Name = plugParts[1]
	conn.SlotRef.Snap = slotParts[0]
	conn.SlotRef.Name = slotParts[1]
	return conn, nil
}

// Interface describes a group of interchangeable capabilities with common features.
// Interfaces act as a contract between system builders, application developers
// and end users.
type Interface interface {
	// Unique and public name of this interface.
	Name() string

	// SanitizePlug checks if a plug is correct, altering if necessary.
	SanitizePlug(plug *Plug) error

	// SanitizeSlot checks if a slot is correct, altering if necessary.
	SanitizeSlot(slot *Slot) error

	// ValidatePlug checks if the attributes of the plug are correct, altering if necessary.
	ValidatePlug(plug *Plug, attrs map[string]interface{}) error

	// ValidateSlot checks if the attributes of the slot are correct, altering if necessary.
	ValidateSlot(slot *Slot, attrs map[string]interface{}) error

	// PermanentPlugSnippet returns the snippet of text for the given security
	// system that is used during the whole lifetime of affected applications,
	// whether the plug is connected or not.
	//
	// Permanent security snippet can be used to grant permissions to a snap that
	// has a plug of a given interface even before the plug is connected to a
	// slot.
	//
	// An empty snippet is returned when there are no additional permissions
	// that are required to implement this interface or when the interface
	// doesn't recognize the security system.
	PermanentPlugSnippet(plug *Plug, securitySystem SecuritySystem) ([]byte, error)

	// ConnectedPlugSnippet returns the snippet of text for the given security
	// system that is used by affected application, while a specific connection
	// between a plug and a slot exists.
	//
	// Connection-specific security snippet can be used to grant permission to
	// a snap that has a plug of a given interface connected to a slot in
	// another snap.
	//
	// The snippet should be specific to both the plug and the slot. If the
	// slot is not necessary then consider using PermanentPlugSnippet()
	// instead.
	//
	// An empty snippet is returned when there are no additional permissions
	// that are required to implement this interface or when the interface
	// doesn't recognize the security system.
	ConnectedPlugSnippet(plug *Plug, plugAttrs map[string]interface{}, slot *Slot, slotAttrs map[string]interface{}, securitySystem SecuritySystem) ([]byte, error)

	// PermanentSlotSnippet returns the snippet of text for the given security
	// system that is used during the whole lifetime of affected applications,
	// whether the slot is connected or not.
	//
	// Permanent security snippet can be used to grant permissions to a snap that
	// has a slot of a given interface even before the first connection to that
	// slot is made.
	//
	// An empty snippet is returned when there are no additional permissions
	// that are required to implement this interface or when the interface
	// doesn't recognize the security system.
	PermanentSlotSnippet(slot *Slot, securitySystem SecuritySystem) ([]byte, error)

	// ConnectedSlotSnippet returns the snippet of text for the given security
	// system that is used by affected application, while a specific connection
	// between a plug and a slot exists.
	//
	// Connection-specific security snippet can be used to grant permission to
	// a snap that has a slot of a given interface connected to a plug in
	// another snap.
	//
	// The snippet should be specific to both the plug and the slot, if the
	// plug is not necessary then consider using PermanentSlotSnippet()
	// instead.
	//
	// An empty snippet is returned when there are no additional permissions
	// that are required to implement this interface or when the interface
	// doesn't recognize the security system.
	ConnectedSlotSnippet(plug *Plug, plugAttrs map[string]interface{}, slot *Slot, slotAttrs map[string]interface{}, securitySystem SecuritySystem) ([]byte, error)

	// AutoConnect returns whether plug and slot should be
	// implicitly auto-connected assuming they will be an
	// unambiguous connection candidate and declaration-based checks
	// allow.
	AutoConnect(plug *Plug, slot *Slot) bool
}

// Specification describes interactions between backends and interfaces.
type Specification interface {
	// AddPermanentSlot records side-effects of having a slot.
	AddPermanentSlot(iface Interface, slot *Slot) error
	// AddPermanentPlug records side-effects of having a plug.
	AddPermanentPlug(iface Interface, plug *Plug) error
	// AddConnectedSlot records side-effects of having a connected slot.
	AddConnectedSlot(iface Interface, plug *Plug, slot *Slot) error
	// AddConnectedPlug records side-effects of having a connected plug.
	AddConnectedPlug(iface Interface, plug *Plug, slot *Slot) error
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
	// SecurityKMod identifies the kernel modules security system
	SecurityKMod SecuritySystem = "kmod"
	// SecuritySystemd identifies the systemd services security system
	SecuritySystemd SecuritySystem = "systemd"
)

// Regular expression describing correct identifiers.
var validName = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidateName checks if a string can be used as a plug or slot name.
func ValidateName(name string) error {
	valid := validName.MatchString(name)
	if !valid {
		return fmt.Errorf("invalid interface name: %q", name)
	}
	return nil
}

// ValidateDBusBusName checks if a string conforms to
// https://dbus.freedesktop.org/doc/dbus-specification.html#message-protocol-names
func ValidateDBusBusName(busName string) error {
	if len(busName) == 0 {
		return fmt.Errorf("DBus bus name must be set")
	} else if len(busName) > 255 {
		return fmt.Errorf("DBus bus name is too long (must be <= 255)")
	}

	validBusName := regexp.MustCompile("^[a-zA-Z_-][a-zA-Z0-9_-]*(\\.[a-zA-Z_-][a-zA-Z0-9_-]*)+$")
	if !validBusName.MatchString(busName) {
		return fmt.Errorf("invalid DBus bus name: %q", busName)
	}
	return nil
}
