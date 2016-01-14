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

package caps

import (
	"fmt"
)

// SecuritySystem is a name of a security system.
type SecuritySystem string

const (
	// SecurityApparmor identifies the apparmor security system.
	SecurityApparmor SecuritySystem = "apparmor"
	// SecuritySeccomp identifies the seccomp security system.
	SecuritySeccomp SecuritySystem = "seccomp"
	// SecurityDBus identifies the DBus security system.
	SecurityDBus SecuritySystem = "dbus"
)

// ErrUnknownSecurity is the error reported for unknown security systems.
type ErrUnknownSecurity struct {
	// SecuritySystem is the name of the unknown security system.
	SecuritySystem SecuritySystem
}

func (err *ErrUnknownSecurity) Error() string {
	return fmt.Sprintf("unknown security system %q", err.SecuritySystem)
}
