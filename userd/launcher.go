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

package userd

import (
	"fmt"
	"net/url"
	"os/exec"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/strutil"
)

const launcherIntrospectionXML = `
<interface name="org.freedesktop.DBus.Peer">
	<method name='Ping'>
	</method>
	<method name='GetMachineId'>
               <arg type='s' name='machine_uuid' direction='out'/>
	</method>
</interface>
<interface name='io.snapcraft.Launcher'>
	<method name='OpenURL'>
		<arg type='s' name='url' direction='in'/>
	</method>
</interface>`

var (
	allowedURLSchemes = []string{"http", "https", "mailto"}
)

// Launcher implements the 'io.snapcraft.Launcher' DBus interface.
type Launcher struct {
	conn *dbus.Conn
}

// Name returns the name of the interface this object implements
func (s *Launcher) Name() string {
	return "io.snapcraft.Launcher"
}

// BasePath returns the base path of the object
func (s *Launcher) BasePath() dbus.ObjectPath {
	return "/io/snapcraft/Launcher"
}

// IntrospectionData gives the XML formatted introspection description
// of the DBus service.
func (s *Launcher) IntrospectionData() string {
	return launcherIntrospectionXML
}

func makeAccessDeniedError(err error) *dbus.Error {
	return &dbus.Error{
		Name: "org.freedesktop.DBus.Error.AccessDenied",
		Body: []interface{}{err.Error()},
	}
}

// OpenURL implements the 'OpenURL' method of the 'io.snapcraft.Launcher'
// DBus interface. Before the provided url is passed to xdg-open the scheme is
// validated against a list of allowed schemes. All other schemes are denied.
func (s *Launcher) OpenURL(addr string) *dbus.Error {
	u, err := url.Parse(addr)
	if err != nil {
		return &dbus.ErrMsgInvalidArg
	}

	if !strutil.ListContains(allowedURLSchemes, u.Scheme) {
		return makeAccessDeniedError(fmt.Errorf("Supplied URL scheme %q is not allowed", u.Scheme))
	}

	if err = exec.Command("xdg-open", addr).Run(); err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot open supplied URL"))
	}

	return nil
}
