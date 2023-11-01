// -*- Mode: Go; indent-tabs-mode: t -*-
//
//go:build !riscv64

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

// this extraDpkgArchToScmpArch does not have riscv64 constant, when
// building on non-riscv64 archtictures with an old seccomp library.
// once all distros upgrade to the new seccomp library we can drop
// this and riscv64 specific files and fold things back into
// DpkgArchToScmpArch() without this function
func extraDpkgArchToScmpArch(dpkgArch string) seccomp.ScmpArch {
	panic(fmt.Sprintf("cannot map dpkg arch %q to a seccomp arch", dpkgArch))
}
