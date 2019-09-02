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
	// RefreshAppAwareness controls refresh being aware of running applications.
	RefreshAppAwareness
	// lastFeature is the final known feature, it is only used for testing.
	lastFeature
)

// KnownFeatures returns the list of all known features.
func KnownFeatures() []SnapdFeature {
	features := make([]SnapdFeature, int(lastFeature))
	for i := range features {
		features[i] = SnapdFeature(i)
	}
	return features
}

// featureNames maps feature constant to stable string representation.
// The constants here must be synchronized with cmd/libsnap-confine-private/feature.c
var featureNames = map[SnapdFeature]string{
	Layouts:               "layouts",
	ParallelInstances:     "parallel-instances",
	Hotplug:               "hotplug",
	SnapdSnap:             "snapd-snap",
	PerUserMountNamespace: "per-user-mount-namespace",
	RefreshAppAwareness:   "refresh-app-awareness",
}

// featuresEnabledWhenUnset contains a set of features that are enabled when not explicitly configured.
var featuresEnabledWhenUnset = map[SnapdFeature]bool{
	Layouts: true,
}

// featuresExported contains a set of features that are exported outside of snapd.
var featuresExported = map[SnapdFeature]bool{
	PerUserMountNamespace: true,
	RefreshAppAwareness:   true,
	ParallelInstances:     true,
}

// String returns the name of a snapd feature.
// The function panics for bogus feature values.
func (f SnapdFeature) String() string {
	if name, ok := featureNames[f]; ok {
		return name
	}
	panic(fmt.Sprintf("unknown feature flag code %d", f))
}

// IsEnabledWhenUnset returns true if a feature is enabled when not set.
//
// A feature may be enabled or disabled with explicit state in snapd. If
// explicit state is absent the effective value is the implicit default
// computed by this function.
func (f SnapdFeature) IsEnabledWhenUnset() bool {
	return featuresEnabledWhenUnset[f]
}

// IsExported returns true if a feature is copied from snapd state to a feature file.
//
// Certain features are available outside of snapd internal state and visible as control
// files in a dedicated directory. Such features can be queried for, via IsEnabled, outside
// of snapd.
func (f SnapdFeature) IsExported() bool {
	return featuresExported[f]
}

// ControlFile returns the path of the file controlling the exported feature.
//
// Snapd considers the feature enabled if the file is present.
// The contents of the file are not important.
//
// The function panics for features that are not exported.
func (f SnapdFeature) ControlFile() string {
	if !f.IsExported() {
		panic(fmt.Sprintf("cannot compute the control file of feature %q because that feature is not exported", f))
	}
	return filepath.Join(dirs.FeaturesDir, f.String())
}

// ConfigOption returns the snap name and configuration option associated with this feature.
func (f SnapdFeature) ConfigOption() (snapName, confName string) {
	return "core", "experimental." + f.String()
}

// IsEnabled checks if a given exported snapd feature is enabled.
//
// The function panics for features that are not exported.
func (f SnapdFeature) IsEnabled() bool {
	if !f.IsExported() {
		panic(fmt.Sprintf("cannot check if feature %q is enabled because that feature is not exported", f))
	}
	return osutil.FileExists(f.ControlFile())
}
