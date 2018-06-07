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

package overlord

import (
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/pilebones/go-udev/crawler"
	"github.com/pilebones/go-udev/netlink"

	"github.com/snapcore/snapd/logger"
)

type UDevMon interface {
	Connect() error
	Run() error
	Stop() error
}

// UDevMonitor monitors kernel uevents making it possible to find USB devices.
// TODO: document new tasks.
type UDevMonitor struct {
	tmb         *tomb.Tomb
	netlinkConn *netlink.UEventConn
	// channels used by netlink connection and monitor
	monitorStop    chan struct{}
	crawlerStop    chan struct{}
	crawlerDevices chan crawler.Device
	crawlerErrors  chan error
	netlinkErrors  chan error
	netlinkEvents  chan netlink.UEvent
}

func NewUDevMonitor() UDevMon {
	m := &UDevMonitor{
		netlinkConn: &netlink.UEventConn{},
		tmb:         new(tomb.Tomb),
	}
	return m
}

func (m *UDevMonitor) Connect() error {
	if m.netlinkConn == nil || m.netlinkConn.Fd != 0 {
		// this cannot happen in real code but may happen in tests
		panic("cannot run UDevMonitor more than once")
	}

	m.crawlerErrors = make(chan error)
	m.crawlerDevices = make(chan crawler.Device)

	if err := m.netlinkConn.Connect(); err != nil {
		return fmt.Errorf("failed to start uevent monitor: %s", err)
	}

	m.netlinkEvents = make(chan netlink.UEvent)
	m.netlinkErrors = make(chan error)

	// TODO: pass filters
	m.monitorStop = m.netlinkConn.Monitor(m.netlinkEvents, m.netlinkErrors, nil)

	// TODO: pass filters
	m.crawlerStop = crawler.ExistingDevices(m.crawlerDevices, m.crawlerErrors, nil)

	return nil
}

// Run enumerates existing USB devices and starts a new goroutine that
// handles hotplug events (devices added or removed). It returns immediately.
// The goroutine must be stopped by calling Stop() method.
func (m *UDevMonitor) Run() error {
	m.tmb.Go(func() error {
		for {
			select {
			case dv := <-m.crawlerDevices:
				m.addDevice(dv.KObj, dv.Env)
			case err := <-m.crawlerErrors:
				logger.Noticef("error enumerating devices: %q\n", err)
			case err := <-m.netlinkErrors:
				logger.Noticef("netlink error: %q\n", err)
			case ev := <-m.netlinkEvents:
				m.udevEvent(&ev)
			case <-m.tmb.Dying():
				close(m.monitorStop)
				close(m.crawlerStop)
				m.netlinkConn.Close()
				return nil
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
	// TODO: handle device added (just plugged, or discovered on startup) by creating "hotplug-add" task
}

func (m *UDevMonitor) removeDevice(kobj string, env map[string]string) {
	// TODO: handle device removal by creating "hotplug-remove" task
}
