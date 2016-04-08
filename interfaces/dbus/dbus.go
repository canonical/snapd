// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package dbus implements interaction between snappy and dbus.
//
// Snappy creates dbus configuration files that describe how various
// services on the system bus can communicate with other peers.
//
// Each configuration is an XML file containing <busconfig>...</busconfig>.
// Particular security snippets define whole <policy>...</policy> entires.
//
// NOTE: This interacts with systemd.
// TODO: Explain how this works (security).
package dbus

import (
	"bytes"
	"fmt"
	"strings"
)

// SafePath returns a string suitable for use in a DBus object
func SafePath(s string) string {
	const allowed = `abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`
	buf := bytes.NewBuffer(make([]byte, 0, len(s)))

	for _, c := range []byte(s) {
		if strings.IndexByte(allowed, c) >= 0 {
			fmt.Fprintf(buf, "%c", c)
		} else {
			fmt.Fprintf(buf, "_%02x", c)
		}
	}

	return buf.String()
}
