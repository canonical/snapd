// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

// Package snapdtool exposes version and related information, supports
// re-execution and inter-tool lookup/execution across all snapd
// tools.
package snapdtool

//go:generate mkversion.sh

// Version will be overwritten at build-time via mkversion.sh
var Version = "unknown"

func MockVersion(version string) (restore func()) {
	old := Version
	Version = version
	return func() { Version = old }
}
