// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2021 Canonical Ltd
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

package portal

import (
	"fmt"
	"syscall"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/osutil"
)

const (
	desktopPortalBusName      = "org.freedesktop.portal.Desktop"
	desktopPortalObjectPath   = "/org/freedesktop/portal/desktop"
	desktopPortalOpenURIIface = "org.freedesktop.portal.OpenURI"
	desktopPortalRequestIface = "org.freedesktop.portal.Request"
)

type ResponseError struct {
	msg string
}

func (u *ResponseError) Error() string { return u.msg }

func (u *ResponseError) Is(err error) bool {
	_, ok := err.(*ResponseError)
	return ok
}

func MakeResponseError(msg string) error {
	osutil.MustBeTestBinary("only to be used in tests")
	return &ResponseError{msg: msg}
}

// desktopPortal gets a reference to the xdg-desktop-portal D-Bus service
func desktopPortal(bus *dbus.Conn) (dbus.BusObject, error) {
	// We call StartServiceByName since old versions of
	// xdg-desktop-portal do not include the AssumedAppArmorLabel
	// key in their service activation file.
	var startResult uint32
	mylog.Check(bus.BusObject().Call("org.freedesktop.DBus.StartServiceByName", 0, desktopPortalBusName, uint32(0)).Store(&startResult))
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

	switch startResult {
	case 1: // DBUS_START_REPLY_SUCCESS
	case 2: // DBUS_START_REPLY_ALREADY_RUNNING
	default:
		return nil, fmt.Errorf("unexpected response from StartServiceByName (code %v)", startResult)
	}
	return bus.Object(desktopPortalBusName, desktopPortalObjectPath), nil
}

// portalResponseSuccess is a numeric value indicating a success carrying out
// the request, returned by the `response` member of
// org.freedesktop.portal.Request.Response signal
const portalResponseSuccess = 0

// timeout for asking the user to make a choice, same value as in usersession/userd/launcher.go
var defaultPortalRequestTimeout = 5 * time.Minute

func portalCall(bus *dbus.Conn, call func() (dbus.ObjectPath, error)) error {
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
	mylog.Check(bus.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchRule).Store())

	defer bus.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, matchRule)

	requestPath := mylog.Check2(call())

	request := bus.Object(desktopPortalBusName, requestPath)

	timeout := time.NewTimer(defaultPortalRequestTimeout)
	defer timeout.Stop()
	for {
		select {
		case <-timeout.C:
			request.Call(desktopPortalRequestIface+".Close", 0).Store()
			return &ResponseError{msg: "timeout waiting for user response"}
		case signal := <-signals:
			if signal.Path != requestPath || signal.Name != desktopPortalRequestIface+".Response" {
				// This isn't the signal we're waiting for
				continue
			}

			var response uint32
			var results map[string]interface{}
			mylog.Check( // don't care
				dbus.Store(signal.Body, &response, &results))

			if response == portalResponseSuccess {
				return nil
			}
			return &ResponseError{msg: fmt.Sprintf("request declined by the user (code %v)", response)}
		}
	}
}

func OpenFile(bus *dbus.Conn, filename string) error {
	portal := mylog.Check2(desktopPortal(bus))

	fd := mylog.Check2(syscall.Open(filename, syscall.O_RDONLY, 0))

	defer syscall.Close(fd)

	return portalCall(bus, func() (dbus.ObjectPath, error) {
		var (
			parent  string
			options map[string]dbus.Variant
			request dbus.ObjectPath
		)
		mylog.Check(portal.Call(desktopPortalOpenURIIface+".OpenFile", 0, parent, dbus.UnixFD(fd), options).Store(&request))
		return request, err
	})
}

func OpenURI(bus *dbus.Conn, uri string) error {
	portal := mylog.Check2(desktopPortal(bus))

	return portalCall(bus, func() (dbus.ObjectPath, error) {
		var (
			parent  string
			options map[string]dbus.Variant
			request dbus.ObjectPath
		)
		mylog.Check(portal.Call(desktopPortalOpenURIIface+".OpenURI", 0, parent, uri, options).Store(&request))
		return request, err
	})
}
