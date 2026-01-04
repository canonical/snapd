// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

const sshAgentSummary = `allows access to users ssh-agent`
const sshAgentBaseDeclarationSlots = `
  ssh-agent:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
`

const sshAgentConnectedPlugAppArmor = `
# allow access to socket owned by user in default locatin for openssh ssh-agent
owner /tmp/ssh-[a-zA-Z0-9]+/agent.[0-9]+ rw,
# allow access to default location for gnome keyring ssh-agent for standard users (uid 1000+)
owner /run/user/[0-9]{4,}/keyring/ssh rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "ssh-agent",
		summary:               sshAgentSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  sshAgentBaseDeclarationSlots,
		connectedPlugAppArmor: sshAgentConnectedPlugAppArmor,
	})
}
