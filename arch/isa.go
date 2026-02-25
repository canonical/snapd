// -*- Mode: Go; indent-tabs-mode: t -*-

package arch

import "fmt"

// IsISASupportedByCPU takes the name of an ISA and checks if it is supported by the CPU and kernel
// snapd is currently running on using architecture-specific code.
// Returns nil when the ISA is supported, or an explicit error otherwise.
var IsISASupportedByCPU = isISASupportedByCPU

func isISASupportedByCPU(isa string) error {
	// Run architecture-dependent compatibility checks
	runningArch := DpkgArchitecture()
	switch runningArch {
	case "riscv64":
		return IsRISCVISASupported(isa)
	default:
		return fmt.Errorf("ISA specification is not supported for arch: %s", runningArch)
	}
}
