// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

// Package policy implements the declaration based policy checks for
// connecting or permitting installation of snaps based on their slots
// and plugs.
package policy

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// InstallCandidate represents a candidate snap for installation.
type InstallCandidate struct {
	Snap            *snap.Info
	SnapDeclaration *asserts.SnapDeclaration
	BaseDeclaration *asserts.BaseDeclaration
}

func (ic *InstallCandidate) checkSlotRule(slot *snap.SlotInfo, rule *asserts.SlotRule, snapRule bool) error {
	context := ""
	if snapRule {
		context = fmt.Sprintf(" for %q snap", ic.SnapDeclaration.SnapName())
	}
	if checkSlotInstallationConstraints(slot, rule.DenyInstallation) == nil {
		return fmt.Errorf("installation denied by %q slot rule of interface %q%s", slot.Name, slot.Interface, context)
	}
	if checkSlotInstallationConstraints(slot, rule.AllowInstallation) != nil {
		return fmt.Errorf("installation not allowed by %q slot rule of interface %q%s", slot.Name, slot.Interface, context)
	}
	return nil
}

func (ic *InstallCandidate) checkPlugRule(plug *snap.PlugInfo, rule *asserts.PlugRule, snapRule bool) error {
	context := ""
	if snapRule {
		context = fmt.Sprintf(" for %q snap", ic.SnapDeclaration.SnapName())
	}
	if checkPlugInstallationConstraints(plug, rule.DenyInstallation) == nil {
		return fmt.Errorf("installation denied by %q plug rule of interface %q%s", plug.Name, plug.Interface, context)
	}
	if checkPlugInstallationConstraints(plug, rule.AllowInstallation) != nil {
		return fmt.Errorf("installation not allowed by %q plug rule of interface %q%s", plug.Name, plug.Interface, context)
	}
	return nil
}

func (ic *InstallCandidate) checkSlot(slot *snap.SlotInfo) error {
	iface := slot.Interface
	if snapDecl := ic.SnapDeclaration; snapDecl != nil {
		if rule := snapDecl.SlotRule(iface); rule != nil {
			return ic.checkSlotRule(slot, rule, true)
		}
	}
	if rule := ic.BaseDeclaration.SlotRule(iface); rule != nil {
		return ic.checkSlotRule(slot, rule, false)
	}
	return nil
}

func (ic *InstallCandidate) checkPlug(plug *snap.PlugInfo) error {
	iface := plug.Interface
	if snapDecl := ic.SnapDeclaration; snapDecl != nil {
		if rule := snapDecl.PlugRule(iface); rule != nil {
			return ic.checkPlugRule(plug, rule, true)
		}
	}
	if rule := ic.BaseDeclaration.PlugRule(iface); rule != nil {
		return ic.checkPlugRule(plug, rule, false)
	}
	return nil
}

// Check checks whether the installation is allowed.
func (ic *InstallCandidate) Check() error {
	if ic.BaseDeclaration == nil {
		return fmt.Errorf("internal error: improperly initialized InstallCandidate")
	}

	for _, slot := range ic.Snap.Slots {
		err := ic.checkSlot(slot)
		if err != nil {
			return err
		}
	}

	for _, plug := range ic.Snap.Plugs {
		err := ic.checkPlug(plug)
		if err != nil {
			return err
		}
	}

	return nil
}

// ConnectCandidate represents a candidate connection.
type ConnectCandidate struct {
	Plug                *interfaces.ConnectedPlug
	PlugSnapDeclaration *asserts.SnapDeclaration

	Slot                *interfaces.ConnectedSlot
	SlotSnapDeclaration *asserts.SnapDeclaration

	BaseDeclaration *asserts.BaseDeclaration
}

func nestedGet(which string, attrs interfaces.Attrer, path string) (interface{}, error) {
	val, ok := attrs.Lookup(path)
	if !ok {
		return nil, fmt.Errorf("%s attribute %q not found", which, path)
	}
	return val, nil
}

func (connc *ConnectCandidate) PlugAttr(arg string) (interface{}, error) {
	return nestedGet("plug", connc.Plug, arg)
}

func (connc *ConnectCandidate) SlotAttr(arg string) (interface{}, error) {
	return nestedGet("slot", connc.Slot, arg)
}

func (connc *ConnectCandidate) plugSnapType() snap.Type {
	return connc.Plug.Snap().Type
}

func (connc *ConnectCandidate) slotSnapType() snap.Type {
	return connc.Slot.Snap().Type
}

func (connc *ConnectCandidate) plugSnapID() string {
	if connc.PlugSnapDeclaration != nil {
		return connc.PlugSnapDeclaration.SnapID()
	}
	return "" // never a valid snap-id
}

func (connc *ConnectCandidate) slotSnapID() string {
	if connc.SlotSnapDeclaration != nil {
		return connc.SlotSnapDeclaration.SnapID()
	}
	return "" // never a valid snap-id
}

func (connc *ConnectCandidate) plugPublisherID() string {
	if connc.PlugSnapDeclaration != nil {
		return connc.PlugSnapDeclaration.PublisherID()
	}
	return "" // never a valid publisher-id
}

func (connc *ConnectCandidate) slotPublisherID() string {
	if connc.SlotSnapDeclaration != nil {
		return connc.SlotSnapDeclaration.PublisherID()
	}
	return "" // never a valid publisher-id
}

func (connc *ConnectCandidate) checkPlugRule(kind string, rule *asserts.PlugRule, snapRule bool) error {
	context := ""
	if snapRule {
		context = fmt.Sprintf(" for %q snap", connc.PlugSnapDeclaration.SnapName())
	}
	denyConst := rule.DenyConnection
	allowConst := rule.AllowConnection
	if kind == "auto-connection" {
		denyConst = rule.DenyAutoConnection
		allowConst = rule.AllowAutoConnection
	}
	if checkPlugConnectionConstraints(connc, denyConst) == nil {
		return fmt.Errorf("%s denied by plug rule of interface %q%s", kind, connc.Plug.Interface(), context)
	}
	if checkPlugConnectionConstraints(connc, allowConst) != nil {
		return fmt.Errorf("%s not allowed by plug rule of interface %q%s", kind, connc.Plug.Interface(), context)
	}
	return nil
}

func (connc *ConnectCandidate) checkSlotRule(kind string, rule *asserts.SlotRule, snapRule bool) error {
	context := ""
	if snapRule {
		context = fmt.Sprintf(" for %q snap", connc.SlotSnapDeclaration.SnapName())
	}
	denyConst := rule.DenyConnection
	allowConst := rule.AllowConnection
	if kind == "auto-connection" {
		denyConst = rule.DenyAutoConnection
		allowConst = rule.AllowAutoConnection
	}
	if checkSlotConnectionConstraints(connc, denyConst) == nil {
		return fmt.Errorf("%s denied by slot rule of interface %q%s", kind, connc.Plug.Interface(), context)
	}
	if checkSlotConnectionConstraints(connc, allowConst) != nil {
		return fmt.Errorf("%s not allowed by slot rule of interface %q%s", kind, connc.Plug.Interface(), context)
	}
	return nil
}

func (connc *ConnectCandidate) check(kind string) error {
	baseDecl := connc.BaseDeclaration
	if baseDecl == nil {
		return fmt.Errorf("internal error: improperly initialized ConnectCandidate")
	}

	iface := connc.Plug.Interface()

	if connc.Slot.Interface() != iface {
		return fmt.Errorf("cannot connect mismatched plug interface %q to slot interface %q", iface, connc.Slot.Interface())
	}

	if plugDecl := connc.PlugSnapDeclaration; plugDecl != nil {
		if rule := plugDecl.PlugRule(iface); rule != nil {
			return connc.checkPlugRule(kind, rule, true)
		}
	}
	if slotDecl := connc.SlotSnapDeclaration; slotDecl != nil {
		if rule := slotDecl.SlotRule(iface); rule != nil {
			return connc.checkSlotRule(kind, rule, true)
		}
	}
	if rule := baseDecl.PlugRule(iface); rule != nil {
		return connc.checkPlugRule(kind, rule, false)
	}
	if rule := baseDecl.SlotRule(iface); rule != nil {
		return connc.checkSlotRule(kind, rule, false)
	}
	return nil
}

// Check checks whether the connection is allowed.
func (connc *ConnectCandidate) Check() error {
	return connc.check("connection")
}

// CheckAutoConnect checks whether the connection is allowed to auto-connect.
func (connc *ConnectCandidate) CheckAutoConnect() error {
	return connc.check("auto-connection")
}
