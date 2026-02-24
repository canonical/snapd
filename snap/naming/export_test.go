// -*- Mode: Go; indent-tabs-mode: t -*-
//

package naming

// MockArchIsISASupportedByCPU mocks the return value of the function checking
// if a RISCV ISA is supported on the running system, and returns a function
// to restore to the current value.
func MockArchIsISASupportedByCPU(newArchisISASupportedByCPU func(isa string) error) (restore func()) {
	originalArchisISASupportedByCPU := archIsISASupportedByCPU
	archIsISASupportedByCPU = newArchisISASupportedByCPU

	return func() { archIsISASupportedByCPU = originalArchisISASupportedByCPU }
}
