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

// Package xdgopenproxy provides a client for snap userd's xdg-open D-Bus proxy
package xdgopenproxy

import (
	"fmt"
	"net/url"
	"syscall"
	"time"

	"github.com/godbus/dbus"
	"golang.org/x/xerrors"
)

type bus interface {
	Object(name string, objectPath dbus.ObjectPath) dbus.BusObject
	AddMatchSignal(options ...dbus.MatchOption) error
	RemoveMatchSignal(options ...dbus.MatchOption) error
	Signal(ch chan<- *dbus.Signal)
	RemoveSignal(ch chan<- *dbus.Signal)
	Close() error
}

type responseError struct {
	msg string
}

func (u *responseError) Error() string { return u.msg }

func (u *responseError) Is(err error) bool {
	_, ok := err.(*responseError)
	return ok
}

type desktopLauncher interface {
	openFile(path string) error
	openURL(url string) error
}

// portalLauncher is a launcher that forwards the requests to xdg-desktop-portal DBus API
type portalLauncher struct {
	bus     bus
	service dbus.BusObject
}

func (p *portalLauncher) openFile(filename string) error {
	fd, err := syscall.Open(filename, syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	return p.portalCall("org.freedesktop.portal.OpenURI.OpenFile", 0, "", dbus.UnixFD(fd),
		map[string]dbus.Variant{})
}

func (p *portalLauncher) openURL(path string) error {
	return p.portalCall("org.freedesktop.portal.OpenURI.OpenURI", 0, "", path,
		map[string]dbus.Variant{})
}

// portalResponseSuccess is a numeric value indicating a success carrying out
// the request, returned by the `response` member of
// org.freedesktop.portal.Request.Response signal
const portalResponseSuccess = 0

// timeout for asking the user to make a choice, same value as in usersession/userd/launcher.go
var defaultPortalRequestTimeout = 5 * time.Minute

func (p *portalLauncher) portalCall(member string, flags dbus.Flags, args ...interface{}) error {
	// see https://flatpak.github.io/xdg-desktop-portal/portal-docs.html for
	// details of the interaction, in short:
	// 1. caller issues a request to the desktop portal
	// 2. desktop portal responds with a handle to a dbus object capturing the Request
	// 3. caller waits for the org.freedesktop.portal.Request.Response
	// 3a. caller can terminate the request earlier by calling close

	// set up signal handling before we call the portal, so that we do not
	// miss the signals
	signals := make(chan *dbus.Signal, 1)
	match := []dbus.MatchOption{
		dbus.WithMatchSender("org.freedesktop.portal.Desktop"),
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	}

	p.bus.Signal(signals)
	p.bus.AddMatchSignal(match...)

	defer func() {
		p.bus.RemoveMatchSignal(match...)
		p.bus.RemoveSignal(signals)
		close(signals)
	}()

	var handle dbus.ObjectPath
	if err := p.service.Call(member, flags, args...).Store(&handle); err != nil {
		// failure to launch the request is not a response error
		return err
	}

	responseObject := p.bus.Object("org.freedesktop.portal.Desktop", handle)

	timeout := time.NewTicker(defaultPortalRequestTimeout)
	defer timeout.Stop()
	for {
		select {
		case <-timeout.C:
			responseObject.Call("org.freedesktop.portal.Request.Close", 0)
			return &responseError{msg: "timeout waiting for user response"}
		case signal := <-signals:
			if signal.Path != responseObject.Path() {
				// different object path
				continue
			}

			var response uint
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

func newPortalLauncher(bus bus) desktopLauncher {
	obj := bus.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")

	return &portalLauncher{service: obj, bus: bus}
}

// snapcraftLauncher is a launcher that forwards the requests to `snap userd` DBus API
type snapcraftLauncher struct {
	service dbus.BusObject
}

func (s *snapcraftLauncher) openFile(filename string) error {
	fd, err := syscall.Open(filename, syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	return s.service.Call("io.snapcraft.Launcher.OpenFile", 0, "", dbus.UnixFD(fd)).Err
}

func (s *snapcraftLauncher) openURL(path string) error {
	return s.service.Call("io.snapcraft.Launcher.OpenURL", 0, path).Err
}

func newSnapcraftLauncher(bus bus) desktopLauncher {
	obj := bus.Object("io.snapcraft.Launcher", "/io/snapcraft/Launcher")

	return &snapcraftLauncher{service: obj}
}

var sessionBus = func() (bus, error) { return dbus.SessionBus() }

// Run attempts to open given file or URL using one of available launchers
func Run(urlOrFile string) error {
	sbus, err := sessionBus()
	if err != nil {
		return err
	}
	// launchers to try in order, prefer portal launcher over snap userd one
	launchers := []desktopLauncher{newPortalLauncher(sbus), newSnapcraftLauncher(sbus)}

	defer sbus.Close()
	return launch(launchers, urlOrFile)
}

func launchWithOne(l desktopLauncher, urlOrFile string) error {
	if u, err := url.Parse(urlOrFile); err == nil {
		if u.Scheme == "file" {
			return l.openFile(u.Path)
		} else if u.Scheme != "" {
			return l.openURL(urlOrFile)
		}
	}
	return l.openFile(urlOrFile)
}

func launch(launchers []desktopLauncher, urlOrFile string) error {
	var err error
	for _, l := range launchers {
		err = launchWithOne(l, urlOrFile)
		if err == nil {
			break
		}
		if xerrors.Is(err, &responseError{}) {
			// got a response which indicates the action was either
			// explicitly rejected by the user or abandoned due to
			// other reasons eg. timeout waiting for user to respond
			break
		}
	}
	return err
}
