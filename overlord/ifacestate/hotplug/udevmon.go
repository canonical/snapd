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

package hotplug

import (
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/osutil/udev/netlink"

	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/logger"
)

var CreateUDevMonitor = newUDevMonitor

type UDevMon interface {
	Connect() error
	Disconnect() error
	Run() error
	Stop() error
}

type DeviceAddedCallback func(device *hotplug.HotplugDeviceInfo)
type DeviceRemovedCallback func(device *hotplug.HotplugDeviceInfo)

// UDevMonitor monitors kernel uevents making it possible to find USB devices.
type UDevMonitor struct {
	tmb             *tomb.Tomb
	deviceAddedCb   DeviceAddedCallback
	deviceRemovedCb DeviceRemovedCallback
	netlinkConn     *netlink.UEventConn
	// channels used by netlink connection and monitor
	monitorStop   chan struct{}
	netlinkErrors chan error
	netlinkEvents chan netlink.UEvent
}

func newUDevMonitor(addedCb DeviceAddedCallback, removedCb DeviceRemovedCallback) UDevMon {
	m := &UDevMonitor{
		deviceAddedCb:   addedCb,
		deviceRemovedCb: removedCb,
		netlinkConn:     &netlink.UEventConn{},
		tmb:             new(tomb.Tomb),
	}

	m.netlinkEvents = make(chan netlink.UEvent)
	m.netlinkErrors = make(chan error)

	return m
}

func (m *UDevMonitor) Connect() error {
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

func (m *UDevMonitor) Disconnect() error {
	close(m.monitorStop)
	return m.netlinkConn.Close()
}

// Run enumerates existing USB devices and starts a new goroutine that
// handles hotplug events (devices added or removed). It returns immediately.
// The goroutine must be stopped by calling Stop() method.
func (m *UDevMonitor) Run() error {
	m.tmb.Go(func() error {
		for {
			select {
			case err := <-m.netlinkErrors:
				logger.Noticef("netlink error: %q\n", err)
			case ev := <-m.netlinkEvents:
				m.udevEvent(&ev)
			case <-m.tmb.Dying():
				close(m.monitorStop)
				return m.netlinkConn.Close()
			}
		}
	})

	return nil
}

func (m *UDevMonitor) Stop() error {
	m.tmb.Kill(nil)
	err := m.tmb.Wait()
	m.netlinkConn = nil
	return err
}

func (m *UDevMonitor) udevEvent(ev *netlink.UEvent) {
	switch ev.Action {
	case netlink.ADD:
		m.addDevice(ev.KObj, ev.Env)
	case netlink.REMOVE:
		m.removeDevice(ev.KObj, ev.Env)
	default:
	}
}

func (m *UDevMonitor) addDevice(kobj string, env map[string]string) {
	di, err := hotplug.NewHotplugDeviceInfo(env)
	if err != nil {
		return
	}
	if m.deviceAddedCb != nil {
		m.deviceAddedCb(di)
	}
}

func (m *UDevMonitor) removeDevice(kobj string, env map[string]string) {
	di, err := hotplug.NewHotplugDeviceInfo(env)
	if err != nil {
		return
	}
	if m.deviceRemovedCb != nil {
		m.deviceRemovedCb(di)
	}
}

type newMonitorFn func(DeviceAddedCallback, DeviceRemovedCallback) UDevMon

func MockCreateUDevMonitor(new newMonitorFn) (restore func()) {
	CreateUDevMonitor = new
	return func() {
		CreateUDevMonitor = newUDevMonitor
	}
}

type UDevMonMock struct{}

func (u *UDevMonMock) Connect() error    { return nil }
func (u *UDevMonMock) Disconnect() error { return nil }
func (u *UDevMonMock) Run() error        { return nil }
func (u *UDevMonMock) Stop() error       { return nil }
