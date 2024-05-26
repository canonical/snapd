// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ifacestate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// deviceKey determines a key for given device and hotplug interface. Every interface may provide a custom HotplugDeviceKey method
// to compute device key - if it doesn't, we fall back to defaultDeviceKey.
func deviceKey(device *hotplug.HotplugDeviceInfo, iface interfaces.Interface, defaultDeviceKey snap.HotplugKey) (deviceKey snap.HotplugKey, err error) {
	if keyhandler, ok := iface.(hotplug.HotplugKeyHandler); ok {
		deviceKey = mylog.Check2(keyhandler.HotplugKey(device))

		if deviceKey != "" {
			return deviceKey, nil
		}
	}
	return defaultDeviceKey, nil
}

// List of attributes that determine the computation of default device key.
// Attributes are grouped by similarity, the first non-empty attribute within the group goes into the key.
// The final key is composed of 4 attributes (some of which may be empty), separated by "/".
// Warning, any future changes to these definitions require a new key version.
var attrGroups = [][][]string{
	// key version 0
	{
		// Name
		{"ID_V4L_PRODUCT", "NAME", "ID_NET_NAME", "PCI_SLOT_NAME"},
		// Vendor
		{"ID_VENDOR_ID", "ID_VENDOR", "ID_WWN", "ID_WWN_WITH_EXTENSION", "ID_VENDOR_FROM_DATABASE", "ID_VENDOR_ENC", "ID_OUI_FROM_DATABASE"},
		// Model
		{"ID_MODEL_ID", "ID_MODEL_ENC"},
		// Identifier
		{"ID_SERIAL", "ID_SERIAL_SHORT", "ID_NET_NAME_MAC", "ID_REVISION"},
	},
}

// deviceKeyVersion is the current version number for the default keys computed by hotplug subsystem.
// Fresh device keys always use current version format
var deviceKeyVersion = len(attrGroups) - 1

// defaultDeviceKey computes device key from the attributes of
// HotplugDeviceInfo. Empty string is returned if too few attributes are present
// to compute a good key. Attributes used to compute device key are defined in
// attrGroups list above and they depend on the keyVersion passed to the
// function.
// The resulting key returned by the function has the following format:
// <version><checksum> where checksum is the sha256 checksum computed over
// select attributes of the device.
func defaultDeviceKey(devinfo *hotplug.HotplugDeviceInfo, keyVersion int) (snap.HotplugKey, error) {
	found := 0
	key := sha256.New()
	if keyVersion >= 16 || keyVersion >= len(attrGroups) {
		return "", fmt.Errorf("internal error: invalid key version %d", keyVersion)
	}
	for _, group := range attrGroups[keyVersion] {
		for _, attr := range group {
			if val, ok := devinfo.Attribute(attr); ok && val != "" {
				key.Write([]byte(attr))
				key.Write([]byte{0})
				key.Write([]byte(val))
				key.Write([]byte{0})
				found++
				break
			}
		}
	}
	if found < 2 {
		return "", nil
	}
	return snap.HotplugKey(fmt.Sprintf("%x%x", keyVersion, key.Sum(nil))), nil
}

// hotplugDeviceAdded gets called when a device is added to the system.
func (m *InterfaceManager) hotplugDeviceAdded(devinfo *hotplug.HotplugDeviceInfo) {
	st := m.state
	st.Lock()
	defer st.Unlock()
	mylog.Check2(systemSnapInfo(st))

	defaultKey := mylog.Check2(defaultDeviceKey(devinfo, deviceKeyVersion))

	hotplugFeature := mylog.Check2(m.hotplugEnabled())

	deviceCtx := mylog.Check2(snapstate.DeviceCtxFromState(st, nil))

	gadget := mylog.Check2(snapstate.GadgetInfo(st, deviceCtx))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		logger.Noticef("internal error: cannot get gadget information: %v", err)
	}

	hotplugIfaces := m.repo.AllHotplugInterfaces()
	gadgetSlotsByInterface := make(map[string][]*snap.SlotInfo)
	if gadget != nil {
		for _, gadgetSlot := range gadget.Slots {
			if _, ok := hotplugIfaces[gadgetSlot.Interface]; ok {
				gadgetSlotsByInterface[gadgetSlot.Interface] = append(gadgetSlotsByInterface[gadgetSlot.Interface], gadgetSlot)
			}
		}
	}

InterfacesLoop:
	// iterate over all hotplug interfaces
	for _, iface := range hotplugIfaces {
		hotplugHandler := iface.(hotplug.Definer)

		// ignore device that is already handled by a gadget slot
		if gadgetSlots, ok := gadgetSlotsByInterface[iface.Name()]; ok {
			for _, gslot := range gadgetSlots {
				if pred, ok := iface.(hotplug.HandledByGadgetPredicate); ok {
					if pred.HandledByGadget(devinfo, gslot) {
						logger.Debugf("ignoring device %s, interface %q (handled by gadget slot %s)", devinfo, iface.Name(), gslot.Name)
						continue InterfacesLoop
					}
				}
			}
		}

		proposedSlot := mylog.Check2(hotplugHandler.HotplugDeviceDetected(devinfo))

		// if the interface doesn't propose a slot, carry on and go to the next interface
		if proposedSlot == nil {
			continue
		}

		// Check the key when we know the interface wants to create a hotplug slot, doing this earlier would generate too much log noise about irrelevant devices
		key := mylog.Check2(deviceKey(devinfo, iface, defaultKey))

		if key == "" {
			logger.Noticef("no valid hotplug key provided by interface %q, device %s ignored", iface.Name(), devinfo)
			continue
		}

		proposedSlot = mylog.Check2(proposedSlot.Clean())

		if proposedSlot.Label == "" {
			si := interfaces.StaticInfoOf(iface)
			proposedSlot.Label = si.Summary
		}

		if !hotplugFeature {
			logger.Noticef("hotplug device add event ignored, enable experimental.hotplug")
			return
		}

		logger.Debugf("adding hotplug device %s for interface %q, hotplug key %q", devinfo, iface.Name(), key)

		seq := mylog.Check2(allocHotplugSeq(st))

		if !m.enumerationDone {
			if m.enumeratedDeviceKeys[iface.Name()] == nil {
				m.enumeratedDeviceKeys[iface.Name()] = make(map[snap.HotplugKey]bool)
			}
			m.enumeratedDeviceKeys[iface.Name()][key] = true
		}
		devPath := devinfo.DevicePath()
		// We may have different interfaces at same paths (e.g. a "foo-observe" and "foo-control" interfaces), therefore use lists.
		// Duplicates are not expected here because if a device is plugged twice, there will be an udev "remove" event between the adds
		// and hotplugDeviceRemoved() will remove affected path from hotplugDevicePaths.
		m.hotplugDevicePaths[devPath] = append(m.hotplugDevicePaths[devPath], deviceData{hotplugKey: key, ifaceName: iface.Name()})

		hotplugAdd := st.NewTask("hotplug-add-slot", fmt.Sprintf("Create slot for device %s with hotplug key %q", devinfo.ShortString(), key.ShortString()))
		setHotplugAttrs(hotplugAdd, iface.Name(), key)
		hotplugAdd.Set("device-info", devinfo)
		hotplugAdd.Set("proposed-slot", proposedSlot)

		hotplugConnect := st.NewTask("hotplug-connect", fmt.Sprintf("Recreate connections of interface %q for device %s with hotplug key %q", iface.Name(), devinfo.ShortString(), key.ShortString()))
		setHotplugAttrs(hotplugConnect, iface.Name(), key)
		hotplugConnect.WaitFor(hotplugAdd)

		chg := st.NewChange(fmt.Sprintf("hotplug-add-slot-%s", iface), fmt.Sprintf("Add hotplug slot of interface %q for device %s with hotplug key %q", devinfo.ShortString(), iface.Name(), key.ShortString()))
		chg.AddTask(hotplugAdd)
		chg.AddTask(hotplugConnect)
		addHotplugSeqWaitTask(chg, key, seq)

		st.EnsureBefore(0)
	}
}

// hotplugDeviceRemoved gets called when a device is removed from the system.
func (m *InterfaceManager) hotplugDeviceRemoved(devinfo *hotplug.HotplugDeviceInfo) {
	st := m.state
	st.Lock()
	defer st.Unlock()

	hotplugFeature := mylog.Check2(m.hotplugEnabled())

	devPath := devinfo.DevicePath()
	devs := m.hotplugDevicePaths[devPath]
	delete(m.hotplugDevicePaths, devPath)

	var changed bool
	for _, dev := range devs {
		hotplugKey := dev.hotplugKey
		ifaceName := dev.ifaceName
		slot := mylog.Check2(m.repo.SlotForHotplugKey(ifaceName, hotplugKey))

		if slot == nil {
			continue
		}

		if !hotplugFeature {
			logger.Noticef("hotplug device remove event ignored, enable experimental.hotplug")
			return
		}

		logger.Debugf("removing hotplug device %s for interface %q, hotplug key %q", devinfo, ifaceName, hotplugKey)

		seq := mylog.Check2(allocHotplugSeq(st))

		ts := removeDevice(st, ifaceName, hotplugKey)
		chg := st.NewChange(fmt.Sprintf("hotplug-remove-%s", ifaceName), fmt.Sprintf("Remove hotplug connections and slots of device %s with interface %q", devinfo.ShortString(), ifaceName))
		chg.AddAll(ts)
		addHotplugSeqWaitTask(chg, hotplugKey, seq)
		changed = true
	}

	if changed {
		st.EnsureBefore(0)
	}
}

// hotplugEnumerationDone gets called when initial enumeration on startup is finished.
func (m *InterfaceManager) hotplugEnumerationDone() {
	st := m.state
	st.Lock()
	defer st.Unlock()

	hotplugSlots := mylog.Check2(getHotplugSlots(st))

	for _, slot := range hotplugSlots {
		if byIface, ok := m.enumeratedDeviceKeys[slot.Interface]; ok {
			if byIface[slot.HotplugKey] {
				continue
			}
		}
		// device not present, disconnect its slots and remove them (as if it was unplugged)
		seq := mylog.Check2(allocHotplugSeq(st))

		ts := removeDevice(st, slot.Interface, slot.HotplugKey)
		chg := st.NewChange(fmt.Sprintf("hotplug-remove-%s", slot.Interface), fmt.Sprintf("Remove hotplug connections and slots of interface %q", slot.Interface))
		chg.AddAll(ts)
		addHotplugSeqWaitTask(chg, slot.HotplugKey, seq)
	}
	st.EnsureBefore(0)

	// the map of enumeratedDeviceKeys is not needed anymore
	m.enumeratedDeviceKeys = nil
	m.enumerationDone = true
}

func (m *InterfaceManager) hotplugEnabled() (bool, error) {
	tr := config.NewTransaction(m.state)
	return features.Flag(tr, features.Hotplug)
}

// ensureUniqueName modifies proposedName so that it's unique according to isUnique predicate.
// Uniqueness is achieved by appending a numeric suffix.
func ensureUniqueName(proposedName string, isUnique func(string) bool) string {
	// if the name is unique right away, do nothing
	if isUnique(proposedName) {
		return proposedName
	}

	baseName := proposedName
	suffixNumValue := 1
	// increase suffix value until we have a unique name
	for {
		proposedName = fmt.Sprintf("%s-%d", baseName, suffixNumValue)
		if isUnique(proposedName) {
			return proposedName
		}
		suffixNumValue++
	}
}

const maxGenerateSlotNameLen = 20

// makeSlotName sanitizes a string to make it a valid slot name that
// passes validation rules implemented by ValidateSlotName (see snap/validate.go):
// - only lowercase letter, digits and dashes are allowed
// - must start with a letter
// - no double dashes, cannot end with a dash.
// In addition names are truncated not to exceed maxGenerateSlotNameLen characters.
func makeSlotName(s string) string {
	var out []rune
	// the dash flag is used to prevent consecutive dashes, and the dash in the front
	dash := true
	for _, c := range s {
		switch {
		case c == '-' && !dash:
			dash = true
			out = append(out, '-')
		case unicode.IsLetter(c):
			out = append(out, unicode.ToLower(c))
			dash = false
		case unicode.IsDigit(c) && len(out) > 0:
			out = append(out, c)
			dash = false
		default:
			// any other character is ignored
		}
		if len(out) >= maxGenerateSlotNameLen {
			break
		}
	}
	// make sure the name doesn't end with a dash
	return strings.TrimRight(string(out), "-")
}

var nameAttrs = []string{"NAME", "ID_MODEL_FROM_DATABASE", "ID_MODEL"}

// suggestedSlotName returns the shortest name derived from attributes defined
// by nameAttrs, or the fallbackName if there is no known attribute to derive
// name from. The name created from attributes is sanitized to ensure it's a
// valid slot name. The fallbackName is typically the name of the interface.
func suggestedSlotName(devinfo *hotplug.HotplugDeviceInfo, fallbackName string) string {
	var shortestName string
	for _, attr := range nameAttrs {
		name, ok := devinfo.Attribute(attr)
		if ok {
			if name := makeSlotName(name); name != "" {
				if shortestName == "" || len(name) < len(shortestName) {
					shortestName = name
				}
			}
		}
	}
	if len(shortestName) == 0 {
		return fallbackName
	}
	return shortestName
}

// hotplugSlotName returns a slot name derived from slotSpecName or device attributes, or interface name, in that priority order, depending
// on which information is available. The chosen name is guaranteed to be unique
func hotplugSlotName(hotplugKey snap.HotplugKey, systemSnapInstanceName, slotSpecName, ifaceName string, devinfo *hotplug.HotplugDeviceInfo, repo *interfaces.Repository, stateSlots map[string]*HotplugSlotInfo) string {
	proposedName := slotSpecName
	if proposedName == "" {
		proposedName = suggestedSlotName(devinfo, ifaceName)
	}
	proposedName = ensureUniqueName(proposedName, func(slotName string) bool {
		if slot, ok := stateSlots[slotName]; ok {
			return slot.HotplugKey == hotplugKey
		}
		return repo.Slot(systemSnapInstanceName, slotName) == nil
	})
	return proposedName
}

// updateDevice creates tasks to disconnect slots of given device and update the slot in the repository.
func updateDevice(st *state.State, ifaceName string, hotplugKey snap.HotplugKey, newAttrs map[string]interface{}) *state.TaskSet {
	hotplugDisconnect := st.NewTask("hotplug-disconnect", fmt.Sprintf("Disable connections of interface %q, hotplug key %q", ifaceName, hotplugKey.ShortString()))
	setHotplugAttrs(hotplugDisconnect, ifaceName, hotplugKey)

	updateSlot := st.NewTask("hotplug-update-slot", fmt.Sprintf("Update slot of interface %q, hotplug key %q", ifaceName, hotplugKey.ShortString()))
	setHotplugAttrs(updateSlot, ifaceName, hotplugKey)
	updateSlot.Set("slot-attrs", newAttrs)
	updateSlot.WaitFor(hotplugDisconnect)

	return state.NewTaskSet(hotplugDisconnect, updateSlot)
}

// removeDevice creates tasks to disconnect slots of given device and remove affected slots.
func removeDevice(st *state.State, ifaceName string, hotplugKey snap.HotplugKey) *state.TaskSet {
	// hotplug-disconnect task will create hooks and disconnect the slot
	hotplugDisconnect := st.NewTask("hotplug-disconnect", fmt.Sprintf("Disable connections of interface %q, hotplug key %q", ifaceName, hotplugKey.ShortString()))
	setHotplugAttrs(hotplugDisconnect, ifaceName, hotplugKey)

	// hotplug-remove-slot will remove this device's slot from the repository.
	removeSlot := st.NewTask("hotplug-remove-slot", fmt.Sprintf("Remove slot for interface %q, hotplug key %q", ifaceName, hotplugKey.ShortString()))
	setHotplugAttrs(removeSlot, ifaceName, hotplugKey)
	removeSlot.WaitFor(hotplugDisconnect)

	return state.NewTaskSet(hotplugDisconnect, removeSlot)
}
