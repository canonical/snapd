// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package xdgopenproxy

import (
	"fmt"
	"syscall"
	"time"

	"github.com/godbus/dbus"
)

const (
	desktopPortalBusName      = "org.freedesktop.portal.Desktop"
	desktopPortalObjectPath   = "/org/freedesktop/portal/desktop"
	desktopPortalOpenURIIface = "org.freedesktop.portal.OpenURI"
	desktopPortalRequestIface = "org.freedesktop.portal.Request"
)

// portalLauncher is a launcher that forwards the requests to xdg-desktop-portal DBus API
type portalLauncher struct{}

// ensureDesktopPortal ensures that the xdg-desktop-portal service is available
func (p *portalLauncher) ensureDesktopPortal(bus *dbus.Conn) (dbus.BusObject, error) {
	// We call StartServiceByName since old versions of
	// xdg-desktop-portal do not include the AssumedAppArmorLabel
	// key in their service activation file.
	var startResult uint32
	if err := bus.BusObject().Call("org.freedesktop.DBus.StartServiceByName", 0, desktopPortalBusName, uint32(0)).Store(&startResult); err != nil {
		return nil, err
	}
	return bus.Object(desktopPortalBusName, desktopPortalObjectPath), nil
}

// portalResponseSuccess is a numeric value indicating a success carrying out
// the request, returned by the `response` member of
// org.freedesktop.portal.Request.Response signal
const portalResponseSuccess = 0

// timeout for asking the user to make a choice, same value as in usersession/userd/launcher.go
var defaultPortalRequestTimeout = 5 * time.Minute

func (p *portalLauncher) portalCall(bus *dbus.Conn, call func() (dbus.ObjectPath, error)) error {
	// see https://flatpak.github.io/xdg-desktop-portal/portal-docs.html for
	// details of the interaction, in short:
	// 1. caller issues a request to the desktop portal
	// 2. desktop portal responds with a handle to a dbus object capturing the Request
	// 3. caller waits for the org.freedesktop.portal.Request.Response
	// 3a. caller can terminate the request earlier by calling close

	// set up signal handling before we call the portal, so that we do not
	// miss the signals
	signals := make(chan *dbus.Signal, 1)
	bus.Signal(signals)
	defer func() {
		bus.RemoveSignal(signals)
		close(signals)
	}()

	match := []dbus.MatchOption{
		dbus.WithMatchSender(desktopPortalBusName),
		dbus.WithMatchInterface(desktopPortalRequestIface),
		dbus.WithMatchMember("Response"),
	}
	if err := bus.AddMatchSignal(match...); err != nil {
		return err
	}
	defer bus.RemoveMatchSignal(match...)

	requestPath, err := call()
	if err != nil {
		return err
	}
	request := bus.Object(desktopPortalBusName, requestPath)

	timeout := time.NewTimer(defaultPortalRequestTimeout)
	defer timeout.Stop()
	for {
		select {
		case <-timeout.C:
			request.Call(desktopPortalRequestIface+".Close", 0).Store()
			return &responseError{msg: "timeout waiting for user response"}
		case signal := <-signals:
			if signal.Path != requestPath || signal.Name != desktopPortalRequestIface+".Response" {
				// This isn't the signal we're waiting for
				continue
			}

			var response uint32
			var results map[string]interface{} // don't care
			if err := dbus.Store(signal.Body, &response, &results); err != nil {
				return &responseError{msg: fmt.Sprintf("cannot unpack response: %v", err)}
			}
			if response == portalResponseSuccess {
				return nil
			}
			return &responseError{msg: fmt.Sprintf("request declined by the user (code %v)", response)}
		}
	}
}

func (p *portalLauncher) OpenFile(bus *dbus.Conn, filename string) error {
	portal, err := p.ensureDesktopPortal(bus)
	if err != nil {
		return err
	}

	fd, err := syscall.Open(filename, syscall.O_RDONLY, 0)
	if err != nil {
		return &responseError{msg: err.Error()}
	}
	defer syscall.Close(fd)

	return p.portalCall(bus, func() (dbus.ObjectPath, error) {
		var (
			parent  string
			options map[string]dbus.Variant
			request dbus.ObjectPath
		)
		err := portal.Call(desktopPortalOpenURIIface+".OpenFile", 0, parent, dbus.UnixFD(fd), options).Store(&request)
		return request, err
	})
}

func (p *portalLauncher) OpenURI(bus *dbus.Conn, uri string) error {
	portal, err := p.ensureDesktopPortal(bus)
	if err != nil {
		return err
	}

	return p.portalCall(bus, func() (dbus.ObjectPath, error) {
		var (
			parent  string
			options map[string]dbus.Variant
			request dbus.ObjectPath
		)
		err := portal.Call(desktopPortalOpenURIIface+".OpenURI", 0, parent, uri, options).Store(&request)
		return request, err
	})
}
