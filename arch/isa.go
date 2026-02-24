// -*- Mode: Go; indent-tabs-mode: t -*-

package arch

import "fmt"

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
