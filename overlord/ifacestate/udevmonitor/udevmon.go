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

package udevmonitor

import (
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/udev/netlink"
)

type Interface interface {
	Connect() error
	Disconnect() error
	Run() error
	Stop() error
}

type DeviceAddedFunc func(device *hotplug.HotplugDeviceInfo)
type DeviceRemovedFunc func(device *hotplug.HotplugDeviceInfo)

// Monitor monitors kernel uevents making it possible to find USB devices.
type Monitor struct {
	tomb          tomb.Tomb
	deviceAdded   DeviceAddedFunc
	deviceRemoved DeviceRemovedFunc
	netlinkConn   *netlink.UEventConn
	// channels used by netlink connection and monitor
	monitorStop         chan struct{}
	netlinkErrors       chan error
	netlinkEvents       chan netlink.UEvent
	startupDevices      map[string]bool
	queuedNetlinkEvents []*netlink.UEvent
}

func New(added DeviceAddedFunc, removed DeviceRemovedFunc) Interface {
	m := &Monitor{
		deviceAdded:   added,
		deviceRemoved: removed,
		netlinkConn:   &netlink.UEventConn{},
	}

	m.netlinkEvents = make(chan netlink.UEvent)
	m.netlinkErrors = make(chan error)
	m.startupDevices = make(map[string]bool)

	return m
}

func (m *Monitor) Connect() error {
	if m.netlinkConn == nil || m.netlinkConn.Fd != 0 {
		// this cannot happen in real code but may happen in tests
		return fmt.Errorf("cannot connect: already connected")
	}

	if err := m.netlinkConn.Connect(netlink.UdevEvent); err != nil {
		return fmt.Errorf("failed to start uevent monitor: %s", err)
	}

	// TODO: consider passing a device filter to reduce noise from irrelevant devices.
	m.monitorStop = m.netlinkConn.Monitor(m.netlinkEvents, m.netlinkErrors, nil)

	return nil
}

func (m *Monitor) Disconnect() error {
	close(m.monitorStop)
	return m.netlinkConn.Close()
}

// Run enumerates existing USB devices and starts a new goroutine that
// handles hotplug events (devices added or removed). It returns immediately.
// The goroutine must be stopped by calling Stop() method.
func (m *Monitor) Run() error {
	m.tomb.Go(func() error {
		existingDevices := make(chan *hotplug.HotplugDeviceInfo)
		udevadmErrors := make(chan error)

		var enumerationFinished bool
		// Gather devices from udevadm info output (enumeration on startup) and from udev event monitor:
		// - devices discovered on startup are reported via existingDevices channel
		// - added/removed devices are reported via netlinkEvents channel
		// Devices reported by netlinkEvents channel are queued until all devices from existingDevices
		// are processed.
		// It might happen that a device is plugged at startup and is reported by both existingDevices
		// channel and netlinkEvents channel; such events are ignored by de-duplication logic based on device path;
		// this de-dup logic is only applied until enumeration is finished.
		if err := hotplug.EnumerateExistingDevices(existingDevices, udevadmErrors); err != nil {
			return fmt.Errorf("cannot enumerate existing devices: %s", err)
		}
		for {
			select {
			case err, ok := <-udevadmErrors:
				if ok {
					logger.Noticef("udevadm error: %q\n", err)
				} else {
					udevadmErrors = nil
				}
			case dev, ok := <-existingDevices:
				if ok && m.deviceAdded != nil {
					m.deviceAdded(dev)
					m.startupDevices[dev.DevicePath()] = true
				}
				if !ok {
					existingDevices = nil
					// enumeration of existing devices has finished, flush queued events
					for _, ev := range m.queuedNetlinkEvents {
						m.udevEvent(ev)
					}
					m.queuedNetlinkEvents = nil
					m.startupDevices = nil
					enumerationFinished = true
				}
			case err := <-m.netlinkErrors:
				logger.Noticef("netlink error: %q\n", err)
			case ev := <-m.netlinkEvents:
				// queue netlink events until enumeration of existing devices finishes
				if !enumerationFinished {
					m.queuedNetlinkEvents = append(m.queuedNetlinkEvents, &ev)
				} else {
					m.udevEvent(&ev)
				}
			case <-m.tomb.Dying():
				m.queuedNetlinkEvents = nil
				return m.Disconnect()
			}
		}
	})

	return nil
}

func (m *Monitor) Stop() error {
	m.tomb.Kill(nil)
	err := m.tomb.Wait()
	m.netlinkConn = nil
	return err
}

func (m *Monitor) udevEvent(ev *netlink.UEvent) {
	switch ev.Action {
	case netlink.ADD:
		m.addDevice(ev.KObj, ev.Env)
	case netlink.REMOVE:
		m.removeDevice(ev.KObj, ev.Env)
	default:
	}
}

func (m *Monitor) addDevice(kobj string, env map[string]string) {
	di, err := hotplug.NewHotplugDeviceInfo(env)
	if err != nil {
		return
	}
	if m.startupDevices != nil && m.startupDevices[di.DevicePath()] {
		return
	}
	if m.deviceAdded != nil {
		m.deviceAdded(di)
	}
}

func (m *Monitor) removeDevice(kobj string, env map[string]string) {
	di, err := hotplug.NewHotplugDeviceInfo(env)
	if err != nil {
		return
	}
	if m.deviceRemoved != nil {
		m.deviceRemoved(di)
	}
}
