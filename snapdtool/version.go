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

// UpstreamVersion and DownstreamVersionSuffix are set at build time through
// one of three intended workflows:
//
//   - snapd.git -> source tarball -> downstream distro packaging:
//     packaging/pack-source generates snapdtool/version_generated.go (included
//     in the source tarball) with UpstreamVersion set to the upstream release
//     version and DownstreamVersionSuffix = "".
//
//     Each distribution's build rules then use sed to patch DownstreamVersion
//     with the distro-specific suffix (e.g. "~0.fc42", "~1", "-1").
//     mkversion.sh is not used in this path.
//
//   - snapd.git -> snapcraft flow calls mkversion.sh during the build, which
//     generates version_generated.go from git history. DownstreamVersionSuffix
//     is not used.
//
//   - snapd.git -> ./mkversion.sh -> go build - developer builds overwrite
//     the version, typically when building the snap for testing.
var UpstreamVersion = "unknown"

// DownstreamVersionSuffix is the distribution-specific version suffix (release
// number or revision) appended to the upstream version by distribution
// packaging. The value includes any separator character, for example "~0.fc42"
// for Fedora, "~1" for an Arch pkgrel, or "-1" for a Debian revision. It is
// empty for unpackaged (development) builds and native Debian packages. The
// value is written by packaging scripts into version_generated.go (sourced
// from the upstream source tarball) and into data/info.
var DownstreamVersionSuffix = ""

// FullVersion returns the full version string for display and version-tracking
// purposes, combining the upstream Version with the distribution-specific
// DownstreamVersionSuffix (if set).
func FullVersion() string {
	return UpstreamVersion + DownstreamVersionSuffix
}

func MockVersion(upstream, downstreamSuffix string) (restore func()) {
	oldUpstream := UpstreamVersion
	oldDownstreamSuffix := DownstreamVersionSuffix
	UpstreamVersion = upstream
	DownstreamVersionSuffix = downstreamSuffix
	return func() {
		UpstreamVersion = oldUpstream
		DownstreamVersionSuffix = oldDownstreamSuffix
	}
}
