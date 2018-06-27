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

	defaultDeviceKey := fmt.Sprintf("%s:%s:%s", devinfo.IdVendor(), devinfo.IdProduct(), devinfo.Serial())

	// Iterate over all hotplug interfaces
	for _, iface := range m.repo.AllHotplugInterfaces() {
		hotplugHandler := iface.(hotplug.HotplugDeviceHandler)
		key, err := deviceKey(defaultDeviceKey, devinfo, iface)
		if err != nil {
			logger.Debugf(err.Error())
			continue
		}
		if key == "" {
			continue
		}
		spec, err := hotplug.NewSpecification(key)
		if err != nil {
			logger.Debugf("Failed to create HotplugSpec for device key %q: %s", key, err)
			continue
		}
		if hotplugHandler.HotplugDeviceDetected(devinfo, spec) != nil {
			logger.Debugf("Failed to process hotplug event by the rule of interface %q: %s", iface.Name(), err)
			continue
		}

		slotSpecs := spec.Slots()
		if len(slotSpecs) == 0 {
			continue
		}

		if coreSnapInfo == nil {
			coreSnapInfo, err = snapstate.CurrentInfo(m.state, coreSnapName)
			if err != nil {
				logger.Noticef("%q snap not available, hotplug event ignored", coreSnapName)
				return
			}
		}

		var connsToRecreate []*interfaces.ConnRef

		// find old connections for slots of this device
		for id, connSt := range conns {
			if connSt.Interface != iface.Name() || connSt.HotplugDeviceKey != key {
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
			// we found an old connection
			connsToRecreate = append(connsToRecreate, connRef)
		}

		if !m.hotplug {
			logger.Debugf("Hotplug 'add' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.Path(), iface.Name())
			continue
		}

		slots := make(map[string]*hotplug.SlotSpec, len(slotSpecs))

		// add slots to the repo
		for _, ss := range slotSpecs {
			slot := &snap.SlotInfo{
				Name:             ss.Name,
				Snap:             coreSnapInfo,
				Interface:        iface.Name(),
				HotplugDeviceKey: key,
			}
			if err := m.repo.AddSlot(slot); err != nil {
				logger.Noticef("Failed to create slot %q for interface %q", slot.Name, slot.Interface)
				continue
			}
			slots[ss.Name] = &ss
			logger.Debugf("Added hotplug slot %q (%s) for device key %q", slot.Name, slot.Interface, key)
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

	defaultDeviceKey := fmt.Sprintf("%s:%s:%s", devinfo.IdVendor(), devinfo.IdProduct(), devinfo.Serial())

	for _, iface := range m.repo.AllHotplugInterfaces() {
		key, err := deviceKey(defaultDeviceKey, devinfo, iface)
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
			logger.Debugf("Hotplug 'remove' event for device %q (interface %q) ignored, enable experimental.hotplug", devinfo.Path(), iface.Name())
			continue
		}
	}
}

func (m *InterfaceManager) hotplugEnabled() (bool, error) {
	tr := config.NewTransaction(m.state)
	var featureFlagHotplug bool
	if err := tr.GetMaybe("core", "experimental.hotplug", &featureFlagHotplug); err != nil {
		return false, err
	}
	return featureFlagHotplug, nil
}
