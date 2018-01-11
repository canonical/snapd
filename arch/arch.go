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
	"syscall"

	"github.com/snapcore/snapd/release"
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

// FIXME: rename all Ubuntu*Architecture() to SnapdArchitecture()
//        (or DpkgArchitecture)

// UbuntuArchitecture returns the debian equivalent architecture for the
// currently running architecture.
//
// If the architecture does not map any debian architecture, the
// GOARCH is returned.
func UbuntuArchitecture() string {
	return string(arch)
}

// UbuntuArchTriplet returns a normalized gnu triplet that is also
// used by Ubuntu and Debian for its multi-arch locations.
//
// Taken from https://wiki.debian.org/Multiarch/Tuples
func NormalizedGNUArchTriplet() string {
	switch arch {
	case "arm64":
		return "aarch64‑linux‑gnu"
	case "armhf":
		return "arm-linux-gnueabihf"
	case "amd64":
		return "x86_64-linux-gnu"
	case "i386":
		return "i386-linux-gnu"
		// ppc64 available in debian and other distros
	case "ppc64":
		return "powerpc64-linux-gnu"
	case "ppc64el":
		return "powerpc64le-linux-gnu"
	case "powerpc":
		return "powerpc-linux-gnu"
	case "s390x":
		return "s390x-linux-gnu"
	default:
		return ""
	}
}

// ubuntuArchFromGoArch maps a go architecture string to the coresponding
// Ubuntu architecture string.
//
// E.g. the go "386" architecture string maps to the ubuntu "i386"
// architecture.
func ubuntuArchFromGoArch(goarch string) string {
	// keep in sync with UbuntuArchTriplet
	goArchMapping := map[string]string{
		// go      ubuntu
		"amd64": "amd64",
		"arm":   "armhf",
		"arm64": "arm64",
		"386":   "i386",
		// ppc64 available in debian and other distros
		"ppc64":   "ppc64",
		"ppc64le": "ppc64el",
		"ppc":     "powerpc",
		"s390x":   "s390x",
	}

	// If we are running on an ARM platform we need to have a
	// closer look if we are on armhf or armel. If we're not
	// on a armv6 platform we can continue to use the Go
	// arch mapping. The Go arch sadly doesn't map this out
	// for us so we have to fallback to uname here.
	if goarch == "arm" {
		machineName := release.Machine()
		if machineName == "armv6l" {
			return "armel"
		}
	}

	ubuntuArch := goArchMapping[goarch]
	if ubuntuArch == "" {
		log.Panicf("unknown goarch %q", goarch)
	}

	return ubuntuArch
}

// UbuntuKernelArchitecture return the debian equivalent architecture
// for the current running kernel. This is usually the same as the
// UbuntuArchitecture - however there maybe cases that you run e.g.
// a snapd:i386 on an amd64 kernel.
func UbuntuKernelArchitecture() string {
	var utsname syscall.Utsname
	if err := syscall.Uname(&utsname); err != nil {
		log.Panicf("cannot get kernel architecture: %v", err)
	}

	// syscall.Utsname{} is using [65]int8 for all char[] inside it,
	// this makes converting it so awkward. The alternative would be
	// to use a unsafe.Pointer() to cast it to a [65]byte slice.
	// see https://github.com/golang/go/issues/20753
	kernelArch := make([]byte, 0, len(utsname.Machine))
	for _, c := range utsname.Machine {
		if c == 0 {
			break
		}
		kernelArch = append(kernelArch, byte(c))
	}

	return ubuntuArchFromKernelArch(string(kernelArch))
}

// ubuntuArchFromkernelArch maps the kernel architecture as reported
// via uname() to the dpkg architecture
func ubuntuArchFromKernelArch(utsMachine string) string {
	kernelArchMapping := map[string]string{
		// kernel  ubuntu
		"i686":    "i386",
		"x86_64":  "amd64",
		"armv7l":  "armhf",
		"aarch64": "arm64",
		"ppc64le": "ppc64el",
		"s390x":   "s390x",
		"ppc":     "powerpc",
		// available in debian and other distros
		"ppc64": "ppc64",
	}

	ubuntuArch := kernelArchMapping[utsMachine]
	if ubuntuArch == "" {
		log.Panicf("unknown kernel arch %q", utsMachine)
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
