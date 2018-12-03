// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package features

import (
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// SnapdFeature is a named feature that may be on or off.
type SnapdFeature int

const (
	// Layouts controls availability of snap layouts.
	Layouts SnapdFeature = iota
	// ParallelInstances controls availability installing a snap multiple times.
	ParallelInstances
	// Hotplug controls availability of dynamically creating slots based on system hardware.
	Hotplug
	// SnapdSnap controls possibility of installing the snapd snap.
	SnapdSnap
	// PerUserMountNamespace controls the persistence of per-user mount namespaces.
	PerUserMountNamespace
)

// KnownFeatures returns the list of all known features.
func KnownFeatures() []SnapdFeature {
	return []SnapdFeature{Layouts, ParallelInstances, Hotplug, SnapdSnap, PerUserMountNamespace}
}

// String returns the name of a snapd feature.
func (f SnapdFeature) String() string {
	// The constants here must be synchronized with cmd/libsnap-confine-private/feature.c
	switch f {
	case Layouts:
		return "layouts"
	case ParallelInstances:
		return "parallel-instances"
	case Hotplug:
		return "hotplug"
	case SnapdSnap:
		return "snapd-snap"
	case PerUserMountNamespace:
		return "per-user-mount-namespace"
	}
	panic(fmt.Sprintf("unknown feature flag code %d", f))
}

// IsEnabledByDefault returns true if a feature is enabled by default.
//
// A feature may be enabled or disabled with explicit state in snapd. If
// explicit state is absent the effective value is the implicit default
// computed by this function.
func (f SnapdFeature) IsEnabledByDefault() bool {
	switch f {
	case Layouts:
		return true
	}
	return false
}

// ControlFile returns the path of the file controlling the exported feature.
//
// Snapd considers the feature enabled if the file is present.
// The contents of the file are not important.
func (f SnapdFeature) ControlFile() string {
	return filepath.Join(dirs.FeaturesDir, f.String())
}

// IsEnabled checks if a given snapd feature is enabled.
func (f SnapdFeature) IsEnabled() bool {
	return osutil.FileExists(f.ControlFile())
}
