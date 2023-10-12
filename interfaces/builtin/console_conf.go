// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

const consoleConfSummary = `allows console-conf capability`

const consoleConfBaseDeclarationSlots = `
  console-conf:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const consoleConfConnectedPlugAppArmor = `
capability dac_read_search,
capability dac_override,

/{,var/}run/console_conf/ rw,
/{,var/}run/console_conf/** rw,
/var/log/console-conf/ rw,
/var/log/console-conf/* rw,
`

type consoleConfInterface struct {
	commonInterface
}

func init() {
	registerIface(&consoleConfInterface{
		commonInterface{
			name:                  "console-conf",
			summary:               consoleConfSummary,
			implicitOnCore:        true,
			implicitOnClassic:     true,
			baseDeclarationSlots:  consoleConfBaseDeclarationSlots,
			connectedPlugAppArmor: consoleConfConnectedPlugAppArmor,
		},
	})
}
