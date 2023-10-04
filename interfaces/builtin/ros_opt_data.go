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

const rosOptDataSummary = `Allows read-only access to the xacro,yaml,urdf,stl files in /opt/ros folder`

const rosOptDataBaseDeclarationSlots = `
  ros-opt-data:
    allow-installation:
      slot-snap-type:
        - core
`

const rosOptDataConnectedPlugAppArmor = `
# Description: Allows read-only access to the xacro,yaml,urdf,stl files in /opt/ros folder
# capability dac_read_search,  # this should not be necessary, the idea is to read only what normal user can read
/var/lib/snapd/hostfs/opt/ros/ r,
/var/lib/snapd/hostfs/opt/ros/**/ r,
/var/lib/snapd/hostfs/opt/ros/**/*.xacro r,
/var/lib/snapd/hostfs/opt/ros/**/*.yaml r,
/var/lib/snapd/hostfs/opt/ros/**/*.urdf r,
/var/lib/snapd/hostfs/opt/ros/**/*.stl r,
`

type rosOptDataInterface struct {
	commonInterface
}

func init() {
	registerIface(&rosOptDataInterface{commonInterface{
		name:                  "ros-opt-data",
		summary:               rosOptDataSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  rosOptDataBaseDeclarationSlots,
		connectedPlugAppArmor: rosOptDataConnectedPlugAppArmor,
	}})
}
