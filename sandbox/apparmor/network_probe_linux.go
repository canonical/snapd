//go:build linux

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

package apparmor

import "syscall"

// networkBugProbeCloneflags are the clone flags the probe process is started
// with. The probe runs in a private network namespace, in which it raises its
// own loopback interface, so that it works whether or not the host's loopback
// is up (e.g. early in boot) and does not depend on, or disturb, the host's
// network. Creating a network namespace requires CAP_SYS_ADMIN. It is a
// variable to allow mocking in tests, which may not run as root.
var networkBugProbeCloneflags uintptr = syscall.CLONE_NEWNET

func networkBugProbeSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Cloneflags: networkBugProbeCloneflags}
}
