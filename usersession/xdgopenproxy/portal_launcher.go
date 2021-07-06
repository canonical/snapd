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
	"os"
	"syscall"
	"time"

	"github.com/godbus/dbus"
)

const (
	desktopPortalBusName      = "org.freedesktop.portal.Desktop"
	desktopPortalObjectPath   = "/org/freedesktop/portal/desktop"
	desktopPortalOpenURIIface = "org.freedesktop.portal.OpenURI"

	desktopPortalRequestIface = "org.freedesktop.portal.Request"

	documentsPortalBusName        = "org.freedesktop.portal.Documents"
	documentsPortalObjectPath     = "/org/freedesktop/portal/documents"
	documentsPortalDocumentsIface = "org.freedesktop.portal.Documents"
)

// portalLauncher is a launcher that forwards the requests to xdg-desktop-portal DBus API
type portalLauncher struct{}

// desktopPortal gets a reference to the xdg-desktop-portal D-Bus service
func (p *portalLauncher) desktopPortal(bus *dbus.Conn) (dbus.BusObject, error) {
	// We call StartServiceByName since old versions of
	// xdg-desktop-portal do not include the AssumedAppArmorLabel
	// key in their service activation file.
	var startResult uint32
	err := bus.BusObject().Call("org.freedesktop.DBus.StartServiceByName", 0, desktopPortalBusName, uint32(0)).Store(&startResult)
	if dbusErr, ok := err.(dbus.Error); ok {
		// If it is not possible to activate the service
		// (i.e. there is no .service file or the systemd unit
		// has been masked), assume it is already
		// running. Subsequent method calls will fail if this
		// assumption is false.
		if dbusErr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" || dbusErr.Name == "org.freedesktop.systemd1.Masked" {
			err = nil
			startResult = 2 // DBUS_START_REPLY_ALREADY_RUNNING
		}
	}
	if err != nil {
		return nil, err
	}
	switch startResult {
	case 1: // DBUS_START_REPLY_SUCCESS
	case 2: // DBUS_START_REPLY_ALREADY_RUNNING
	default:
		return nil, fmt.Errorf("unexpected response from StartServiceByName (code %v)", startResult)
	}
	return bus.Object(desktopPortalBusName, desktopPortalObjectPath), nil
}

// documentsPortal gets a reference to the xdg-documents-portal D-Bus service
func (p *portalLauncher) documentsPortal(bus *dbus.Conn) (dbus.BusObject, error) {
	// We call StartServiceByName since old versions of
	// xdg-desktop-portal do not include the AssumedAppArmorLabel
	// key in their service activation file.
	var startResult uint32
	err := bus.BusObject().Call("org.freedesktop.DBus.StartServiceByName", 0, documentsPortalBusName, uint32(0)).Store(&startResult)
	if dbusErr, ok := err.(dbus.Error); ok {
		// If it is not possible to activate the service
		// (i.e. there is no .service file or the systemd unit
		// has been masked), assume it is already
		// running. Subsequent method calls will fail if this
		// assumption is false.
		if dbusErr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" || dbusErr.Name == "org.freedesktop.systemd1.Masked" {
			err = nil
			startResult = 2 // DBUS_START_REPLY_ALREADY_RUNNING
		}
	}
	if err != nil {
		return nil, err
	}
	switch startResult {
	case 1: // DBUS_START_REPLY_SUCCESS
	case 2: // DBUS_START_REPLY_ALREADY_RUNNING
	default:
		return nil, fmt.Errorf("unexpected response from StartServiceByName (code %v)", startResult)
	}
	return bus.Object(documentsPortalBusName, documentsPortalObjectPath), nil
}

// portalResponseSuccess is a numeric value indicating a success carrying out
// the request, returned by the `response` member of
// org.freedesktop.portal.Request.Response signal
const portalResponseSuccess = 0

// timeout for asking the user to make a choice, same value as in usersession/userd/launcher.go
var defaultDesktopPortalRequestTimeout = 5 * time.Minute

func (p *portalLauncher) desktopPortalCall(bus *dbus.Conn, call func() (dbus.ObjectPath, error)) error {
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

	// TODO: this should use dbus.Conn.AddMatchSignal, but that
	// does not exist in the external copies of godbus on some
	// supported platforms.
	const matchRule = "type='signal',sender='" + desktopPortalBusName + "',interface='" + desktopPortalRequestIface + "',member='Response'"
	if err := bus.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchRule).Store(); err != nil {
		return err
	}
	defer bus.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, matchRule)

	requestPath, err := call()
	if err != nil {
		return err
	}
	request := bus.Object(desktopPortalBusName, requestPath)

	timeout := time.NewTimer(defaultDesktopPortalRequestTimeout)
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

func (p *portalLauncher) OpenFile(bus *dbus.Conn, filename string, options LauncherModifiers) error {

	var attemptWritable bool
	var fd int

	pathStat, err := os.Stat(filename)
	if err != nil {
		return &responseError{msg: err.Error()}
	}

	// Trying to make paths writable is not valid, but doesn't fail here, so explicitly avoid bothering.
	// Otherwise try open a file with RDRW and if that fails, try again with RDONLY.
	if pathStat.IsDir() {
		fd, err = syscall.Open(filename, syscall.O_RDONLY, 0)
		if err != nil {
			return &responseError{msg: err.Error()}
		}
	} else {
		attemptWritable = true
		fd, err = syscall.Open(filename, syscall.O_RDWR, 0)
		if err != nil {
			fd, err = syscall.Open(filename, syscall.O_RDONLY, 0)
			if err != nil {
				return &responseError{msg: err.Error()}
			}
			attemptWritable = false
		}
	}
	defer syscall.Close(fd)

	portal, err := p.documentsPortal(bus)
	if err != nil {
		return err
	}

	// Try add the file to the documents portal, allowing OpenURI to mediate sandbox permissions if needed.
	// Failure here is not a critical error, the application opened by OpenURI might not need mediation.
	if attemptWritable {
		var docID interface{}
		err = portal.Call(documentsPortalDocumentsIface+".Add", 0, dbus.UnixFD(fd), false, false).Store(&docID)
		if err != nil {
			attemptWritable = false
		}
	}

	portal, err = p.desktopPortal(bus)
	if err != nil {
		return err
	}

	return p.desktopPortalCall(bus, func() (dbus.ObjectPath, error) {
		var (
			parent  string
			request dbus.ObjectPath
		)

		// The first call might fail if the file isn't actually writable by the host
		// e.g, loading files in $SNAP due to the read only squashfs nature.
		// Try with write enabled first and retry in the event of an error.
		optionalFlags := make(map[string]dbus.Variant)
		if options.Ask {
			optionalFlags["ask"] = dbus.MakeVariant(true)
		}
		if attemptWritable {
			optionalFlags["writable"] = dbus.MakeVariant(true)
		}

		err := portal.Call(desktopPortalOpenURIIface+".OpenFile", 0, parent, dbus.UnixFD(fd), optionalFlags).Store(&request)
		// Only bother again if writable was set in the first place
		if err != nil && attemptWritable {
			optionalFlags["writable"] = dbus.MakeVariant(false)
			err = portal.Call(desktopPortalOpenURIIface+".OpenFile", 0, parent, dbus.UnixFD(fd), optionalFlags).Store(&request)
		}

		return request, err
	})
}

func (p *portalLauncher) OpenURI(bus *dbus.Conn, uri string, options LauncherModifiers) error {
	portal, err := p.desktopPortal(bus)
	if err != nil {
		return err
	}

	return p.desktopPortalCall(bus, func() (dbus.ObjectPath, error) {
		var (
			parent  string
			options map[string]dbus.Variant
			request dbus.ObjectPath
		)
		err := portal.Call(desktopPortalOpenURIIface+".OpenURI", 0, parent, uri, options).Store(&request)
		return request, err
	})
}
