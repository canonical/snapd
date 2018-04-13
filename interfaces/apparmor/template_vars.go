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
func templateVariables(info *snap.Info, securityTag string, onClassic bool) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "@{SNAP_NAME}=\"%s\"\n", info.Name())
	fmt.Fprintf(&buf, "@{SNAP_REVISION}=\"%s\"\n", info.Revision)
	fmt.Fprintf(&buf, "@{PROFILE_DBUS}=\"%s\"\n",
		dbus.SafePath(securityTag))
	fmt.Fprintf(&buf, "@{INSTALL_DIR}=\"/{,var/lib/snapd/}snap\"\n")
	if onClassic {
		fmt.Fprintf(&buf, "@{RANDOM}=\"%d\"", randInt())
	} else {
		// Core devices are not affected by the kernel bug that
		// this is a workaround for. It's not the best place to add
		// this check but it's certainly the easiest one.
		fmt.Fprintf(&buf, "@{RANDOM}=\"%d\"", 0)
	}
	return buf.String()
}

func updateNSTemplateVariables(info *snap.Info, onClassic bool) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "@{SNAP_NAME}=\"%s\"\n", info.Name())
	fmt.Fprintf(&buf, "@{SNAP_REVISION}=\"%s\"\n", info.Revision)
	if onClassic {
		fmt.Fprintf(&buf, "@{RANDOM}=\"%d\"", randInt())
	} else {
		// Core devices are not affected by the kernel bug that
		// this is a workaround for. It's not the best place to add
		// this check but it's certainly the easiest one.
		fmt.Fprintf(&buf, "@{RANDOM}=\"%d\"", 0)
	}
	return buf.String()
}
