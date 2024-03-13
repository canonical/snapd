// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2024 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/systemd"
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
	// ClassicPreservesXdgRuntimeDir controls $XDG_RUNTIME_DIR in snaps with classic confinement.
	ClassicPreservesXdgRuntimeDir
	// RobustMountNamespaceUpdates controls how snap-update-ns updates existing mount namespaces.
	RobustMountNamespaceUpdates
	// UserDaemons controls availability of user mode service support.
	UserDaemons
	// DbusActivation controls whether snaps daemons can be activated via D-Bus
	DbusActivation
	// HiddenSnapDataHomeDir controls if the snaps' data dir is ~/.snap/data instead of ~/snap
	HiddenSnapDataHomeDir
	// MoveSnapHomeDir controls whether snap user data under ~/snap (or ~/.snap/data) can be moved to ~/Snap.
	MoveSnapHomeDir
	// CheckDiskSpaceRemove controls free disk space check on remove whenever automatic snapshot needs to be created.
	CheckDiskSpaceRemove
	// CheckDiskSpaceInstall controls free disk space check on snap install.
	CheckDiskSpaceInstall
	// CheckDiskSpaceRefresh controls free disk space check on snap refresh.
	CheckDiskSpaceRefresh
	// GateAutoRefreshHook enables refresh control from snaps via gate-auto-refresh hook.
	GateAutoRefreshHook
	// QuotaGroups enables any current experimental features related to the Quota Groups API, on top of the features
	// already graduated past experimental:
	//  * journal quotas are still experimental
	// while guota groups creation and management and memory, cpu, quotas are no longer experimental.
	QuotaGroups
	// RefreshAppAwarenessUX enables experimental UX improvements for refresh-app-awareness.
	RefreshAppAwarenessUX
	// AspectsConfiguration enables experimental aspect-based configuration.
	AspectsConfiguration
	// AppArmorPrompting enables AppArmor to prompt the user for permission when apps perform certain operations.
	AppArmorPrompting

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

	ClassicPreservesXdgRuntimeDir: "classic-preserves-xdg-runtime-dir",
	RobustMountNamespaceUpdates:   "robust-mount-namespace-updates",

	UserDaemons:    "user-daemons",
	DbusActivation: "dbus-activation",

	HiddenSnapDataHomeDir: "hidden-snap-folder",
	MoveSnapHomeDir:       "move-snap-home-dir",

	CheckDiskSpaceInstall: "check-disk-space-install",
	CheckDiskSpaceRefresh: "check-disk-space-refresh",
	CheckDiskSpaceRemove:  "check-disk-space-remove",

	GateAutoRefreshHook: "gate-auto-refresh-hook",

	QuotaGroups: "quota-groups",

	RefreshAppAwarenessUX: "refresh-app-awareness-ux",
	AspectsConfiguration:  "aspects-configuration",

	AppArmorPrompting: "apparmor-prompting",
}

// featuresEnabledWhenUnset contains a set of features that are enabled when not explicitly configured.
var featuresEnabledWhenUnset = map[SnapdFeature]bool{
	Layouts:                       true,
	RefreshAppAwareness:           true,
	RobustMountNamespaceUpdates:   true,
	ClassicPreservesXdgRuntimeDir: true,
	DbusActivation:                true,
}

// featuresExported contains a set of features that are exported outside of snapd.
var featuresExported = map[SnapdFeature]bool{
	PerUserMountNamespace: true,
	RefreshAppAwareness:   true,
	ParallelInstances:     true,

	ClassicPreservesXdgRuntimeDir: true,
	RobustMountNamespaceUpdates:   true,
	HiddenSnapDataHomeDir:         true,
	MoveSnapHomeDir:               true,

	RefreshAppAwarenessUX: true,
	AspectsConfiguration:  true,
}

// featuresSupportedCallbacks maps features to a callback function which may be
// run to determine if the feature is supported and, if not, return false along
// with a reason why the feature is unsupported. If a function has no callback
// defined, it should be assumed to be supported.
var featuresSupportedCallbacks = map[SnapdFeature]func() (bool, string){
	// QuotaGroups requires systemd version 230 or higher
	QuotaGroups: func() (bool, string) {
		if err := systemd.EnsureAtLeast(230); err != nil {
			return false, err.Error()
		}
		return true, ""
	},
	// UserDaemons requires user units
	UserDaemons: func() (bool, string) {
		if !release.SystemctlSupportsUserUnits() {
			return false, "user session daemons are not supported on this system's distribution version"
		}
		return true, ""
	},
	// AppArmorPrompting requires a newer version of snapd with all the
	// prompting components in place. TODO: change this callback once ready.
	AppArmorPrompting: func() (bool, string) {
		return false, "requires newer version of snapd"
	},
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

	// TODO: this returns false on errors != ErrNotExist.
	// Consider using os.Stat and handling other errors
	return osutil.FileExists(f.ControlFile())
}

type confGetter interface {
	GetMaybe(snapName, key string, result interface{}) error
}

// Flag returns whether the given feature flag is enabled.
func Flag(tr confGetter, feature SnapdFeature) (bool, error) {
	var isEnabled interface{}
	snapName, confName := feature.ConfigOption()
	if err := tr.GetMaybe(snapName, confName, &isEnabled); err != nil {
		return false, err
	}
	switch isEnabled {
	case true, "true":
		return true, nil
	case false, "false":
		return false, nil
	case nil, "":
		return feature.IsEnabledWhenUnset(), nil
	}
	return false, fmt.Errorf("%s can only be set to 'true' or 'false', got %q", feature, isEnabled)
}

// FeatureInfo records whether a particular feature is supported and/or enabled.
//
// If the feature is not supported, it should also contain a reason describing
// why the feature is not supported. A feature is enabled if its feature flag is
// set to true, regardless of whether or not it is supported.
type FeatureInfo struct {
	Supported         bool   `json:"supported"`
	UnsupportedReason string `json:"unsupported-reason,omitempty"`
	Enabled           bool   `json:"enabled"`
}

// All returns a map from feature flags to information about that feature.
//
// In particular, the information contains whether the feature is supported
// and/or enabled. If the feature is not supported, it should also contain a
// reason describing why the feature is not supported. If a feature's value is
// not set to true or false, it is excluded from the list, since it is not in
// this case considered to be a feature flag.
func All(tr confGetter) map[string]FeatureInfo {
	knownFeatures := KnownFeatures()
	allFeaturesInfo := make(map[string]FeatureInfo, len(knownFeatures))
	for _, feature := range knownFeatures {
		enabled, err := Flag(tr, feature)
		if err != nil {
			// Skip features with values other than true or false
			continue
		}
		// Features implicitly supported if no callback exists
		supported := true
		var unsupportedReason string
		if callback, exists := featuresSupportedCallbacks[feature]; exists {
			supported, unsupportedReason = callback()
		}
		name := feature.String()
		info := FeatureInfo{
			Supported: supported,
			Enabled:   enabled,
		}
		if !supported {
			info.UnsupportedReason = unsupportedReason
		}
		allFeaturesInfo[name] = info
	}
	return allFeaturesInfo
}
