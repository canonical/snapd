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

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"
	"github.com/jessevdk/go-flags"
	"gopkg.in/tomb.v2"

	dbusifaces "github.com/snapcore/snapd/dbus"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
)

const (
	busName  = "io.snapcraft.SafeLauncher"
	basePath = "/io/snapcraft/SafeLauncher"
)

type registeredDBusInterface interface {
	Name() string
	IntrospectionData() string
}

type cmdUserd struct {
	tomb       tomb.Tomb
	conn       *dbus.Conn
	dbusIfaces []registeredDBusInterface
}

var shortUserdHelp = i18n.G("Start the snap userd service")
var longUserdHelp = i18n.G("The userd command starts the snap user session service.")

func init() {
	cmd := addCommand("userd",
		shortAbortHelp,
		longAbortHelp,
		func() flags.Commander {
			return &cmdUserd{}
		},
		nil,
		[]argDesc{},
	)
	cmd.hidden = true
}

func (x *cmdUserd) createAndExportInterfaces() {
	x.dbusIfaces = []registeredDBusInterface{&dbusifaces.SafeLauncher{}}

	var buffer bytes.Buffer
	buffer.WriteString("<node>")

	for _, iface := range x.dbusIfaces {
		x.conn.Export(iface, basePath, iface.Name())
		buffer.WriteString(iface.IntrospectionData())
	}

	buffer.WriteString(introspect.IntrospectDataString)
	buffer.WriteString("</node>")

	x.conn.Export(introspect.Introspectable(buffer.String()), basePath, "org.freedesktop.DBus.Introspectable")
}

func (x *cmdUserd) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	x.tomb.Go(func() error {
		var err error
		x.conn, err = dbus.SessionBus()
		if err != nil {
			return err
		}

		reply, err := x.conn.RequestName(busName, dbus.NameFlagDoNotQueue)
		if err != nil {
			return err
		}

		if reply != dbus.RequestNameReplyPrimaryOwner {
			err = fmt.Errorf("Failed to request bus name '%s'", busName)
			return err
		}

		x.createAndExportInterfaces()

		// Listen to keep our thread up and running. All DBus bits
		// are running in the background
		select {}
	})

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-ch:
		logger.Noticef("Exiting on %s signal.\n", sig)
	case <-x.tomb.Dying():
	}

	x.tomb.Kill(nil)
	if x.conn != nil {
		x.conn.Close()
	}
	return x.tomb.Wait()
}
