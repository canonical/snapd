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

// Probe checks which apparmor features are available.
//
// The error
func Probe() (FeatureLevel, error) {
	_, err := os.Stat(featuresSysPath)
	if err != nil {
		return None, fmt.Errorf("apparmor feature directory not found: %s", err)
	}
	for _, feature := range requiredFeatures {
		p := filepath.Join(featuresSysPath, feature)
		if _, err := os.Stat(p); err != nil {
			return Partial, fmt.Errorf("apparmor feature %q not found: %s", feature, err)
		}
	}
	return Full, nil
}

// MockFeatureLevel fakes the desired apparmor feature level.
func MockFeatureLevel(level FeatureLevel) (restore func()) {
	oldFeaturesSysPath := featuresSysPath

	temp, err := ioutil.TempDir("", "mock-apparmor-feature-level")
	if err != nil {
		panic(err)
	}
	fakeFeaturesSysPath := filepath.Join(temp, "features")

	switch level {
	case None:
		// create no directory at all (apparmor not available).
		break
	case Partial:
		// create just the empty directory with no features.
		if err := os.MkdirAll(fakeFeaturesSysPath, 0755); err != nil {
			panic(err)
		}
		break
	case Full:
		// create all the feature directories.
		for _, feature := range requiredFeatures {
			if err := os.MkdirAll(filepath.Join(fakeFeaturesSysPath, feature), 0755); err != nil {
				panic(err)
			}
		}
		break
	}

	featuresSysPath = fakeFeaturesSysPath
	return func() {
		if err := os.RemoveAll(temp); err != nil {
			panic(err)
		}
		featuresSysPath = oldFeaturesSysPath
	}
}
