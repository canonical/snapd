// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package boottest

import (
	"fmt"

	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/snap"
)

// Bootenv16 implements manipulating a UC16/18 boot env for testing.
type Bootenv16 struct {
	*bootloadertest.MockBootloader
	statusVar string
}

// MockUC16Bootenv wraps a mock bootloader for UC16/18 boot env
// manipulation.
func MockUC16Bootenv(b *bootloadertest.MockBootloader) *Bootenv16 {
	return &Bootenv16{
		MockBootloader: b,
		statusVar:      "snap_mode",
	}
}

// SetBootKernel sets the current boot kernel string. Should be
// something like "pc-kernel_1234.snap".
func (b16 Bootenv16) SetBootKernel(kernel string) {
	b16.SetBootVars(map[string]string{"snap_kernel": kernel})
}

// SetBootTryKernel sets the try boot kernel string. Should be
// something like "pc-kernel_1235.snap".
func (b16 Bootenv16) SetBootTryKernel(kernel string) {
	b16.SetBootVars(map[string]string{"snap_try_kernel": kernel})
}

// SetBootBase sets the current boot base string. Should be something
// like "core_1234.snap".
func (b16 Bootenv16) SetBootBase(base string) {
	b16.SetBootVars(map[string]string{"snap_core": base})
}

// SetTryingDuringReboot indicates that new kernel or base are being tried
// same as done by bootloader config.
func (b16 Bootenv16) SetTryingDuringReboot(which []snap.Type) error {
	if b16.BootVars[b16.statusVar] != "try" {
		return fmt.Errorf("bootloader must be in 'try' mode")
	}
	b16.BootVars[b16.statusVar] = "trying"
	return nil
}

func includesType(which []snap.Type, t snap.Type) bool {
	for _, t1 := range which {
		if t1 == t {
			return true
		}
	}
	return false
}

func exactlyType(which []snap.Type, t snap.Type) bool {
	if len(which) != 1 {
		return false
	}
	if which[0] != t {
		return false
	}
	return true
}

// SetRollbackAcrossReboot will simulate a rollback across reboots. This
// means that the bootloader had "snap_try_{core,kernel}" set but this
// boot failed. In this case the bootloader will clear
// "snap_try_{core,kernel}" and "snap_mode" which means the "old" kernel,core
// in "snap_{core,kernel}" will be used. which indicates whether rollback
// applies to kernel, base or both.
func (b16 Bootenv16) SetRollbackAcrossReboot(which []snap.Type) error {
	if b16.BootVars[b16.statusVar] != "try" {
		return fmt.Errorf("rollback can only be simulated in 'try' mode")
	}
	rollbackBase := includesType(which, snap.TypeBase)
	rollbackKernel := includesType(which, snap.TypeKernel)
	if !rollbackBase && !rollbackKernel {
		return fmt.Errorf("rollback of either base or kernel must be requested")
	}
	if rollbackBase && b16.BootVars["snap_core"] == "" && b16.BootVars["snap_kernel"] == "" {
		return fmt.Errorf("base rollback can only be simulated if snap_core is set")
	}
	if rollbackKernel && b16.BootVars["snap_kernel"] == "" {
		return fmt.Errorf("kernel rollback can only be simulated if snap_kernel is set")
	}
	// clean only statusVar - the try vars will be cleaned by snapd NOT by the
	// bootloader
	b16.BootVars[b16.statusVar] = ""
	return nil
}

// RunBootenv20 implements manipulating a UC20 run-mode boot env for
// testing.
type RunBootenv20 struct {
	*bootloadertest.MockExtractedRunKernelImageBootloader
}

// MockUC20EnvRefExtractedKernelRunBootenv wraps a mock bootloader for UC20 run-mode boot
// env manipulation.
func MockUC20EnvRefExtractedKernelRunBootenv(b *bootloadertest.MockBootloader) *Bootenv16 {
	// TODO:UC20: implement this w/o returning Bootenv16 because that doesn't
	//            make a lot of sense to the caller
	return &Bootenv16{
		MockBootloader: b,
		statusVar:      "kernel_status",
	}
}

// MockUC20RunBootenv wraps a mock bootloader for UC20 run-mode boot
// env manipulation.
func MockUC20RunBootenv(b *bootloadertest.MockBootloader) *RunBootenv20 {
	return &RunBootenv20{b.WithExtractedRunKernelImage()}
}

// TODO:UC20: expose actual snap-boostrap logic for testing

// SetTryingDuringReboot indicates that new kernel or base are being tried
// same as done by bootloader config.
func (b20 RunBootenv20) SetTryingDuringReboot(which []snap.Type) error {
	if !exactlyType(which, snap.TypeKernel) {
		return fmt.Errorf("for now only kernel related simulation is supported")
	}
	if b20.BootVars["kernel_status"] != "try" {
		return fmt.Errorf("bootloader must be in 'try' mode")
	}
	b20.BootVars["kernel_status"] = "trying"
	return nil
}

// SetRollbackAcrossReboot will simulate a rollback across reboots for either
// a new base or kernel or both, as indicated by which.
// TODO: only kernel is supported for now.
func (b20 RunBootenv20) SetRollbackAcrossReboot(which []snap.Type) error {
	if !exactlyType(which, snap.TypeKernel) {
		return fmt.Errorf("for now only kernel related simulation is supported")
	}
	if b20.BootVars["kernel_status"] != "try" {
		return fmt.Errorf("rollback can only be simulated in 'try' mode")
	}
	// clean try bootvars and snap_mode
	b20.BootVars["kernel_status"] = ""
	return nil
}

// RunBootenvNotScript20 implements manipulating a UC20 run-mode boot
// env for testing, for the case of not scriptable bootloader
// (i.e. piboot).
type RunBootenvNotScript20 struct {
	*bootloadertest.MockExtractedRecoveryKernelNotScriptableBootloader
}

// MockUC20RunBootenvNotScript wraps a mock bootloader for UC20
// run-mode boot env manipulation, for the case of not scriptable
// bootloader (i.e. piboot).
func MockUC20RunBootenvNotScript(b *bootloadertest.MockBootloader) *RunBootenvNotScript20 {
	return &RunBootenvNotScript20{b.RecoveryAware().WithNotScriptable().WithExtractedRecoveryKernel()}
}
