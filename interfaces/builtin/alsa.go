// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const alsaSummary = `allows access to raw ALSA devices`

const alsaBaseDeclarationSlots = `
  alsa:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const alsaConnectedPlugAppArmor = `
# Description: Allow access to raw ALSA devices.

/dev/snd/  r,
/dev/snd/* rw,

/run/udev/data/c116:[0-9]* r, # alsa
/run/udev/data/+sound:card[0-9]* r,

# Allow access to the alsa state dir
/var/lib/alsa/{,*}         r,

# Allow access to alsa /proc entries
@{PROC}/asound/   r,
@{PROC}/asound/** rw,
`

var alsaConnectedPlugUDev = []string{
	`KERNEL=="controlC[0-9]*"`,
	`KERNEL=="hwC[0-9]*D[0-9]*"`,
	`KERNEL=="pcmC[0-9]*D[0-9]*[cp]"`,
	`KERNEL=="midiC[0-9]*D[0-9]*"`,
	`KERNEL=="timer"`,
	`KERNEL=="seq"`,
	`SUBSYSTEM=="sound", KERNEL=="card[0-9]*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "alsa",
		summary:               alsaSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  alsaBaseDeclarationSlots,
		connectedPlugAppArmor: alsaConnectedPlugAppArmor,
		connectedPlugUDev:     alsaConnectedPlugUDev,
		reservedForOS:         true,
	})
}
