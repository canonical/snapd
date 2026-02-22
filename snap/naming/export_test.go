// -*- Mode: Go; indent-tabs-mode: t -*-
//

package naming

// MockIsRISCVISASupported mocks the return value of the function checking
// if a RISCV ISA is supported on the running system, and returns a function
// to restore to the current value.
func MockIsRISCVISASupported(newArchIsRISCVISASupported func(isa string) error) (restore func()) {
	originalIsRISCVISASupported := archIsRISCVISASupported
	archIsRISCVISASupported = newArchIsRISCVISASupported

	return func() { archIsRISCVISASupported = originalIsRISCVISASupported }
}
