// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapd_kernel_probe

import (
	"os"
)

var (
	Run               = run
	RunConfinedHelper = runConfinedHelper
)

func MockOsWriteFile(f func(string, []byte, os.FileMode) error) (restore func()) {
	old := osWriteFile
	osWriteFile = f
	return func() {
		osWriteFile = old
	}
}

func MockSyscallExec(f func(argv0 string, argv []string, envv []string) error) (restore func()) {
	old := syscallExec
	syscallExec = f
	return func() {
		syscallExec = old
	}
}

func MockProcThreadSelfAttrExec(new string) (restore func()) {
	old := procThreadSelfAttrExec
	procThreadSelfAttrExec = new
	return func() {
		procThreadSelfAttrExec = old
	}
}
