// -*- Mode: Go; indent-tabs-mode: t -*-
//

//go:build !linux || !riscv64

package naming

import (
	"fmt"
	"runtime"
)

func validateAssumesRiscvISA(_ string) error {
	// Shouldn't get here, error out just in case
	return fmt.Errorf("cannot validate RiscV ISA support for OS and architecture combination: %s, %s", runtime.GOOS, runtime.GOARCH)
}
