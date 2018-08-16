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
	"github.com/snapcore/snapd/snap"
)

func deviceKey(defaultDeviceKey string, devinfo *hotplug.HotplugDeviceInfo, iface interfaces.Interface) (deviceKey string, err error) {
	if keyhandler, ok := iface.(hotplug.HotplugDeviceKeyHandler); ok {
		deviceKey, err = keyhandler.HotplugDeviceKey(devinfo)
		if err != nil {
			return "", fmt.Errorf("failed to create device key for interface %q: %s", iface.Name(), err)
		}
		return deviceKey, nil
	}
	return defaultDeviceKey, nil
}

func validateDeviceKey(key string) bool {
	if key == "" || key == ":::" {
		return false
	}
	return true
}

func defaultDeviceKey(devinfo *hotplug.HotplugDeviceInfo) string {
	vendor, _ := devinfo.Attribute("ID_VENDOR_ID")
	model, _ := devinfo.Attribute("ID_MODEL_ID")
	rev, _ := devinfo.Attribute("ID_REVISION")
	serial, _ := devinfo.Attribute("ID_SERIAL_SHORT")
	return fmt.Sprintf("%s:%s:%s:%s", vendor, model, rev, serial)
}

func (m *InterfaceManager) HotplugDeviceAdded(devinfo *hotplug.HotplugDeviceInfo) {
	st := m.state
	st.Lock()
	defer st.Unlock()

	const coreSnapName = "core"
	coreSnapInfo, err := snapstate.CurrentInfo(st, coreSnapName)
	if err != nil {
		logger.Noticef("%q snap not available, hotplug events ignored", coreSnapName)
		return
	}

	hotplugIfaces := m.repo.AllHotplugInterfaces()
	defaultKey := defaultDeviceKey(devinfo)
	logger.Debugf("HotplugDeviceAdded: %s (default key: %q, devname %s, subsystem: %s)", devinfo.DevicePath(), defaultKey, devinfo.DeviceName(), devinfo.Subsystem())

	// iterate over all hotplug interfaces
	for _, iface := range hotplugIfaces {
		hotplugHandler := iface.(hotplug.Definer)
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
		slotSpecs := spec.Slots()
		if len(slotSpecs) == 0 {
			continue
		}

		if !validateDeviceKey(key) {
			logger.Debugf("No valid device key provided by interface %s, device %q ignored", iface.Name(), devinfo.DeviceName())
			continue
		}

		if !m.hotplug {
			logger.Noticef("Hotplug 'add' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.DevicePath(), iface.Name())
			continue
		}

		slots := make(map[string]*hotplug.SlotSpec, len(slotSpecs))

		// add slots to the repo based on the slot specs returned by the interface
		for _, ss := range slotSpecs {
			slot := &snap.SlotInfo{
				Name:             ss.Name,
				Snap:             coreSnapInfo,
				Interface:        iface.Name(),
				Attrs:            ss.Attrs,
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
			slots[ss.Name] = ss
			logger.Noticef("Added hotplug slot %q (%s) for device key %q", slot.Name, slot.Interface, key)
		}

		chg := st.NewChange(fmt.Sprintf("hotplug-connect-%s", iface), fmt.Sprintf("Connect hotplug slots of interface %s", iface))
		hotplugConnect := st.NewTask("hotplug-connect", fmt.Sprintf("Re-establish connections of device $%q", key))
		hotplugConnect.Set("device-key", key)
		hotplugConnect.Set("interface", iface)
		chg.AddTask(hotplugConnect)
	}
}

func (m *InterfaceManager) HotplugDeviceRemoved(devinfo *hotplug.HotplugDeviceInfo) {
	st := m.state
	st.Lock()
	defer st.Unlock()

	defaultKey := defaultDeviceKey(devinfo)
	for _, iface := range m.repo.AllHotplugInterfaces() {
		key, err := deviceKey(defaultKey, devinfo, iface)
		if err != nil {
			logger.Debugf(err.Error())
			continue
		}

		if !validateDeviceKey(key) {
			continue
		}

		if !m.hotplug {
			logger.Noticef("Hotplug 'remove' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.DevicePath(), iface.Name())
			continue
		}

		chg := st.NewChange(fmt.Sprintf("hotplug-remove-%s", iface), fmt.Sprintf("Remove hotplug slots of interface %s", iface))
		hotplugRemove := st.NewTask("hotplug-disconnect", fmt.Sprintf("Disable connections of device $%q", key))
		hotplugRemove.Set("interface", iface)
		hotplugRemove.Set("device-key", key)
		chg.AddTask(hotplugRemove)
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
	var featureFlagHotplug bool
	if err := tr.GetMaybe("core", "experimental.hotplug", &featureFlagHotplug); err != nil {
		return false, err
	}
	return featureFlagHotplug, nil
}
