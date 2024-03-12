// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build faultinject

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

package osutil

import (
	"io"
)

func MockInjectSysroot(m string) (restore func()) {
	oldInjectSysroot := injectSysroot
	injectSysroot = m
	return func() {
		injectSysroot = oldInjectSysroot
	}
}

func MockForeverLoop(f func()) (restore func()) {
	oldForeverLoop := foreverLoop
	foreverLoop = f
	return func() {
		foreverLoop = oldForeverLoop
	}
}

func MockStderr(w io.Writer) (restore func()) {
	oldStderr := stderr
	stderr = w
	return func() {
		stderr = oldStderr
	}
}
