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
	"time"

	"gopkg.in/tomb.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/udev/netlink"
)

type Interface interface {
	Connect() error
	Run() error
	Stop() error
}

type (
	DeviceAddedFunc     func(device *hotplug.HotplugDeviceInfo)
	DeviceRemovedFunc   func(device *hotplug.HotplugDeviceInfo)
	EnumerationDoneFunc func()
)

// Monitor monitors kernel uevents making it possible to find hotpluggable devices.
type Monitor struct {
	tomb            tomb.Tomb
	deviceAdded     DeviceAddedFunc
	deviceRemoved   DeviceRemovedFunc
	enumerationDone func()
	netlinkConn     *netlink.UEventConn
	// channels used by netlink connection and monitor
	monitorStop   func(stopTimeout time.Duration) bool
	netlinkErrors chan error
	netlinkEvents chan netlink.UEvent

	// seen keeps track of all observed devices to know when to
	// ignore a spurious event (in case it happens, e.g. when
	// device gets reported by both the enumeration and monitor on
	// startup).  the keys are based on device paths which are
	// guaranteed to be unique and stable till device gets
	// removed.  the lookup is not persisted and gets populated
	// and updated in response to enumeration and hotplug events.
	seen map[string]bool
}

func New(added DeviceAddedFunc, removed DeviceRemovedFunc, enumerationDone EnumerationDoneFunc) Interface {
	m := &Monitor{
		deviceAdded:     added,
		deviceRemoved:   removed,
		enumerationDone: enumerationDone,
		netlinkConn:     &netlink.UEventConn{},
		seen:            make(map[string]bool),
	}

	m.netlinkEvents = make(chan netlink.UEvent)
	m.netlinkErrors = make(chan error)

	return m
}

func (m *Monitor) EventsChannel() chan netlink.UEvent {
	return m.netlinkEvents
}

func (m *Monitor) Connect() error {
	if m.netlinkConn == nil || m.netlinkConn.Fd != 0 {
		// this cannot happen in real code but may happen in tests
		return fmt.Errorf("cannot connect: already connected")
	}
	mylog.Check(m.netlinkConn.Connect(netlink.UdevEvent))

	var filter netlink.Matcher
	// TODO: extend with other criteria based on the hotplug interfaces
	filter = &netlink.RuleDefinitions{
		Rules: []netlink.RuleDefinition{
			{Env: map[string]string{"SUBSYSTEM": "net"}},
			{Env: map[string]string{"SUBSYSTEM": "tty"}},
			{Env: map[string]string{"SUBSYSTEM": "usb"}},
		},
	}

	m.monitorStop = m.netlinkConn.Monitor(m.netlinkEvents, m.netlinkErrors, filter)

	return nil
}

func (m *Monitor) disconnect() error {
	if m.monitorStop != nil {
		if ok := m.monitorStop(5 * time.Second); !ok {
			logger.Noticef("udev monitor stopping timed out")
		}
	}
	return m.netlinkConn.Close()
}

// Run enumerates existing USB devices and starts a new goroutine that
// handles hotplug events (devices added or removed). It returns immediately.
// The goroutine must be stopped by calling Stop() method.
func (m *Monitor) Run() error {
	// Gather devices from udevadm info output (enumeration on startup).
	devices, parseErrors := mylog.Check3(hotplug.EnumerateExistingDevices())

	m.tomb.Go(func() error {
		for _, perr := range parseErrors {
			logger.Noticef("udev enumeration error: %s", perr)
		}
		for _, dev := range devices {
			devPath := dev.DevicePath()
			if m.seen[devPath] {
				continue
			}
			m.seen[devPath] = true
			if m.deviceAdded != nil {
				m.deviceAdded(dev)
			}
		}
		if m.enumerationDone != nil {
			m.enumerationDone()
		}

		// Process hotplug events reported by udev monitor.
		for {
			select {
			case err := <-m.netlinkErrors:
				logger.Noticef("udev event error: %s", err)
			case ev := <-m.netlinkEvents:
				m.udevEvent(&ev)
			case <-m.tomb.Dying():
				return m.disconnect()
			}
		}
	})

	return nil
}

func (m *Monitor) Stop() error {
	m.tomb.Kill(nil)
	mylog.Check(m.tomb.Wait())
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
	dev := mylog.Check2(hotplug.NewHotplugDeviceInfo(env))

	devPath := dev.DevicePath()
	if m.seen[devPath] {
		return
	}
	m.seen[devPath] = true
	if m.deviceAdded != nil {
		m.deviceAdded(dev)
	}
}

func (m *Monitor) removeDevice(kobj string, env map[string]string) {
	dev := mylog.Check2(hotplug.NewHotplugDeviceInfo(env))

	devPath := dev.DevicePath()
	if !m.seen[devPath] {
		logger.Debugf("udev monitor observed remove event for unknown device %s", dev)
		return
	}
	delete(m.seen, devPath)
	if m.deviceRemoved != nil {
		m.deviceRemoved(dev)
	}
}
