// -*- Mode: Go; indent-tabs-mode: t -*-
//
//go:build linux && riscv64

package naming

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Extensions to be retrieved via unix.RISCV_HWPROBE_KEY_IMA_EXT_0
// Note: This list is declared here since the one in golang.org/x/sys/unix/ztypes_linux_riscv64.go is not complete, and only
// contains the values up to RISCV_HWPROBE_EXT_ZIHINTPAUSE. Some of the constants missing in that file are
// required for RVA23, e.g. Zcmop
const (
	RISCV_HWPROBE_IMA_FD uint64 = 1 << iota
	RISCV_HWPROBE_IMA_C
	RISCV_HWPROBE_IMA_V
	RISCV_HWPROBE_EXT_ZBA
	RISCV_HWPROBE_EXT_ZBB
	RISCV_HWPROBE_EXT_ZBS
	RISCV_HWPROBE_EXT_ZICBOZ
	RISCV_HWPROBE_EXT_ZBC
	RISCV_HWPROBE_EXT_ZBKB
	RISCV_HWPROBE_EXT_ZBKC
	RISCV_HWPROBE_EXT_ZBKX
	RISCV_HWPROBE_EXT_ZKND
	RISCV_HWPROBE_EXT_ZKNE
	RISCV_HWPROBE_EXT_ZKNH
	RISCV_HWPROBE_EXT_ZKSED
	RISCV_HWPROBE_EXT_ZKSH
	RISCV_HWPROBE_EXT_ZKT
	RISCV_HWPROBE_EXT_ZVBB
	RISCV_HWPROBE_EXT_ZVBC
	RISCV_HWPROBE_EXT_ZVKB
	RISCV_HWPROBE_EXT_ZVKG
	RISCV_HWPROBE_EXT_ZVKNED
	RISCV_HWPROBE_EXT_ZVKNHA
	RISCV_HWPROBE_EXT_ZVKNHB
	RISCV_HWPROBE_EXT_ZVKSED
	RISCV_HWPROBE_EXT_ZVKSH
	RISCV_HWPROBE_EXT_ZVKT
	RISCV_HWPROBE_EXT_ZFH
	RISCV_HWPROBE_EXT_ZFHMIN
	RISCV_HWPROBE_EXT_ZIHINTNTL
	RISCV_HWPROBE_EXT_ZVFH
	RISCV_HWPROBE_EXT_ZVFHMIN
	RISCV_HWPROBE_EXT_ZFA
	RISCV_HWPROBE_EXT_ZTSO
	RISCV_HWPROBE_EXT_ZACAS
	RISCV_HWPROBE_EXT_ZICOND
	RISCV_HWPROBE_EXT_ZIHINTPAUSE
	RISCV_HWPROBE_EXT_ZVE32X
	RISCV_HWPROBE_EXT_ZVE32F
	RISCV_HWPROBE_EXT_ZVE64X
	RISCV_HWPROBE_EXT_ZVE64F
	RISCV_HWPROBE_EXT_ZVE64D
	RISCV_HWPROBE_EXT_ZIMOP
	RISCV_HWPROBE_EXT_ZCA
	RISCV_HWPROBE_EXT_ZCB
	RISCV_HWPROBE_EXT_ZCD
	RISCV_HWPROBE_EXT_ZCF
	RISCV_HWPROBE_EXT_ZCMOP
	RISCV_HWPROBE_EXT_ZAWRS
	RISCV_HWPROBE_EXT_SUPM
	RISCV_HWPROBE_EXT_ZICNTR
	RISCV_HWPROBE_EXT_ZIHPM
	RISCV_HWPROBE_EXT_ZFBFMIN
	RISCV_HWPROBE_EXT_ZVFBFMIN
	RISCV_HWPROBE_EXT_ZVFBFWMA
	RISCV_HWPROBE_EXT_ZICBOM
	RISCV_HWPROBE_EXT_ZAAMO
	RISCV_HWPROBE_EXT_ZALRSC
	RISCV_HWPROBE_EXT_ZABHA
)

// The above list contains all extension keys that are in the kernel sources for version 6.17.
//
// The following keys are not present in the above list, but mandatory for RVA23 according to
// the specification
// https://github.com/riscv/riscv-profiles/blob/main/src/rva23-profile.adoc#rva23u64-profile,
// but are not queriable to the kernel as RISCV_HWPROBE_EXT_<NAME>:
//   - Ziccif
//   - Ziccrse
//   - Ziccamoa
//   - Zicclsm
//   - Za64rs
//   - Zic64b
//   - Zicbop
// All these keys do not have a "feature flag" RISCV_ISA_EXT_<NAME> (except for Ziccrse, Zicbop
// and Zicsr).
// These keys also do not appear in the latest (2025-12-01) Risc-V ISA manual
// (https://github.com/riscv/riscv-isa-manual/releases/tag/riscv-isa-release-fcd76ed-2025-12-01)
// nor in the (archived) riscv-v-spec repository (https://github.com/riscvarchive/riscv-v-spec)
//
// Zicbop is the only one mentioned in the documentation in a meaningful way, but no hints are
// given as to why RISCV_HWPROBE_EXT_ZICBOP does not exist.
//
// Zicsr is missing from the list as it is implied by "f" so it doesn't require a separate probe key.

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
