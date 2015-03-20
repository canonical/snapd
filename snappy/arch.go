package snappy

import (
	"launchpad.net/snappy/helpers"
)

var arch = helpers.Architecture()

// Architecture returns the native architecture that snappy runs on
func Architecture() string {
	return arch
}

// SetArchitecture allows overriding the auto detected Architecture
func SetArchitecture(newArch string) {
	arch = newArch
}
