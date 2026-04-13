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

// VersionDistroPatch is the distribution-specific patch level (release number
// or revision) appended to the upstream version by distribution packaging.
// For example "0.fc42" for Fedora, "1" for a Debian revision, "1" for an Arch
// pkgrel. It is empty for unpackaged (development) builds and native Debian
// packages. The value is written by packaging scripts into version_generated.go
// (sourced from the upstream source tarball) and into data/info.
var VersionDistroPatch = ""

func MockVersion(version string) (restore func()) {
	old := Version
	Version = version
	return func() { Version = old }
}
