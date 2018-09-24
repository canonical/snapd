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
	"fmt"
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
	if keyhandler, ok := iface.(hotplug.HotplugDeviceKeyHandler); ok {
		deviceKey, err = keyhandler.HotplugDeviceKey(device)
		if err != nil {
			return "", fmt.Errorf("failed to create device key for interface %q: %s", iface.Name(), err)
		}
		if deviceKey != "" {
			return deviceKey, nil
		}
	}
	return defaultDeviceKey, nil
}

func hotplugTaskSetAttrs(task *state.Task, deviceKey, ifaceName string) {
	task.Set("device-key", deviceKey)
	task.Set("interface", ifaceName)
}

func hotplugTaskGetAttrs(task *state.Task) (deviceKey, ifaceName string, err error) {
	if err = task.Get("interface", &ifaceName); err != nil {
		return "", "", fmt.Errorf("internal error: failed to get interface name: %s", err)
	}
	if err = task.Get("device-key", &deviceKey); err != nil {
		return "", "", fmt.Errorf("internal error: failed to get device key: %s", err)
	}
	return deviceKey, ifaceName, err
}

// List of attributes that determine the computation of default device key.
// Attributes are grouped by similarity, the first non-empty attribute within the group goes into the key.
// The final key is composed of 4 attributes (some of which may be empty), separated by "/".
var attrGroups = [][]string{
	// Name
	{"ID_V4L_PRODUCT", "NAME", "ID_NET_NAME", "PCI_SLOT_NAME"},
	// Vendor
	{"ID_VENDOR_ID", "ID_VENDOR", "ID_WWN", "ID_WWN_WITH_EXTENSION", "ID_VENDOR_FROM_DATABASE", "ID_VENDOR_ENC", "ID_OUI_FROM_DATABASE"},
	// Model
	{"ID_MODEL_ID", "ID_MODEL_ENC"},
	// Identifier
	{"ID_SERIAL", "ID_SERIAL_SHORT", "ID_NET_NAME_MAC", "ID_REVISION"},
}

// defaultDeviceKey computes device key from the attributes of
// HotplugDeviceInfo. Empty string is returned if too few attributes are present
// to compute a good key. Attributes used to compute device key are defined in
// attrGroups list above.
func defaultDeviceKey(devinfo *hotplug.HotplugDeviceInfo) string {
	key := make([]string, len(attrGroups))
	found := 0
	for i, group := range attrGroups {
		for _, attr := range group {
			if val, ok := devinfo.Attribute(attr); ok && val != "" {
				key[i] = val
				found++
				break
			}
		}
	}
	if found < 2 {
		return ""
	}
	return strings.Join(key, "/")
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
	defaultKey := defaultDeviceKey(devinfo)

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
			logger.Debugf("No valid device key provided by interface %s, device %q ignored", iface.Name(), devinfo.DeviceName())
			continue
		}

		if !hotplugFeature {
			logger.Noticef("Hotplug 'add' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.DevicePath(), iface.Name())
			continue
		}

		logger.Debugf("HotplugDeviceAdded: %s (interface: %s, device key: %q, devname %s, subsystem: %s)", devinfo.DevicePath(), iface, key, devinfo.DeviceName(), devinfo.Subsystem())

		stateSlots, err := getHotplugSlots(st)
		if err != nil {
			logger.Noticef(err.Error())
			return
		}

		// add slot to the repo and state based on the slot spec returned by the interface
		attrs := slotSpec.Attrs
		if attrs == nil {
			attrs = make(map[string]interface{})
		}
		label := slotSpec.Label
		if label == "" {
			si := interfaces.StaticInfoOf(iface)
			label = si.Summary
		}

		// Determine slot name:
		// - if a slot for given device key exists in hotplug state, use old name
		// - otherwise use name provided by slot spec, if set
		// - if not, use auto-generated name.
		proposedName := slotSpec.Name
		for _, stateSlot := range stateSlots {
			if stateSlot.HotplugDeviceKey == key {
				proposedName = stateSlot.Name
				break
			}
		}
		if proposedName == "" {
			proposedName = suggestedSlotName(devinfo, iface.Name())
		}
		proposedName = ensureUniqueName(proposedName, func(name string) bool {
			if slot, ok := stateSlots[name]; ok {
				return slot.HotplugDeviceKey == key
			}
			return m.repo.Slot(coreSnapInfo.InstanceName(), name) == nil
		})
		slot := &snap.SlotInfo{
			Name:             proposedName,
			Label:            label,
			Snap:             coreSnapInfo,
			Interface:        iface.Name(),
			Attrs:            attrs,
			HotplugDeviceKey: key,
		}
		if iface, ok := iface.(interfaces.SlotSanitizer); ok {
			if err := iface.BeforePrepareSlot(slot); err != nil {
				logger.Noticef("Failed to sanitize hotplug-created slot %q for interface %s: %s", slot.Name, slot.Interface, err)
				continue
			}
		}

		if err := m.repo.AddSlot(slot); err != nil {
			logger.Noticef("Failed to create slot %q for interface %s", slot.Name, slot.Interface)
			continue
		}
		stateSlots[slot.Name] = hotplugSlotDef{
			Name:             slot.Name,
			Interface:        slot.Interface,
			StaticAttrs:      slot.Attrs,
			HotplugDeviceKey: slot.HotplugDeviceKey,
		}
		setHotplugSlots(st, stateSlots)

		if m.enumeratedDeviceKeys != nil {
			if m.enumeratedDeviceKeys[iface.Name()] == nil {
				m.enumeratedDeviceKeys[iface.Name()] = make(map[string]bool)
			}
			m.enumeratedDeviceKeys[iface.Name()][key] = true
		}

		logger.Noticef("Added hotplug slot %s:%s of interface %s for device key %q", slot.Snap.InstanceName(), slot.Name, slot.Interface, key)

		chg := st.NewChange(fmt.Sprintf("hotplug-connect-%s", iface), fmt.Sprintf("Connect hotplug slot of interface %s", iface))
		hotplugConnect := st.NewTask("hotplug-connect", fmt.Sprintf("Recreate connections of device %q", key))
		hotplugTaskSetAttrs(hotplugConnect, key, iface.Name())
		chg.AddTask(hotplugConnect)
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

	defaultKey := defaultDeviceKey(devinfo)
	for _, iface := range m.repo.AllHotplugInterfaces() {
		// determine device key for the interface; note that interface might provide own device keys.
		key, err := deviceKey(defaultKey, devinfo, iface)
		if err != nil {
			logger.Debugf(err.Error())
			continue
		}

		if key == "" {
			continue
		}

		hasSlots, err := m.repo.HasHotplugSlot(key, iface.Name())
		if err != nil {
			logger.Noticef(err.Error())
			continue
		}
		if !hasSlots {
			continue
		}

		if !hotplugFeature {
			logger.Noticef("Hotplug 'remove' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.DevicePath(), iface.Name())
			continue
		}

		logger.Debugf("HotplugDeviceRemoved: %s (interface: %s, device key: %q, devname %s, subsystem: %s)", devinfo.DevicePath(), iface, key, devinfo.DeviceName(), devinfo.Subsystem())

		chg := st.NewChange(fmt.Sprintf("hotplug-remove-%s", iface), fmt.Sprintf("Remove hotplug connections and slots of interface %s", iface))
		ts := removeDevice(st, key, iface.Name())
		chg.AddAll(ts)
	}
	st.EnsureBefore(0)
}

// create tasks to disconnect slots of given device and remove affected slots.
func removeDevice(st *state.State, deviceKey, ifaceName string) *state.TaskSet {
	// hotplug-disconnect task will create hooks and disconnect the slot
	hotplugDisconnect := st.NewTask("hotplug-disconnect", fmt.Sprintf("Disable connections of device %q", deviceKey))
	hotplugTaskSetAttrs(hotplugDisconnect, deviceKey, ifaceName)

	// hotplug-remove-slot will remove this device's slot from the repository.
	removeSlot := st.NewTask("hotplug-remove-slot", fmt.Sprintf("Remove slot for device %q, interface %q", deviceKey, ifaceName))
	hotplugTaskSetAttrs(removeSlot, deviceKey, ifaceName)
	removeSlot.WaitFor(hotplugDisconnect)

	return state.NewTaskSet(hotplugDisconnect, removeSlot)
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
			if byIface[slot.HotplugDeviceKey] {
				continue
			}
		}
		// device not present, disconnect its slots and remove them (as if it was unplugged)
		chg := st.NewChange(fmt.Sprintf("hotplug-remove-%s", slot.Interface), fmt.Sprintf("Remove hotplug connections and slots of interface %s", slot.Interface))
		ts := removeDevice(st, slot.HotplugDeviceKey, slot.Interface)
		chg.AddAll(ts)
	}
	st.EnsureBefore(0)

	// the map of enumeratedDeviceKeys is not needed anymore
	m.enumeratedDeviceKeys = nil
}

func findConnsForDeviceKey(conns *map[string]connState, coreSnapName, ifaceName, deviceKey string) []string {
	var connsForDevice []string
	for id, connSt := range *conns {
		if connSt.Interface != ifaceName || connSt.HotplugDeviceKey != deviceKey {
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

type hotplugSlotDef struct {
	Name             string                 `json:"name"`
	Interface        string                 `json:"interface"`
	StaticAttrs      map[string]interface{} `json:"static-attrs,omitempty"`
	HotplugDeviceKey string                 `json:"device-key"`
}

func getHotplugSlots(st *state.State) (map[string]hotplugSlotDef, error) {
	var slots map[string]hotplugSlotDef
	err := st.Get("hotplug-slots", &slots)
	if err != nil {
		if err != state.ErrNoState {
			return nil, err
		}
		slots = make(map[string]hotplugSlotDef)
	}
	return slots, nil
}

func setHotplugSlots(st *state.State, slots map[string]hotplugSlotDef) {
	st.Set("hotplug-slots", slots)
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
	var charCount int
	// the dash flag is used to prevent consecutive dashes, and the dash in the front
	dash := true
Loop:
	for _, c := range s {
		switch {
		case c == '-' && !dash:
			dash = true
			out = append(out, '-')
		case unicode.IsLetter(c):
			out = append(out, unicode.ToLower(c))
			dash = false
			charCount++
			if charCount >= maxGenerateSlotNameLen {
				break Loop
			}
		case unicode.IsDigit(c) && charCount > 0:
			out = append(out, c)
			dash = false
			charCount++
			if charCount >= maxGenerateSlotNameLen {
				break Loop
			}
		default:
			// any other character is ignored
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
	var candidates []string
	for _, attr := range nameAttrs {
		name, ok := devinfo.Attribute(attr)
		if ok {
			name = makeSlotName(name)
			if name != "" {
				candidates = append(candidates, name)
			}
		}
	}
	if len(candidates) == 0 {
		return fallbackName
	}
	shortestName := candidates[0]
	for _, cand := range candidates[1:] {
		if len(cand) < len(shortestName) {
			shortestName = cand
		}
	}
	return shortestName
}
