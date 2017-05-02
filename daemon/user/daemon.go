// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package user

import (
	"bytes"

	"github.com/godbus/dbus"

	"fmt"

	"github.com/godbus/dbus/introspect"
	tomb "gopkg.in/tomb.v2"
)

const (
	busName  = "com.canonical.SafeLauncher"
	basePath = "/"
)

type registeredInterface interface {
	Name() string
	IntrospectionData() string
}

// A Daemon listens for requests and routes them to the right command
type Daemon struct {
	tomb   tomb.Tomb
	conn   *dbus.Conn
	ifaces []registeredInterface
}

// NewDaemon creates a new daemon instance
func NewDaemon() (*Daemon, error) {
	return &Daemon{}, nil
}

// SetVersion sets the version of the daemon
func (d *Daemon) SetVersion(version string) {
	// Nothing to do for us here
}

// Init sets up the Daemon's internal workings.
// Don't call more than once.
func (d *Daemon) Init() error {
	return nil
}

func (d *Daemon) createAndExportInterfaces() {
	d.ifaces = []registeredInterface{&SafeLauncher{}}

	var buffer bytes.Buffer
	buffer.WriteString("<node>")

	for _, iface := range d.ifaces {
		d.conn.Export(iface, basePath, iface.Name())
		buffer.WriteString(iface.IntrospectionData())
	}

	buffer.WriteString(introspect.IntrospectDataString)
	buffer.WriteString("</node>")

	d.conn.Export(introspect.Introspectable(buffer.String()), basePath, "org.freedesktop.DBus.Introspectable")
}

// Start the Daemon
func (d *Daemon) Start() {
	d.tomb.Go(func() error {
		var err error
		d.conn, err = dbus.SessionBus()
		if err != nil {
			return err
		}

		reply, err := d.conn.RequestName(busName, dbus.NameFlagDoNotQueue)
		if err != nil {
			return err
		}

		if reply != dbus.RequestNameReplyPrimaryOwner {
			return fmt.Errorf("Failed to request bus name '%s'", busName)
		}

		d.createAndExportInterfaces()

		ch := make(chan *dbus.Signal)
		d.conn.Signal(ch)
		for _ = range ch {
		}

		return nil
	})
}

// Stop shuts down the Daemon
func (d *Daemon) Stop() error {
	d.tomb.Kill(nil)
	d.conn.Close()
	return d.tomb.Wait()
}

// Dying is a tomb-ish thing
func (d *Daemon) Dying() <-chan struct{} {
	return d.tomb.Dying()
}
