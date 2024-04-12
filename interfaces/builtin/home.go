// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package builtin

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

const homeSummary = `allows access to non-hidden files in the home directory`

const homeBaseDeclarationSlots = `
  home:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection:
      plug-attributes:
        read: all
    deny-auto-connection:
      -
        on-classic: false
      -
        plug-attributes:
          read: all
`

const homeConnectedPlugAppArmor = `
# Description: Can access non-hidden files in user's $HOME. This is restricted
# because it gives file access to all of the user's $HOME.

# Note, @{HOME} is the user's $HOME, not the snap's $HOME

# Allow read access to toplevel $HOME for the user
owner @{HOME}/ r,

# Allow read/write access to all files in @{HOME}, except snap application
# data in @{HOME}/snap and toplevel hidden directories in @{HOME}.
###PROMPT###owner @{HOME}/[^s.]**             rwkl###HOME_IX###,
###PROMPT###owner @{HOME}/s[^n]**             rwkl###HOME_IX###,
###PROMPT###owner @{HOME}/sn[^a]**            rwkl###HOME_IX###,
###PROMPT###owner @{HOME}/sna[^p]**           rwkl###HOME_IX###,
###PROMPT###owner @{HOME}/snap[^/]**          rwkl###HOME_IX###,

# Allow creating a few files not caught above
###PROMPT###owner @{HOME}/{s,sn,sna}{,/} rwkl###HOME_IX###,

# Allow access to @{HOME}/snap/ to allow directory traversals from
# @{HOME}/snap/@{SNAP_INSTANCE_NAME} through @{HOME}/snap to @{HOME}.
# While this leaks snap names, it fixes usability issues for snaps
# that require this transitional interface.
###PROMPT###owner @{HOME}/snap/ r,

# Allow access to gvfs mounts for files owned by the user (including hidden
# files; only allow writes to files, not the mount point).
###PROMPT###owner /run/user/[0-9]*/gvfs/{,**} r,
###PROMPT###owner /run/user/[0-9]*/gvfs/*/**  w,

# Disallow writes to the well-known directory included in
# the user's PATH on several distributions
audit deny @{HOME}/bin/{,**} wl,
audit deny @{HOME}/bin wl,
`

const homeConnectedPlugAppArmorWithAllRead = `
# Allow non-owner read to non-hidden and non-snap files and directories
capability dac_read_search,
###PROMPT###@{HOME}/               r,
###PROMPT###@{HOME}/[^s.]**        r,
###PROMPT###@{HOME}/s[^n]**        r,
###PROMPT###@{HOME}/sn[^a]**       r,
###PROMPT###@{HOME}/sna[^p]**      r,
###PROMPT###@{HOME}/snap[^/]**     r,
###PROMPT###@{HOME}/{s,sn,sna}{,/} r,
`

type homeInterface struct {
	commonInterface
}

func (iface *homeInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	// It's fine if 'read' isn't specified, but if it is, it needs to be
	// 'all'
	if r, ok := plug.Attrs["read"]; ok && r != "all" {
		return fmt.Errorf(`home plug requires "read" be 'all'`)
	}

	return nil
}

func (iface *homeInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var read string
	_ = plug.Attr("read", &read)
	// 'owner' is the standard policy
	spec.AddSnippet(homeConnectedPlugAppArmor)

	// 'all' grants standard policy plus read access to home without owner
	// match
	if read == "all" {
		spec.AddSnippet(homeConnectedPlugAppArmorWithAllRead)
	}
	return nil
}

func init() {
	registerIface(&homeInterface{commonInterface{
		name:                 "home",
		summary:              homeSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationSlots: homeBaseDeclarationSlots,
	}})
}
