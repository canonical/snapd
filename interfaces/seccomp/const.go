// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package seccomp implements integration between snappy and
// ubuntu-core-launcher around seccomp.
//
// Snappy creates so-called seccomp profiles for each application (for each
// snap) present in the system.  Upon each execution of ubuntu-core-launcher,
// the profile is read and "compiled" to an eBPF program and injected into the
// kernel for the duration of the execution of the process.
//
// There is no binary cache for seccomp, each time the launcher starts an
// application the profile is parsed and re-compiled.
//
// The actual profiles are stored in /var/lib/snappy/seccomp/profiles.
// This directory is hard-coded in ubuntu-core-launcher.
package seccomp

// #include<linux/quota.h>
// #include<linux/dqblk_xfs.h>
// #include<asm-generic/ioctls.h>
import "C"

var seccompSymbolTable = map[string]int{
	// from linux/quota.h:72
	"Q_SYNC":      C.Q_SYNC,
	"Q_GETFMT":    C.Q_GETFMT,
	"Q_GETINFO":   C.Q_GETINFO,
	"Q_SETINFO":   C.Q_SETINFO,
	"Q_GETQUOTA":  C.Q_GETQUOTA,
	"Q_SETQUOTA":  C.Q_SETQUOTA,
	"Q_XGETQUOTA": C.Q_XGETQUOTA,
	"Q_XGETQSTAT": C.Q_XGETQSTAT,

	"TIOCSTI": C.TIOCSTI,
}
