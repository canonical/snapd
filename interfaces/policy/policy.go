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

// Package policy implements the declaration based policy checks for
// connecting or permitting installation of snaps based on their slots
// and plugs.
package policy

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
)

// TODO: InstallCandidate

// ConnectCandidate represents a candidate connection.
type ConnectCandidate struct {
	// TODO: later we need to carry dynamic attributes once we have those
	Plug                *snap.PlugInfo
	PlugSnapDeclaration *asserts.SnapDeclaration

	Slot                *snap.SlotInfo
	SlotSnapDeclaration *asserts.SnapDeclaration

	BaseDeclaration *asserts.BaseDeclaration
}

func (connc *ConnectCandidate) plugAttrs() map[string]interface{} {
	return connc.Plug.Attrs
}

func (connc *ConnectCandidate) slotAttrs() map[string]interface{} {
	return connc.Slot.Attrs
}

func (connc *ConnectCandidate) plugSnapType() snap.Type {
	return connc.Plug.Snap.Type
}

func (connc *ConnectCandidate) slotSnapType() snap.Type {
	return connc.Slot.Snap.Type
}

func (connc *ConnectCandidate) plugSnapID() string {
	if connc.PlugSnapDeclaration != nil {
		return connc.PlugSnapDeclaration.SnapID()
	}
	return "" // unset
}

func (connc *ConnectCandidate) slotSnapID() string {
	if connc.SlotSnapDeclaration != nil {
		return connc.SlotSnapDeclaration.SnapID()
	}
	return "" // unset
}

func (connc *ConnectCandidate) plugPublisherID() string {
	if connc.PlugSnapDeclaration != nil {
		return connc.PlugSnapDeclaration.PublisherID()
	}
	return "" // unset
}

func (connc *ConnectCandidate) slotPublisherID() string {
	if connc.SlotSnapDeclaration != nil {
		return connc.SlotSnapDeclaration.PublisherID()
	}
	return "" // unset
}

func (connc *ConnectCandidate) checkPlugRule(rule *asserts.PlugRule, snapRule bool) error {
	context := ""
	if snapRule {
		context = fmt.Sprintf(" for %q snap", connc.PlugSnapDeclaration.SnapName())
	}
	if checkPlugConnectionConstraints(connc, rule.DenyConnection) == nil {
		return fmt.Errorf("connection denied by plug rule of interface %q%s", connc.Plug.Interface, context)
	}
	if checkPlugConnectionConstraints(connc, rule.AllowConnection) != nil {
		return fmt.Errorf("connection not allowed by plug rule of interface %q%s", connc.Plug.Interface, context)
	}
	return nil
}

func (connc *ConnectCandidate) checkSlotRule(rule *asserts.SlotRule, snapRule bool) error {
	context := ""
	if snapRule {
		context = fmt.Sprintf(" for %q snap", connc.SlotSnapDeclaration.SnapName())
	}
	if checkSlotConnectionConstraints(connc, rule.DenyConnection) == nil {
		return fmt.Errorf("connection denied by slot rule of interface %q%s", connc.Plug.Interface, context)
	}
	if checkSlotConnectionConstraints(connc, rule.AllowConnection) != nil {
		return fmt.Errorf("connection not allowed by slot rule of interface %q%s", connc.Plug.Interface, context)
	}
	return nil
}

// Check checks whether the connection is allowed.
func (connc *ConnectCandidate) Check() error {
	baseDecl := connc.BaseDeclaration
	if baseDecl == nil {
		return fmt.Errorf("internal error: improperly initialized ConnectCandidate")
	}
	iface := connc.Plug.Interface

	if plugDecl := connc.PlugSnapDeclaration; plugDecl != nil {
		if rule := plugDecl.PlugRule(iface); rule != nil {
			return connc.checkPlugRule(rule, true)
		}
	}
	if slotDecl := connc.SlotSnapDeclaration; slotDecl != nil {
		if rule := slotDecl.SlotRule(iface); rule != nil {
			return connc.checkSlotRule(rule, true)
		}
	}
	if rule := baseDecl.PlugRule(iface); rule != nil {
		return connc.checkPlugRule(rule, false)
	}
	if rule := baseDecl.SlotRule(iface); rule != nil {
		return connc.checkSlotRule(rule, false)
	}
	return nil
}

// TODO: CheckAutoConnect()
