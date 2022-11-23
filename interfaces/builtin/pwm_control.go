// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Zygmunt Krynicki
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

// https://www.kernel.org/doc/Documentation/pwm.txt
const pwmControlSummary = `allows control of all aspects of PWM channels`

// Controlling all aspects of PWM channels can potentially impact other snaps
// and grant wide access to specific hardware and the system, so treat as
// super-privileged
const pwmControlBaseDeclarationPlugs = `
  pwm-control:
    allow-installation: false
    deny-auto-connection: true
`
const pwmControlBaseDeclarationSlots = `
  pwm-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const pwmControlConnectedPlugAppArmor = `
# Description: Allow controlling all aspects of PWM channels. This can
# potentially impact the system and other snaps, and allows privileged access
# to hardware.

/sys/class/pwm/pwmchip[0-9]*/{,un}export rw,
/sys/class/pwm/pwmchip[0-9]*/npwm r,
/sys/class/pwm/pwmchip[0-9]*/pwm[0-9]*/* rwk,
`

func init() {
	registerIface(&commonInterface{
		name:                  "pwm-control",
		summary:               pwmControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  pwmControlBaseDeclarationPlugs,
		baseDeclarationSlots:  pwmControlBaseDeclarationSlots,
		connectedPlugAppArmor: pwmControlConnectedPlugAppArmor,
	})
}
