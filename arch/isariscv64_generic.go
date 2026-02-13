// -*- Mode: Go; indent-tabs-mode: t -*-
//

//go:build !linux || !riscv64

package arch

// Re-defined like this because Mock functions have been moved to export_test.go
type RISCVHWProbePairs struct {
	Key   int64
	Value uint64
}

// Re-defined to allow mocking this
var RISCVHWProbe = func(pairs []RISCVHWProbePairs, set *CPUSet, flags uint) (err error) { return nil }

// Re-defined and assigned values matching the unix.* constants for tests
const (
	RISCV_HWPROBE_KEY_BASE_BEHAVIOR int64  = 0x3
	RISCV_HWPROBE_KEY_IMA_EXT_0     int64  = 0x4
	RISCV_HWPROBE_BASE_BEHAVIOR_IMA uint64 = 0x1
)

type CPUSet [0]uint64
