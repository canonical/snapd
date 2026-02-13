// -*- Mode: Go; indent-tabs-mode: t -*-
//

package arch

import (
	"fmt"
	"runtime"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
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
	RISCV_HWPROBE_EXT_ZALASR
	RISCV_HWPROBE_EXT_ZICBOP
	RISCV_HWPROBE_EXT_ZILSD
	RISCV_HWPROBE_EXT_ZCLSD
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
// All these keys do not have a "feature flag" RISCV_ISA_EXT_<NAME> (except for Ziccrse, and Zicsr)
// These keys also do not appear in the latest (2025-12-01) Risc-V ISA manual
// (https://github.com/riscv/riscv-isa-manual/releases/tag/riscv-isa-release-fcd76ed-2025-12-01)
// nor in the (archived) riscv-v-spec repository (https://github.com/riscvarchive/riscv-v-spec)
//
// Zicsr is missing from the list as it is implied by "f" so it doesn't require a separate probe key.

// Define extension descriptions
type extDesc struct {
	// Bitmask for the extension support bit
	Key uint64
	// Human-readable name of the extension
	Text string
	// Declares if the extension is required for RVA23 support
	Required bool
	// Declares in which kernel version the key was first introduced. "0" if before 6.11
	// or not required
	Since string
}

// Define extensions list
//
// The value of the "Required" field for all the entries is taken from
// [RVA23 Profiles Specification, Version 1.0, 2024-10-17].
// The comments at the end of the line explain why the item is marked as Required.
//
// [RVA23 Profiles Specification, Version 1.0, 2024-10-17]: https://docs.riscv.org/reference/profiles/rva23/_attachments/rva23-profile.pdf
var RiscVExtensions = []extDesc{
	{Key: RISCV_HWPROBE_IMA_FD, Text: "F and D", Required: true, Since: "0"},    // required
	{Key: RISCV_HWPROBE_IMA_C, Text: "C", Required: true, Since: "0"},           // required
	{Key: RISCV_HWPROBE_IMA_V, Text: "V", Required: true, Since: "0"},           // required
	{Key: RISCV_HWPROBE_EXT_ZBA, Text: "Zba", Required: true, Since: "0"},       // required, element of composite extension B
	{Key: RISCV_HWPROBE_EXT_ZBB, Text: "Zbb", Required: true, Since: "0"},       // required, element of composite extension B
	{Key: RISCV_HWPROBE_EXT_ZBS, Text: "Zbs", Required: true, Since: "0"},       // required, element of composite extension B
	{Key: RISCV_HWPROBE_EXT_ZICBOZ, Text: "Zicboz", Required: true, Since: "0"}, // required
	{Key: RISCV_HWPROBE_EXT_ZBC, Text: "Zbc", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZBKB, Text: "Zbkb", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZBKC, Text: "Zbkc", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZBKX, Text: "Zbkx", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZKND, Text: "Zknd", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZKNE, Text: "Zkne", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZKNH, Text: "Zknh", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZKSED, Text: "Zksed", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZKSH, Text: "Zksh", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZKT, Text: "Zkt", Required: true, Since: "0"},   // required
	{Key: RISCV_HWPROBE_EXT_ZVBB, Text: "Zvbb", Required: true, Since: "0"}, // required
	{Key: RISCV_HWPROBE_EXT_ZVBC, Text: "Zvbc", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVKB, Text: "Zvkb", Required: true, Since: "0"}, // required, supersetted by Zvbb
	{Key: RISCV_HWPROBE_EXT_ZVKG, Text: "Zvkg", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVKNED, Text: "Zvkned", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVKNHA, Text: "Zvknha", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVKNHB, Text: "Zvknhb", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVKSED, Text: "Zvksed", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVKSH, Text: "Zvksh", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVKT, Text: "Zvkt", Required: true, Since: "0"}, // required
	{Key: RISCV_HWPROBE_EXT_ZFH, Text: "Zfh", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZFHMIN, Text: "Zfhmin", Required: true, Since: "0"},       // required
	{Key: RISCV_HWPROBE_EXT_ZIHINTNTL, Text: "Zihintntl", Required: true, Since: "0"}, // required
	{Key: RISCV_HWPROBE_EXT_ZVFH, Text: "Zvfh", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVFHMIN, Text: "Zvfhmin", Required: true, Since: "0"}, // required
	{Key: RISCV_HWPROBE_EXT_ZFA, Text: "Zfa", Required: true, Since: "0"},         // required
	{Key: RISCV_HWPROBE_EXT_ZTSO, Text: "Ztso", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZACAS, Text: "Zacas", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZICNTR, Text: "Zicntr", Required: true, Since: "6.15"},        // required, since 6.15
	{Key: RISCV_HWPROBE_EXT_ZICOND, Text: "Zicond", Required: true, Since: "0"},           // required
	{Key: RISCV_HWPROBE_EXT_ZIHINTPAUSE, Text: "Zihintpause", Required: true, Since: "0"}, // required
	{Key: RISCV_HWPROBE_EXT_ZIHPM, Text: "Zihpm", Required: true, Since: "6.15"},          // required, since 6.15
	{Key: RISCV_HWPROBE_EXT_ZVE32X, Text: "Zve32x", Required: true, Since: "0"},           // In the kernel, implied by 'V'
	{Key: RISCV_HWPROBE_EXT_ZVE32F, Text: "Zve32f", Required: true, Since: "0"},           // ^
	{Key: RISCV_HWPROBE_EXT_ZVE64X, Text: "Zve64x", Required: true, Since: "0"},           // ^
	{Key: RISCV_HWPROBE_EXT_ZVE64F, Text: "Zve64f", Required: true, Since: "0"},           // ^
	{Key: RISCV_HWPROBE_EXT_ZVE64D, Text: "Zfe64d", Required: true, Since: "0"},           // ^
	{Key: RISCV_HWPROBE_EXT_ZIMOP, Text: "Zimop", Required: true, Since: "0"},             // required
	{Key: RISCV_HWPROBE_EXT_ZCA, Text: "Zca", Required: true, Since: "0"},                 // required, dependency of Zcmop
	{Key: RISCV_HWPROBE_EXT_ZCB, Text: "Zcb", Required: true, Since: "0"},                 // required
	{Key: RISCV_HWPROBE_EXT_ZCD, Text: "Zcd", Required: true, Since: "0"},                 // implied by C+D
	{Key: RISCV_HWPROBE_EXT_ZCF, Text: "Zcf", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZCMOP, Text: "Zcmop", Required: true, Since: "0"},      // required
	{Key: RISCV_HWPROBE_EXT_ZAWRS, Text: "Zawrs", Required: true, Since: "0"},      // required
	{Key: RISCV_HWPROBE_EXT_ZAAMO, Text: "Zaamo", Required: true, Since: "6.15"},   // required, component of A extension, since 6.15
	{Key: RISCV_HWPROBE_EXT_ZALRSC, Text: "Zalrsc", Required: true, Since: "6.15"}, // required, component of A extension, since 6.15
	{Key: RISCV_HWPROBE_EXT_SUPM, Text: "Supm", Required: true, Since: "6.13"},     // required, since 6.13
	{Key: RISCV_HWPROBE_EXT_ZFBFMIN, Text: "Zfbfmin", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVFBFMIN, Text: "Zvfbfmin", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZVFBFWMA, Text: "Zvfbfwma", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZICBOM, Text: "Zicbom", Required: true, Since: "6.15"}, // required, since 6.15
	{Key: RISCV_HWPROBE_EXT_ZABHA, Text: "Zabha", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZALASR, Text: "Zalasr", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZICBOP, Text: "Zicbop", Required: true, Since: "6.19"}, // required, since 6.19
	{Key: RISCV_HWPROBE_EXT_ZILSD, Text: "Zilsd", Required: false, Since: "0"},
	{Key: RISCV_HWPROBE_EXT_ZCLSD, Text: "Zclsd", Required: false, Since: "0"},
}

var KernelVersion = osutil.KernelVersion

// IsRISCVISASupported takes the name of a RISCV64 ISA, gathers the list of extensions
// supported by the running platform, and verifies if all extensions required for
// compliance with the input ISA are supported.
// If they are, nil is returned. Otherwise, the missing requirement is returned.
//
// Currently, only support for [RVA23] can be checked for.
//
// [RVA23]: https://github.com/riscv/riscv-profiles/blob/main/src/rva23-profile.adoc#rva23u64-profile
func IsRISCVISASupported(isa string) error {
	// If the architecture is not riscv64 we exit immediately
	if runtime.GOOS != "linux" || DpkgArchitecture() != "riscv64" {
		return fmt.Errorf("cannot validate RiscV ISA support while running on: %s, %s. Need linux, riscv64.", runtime.GOOS, DpkgArchitecture())
	}

	// Only the RVA23 instruction set is currently supported for RiscV
	if isa != "rva23" {
		return fmt.Errorf("unsupported ISA for riscv64 architecture: %s", isa)
	}

	// Initialize probe_items array
	pairs := []RISCVHWProbePairs{
		{Key: RISCV_HWPROBE_KEY_BASE_BEHAVIOR},
		{Key: RISCV_HWPROBE_KEY_IMA_EXT_0},
	}

	// Call the hwprobe syscall
	err := RISCVHWProbe(pairs, nil, 0)
	if err != nil {
		return fmt.Errorf("error while querying RVA23 extensions supported by CPU: %s", err)
	}

	// Check RISC-V base behavior
	if pairs[0].Value&RISCV_HWPROBE_BASE_BEHAVIOR_IMA == 0 {
		return fmt.Errorf("missing base RISC-V support")
	}

	// Retrieve running kernel version
	kernelVersion := KernelVersion()

	// Check extensions
	for _, ext := range RiscVExtensions {
		if pairs[1].Value&ext.Key == 0 && ext.Required {
			// Compare the running kernel version to the required one
			versionDifference, err := strutil.VersionCompare(kernelVersion, ext.Since)
			if err != nil {
				return fmt.Errorf("error comparing kernel versions: %s", err)
			}

			// If running kernel version is equal (0) to the required one, or more recent (1)
			// the extension is supposed to be supported and we return the error
			if versionDifference >= 0 {
				return fmt.Errorf("missing required RVA23 extension: %s", ext.Text)
			}
		}
	}

	return nil
}
