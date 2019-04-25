// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package seccomp

import (
	"github.com/mvo5/libseccomp-golang"
)

var GoSeccompCanActLog = goSeccompCanActLogImpl

// GoSeccompCanActLogImpl verifies if golang-seccomp supports the ActLog action
func goSeccompCanActLogImpl() bool {
	// Guess at the ActLog value by adding one to ActAllow and then verify
	// that the string representation is what we expect for ActLog. The
	// value and string is defined in
	// https://github.com/seccomp/libseccomp-golang/pull/29.
	//
	// Ultimately, the fix for this workaround is to be able to use the
	// GetApi() function created in the PR above. It'll tell us if the
	// kernel, libseccomp, and libseccomp-golang all support ActLog.
	var actLog seccomp.ScmpAction = seccomp.ActAllow + 1

	if actLog.String() == "Action: Log system call" {
		return true
	}
	return false
}

func MockGoSeccompCanActLog(f func() bool) (restore func()) {
	old := GoSeccompCanActLog
	GoSeccompCanActLog = f
	return func() {
		GoSeccompCanActLog = old
	}
}
