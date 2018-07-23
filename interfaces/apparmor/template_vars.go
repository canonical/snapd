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

package apparmor

import (
	"bytes"
	"fmt"

	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/snap"
)

// templateVariables returns text defining apparmor variables that can be used in the
// apparmor template and by apparmor snippets.
func templateVariables(info *snap.Info, securityTag string) string {
	var buf bytes.Buffer
	// TODO parallel-install: use of proper instance/store name
	fmt.Fprintf(&buf, "@{SNAP_NAME}=\"%s\"\n", info.InstanceName())
	fmt.Fprintf(&buf, "@{SNAP_REVISION}=\"%s\"\n", info.Revision)
	fmt.Fprintf(&buf, "@{PROFILE_DBUS}=\"%s\"\n",
		dbus.SafePath(securityTag))
	fmt.Fprintf(&buf, "@{INSTALL_DIR}=\"/{,var/lib/snapd/}snap\"")
	return buf.String()
}
