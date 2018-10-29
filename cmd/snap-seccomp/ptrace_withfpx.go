// -*- Mode: Go; indent-tabs-mode: t -*-
// +build 386 amd64

/*
 * Copyright (C) 2018 Canonical Ltd
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

import "syscall"

func fpSeccompResolver(token string) (uint64, bool) {
	switch token {
	case "PTRACE_GETFPREGS":
		return syscall.PTRACE_GETFPREGS, true
	case "PTRACE_GETFPXREGS":
		return syscall.PTRACE_GETFPXREGS, true
	default:
		return 0, false
	}
}
