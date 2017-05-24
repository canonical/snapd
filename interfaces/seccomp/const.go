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

import "syscall"

// we could use cgo here to get the flags, but because we try to avoid cgo
// this table is added manually
var seccompSymbolTable = map[string]int{
	// from linux/quota.h:72
	"Q_SYNC":     0x800001,
	"Q_GETFMT":   0x800004,
	"Q_GETINFO":  0x800005,
	"Q_SETINFO":  0x800006,
	"Q_GETQUOTA": 0x800007,
	"Q_SETQUOTA": 0x800008,
	// from linux/dqblk_xfs.h
	"Q_XGETQUOTA": 0x5803,
	"Q_XGETQSTAT": 0x5805,

	"TIOCSTI": syscall.TIOCSTI,

	"quotactl": syscall.SYS_QUOTACTL,
}
