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

	"github.com/snapcore/snapd/osutil"
)

// ArchitectureType is the type for a supported snappy architecture
type ArchitectureType string

// arch is global to allow tools like ubuntu-device-flash to
// change the architecture. This is important to e.g. install
// armhf snaps onto a armhf image that is generated on an amd64
// machine
var arch = ArchitectureType(dpkgArchFromGoArch(runtime.GOARCH))

// SetArchitecture allows overriding the auto detected Architecture
func SetArchitecture(newArch ArchitectureType) {
	arch = newArch
}

// DpkgArchitecture returns the debian equivalent architecture for the
// currently running architecture.
//
// If the architecture does not map any debian architecture, the
// GOARCH is returned.
func DpkgArchitecture() string {
	return string(arch)
}

// dpkgArchFromGoArch maps a go architecture string to the coresponding
// Debian equivalent architecture string.
//
// E.g. the go "386" architecture string maps to the ubuntu "i386"
// architecture.
func dpkgArchFromGoArch(goarch string) string {
	goArchMapping := map[string]string{
		// go      dpkg
		"386":     "i386",
		"amd64":   "amd64",
		"arm":     "armhf",
		"arm64":   "arm64",
		"ppc64le": "ppc64el",
		"s390x":   "s390x",
		"ppc":     "powerpc",
		// available in debian and other distros
		"ppc64": "ppc64",
	}

	// If we are running on an ARM platform we need to have a
	// closer look if we are on armhf or armel. If we're not
	// on a armv6 platform we can continue to use the Go
	// arch mapping. The Go arch sadly doesn't map this out
	// for us so we have to fallback to uname here.
	if goarch == "arm" {
		if osutil.MachineName() == "armv6l" {
			return "armel"
		}
	}

	dpkgArch := goArchMapping[goarch]
	if dpkgArch == "" {
		log.Panicf("unknown goarch %q", goarch)
	}

	return dpkgArch
}

// DpkgKernelArchitecture returns the debian equivalent architecture
// for the current running kernel. This is usually the same as the
// DpkgArchitecture - however there maybe cases that you run e.g.
// a snapd:i386 on an amd64 kernel.
func DpkgKernelArchitecture() string {
	return dpkgArchFromKernelArch(osutil.MachineName())
}

// dpkgArchFromkernelArch maps the kernel architecture as reported
// via uname() to the dpkg architecture
func dpkgArchFromKernelArch(utsMachine string) string {
	kernelArchMapping := map[string]string{
		// kernel  dpkg
		"i686":    "i386",
		"x86_64":  "amd64",
		"armv7l":  "armhf",
		"armv8l":  "arm64",
		"aarch64": "arm64",
		"ppc64le": "ppc64el",
		"s390x":   "s390x",
		"ppc":     "powerpc",
		// available in debian and other distros
		"ppc64": "ppc64",
	}

	dpkgArch := kernelArchMapping[utsMachine]
	if dpkgArch == "" {
		log.Panicf("unknown kernel arch %q", utsMachine)
	}

	return dpkgArch
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
