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
)

type Bootenv16 struct {
	*bootloadertest.MockBootloader
}

// XXX ...
func MockUC16Bootenv(b *bootloadertest.MockBootloader) *Bootenv16 {
	return &Bootenv16{b}
}

// SetBootKernel sets the current boot kernel string. Should be
// something like "pc-kernel_1234.snap".
func (b16 Bootenv16) SetBootKernel(kernel string) {
	b16.SetBootVars(map[string]string{"snap_kernel": kernel})
}

// SetBootBase sets the current boot base string. Should be something
// like "core_1234.snap".
func (b16 Bootenv16) SetBootBase(base string) {
	b16.SetBootVars(map[string]string{"snap_core": base})
}

func (b16 Bootenv16) SetTryingDuringReboot() error {
	if b16.BootVars["snap_mode"] != "try" {
		return fmt.Errorf("bootloader must be in 'try' mode")
	}
	b16.BootVars["snap_mode"] = "trying"
	return nil
}

// SetRollbackAcrossReboot will simulate a rollback across reboots. This
// means that the bootloader had "snap_try_{core,kernel}" set but this
// boot failed. In this case the bootloader will clear
// "snap_try_{core,kernel}" and "snap_mode" which means the "old" kernel,core
// in "snap_{core,kernel}" will be used.
func (b16 Bootenv16) SetRollbackAcrossReboot() error {
	if b16.BootVars["snap_mode"] != "try" {
		return fmt.Errorf("rollback can only be simulated in 'try' mode")
	}
	if b16.BootVars["snap_core"] == "" && b16.BootVars["snap_kernel"] == "" {
		return fmt.Errorf("rollback can only be simulated if either snap_core or snap_kernel is set")
	}
	// clean try bootvars and snap_mode
	b16.BootVars["snap_mode"] = ""
	b16.BootVars["snap_try_core"] = ""
	b16.BootVars["snap_try_kernel"] = ""
	return nil
}

// XXX
type RunBootenv20 struct {
	*bootloadertest.MockExtractedRunKernelImageBootloader
}

// XXX ...
func MockUC20RunBootenv(b *bootloadertest.MockBootloader) *RunBootenv20 {
	return &RunBootenv20{b.WithExtractedRunKernelImage()}
}

// XXX distinguish kernel vs base
func (b20 RunBootenv20) SetTryingDuringReboot() error {
	if b20.BootVars["kernel_status"] != "try" {
		return fmt.Errorf("bootloader must be in 'try' mode")
	}
	b20.BootVars["kernel_status"] = "trying"
	return nil
}

// XXX distinguish kernel vs base
func (b20 RunBootenv20) SetRollbackAcrossReboot() error {
	if b20.BootVars["kernel_status"] != "try" {
		return fmt.Errorf("rollback can only be simulated in 'try' mode")
	}
	/* XXX if b20.BootVars["snap_core"] == "" && b20.BootVars["snap_kernel"] == "" {
		return fmt.Errorf("rollback can only be simulated if either snap_core or snap_kernel is set")
	}*/
	// clean try bootvars and snap_mode
	b20.BootVars["kernel_status"] = ""
	return nil
}
