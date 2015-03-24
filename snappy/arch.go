package snappy

import (
	"launchpad.net/snappy/helpers"
)

// ArchitectureType is the type for a supported snappy architecture
type ArchitectureType string

const (
	// I386 is the i386 architecture
	I386 ArchitectureType = "i386"
	// Amd64 is the amd64 architecture
	Amd64 = "amd64"
	// Armhf is the armhf architecture
	Armhf = "armhf"
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
