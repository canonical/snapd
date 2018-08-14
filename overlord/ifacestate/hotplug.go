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

func defaultDeviceKey(devinfo *hotplug.HotplugDeviceInfo) string {
	vendor, _ := devinfo.Attribute("ID_VENDOR_ID")
	product, _ := devinfo.Attribute("ID_MODEL_ID")
	serial, _ := devinfo.Attribute("ID_SERIAL_SHORT")
	return fmt.Sprintf("%s:%s:%s", vendor, product, serial)
}

func (m *InterfaceManager) HotplugDeviceAdded(devinfo *hotplug.HotplugDeviceInfo) {
	const coreSnapName = "core"
	var coreSnapInfo *snap.Info

	m.state.Lock()
	defer m.state.Unlock()

	conns, err := getConns(m.state)
	if err != nil {
		logger.Noticef("Failed to read connections data: %s", err)
		return
	}

	hotplugIfaces := m.repo.AllHotplugInterfaces()
	defaultKey := defaultDeviceKey(devinfo)
	logger.Debugf("HotplugDeviceAdded: %s (default key: %q, devname %s, subsystem: %s)", devinfo.DevicePath(), defaultKey, devinfo.DeviceName(), devinfo.Subsystem())

	// Iterate over all hotplug interfaces
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

		if key == "" || key == "::" {
			logger.Debugf("No valid device key provided by interface %s, device %q ignored", iface.Name(), devinfo.DeviceName())
			continue
		}

		if coreSnapInfo == nil {
			coreSnapInfo, err = snapstate.CurrentInfo(m.state, coreSnapName)
			if err != nil {
				logger.Noticef("%q snap not available, hotplug event ignored", coreSnapName)
				return
			}
		}

		if !m.hotplug {
			logger.Noticef("Hotplug 'add' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.DevicePath(), iface.Name())
			continue
		}

		// find old connections for slots of this device - note can't ask the repository since we need
		// to recreate old connections that are only remembered in the state.
		connsToRecreate := findConnsForDeviceKey(&conns, coreSnapName, iface.Name(), key)

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

		// we see this device for the first time (or it didn't have any connected slots before)
		if len(connsToRecreate) == 0 {
			// TODO: trigger auto-connects where appropriate
			continue
		}

		// in typical case given interface creates exactly one slot, so we just re-create old connection
		// regardless of slot name
		if len(connsToRecreate) == 1 {
			// TODO: create connect or autoconnect task to recreate the connection
		} else {
			// in rare cases given interface may create multiple slots - we identify (match) old slots by their names
			for _, oldConn := range connsToRecreate {
				if _, ok := slots[oldConn.SlotRef.Name]; ok {
					// TODO: create connect or autoconnect task to recreate the connection
				}
			}
		}
	}
}

func (m *InterfaceManager) HotplugDeviceRemoved(devinfo *hotplug.HotplugDeviceInfo) {
	m.state.Lock()
	defer m.state.Unlock()

	defaultKey := defaultDeviceKey(devinfo)

	for _, iface := range m.repo.AllHotplugInterfaces() {
		key, err := deviceKey(defaultKey, devinfo, iface)
		if err != nil {
			logger.Debugf(err.Error())
			continue
		}
		if key == "" {
			continue
		}

		// TODO: remove slot, disconnect if connected; mark disconnect as triggered by hotplug event so that the connection
		// is maintained in connState.

		if !m.hotplug {
			logger.Noticef("Hotplug 'remove' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.DevicePath(), iface.Name())
			continue
		}
	}
}

func findConnsForDeviceKey(conns *map[string]connState, coreSnapName, ifaceName, deviceKey string) []*interfaces.ConnRef {
	var connsForDevice []*interfaces.ConnRef
	for id, connSt := range *conns {
		if connSt.Interface != ifaceName || connSt.HotplugDeviceKey != deviceKey {
			continue
		}
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			logger.Noticef("Failed to parse connection id %q: %s", id, err)
			continue
		}
		if connRef.SlotRef.Snap != coreSnapName {
			continue
		}
		// we found a connection
		connsForDevice = append(connsForDevice, connRef)
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
