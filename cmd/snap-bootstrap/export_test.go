// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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
	"io"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
)

var (
	Parser = parser
)

func MockBootstrapRun(f func(string, string, *bootstrap.Options) error) (restore func()) {
	oldBootstrapRun := bootstrapRun
	bootstrapRun = f
	return func() {
		bootstrapRun = oldBootstrapRun
	}
}

func MockProcCmdline(newPath string) (restore func()) {
	oldProcCmdline := procCmdline
	procCmdline = newPath
	return func() {
		procCmdline = oldProcCmdline
	}
}

func MockStdout(newStdout io.Writer) (restore func()) {
	oldStdout := stdout
	stdout = newStdout
	return func() {
		stdout = oldStdout
	}
}

func MockOsutilIsMounted(f func(path string) (bool, error)) (restore func()) {
	oldOsutilIsMounted := osutilIsMounted
	osutilIsMounted = f
	return func() {
		osutilIsMounted = oldOsutilIsMounted
	}
}

func MockRunMnt(newRunMnt string) (restore func()) {
	oldRunMnt := runMnt
	runMnt = newRunMnt
	return func() {
		runMnt = oldRunMnt
	}
}
