// -*- Mode: Go; indent-tabs-mode: t -*-
//

//go:build !linux || !riscv64

package arch

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

func IsRISCVISASupported(_ string) error {
	// Shouldn't get here, error out just in case
	return fmt.Errorf("cannot validate RiscV ISA support while running on: %s, %s", runtime.GOOS, runtime.GOARCH)
}

// Re-defined like this because Mock functions have been moved to export_test.go
type RISCVHWProbePairs struct {
	Key   int64
	Value uint64
}

// Re-defined to allow mocking this
var RISCVHWProbe = func(pairs []RISCVHWProbePairs, set *unix.CPUSet, flags uint) (err error) { return nil }

var KernelVersion = func() string { return "0" }
