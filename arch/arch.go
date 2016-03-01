// -*- Mode: Go; indent-tabs-mode: t -*-

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

package arch

import (
	"log"
	"runtime"
)

// ArchitectureType is the type for a supported snappy architecture
type ArchitectureType string

// arch is global to allow tools like ubuntu-device-flash to
// change the architecture. This is important to e.g. install
// armhf snaps onto a armhf image that is generated on an amd64
// machine
var arch = ArchitectureType(ubuntuArchFromGoArch(runtime.GOARCH))

// SetArchitecture allows overriding the auto detected Architecture
func SetArchitecture(newArch ArchitectureType) {
	arch = newArch
}

// UbuntuArchitecture returns the debian equivalent architecture for the
// currently running architecture.
//
// If the architecture does not map any debian architecture, the
// GOARCH is returned.
func UbuntuArchitecture() string {
	return string(arch)
}

// ubuntuArchFromGoArch maps a go architecture string to the coresponding
// Ubuntu architecture string.
//
// E.g. the go "386" architecture string maps to the ubuntu "i386"
// architecture.
func ubuntuArchFromGoArch(goarch string) string {
	goArchMapping := map[string]string{
		// go      ubuntu
		"386":     "i386",
		"amd64":   "amd64",
		"arm":     "armhf",
		"arm64":   "arm64",
		"ppc64le": "ppc64el",
		"s390x":   "s390x",
		"ppc":     "powerpc",
	}

	ubuntuArch := goArchMapping[goarch]
	if ubuntuArch == "" {
		log.Panicf("unknown goarch %v", goarch)
	}

	return ubuntuArch
}

// IsSupportedArchitecture returns true if the system architecture is in the
// list of architectures.
func IsSupportedArchitecture(architectures []string) bool {
	for _, a := range architectures {
		if a == "all" || a == string(arch) {
			return true
		}
	}

	return false
}
