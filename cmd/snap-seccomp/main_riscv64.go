// -*- Mode: Go; indent-tabs-mode: t -*-
//
//go:build riscv64

/*
 * Copyright (C) 2021 Canonical Ltd
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

package main

import (
	"fmt"

	"github.com/seccomp/libseccomp-golang"
)

// this extraDpkgArchToScmpArch uses riscv64 constant, when building
// on riscv64 architecture which requires newer snapshot of libseccomp
// library. Once all distros have newer libseccomp golang library,
// this portion can be just folded into the DpkgArchToScmArch()
// function to be compiled on all architecutres.
func extraDpkgArchToScmpArch(dpkgArch string) seccomp.ScmpArch {
	switch dpkgArch {
	case "riscv64":
		return seccomp.ArchRISCV64
	}
	panic(fmt.Sprintf("cannot map dpkg arch %q to a seccomp arch", dpkgArch))
}
