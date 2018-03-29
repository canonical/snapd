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

package hardwarestate

import (
	"fmt"

	"github.com/snapcore/snapd/logger"

	"github.com/pilebones/go-udev/crawler"
	"github.com/pilebones/go-udev/netlink"
	"gopkg.in/tomb.v2"
)

type udevMonitor interface {
	Run() error
	Stop() error
}

type UDevMonitor struct {
	tmb         *tomb.Tomb
	netlinkConn *netlink.UEventConn
	monitorStop chan struct{}
	crawlerStop chan struct{}
}

func NewUDevMonitor() *UDevMonitor {
	udevMon := &UDevMonitor{
		tmb: new(tomb.Tomb),
	}
	return udevMon
}

func (m *UDevMonitor) Run() error {
	cerrors := make(chan error)
	devs := make(chan crawler.Device)

	m.netlinkConn = &netlink.UEventConn{}
	if err := m.netlinkConn.Connect(); err != nil {
		return fmt.Errorf("failed to start uevent monitor: %s", err)
	}

	events := make(chan netlink.UEvent)
	errors := make(chan error)

	// TODO: pass filters set by interfaces once filter type is defined
	m.monitorStop = m.netlinkConn.Monitor(events, errors, nil)

	// TODO: pass filters set by interfaces once filter type is defined
	m.crawlerStop = crawler.ExistingDevices(devs, cerrors, nil)

	m.tmb.Go(func() error {
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
			case <-m.tmb.Dying():
				m.monitorStop <- struct{}{}
				m.crawlerStop <- struct{}{}
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

type UDevMonitorMock struct{}

func (u *UDevMonitorMock) Run() error { return nil }
func (u *UDevMonitorMock) Stop()      {}
