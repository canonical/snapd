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

	Model *asserts.Model
	Store *asserts.Store
}

func (ic *InstallCandidate) snapID() string {
	if ic.SnapDeclaration != nil {
		return ic.SnapDeclaration.SnapID()
	}
	return "" // never a valid snap-id
}

func (ic *InstallCandidate) checkSlotRule(slot *snap.SlotInfo, rule *asserts.SlotRule, snapRule bool) error {
	context := ""
	if snapRule {
		context = fmt.Sprintf(" for %q snap", ic.SnapDeclaration.SnapName())
	}
	if checkSlotInstallationAltConstraints(ic, slot, rule.DenyInstallation) == nil {
		return fmt.Errorf("installation denied by %q slot rule of interface %q%s", slot.Name, slot.Interface, context)
	}
	if checkSlotInstallationAltConstraints(ic, slot, rule.AllowInstallation) != nil {
		return fmt.Errorf("installation not allowed by %q slot rule of interface %q%s", slot.Name, slot.Interface, context)
	}
	return nil
}

func (ic *InstallCandidate) checkPlugRule(plug *snap.PlugInfo, rule *asserts.PlugRule, snapRule bool) error {
	context := ""
	if snapRule {
		context = fmt.Sprintf(" for %q snap", ic.SnapDeclaration.SnapName())
	}
	if checkPlugInstallationAltConstraints(ic, plug, rule.DenyInstallation) == nil {
		return fmt.Errorf("installation denied by %q plug rule of interface %q%s", plug.Name, plug.Interface, context)
	}
	if checkPlugInstallationAltConstraints(ic, plug, rule.AllowInstallation) != nil {
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

	Model *asserts.Model
	Store *asserts.Store
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

func (connc *ConnectCandidate) PlugPublisherID() string {
	if connc.PlugSnapDeclaration != nil {
		return connc.PlugSnapDeclaration.PublisherID()
	}
	return "" // never a valid publisher-id
}

func (connc *ConnectCandidate) SlotPublisherID() string {
	if connc.SlotSnapDeclaration != nil {
		return connc.SlotSnapDeclaration.PublisherID()
	}
	return "" // never a valid publisher-id
}

func (connc *ConnectCandidate) checkPlugRule(kind string, rule *asserts.PlugRule, snapRule bool) (interfaces.SideArity, error) {
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
	if _, err := checkPlugConnectionAltConstraints(connc, denyConst); err == nil {
		return nil, fmt.Errorf("%s denied by plug rule of interface %q%s", kind, connc.Plug.Interface(), context)
	}

	allowedConstraints, err := checkPlugConnectionAltConstraints(connc, allowConst)
	if err != nil {
		return nil, fmt.Errorf("%s not allowed by plug rule of interface %q%s", kind, connc.Plug.Interface(), context)
	}
	return sideArity{allowedConstraints.SlotsPerPlug}, nil
}

func (connc *ConnectCandidate) checkSlotRule(kind string, rule *asserts.SlotRule, snapRule bool) (interfaces.SideArity, error) {
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
	if _, err := checkSlotConnectionAltConstraints(connc, denyConst); err == nil {
		return nil, fmt.Errorf("%s denied by slot rule of interface %q%s", kind, connc.Plug.Interface(), context)
	}

	allowedConstraints, err := checkSlotConnectionAltConstraints(connc, allowConst)
	if err != nil {
		return nil, fmt.Errorf("%s not allowed by slot rule of interface %q%s", kind, connc.Plug.Interface(), context)
	}
	return sideArity{allowedConstraints.SlotsPerPlug}, nil
}

func (connc *ConnectCandidate) check(kind string) (interfaces.SideArity, error) {
	baseDecl := connc.BaseDeclaration
	if baseDecl == nil {
		return nil, fmt.Errorf("internal error: improperly initialized ConnectCandidate")
	}

	iface := connc.Plug.Interface()

	if connc.Slot.Interface() != iface {
		return nil, fmt.Errorf("cannot connect mismatched plug interface %q to slot interface %q", iface, connc.Slot.Interface())
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
	return nil, nil
}

// Check checks whether the connection is allowed.
func (connc *ConnectCandidate) Check() error {
	_, err := connc.check("connection")
	return err
}

// CheckAutoConnect checks whether the connection is allowed to auto-connect.
func (connc *ConnectCandidate) CheckAutoConnect() (interfaces.SideArity, error) {
	arity, err := connc.check("auto-connection")
	if err != nil {
		return nil, err
	}
	if arity == nil {
		// shouldn't happen but be safe, the callers should be able
		// to assume arity to be non nil
		arity = sideArity{asserts.SideArityConstraint{N: 1}}
	}
	return arity, nil
}

// InstallCandidateMinimalCheck represents a candidate snap installed with --dangerous flag that should pass minimum checks
// against snap type (if present). It doesn't check interface attributes.
type InstallCandidateMinimalCheck struct {
	Snap            *snap.Info
	BaseDeclaration *asserts.BaseDeclaration
	Model           *asserts.Model
	Store           *asserts.Store
}

func (ic *InstallCandidateMinimalCheck) checkSlotRule(slot *snap.SlotInfo, rule *asserts.SlotRule) error {
	// we use the allow-installation to check if the snap type
	// is expected to have this kind of slot at all,
	// the potential deny-installation is ignored here, but allows
	// to for example constraint super-privileged app-provided slots
	// while letting user test them locally with --dangerous
	// TODO check that the snap is an app or gadget if allow-installation had no slot-snap-type constraints
	if _, err := checkMinimalSlotInstallationAltConstraints(slot, rule.AllowInstallation); err != nil {
		return fmt.Errorf("installation not allowed by %q slot rule of interface %q", slot.Name, slot.Interface)
	}
	return nil
}

func (ic *InstallCandidateMinimalCheck) checkSlot(slot *snap.SlotInfo) error {
	iface := slot.Interface
	if rule := ic.BaseDeclaration.SlotRule(iface); rule != nil {
		return ic.checkSlotRule(slot, rule)
	}
	return nil
}

// Check checks whether the installation is allowed.
func (ic *InstallCandidateMinimalCheck) Check() error {
	if ic.BaseDeclaration == nil {
		return fmt.Errorf("internal error: improperly initialized InstallCandidateMinimalCheck")
	}

	for _, slot := range ic.Snap.Slots {
		err := ic.checkSlot(slot)
		if err != nil {
			return err
		}
	}

	return nil
}
