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
	"github.com/snapcore/snapd/strutil"
)

// check helpers

func checkSnapType(snapInfo *snap.Info, types []string) error {
	if len(types) == 0 {
		return nil
	}
	snapType := snapInfo.GetType()
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
	if model == nil {
		return fmt.Errorf("cannot match on-store/on-brand/on-model without model")
	}
	if store != nil && store.Store() != model.Store() {
		return fmt.Errorf("store assertion and model store must match")
	}
	if len(c.Store) != 0 {
		if !strutil.ListContains(c.Store, model.Store()) {
			mismatch := true
			if store != nil {
				for _, sto := range c.Store {
					if strutil.ListContains(store.FriendlyStores(), sto) {
						mismatch = false
						break
					}
				}
			}
			if mismatch {
				return fmt.Errorf("on-store mismatch")
			}
		}
	}
	if len(c.Brand) != 0 {
		if !strutil.ListContains(c.Brand, model.BrandID()) {
			return fmt.Errorf("on-brand mismatch")
		}
	}
	if len(c.Model) != 0 {
		brandModel := fmt.Sprintf("%s/%s", model.BrandID(), model.Model())
		if !strutil.ListContains(c.Model, brandModel) {
			return fmt.Errorf("on-model mismatch")
		}
	}
	return nil
}

func checkPlugConnectionConstraints1(connc *ConnectCandidate, cstrs *asserts.PlugConnectionConstraints) error {
	if err := cstrs.PlugAttributes.Check(connc.Plug, connc); err != nil {
		return err
	}
	if err := cstrs.SlotAttributes.Check(connc.Slot, connc); err != nil {
		return err
	}
	if err := checkSnapType(connc.Slot.Snap(), cstrs.SlotSnapTypes); err != nil {
		return err
	}
	if err := checkID("snap id", connc.slotSnapID(), cstrs.SlotSnapIDs, nil); err != nil {
		return err
	}
	err := checkID("publisher id", connc.slotPublisherID(), cstrs.SlotPublisherIDs, map[string]string{
		"$PLUG_PUBLISHER_ID": connc.plugPublisherID(),
	})
	if err != nil {
		return err
	}
	if err := checkOnClassic(cstrs.OnClassic); err != nil {
		return err
	}
	if err := checkDeviceScope(cstrs.DeviceScope, connc.Model, connc.Store); err != nil {
		return err
	}
	return nil
}

func checkPlugConnectionConstraints(connc *ConnectCandidate, cstrs []*asserts.PlugConnectionConstraints) error {
	var firstErr error
	// OR of constraints
	for _, cstrs1 := range cstrs {
		err := checkPlugConnectionConstraints1(connc, cstrs1)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func checkSlotConnectionConstraints1(connc *ConnectCandidate, cstrs *asserts.SlotConnectionConstraints) error {
	if err := cstrs.PlugAttributes.Check(connc.Plug, connc); err != nil {
		return err
	}
	if err := cstrs.SlotAttributes.Check(connc.Slot, connc); err != nil {
		return err
	}
	if err := checkSnapType(connc.Plug.Snap(), cstrs.PlugSnapTypes); err != nil {
		return err
	}
	if err := checkID("snap id", connc.plugSnapID(), cstrs.PlugSnapIDs, nil); err != nil {
		return err
	}
	err := checkID("publisher id", connc.plugPublisherID(), cstrs.PlugPublisherIDs, map[string]string{
		"$SLOT_PUBLISHER_ID": connc.slotPublisherID(),
	})
	if err != nil {
		return err
	}
	if err := checkOnClassic(cstrs.OnClassic); err != nil {
		return err
	}
	if err := checkDeviceScope(cstrs.DeviceScope, connc.Model, connc.Store); err != nil {
		return err
	}
	return nil
}

func checkSlotConnectionConstraints(connc *ConnectCandidate, cstrs []*asserts.SlotConnectionConstraints) error {
	var firstErr error
	// OR of constraints
	for _, cstrs1 := range cstrs {
		err := checkSlotConnectionConstraints1(connc, cstrs1)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func checkSnapTypeSlotInstallationConstraints1(ic *InstallCandidateMinimalCheck, slot *snap.SlotInfo, cstrs *asserts.SlotInstallationConstraints) error {
	if err := checkSnapType(slot.Snap, cstrs.SlotSnapTypes); err != nil {
		return err
	}
	if err := checkOnClassic(cstrs.OnClassic); err != nil {
		return err
	}
	return nil
}

func checkMinimalSlotInstallationConstraints(ic *InstallCandidateMinimalCheck, slot *snap.SlotInfo, cstrs []*asserts.SlotInstallationConstraints) (bool, error) {
	var firstErr error
	var hasSnapTypeConstraints bool
	// OR of constraints
	for _, cstrs1 := range cstrs {
		if cstrs1.OnClassic == nil && len(cstrs1.SlotSnapTypes) == 0 {
			continue
		}
		hasSnapTypeConstraints = true
		err := checkSnapTypeSlotInstallationConstraints1(ic, slot, cstrs1)
		if err == nil {
			return true, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return hasSnapTypeConstraints, firstErr
}

func checkSlotInstallationConstraints1(ic *InstallCandidate, slot *snap.SlotInfo, cstrs *asserts.SlotInstallationConstraints) error {
	// TODO: allow evaluated attr constraints here too?
	if err := cstrs.SlotAttributes.Check(slot, nil); err != nil {
		return err
	}
	if err := checkSnapType(slot.Snap, cstrs.SlotSnapTypes); err != nil {
		return err
	}
	if err := checkOnClassic(cstrs.OnClassic); err != nil {
		return err
	}
	if err := checkDeviceScope(cstrs.DeviceScope, ic.Model, ic.Store); err != nil {
		return err
	}
	return nil
}

func checkSlotInstallationConstraints(ic *InstallCandidate, slot *snap.SlotInfo, cstrs []*asserts.SlotInstallationConstraints) error {
	var firstErr error
	// OR of constraints
	for _, cstrs1 := range cstrs {
		err := checkSlotInstallationConstraints1(ic, slot, cstrs1)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func checkPlugInstallationConstraints1(ic *InstallCandidate, plug *snap.PlugInfo, cstrs *asserts.PlugInstallationConstraints) error {
	// TODO: allow evaluated attr constraints here too?
	if err := cstrs.PlugAttributes.Check(plug, nil); err != nil {
		return err
	}
	if err := checkSnapType(plug.Snap, cstrs.PlugSnapTypes); err != nil {
		return err
	}
	if err := checkOnClassic(cstrs.OnClassic); err != nil {
		return err
	}
	if err := checkDeviceScope(cstrs.DeviceScope, ic.Model, ic.Store); err != nil {
		return err
	}
	return nil
}

func checkPlugInstallationConstraints(ic *InstallCandidate, plug *snap.PlugInfo, cstrs []*asserts.PlugInstallationConstraints) error {
	var firstErr error
	// OR of constraints
	for _, cstrs1 := range cstrs {
		err := checkPlugInstallationConstraints1(ic, plug, cstrs1)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
