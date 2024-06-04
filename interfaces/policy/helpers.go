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

package policy

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// check helpers

func checkSnapType(snapInfo *snap.Info, types []string) error {
	if len(types) == 0 {
		return nil
	}
	snapType := snapInfo.Type()
	s := string(snapType)
	if snapType == snap.TypeOS || snapType == snap.TypeSnapd {
		// we use "core" in the assertions and we need also to
		// allow for the "snapd" snap
		s = "core"
	}
	for _, t := range types {
		if t == s {
			return nil
		}
	}
	return fmt.Errorf("snap type does not match")
}

func checkID(kind, id string, ids []string, special map[string]string) error {
	if len(ids) == 0 {
		return nil
	}
	if id == "" { // unset values never match
		return fmt.Errorf("%s does not match", kind)
	}
	for _, cand := range ids {
		if strings.HasPrefix(cand, "$") {
			cand = special[cand]
			if cand == "" { // we ignore unknown special "ids"
				continue
			}
		}
		if id == cand {
			return nil
		}
	}
	return fmt.Errorf("%s does not match", kind)
}

func checkOnClassic(c *asserts.OnClassicConstraint) error {
	if c == nil {
		return nil
	}
	if c.Classic != release.OnClassic {
		return fmt.Errorf("on-classic mismatch")
	}
	if c.Classic && len(c.SystemIDs) != 0 {
		return checkID("operating system ID", release.ReleaseInfo.ID, c.SystemIDs, nil)
	}
	return nil
}

func checkDeviceScope(c *asserts.DeviceScopeConstraint, model *asserts.Model, store *asserts.Store) error {
	if c == nil {
		return nil
	}
	opts := asserts.DeviceScopeConstraintCheckOptions{
		UseFriendlyStores: true,
	}
	return c.Check(model, store, &opts)
}

func checkNameConstraints(c *asserts.NameConstraints, iface, which, name string) error {
	if c == nil {
		return nil
	}
	special := map[string]string{
		"$INTERFACE": iface,
	}
	return c.Check(which, name, special)
}

func checkPlugConnectionConstraints1(connc *ConnectCandidate, constraints *asserts.PlugConnectionConstraints) error {
	if err := checkNameConstraints(constraints.PlugNames, connc.Plug.Interface(), "plug name", connc.Plug.Name()); err != nil {
		return err
	}
	if err := checkNameConstraints(constraints.SlotNames, connc.Slot.Interface(), "slot name", connc.Slot.Name()); err != nil {
		return err
	}

	if err := constraints.PlugAttributes.Check(connc.Plug, connc); err != nil {
		return err
	}
	if err := constraints.SlotAttributes.Check(connc.Slot, connc); err != nil {
		return err
	}
	if err := checkSnapType(connc.Slot.Snap(), constraints.SlotSnapTypes); err != nil {
		return err
	}
	if err := checkID("snap id", connc.slotSnapID(), constraints.SlotSnapIDs, nil); err != nil {
		return err
	}
	err := checkID("publisher id", connc.slotPublisherID(), constraints.SlotPublisherIDs, map[string]string{
		"$PLUG_PUBLISHER_ID": connc.plugPublisherID(),
	})
	if err != nil {
		return err
	}
	if err := checkOnClassic(constraints.OnClassic); err != nil {
		return err
	}
	if err := checkDeviceScope(constraints.DeviceScope, connc.Model, connc.Store); err != nil {
		return err
	}
	return nil
}

func checkPlugConnectionAltConstraints(connc *ConnectCandidate, altConstraints []*asserts.PlugConnectionConstraints) (*asserts.PlugConnectionConstraints, error) {
	var firstErr error
	// OR of constraints
	for _, constraints := range altConstraints {
		err := checkPlugConnectionConstraints1(connc, constraints)
		if err == nil {
			return constraints, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func checkSlotConnectionConstraints1(connc *ConnectCandidate, constraints *asserts.SlotConnectionConstraints) error {
	if err := checkNameConstraints(constraints.PlugNames, connc.Plug.Interface(), "plug name", connc.Plug.Name()); err != nil {
		return err
	}
	if err := checkNameConstraints(constraints.SlotNames, connc.Slot.Interface(), "slot name", connc.Slot.Name()); err != nil {
		return err
	}

	if err := constraints.PlugAttributes.Check(connc.Plug, connc); err != nil {
		return err
	}
	if err := constraints.SlotAttributes.Check(connc.Slot, connc); err != nil {
		return err
	}
	if err := checkSnapType(connc.Slot.Snap(), constraints.SlotSnapTypes); err != nil {
		return err
	}
	if err := checkSnapType(connc.Plug.Snap(), constraints.PlugSnapTypes); err != nil {
		return err
	}
	if err := checkID("snap id", connc.plugSnapID(), constraints.PlugSnapIDs, nil); err != nil {
		return err
	}
	err := checkID("publisher id", connc.plugPublisherID(), constraints.PlugPublisherIDs, map[string]string{
		"$SLOT_PUBLISHER_ID": connc.slotPublisherID(),
	})
	if err != nil {
		return err
	}
	if err := checkOnClassic(constraints.OnClassic); err != nil {
		return err
	}
	if err := checkDeviceScope(constraints.DeviceScope, connc.Model, connc.Store); err != nil {
		return err
	}
	return nil
}

func checkSlotConnectionAltConstraints(connc *ConnectCandidate, altConstraints []*asserts.SlotConnectionConstraints) (*asserts.SlotConnectionConstraints, error) {
	var firstErr error
	// OR of constraints
	for _, constraints := range altConstraints {
		err := checkSlotConnectionConstraints1(connc, constraints)
		if err == nil {
			return constraints, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func checkSnapTypeSlotInstallationConstraints1(slot *snap.SlotInfo, constraints *asserts.SlotInstallationConstraints) error {
	if err := checkSnapType(slot.Snap, constraints.SlotSnapTypes); err != nil {
		return err
	}
	if err := checkOnClassic(constraints.OnClassic); err != nil {
		return err
	}
	return nil
}

func checkMinimalSlotInstallationAltConstraints(slot *snap.SlotInfo, altConstraints []*asserts.SlotInstallationConstraints) (bool, error) {
	var firstErr error
	var hasSnapTypeConstraints bool
	// OR of constraints
	for _, constraints := range altConstraints {
		if constraints.OnClassic == nil && len(constraints.SlotSnapTypes) == 0 {
			continue
		}
		hasSnapTypeConstraints = true
		err := checkSnapTypeSlotInstallationConstraints1(slot, constraints)
		if err == nil {
			return true, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return hasSnapTypeConstraints, firstErr
}

func checkSlotInstallationConstraints1(ic *InstallCandidate, slot *snap.SlotInfo, constraints *asserts.SlotInstallationConstraints) error {
	if err := checkNameConstraints(constraints.SlotNames, slot.Interface, "slot name", slot.Name); err != nil {
		return err
	}

	// TODO: allow evaluated attr constraints here too?
	if err := constraints.SlotAttributes.Check(slot, nil); err != nil {
		return err
	}
	if err := checkSnapType(slot.Snap, constraints.SlotSnapTypes); err != nil {
		return err
	}
	if err := checkID("snap id", ic.snapID(), constraints.SlotSnapIDs, nil); err != nil {
		return err
	}
	if err := checkOnClassic(constraints.OnClassic); err != nil {
		return err
	}
	if err := checkDeviceScope(constraints.DeviceScope, ic.Model, ic.Store); err != nil {
		return err
	}
	return nil
}

func checkSlotInstallationAltConstraints(ic *InstallCandidate, slot *snap.SlotInfo, altConstraints []*asserts.SlotInstallationConstraints) error {
	var firstErr error
	// OR of constraints
	for _, constraints := range altConstraints {
		err := checkSlotInstallationConstraints1(ic, slot, constraints)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func checkPlugInstallationConstraints1(ic *InstallCandidate, plug *snap.PlugInfo, constraints *asserts.PlugInstallationConstraints) error {
	if err := checkNameConstraints(constraints.PlugNames, plug.Interface, "plug name", plug.Name); err != nil {
		return err
	}

	// TODO: allow evaluated attr constraints here too?
	if err := constraints.PlugAttributes.Check(plug, nil); err != nil {
		return err
	}
	if err := checkSnapType(plug.Snap, constraints.PlugSnapTypes); err != nil {
		return err
	}
	if err := checkID("snap id", ic.snapID(), constraints.PlugSnapIDs, nil); err != nil {
		return err
	}
	if err := checkOnClassic(constraints.OnClassic); err != nil {
		return err
	}
	if err := checkDeviceScope(constraints.DeviceScope, ic.Model, ic.Store); err != nil {
		return err
	}
	return nil
}

func checkPlugInstallationAltConstraints(ic *InstallCandidate, plug *snap.PlugInfo, altConstraints []*asserts.PlugInstallationConstraints) error {
	var firstErr error
	// OR of constraints
	for _, constraints := range altConstraints {
		err := checkPlugInstallationConstraints1(ic, plug, constraints)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// sideArity carries relevant arity constraints for successful
// allow-auto-connection rules. It implements policy.SideArity.
// ATM only slots-per-plug might have an interesting non-default
// value.
// See: https://forum.snapcraft.io/t/plug-slot-declaration-rules-greedy-plugs/12438
type sideArity struct {
	slotsPerPlug asserts.SideArityConstraint
}

func (a sideArity) SlotsPerPlugOne() bool {
	return a.slotsPerPlug.N == 1
}

func (a sideArity) SlotsPerPlugAny() bool {
	return a.slotsPerPlug.Any()
}
