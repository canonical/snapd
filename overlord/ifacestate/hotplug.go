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

func validateDeviceKey(key string) bool {
	if key == "" || key == ":::" {
		return false
	}
	return true
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

func defaultDeviceKey(devinfo *hotplug.HotplugDeviceInfo) string {
	vendor, _ := devinfo.Attribute("ID_VENDOR_ID")
	model, _ := devinfo.Attribute("ID_MODEL_ID")
	rev, _ := devinfo.Attribute("ID_REVISION")
	serial, _ := devinfo.Attribute("ID_SERIAL_SHORT")
	return fmt.Sprintf("%s:%s:%s:%s", vendor, model, rev, serial)
}

// HotplugDeviceAdded gets called when a device is added to the system.
func (m *InterfaceManager) hotplugDeviceAdded(devinfo *hotplug.HotplugDeviceInfo) {
	st := m.state
	st.Lock()
	defer st.Unlock()

	// FIXME: agreement needed how to find about system snap and where to attach interfaces.
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

		if !validateDeviceKey(key) {
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
		slot := &snap.SlotInfo{
			Name:             slotSpec.Name,
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
		logger.Noticef("Added hotplug slot %s:%s of interface %s for device key %q", slot.Snap.InstanceName(), slot.Name, slot.Interface, key)

		setHotplugSlots(st, stateSlots)

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

		if !validateDeviceKey(key) {
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

		// create tasks to disconnect given interface
		chg := st.NewChange(fmt.Sprintf("hotplug-remove-%s", iface), fmt.Sprintf("Remove hotplug connections and slots of interface %s", iface))

		// hotplug-disconnect task will create hooks and disconnect the slot
		hotplugRemove := st.NewTask("hotplug-disconnect", fmt.Sprintf("Disable connections of device %q", key))
		hotplugTaskSetAttrs(hotplugRemove, key, iface.Name())
		chg.AddTask(hotplugRemove)

		// hotplug-remove-slot will remove this device's slot from the repository.
		removeSlot := st.NewTask("hotplug-remove-slot", fmt.Sprintf("Remove slot for device %q, interface %q", key, iface))
		hotplugTaskSetAttrs(removeSlot, key, iface.Name())
		removeSlot.WaitFor(hotplugRemove)
		chg.AddTask(removeSlot)
		st.EnsureBefore(0)
	}
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
