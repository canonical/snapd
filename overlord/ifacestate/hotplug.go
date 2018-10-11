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
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"

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
func deviceKey(defaultDeviceKey string, device *hotplug.HotplugDeviceInfo, iface interfaces.Interface) (deviceKey string, err error) {
	if keyhandler, ok := iface.(hotplug.HotplugKeyHandler); ok {
		deviceKey, err = keyhandler.HotplugKey(device)
		if err != nil {
			return "", fmt.Errorf("cannot create hotplug key for interface %q: %s", iface.Name(), err)
		}
		if deviceKey != "" {
			return deviceKey, nil
		}
	}
	return defaultDeviceKey, nil
}

// List of attributes that determine the computation of default device key.
// Attributes are grouped by similarity, the first non-empty attribute within the group goes into the key.
// The final key is composed of 4 attributes (some of which may be empty), separated by "/".
// Warning, any future changes to these definitions requrie a new key version.
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
func defaultDeviceKey(devinfo *hotplug.HotplugDeviceInfo, keyVersion int) (string, error) {
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
	return fmt.Sprintf("%x%x", keyVersion, key.Sum(nil)), nil
}

// HotplugDeviceAdded gets called when a device is added to the system.
func (m *InterfaceManager) hotplugDeviceAdded(devinfo *hotplug.HotplugDeviceInfo) {
	st := m.state
	st.Lock()
	defer st.Unlock()

	coreSnapInfo, err := snapstate.CoreInfo(st)
	if err != nil {
		logger.Noticef("core snap not available, hotplug events ignored")
		return
	}

	hotplugIfaces := m.repo.AllHotplugInterfaces()
	defaultKey, err := defaultDeviceKey(devinfo, deviceKeyVersion)
	if err != nil {
		logger.Noticef(err.Error())
	}

	hotplugFeature, err := m.hotplugEnabled()
	if err != nil {
		logger.Noticef(err.Error())
		return
	}

	// iterate over all hotplug interfaces
	for _, iface := range hotplugIfaces {
		hotplugHandler := iface.(hotplug.Definer)

		// determine device key for the interface; note that interface might provide own device keys.
		key, err := deviceKey(defaultKey, devinfo, iface)
		if err != nil {
			logger.Noticef(err.Error())
			continue
		}

		spec := hotplug.NewSpecification()
		if hotplugHandler.HotplugDeviceDetected(devinfo, spec) != nil {
			logger.Noticef("Failed to process hotplug event by the rule of interface %q: %s", iface.Name(), err)
			continue
		}
		slotSpec := spec.Slot()
		if slotSpec == nil {
			continue
		}

		if key == "" {
			logger.Debugf("No valid hotplug key provided by interface %s, device %q ignored", iface.Name(), devinfo.DeviceName())
			continue
		}

		if slotSpec.Label == "" {
			si := interfaces.StaticInfoOf(iface)
			slotSpec.Label = si.Summary
		}

		if !hotplugFeature {
			logger.Noticef("Hotplug 'add' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.DevicePath(), iface.Name())
			continue
		}

		logger.Debugf("HotplugDeviceAdded: %s (interface: %s, hotplug key: %q, devname %s, subsystem: %s)", devinfo.DevicePath(), iface, key, devinfo.DeviceName(), devinfo.Subsystem())

		if m.enumeratedDeviceKeys != nil {
			if m.enumeratedDeviceKeys[iface.Name()] == nil {
				m.enumeratedDeviceKeys[iface.Name()] = make(map[string]bool)
			}
			m.enumeratedDeviceKeys[iface.Name()][key] = true
		}
		devPath := devinfo.DevicePath()
		m.hotplugDevicePaths[devPath] = append(m.hotplugDevicePaths[devPath], deviceData{hotplugKey: key, ifaceName: iface.Name()})

		stateSlots, err := getHotplugSlots(st)
		if err != nil {
			logger.Noticef(err.Error())
			return
		}

		// if we know this slot already, check if its static attributes changed - if so, we need to update the repo and connections (if any)
		slot, err := m.repo.SlotForHotplugKey(iface.Name(), key)
		if err != nil {
			logger.Noticef("internal error: %s", err)
		}

		// Add or update slot in the repository
		if slot != nil {
			if reflect.DeepEqual(slotSpec.Attrs, slot.Attrs) {
				// slot attributes unchanged, nothing to do
				logger.Debugf("Slot %s for device %s already present and unchanged", slot.Name, key)
			} else {
				logger.Debugf("Slot %s for device %s has changed", slot.Name, key)
				ts := updateDevice(st, iface.Name(), key, slotSpec.Attrs)
				chg := st.NewChange(fmt.Sprintf("hotplug-update-%s", iface), fmt.Sprintf("Update hotplug slot of interface %s, device %s", iface.Name(), key))
				chg.AddAll(ts)
			}
		} else {
			// Determine slot name:
			// - if a slot for given hotplug key exists in hotplug state, use old name
			// - otherwise use name provided by slot spec, if set
			// - if not, use auto-generated name.
			proposedName := slotSpec.Name
			for _, stateSlot := range stateSlots {
				if stateSlot.HotplugKey == key {
					proposedName = stateSlot.Name
					break
				}
			}
			if proposedName == "" {
				proposedName = suggestedSlotName(devinfo, iface.Name())
			}
			proposedName = ensureUniqueName(proposedName, func(name string) bool {
				if slot, ok := stateSlots[name]; ok {
					return slot.HotplugKey == key
				}
				return m.repo.Slot(coreSnapInfo.InstanceName(), name) == nil
			})
			newSlot := &snap.SlotInfo{
				Name:       proposedName,
				Label:      slotSpec.Label,
				Snap:       coreSnapInfo,
				Interface:  iface.Name(),
				Attrs:      slotSpec.Attrs,
				HotplugKey: key,
			}
			if iface, ok := iface.(interfaces.SlotSanitizer); ok {
				if err := iface.BeforePrepareSlot(newSlot); err != nil {
					logger.Noticef("Failed to sanitize hotplug-created slot %q for interface %s: %s", newSlot.Name, newSlot.Interface, err)
					continue
				}
			}

			if err := m.repo.AddSlot(newSlot); err != nil {
				logger.Noticef("Failed to create slot %q for interface %s: %s", newSlot.Name, newSlot.Interface, err)
				continue
			}
			stateSlots[newSlot.Name] = &HotplugSlotInfo{
				Name:        newSlot.Name,
				Interface:   newSlot.Interface,
				StaticAttrs: newSlot.Attrs,
				HotplugKey:  newSlot.HotplugKey,
			}
			setHotplugSlots(st, stateSlots)

			logger.Noticef("Added hotplug slot %s:%s of interface %s for hotplug key %q", newSlot.Snap.InstanceName(), newSlot.Name, newSlot.Interface, key)

			chg := st.NewChange(fmt.Sprintf("hotplug-connect-%s", iface), fmt.Sprintf("Connect hotplug slot of interface %s", iface.Name()))
			hotplugConnect := st.NewTask("hotplug-connect", fmt.Sprintf("Recreate connections of device %q", key))
			setHotplugAttrs(hotplugConnect, iface.Name(), key)
			chg.AddTask(hotplugConnect)
		}
		st.EnsureBefore(0)
	}
}

// HotplugDeviceRemoved gets called when a device is removed from the system.
func (m *InterfaceManager) hotplugDeviceRemoved(devinfo *hotplug.HotplugDeviceInfo) {
	st := m.state
	st.Lock()
	defer st.Unlock()

	hotplugFeature, err := m.hotplugEnabled()
	if err != nil {
		logger.Noticef(err.Error())
		return
	}

	devPath := devinfo.DevicePath()
	devs := m.hotplugDevicePaths[devPath]
	delete(m.hotplugDevicePaths, devPath)

	for _, dev := range devs {
		hotplugKey := dev.hotplugKey
		ifaceName := dev.ifaceName
		slot, err := m.repo.SlotForHotplugKey(ifaceName, hotplugKey)
		if err != nil {
			logger.Noticef(err.Error())
			continue
		}
		if slot == nil {
			continue
		}

		if !hotplugFeature {
			logger.Noticef("Hotplug 'remove' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.DevicePath(), ifaceName)
			continue
		}
		logger.Debugf("HotplugDeviceRemoved: %s (interface: %s, hotplug key: %q, devname %s, subsystem: %s)", devinfo.DevicePath(), ifaceName, hotplugKey, devinfo.DeviceName(), devinfo.Subsystem())

		chg := st.NewChange(fmt.Sprintf("hotplug-remove-%s", ifaceName), fmt.Sprintf("Remove hotplug connections and slots of interface %s", ifaceName))
		ts := removeDevice(st, ifaceName, hotplugKey)
		chg.AddAll(ts)
	}

	if len(devs) > 0 {
		st.EnsureBefore(0)
	}
}

// create tasks to disconnect slots of given device and remove affected slots.
func removeDevice(st *state.State, ifaceName, hotplugKey string) *state.TaskSet {
	// hotplug-disconnect task will create hooks and disconnect the slot
	hotplugDisconnect := st.NewTask("hotplug-disconnect", fmt.Sprintf("Disable connections for interface %s, hotplug key %q", ifaceName, hotplugKey))
	setHotplugAttrs(hotplugDisconnect, ifaceName, hotplugKey)

	// hotplug-remove-slot will remove this device's slot from the repository.
	removeSlot := st.NewTask("hotplug-remove-slot", fmt.Sprintf("Remove slot for interface %s, hotplug key %q", ifaceName, hotplugKey))
	setHotplugAttrs(removeSlot, ifaceName, hotplugKey)
	removeSlot.WaitFor(hotplugDisconnect)

	return state.NewTaskSet(hotplugDisconnect, removeSlot)
}

// create tasks to disconnect slots of given device, update the slot in the repository, then connect it back.
func updateDevice(st *state.State, ifaceName, hotplugKey string, newAttrs map[string]interface{}) *state.TaskSet {
	hotplugDisconnect := st.NewTask("hotplug-disconnect", fmt.Sprintf("Disable connections of device %q", hotplugKey))
	setHotplugAttrs(hotplugDisconnect, ifaceName, hotplugKey)

	updateSlot := st.NewTask("hotplug-update-slot", fmt.Sprintf("Update slot of interface %s, hotplug key %q", ifaceName, hotplugKey))
	setHotplugAttrs(updateSlot, ifaceName, hotplugKey)
	updateSlot.Set("slot-attrs", newAttrs)
	updateSlot.WaitFor(hotplugDisconnect)

	hotplugConnect := st.NewTask("hotplug-connect", fmt.Sprintf("Recreate connections of interface %s hotplug key %s", ifaceName, hotplugKey))
	setHotplugAttrs(hotplugConnect, ifaceName, hotplugKey)
	hotplugConnect.WaitFor(updateSlot)

	return state.NewTaskSet(hotplugDisconnect, updateSlot, hotplugConnect)
}

func (m *InterfaceManager) hotplugEnumerationDone() {
	st := m.state
	st.Lock()
	defer st.Unlock()

	hotplugSlots, err := getHotplugSlots(st)
	if err != nil {
		logger.Noticef(err.Error())
		return
	}

	for _, slot := range hotplugSlots {
		if byIface, ok := m.enumeratedDeviceKeys[slot.Interface]; ok {
			if byIface[slot.HotplugKey] {
				continue
			}
		}
		// device not present, disconnect its slots and remove them (as if it was unplugged)
		chg := st.NewChange(fmt.Sprintf("hotplug-remove-%s", slot.Interface), fmt.Sprintf("Remove hotplug connections and slots of interface %s", slot.Interface))
		ts := removeDevice(st, slot.Interface, slot.HotplugKey)
		chg.AddAll(ts)
	}
	st.EnsureBefore(0)

	// the map of enumeratedDeviceKeys is not needed anymore
	m.enumeratedDeviceKeys = nil
}

func findConnsForDeviceKey(conns *map[string]connState, coreSnapName, ifaceName, hotplugKey string) []string {
	var connsForDevice []string
	for id, connSt := range *conns {
		if connSt.Interface != ifaceName || connSt.HotplugKey != hotplugKey {
			continue
		}
		connsForDevice = append(connsForDevice, id)
	}
	return connsForDevice
}

func (m *InterfaceManager) hotplugEnabled() (bool, error) {
	tr := config.NewTransaction(m.state)
	return snapstate.GetFeatureFlagBool(tr, "experimental.hotplug")
}

// ensureUniqueName modifies proposedName so that it's unique according to isUnique predicate.
// Uniqueness is achieved by appending a numeric suffix, or increasing existing suffix.
func ensureUniqueName(proposedName string, isUnique func(string) bool) string {
	// if the name is unique right away, do nothing
	if isUnique(proposedName) {
		return proposedName
	}

	suffixNumValue := 0
	prefix := strings.TrimRightFunc(proposedName, unicode.IsDigit)
	if prefix != proposedName {
		suffixNumValue, _ = strconv.Atoi(proposedName[len(prefix):])
	}
	prefix = strings.TrimRight(prefix, "-")

	// increase suffix value until we have a unique name
	for {
		suffixNumValue++
		proposedName = fmt.Sprintf("%s%d", prefix, suffixNumValue)
		if isUnique(proposedName) {
			return proposedName
		}
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
