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

	"github.com/snapcore/snapd/osutil/udev/netlink"

	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/logger"
)

var CreateUDevMonitor = New

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
	monitorStop   chan struct{}
	netlinkErrors chan error
	netlinkEvents chan netlink.UEvent
}

func New(added DeviceAddedFunc, removed DeviceRemovedFunc) Interface {
	m := &Monitor{
		deviceAdded:   added,
		deviceRemoved: removed,
		netlinkConn:   &netlink.UEventConn{},
	}

	m.netlinkEvents = make(chan netlink.UEvent)
	m.netlinkErrors = make(chan error)

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
		for {
			select {
			case err := <-m.netlinkErrors:
				logger.Noticef("netlink error: %q\n", err)
			case ev := <-m.netlinkEvents:
				m.udevEvent(&ev)
			case <-m.tomb.Dying():
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
