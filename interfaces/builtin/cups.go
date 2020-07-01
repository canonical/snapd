// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

// On systems where the slot is provided by an app snap, the cups interface is
// the companion interface to the cups-control interface. The design of these
// interfaces is based on the idea that the slot implementation (eg cupsd) is
// expected to query snapd to determine if the cups-control interface is
// connected or not for the peer client process and the print service will
// mediate admin functionality (ie, the rules in these interfaces allow
// connecting to the print service, but do not implement enforcement rules; it
// is up to the print service to provide enforcement).
const cupsSummary = `allows access to the CUPS socket for printing`

// cups is currently only available via a providing app snap and this interface
// assumes that the providing app snap also slots 'cups-control' (the current
// design allows the snap provider to slots both cups-control and cups or just
// cups-control (like with implicit classic or any slot provider without
// mediation patches), but not just cups).
const cupsBaseDeclarationSlots = `
  cups:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
`

const cupsConnectedPlugAppArmor = `
# Allow communicating with the cups server

#include <abstractions/cups-client>
/{,var/}run/cups/printcap r,
`

func init() {
	registerIface(&commonInterface{
		name:                  "cups",
		summary:               cupsSummary,
		implicitOnCore:        false,
		implicitOnClassic:     false,
		baseDeclarationSlots:  cupsBaseDeclarationSlots,
		connectedPlugAppArmor: cupsConnectedPlugAppArmor,
	})
}
