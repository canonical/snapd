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

const hdmiCecSummary = `allows access to HDMI CEC devices`

const hdmiCecBaseDeclarationSlots = `
  hdmi-cec:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const hdmiCecConnectedPlugAppArmor = `
# CEC adapter character devices
/dev/cec[0-9]* rw,
/dev/hdmicec rw,
/dev/CEC rw,
/dev/aocec rw,
/dev/mxc_hdmi_cec rw,
/dev/tegra_cec rw,
/dev/vchiq rw,

# CEC adapter enumeration and metadata
/sys/bus/cec/devices/ r,
/sys/bus/cec/devices/** r,
/sys/devices/**/cec[0-9]*/{,**} r,
/sys/devices/platform/**/{subsystem,uevent} r,

# Legacy SoC backend metadata and control
/sys/module/s5p_hdmi/parameters/source_phy_addr r,
/sys/devices/platform/tegra_cec/cec_logical_addr_config rw,
`

var hdmiCecConnectedPlugUDev = []string{
	`SUBSYSTEM=="cec"`,
	`KERNEL=="hdmicec"`,
	`KERNEL=="CEC"`,
	`KERNEL=="aocec"`,
	`KERNEL=="mxc_hdmi_cec"`,
	`KERNEL=="tegra_cec"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "hdmi-cec",
		summary:               hdmiCecSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  hdmiCecBaseDeclarationSlots,
		connectedPlugAppArmor: hdmiCecConnectedPlugAppArmor,
		connectedPlugUDev:     hdmiCecConnectedPlugUDev,
	})
}
