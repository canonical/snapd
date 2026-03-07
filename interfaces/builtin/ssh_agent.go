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
        - core
    deny-auto-connection: true
`

const sshAgentConnectedPlugAppArmor = `
# allow access to socket owned by user in default location for openssh ssh-agent
owner /tmp/ssh-*/agent.* rw,
# allow access to default location for gnome keyring ssh-agent
owner /run/user/*/keyring/ssh rw,
# allow access to default location for gcr ssh-agent socket
owner /run/user/*/gcr/ssh rw,
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
