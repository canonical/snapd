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

// Version and VersionDistroPatch are set at build time through one of two
// workflows:
//
//   - snapd.git → source tarball → downstream distro packaging:
//     packaging/pack-source generates snapdtool/version_generated.go (included
//     in the source tarball) with Version set to the upstream release version
//     and VersionDistroPatch = "". Each distribution's build rules then use sed
//     to patch VersionDistroPatch with the distro-specific suffix (e.g.
//     "~0.fc42", "~1", "-1"). mkversion.sh is not used in this path.
//
//   - snapd.git → snapcraft (upstream snap build):
//     build-aux/snap/snapcraft.yaml calls mkversion.sh during the build, which
//     generates version_generated.go from git history. VersionDistroPatch is
//     not set in this path.
//
// For developer (unpackaged) builds, running `go generate ./snapdtool` also
// calls mkversion.sh to produce version_generated.go locally.
var Version = "unknown"

// VersionDistroPatch is the distribution-specific patch level (release number
// or revision) appended to the upstream version by distribution packaging.
// The value includes any separator character, for example "~0.fc42" for Fedora,
// "~1" for an Arch pkgrel, or "-1" for a Debian revision. It is empty for
// unpackaged (development) builds and native Debian packages. The value is
// written by packaging scripts into version_generated.go (sourced from the
// upstream source tarball) and into data/info.
var VersionDistroPatch = ""

// FullVersion returns the full version string for display and version-tracking
// purposes, combining the upstream Version with the distribution-specific
// VersionDistroPatch (if set). When VersionDistroPatch is empty, it returns
// Version unchanged. VersionDistroPatch includes any separator character so
// that each distribution controls comparison order (e.g. "~0.fc42", "-1",
// "+b1").
//
// Note: use Version (not FullVersion) when comparing against snap package
// version strings (assumes assertions, info file, etc.) since those always carry
// only the upstream version component.
func FullVersion() string {
	return Version + VersionDistroPatch
}

func MockVersion(version, patch string) (restore func()) {
	oldVersion := Version
	oldPatch := VersionDistroPatch
	Version = version
	VersionDistroPatch = patch
	return func() {
		Version = oldVersion
		VersionDistroPatch = oldPatch
	}
}
