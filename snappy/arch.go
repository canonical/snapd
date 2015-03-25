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
