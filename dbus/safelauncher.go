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

package dbus

import (
	"net/url"
	"os/exec"

	"fmt"

	"github.com/godbus/dbus"
)

const safeLauncherIntrospectionXML = `
<interface name='com.canonical.SafeLauncher'>
	<method name='OpenURL'>
		<arg type='s' name='url' direction='in'/>
	</method>
</interface>`

var (
	allowedURLSchemes = []string{"http", "https", "mailto"}
)

// SafeLauncher implements the 'com.canonical.SafeLauncher' DBus interface
type SafeLauncher struct{}

// Name returns the name of the interface this object implements
func (s *SafeLauncher) Name() string {
	return "com.canonical.SafeLauncher"
}

// IntrospectionData gives the
func (s *SafeLauncher) IntrospectionData() string {
	return safeLauncherIntrospectionXML
}

func runXdgOpen(args ...string) error {
	err := exec.Command("xdg-open", args...).Run()
	if err != nil {
		return err
	}
	return nil
}

// XdgOpenCommand is called from within the SafeLauncher. It's exported
// so it can be overridden by testing.
var XdgOpenCommand = runXdgOpen

// OpenURL implements the 'OpenURL' method of the 'com.canonical.SafeLauncher'
// DBus interface. Before the provided url is passed to xdg-open the scheme is
// validated against a list of allowed schemes. All other schemes are denied.
func (s *SafeLauncher) OpenURL(addr string) *dbus.Error {
	u, err := url.Parse(addr)
	if err != nil {
		return &dbus.ErrMsgInvalidArg
	}

	validScheme := false
	for _, s := range allowedURLSchemes {
		if s == u.Scheme {
			validScheme = true
			break
		}
	}

	if !validScheme {
		return &dbus.Error{
			Name: "org.freedesktop.DBus.Error.AccessDenied",
			Body: []interface{}{fmt.Sprintf("Supplied URL scheme %q is not allowed", u.Scheme)},
		}
	}

	err = XdgOpenCommand(addr)
	if err != nil {
		return dbus.MakeFailedError(fmt.Errorf("Can not open supplied URL"))
	}

	return nil
}
