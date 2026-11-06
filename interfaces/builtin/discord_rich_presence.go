// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

const discordRichPresenceSummary = `allows access to the ipc sockets exposed by discord`

const discordRichPresenceBaseDeclarationSlots = `
  discord-rich-presence:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const discordRichPresenceConnectedPlugAppArmor = `
# Description: Can access the IPC socket of Discord exposed by
# either Discord snap or classically installed Discord.

owner /{,var/}run/user/[0-9]*/discord-ipc-[0-9] rw,
owner /{,var/}run/user/[0-9]*/snap.discord/discord-ipc-[0-9] rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "discord-rich-presence",
		summary:               discordRichPresenceSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  discordRichPresenceBaseDeclarationSlots,
		connectedPlugAppArmor: discordRichPresenceConnectedPlugAppArmor,
	})
}
