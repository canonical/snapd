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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func deviceKey(defaultDeviceKey string, devinfo *interfaces.HotplugDeviceInfo, iface interfaces.Interface) (deviceKey string, err error) {
	if keyhandler, ok := iface.(interfaces.HotplugDeviceKeyHandler); ok {
		deviceKey, err = keyhandler.HotplugDeviceKey(devinfo)
		if err != nil {
			return "", fmt.Errorf("failed to create device key for interface %q: %s", iface.Name(), err)
		}
		return deviceKey, nil
	}
	return defaultDeviceKey, nil
}

func (m *InterfaceManager) HotplugDeviceAdded(devinfo *interfaces.HotplugDeviceInfo) {
	const coreSnapName = "core"
	var coreSnapInfo *snap.Info

	defaultDeviceKey := fmt.Sprintf("%s:%s:%s", devinfo.IdVendor(), devinfo.IdProduct(), devinfo.Serial())

	for _, iface := range m.repo.AllInterfaces() {
		key, err := deviceKey(defaultDeviceKey, devinfo, iface)
		if err != nil {
			logger.Debugf(err.Error())
			continue
		}
		if key == "" {
			continue
		}
		if hotplugHandler, ok := iface.(interfaces.HotplugDeviceHandler); ok {
			spec, err := interfaces.NewHotplugSpec(key)
			if err != nil {
				logger.Debugf("Failed to create HotplugSpec for device key %q: %s", key, err)
				continue
			}
			if hotplugHandler.HotplugDeviceAdd(devinfo, spec) != nil {
				logger.Debugf("Failed to process hotplug event by the rule of interface %q: %s", iface.Name(), err)
				continue
			}

			slotSpecs := spec.Slots()
			if len(slotSpecs) == 0 {
				continue
			}

			devRecord, err := m.getHotplugDeviceRecord(key, iface.Name())
			if err != nil {
				logger.Noticef("Failed to get hotplug device record for device key %q, interface %q: %s", key, iface.Name(), err)
				// there is no point in continuing if we hit an error here
				return
			}

			if coreSnapInfo == nil {
				coreSnapInfo, err = snapstate.CurrentInfo(m.state, coreSnapName)
				if err != nil {
					logger.Noticef("%q snap not available, hotplug event ignored", coreSnapName)
					return
				}
			}

			// if we see this device for the first time, just create the Slot.
			if devRecord == nil {
				for _, ss := range slotSpecs {
					slot := &snap.SlotInfo{
						Name:      ss.Name,
						Snap:      coreSnapInfo,
						Interface: iface.Name(),
						Attrs:     ss.Attrs,
					}

					if err := m.repo.AddSlot(slot); err != nil {
						logger.Noticef("Failed to create slot %q for interface %q", slot.Name, slot.Interface)
					}
				}
				// TODO: trigger auto-connects where appropriate
			} else {
				// if we know this device already, recreate the slot and re-connect if needed
				m.state.Lock()
				defer m.state.Unlock()
				conns, err := getConns(m.state)
				if err != nil {
					logger.Noticef("Failed to read connections data: %s", err)
					return
				}

				// iterate over conns in state to find old/affected 'core' connections;
				// note, we cannot ask repo because we have to deal with hotplug-removed slots
				// that don't exist in the repo yet.
				var connsToRecreate []*interfaces.ConnRef
				for id, connSt := range conns {
					if connSt.Interface != iface.Name() {
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
					connsToRecreate = append(connsToRecreate, connRef)

				}
				for _, slotName := range devRecord.slots {
				}
			}
		}
	}
}

func (m *InterfaceManager) HotplugDeviceRemoved(devinfo *interfaces.HotplugDeviceInfo) {
	m.state.Lock()
	defer m.state.Unlock()

	var hotplugData map[string]map[string]hotPlugDeviceRecord
	err := m.state.Get("hotplug", &hotplugData)
	if err != nil {
		if err == state.ErrNoState {
			return
		}
		logger.Noticef("Failed to get hotplug device records: %s", err)
		return
	}

	defaultDeviceKey := fmt.Sprintf("%s:%s:%s", devinfo.IdVendor(), devinfo.IdProduct(), devinfo.Serial())

	for _, iface := range m.repo.AllInterfaces() {
		key, err := deviceKey(defaultDeviceKey, devinfo, iface)
		if err != nil {
			logger.Debugf(err.Error())
			continue
		}
		if key == "" {
			continue
		}

		if bykeydata, ok := hotplugData[key]; ok {
			if _, ok2 := bykeydata[iface.Name()]; ok2 {
				// TODO: disconnect all slots
				/*for _, slot := range rec.slots {
					// TODO
				}*/
			}
		}
	}
}

type hotPlugDeviceRecord struct {
	slots []string
}

func (m *InterfaceManager) getHotplugDeviceRecord(deviceKey, ifaceName string) (*hotPlugDeviceRecord, error) {
	m.state.Lock()
	defer m.state.Unlock()

	var hotplugData map[string]map[string]hotPlugDeviceRecord
	err := m.state.Get("hotplug", &hotplugData)
	if err != nil {
		if err != state.ErrNoState {
			return nil, err
		}
		return nil, nil
	}

	if hp, ok := hotplugData[deviceKey]; ok {
		if ifdata, ok2 := hp[ifaceName]; ok2 {
			return &ifdata, nil
		}
	}
	return nil, nil
}
