// -*- Mode: Go; indent-tabs-mode: t -*-
//

//go:build !linux || !riscv64

package arch

import (
	"fmt"
	"runtime"
)

func IsRISCVISASupported(_ string) error {
	// Shouldn't get here, error out just in case
	return fmt.Errorf("cannot validate RiscV ISA support while running on: %s, %s", runtime.GOOS, runtime.GOARCH)
}
