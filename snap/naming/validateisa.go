// -*- Mode: Go; indent-tabs-mode: t -*-
//

package naming

import (
	"fmt"

	"github.com/snapcore/snapd/arch"
)

var IsRISCVISASupported = arch.IsRISCVISASupported

// MockIsRISCVISASupported mocks the return value of the function checking
// if a RISCV ISA is supported on the running system, and returns a function
// to restore to the current value.
func MockIsRISCVISASupported(err string) (restore func()) {
	// If no error is expected, should return nil
	var returnErr error
	if err == "" {
		returnErr = nil
	} else {
		returnErr = fmt.Errorf(err)
	}

	originalIsRISCVISASupported := IsRISCVISASupported
	IsRISCVISASupported = func(_ string) error { return returnErr }

	return func() { IsRISCVISASupported = originalIsRISCVISASupported }
}

func validateAssumedRISCVISA(isa string) error {
	return IsRISCVISASupported(isa)
}
