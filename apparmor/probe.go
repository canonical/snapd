// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package apparmor

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FeatureLevel encodes the kind of support for apparmor found on this system.
type FeatureLevel int

const (
	// None indicates that apparmor is not enabled.
	None FeatureLevel = iota
	// Partial indicates that apparmor is enabled but some features are missing.
	Partial
	// Full indicates that all features are supported.
	Full
)

var (
	// featureSysPath points to the sysfs directory where apparmor features are listed.
	featuresSysPath = "/sys/kernel/security/apparmor/features"
	// requiredFeatures are the apparmor features needed for strict confinement.
	requiredFeatures = []string{
		"caps",
		"dbus",
		"domain",
		"file",
		"mount",
		"namespaces",
		"network",
		"ptrace",
		"rlimit",
		"signal",
	}
)

// KernelSupport describes apparmor features supported by the kernel.
type KernelSupport struct {
	enabled  bool
	features map[string]bool
}

// ProbeKernel checks which apparmor features are available.
func ProbeKernel() *KernelSupport {
	entries, err := ioutil.ReadDir(featuresSysPath)
	if err != nil {
		return nil
	}
	ks := &KernelSupport{
		enabled:  err == nil,
		features: make(map[string]bool, len(entries)),
	}
	for _, entry := range entries {
		// Each sub-directory represents a speicfic feature. Some have more
		// details as additional sub-directories or files therein but we are
		// not inspecting that at the moment.
		if entry.IsDir() {
			ks.features[entry.Name()] = true
		}
	}
	return ks
}

// IsEnabled returns true if apparmor is enabled.
func (ks *KernelSupport) IsEnabled() bool {
	return ks != nil && ks.enabled
}

// SupportsFeature returns true if a given apparmor feature is supported.
func (ks *KernelSupport) SupportsFeature(feature string) bool {
	return ks != nil && ks.features[feature]
}

// Evaluate checks if the apparmor module is enabled and if all the required features are available.
func (ks *KernelSupport) Evaluate() (level FeatureLevel, summary string) {
	if !ks.IsEnabled() {
		return None, fmt.Sprintf("apparmor is not enabled")
	}
	var missing []string
	for _, feature := range requiredFeatures {
		if !ks.SupportsFeature(feature) {
			missing = append(missing, feature)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return Partial, fmt.Sprintf("apparmor is enabled but some features are missing: %s", strings.Join(missing, ", "))
	}
	return Full, "apparmor is enabled and all features are available"
}

// MockFeatureLevel fakes the desired apparmor feature level.
func MockFeatureLevel(level FeatureLevel) (restore func()) {
	oldFeaturesSysPath := featuresSysPath

	temp, err := ioutil.TempDir("", "mock-apparmor-feature-level")
	if err != nil {
		panic(err)
	}
	featuresSysPath = filepath.Join(temp, "features")

	switch level {
	case None:
		// create no directory at all (apparmor not available).
	case Partial:
		// create several feature directories, matching vanilla 4.12 kernel.
		for _, feature := range []string{"caps", "domain", "file", "network", "policy", "rlimit"} {
			if err := os.MkdirAll(filepath.Join(featuresSysPath, feature), 0755); err != nil {
				panic(err)
			}
		}
	case Full:
		// create all the feature directories, matching Ubuntu kernels.
		for _, feature := range requiredFeatures {
			if err := os.MkdirAll(filepath.Join(featuresSysPath, feature), 0755); err != nil {
				panic(err)
			}
		}
	}

	return func() {
		if err := os.RemoveAll(temp); err != nil {
			panic(err)
		}
		featuresSysPath = oldFeaturesSysPath
	}
}
