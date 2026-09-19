// -*- Mode: Go; indent-tabs-mode: t -*-
//
//go:build linux && riscv64

package arch

import (
	"golang.org/x/sys/unix"
)

// Re-defined like this because Mock functions have been moved to export_test.go
type RISCVHWProbePairs = unix.RISCVHWProbePairs

// Re-defined to allow mocking this
var RISCVHWProbe = unix.RISCVHWProbe

// Re-defined because only available for riscv64 architecture
const (
	RISCV_HWPROBE_KEY_BASE_BEHAVIOR int64  = unix.RISCV_HWPROBE_KEY_BASE_BEHAVIOR
	RISCV_HWPROBE_KEY_IMA_EXT_0     int64  = unix.RISCV_HWPROBE_KEY_IMA_EXT_0
	RISCV_HWPROBE_BASE_BEHAVIOR_IMA uint64 = unix.RISCV_HWPROBE_BASE_BEHAVIOR_IMA
	// RISCV_HWPROBE_KEY_IMA_EXT_1 is not yet defined in golang.org/x/sys/unix, so it
	// is re-defined here matching the value used in the Linux kernel sources.
	RISCV_HWPROBE_KEY_IMA_EXT_1 int64 = 16
)

type CPUSet = unix.CPUSet
