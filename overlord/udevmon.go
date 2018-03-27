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
	"sync"

	"github.com/snapcore/snapd/logger"

	"github.com/pilebones/go-udev/crawler"
	"github.com/pilebones/go-udev/netlink"
)

var (
	udevMon *UDevMonitor
	once    sync.Once
)

type udevMonitor interface {
	Run() error
	Stop()
}

type UDevMonitor struct {
	netlinkConn *netlink.UEventConn
	stop        chan struct{}
	monitorStop chan struct{}
	crawlerStop chan struct{}
}

func NewUDevMonitor() (*UDevMonitor, error) {
	var err error
	// There can only ever be one instance of netlink monitor per process,
	// use singleton pattern to force this as otherwise it's very painful
	// for the unit tests that do not delete overlord instances, or
	// instantiate more than one overlord.
	once.Do(func() {
		udevMon = &UDevMonitor{
			stop:        make(chan struct{}),
			netlinkConn: &netlink.UEventConn{},
		}
		err = udevMon.netlinkConn.Connect()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start uevent monitor: %s", err)
	}
	return udevMon, nil
}

func (m *UDevMonitor) Run() error {
	cerrors := make(chan error)
	devs := make(chan crawler.Device)

	// TODO: pass filters set by interfaces once filter type is defined
	m.crawlerStop = crawler.ExistingDevices(devs, cerrors, nil)

	go func() {
		events := make(chan netlink.UEvent)
		errors := make(chan error)

		// TODO: pass filters set by interfaces once filter type is defined
		m.monitorStop = m.netlinkConn.Monitor(events, errors, nil)

		for {
			select {
			case dv := <-devs:
				m.addDevice(dv.KObj, dv.Env)
			case err := <-cerrors:
				logger.Noticef("error enumerating devices: %q\n", err)
			case err := <-errors:
				logger.Noticef("netlink error: %q\n", err)
			case ev := <-events:
				m.udevEvent(&ev)
			case <-m.stop:
				m.monitorStop <- struct{}{}
				m.crawlerStop <- struct{}{}
				return
			}
		}
	}()

	return nil
}

func (m *UDevMonitor) Stop() {
	m.stop <- struct{}{}
}

func (m *UDevMonitor) udevEvent(ev *netlink.UEvent) {
	// TODO: handle the event, e.g. call addDevice, removeDevice
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
