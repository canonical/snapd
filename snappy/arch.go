/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
package snappy

import (
	"launchpad.net/snappy/helpers"
)

// ArchitectureType is the type for a supported snappy architecture
type ArchitectureType string

const (
	// Archi386 is the i386 architecture
	Archi386 ArchitectureType = "i386"
	// ArchAmd64 is the amd64 architecture
	ArchAmd64 = "amd64"
	// ArchArmhf is the armhf architecture
	ArchArmhf = "armhf"
)

var arch = ArchitectureType(helpers.UbuntuArchitecture())

// Architecture returns the native architecture that snappy runs on
func Architecture() ArchitectureType {
	return arch
}

// SetArchitecture allows overriding the auto detected Architecture
func SetArchitecture(newArch ArchitectureType) {
	arch = newArch
}
