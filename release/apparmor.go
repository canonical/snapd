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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/strutil"
)

// ApparmorLevelType encodes the kind of support for apparmor
// found on this system.
type AppArmorLevelType int

const (
	// NoAppArmor indicates that apparmor is not enabled.
	NoAppArmor AppArmorLevelType = iota
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
	if appArmorSummary == "" {
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

// runApparmorParser runs the apparmor parser with the given option and input
func runApparmorParser(option, input string) error {
	cmd := exec.Command("apparmor_parser", option)
	cmd.Stdin = bytes.NewBufferString(input)
	if output, err := cmd.CombinedOutput(); err != nil {
		// we cannot import osutil.OutputErr here (import cycle)
		return fmt.Errorf("%s\n%s", err, output)
	}
	return nil
}

// we cannot import logger (import cycles)
func debug(format string, a ...interface{}) {
	if os.Getenv("SNAPD_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, format, a...)
	}
}

func probeAppArmor() (AppArmorLevelType, string) {
	if !isDirectory(appArmorFeaturesSysPath) {
		return NoAppArmor, "apparmor not enabled"
	}

	// Check if we can load an apparmor profiles at all, under some
	// systems (like lxd without apparmor stacking) this won't work
	canaryProfile := fmt.Sprintf("/canary-%s {}", strutil.MakeRandomString(20))
	if err := runApparmorParser("--add", canaryProfile); err != nil {
		debug("cannot load canary profile: %s", err)
		return NoAppArmor, "apparmor not usable, cannot load empty profile"
	}
	if err := runApparmorParser("--remove", canaryProfile); err != nil {
		debug("cannot unload canary profile: %s", err)
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
