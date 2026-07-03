// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build linux

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

package main

import "github.com/snapcore/snapd/testutil"

// Main exposes the unexported main() for testing.
var Main = main

// MockToolMains replaces the toolMains dispatch map for the duration of a test.
func MockToolMains(m map[string]func()) (restore func()) {
	return testutil.Mock(&toolMains, m)
}

// MockDaemonMain replaces the daemon entry point for the duration of a test.
func MockDaemonMain(f func()) (restore func()) {
	return testutil.Mock(&daemonMain, f)
}

// MockCLIMain replaces the CLI entry point for the duration of a test.
func MockCLIMain(f func()) (restore func()) {
	return testutil.Mock(&cliMain, f)
}
