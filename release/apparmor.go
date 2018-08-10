// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package release

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// ApparmorLevelType encodes the kind of support for apparmor
// found on this system.
type AppArmorLevelType int

const (
	// UninitializedAppArmor indicates that no apparmor detection was
	// done yet
	UninitializedAppArmorDetection AppArmorLevelType = iota
	// NoAppArmor indicates that apparmor is not enabled.
	NoAppArmor
	// PartialAppArmor indicates that apparmor is enabled but some
	// features are missing.
	PartialAppArmor
	// FullAppArmor indicates that all features are supported.
	FullAppArmor
)

var (
	appArmorLevel   AppArmorLevelType
	appArmorSummary string
)

// AppArmorLevel quantifies how well apparmor is supported on the
// current kernel.
func AppArmorLevel() AppArmorLevelType {
	if appArmorSummary == "" {
		appArmorLevel, appArmorSummary = probeAppArmor()
	}
	return appArmorLevel
}

// AppArmorSummary describes how well apparmor is supported on the
// current kernel.
func AppArmorSummary() string {
	if appArmorLevel == UninitializedAppArmorDetection {
		appArmorLevel, appArmorSummary = probeAppArmor()
	}
	return appArmorSummary
}

// MockAppArmorSupportLevel makes the system believe it has certain
// level of apparmor support.
func MockAppArmorLevel(level AppArmorLevelType) (restore func()) {
	oldAppArmorLevel := appArmorLevel
	oldAppArmorSummary := appArmorSummary
	appArmorLevel = level
	appArmorSummary = fmt.Sprintf("mocked apparmor level: %v", level)
	return func() {
		appArmorLevel = oldAppArmorLevel
		appArmorSummary = oldAppArmorSummary
	}
}

// probe related code
var (
	appArmorFeaturesSysPath  = "/sys/kernel/security/apparmor/features"
	requiredAppArmorFeatures = []string{
		"caps",
		"dbus",
		"domain",
		"file",
		"mount",
		"namespaces",
		"network",
		"ptrace",
		"signal",
	}
)

// isDirectoy is like osutil.IsDirectory but we cannot import this
// because of import cycles
func isDirectory(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.IsDir()
}

var (
	osGetuid             = os.Getuid
	apparmorProfilesPath = "/sys/kernel/security/apparmor/profiles"
)

func probeAppArmor() (AppArmorLevelType, string) {
	if !isDirectory(appArmorFeaturesSysPath) {
		return NoAppArmor, "apparmor not enabled"
	}

	// Check that apparmor is actually usable. In some
	// configurations of lxd, apparmor looks available when in
	// reality it isn't. Eg, this can happen when a container runs
	// unprivileged (eg, root in the container is non-root
	// outside) and also unconfined (where lxd doesn't set up an
	// apparmor policy namespace). We can therefore simply check
	// if /sys/kernel/security/apparmor/profiles is readable (like
	// aa-status does), and if it isn't, we know we can't manipulate
	// policy.
	if osGetuid() == 0 {
		f, err := os.Open(apparmorProfilesPath)
		if os.IsPermission(err) {
			return NoAppArmor, "apparmor detected but insufficient permissions to use it"
		}
		f.Close()
	}

	// Check apparmor features
	var missing []string
	for _, feature := range requiredAppArmorFeatures {
		if !isDirectory(filepath.Join(appArmorFeaturesSysPath, feature)) {
			missing = append(missing, feature)
		}
	}
	if len(missing) > 0 {
		return PartialAppArmor, fmt.Sprintf("apparmor is enabled but some features are missing: %s", strings.Join(missing, ", "))
	}
	return FullAppArmor, "apparmor is enabled and all features are available"
}

// AppArmorFeatures returns a sorted list of apparmor features like
// []string{"dbus", "network"}.
func AppArmorFeatures() []string {
	// note that ioutil.ReadDir() is already sorted
	dentries, err := ioutil.ReadDir(appArmorFeaturesSysPath)
	if err != nil {
		return nil
	}
	appArmorFeatures := make([]string, 0, len(dentries))
	for _, f := range dentries {
		if isDirectory(filepath.Join(appArmorFeaturesSysPath, f.Name())) {
			appArmorFeatures = append(appArmorFeatures, f.Name())
		}
	}
	return appArmorFeatures
}
