// -*- Mode: Go; indent-tabs-mode: t -*-
//
//go:build linux && riscv64

package naming

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Extensions to be retrieved via unix.RISCV_HWPROBE_KEY_IMA_EXT_0
// These keys are mandatory for RVA23 according to the docs https://github.com/riscv/riscv-profiles/blob/main/src/rva23-profile.adoc#rva23u64-profile
// but are not found as a RISCV_HWPROBE_EXT_<NAME> in the kernel sources for 6.17,
// These don't even have a "feature flag" RISCV_ISA_EXT_<NAME> inside the current resolute kernel that shows
// that they are supported. (The only one is Zicsr, not listed here, that is implied by "f" so it
// doesn't require a separate probe key).
// These keys also do not appear in the latest (2025-12-01) Risc-V ISA manual (https://github.com/riscv/riscv-isa-manual/releases/tag/riscv-isa-release-fcd76ed-2025-12-01)
// nor in the (archived) riscv-v-spec repository (https://github.com/riscvarchive/riscv-v-spec)
//   - Ziccif
//   - Ziccrse (has _ISA_ flag but not _HWPROBE_ flag)
//   - Ziccamoa
//   - Zicclsm
//   - Za64rs
//   - Zic64b
//   - Zicbop (has _ISA_ flag but not _HWPROBE_ flag)
//
// Zicbop is also the only one mentioned in the documentation in a meaningful way
// Note: This list is declared here since the one in golang.org/x/sys/unix/ztypes_linux_riscv64.go is not complete, and only
// contains the values up to RISCV_HWPROBE_EXT_ZIHINTPAUSE. Some of the constants missing in that file are
// required for RVA23, e.g. Zcmop
const (
	RISCV_HWPROBE_IMA_FD          uint64 = 1 << 0
	RISCV_HWPROBE_IMA_C           uint64 = 1 << 1
	RISCV_HWPROBE_IMA_V           uint64 = 1 << 2
	RISCV_HWPROBE_EXT_ZBA         uint64 = 1 << 3
	RISCV_HWPROBE_EXT_ZBB         uint64 = 1 << 4
	RISCV_HWPROBE_EXT_ZBS         uint64 = 1 << 5
	RISCV_HWPROBE_EXT_ZICBOZ      uint64 = 1 << 6
	RISCV_HWPROBE_EXT_ZBC         uint64 = 1 << 7
	RISCV_HWPROBE_EXT_ZBKB        uint64 = 1 << 8
	RISCV_HWPROBE_EXT_ZBKC        uint64 = 1 << 9
	RISCV_HWPROBE_EXT_ZBKX        uint64 = 1 << 10
	RISCV_HWPROBE_EXT_ZKND        uint64 = 1 << 11
	RISCV_HWPROBE_EXT_ZKNE        uint64 = 1 << 12
	RISCV_HWPROBE_EXT_ZKNH        uint64 = 1 << 13
	RISCV_HWPROBE_EXT_ZKSED       uint64 = 1 << 14
	RISCV_HWPROBE_EXT_ZKSH        uint64 = 1 << 15
	RISCV_HWPROBE_EXT_ZKT         uint64 = 1 << 16
	RISCV_HWPROBE_EXT_ZVBB        uint64 = 1 << 17
	RISCV_HWPROBE_EXT_ZVBC        uint64 = 1 << 18
	RISCV_HWPROBE_EXT_ZVKB        uint64 = 1 << 19
	RISCV_HWPROBE_EXT_ZVKG        uint64 = 1 << 20
	RISCV_HWPROBE_EXT_ZVKNED      uint64 = 1 << 21
	RISCV_HWPROBE_EXT_ZVKNHA      uint64 = 1 << 22
	RISCV_HWPROBE_EXT_ZVKNHB      uint64 = 1 << 23
	RISCV_HWPROBE_EXT_ZVKSED      uint64 = 1 << 24
	RISCV_HWPROBE_EXT_ZVKSH       uint64 = 1 << 25
	RISCV_HWPROBE_EXT_ZVKT        uint64 = 1 << 26
	RISCV_HWPROBE_EXT_ZFH         uint64 = 1 << 27
	RISCV_HWPROBE_EXT_ZFHMIN      uint64 = 1 << 28
	RISCV_HWPROBE_EXT_ZIHINTNTL   uint64 = 1 << 29
	RISCV_HWPROBE_EXT_ZVFH        uint64 = 1 << 30
	RISCV_HWPROBE_EXT_ZVFHMIN     uint64 = 1 << 31
	RISCV_HWPROBE_EXT_ZFA         uint64 = 1 << 32
	RISCV_HWPROBE_EXT_ZTSO        uint64 = 1 << 33
	RISCV_HWPROBE_EXT_ZACAS       uint64 = 1 << 34
	RISCV_HWPROBE_EXT_ZICOND      uint64 = 1 << 35
	RISCV_HWPROBE_EXT_ZIHINTPAUSE uint64 = 1 << 36
	RISCV_HWPROBE_EXT_ZVE32X      uint64 = 1 << 37
	RISCV_HWPROBE_EXT_ZVE32F      uint64 = 1 << 38
	RISCV_HWPROBE_EXT_ZVE64X      uint64 = 1 << 39
	RISCV_HWPROBE_EXT_ZVE64F      uint64 = 1 << 40
	RISCV_HWPROBE_EXT_ZVE64D      uint64 = 1 << 41
	RISCV_HWPROBE_EXT_ZIMOP       uint64 = 1 << 42
	RISCV_HWPROBE_EXT_ZCA         uint64 = 1 << 43
	RISCV_HWPROBE_EXT_ZCB         uint64 = 1 << 44
	RISCV_HWPROBE_EXT_ZCD         uint64 = 1 << 45
	RISCV_HWPROBE_EXT_ZCF         uint64 = 1 << 46
	RISCV_HWPROBE_EXT_ZCMOP       uint64 = 1 << 47
	RISCV_HWPROBE_EXT_ZAWRS       uint64 = 1 << 48
	RISCV_HWPROBE_EXT_SUPM        uint64 = 1 << 49
	RISCV_HWPROBE_EXT_ZICNTR      uint64 = 1 << 50
	RISCV_HWPROBE_EXT_ZIHPM       uint64 = 1 << 51
	RISCV_HWPROBE_EXT_ZFBFMIN     uint64 = 1 << 52
	RISCV_HWPROBE_EXT_ZVFBFMIN    uint64 = 1 << 53
	RISCV_HWPROBE_EXT_ZVFBFWMA    uint64 = 1 << 54
	RISCV_HWPROBE_EXT_ZICBOM      uint64 = 1 << 55
	RISCV_HWPROBE_EXT_ZAAMO       uint64 = 1 << 56
	RISCV_HWPROBE_EXT_ZALRSC      uint64 = 1 << 57
	RISCV_HWPROBE_EXT_ZABHA       uint64 = 1 << 58
)

// Define extension descriptions
type extDesc struct {
	Key      uint64
	Text     string
	Required bool
}

// Define extensions
// TODO potential improvement, remove all non-required extensions, and
// remove the property from the struct
//
// The value of the "Required" field for all the entries comes from the
// "example implementation" at https://github.com/xypron/hwprobe/blob/main/go/hwprobe.go
// The comments at the end of the line explain why the item is marked as Required in that implementation.
// If the comment starts with "required", the extension is explicitly required by the spec, either directly
// or through implication/shorthands.
// Otherwise, the item was "Required" only in the example implementation, and a justification was
// found by looking at the Risc-V ISA specs for dependencies, supersetting, etc.
// The only explicit change that was done compared to the example implementation is setting the "Zve{32,64}"
// as "not required", since they are meant to be used by "Embedded Processors", and this would too-restrictive
var RiscVExtensions = []extDesc{
	{Key: RISCV_HWPROBE_IMA_FD, Text: "F and D", Required: true},    // required
	{Key: RISCV_HWPROBE_IMA_C, Text: "C", Required: true},           // required
	{Key: RISCV_HWPROBE_IMA_V, Text: "V", Required: true},           // required
	{Key: RISCV_HWPROBE_EXT_ZBA, Text: "Zba", Required: true},       // required, from composite B
	{Key: RISCV_HWPROBE_EXT_ZBB, Text: "Zbb", Required: true},       // required, from composite B
	{Key: RISCV_HWPROBE_EXT_ZBS, Text: "Zbs", Required: true},       // required, from composite B
	{Key: RISCV_HWPROBE_EXT_ZICBOZ, Text: "Zicboz", Required: true}, // required
	{Key: RISCV_HWPROBE_EXT_ZBC, Text: "Zbc", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZBKB, Text: "Zbkb", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZBKC, Text: "Zbkc", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZBKX, Text: "Zbkx", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZKND, Text: "Zknd", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZKNE, Text: "Zkne", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZKNH, Text: "Zknh", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZKSED, Text: "Zksed", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZKSH, Text: "Zksh", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZKT, Text: "Zkt", Required: true},   // required
	{Key: RISCV_HWPROBE_EXT_ZVBB, Text: "Zvbb", Required: true}, // required
	{Key: RISCV_HWPROBE_EXT_ZVBC, Text: "Zvbc", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVKB, Text: "Zvkb", Required: true}, // supersetted by Zvbb
	{Key: RISCV_HWPROBE_EXT_ZVKG, Text: "Zvkg", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVKNED, Text: "Zvkned", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVKNHA, Text: "Zvknha", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVKNHB, Text: "Zvknhb", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVKSED, Text: "Zvksed", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVKSH, Text: "Zvksh", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVKT, Text: "Zvkt", Required: true}, // required
	{Key: RISCV_HWPROBE_EXT_ZFH, Text: "Zfh", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZFHMIN, Text: "Zfhmin", Required: true},       // required
	{Key: RISCV_HWPROBE_EXT_ZIHINTNTL, Text: "Zihintntl", Required: true}, // required
	{Key: RISCV_HWPROBE_EXT_ZVFH, Text: "Zvfh", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVFHMIN, Text: "Zvfhmin", Required: true}, // required
	{Key: RISCV_HWPROBE_EXT_ZFA, Text: "Zfa", Required: true},         // required
	{Key: RISCV_HWPROBE_EXT_ZTSO, Text: "Ztso", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZACAS, Text: "Zacas", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZICNTR, Text: "Zicntr", Required: true},           // required
	{Key: RISCV_HWPROBE_EXT_ZICOND, Text: "Zicond", Required: true},           // required
	{Key: RISCV_HWPROBE_EXT_ZIHINTPAUSE, Text: "Zihintpause", Required: true}, // required
	{Key: RISCV_HWPROBE_EXT_ZIHPM, Text: "Zihpm", Required: true},             // required
	{Key: RISCV_HWPROBE_EXT_ZVE32X, Text: "Zve32x", Required: false},          // for Embedded Processors. Changed to false
	{Key: RISCV_HWPROBE_EXT_ZVE32F, Text: "Zve32f", Required: false},          // ^
	{Key: RISCV_HWPROBE_EXT_ZVE64X, Text: "Zve64x", Required: false},          // ^
	{Key: RISCV_HWPROBE_EXT_ZVE64F, Text: "Zve64f", Required: false},          // ^
	{Key: RISCV_HWPROBE_EXT_ZVE64D, Text: "Zfe64d", Required: false},          // ^
	{Key: RISCV_HWPROBE_EXT_ZIMOP, Text: "Zimop", Required: true},             // required
	{Key: RISCV_HWPROBE_EXT_ZCA, Text: "Zca", Required: true},                 // dependency of Zcmop
	{Key: RISCV_HWPROBE_EXT_ZCB, Text: "Zcb", Required: true},                 // required
	{Key: RISCV_HWPROBE_EXT_ZCD, Text: "Zcd", Required: true},                 // implied by C+D
	{Key: RISCV_HWPROBE_EXT_ZCF, Text: "Zcf", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZCMOP, Text: "Zcmop", Required: true},   // required
	{Key: RISCV_HWPROBE_EXT_ZAWRS, Text: "Zawrs", Required: true},   // required
	{Key: RISCV_HWPROBE_EXT_ZAAMO, Text: "Zaamo", Required: true},   // component of A extension
	{Key: RISCV_HWPROBE_EXT_ZALRSC, Text: "Zalrsc", Required: true}, // component of A extension
	{Key: RISCV_HWPROBE_EXT_SUPM, Text: "Supm", Required: true},     // required
	{Key: RISCV_HWPROBE_EXT_ZFBFMIN, Text: "Zfbfmin", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVFBFMIN, Text: "Zvfbfmin", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZVFBFWMA, Text: "Zvfbfwma", Required: false},
	{Key: RISCV_HWPROBE_EXT_ZICBOM, Text: "Zicbom", Required: true}, // required
	{Key: RISCV_HWPROBE_EXT_ZABHA, Text: "Zabha", Required: false},
}

var RISCVHWProbe = unix.RISCVHWProbe

func validateAssumesRiscvISA(isa string) error {
	// Only the RVA23 instruction set is currently supported for RiscV
	if isa != "rva23" {
		return fmt.Errorf("unsupported ISA for riscv64 architecture: %s", isa)
	}

	// Initialize probe_items array
	pairs := []unix.RISCVHWProbePairs{
		{Key: unix.RISCV_HWPROBE_KEY_BASE_BEHAVIOR},
		{Key: unix.RISCV_HWPROBE_KEY_IMA_EXT_0},
	}

	// Call the hwprobe syscall
	err := RISCVHWProbe(pairs, nil, 0)
	if err != nil {
		return fmt.Errorf("error while querying RVA23 extensions supported by CPU: %s", err)
	}

	// Check RISC-V base behavior
	if pairs[0].Value&unix.RISCV_HWPROBE_BASE_BEHAVIOR_IMA == 0 {
		return fmt.Errorf("missing base RISC-V support")
	}

	// Check extensions
	for _, ext := range RiscVExtensions {
		if pairs[1].Value&ext.Key == 0 && ext.Required {
			return fmt.Errorf("missing required RVA23 extension: %s", ext.Text)
		}
	}

	return nil
}
